package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/shopeestock"
)

type ShopeeStockHandler struct {
	service *shopeestock.Service
	aliases *repository.MarketplaceAliasRepo
	audit   *repository.AuditLogRepo
	log     *zap.Logger
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func firstNonEmptyHandler(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func NewShopeeStockHandler(service *shopeestock.Service, aliases *repository.MarketplaceAliasRepo, audit *repository.AuditLogRepo, log *zap.Logger) *ShopeeStockHandler {
	return &ShopeeStockHandler{service: service, aliases: aliases, audit: audit, log: log}
}

func (h *ShopeeStockHandler) Overview(c *gin.Context) {
	shopID, err := optionalInt64(c.Query("shop_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shop_id ไม่ถูกต้อง"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
	result, err := h.service.Overview(c.Request.Context(), shopID, shopeestock.ProductFilter{Status: c.Query("status"), Query: c.Query("q"), Page: page, Size: size})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ShopeeStockHandler) ProductGroups(c *gin.Context) {
	if !h.service.GroupedUIEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "grouped_ui_disabled"})
		return
	}
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	limit := marketplacePageLimit(c.Query("limit"), 30, 50)
	after, err := optionalInt64(c.Query("cursor"))
	if err != nil || after < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ไม่ถูกต้อง กรุณาโหลดหน้าแรกใหม่"})
		return
	}
	groups, hasMore, err := h.service.ProductGroups(c.Request.Context(), shopID, shopeestock.ProductGroupFilter{
		Status: c.Query("status"), Query: c.Query("q"), Limit: limit, AfterItemID: after,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	next := ""
	if hasMore && len(groups) > 0 {
		next = strconv.FormatInt(groups[len(groups)-1].ItemID, 10)
	}
	c.JSON(http.StatusOK, gin.H{"data": groups, "has_more": hasMore, "next_cursor": next})
}

func (h *ShopeeStockHandler) ProductGroupVariants(c *gin.Context) {
	if !h.service.GroupedUIEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "grouped_ui_disabled"})
		return
	}
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(c.Param("item_id"), 10, 64)
	if err != nil || itemID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id ไม่ถูกต้อง"})
		return
	}
	after, err := optionalInt64(c.Query("cursor"))
	if err != nil || after < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ไม่ถูกต้อง กรุณาเปิดกลุ่มใหม่"})
		return
	}
	variants, hasMore, err := h.service.ProductGroupVariants(c.Request.Context(), shopID, shopeestock.ProductVariantFilter{
		ItemID: itemID, Status: c.Query("status"), Query: c.Query("q"), Limit: marketplacePageLimit(c.Query("limit"), 50, 100), AfterModelID: after,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	next := ""
	if hasMore && len(variants) > 0 {
		next = strconv.FormatInt(variants[len(variants)-1].ModelID, 10)
	}
	c.JSON(http.StatusOK, gin.H{"data": variants, "has_more": hasMore, "next_cursor": next})
}

func (h *ShopeeStockHandler) UpdateSettings(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	var request shopeestock.SettingsUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลตั้งค่าไม่ถูกต้อง"})
		return
	}
	result, err := h.service.UpdateSettings(c.Request.Context(), shopID, request, c.GetString("user_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	h.auditChange(c, "shopee_stock_settings_updated", shopID, map[string]any{
		"enabled": result.Enabled, "stock_pct": result.StockPct,
		"scope_mode": result.ScopeMode, "location_count": len(result.Locations),
		"schedule_mode": result.ScheduleMode, "interval_seconds": result.IntervalSeconds,
		"monthly_interval": result.MonthlyInterval, "monthly_day": result.MonthlyDay,
		"monthly_time": result.MonthlyTime, "schedule_risk_acknowledged": result.ScheduleRiskAcknowledged,
	})
	c.JSON(http.StatusOK, result)
}

func (h *ShopeeStockHandler) SyncCatalog(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	result, err := h.service.SyncCatalog(c.Request.Context(), shopID)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.auditChange(c, "shopee_stock_catalog_synced", shopID, map[string]any{"run_id": result.ID, "total_count": result.TotalCount})
	c.JSON(http.StatusOK, result)
}

