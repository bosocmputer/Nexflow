package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
	"nexflow/internal/worker"
)

// LineHandler validates multi-OA webhooks and captures destination IDs for
// order-notification recipients. LINE chat and message extraction are disabled.
type LineHandler struct {
	registry             *lineservice.Registry
	lineNotificationRepo *repository.LineNotificationRepo
	pool                 *worker.Pool
	logger               *zap.Logger
}

func NewLineHandler(
	registry *lineservice.Registry,
	lineNotificationRepo *repository.LineNotificationRepo,
	pool *worker.Pool,
	logger *zap.Logger,
) *LineHandler {
	return &LineHandler{
		registry:             registry,
		lineNotificationRepo: lineNotificationRepo,
		pool:                 pool,
		logger:               logger,
	}
}

// ── Minimal webhook payload structs ──────────────────────────────────────────

type linePayload struct {
	Destination string      `json:"destination"`
	Events      []lineEvent `json:"events"`
}

type lineEvent struct {
	Type            string           `json:"type"`
	Timestamp       int64            `json:"timestamp"`
	ReplyToken      string           `json:"replyToken"`
	Source          lineSource       `json:"source"`
	Message         *lineMessage     `json:"message,omitempty"`
	DeliveryContext *lineDeliveryCtx `json:"deliveryContext,omitempty"`
	WebhookEventID  string           `json:"webhookEventId,omitempty"`
}

// lineDeliveryCtx — LINE marks isRedelivery=true when the same event is sent
// again after a webhook timeout. Tokens in redelivered events may already be
// invalid (or about to be), so we skip overwriting our cached replyToken.
type lineDeliveryCtx struct {
	IsRedelivery bool `json:"isRedelivery"`
}

type lineSource struct {
	Type    string `json:"type"`
	UserID  string `json:"userId"`
	GroupID string `json:"groupId,omitempty"`
	RoomID  string `json:"roomId,omitempty"`
}

type lineMessage struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Text     string `json:"text,omitempty"`
	Duration int    `json:"duration,omitempty"`
	FileName string `json:"fileName,omitempty"`
}

// ── Webhook handler ──────────────────────────────────────────────────────────

// POST /webhook/line/:oaId
// POST /webhook/line          (legacy — falls back to Destination lookup)
//
// Resolution order for which OA to route to:
//  1. URL param :oaId (new convention; admin pastes /webhook/line/<oa_id>
//     into LINE Developer Console)
//  2. payload.Destination (bot's own user ID) → registry.GetByBotUserID
//  3. registry.Any() (single-OA fallback for legacy URL with no destination)
func (h *LineHandler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	// Parse payload first so we can use Destination for OA lookup if URL has no :oaId.
	var payload linePayload
	if jerr := json.Unmarshal(body, &payload); jerr != nil {
		h.logger.Error("parse LINE webhook", zap.Error(jerr))
		c.Status(http.StatusBadRequest)
		return
	}

	oaID := c.Param("oaId")
	var svc *lineservice.Service
	var oaAccount *models.LineOAAccount
	if oaID != "" {
		svc = h.registry.Get(oaID)
		oaAccount = h.registry.Account(oaID)
	}
	if svc == nil && payload.Destination != "" {
		svc = h.registry.GetByBotUserID(payload.Destination)
	}
	if svc == nil {
		// Final fallback for legacy single-OA setups.
		svc = h.registry.Any()
		if oaAccount == nil {
			oaAccount = h.registry.AnyAccount()
		}
	}
	if svc == nil {
		h.logger.Warn("LINE webhook with no matching OA",
			zap.String("oa_id", oaID),
			zap.String("destination", payload.Destination))
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if oaAccount == nil && oaID != "" {
		oaAccount = h.registry.Account(oaID)
	}

	// Verify X-Line-Signature with the resolved OA's secret.
	sig := c.GetHeader("X-Line-Signature")
	if sig == "" || !svc.ValidateSignature(body, sig) {
		h.logger.Warn("invalid LINE signature",
			zap.String("oa_id", oaID),
			zap.String("destination", payload.Destination))
		c.Status(http.StatusBadRequest)
		return
	}

	// LINE expects 200 < 1s; do work async.
	c.Status(http.StatusOK)

	for _, event := range payload.Events {
		ev := event
		acc := oaAccount
		s := svc
		h.pool.Submit(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			h.processEvent(ctx, ev, s, acc)
		})
	}
}

