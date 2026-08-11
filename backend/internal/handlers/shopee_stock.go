package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
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
	audit   *repository.AuditLogRepo
	log     *zap.Logger
}

func NewShopeeStockHandler(service *shopeestock.Service, audit *repository.AuditLogRepo, log *zap.Logger) *ShopeeStockHandler {
	return &ShopeeStockHandler{service: service, audit: audit, log: log}
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
	h.auditChange(c, "shopee_stock_settings_updated", shopID, map[string]any{"enabled": result.Enabled, "stock_pct": result.StockPct, "scope_mode": result.ScopeMode, "location_count": len(result.Locations)})
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
	result, err := h.service.Preview(c.Request.Context(), shopID, request.AsOfDate)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.auditChange(c, "shopee_stock_previewed", shopID, map[string]any{"run_id": result.RunID, "changed_count": result.ChangedCount, "blocked_count": result.BlockedCount})
	c.JSON(http.StatusOK, result)
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
	result, err := h.service.UpdateMapping(c.Request.Context(), shopID, itemID, modelID, request, c.GetString("user_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	h.auditChange(c, "shopee_stock_mapping_updated", shopID, map[string]any{
		"item_id": itemID, "model_id": modelID, "sml_item_code": result.SMLItemCode,
		"sml_unit_code": result.SMLUnitCode, "manual_unit_factor": result.ManualUnitFactor,
		"excluded": result.Excluded,
	})
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
	if errors.Is(err, shopeestock.ErrSyncInProgress) || errors.Is(err, shopeestock.ErrMappingConflict) {
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
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	if status >= 500 {
		h.log.Warn("shopee stock", zap.Error(err))
	}
	c.JSON(status, gin.H{"error": err.Error()})
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
