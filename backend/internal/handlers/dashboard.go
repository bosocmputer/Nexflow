package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	nextStepMarketplace  *sml.NextStepMarketplaceClient
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

func (h *DashboardHandler) SetNextStepMarketplace(client *sml.NextStepMarketplaceClient, _ *repository.AppSettingsRepo) {
	h.nextStepMarketplace = client
}

// GET /api/dashboard/stats
//
// Returns the existing bill stats plus `unread_messages` so the Sidebar can
// power both the /bills pending badge and the /messages unread badge with a
// single poll instead of two.
func (h *DashboardHandler) Stats(c *gin.Context) {
	fromDate := c.Query("from_date")
	toDate := c.Query("to_date")
	stats, err := h.billRepo.DashboardStatsForDateRange(fromDate, toDate)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidDashboardDateRange) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
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
	if c.Query("include_nextstep") == "1" {
		from, to := dashboardDateRangeForNextStep(out, fromDate, toDate)
		state := h.nextStepMarketplaceState(c.Request.Context(), from, to, 1, 5, "", "", false)
		if state["available"] == true {
			if previousFrom, previousTo := dashboardPreviousDateRange(out); previousFrom != "" && previousTo != "" {
				h.attachPreviousNextStepMarketplaceState(c.Request.Context(), state, previousFrom, previousTo)
			}
		}
		out["nextstep_marketplace"] = state
	}
	c.JSON(http.StatusOK, out)
}

func dashboardDateRangeForNextStep(stats map[string]interface{}, fromDate, toDate string) (string, string) {
	fromDate = strings.TrimSpace(fromDate)
	toDate = strings.TrimSpace(toDate)
	if fromDate != "" && toDate != "" {
		return fromDate, toDate
	}
	meta, ok := stats["platform_sales_meta"].(map[string]interface{})
	if !ok {
		return fromDate, toDate
	}
	if fromDate == "" {
		if v, ok := meta["from_date"].(string); ok {
			fromDate = v
		}
	}
	if toDate == "" {
		if v, ok := meta["to_date"].(string); ok {
			toDate = v
		}
	}
	return fromDate, toDate
}

func dashboardPreviousDateRange(stats map[string]interface{}) (string, string) {
	meta, ok := stats["platform_sales_meta"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	from, _ := meta["previous_from_date"].(string)
	to, _ := meta["previous_to_date"].(string)
	return strings.TrimSpace(from), strings.TrimSpace(to)
}

// NextStepMarketplaceOrders proxies SML marketplace MQT orders for the
// authenticated Nexflow UI. It soft-fails into a state object so the page can
// still render filters and setup actions when SML is unavailable.
func (h *DashboardHandler) NextStepMarketplaceOrders(c *gin.Context) {
	page := parsePositiveIntQuery(c, "page", 1)
	size := parsePositiveIntQuery(c, "size", 20)
	if size > 50 {
		size = 50
	}
	state := h.nextStepMarketplaceState(
		c.Request.Context(),
		strings.TrimSpace(c.Query("from_date")),
		strings.TrimSpace(c.Query("to_date")),
		page,
		size,
		strings.TrimSpace(c.Query("search")),
		strings.TrimSpace(c.Query("status")),
		true,
	)
	c.JSON(http.StatusOK, state)
}

func (h *DashboardHandler) nextStepMarketplaceState(ctx context.Context, fromDate, toDate string, page, size int, search, status string, includeOrders bool) gin.H {
	state := gin.H{
		"configured": false,
		"available":  false,
		"message":    "ยังไม่ได้ตั้งค่า NextStep Marketplace",
	}
	if h.nextStepMarketplace == nil {
		state["error"] = "not_configured"
		return state
	}
	if !h.nextStepMarketplace.IsConfigured() {
		state["error"] = "sml_not_configured"
		state["message"] = "ยังไม่ได้ตั้งค่า SML REST URL, API key หรือ tenant"
		return state
	}

	state["configured"] = true
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := h.nextStepMarketplace.Fetch(reqCtx, sml.NextStepMarketplaceRequest{
		DateFrom:      fromDate,
		DateTo:        toDate,
		Search:        search,
		Status:        status,
		Page:          page,
		Size:          size,
		IncludeOrders: &includeOrders,
	})
	if err != nil {
		h.log.Warn("nextstep marketplace dashboard fetch failed",
			zap.String("from_date", fromDate),
			zap.String("to_date", toDate),
			zap.String("search", search),
			zap.String("status", status),
			zap.Bool("include_orders", includeOrders),
			zap.Error(err),
		)
		state["error"] = "sml_unavailable"
		state["message"] = "โหลดข้อมูล NextStep Marketplace จาก SML ไม่สำเร็จ"
		return state
	}
	state["available"] = true
	state["message"] = "พร้อมใช้งาน"
	state["summary"] = data.Summary
	state["orders"] = data.Orders
	state["trend"] = data.Trend
	state["meta"] = data.Meta
	return state
}

func (h *DashboardHandler) attachPreviousNextStepMarketplaceState(ctx context.Context, state gin.H, fromDate, toDate string) {
	previous := h.nextStepMarketplaceState(ctx, fromDate, toDate, 1, 1, "", "", false)
	state["previous_available"] = previous["available"] == true
	if previous["available"] != true {
		state["previous_error"] = previous["error"]
		state["previous_message"] = previous["message"]
		return
	}
	state["previous_summary"] = previous["summary"]
	state["previous_trend"] = previous["trend"]
	state["previous_meta"] = previous["meta"]

	currentTotal := nextStepSummaryTotal(state["summary"])
	previousTotal := nextStepSummaryTotal(previous["summary"])
	state["change_pct"] = dashboardSalesChangePct(currentTotal, previousTotal)
}

func nextStepSummaryTotal(value interface{}) float64 {
	switch v := value.(type) {
	case sml.NextStepMarketplaceSummary:
		return v.TotalAmount
	case map[string]interface{}:
		if n, ok := v["total_amount"].(float64); ok {
			return n
		}
	}
	return 0
}

func dashboardSalesChangePct(current, previous float64) *float64 {
	if previous <= 0 {
		return nil
	}
	pct := math.Round(((current-previous)/previous*100)*10) / 10
	return &pct
}

func parsePositiveIntQuery(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
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
