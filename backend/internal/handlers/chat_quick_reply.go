package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

// ChatQuickReplyHandler exposes /api/admin/quick-replies CRUD.
// Templates are global (not per-OA / per-admin) — keeps Phase 4 simple.
type ChatQuickReplyHandler struct {
	repo      *repository.ChatQuickReplyRepo
	auditRepo *repository.AuditLogRepo
}

func NewChatQuickReplyHandler(repo *repository.ChatQuickReplyRepo, auditRepo *repository.AuditLogRepo) *ChatQuickReplyHandler {
	return &ChatQuickReplyHandler{repo: repo, auditRepo: auditRepo}
}

func (h *ChatQuickReplyHandler) audit(c *gin.Context, action string, detail map[string]interface{}) {
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

// GET /api/admin/quick-replies
func (h *ChatQuickReplyHandler) List(c *gin.Context) {
	rows, err := h.repo.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// POST /api/admin/quick-replies
func (h *ChatQuickReplyHandler) Create(c *gin.Context) {
	var in models.ChatQuickReplyUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := &models.ChatQuickReply{
		Label:     in.Label,
		Body:      in.Body,
		SortOrder: in.SortOrder,
	}
	if uid := c.GetString("user_id"); uid != "" {
		q.CreatedBy = &uid
	}
	if err := h.repo.Create(q); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_quick_reply_created", map[string]interface{}{
		"template_id": q.ID,
		"label":       q.Label,
	})
	c.JSON(http.StatusCreated, q)
}

// PUT /api/admin/quick-replies/:id
func (h *ChatQuickReplyHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var in models.ChatQuickReplyUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q, err := h.repo.Update(id, in)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_quick_reply_updated", map[string]interface{}{
		"template_id": id,
		"label":       in.Label,
	})
	c.JSON(http.StatusOK, q)
}

// DELETE /api/admin/quick-replies/:id
func (h *ChatQuickReplyHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	// Snapshot label before delete so /logs preserves what disappeared.
	var labelSnapshot string
	if rows, _ := h.repo.List(); rows != nil {
		for _, r := range rows {
			if r.ID == id {
				labelSnapshot = r.Label
				break
			}
		}
	}
	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_quick_reply_deleted", map[string]interface{}{
		"template_id": id,
		"label":       labelSnapshot,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
