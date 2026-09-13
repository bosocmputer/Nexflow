package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/tiktokshop"
)

type TikTokShopGateway interface {
	Configured() bool
	CreateAuthURL(context.Context, tiktokshop.GatewayAuthURLRequest) (*tiktokshop.GatewayAuthURLResponse, error)
	ListConnections(context.Context) ([]tiktokshop.GatewayConnection, error)
	SearchOrders(context.Context, tiktokshop.GatewayOrderSearchRequest) (*tiktokshop.GatewayOrderSearchResponse, error)
	GetOrderDetails(context.Context, tiktokshop.GatewayOrderDetailsRequest) (*tiktokshop.GatewayOrderDetailsResponse, error)
	GetPriceDetail(context.Context, tiktokshop.GatewayOrderPriceDetailRequest) (*tiktokshop.GatewayOrderPriceDetailResponse, error)
}

type TikTokShopConnectionSyncer interface {
	Sync(context.Context, []tiktokshop.GatewayConnection) error
}

type TikTokShopOrderSnapshotter interface {
	Sync(context.Context, tiktokshop.TikTokOrderSnapshotRequest) (*tiktokshop.TikTokOrderSnapshotResult, error)
}

type TikTokShopOrderReconciler interface {
	Reconcile(context.Context, tiktokshop.TikTokOrderReconcileRequest) (*tiktokshop.TikTokOrderReconcileResult, error)
}

type TikTokShopOrderSyncSettings interface {
	ListSettings(context.Context) ([]tiktokshop.TikTokOrderSyncSetting, error)
	UpdateSetting(context.Context, string, tiktokshop.TikTokOrderSyncSettingUpdate) (*tiktokshop.TikTokOrderSyncSetting, error)
}

type TikTokShopOrderReader interface {
	List(context.Context, tiktokshop.TikTokOrderSnapshotListFilter) (*tiktokshop.TikTokOrderSnapshotListResult, error)
}

type TikTokShopBillShadowPreviewer interface {
	Preview(context.Context, string, string) (*tiktokshop.TikTokBillShadowPreview, error)
}

type TikTokShopBillShadowMapper interface {
	Preview(context.Context, tiktokshop.TikTokBillShadowMappingSelection) (models.MarketplaceAliasImpact, error)
	Confirm(context.Context, tiktokshop.TikTokBillShadowMappingConfirmation, string) (*repository.MarketplaceAliasCommitResult, error)
}

type tikTokBillShadowMappingRequest struct {
	ProductID               string `json:"product_id"`
	SKUID                   string `json:"sku_id"`
	ItemCode                string `json:"item_code"`
	UnitCode                string `json:"unit_code"`
	QuantityMultiplier      int64  `json:"quantity_multiplier"`
	ExpectedMappingRevision int64  `json:"expected_mapping_revision"`
	ImpactDigest            string `json:"impact_digest"`
}

type TikTokShopAPIHandler struct {
	config       *config.Config
	gateway      TikTokShopGateway
	store        TikTokShopConnectionSyncer
	snapshots    TikTokShopOrderSnapshotter
	reconciler   TikTokShopOrderReconciler
	syncSettings TikTokShopOrderSyncSettings
	orderReader  TikTokShopOrderReader
	billShadow   TikTokShopBillShadowPreviewer
	billMapper   TikTokShopBillShadowMapper
	logger       *zap.Logger
}

func (h *TikTokShopAPIHandler) WithOrderReader(reader TikTokShopOrderReader) *TikTokShopAPIHandler {
	if h != nil {
		h.orderReader = reader
	}
	return h
}

func (h *TikTokShopAPIHandler) WithBillShadowPreviewer(previewer TikTokShopBillShadowPreviewer) *TikTokShopAPIHandler {
	if h != nil {
		h.billShadow = previewer
	}
	return h
}

func (h *TikTokShopAPIHandler) WithBillShadowMapper(mapper TikTokShopBillShadowMapper) *TikTokShopAPIHandler {
	if h != nil {
		h.billMapper = mapper
	}
	return h
}

