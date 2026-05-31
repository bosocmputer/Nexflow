package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/insight"
	"nexflow/internal/services/sml"
)

type DashboardHandler struct {
	billRepo             *repository.BillRepo
	insightRepo          *repository.InsightRepo
	convRepo             *repository.ChatConversationRepo
	imapRepo             *repository.ImapAccountRepo
	lineOARepo           *repository.LineOAAccountRepo
	insightSvc           *insight.Service
	lineConfigured       bool
	imapConfigured       bool
	smlConfigured        bool
	smlReadiness         *sml.ReadinessChecker
	aiConfigured         bool
	autoConfirmThreshold float64
	log                  *zap.Logger
}

func NewDashboardHandler(
	billRepo *repository.BillRepo,
	insightRepo *repository.InsightRepo,
	convRepo *repository.ChatConversationRepo,
	imapRepo *repository.ImapAccountRepo,
	lineOARepo *repository.LineOAAccountRepo,
	insightSvc *insight.Service,
	log *zap.Logger,
) *DashboardHandler {
	return &DashboardHandler{
		billRepo:    billRepo,
		insightRepo: insightRepo,
		convRepo:    convRepo,
		imapRepo:    imapRepo,
		lineOARepo:  lineOARepo,
		insightSvc:  insightSvc,
		log:         log,
	}
}

// SetConfigStatus sets config flags for the settings status endpoint
func (h *DashboardHandler) SetConfigStatus(line, imap, sml, ai bool, threshold float64) {
	h.lineConfigured = line
	h.imapConfigured = imap
	h.smlConfigured = sml
	h.aiConfigured = ai
	h.autoConfirmThreshold = threshold
}

func (h *DashboardHandler) SetSMLReadiness(checker *sml.ReadinessChecker) {
	h.smlReadiness = checker
}

// GET /api/dashboard/stats
//
// Returns the existing bill stats plus `unread_messages` so the Sidebar can
// power both the /bills pending badge and the /messages unread badge with a
// single poll instead of two.
func (h *DashboardHandler) Stats(c *gin.Context) {
	stats, err := h.billRepo.DashboardStats()
	if err != nil {
		h.log.Error("DashboardStats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	// Wrap the stats struct as a map so we can attach extra fields without
	// changing the existing DashboardStats type signature.
	out := map[string]interface{}{}
	statsBytes, _ := json.Marshal(stats)
	_ = json.Unmarshal(statsBytes, &out)

	if h.convRepo != nil {
		if unread, err := h.convRepo.UnreadCount(); err == nil {
			out["unread_messages"] = unread
		}
	}
	// Email-inbox health — surfaces "1 inbox มีปัญหา" on the dashboard
	// without admin needing to open /settings/email to check status.
	if h.imapRepo != nil {
		if failing, err := h.imapRepo.CountFailing(); err == nil {
			out["email_inbox_errors"] = failing
		}
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/dashboard/insights — returns last 7 daily insights
func (h *DashboardHandler) Insights(c *gin.Context) {
	if h.insightRepo == nil {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
		return
	}
	items, err := h.insightRepo.List(7)
	if err != nil {
		h.log.Error("Insights", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if items == nil {
		items = []models.DailyInsight{}
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// POST /api/dashboard/insights/generate — on-demand F4 insight generation
func (h *DashboardHandler) GenerateInsight(c *gin.Context) {
	if h.insightSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service not configured"})
		return
	}

	stats, err := h.billRepo.DashboardStats()
	if err != nil {
		h.log.Error("GenerateInsight: get stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	statsBytes, _ := json.Marshal(stats)
	text, err := h.insightSvc.Generate(string(statsBytes))
	if err != nil {
		h.log.Error("GenerateInsight: AI", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI generation failed"})
		return
	}

	if h.insightRepo != nil {
		_ = h.insightRepo.Save(string(statsBytes), text)
	}

	c.JSON(http.StatusOK, gin.H{"insight": text})
}

// GET /api/settings/status — live system status for the /settings root page.
//
// Returns multi-account aware counts (LINE OA, IMAP) instead of the old
// env-flag booleans which were misleading once we moved both subsystems
// from .env into DB tables. Frontend renders one row per subsystem with
// click-through to the manage page.
func (h *DashboardHandler) SettingsStatus(c *gin.Context) {
	out := gin.H{
		// SML / AI come from env at boot — no live multi-account state to query.
		"sml_configured":         h.smlConfigured,
		"ai_configured":          h.aiConfigured,
		"auto_confirm_threshold": h.autoConfirmThreshold,
	}
	if h.smlReadiness != nil {
		readiness := h.smlReadiness.Check(c.Request.Context(), c.Query("refresh_sml") == "1")
		out["sml_readiness"] = readiness
		out["sml_configured"] = readiness.Configured
	}

	// LINE OA — count enabled vs total. Multi-OA in DB since session 13.
	if h.lineOARepo != nil {
		if rows, err := h.lineOARepo.ListAll(); err == nil {
			enabled := 0
			for _, r := range rows {
				if r.Enabled {
					enabled++
				}
			}
			out["line_oa_total"] = len(rows)
			out["line_oa_enabled"] = enabled
		}
	}

	// IMAP — count total + failing (consecutive_failures > 0). Multi-account
	// in DB since session 6.
	if h.imapRepo != nil {
		if rows, err := h.imapRepo.ListAll(); err == nil {
			enabled, failing := 0, 0
			for _, r := range rows {
				if r.Enabled {
					enabled++
					if r.ConsecutiveFailures > 0 {
						failing++
					}
				}
			}
			out["imap_total"] = len(rows)
			out["imap_enabled"] = enabled
			out["imap_failing"] = failing
		}
	}

	c.JSON(http.StatusOK, out)
}
