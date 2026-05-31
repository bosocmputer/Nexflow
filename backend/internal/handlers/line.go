package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
	lineservice "nexflow/internal/services/line"
	"nexflow/internal/worker"
)

// LineHandler turns LINE webhook events into rows in chat_conversations +
// chat_messages (+ chat_media for binary attachments). Multi-OA aware: routes
// each event to the right OA via either /webhook/line/:oaId URL param OR the
// "destination" field in the payload (bot's own userID) for the legacy URL.
//
// No AI, no auto-replies (except the optional configured greeting per-OA).
type LineHandler struct {
	registry  *lineservice.Registry
	convRepo  *repository.ChatConversationRepo
	msgRepo   *repository.ChatMessageRepo
	mediaRepo *repository.ChatMediaRepo
	auditRepo *repository.AuditLogRepo
	pool      *worker.Pool
	cfg       *config.Config
	broker    *events.Broker
	logger    *zap.Logger
}

func NewLineHandler(
	registry *lineservice.Registry,
	convRepo *repository.ChatConversationRepo,
	msgRepo *repository.ChatMessageRepo,
	mediaRepo *repository.ChatMediaRepo,
	auditRepo *repository.AuditLogRepo,
	pool *worker.Pool,
	cfg *config.Config,
	broker *events.Broker,
	logger *zap.Logger,
) *LineHandler {
	return &LineHandler{
		registry:  registry,
		convRepo:  convRepo,
		msgRepo:   msgRepo,
		mediaRepo: mediaRepo,
		auditRepo: auditRepo,
		pool:      pool,
		cfg:       cfg,
		broker:    broker,
		logger:    logger,
	}
}

// ── Minimal webhook payload structs ──────────────────────────────────────────

type linePayload struct {
	Destination string      `json:"destination"`
	Events      []lineEvent `json:"events"`
}

type lineEvent struct {
	Type            string              `json:"type"`
	Timestamp       int64               `json:"timestamp"`
	ReplyToken      string              `json:"replyToken"`
	Source          lineSource          `json:"source"`
	Message         *lineMessage        `json:"message,omitempty"`
	DeliveryContext *lineDeliveryCtx    `json:"deliveryContext,omitempty"`
	WebhookEventID  string              `json:"webhookEventId,omitempty"`
}

// lineDeliveryCtx — LINE marks isRedelivery=true when the same event is sent
// again after a webhook timeout. Tokens in redelivered events may already be
// invalid (or about to be), so we skip overwriting our cached replyToken.
type lineDeliveryCtx struct {
	IsRedelivery bool `json:"isRedelivery"`
}

type lineSource struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
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
//   1. URL param :oaId (new convention; admin pastes /webhook/line/<oa_id>
//      into LINE Developer Console)
//   2. payload.Destination (bot's own user ID) → registry.GetByBotUserID
//   3. registry.Any() (single-OA fallback for legacy URL with no destination)
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
	h.processMessage(ctx, event, svc, oa)
}