func NewTikTokShopAPIHandler(config *config.Config, gateway TikTokShopGateway, store TikTokShopConnectionSyncer, snapshots TikTokShopOrderSnapshotter, logger *zap.Logger) *TikTokShopAPIHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokShopAPIHandler{config: config, gateway: gateway, store: store, snapshots: snapshots, logger: logger}
}

func (h *TikTokShopAPIHandler) WithOrderReconciler(reconciler TikTokShopOrderReconciler) *TikTokShopAPIHandler {
	if h != nil {
		h.reconciler = reconciler
	}
	return h
}

func (h *TikTokShopAPIHandler) WithOrderSyncSettings(settings TikTokShopOrderSyncSettings) *TikTokShopAPIHandler {
	if h != nil {
		h.syncSettings = settings
	}
	return h
}

func (h *TikTokShopAPIHandler) Status(c *gin.Context) {
	enabled, configured := h.readiness()
	redirectURL := ""
	if h != nil && h.config != nil && strings.TrimSpace(h.config.TikTokShopGatewayPublicURL) != "" {
		redirectURL = strings.TrimRight(strings.TrimSpace(h.config.TikTokShopGatewayPublicURL), "/") + "/api/tiktok-shop/callback"
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled, "configured": configured,
		"mode": "gateway", "redirect_url": redirectURL,
	})
}