func (h *ShopeeStockHandler) Preview(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	var request struct {
		AsOfDate string `json:"as_of_date"`
	}
	_ = c.ShouldBindJSON(&request)
	if strings.TrimSpace(request.AsOfDate) == "" {
		request.AsOfDate = shopeestock.TodayBangkok()
	}
	runID, err := h.service.QueuePreview(c.Request.Context(), shopID, request.AsOfDate)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.auditChange(c, "shopee_stock_preview_queued", shopID, map[string]any{"run_id": runID, "as_of_date": request.AsOfDate})
	c.JSON(http.StatusAccepted, gin.H{"run_id": runID, "status": "queued"})
}

func (h *ShopeeStockHandler) PreviewRun(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	runID := strings.TrimSpace(c.Param("run_id"))
	if !uuidPattern.MatchString(runID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id ไม่ถูกต้อง"})
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), shopID, runID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *ShopeeStockHandler) PreviewRunLines(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	runID := strings.TrimSpace(c.Param("run_id"))
	if !uuidPattern.MatchString(runID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id ไม่ถูกต้อง"})
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "changed" && status != "unchanged" && status != "blocked" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status ไม่ถูกต้อง"})
		return
	}
	after, err := optionalInt64(c.Query("cursor"))
	if err != nil || after < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cursor ไม่ถูกต้อง"})
		return
	}
	lines, hasMore, err := h.service.RunLines(c.Request.Context(), shopID, runID, status, after, marketplacePageLimit(c.Query("limit"), 50, 100))
	if err != nil {
		h.fail(c, err)
		return
	}
	next := ""
	if hasMore && len(lines) > 0 {
		next = strconv.FormatInt(lines[len(lines)-1].LineOrder, 10)
	}
	c.JSON(http.StatusOK, gin.H{"data": lines, "has_more": hasMore, "next_cursor": next})
}

func (h *ShopeeStockHandler) Run(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	result, err := h.service.RunSync(c.Request.Context(), shopID, "manual")
	if err != nil {
		h.fail(c, err)
		return
	}
	h.auditChange(c, "shopee_stock_synced", shopID, map[string]any{"run_id": result.RunID, "changed_count": result.ChangedCount, "error_count": result.ErrorCount, "unknown_count": result.UnknownCount})
	c.JSON(http.StatusOK, result)
}

