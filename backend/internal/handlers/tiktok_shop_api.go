package handlers

import (
	"context"
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
}

type TikTokShopConnectionSyncer interface {
	Sync(context.Context, []tiktokshop.GatewayConnection) error
}

type TikTokShopAPIHandler struct {
	config  *config.Config
	gateway TikTokShopGateway
	store   TikTokShopConnectionSyncer
	logger  *zap.Logger
}

func NewTikTokShopAPIHandler(config *config.Config, gateway TikTokShopGateway, store TikTokShopConnectionSyncer, logger *zap.Logger) *TikTokShopAPIHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokShopAPIHandler{config: config, gateway: gateway, store: store, logger: logger}
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