// processMessage stores the inbound message and (on first contact) hydrates
// the conversation with display_name + picture from the LINE profile API.
// No AI, no auto-replies (except the optional configured greeting from the
// resolved OA's `greeting` column).
func (h *LineHandler) processMessage(ctx context.Context, event lineEvent, svc *lineservice.Service, oa *models.LineOAAccount) {
	_ = ctx
	msg := event.Message
	userID := event.Source.UserID
	if userID == "" {
		return
	}

	var oaID *string
	if oa != nil {
		id := oa.ID
		oaID = &id
	}

	conv, isNew, err := h.convRepo.UpsertWithOA(userID, "", "", oaID)
	if err != nil {
		h.logger.Error("upsert conversation", zap.String("user", userID), zap.Error(err))
		return
	}

	if isNew {
		if profile, perr := svc.GetProfile(userID); perr == nil && profile != nil {
			if _, _, uerr := h.convRepo.UpsertWithOA(userID, profile.DisplayName, profile.PictureURL, oaID); uerr == nil {
				conv.DisplayName = profile.DisplayName
				conv.PictureURL = profile.PictureURL
			}
		} else if perr != nil {
			h.logger.Warn("get LINE profile",
				zap.String("user", userID), zap.Error(perr))
		}
	}

	var inserted *models.ChatMessage
	switch msg.Type {
	case "text":
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return
		}
		inserted = h.insertText(userID, text, msg.ID, event.Timestamp)
	case "image", "file", "audio":
		inserted = h.insertMedia(userID, msg, event.Timestamp, svc)
	default:
		return
	}
	if inserted == nil {
		return
	}

	if err := h.convRepo.TouchLastMessage(userID, true); err != nil {
		h.logger.Warn("touch conversation", zap.String("user", userID), zap.Error(err))
	}
	if err := h.convRepo.IncrementUnread(userID); err != nil {
		h.logger.Warn("increment unread", zap.String("user", userID), zap.Error(err))
	}
	// Phase 4.2: customer messaged us → revive a 'resolved' thread to 'open'.
	// 'archived' stays sticky (admin must un-archive manually).
	if _, err := h.convRepo.AutoReviveOnInbound(userID); err != nil {
		h.logger.Warn("auto-revive status", zap.String("user", userID), zap.Error(err))
	}

	// First-contact greeting (if configured) consumes the replyToken.
	// We only cache the token for admin re-use when greeting did NOT consume it.
	isRedelivery := event.DeliveryContext != nil && event.DeliveryContext.IsRedelivery
	greetingSent := false
	if isNew && event.ReplyToken != "" {
		greet := ""
		if oa != nil && oa.Greeting != "" {
			greet = oa.Greeting
		} else if h.cfg != nil {
			greet = h.cfg.LineGreeting
		}
		if greet != "" {
			if err := svc.ReplyText(event.ReplyToken, greet); err != nil {
				h.logger.Warn("send greeting", zap.String("user", userID), zap.Error(err))
			} else {
				greetingSent = true
			}
		}
	}

	// Hybrid Reply+Push: cache the replyToken so admin's first response can
	// use the (free) Reply API instead of Push. Skip when:
	//   - the event is a redelivery (token may be stale)
	//   - greeting already consumed this token
	if event.ReplyToken != "" && !isRedelivery && !greetingSent {
		if err := h.convRepo.SetReplyToken(userID, event.ReplyToken); err != nil {
			h.logger.Warn("cache reply token", zap.String("user", userID), zap.Error(err))
		}
	}

	// Real-time push to admin tabs — broadcast both the new message AND the
	// updated unread count so the inbox list, the sidebar badge, and any open
	// thread of this user all update without polling.
	if h.broker != nil {
		h.broker.Publish(events.Event{
			Type: events.TypeMessageReceived,
			Payload: map[string]any{
				"line_user_id": userID,
				"message":      inserted,
			},
		})
		if total, err := h.convRepo.UnreadCount(); err == nil {
			h.broker.Publish(events.Event{
				Type:    events.TypeUnreadChanged,
				Payload: map[string]any{"total": total},
			})
		}
	}

	if h.auditRepo != nil {
		detail := map[string]interface{}{
			"line_user_id": userID,
			"kind":         inserted.Kind,
			"message_id":   inserted.ID,
		}
		// Add a content snippet so /logs shows what the customer actually
		// said (was previously message_id only — useless for at-a-glance).
		switch inserted.Kind {
		case models.ChatKindText:
			detail["text_preview"] = textPreview(inserted.TextContent, 100)
		case models.ChatKindImage, models.ChatKindFile, models.ChatKindAudio:
			if inserted.Media != nil {
				detail["filename"] = inserted.Media.Filename
				detail["size_bytes"] = inserted.Media.SizeBytes
			}
		}
		if oa != nil {
			detail["line_oa_id"] = oa.ID
			detail["line_oa_name"] = oa.Name
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action: "line_message_received",
			Source: "line",
			Level:  "info",
			Detail: detail,
		})
	}
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

func (h *LineHandler) insertText(userID, text, lineMsgID string, ts int64) *models.ChatMessage {
	m := &models.ChatMessage{
		LineUserID:    userID,
		Direction:     models.ChatDirectionIncoming,
		Kind:          models.ChatKindText,
		TextContent:   text,
		LineMessageID: lineMsgID,
	}
	if ts > 0 {
		t := ts
		m.LineEventTS = &t
	}
	if err := h.msgRepo.Insert(m); err != nil {
		h.logger.Error("insert text message",
			zap.String("user", userID), zap.Error(err))
		return nil
	}
	return m
}

func (h *LineHandler) insertMedia(userID string, msg *lineMessage, ts int64, svc *lineservice.Service) *models.ChatMessage {
	data, contentType, err := svc.DownloadContent(msg.ID)
	if err != nil {
		h.logger.Error("download LINE content",
			zap.String("user", userID), zap.String("msg_id", msg.ID), zap.Error(err))
		return nil
	}

	kind := lineMsgTypeToKind(msg.Type)
	filename := msg.FileName
	if filename == "" {
		filename = fmt.Sprintf("%s-%s", msg.Type, msg.ID)
	}

	m := &models.ChatMessage{
		LineUserID:    userID,
		Direction:     models.ChatDirectionIncoming,
		Kind:          kind,
		LineMessageID: msg.ID,
	}
	if ts > 0 {
		t := ts
		m.LineEventTS = &t
	}
	if err := h.msgRepo.Insert(m); err != nil {
		h.logger.Error("insert media message", zap.Error(err))
		return nil
	}

	mediaRow, err := h.mediaRepo.Save(m.ID, filename, contentType, data)
	if err != nil {
		h.logger.Error("save chat media",
			zap.String("msg_id", m.ID), zap.Error(err))
	} else {
		// Attach so the audit-log preview can include filename + size.
		m.Media = mediaRow
	}
	return m
}

// lineMsgTypeToKind maps LINE message type names to our chat_messages.kind enum.
func lineMsgTypeToKind(t string) string {
	switch t {
	case "image":
		return models.ChatKindImage
	case "file":
		return models.ChatKindFile
	case "audio":
		return models.ChatKindAudio
	}
	return models.ChatKindSystem
}