func (h *ShopeeStockHandler) UpdateMapping(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(c.Param("item_id"), 10, 64)
	if err != nil || itemID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id ไม่ถูกต้อง"})
		return
	}
	modelID, err := strconv.ParseInt(c.Param("model_id"), 10, 64)
	if err != nil || modelID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id ไม่ถูกต้อง"})
		return
	}
	var request shopeestock.MappingUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลการจับคู่ไม่ถูกต้อง"})
		return
	}
	if request.Excluded {
		result, err := h.service.UpdateMapping(c.Request.Context(), shopID, itemID, modelID, request, c.GetString("user_id"))
		if err != nil {
			h.fail(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}
	if h.aliases == nil || strings.TrimSpace(request.ImpactDigest) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาตรวจผลกระทบล่าสุดก่อนบันทึก Product Master"})
		return
	}
	product, err := h.service.ProductForMutation(c.Request.Context(), shopID, itemID, modelID)
	if err != nil {
		h.fail(c, err)
		return
	}
	scopeConfirmed := true
	result, err := h.aliases.CommitMutation(c.Request.Context(), repository.MarketplaceAliasProposal{
		AliasID: request.MarketplaceAliasID,
		Identity: models.MarketplaceAliasIdentity{
			Source: "shopee", AccountKey: "shop:" + strconv.FormatInt(shopID, 10),
			ExternalItemID: strconv.FormatInt(itemID, 10), ExternalVariantID: strconv.FormatInt(modelID, 10),
			SourceSKU: firstNonEmptyHandler(product.ModelSKU, product.ItemSKU), RawName: firstNonEmptyHandler(product.ModelName, product.ItemName),
		},
		BillType: "sale", ItemCode: request.SMLItemCode, UnitCode: request.SMLUnitCode,
		QuantityMultiplier: request.QuantityMultiplier, SalesEnabled: request.SalesEnabled, StockPolicy: request.StockPolicy,
		AcknowledgeManualUnmanaged: request.AcknowledgeManualUnmanaged,
		ScopeConfirmed:             &scopeConfirmed, MatchMethod: "manual_identity", ConfirmedBy: c.GetString("user_id"),
		ExpectedRevision: request.ExpectedRevision, ExpectedImpactDigest: request.ImpactDigest,
	})
	if errors.Is(err, repository.ErrMarketplaceAliasConflict) || errors.Is(err, repository.ErrMarketplaceImpactChanged) {
		c.JSON(http.StatusConflict, gin.H{"error": "Product Master หรือ stock job เปลี่ยนไปแล้ว กรุณารีเฟรชและตรวจผลกระทบใหม่"})
		return
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func (h *ShopeeStockHandler) SharedPool(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	result, err := h.service.GetSharedPool(c.Request.Context(), shopID, c.Query("sml_item_code"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ShopeeStockHandler) UpdateSharedPool(c *gin.Context) {
	shopID, ok := h.shopID(c)
	if !ok {
		return
	}
	var request shopeestock.SharedPoolUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลสต๊อกร่วมกันไม่ถูกต้อง"})
		return
	}
	result, err := h.service.UpdateSharedPool(c.Request.Context(), shopID, request, c.GetString("user_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ShopeeStockHandler) SearchCatalog(c *gin.Context) {
	items, err := h.service.SearchCatalog(c.Request.Context(), c.Query("q"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *ShopeeStockHandler) shopID(c *gin.Context) (int64, bool) {
	value, err := strconv.ParseInt(c.Param("shop_id"), 10, 64)
	if err != nil || value <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shop_id ไม่ถูกต้อง"})
		return 0, false
	}
	return value, true
}
func optionalInt64(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func (h *ShopeeStockHandler) fail(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := err.Error()
	var validation *shopeestock.ValidationError
	var gatewayErr *shopeeapi.GatewayError
	if errors.As(err, &validation) || errors.Is(err, shopeestock.ErrScopeRequired) ||
		errors.Is(err, shopeestock.ErrSelectedLocation) ||
		errors.Is(err, shopeestock.ErrDryRunRequired) {
		status = http.StatusUnprocessableEntity
	}
	if errors.Is(err, shopeestock.ErrUnavailable) || errors.Is(err, shopeestock.ErrGatewayOnly) {
		status = http.StatusServiceUnavailable
	}
	if errors.Is(err, shopeestock.ErrSyncInProgress) || errors.Is(err, shopeestock.ErrMappingConflict) || errors.Is(err, shopeestock.ErrBlockedReservations) || errors.Is(err, shopeestock.ErrPreviewStale) {
		status = http.StatusConflict
	}
	if errors.Is(err, repository.ErrMarketplaceStockZeroingInProgress) {
		status = http.StatusConflict
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	} else if errors.As(err, &gatewayErr) || strings.Contains(strings.ToLower(err.Error()), "sml stock") {
		status = http.StatusBadGateway
	}
	if errors.Is(err, shopeestock.ErrInvalidUnit) || errors.Is(err, shopeestock.ErrInvalidManualFactor) {
		status = http.StatusUnprocessableEntity
	}
	if errors.Is(err, shopeestock.ErrUnsafeManagedExclusion) ||
		errors.Is(err, repository.ErrMarketplaceUnsafeStockDisable) ||
		errors.Is(err, repository.ErrMarketplaceManualUnmanagedAcknowledgementRequired) {
		status = http.StatusUnprocessableEntity
	}
	if errors.Is(err, repository.ErrMarketplaceUnsafeStockDisable) {
		message = "กรุณาเลือกตั้ง stock เป็น 0 แล้วปิด หรือยืนยันว่าจะจัดการ stock เอง ก่อนหยุดให้ Nexflow จัดการรายการนี้"
	}
	if errors.Is(err, repository.ErrMarketplaceManualUnmanagedAcknowledgementRequired) {
		message = "กรุณายืนยันว่ารับทราบว่าจะตรวจและจัดการ stock Shopee รายการนี้เอง"
	}
	if errors.Is(err, repository.ErrMarketplaceStockZeroingInProgress) {
		message = "กำลังตั้ง stock เป็น 0 และรอผลยืนยันจาก Shopee กรุณารอให้งานเดิมเสร็จ"
	}
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	if status >= 500 {
		h.log.Warn("shopee stock", zap.Error(err))
	}
	c.JSON(status, gin.H{"error": message})
}

func (h *ShopeeStockHandler) auditChange(c *gin.Context, action string, shopID int64, detail map[string]any) {
	if h.audit == nil {
		return
	}
	target := strconv.FormatInt(shopID, 10)
	var userID *string
	if value := c.GetString("user_id"); value != "" {
		userID = &value
	}
	_ = h.audit.Log(models.AuditEntry{Action: action, TargetID: &target, UserID: userID, Source: "shopee_stock", Detail: detail})
}
