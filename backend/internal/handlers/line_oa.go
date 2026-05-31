package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
)

// LineOAHandler exposes /api/settings/line-oa CRUD endpoints. Admin-only.
//
// Each Create/Update/Delete reloads the LineRegistry so the new credentials
// take effect immediately for the next webhook event or admin reply.
type LineOAHandler struct {
	repo      *repository.LineOAAccountRepo
	registry  *lineservice.Registry
	auditRepo *repository.AuditLogRepo
	logger    *zap.Logger
}

func NewLineOAHandler(
	repo *repository.LineOAAccountRepo,
	registry *lineservice.Registry,
	auditRepo *repository.AuditLogRepo,
	logger *zap.Logger,
) *LineOAHandler {
	return &LineOAHandler{
		repo:      repo,
		registry:  registry,
		auditRepo: auditRepo,
		logger:    logger,
	}
}

// maskAccount returns a copy with credentials redacted — used by list endpoint
// so accidentally-leaked logs don't expose tokens.
func maskAccount(a *models.LineOAAccount) *models.LineOAAccount {
	clone := *a
	clone.ChannelSecret = ""
	clone.ChannelAccessToken = ""
	return &clone
}

// GET /api/settings/line-oa
func (h *LineOAHandler) List(c *gin.Context) {
	rows, err := h.repo.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]*models.LineOAAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, maskAccount(r))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// GET /api/settings/line-oa/:id
//
// Returns the row WITH credentials so the admin's edit dialog can pre-fill
// (rendered behind a "show secret" toggle in the UI).
func (h *LineOAHandler) Get(c *gin.Context) {
	id := c.Param("id")
	a, err := h.repo.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// POST /api/settings/line-oa
func (h *LineOAHandler) Create(c *gin.Context) {
	var in models.LineOAAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.ChannelSecret == "" || in.ChannelAccessToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "channel_secret and channel_access_token are required",
		})
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	a := &models.LineOAAccount{
		Name:               in.Name,
		ChannelSecret:      in.ChannelSecret,
		ChannelAccessToken: in.ChannelAccessToken,
		AdminUserID:        in.AdminUserID,
		Greeting:           in.Greeting,
		Enabled:            enabled,
	}
	if err := h.repo.Create(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Try to fetch + cache the bot user ID so webhook routing by Destination works.
	h.tryFillBotUserID(a)

	if err := h.registry.Reload(); err != nil {
		h.logger.Warn("registry reload after create", zap.Error(err))
	}
	h.audit(c, "line_oa_created", a.ID, map[string]interface{}{"name": a.Name})
	c.JSON(http.StatusCreated, a)
}

// PUT /api/settings/line-oa/:id
func (h *LineOAHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var in models.LineOAAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := h.repo.Update(id, in)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Refresh bot_user_id when secret/token changed.
	if in.ChannelAccessToken != "" {
		h.tryFillBotUserID(updated)
	}
	if err := h.registry.Reload(); err != nil {
		h.logger.Warn("registry reload after update", zap.Error(err))
	}
	h.audit(c, "line_oa_updated", id, map[string]interface{}{"name": updated.Name})
	c.JSON(http.StatusOK, updated)
}

// DELETE /api/settings/line-oa/:id
func (h *LineOAHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ลบไม่สำเร็จ — อาจมีบทสนทนาผูกอยู่ (ลบ/ย้ายห้องก่อน): " + err.Error(),
		})
		return
	}
	if err := h.registry.Reload(); err != nil {
		h.logger.Warn("registry reload after delete", zap.Error(err))
	}
	h.audit(c, "line_oa_deleted", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/settings/line-oa/:id/test
//
// Fetches /v2/bot/info using the OA's access_token. Success means the token
// is valid; the bot's user ID is cached for webhook routing.
func (h *LineOAHandler) Test(c *gin.Context) {
	id := c.Param("id")
	a, err := h.repo.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	svc, err := lineservice.New(a.ChannelSecret, a.ChannelAccessToken, a.AdminUserID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "service init failed: " + err.Error()})
		return
	}
	info, err := svc.GetBotInfo()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "LINE API call failed: " + err.Error()})
		return
	}
	if info != nil && info.UserID != "" {
		_ = h.repo.SetBotUserID(id, info.UserID)
		_ = h.registry.Reload()
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"bot_user_id":  info.UserID,
		"display_name": info.DisplayName,
		"basic_id":     info.BasicID,
		"premium_id":   info.PremiumID,
	})
}

// tryFillBotUserID does a best-effort /v2/bot/info call to cache bot_user_id
// after Create/Update. Non-fatal if it fails — webhook routing falls back to
// URL :oaId or registry.Any().
func (h *LineOAHandler) tryFillBotUserID(a *models.LineOAAccount) {
	svc, err := lineservice.New(a.ChannelSecret, a.ChannelAccessToken, a.AdminUserID)
	if err != nil {
		return
	}
	info, err := svc.GetBotInfo()
	if err != nil || info == nil || info.UserID == "" {
		return
	}
	_ = h.repo.SetBotUserID(a.ID, info.UserID)
	a.BotUserID = info.UserID
}

func (h *LineOAHandler) audit(c *gin.Context, action, targetID string, detail map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	id := targetID
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   action,
		TargetID: &id,
		UserID:   userID,
		Source:   "line_oa",
		Level:    "info",
		TraceID:  c.GetString("trace_id"),
		Detail:   detail,
	})
}
