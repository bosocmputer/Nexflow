package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
)

// ChatTagsHandler exposes:
//   /api/settings/chat-tags          — global tag CRUD
//   /api/admin/conversations/:user/tags — m2m attach/detach
type ChatTagsHandler struct {
	repo      *repository.ChatTagRepo
	auditRepo *repository.AuditLogRepo
	broker    *events.Broker
}

func NewChatTagsHandler(repo *repository.ChatTagRepo, auditRepo *repository.AuditLogRepo, broker *events.Broker) *ChatTagsHandler {
	return &ChatTagsHandler{repo: repo, auditRepo: auditRepo, broker: broker}
}

func (h *ChatTagsHandler) audit(c *gin.Context, action string, detail map[string]interface{}) {
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

// ── Global CRUD (admin only) ─────────────────────────────────────────────────

// GET /api/settings/chat-tags
func (h *ChatTagsHandler) ListAll(c *gin.Context) {
	rows, err := h.repo.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// POST /api/settings/chat-tags
func (h *ChatTagsHandler) Create(c *gin.Context) {
	var in models.ChatTagUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t := &models.ChatTag{Label: in.Label, Color: in.Color}
	if err := h.repo.Create(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_tag_created", map[string]interface{}{
		"tag_id": t.ID,
		"label":  t.Label,
		"color":  t.Color,
	})
	c.JSON(http.StatusCreated, t)
}

// PUT /api/settings/chat-tags/:id
func (h *ChatTagsHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var in models.ChatTagUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.repo.Update(id, in)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_tag_updated", map[string]interface{}{
		"tag_id": id,
		"label":  in.Label,
		"color":  in.Color,
	})
	c.JSON(http.StatusOK, t)
}

// DELETE /api/settings/chat-tags/:id
func (h *ChatTagsHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	// Snapshot label before delete so audit preserves what was removed (the
	// FK CASCADE also detaches every conversation, so this is a meaningful event).
	var labelSnapshot, colorSnapshot string
	if rows, _ := h.repo.ListAll(); rows != nil {
		for _, r := range rows {
			if r.ID == id {
				labelSnapshot = r.Label
				colorSnapshot = r.Color
				break
			}
		}
	}
	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "chat_tag_deleted", map[string]interface{}{
		"tag_id": id,
		"label":  labelSnapshot,
		"color":  colorSnapshot,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── Per-conversation tag attach ──────────────────────────────────────────────

// GET /api/admin/conversations/:lineUserId/tags
func (h *ChatTagsHandler) TagsForConversation(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	rows, err := h.repo.TagsForConversation(lineUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// PUT /api/admin/conversations/:lineUserId/tags
// Body: {tag_ids: ["uuid", "uuid", …]}  — replaces the full set
func (h *ChatTagsHandler) SetTagsForConversation(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	var in struct {
		TagIDs []string `json:"tag_ids"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.SetTagsForConversation(lineUserID, in.TagIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := h.repo.TagsForConversation(lineUserID)
	// Audit the resulting tag set (labels, not just IDs) so /logs is readable.
	labels := make([]string, 0, len(rows))
	for _, r := range rows {
		labels = append(labels, r.Label)
	}
	h.audit(c, "chat_conv_tags_set", map[string]interface{}{
		"line_user_id": lineUserID,
		"tag_count":    len(rows),
		"labels":       labels,
	})
	// Push the new tag set so other admin tabs see chips update without polling.
	if h.broker != nil {
		h.broker.Publish(events.Event{
			Type: events.TypeConversationUpdated,
			Payload: map[string]any{
				"line_user_id": lineUserID,
				"tags":         rows,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}