func (h *TikTokShopAPIHandler) CreateAuthURL(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured {
		h.error(c, http.StatusServiceUnavailable, "gateway_not_configured", "TikTok Shop Gateway ของ tenant ยังไม่พร้อม")
		return
	}
	userID := strings.TrimSpace(c.GetString("user_id"))
	if userID == "" {
		h.error(c, http.StatusUnauthorized, "session_expired", "กรุณาเข้าสู่ระบบใหม่")
		return
	}
	returnURL, err := tenantTikTokReturnURL(h.config.PublicBaseURL)
	if err != nil {
		h.error(c, http.StatusServiceUnavailable, "public_url_invalid", "PUBLIC_BASE_URL ของ tenant ยังไม่พร้อม")
		return
	}
	result, err := h.gateway.CreateAuthURL(c.Request.Context(), tiktokshop.GatewayAuthURLRequest{UserID: userID, ReturnURL: returnURL})
	if err != nil {
		h.logger.Warn("tiktok_shop_auth_url_failed", zap.Error(err))
		h.error(c, http.StatusBadGateway, "gateway_request_failed", "สร้างลิงก์เชื่อมต่อ TikTok Shop ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{"auth_url": result.AuthURL, "redirect_url": result.RedirectURL, "expires_at": result.ExpiresAt})
}

func (h *TikTokShopAPIHandler) ListConnections(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured || h.store == nil {
		h.error(c, http.StatusServiceUnavailable, "gateway_not_configured", "TikTok Shop Gateway ของ tenant ยังไม่พร้อม")
		return
	}
	connections, err := h.gateway.ListConnections(c.Request.Context())
	if err != nil {
		h.logger.Warn("tiktok_shop_connections_failed", zap.Error(err))
		h.error(c, http.StatusBadGateway, "gateway_request_failed", "โหลดร้าน TikTok Shop จาก Gateway ไม่สำเร็จ")
		return
	}
	if err := h.store.Sync(c.Request.Context(), connections); err != nil {
		h.logger.Warn("tiktok_shop_connections_sync_failed", zap.Error(err))
		h.error(c, http.StatusInternalServerError, "connection_sync_failed", "บันทึกข้อมูลร้าน TikTok Shop ใน tenant ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": connections})
}

func (h *TikTokShopAPIHandler) SearchOrders(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured {
		h.error(c, http.StatusServiceUnavailable, "gateway_not_configured", "TikTok Shop Gateway ของ tenant ยังไม่พร้อม")
		return
	}
	var input tiktokshop.GatewayOrderSearchRequest
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.ShopID) == "" || input.Search.Validate() != nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลค้นหาออเดอร์ TikTok Shop ไม่ถูกต้อง")
		return
	}
	result, err := h.gateway.SearchOrders(c.Request.Context(), input)
	if err != nil {
		h.logger.Warn("tiktok_shop_order_search_failed", zap.Error(err))
		h.error(c, http.StatusBadGateway, "gateway_request_failed", "โหลดรายการออเดอร์ TikTok Shop ไม่สำเร็จ")
		return
	}
	if result == nil {
		h.error(c, http.StatusBadGateway, "gateway_response_invalid", "Gateway ส่งข้อมูลออเดอร์ไม่สมบูรณ์")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *TikTokShopAPIHandler) GetOrderDetails(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured {
		h.error(c, http.StatusServiceUnavailable, "gateway_not_configured", "TikTok Shop Gateway ของ tenant ยังไม่พร้อม")
		return
	}
	var input tiktokshop.GatewayOrderDetailsRequest
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.ShopID) == "" || !validTikTokOrderIDs(input.OrderIDs) {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลออเดอร์ TikTok Shop ไม่ถูกต้อง")
		return
	}
	result, err := h.gateway.GetOrderDetails(c.Request.Context(), input)
	if err != nil {
		h.logger.Warn("tiktok_shop_order_detail_failed", zap.Error(err))
		h.error(c, http.StatusBadGateway, "gateway_request_failed", "โหลดรายละเอียดออเดอร์ TikTok Shop ไม่สำเร็จ")
		return
	}
	if result == nil {
		h.error(c, http.StatusBadGateway, "gateway_response_invalid", "Gateway ส่งรายละเอียดออเดอร์ไม่สมบูรณ์")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *TikTokShopAPIHandler) GetOrderPriceDetail(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured {
		h.error(c, http.StatusServiceUnavailable, "gateway_not_configured", "TikTok Shop Gateway ของ tenant ยังไม่พร้อม")
		return
	}
	var input tiktokshop.GatewayOrderPriceDetailRequest
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.ShopID) == "" || strings.TrimSpace(input.OrderID) == "" || strings.ContainsAny(input.OrderID, "/?#") {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลราคาของออเดอร์ TikTok Shop ไม่ถูกต้อง")
		return
	}
	result, err := h.gateway.GetPriceDetail(c.Request.Context(), input)
	if err != nil {
		h.logger.Warn("tiktok_shop_order_price_detail_failed", zap.Error(err))
		h.error(c, http.StatusBadGateway, "gateway_request_failed", "โหลดรายละเอียดราคาออเดอร์ TikTok Shop ไม่สำเร็จ")
		return
	}
	if result == nil || result.PriceDetail == nil {
		h.error(c, http.StatusBadGateway, "gateway_response_invalid", "Gateway ส่งรายละเอียดราคาออเดอร์ไม่สมบูรณ์")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *TikTokShopAPIHandler) SnapshotOrders(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured || h.snapshots == nil {
		h.error(c, http.StatusServiceUnavailable, "snapshot_not_configured", "ระบบบันทึกออเดอร์ TikTok Shop ยังไม่พร้อม")
		return
	}
	var input tiktokshop.TikTokOrderSnapshotRequest
	if err := c.ShouldBindJSON(&input); err != nil || !validTikTokSnapshotInput(input) {
		h.error(c, http.StatusBadRequest, "invalid_request", "ระบุร้านและ Order ID ของ TikTok Shop จำนวน 1–20 รายการ")
		return
	}
	result, err := h.snapshots.Sync(c.Request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, tiktokshop.ErrInvalidSnapshotInput):
			h.error(c, http.StatusBadRequest, "invalid_snapshot", "ข้อมูลออเดอร์ TikTok Shop ไม่สมบูรณ์")
		case errors.Is(err, tiktokshop.ErrSnapshotAmountMismatch):
			h.error(c, http.StatusConflict, "amount_mismatch", "ยอด Order Detail และ Price Detail ไม่ตรงกัน จึงยังไม่บันทึก")
		case errors.Is(err, tiktokshop.ErrSnapshotPersistenceFailed):
			h.logger.Error("tiktok_shop_order_snapshot_persist_failed",
				zap.String("shop_id", strings.TrimSpace(input.ShopID)), zap.Int("order_count", len(input.OrderIDs)),
				zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
			h.error(c, http.StatusInternalServerError, "snapshot_persist_failed", "บันทึกออเดอร์ TikTok Shop ไม่สำเร็จ")
		default:
			h.logger.Warn("tiktok_shop_order_snapshot_source_failed",
				zap.String("shop_id", strings.TrimSpace(input.ShopID)), zap.Int("order_count", len(input.OrderIDs)),
				zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
			h.error(c, http.StatusBadGateway, "gateway_request_failed", "โหลดข้อมูลออเดอร์ TikTok Shop ไม่สำเร็จ")
		}
		return
	}
	h.logger.Info("tiktok_shop_order_snapshot_synced",
		zap.String("shop_id", result.ShopID), zap.Int("order_count", result.SyncedCount),
		zap.String("trace_id", c.GetString("trace_id")), zap.String("entry_point", "manual_api"))
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *TikTokShopAPIHandler) ReconcileOrders(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured || h.reconciler == nil {
		h.error(c, http.StatusServiceUnavailable, "reconciliation_not_configured", "ระบบ reconciliation ออเดอร์ TikTok Shop ยังไม่พร้อม")
		return
	}
	var input tiktokshop.TikTokOrderReconcileRequest
	if err := c.ShouldBindJSON(&input); err != nil || input.Validate() != nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "ระบุร้านและช่วง update time ไม่เกิน 24 ชั่วโมง")
		return
	}
	result, err := h.reconciler.Reconcile(c.Request.Context(), input)
	if err != nil {
		if errors.Is(err, tiktokshop.ErrInvalidOrderReconcileInput) {
			h.error(c, http.StatusBadRequest, "invalid_reconciliation", "ข้อมูล reconciliation ออเดอร์ TikTok Shop ไม่ถูกต้อง")
			return
		}
		h.logger.Warn("tiktok_shop_order_reconciliation_failed",
			zap.String("shop_id", strings.TrimSpace(input.ShopID)),
			zap.Int64("update_time_ge", input.UpdateTimeGE), zap.Int64("update_time_lt", input.UpdateTimeLT),
			zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		h.error(c, http.StatusBadGateway, "reconciliation_failed", "Reconcile ออเดอร์ TikTok Shop ไม่สำเร็จ")
		return
	}
	h.logger.Info("tiktok_shop_order_reconciliation_succeeded",
		zap.String("shop_id", result.ShopID), zap.String("run_id", result.RunID),
		zap.Int("page_count", result.PageCount), zap.Int("order_count", result.SnapshottedCount),
		zap.String("trace_id", c.GetString("trace_id")), zap.String("entry_point", "manual_api"))
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *TikTokShopAPIHandler) ListOrderSyncSettings(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured || h.syncSettings == nil {
		h.error(c, http.StatusServiceUnavailable, "order_sync_not_configured", "ระบบตั้งค่า sync ออเดอร์ TikTok Shop ยังไม่พร้อม")
		return
	}
	settings, err := h.syncSettings.ListSettings(c.Request.Context())
	if err != nil {
		h.logger.Error("tiktok_shop_order_sync_settings_list_failed", zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		h.error(c, http.StatusInternalServerError, "order_sync_settings_failed", "โหลดการตั้งค่า sync ออเดอร์ TikTok Shop ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{"worker_enabled": h.config.TikTokShopOrderSyncEnabled, "data": settings})
}

func (h *TikTokShopAPIHandler) ListOrders(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured || h.orderReader == nil {
		h.error(c, http.StatusServiceUnavailable, "order_list_not_configured", "ระบบรายการออเดอร์ TikTok Shop ยังไม่พร้อม")
		return
	}
	page, pageSize, err := parseTikTokOrderListPagination(c.Query("page"), c.Query("page_size"))
	filter := tiktokshop.TikTokOrderSnapshotListFilter{
		ShopID: strings.TrimSpace(c.Query("shop_id")), Status: tiktokshop.OrderStatus(strings.TrimSpace(c.Query("status"))),
		StatusGroup:   tiktokshop.TikTokOrderStatusGroup(strings.TrimSpace(c.Query("status_group"))),
		OrderIDPrefix: strings.TrimSpace(c.Query("order_id")), Page: page, PageSize: pageSize,
	}
	if err != nil || (filter.ShopID != "" && !tiktokshop.ValidTikTokShopID(filter.ShopID)) ||
		(filter.OrderIDPrefix != "" && !tiktokshop.ValidTikTokShopID(filter.OrderIDPrefix)) ||
		(filter.Status != "" && !validTikTokListStatus(filter.Status)) || !tiktokshop.ValidTikTokOrderStatusGroup(filter.StatusGroup) {
		h.error(c, http.StatusBadRequest, "invalid_request", "ตัวกรองรายการออเดอร์ TikTok Shop ไม่ถูกต้อง")
		return
	}
	result, err := h.orderReader.List(c.Request.Context(), filter)
	if err != nil {
		if errors.Is(err, tiktokshop.ErrInvalidSnapshotListFilter) {
			h.error(c, http.StatusBadRequest, "invalid_request", "ตัวกรองรายการออเดอร์ TikTok Shop ไม่ถูกต้อง")
			return
		}
		h.logger.Error("tiktok_shop_order_list_failed", zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		h.error(c, http.StatusInternalServerError, "order_list_failed", "โหลดรายการออเดอร์ TikTok Shop ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TikTokShopAPIHandler) GetBillShadowPreview(c *gin.Context) {
	enabled, _ := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if h.billShadow == nil {
		h.error(c, http.StatusServiceUnavailable, "bill_shadow_not_configured", "ระบบตรวจตัวอย่าง Bill TikTok Shop ยังไม่พร้อม")
		return
	}
	shopID := strings.TrimSpace(c.Param("shop_id"))
	orderID := strings.TrimSpace(c.Param("order_id"))
	if !tiktokshop.ValidTikTokShopID(shopID) || !tiktokshop.ValidTikTokShopID(orderID) {
		h.error(c, http.StatusBadRequest, "invalid_request", "Shop ID หรือ Order ID ของ TikTok Shop ไม่ถูกต้อง")
		return
	}
	preview, err := h.billShadow.Preview(c.Request.Context(), shopID, orderID)
	if err != nil {
		switch {
		case errors.Is(err, tiktokshop.ErrTikTokBillShadowInvalidInput):
			h.error(c, http.StatusBadRequest, "invalid_request", "Shop ID หรือ Order ID ของ TikTok Shop ไม่ถูกต้อง")
		case errors.Is(err, tiktokshop.ErrTikTokBillShadowNotFound):
			h.error(c, http.StatusNotFound, "snapshot_not_found", "ไม่พบ Snapshot ของออเดอร์ TikTok Shop นี้")
		default:
			h.logger.Error("tiktok_shop_bill_shadow_preview_failed",
				zap.String("shop_id", shopID), zap.String("order_id", orderID),
				zap.String("trace_id", c.GetString("trace_id")), zap.String("entry_point", "manual_api"), zap.Error(err))
			h.error(c, http.StatusInternalServerError, "bill_shadow_failed", "ตรวจตัวอย่าง Bill TikTok Shop ไม่สำเร็จ")
		}
		return
	}
	readyMappings := 0
	for _, item := range preview.Items {
		if item.Mapping.Status == tiktokshop.TikTokBillShadowMappingReady {
			readyMappings++
		}
	}
	event := "tiktok_shop_bill_shadow_preview_blocked"
	if preview.ReadyForReviewedBill {
		event = "tiktok_shop_bill_shadow_preview_succeeded"
	}
	h.logger.Info(event,
		zap.String("shop_id", shopID), zap.String("order_id", orderID),
		zap.Int("mapping_ready_count", readyMappings), zap.Int("item_count", len(preview.Items)),
		zap.Int("blocker_count", len(preview.Blockers)), zap.String("trace_id", c.GetString("trace_id")),
		zap.String("entry_point", "manual_api"))
	c.JSON(http.StatusOK, preview)
}

func (h *TikTokShopAPIHandler) PreviewBillShadowMapping(c *gin.Context) {
	shopID, orderID, selection, _, ok := h.bindBillShadowMappingSelection(c)
	if !ok {
		return
	}
	impact, err := h.billMapper.Preview(c.Request.Context(), selection)
	if err != nil {
		h.writeBillShadowMappingError(c, err, shopID, orderID, selection.ProductID, selection.SKUID, "preview")
		return
	}
	h.logger.Info("tiktok_shop_bill_shadow_mapping_previewed",
		zap.String("shop_id", shopID), zap.String("order_id", orderID),
		zap.String("product_id", selection.ProductID), zap.String("sku_id", selection.SKUID),
		zap.String("item_code", selection.ItemCode), zap.Int64("mapping_revision", impact.CurrentMappingRevision),
		zap.String("actor_id", c.GetString("user_id")), zap.String("trace_id", c.GetString("trace_id")))
	c.JSON(http.StatusOK, impact)
}

func (h *TikTokShopAPIHandler) ConfirmBillShadowMapping(c *gin.Context) {
	shopID, orderID, selection, reviewed, ok := h.bindBillShadowMappingSelection(c)
	if !ok {
		return
	}
	result, err := h.billMapper.Confirm(c.Request.Context(), tiktokshop.TikTokBillShadowMappingConfirmation{
		Selection: selection, ExpectedMappingRevision: reviewed.ExpectedMappingRevision, ImpactDigest: reviewed.ImpactDigest,
	}, c.GetString("user_id"))
	if err != nil {
		h.writeBillShadowMappingError(c, err, shopID, orderID, selection.ProductID, selection.SKUID, "confirm")
		return
	}
	h.logger.Info("tiktok_shop_bill_shadow_mapping_confirmed",
		zap.String("shop_id", shopID), zap.String("order_id", orderID),
		zap.String("product_id", selection.ProductID), zap.String("sku_id", selection.SKUID),
		zap.String("item_code", selection.ItemCode), zap.String("alias_id", result.Job.AliasID),
		zap.Int64("target_revision", result.Job.TargetRevision), zap.String("actor_id", c.GetString("user_id")),
		zap.String("trace_id", c.GetString("trace_id")))
	c.JSON(http.StatusAccepted, result)
}

func (h *TikTokShopAPIHandler) bindBillShadowMappingSelection(c *gin.Context) (string, string, tiktokshop.TikTokBillShadowMappingSelection, tikTokBillShadowMappingRequest, bool) {
	shopID := strings.TrimSpace(c.Param("shop_id"))
	orderID := strings.TrimSpace(c.Param("order_id"))
	if h == nil || h.config == nil || !h.config.TikTokShopOpenAPIEnabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return shopID, orderID, tiktokshop.TikTokBillShadowMappingSelection{}, tikTokBillShadowMappingRequest{}, false
	}
	if h.billMapper == nil {
		h.error(c, http.StatusServiceUnavailable, "bill_shadow_mapping_not_configured", "ระบบจับคู่ Product Master ของ TikTok Shop ยังไม่พร้อม")
		return shopID, orderID, tiktokshop.TikTokBillShadowMappingSelection{}, tikTokBillShadowMappingRequest{}, false
	}
	var input tikTokBillShadowMappingRequest
	if !tiktokshop.ValidTikTokShopID(shopID) || !tiktokshop.ValidTikTokShopID(orderID) || c.ShouldBindJSON(&input) != nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลสินค้าและหน่วย SML ไม่ถูกต้อง")
		return shopID, orderID, tiktokshop.TikTokBillShadowMappingSelection{}, tikTokBillShadowMappingRequest{}, false
	}
	selection := tiktokshop.TikTokBillShadowMappingSelection{
		ShopID: shopID, OrderID: orderID, ProductID: input.ProductID, SKUID: input.SKUID,
		ItemCode: input.ItemCode, UnitCode: input.UnitCode, QuantityMultiplier: input.QuantityMultiplier,
	}
	return shopID, orderID, selection, input, true
}

func (h *TikTokShopAPIHandler) writeBillShadowMappingError(c *gin.Context, err error, shopID, orderID, productID, skuID, operation string) {
	switch {
	case errors.Is(err, tiktokshop.ErrTikTokBillShadowMappingInvalidInput):
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลสินค้าและหน่วย SML ไม่ถูกต้อง")
	case errors.Is(err, tiktokshop.ErrTikTokBillShadowNotFound), errors.Is(err, tiktokshop.ErrTikTokBillShadowMappingItemNotFound):
		h.error(c, http.StatusNotFound, "snapshot_item_not_found", "ไม่พบสินค้านี้ใน Snapshot ล่าสุด กรุณารีเฟรชออเดอร์แล้วลองใหม่")
	case errors.Is(err, repository.ErrMarketplaceAliasConflict), errors.Is(err, repository.ErrMarketplaceImpactChanged):
		h.error(c, http.StatusConflict, "mapping_changed", "Product Master ถูกแก้ไขแล้ว กรุณาตรวจผลกระทบใหม่ก่อนยืนยัน")
	case errors.Is(err, repository.ErrMarketplaceUnitNotReady), errors.Is(err, sql.ErrNoRows):
		h.error(c, http.StatusUnprocessableEntity, "catalog_not_ready", "สินค้า/หน่วย SML ไม่อยู่ใน Catalog รุ่นที่ใช้งาน กรุณาอัปเดต Catalog แล้วเลือกใหม่")
	default:
		h.logger.Error("tiktok_shop_bill_shadow_mapping_failed",
			zap.String("operation", operation), zap.String("shop_id", shopID), zap.String("order_id", orderID),
			zap.String("product_id", productID), zap.String("sku_id", skuID),
			zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		h.error(c, http.StatusInternalServerError, "mapping_failed", "บันทึกการจับคู่ Product Master ไม่สำเร็จ และยังไม่มีข้อมูลถูกเปลี่ยน")
	}
}

func (h *TikTokShopAPIHandler) UpdateOrderSyncSetting(c *gin.Context) {
	enabled, configured := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !configured || h.syncSettings == nil {
		h.error(c, http.StatusServiceUnavailable, "order_sync_not_configured", "ระบบตั้งค่า sync ออเดอร์ TikTok Shop ยังไม่พร้อม")
		return
	}
	shopID := strings.TrimSpace(c.Param("shop_id"))
	var input tiktokshop.TikTokOrderSyncSettingUpdate
	if !tiktokshop.ValidTikTokShopID(shopID) || c.ShouldBindJSON(&input) != nil || input.Validate() != nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "การตั้งค่า sync ออเดอร์ TikTok Shop ไม่ถูกต้อง")
		return
	}
	if input.Enabled && !h.config.TikTokShopOrderSyncEnabled {
		h.error(c, http.StatusConflict, "worker_disabled", "Server ยังไม่ได้เปิด TikTok Shop order sync worker")
		return
	}
	setting, err := h.syncSettings.UpdateSetting(c.Request.Context(), shopID, input)
	if err != nil {
		switch {
		case errors.Is(err, tiktokshop.ErrInvalidOrderReconcileInput):
			h.error(c, http.StatusBadRequest, "invalid_setting", "ไม่พบร้าน TikTok Shop ที่เปิดใช้งาน หรือค่าที่ระบุไม่ถูกต้อง")
		case errors.Is(err, tiktokshop.ErrOrderSyncVersionConflict):
			h.error(c, http.StatusConflict, "version_conflict", "การตั้งค่าถูกแก้ไขแล้ว กรุณาโหลดใหม่")
		default:
			h.logger.Error("tiktok_shop_order_sync_setting_update_failed", zap.String("shop_id", shopID), zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
			h.error(c, http.StatusInternalServerError, "order_sync_setting_failed", "บันทึกการตั้งค่า sync ออเดอร์ TikTok Shop ไม่สำเร็จ")
		}
		return
	}
	h.logger.Info("tiktok_shop_order_sync_setting_updated",
		zap.String("shop_id", setting.ShopID), zap.Bool("enabled", setting.Enabled),
		zap.Int("interval_seconds", setting.IntervalSeconds), zap.Int("overlap_seconds", setting.OverlapSeconds),
		zap.Int64("config_version", setting.ConfigVersion), zap.String("actor_id", c.GetString("user_id")),
		zap.String("trace_id", c.GetString("trace_id")), zap.String("entry_point", "settings_api"))
	c.JSON(http.StatusOK, gin.H{"data": setting})
}

func (h *TikTokShopAPIHandler) readiness() (enabled, configured bool) {
	if h == nil || h.config == nil {
		return false, false
	}
	return h.config.TikTokShopOpenAPIEnabled, h.gateway != nil && h.gateway.Configured()
}

func (h *TikTokShopAPIHandler) error(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func tenantTikTokReturnURL(publicBaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", tiktokshop.ErrInvalidGatewayInput
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/settings/tiktok-shop"
	query := parsed.Query()
	query.Set("connected", "1")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func validTikTokOrderIDs(orderIDs []string) bool {
	if len(orderIDs) == 0 || len(orderIDs) > 50 {
		return false
	}
	seen := make(map[string]struct{}, len(orderIDs))
	for i := range orderIDs {
		orderIDs[i] = strings.TrimSpace(orderIDs[i])
		if orderIDs[i] == "" {
			return false
		}
		if _, exists := seen[orderIDs[i]]; exists {
			return false
		}
		seen[orderIDs[i]] = struct{}{}
	}
	return true
}

func validTikTokSnapshotInput(input tiktokshop.TikTokOrderSnapshotRequest) bool {
	if strings.TrimSpace(input.ShopID) == "" || len(input.OrderIDs) == 0 || len(input.OrderIDs) > 20 {
		return false
	}
	seen := make(map[string]struct{}, len(input.OrderIDs))
	for _, orderID := range input.OrderIDs {
		orderID = strings.TrimSpace(orderID)
		if orderID == "" {
			return false
		}
		if _, duplicate := seen[orderID]; duplicate {
			return false
		}
		seen[orderID] = struct{}{}
	}
	return true
}

func parseTikTokOrderListPagination(rawPage, rawPageSize string) (int, int, error) {
	page, pageSize := 1, 20
	var err error
	if strings.TrimSpace(rawPage) != "" {
		page, err = strconv.Atoi(strings.TrimSpace(rawPage))
		if err != nil || page < 1 || page > 1000000 {
			return 0, 0, tiktokshop.ErrInvalidSnapshotListFilter
		}
	}
	if strings.TrimSpace(rawPageSize) != "" {
		pageSize, err = strconv.Atoi(strings.TrimSpace(rawPageSize))
		if err != nil || pageSize < 1 || pageSize > 50 {
			return 0, 0, tiktokshop.ErrInvalidSnapshotListFilter
		}
	}
	return page, pageSize, nil
}

func validTikTokListStatus(status tiktokshop.OrderStatus) bool {
	switch status {
	case tiktokshop.OrderStatusUnpaid, tiktokshop.OrderStatusOnHold, tiktokshop.OrderStatusAwaitingShipment,
		tiktokshop.OrderStatusPartiallyShipping, tiktokshop.OrderStatusAwaitingCollection,
		tiktokshop.OrderStatusInTransit, tiktokshop.OrderStatusDelivered, tiktokshop.OrderStatusCompleted,
		tiktokshop.OrderStatusCancelled:
		return true
	default:
		return false
	}
}
