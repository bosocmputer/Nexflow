package handlers

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/tiktokshop"
)

type TikTokWebhookIngester interface {
	Ingest(context.Context, tiktokshop.GatewayWebhookDelivery) (bool, error)
}

const tiktokGatewayInternalBodyLimit = 2 << 20

type TikTokGatewayInternalHandler struct {
	database *sql.DB
	config   *config.Config
	ingester TikTokWebhookIngester
	verify   gatewayauth.Verifier
	logger   *zap.Logger
}

type tikTokGatewayNonceStore struct{ database *sql.DB }

func (s tikTokGatewayNonceStore) Consume(ctx context.Context, tenant, nonce string, expiresAt time.Time) error {
	result, err := s.database.ExecContext(ctx,
		`INSERT INTO tiktok_gateway_request_nonces (tenant_slug, nonce, expires_at)
		 VALUES ($1, $2, $3) ON CONFLICT (tenant_slug, nonce) DO NOTHING`, tenant, nonce, expiresAt)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return gatewayauth.ErrReplay
	}
	_, _ = s.database.ExecContext(ctx, `DELETE FROM tiktok_gateway_request_nonces WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return nil
}

func NewTikTokGatewayInternalHandler(database *sql.DB, cfg *config.Config, ingester TikTokWebhookIngester, logger *zap.Logger) *TikTokGatewayInternalHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	handler := &TikTokGatewayInternalHandler{database: database, config: cfg, ingester: ingester, logger: logger}
	handler.verify = gatewayauth.Verifier{
		ResolveSecret: func(_ context.Context, tenant string) (string, error) {
			if cfg == nil || !strings.EqualFold(strings.TrimSpace(tenant), strings.TrimSpace(cfg.TikTokShopGatewayTenant)) || strings.TrimSpace(cfg.TikTokShopGatewayInternalSecret) == "" {
				return "", gatewayauth.ErrInvalidSignature
			}
			return strings.TrimSpace(cfg.TikTokShopGatewayInternalSecret), nil
		},
		Nonces: tikTokGatewayNonceStore{database: database}, MaxSkew: 5 * time.Minute,
	}
	return handler
}

func (h *TikTokGatewayInternalHandler) Register(router *gin.Engine) {
	router.POST(tiktokshop.GatewayWebhookDeliveryPath, h.OrderStatusWebhook)
}

func (h *TikTokGatewayInternalHandler) OrderStatusWebhook(c *gin.Context) {
	body, ok := h.authenticate(c)
	if !ok {
		return
	}
	if h.ingester == nil {
		h.respondError(c, http.StatusServiceUnavailable, "webhook_ingest_unavailable", "ระบบรับ TikTok Shop webhook ยังไม่พร้อม")
		return
	}
	var delivery tiktokshop.GatewayWebhookDelivery
	if err := strictDecode(body, &delivery); err != nil {
		h.respondError(c, http.StatusBadRequest, "invalid_webhook_delivery", "ข้อมูล TikTok Shop webhook ไม่ถูกต้อง")
		return
	}
	inserted, err := h.ingester.Ingest(c.Request.Context(), delivery)
	if err != nil {
		status := http.StatusServiceUnavailable
		code := "webhook_ingest_failed"
		if errors.Is(err, tiktokshop.ErrInvalidWebhookDelivery) {
			status = http.StatusConflict
			code = "webhook_shop_mismatch"
		}
		h.logger.Warn("tiktok_webhook_ingest_failed",
			zap.String("gateway_event_id", strings.TrimSpace(delivery.GatewayEventID)),
			zap.String("notification_id", strings.TrimSpace(delivery.NotificationID)),
			zap.String("shop_id", strings.TrimSpace(delivery.ShopID)), zap.String("order_id", strings.TrimSpace(delivery.OrderID)),
			zap.String("error_code", code))
		h.respondError(c, status, code, "บันทึก TikTok Shop webhook ไม่สำเร็จ")
		return
	}
	h.logger.Info("tiktok_webhook_ingested",
		zap.String("gateway_event_id", strings.TrimSpace(delivery.GatewayEventID)),
		zap.String("notification_id", strings.TrimSpace(delivery.NotificationID)),
		zap.String("shop_id", strings.TrimSpace(delivery.ShopID)), zap.String("order_id", strings.TrimSpace(delivery.OrderID)),
		zap.Bool("inserted", inserted))
	c.JSON(http.StatusOK, gin.H{"success": true, "queued": inserted})
}

func (h *TikTokGatewayInternalHandler) authenticate(c *gin.Context) ([]byte, bool) {
	if h == nil || h.database == nil || h.config == nil || !h.config.TikTokShopWebhookEnabled || !h.config.TikTokShopOpenAPIEnabled ||
		strings.TrimSpace(h.config.TikTokShopGatewayTenant) == "" || strings.TrimSpace(h.config.TikTokShopGatewayInternalSecret) == "" {
		h.respondError(c, http.StatusNotFound, "tiktok_webhook_disabled", "tenant นี้ยังไม่ได้เปิด TikTok Shop webhook")
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, tiktokGatewayInternalBodyLimit+1))
	if err != nil || len(body) > tiktokGatewayInternalBodyLimit {
		h.respondError(c, http.StatusRequestEntityTooLarge, "request_too_large", "request มีขนาดใหญ่เกินกำหนด")
		return nil, false
	}
	if _, err := h.verify.Verify(c.Request.Context(), c.Request, body); err != nil {
		status := http.StatusUnauthorized
		code := "invalid_internal_auth"
		if errors.Is(err, gatewayauth.ErrReplay) {
			status = http.StatusConflict
			code = "replayed_request"
		}
		h.respondError(c, status, code, "ยืนยัน TikTok Shop gateway ไม่สำเร็จ")
		return nil, false
	}
	return body, true
}

func (h *TikTokGatewayInternalHandler) respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