func (h *LineHandler) processEvent(ctx context.Context, event lineEvent, svc *lineservice.Service, oa *models.LineOAAccount) {
	if event.Type != "message" || event.Message == nil {
		// follow/unfollow/postback/join/leave — ignored in v1
		return
	}
	h.captureNotificationCandidate(ctx, event, svc, oa)
}

func (h *LineHandler) captureNotificationCandidate(ctx context.Context, event lineEvent, svc *lineservice.Service, oa *models.LineOAAccount) {
	if h == nil || h.lineNotificationRepo == nil || oa == nil || oa.ID == "" || event.Message == nil {
		return
	}
	destinationType, destinationID := lineNotificationDestination(event.Source)
	if destinationID == "" {
		return
	}
	displayName := ""
	if destinationType == "user" && svc != nil {
		if profile, err := svc.GetProfile(destinationID); err == nil && profile != nil {
			displayName = profile.DisplayName
		} else if err != nil && h.logger != nil {
			h.logger.Warn("get LINE profile for notification candidate failed",
				zap.String("line_oa_id", oa.ID),
				zap.String("destination_type", destinationType),
				zap.String("destination", maskLineDestination(destinationID)),
				zap.Error(err))
		}
	}
	lastSeen := time.Now()
	if event.Timestamp > 0 {
		lastSeen = time.UnixMilli(event.Timestamp)
	}
	if _, err := h.lineNotificationRepo.UpsertContactCandidate(ctx, models.LineNotificationContactCandidateUpsert{
		LineOAID:           oa.ID,
		DestinationType:    destinationType,
		DestinationID:      destinationID,
		DisplayName:        displayName,
		LastMessagePreview: lineNotificationMessagePreview(event.Message),
		LastWebhookEventID: event.WebhookEventID,
		LastSeenAt:         lastSeen,
	}); err != nil && h.logger != nil {
		h.logger.Warn("upsert LINE notification candidate failed",
			zap.String("line_oa_id", oa.ID),
			zap.String("destination_type", destinationType),
			zap.String("destination", maskLineDestination(destinationID)),
			zap.Error(err))
	}
}

func lineNotificationDestination(source lineSource) (string, string) {
	sourceType := strings.ToLower(strings.TrimSpace(source.Type))
	switch sourceType {
	case "group":
		return "group", strings.TrimSpace(source.GroupID)
	case "room":
		return "room", strings.TrimSpace(source.RoomID)
	default:
		return "user", strings.TrimSpace(source.UserID)
	}
}

func lineNotificationMessagePreview(msg *lineMessage) string {
	if msg == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(msg.Type)) {
	case "text":
		text := strings.Join(strings.Fields(strings.TrimSpace(msg.Text)), " ")
		if text == "" {
			return "ส่งข้อความ"
		}
		return textPreview(text, 100)
	case "image":
		return "ส่งรูปภาพ"
	case "file":
		name := strings.TrimSpace(msg.FileName)
		if name == "" {
			return "ส่งไฟล์"
		}
		return textPreview("ส่งไฟล์: "+name, 100)
	case "audio":
		return "ส่งเสียง"
	default:
		if msg.Type == "" {
			return "ส่งข้อความ"
		}
		return textPreview("ส่ง "+msg.Type, 100)
	}
}

func maskLineDestination(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 10 {
		return id
	}
	return id[:6] + "..." + id[len(id)-4:]
}

// textPreview truncates s to n runes plus a single ellipsis. Rune-aware so
// Thai characters don't get cut mid-codepoint.
func textPreview(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
