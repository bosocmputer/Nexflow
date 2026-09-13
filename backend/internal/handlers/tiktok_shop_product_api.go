package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/tiktokshop"
)

type tikTokProductCatalogSyncRequest struct {
	ShopID string `json:"shop_id"`
}

func (h *TikTokShopAPIHandler) SyncProductCatalog(c *gin.Context) {
	if !h.productCatalogReady(c, true) {
		return
	}
	var input tikTokProductCatalogSyncRequest
	if c.ShouldBindJSON(&input) != nil || !tiktokshop.ValidTikTokShopID(strings.TrimSpace(input.ShopID)) {
		h.error(c, http.StatusBadRequest, "invalid_request", "Shop ID ของ TikTok Shop ไม่ถูกต้อง")
		return
	}
	input.ShopID = strings.TrimSpace(input.ShopID)
	result, err := h.productCatalog.Sync(c.Request.Context(), input.ShopID, "manual")
	if err != nil {
		status, code, message := productCatalogHandlerError(err)
		h.logger.Warn("tiktok_shop_product_catalog_sync_failed",
			zap.String("shop_id", input.ShopID), zap.String("error_code", code),
			zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		h.auditProductCatalog(c, "tiktok_shop_product_catalog_sync_failed", input.ShopID, "error", map[string]any{"error_code": code})
		h.error(c, status, code, message)
		return
	}
	h.logger.Info("tiktok_shop_product_catalog_synced",
		zap.String("shop_id", result.ShopID), zap.String("run_id", result.ID),
		zap.Int("product_count", result.ProductCount), zap.Int("sku_count", result.SKUCount),
		zap.Int("warehouse_count", result.WarehouseCount), zap.String("trace_id", c.GetString("trace_id")))
	h.auditProductCatalog(c, "tiktok_shop_product_catalog_synced", input.ShopID, "info", map[string]any{
		"run_id": result.ID, "product_count": result.ProductCount, "sku_count": result.SKUCount,
		"warehouse_count": result.WarehouseCount, "page_count": result.PageCount,
	})
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *TikTokShopAPIHandler) ListProductCatalog(c *gin.Context) {
	if !h.productCatalogReady(c, false) {
		return
	}
	page, pageSize, err := parseTikTokCatalogPagination(c.Query("page"), c.Query("page_size"))
	status := tiktokshop.ProductStatus(strings.TrimSpace(c.Query("status")))
	filter := tiktokshop.TikTokCatalogListFilter{
		ShopID: strings.TrimSpace(c.Query("shop_id")), Query: strings.TrimSpace(c.Query("q")),
		Status: status, Page: page, PageSize: pageSize,
	}
	if err != nil || (filter.ShopID != "" && !tiktokshop.ValidTikTokShopID(filter.ShopID)) {
		h.error(c, http.StatusBadRequest, "invalid_request", "ตัวกรองสินค้า TikTok Shop ไม่ถูกต้อง")
		return
	}
	result, err := h.productReader.List(c.Request.Context(), filter)
	if err != nil {
		if errors.Is(err, tiktokshop.ErrProductCatalogInvalidInput) {
			h.error(c, http.StatusBadRequest, "invalid_request", "ตัวกรองสินค้า TikTok Shop ไม่ถูกต้อง")
			return
		}
		h.logger.Error("tiktok_shop_product_catalog_list_failed", zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		h.error(c, http.StatusInternalServerError, "catalog_list_failed", "โหลดรายการสินค้า TikTok Shop ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TikTokShopAPIHandler) productCatalogReady(c *gin.Context, write bool) bool {
	enabled, configured := h.readiness()
	if !enabled || h.config == nil || !h.config.TikTokShopProductCatalogEnabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด Product Catalog ของ TikTok Shop")
		return false
	}
	if !configured || h.productReader == nil || write && h.productCatalog == nil {
		h.error(c, http.StatusServiceUnavailable, "catalog_not_configured", "ระบบ Product Catalog ของ TikTok Shop ยังไม่พร้อม")
		return false
	}
	return true
}

func (h *TikTokShopAPIHandler) auditProductCatalog(c *gin.Context, action, shopID, level string, detail map[string]any) {
	if h == nil || h.audit == nil {
		return
	}
	var userID *string
	if value := strings.TrimSpace(c.GetString("user_id")); value != "" {
		userID = &value
	}
	target := strings.TrimSpace(shopID)
	_ = h.audit.Log(models.AuditEntry{
		Action: action, TargetID: &target, UserID: userID, Source: "tiktok_shop",
		Level: level, TraceID: c.GetString("trace_id"), Detail: detail,
	})
}

func productCatalogHandlerError(err error) (int, string, string) {
	var gatewayError *tiktokshop.GatewayError
	switch {
	case errors.Is(err, tiktokshop.ErrProductCatalogInvalidInput):
		return http.StatusBadRequest, "invalid_request", "ข้อมูลรีเฟรช Product Catalog ไม่ถูกต้อง"
	case errors.Is(err, tiktokshop.ErrProductCatalogSyncInProgress):
		return http.StatusConflict, "sync_in_progress", "ร้านนี้กำลังรีเฟรช Product Catalog อยู่"
	case errors.Is(err, tiktokshop.ErrProductCatalogSourceInvalid):
		return http.StatusBadGateway, "source_invalid", "TikTok Shop ส่งข้อมูลสินค้าหรือสต๊อกไม่ครบ จึงเก็บ snapshot เดิมไว้"
	case errors.Is(err, tiktokshop.ErrProductCatalogNotConfigured):
		return http.StatusServiceUnavailable, "catalog_not_configured", "ระบบ Product Catalog ของ TikTok Shop ยังไม่พร้อม"
	case errors.As(err, &gatewayError) && gatewayError.Code == "product_scope_required":
		return http.StatusForbidden, "product_scope_required", "แอปหรือร้านยังไม่ได้อนุญาตสิทธิ์ Product Basic"
	case errors.As(err, &gatewayError):
		return http.StatusBadGateway, "gateway_request_failed", "โหลด Product Catalog จาก TikTok Shop ไม่สำเร็จ"
	default:
		return http.StatusInternalServerError, "catalog_sync_failed", "รีเฟรช Product Catalog ของ TikTok Shop ไม่สำเร็จ"
	}
}

func parseTikTokCatalogPagination(rawPage, rawPageSize string) (int, int, error) {
	page, pageSize := 1, 50
	var err error
	if strings.TrimSpace(rawPage) != "" {
		page, err = strconv.Atoi(strings.TrimSpace(rawPage))
		if err != nil || page < 1 || page > 1_000_000 {
			return 0, 0, tiktokshop.ErrProductCatalogInvalidInput
		}
	}
	if strings.TrimSpace(rawPageSize) != "" {
		pageSize, err = strconv.Atoi(strings.TrimSpace(rawPageSize))
		if err != nil || pageSize < 1 || pageSize > 100 {
			return 0, 0, tiktokshop.ErrProductCatalogInvalidInput
		}
	}
	return page, pageSize, nil
}
