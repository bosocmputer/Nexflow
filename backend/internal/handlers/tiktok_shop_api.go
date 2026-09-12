package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
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

type TikTokShopAPIHandler struct {
	config    *config.Config
	gateway   TikTokShopGateway
	store     TikTokShopConnectionSyncer
	snapshots TikTokShopOrderSnapshotter
	logger    *zap.Logger
}

func NewTikTokShopAPIHandler(config *config.Config, gateway TikTokShopGateway, store TikTokShopConnectionSyncer, snapshots TikTokShopOrderSnapshotter, logger *zap.Logger) *TikTokShopAPIHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokShopAPIHandler{config: config, gateway: gateway, store: store, snapshots: snapshots, logger: logger}
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
