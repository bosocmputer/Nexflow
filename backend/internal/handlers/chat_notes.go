package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

// ChatNotesHandler exposes /api/admin/conversations/:lineUserId/notes
// (Phase 4.8 — internal admin annotations on a conversation).
type ChatNotesHandler struct {
	repo      *repository.ChatNoteRepo
	auditRepo *repository.AuditLogRepo
}

func NewChatNotesHandler(repo *repository.ChatNoteRepo, auditRepo *repository.AuditLogRepo) *ChatNotesHandler {
	return &ChatNotesHandler{repo: repo, auditRepo: auditRepo}
}

func (h *ChatNotesHandler) audit(c *gin.Context, action string, detail map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	var uid *string
	if u := c.GetString("user_id"); u != "" {
		uid = &u
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:  action,
		UserID:  uid,
		Source:  "line",
		Level:   "info",
		TraceID: c.GetString("trace_id"),
		Detail:  detail,
	})
}

// GET /api/admin/conversations/:lineUserId/notes
func (h *ChatNotesHandler) List(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	rows, err := h.repo.ListByUser(lineUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// POST /api/admin/conversations/:lineUserId/notes
func (h *ChatNotesHandler) Create(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	var in models.ChatNoteUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n := &models.ChatNote{LineUserID: lineUserID, Body: in.Body}
	if uid := c.GetString("user_id"); uid != "" {
		n.CreatedBy = &uid
	}
	if err := h.repo.Create(n); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_note_created", map[string]interface{}{
		"line_user_id": lineUserID,
		"note_id":      n.ID,
		"body_preview": preview(in.Body, 80),
	})
	c.JSON(http.StatusCreated, n)
}

// PUT /api/admin/conversations/:lineUserId/notes/:noteId
func (h *ChatNotesHandler) Update(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	noteID := c.Param("noteId")
	var in models.ChatNoteUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n, err := h.repo.Update(noteID, in.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_note_updated", map[string]interface{}{
		"line_user_id": lineUserID,
		"note_id":      noteID,
		"body_preview": preview(in.Body, 80),
	})
	c.JSON(http.StatusOK, n)
}

// DELETE /api/admin/conversations/:lineUserId/notes/:noteId
func (h *ChatNotesHandler) Delete(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	noteID := c.Param("noteId")
	// Snapshot the body before delete so the audit row preserves what was lost.
	var bodySnapshot string
	if rows, _ := h.repo.ListByUser(lineUserID); rows != nil {
		for _, r := range rows {
			if r.ID == noteID {
				bodySnapshot = r.Body
				break
			}
		}
	}
	if err := h.repo.Delete(noteID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_note_deleted", map[string]interface{}{
		"line_user_id": lineUserID,
		"note_id":      noteID,
		"body_preview": preview(bodySnapshot, 80),
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// preview truncates s to n runes plus "…" — used for audit detail so /logs
// shows a hint of what was added/removed without bloating the JSON.
func preview(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
