package tiktokgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/services/tiktokshop"
)

const maxWebhookBodySize = 1 << 20

var webhookNumericIDPattern = regexp.MustCompile(`^[0-9]{1,32}$`)

var (
	ErrInvalidWebhookEvent          = errors.New("invalid TikTok Shop webhook event")
	ErrWebhookNotificationCollision = errors.New("TikTok Shop webhook notification ID collision")
)

type WebhookReceiver interface {
	AcceptWebhookEvent(context.Context, WebhookEventInput) (*WebhookEventResult, error)
}

type WebhookEventInput struct {
	NotificationID   string
	NotificationType int
	ShopID           string
	OrderID          string
	OrderStatus      string
	Timestamp        time.Time
	OrderUpdateAt    time.Time
	BodySHA256       string
}

type WebhookEventResult struct {
	EventID  string
	Inserted bool
	Tenant   *Tenant
}

type orderStatusWebhookPayload struct {
	Type           int    `json:"type"`
	NotificationID string `json:"tts_notification_id"`
	ShopID         string `json:"shop_id"`
	Timestamp      int64  `json:"timestamp"`
	Data           struct {
		OrderID       string `json:"order_id"`
		OrderStatus   string `json:"order_status"`
		OrderUpdateAt int64  `json:"update_time"`
	} `json:"data"`
}

func ParseOrderStatusWebhook(rawBody []byte) (WebhookEventInput, error) {
	if len(rawBody) == 0 || !json.Valid(rawBody) {
		return WebhookEventInput{}, ErrInvalidWebhookEvent
	}
	var payload orderStatusWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return WebhookEventInput{}, ErrInvalidWebhookEvent
	}
	payload.NotificationID = strings.TrimSpace(payload.NotificationID)
	payload.ShopID = strings.TrimSpace(payload.ShopID)
	payload.Data.OrderID = strings.TrimSpace(payload.Data.OrderID)
	payload.Data.OrderStatus = strings.ToUpper(strings.TrimSpace(payload.Data.OrderStatus))
	if payload.Type != 1 || !webhookNumericIDPattern.MatchString(payload.NotificationID) ||
		!webhookNumericIDPattern.MatchString(payload.ShopID) || !webhookNumericIDPattern.MatchString(payload.Data.OrderID) ||
		payload.Timestamp <= 0 || payload.Data.OrderUpdateAt <= 0 || payload.Data.OrderStatus == "" || len(payload.Data.OrderStatus) > 64 {
		return WebhookEventInput{}, ErrInvalidWebhookEvent
	}
	digest := sha256.Sum256(rawBody)
	return WebhookEventInput{
		NotificationID: payload.NotificationID, NotificationType: payload.Type,
		ShopID: payload.ShopID, OrderID: payload.Data.OrderID, OrderStatus: payload.Data.OrderStatus,
		Timestamp: time.Unix(payload.Timestamp, 0).UTC(), OrderUpdateAt: time.Unix(payload.Data.OrderUpdateAt, 0).UTC(),
		BodySHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func (h *Handler) ReceiveWebhook(c *gin.Context) {
	requestID := webhookRequestID(c)
	if h == nil || !h.config.WebhookEnabled || h.webhooks == nil {
		c.Status(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize+1))
	if err != nil || len(body) > maxWebhookBodySize {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	if err := tiktokshop.VerifyWebhookSignature(h.config.AppKey, h.config.AppSecret, c.GetHeader("Authorization"), body); err != nil {
		h.logger.Warn("tiktok_gateway_webhook_auth_failed", zap.String("request_id", requestID))
		c.Status(http.StatusUnauthorized)
		return
	}
	if len(body) == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	event, err := ParseOrderStatusWebhook(body)
	if err != nil {
		h.logger.Warn("tiktok_gateway_webhook_payload_rejected", zap.String("request_id", requestID))
		c.Status(http.StatusBadRequest)
		return
	}
	storeContext, cancel := context.WithTimeout(c.Request.Context(), 2500*time.Millisecond)
	defer cancel()
	result, err := h.webhooks.AcceptWebhookEvent(storeContext, event)
	if err != nil || result == nil {
		h.logger.Warn("tiktok_gateway_webhook_store_failed",
			zap.String("request_id", requestID), zap.String("notification_id", event.NotificationID),
			zap.String("shop_id", event.ShopID), zap.String("order_id", event.OrderID))
		c.Status(http.StatusServiceUnavailable)
		return
	}
	tenant := ""
	if result.Tenant != nil {
		tenant = result.Tenant.Slug
	}
	h.logger.Info("tiktok_gateway_webhook_accepted",
		zap.String("request_id", requestID), zap.String("notification_id", event.NotificationID),
		zap.String("shop_id", event.ShopID), zap.String("order_id", event.OrderID), zap.String("tenant", tenant),
		zap.Bool("inserted", result.Inserted), zap.Bool("queued", result.Inserted && result.Tenant != nil))
	c.Status(http.StatusOK)
}

func webhookRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if requestID == "" {
		requestID = newRequestID()
	}
	return requestID
}
