package handlers

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/ai"
	"nexflow/internal/services/events"
	lineservice "nexflow/internal/services/line"
	"nexflow/internal/services/media"
	"nexflow/internal/services/mistral"
)

// ChatInboxHandler exposes /api/admin/conversations/* — admin-side endpoints
// for the human chat inbox. List conversations, fetch messages, send replies,
// download media, AI-extract a media row, create a bill from chat context,
// mark-read, unread-count.
//
// Multi-OA: SendReply looks up the conversation's line_oa_id and pushes via
// the matching service from lineRegistry, so each OA's own access_token is
// used. Falls back to registry.Any() when a conversation has no OA tagged
// (legacy rows from before migration 014).
type ChatInboxHandler struct {
	convRepo     *repository.ChatConversationRepo
	msgRepo      *repository.ChatMessageRepo
	mediaRepo    *repository.ChatMediaRepo
	billRepo     *repository.BillRepo
	auditRepo    *repository.AuditLogRepo
	lineRegistry *lineservice.Registry
	aiClient     *ai.Client
	ocrClient    *mistral.OCRClient
	mediaSigner  *media.Signer
	broker       *events.Broker
	publicURL    string
	logger       *zap.Logger
}

func NewChatInboxHandler(
	convRepo *repository.ChatConversationRepo,
	msgRepo *repository.ChatMessageRepo,
	mediaRepo *repository.ChatMediaRepo,
	billRepo *repository.BillRepo,
	auditRepo *repository.AuditLogRepo,
	lineRegistry *lineservice.Registry,
	aiClient *ai.Client,
	ocrClient *mistral.OCRClient,
	mediaSigner *media.Signer,
	broker *events.Broker,
	publicURL string,
	logger *zap.Logger,
) *ChatInboxHandler {
	return &ChatInboxHandler{
		convRepo:     convRepo,
		msgRepo:      msgRepo,
		mediaRepo:    mediaRepo,
		billRepo:     billRepo,
		auditRepo:    auditRepo,
		lineRegistry: lineRegistry,
		aiClient:     aiClient,
		ocrClient:    ocrClient,
		mediaSigner:  mediaSigner,
		broker:       broker,
		publicURL:    publicURL,
		logger:       logger,
	}
}

// publishUnread broadcasts the current global unread count after a state
// change. Best-effort — if it fails the next polling cycle catches up.
func (h *ChatInboxHandler) publishUnread() {
	if h.broker == nil {
		return
	}
	if n, err := h.convRepo.UnreadCount(); err == nil {
		h.broker.Publish(events.Event{
			Type:    events.TypeUnreadChanged,
			Payload: map[string]any{"total": n},
		})
	}
}

// publishConvUpdated broadcasts that a single conversation's metadata changed.
// Frontend subscribers will refetch / patch the row in their list.
func (h *ChatInboxHandler) publishConvUpdated(lineUserID string, fields map[string]any) {
	if h.broker == nil {
		return
	}
	payload := map[string]any{"line_user_id": lineUserID}
	for k, v := range fields {
		payload[k] = v
	}
	h.broker.Publish(events.Event{
		Type:    events.TypeConversationUpdated,
		Payload: payload,
	})
}

// pushService returns the LINE service for a given conversation's OA.
// Falls back to registry.Any() when the row has no line_oa_id (legacy).
func (h *ChatInboxHandler) pushService(conv *models.ChatConversation) *lineservice.Service {
	if conv != nil && conv.LineOAID != nil && *conv.LineOAID != "" {
		if svc := h.lineRegistry.Get(*conv.LineOAID); svc != nil {
			return svc
		}
	}
	return h.lineRegistry.Any()
}

// ── Conversation list ────────────────────────────────────────────────────────

// GET /api/admin/conversations?unread=true&status=open&q=ปูน&tags=id1,id2&limit=50&offset=0
func (h *ChatInboxHandler) ListConversations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	// Parse tags=id1,id2,id3 — comma-separated tag UUIDs (ANY-match).
	var tagIDs []string
	if raw := strings.TrimSpace(c.Query("tags")); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				tagIDs = append(tagIDs, id)
			}
		}
	}
	f := repository.ConversationListFilter{
		Limit:      limit,
		Offset:     offset,
		UnreadOnly: c.Query("unread") == "true",
		Status:     c.Query("status"),
		Q:          strings.TrimSpace(c.Query("q")),
		TagIDs:     tagIDs,
	}
	rows, err := h.convRepo.List(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	total, _ := h.convRepo.CountAll(f)
	c.JSON(http.StatusOK, gin.H{
		"data":   rows,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

// PATCH /api/admin/conversations/:lineUserId/phone
// Body: {phone: "081-234-5678"}  — pass empty string to clear.
func (h *ChatInboxHandler) SetPhone(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	var body struct {
		Phone string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	phone := strings.TrimSpace(body.Phone)
	if err := h.convRepo.SetPhone(lineUserID, phone); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "chat_phone_saved",
			UserID:  userID,
			Source:  "line",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail:  map[string]interface{}{"line_user_id": lineUserID, "phone": phone},
		})
	}
	h.publishConvUpdated(lineUserID, map[string]any{"phone": phone})
	c.JSON(http.StatusOK, gin.H{"ok": true, "phone": phone})
}

// PATCH /api/admin/conversations/:lineUserId/status
// Body: {status: "open"|"resolved"|"archived"}
func (h *ChatInboxHandler) SetStatus(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	var body struct {
		Status string `json:"status" binding:"required,oneof=open resolved archived"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.convRepo.SetStatus(lineUserID, body.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "line_conversation_status",
			UserID:  userID,
			Source:  "line",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail:  map[string]interface{}{"line_user_id": lineUserID, "status": body.Status},
		})
	}
	h.publishConvUpdated(lineUserID, map[string]any{"status": body.Status})
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": body.Status})
}

// GET /api/admin/conversations/unread-count
func (h *ChatInboxHandler) UnreadCount(c *gin.Context) {
	n, err := h.convRepo.UnreadCount()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

// ── Messages within a conversation ───────────────────────────────────────────

// GET /api/admin/conversations/:lineUserId/messages?since=ISO&limit=100
func (h *ChatInboxHandler) ListMessages(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	var since *time.Time
	if s := c.Query("since"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err == nil {
			since = &t
		}
	}
	q := strings.TrimSpace(c.Query("q"))
	rows, err := h.msgRepo.ListByUser(lineUserID, since, limit, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	conv, _ := h.convRepo.Get(lineUserID)
	c.JSON(http.StatusOK, gin.H{
		"data":         rows,
		"conversation": conv,
	})
}

// ── Admin sends a text reply ─────────────────────────────────────────────────

type sendReplyRequest struct {
	Text string `json:"text" binding:"required"`
}

// POST /api/admin/conversations/:lineUserId/messages
//
// Inserts an outgoing chat_message in DB (status=pending) → calls LINE Push API
// → updates status to sent or failed depending on the result.
// Returns the persisted row so the client can render it immediately.
func (h *ChatInboxHandler) SendReply(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	var req sendReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty text"})
		return
	}

	// Look up the conversation to determine which OA's token to push with.
	conv, _ := h.convRepo.Get(lineUserID)
	if conv == nil {
		// Stub row so the FK on chat_messages doesn't fail. No OA tagged —
		// pushService falls back to registry.Any() in single-OA setups.
		_, _, _ = h.convRepo.Upsert(lineUserID, "", "")
		conv, _ = h.convRepo.Get(lineUserID)
	}

	svc := h.pushService(conv)
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "LINE OA not configured — go to /settings/line-oa to add one",
		})
		return
	}

	var senderID *string
	if uid := c.GetString("user_id"); uid != "" {
		senderID = &uid
	}

	m := &models.ChatMessage{
		LineUserID:     lineUserID,
		Direction:      models.ChatDirectionOutgoing,
		Kind:           models.ChatKindText,
		TextContent:    text,
		SenderAdminID:  senderID,
		DeliveryStatus: models.ChatDeliveryPending,
	}
	if err := h.msgRepo.Insert(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Hybrid Reply+Push: try the (free) Reply API first if we have a fresh
	// token cached from the customer's last inbound. Fall back to Push only
	// when LINE explicitly rejects the token (expired/used).
	method, sendErr := h.sendOutgoingText(lineUserID, text, svc)
	if sendErr != nil {
		_ = h.msgRepo.UpdateDeliveryStatus(m.ID, models.ChatDeliveryFailed, method, sendErr.Error())
		m.DeliveryStatus = models.ChatDeliveryFailed
		m.DeliveryMethod = method
		m.DeliveryError = sendErr.Error()
		h.logger.Warn("LINE send failed",
			zap.String("user", lineUserID),
			zap.String("method", method),
			zap.Error(sendErr))
		c.JSON(http.StatusOK, gin.H{"message": m, "delivery": "failed"})
		return
	}

	_ = h.msgRepo.UpdateDeliveryStatus(m.ID, models.ChatDeliverySent, method, "")
	m.DeliveryStatus = models.ChatDeliverySent
	m.DeliveryMethod = method
	_ = h.convRepo.TouchLastMessage(lineUserID, false)

	if h.auditRepo != nil {
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "line_admin_reply",
			UserID:  senderID,
			Source:  "line",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"line_user_id":    lineUserID,
				"message_id":      m.ID,
				"delivery_method": method,
				"text_preview":    preview(text, 100),
			},
		})
	}
	// Broadcast to other admin tabs so they can render the new outgoing
	// bubble without polling. The sending tab has the optimistic insert
	// already; this catches everyone else.
	if h.broker != nil {
		h.broker.Publish(events.Event{
			Type: events.TypeMessageReceived,
			Payload: map[string]any{
				"line_user_id": lineUserID,
				"message":      m,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": m, "delivery": "sent", "method": method})
}

// sendOutgoingText tries Reply API first (free), falls back to Push (counted)
// if no token is cached or LINE rejects the cached token. Returns the actual
// transport used so the caller can record it on the chat_messages row.
//
// Errors that are NOT reply-token-related (auth/429/network) propagate back
// without falling back — pushing in those cases would either also fail or
// burn quota for no gain.
func (h *ChatInboxHandler) sendOutgoingText(lineUserID, text string, svc *lineservice.Service) (string, error) {
	token, _ := h.convRepo.ConsumeReplyToken(lineUserID)
	if token != "" {
		if err := svc.ReplyText(token, text); err == nil {
			return models.ChatDeliveryMethodReply, nil
		} else if !lineservice.IsReplyTokenError(err) {
			// Not a token problem — auth/rate-limit/network. Don't burn a Push.
			return models.ChatDeliveryMethodReply, err
		}
		// Token expired/invalid → fall through to Push.
		h.logger.Info("reply token rejected, falling back to push",
			zap.String("user", lineUserID))
	}
	if err := svc.PushText(lineUserID, text); err != nil {
		return models.ChatDeliveryMethodPush, err
	}
	return models.ChatDeliveryMethodPush, nil
}

// ── Customer history (Phase 4.5) ─────────────────────────────────────────────

// GET /api/admin/conversations/:lineUserId/history
//
// Returns the bills this LINE customer has placed (joined via raw_data->>
// 'line_user_id') so admin can see "this person ordered X last week" right
// next to the chat thread.
func (h *ChatInboxHandler) CustomerHistory(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	rows, err := h.billRepo.ListByLineUserID(lineUserID, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// ── Admin sends an image (Phase B) ───────────────────────────────────────────

// POST /api/admin/conversations/:lineUserId/messages/media
//
// Multipart upload (field "file") of an image. Saves the bytes to chat_media,
// inserts an outgoing chat_message (kind='image'), then pushes to LINE via
// the conversation's OA. LINE's servers fetch the bytes from the public
// /public/media/:id?t=<token> endpoint.
//
// LINE only supports image/video/audio for Push — no `file` type. v1 accepts
// images only; admin gets a clear toast if they try a non-image upload.
func (h *ChatInboxHandler) SendMedia(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	if h.publicURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "PUBLIC_BASE_URL ยังไม่ได้ตั้ง — ดู /tmp/nexflow-tunnel.log แล้ว paste URL ลง .env",
		})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องแนบไฟล์ในฟิลด์ 'file'"})
		return
	}
	// LINE limits image to ≤10MB. Reject early to avoid wasting upload bandwidth.
	if fileHeader.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ไฟล์เกิน 10MB — LINE จำกัดขนาดรูปไว้ที่ 10MB",
		})
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เปิดไฟล์ไม่ได้: " + err.Error()})
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านไฟล์ไม่ได้: " + err.Error()})
		return
	}

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "LINE รองรับเฉพาะรูปภาพในการ push — ไฟล์อื่นกรุณาส่งทาง email หรือ link (ปัจจุบัน: " + contentType + ")",
		})
		return
	}

	conv, _ := h.convRepo.Get(lineUserID)
	if conv == nil {
		_, _, _ = h.convRepo.Upsert(lineUserID, "", "")
		conv, _ = h.convRepo.Get(lineUserID)
	}
	svc := h.pushService(conv)
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "LINE OA ไม่ได้ตั้งค่า — เพิ่ม OA ใน /settings/line-oa ก่อน",
		})
		return
	}

	var senderID *string
	if uid := c.GetString("user_id"); uid != "" {
		senderID = &uid
	}

	// 1. Insert outgoing message row first (status=pending) — same pattern as
	//    SendReply for text. Lets the UI render an optimistic bubble.
	m := &models.ChatMessage{
		LineUserID:     lineUserID,
		Direction:      models.ChatDirectionOutgoing,
		Kind:           models.ChatKindImage,
		SenderAdminID:  senderID,
		DeliveryStatus: models.ChatDeliveryPending,
	}
	if err := h.msgRepo.Insert(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 2. Save the binary alongside.
	mediaRow, err := h.mediaRepo.Save(m.ID, fileHeader.Filename, contentType, data)
	if err != nil {
		_ = h.msgRepo.UpdateDeliveryStatus(m.ID, models.ChatDeliveryFailed, models.ChatDeliveryMethodPush, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกไฟล์ล้มเหลว: " + err.Error()})
		return
	}
	m.Media = mediaRow

	// 3. Build the public signed URL and send to LINE (Reply if token fresh,
	//    else Push). Same hybrid logic as sendOutgoingText.
	publicURL := h.mediaSigner.PublicURL(h.publicURL, mediaRow.ID)
	method, sendErr := h.sendOutgoingImage(lineUserID, publicURL, publicURL, svc)
	if sendErr != nil {
		_ = h.msgRepo.UpdateDeliveryStatus(m.ID, models.ChatDeliveryFailed, method, sendErr.Error())
		m.DeliveryStatus = models.ChatDeliveryFailed
		m.DeliveryMethod = method
		m.DeliveryError = sendErr.Error()
		h.logger.Warn("LINE send-image failed",
			zap.String("user", lineUserID),
			zap.String("method", method),
			zap.Error(sendErr))
		c.JSON(http.StatusOK, gin.H{"message": m, "delivery": "failed"})
		return
	}

	_ = h.msgRepo.UpdateDeliveryStatus(m.ID, models.ChatDeliverySent, method, "")
	m.DeliveryStatus = models.ChatDeliverySent
	m.DeliveryMethod = method
	_ = h.convRepo.TouchLastMessage(lineUserID, false)

	if h.auditRepo != nil {
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "line_admin_send_media",
			UserID:  senderID,
			Source:  "line",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"line_user_id":    lineUserID,
				"message_id":      m.ID,
				"filename":        mediaRow.Filename,
				"size_bytes":      mediaRow.SizeBytes,
				"content_type":    mediaRow.ContentType,
				"delivery_method": method,
			},
		})
	}
	if h.broker != nil {
		h.broker.Publish(events.Event{
			Type: events.TypeMessageReceived,
			Payload: map[string]any{
				"line_user_id": lineUserID,
				"message":      m,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": m, "delivery": "sent", "method": method})
}

// sendOutgoingImage mirrors sendOutgoingText but for image messages.
func (h *ChatInboxHandler) sendOutgoingImage(lineUserID, originalURL, previewURL string, svc *lineservice.Service) (string, error) {
	token, _ := h.convRepo.ConsumeReplyToken(lineUserID)
	if token != "" {
		if err := svc.ReplyImage(token, originalURL, previewURL); err == nil {
			return models.ChatDeliveryMethodReply, nil
		} else if !lineservice.IsReplyTokenError(err) {
			return models.ChatDeliveryMethodReply, err
		}
		h.logger.Info("reply token rejected on image, falling back to push",
			zap.String("user", lineUserID))
	}
	if err := svc.PushImage(lineUserID, originalURL, previewURL); err != nil {
		return models.ChatDeliveryMethodPush, err
	}
	return models.ChatDeliveryMethodPush, nil
}

// ── Mark conversation as read ────────────────────────────────────────────────

// POST /api/admin/conversations/:lineUserId/mark-read
//
// Side effects:
//   1. Zero unread_admin_count for this conversation in Nexflow
//   2. Broadcast UnreadChanged + ConversationUpdated so other admin tabs
//      and the sidebar badge update without polling
//   3. If the OA has mark_as_read_enabled=true, call LINE markMessagesAsRead
//      so the customer sees "อ่านแล้ว" (Premium feature; ignored otherwise)
func (h *ChatInboxHandler) MarkRead(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	if err := h.convRepo.MarkRead(lineUserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.publishConvUpdated(lineUserID, map[string]any{"unread_admin_count": 0})
	h.publishUnread()

	// Best-effort LINE read receipt — only if the OA has it enabled.
	// Errors are logged + swallowed (Free OA returns 403).
	if conv, _ := h.convRepo.Get(lineUserID); conv != nil && conv.LineOAID != nil {
		oa := h.lineRegistry.Account(*conv.LineOAID)
		svc := h.lineRegistry.Get(*conv.LineOAID)
		if oa != nil && oa.MarkAsReadEnabled && svc != nil {
			if err := svc.MarkMessagesAsRead(lineUserID); err != nil {
				h.logger.Debug("LINE markMessagesAsRead failed",
					zap.String("user", lineUserID), zap.Error(err))
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── Media file download (image/file/audio) ───────────────────────────────────

// GET /api/admin/conversations/:lineUserId/messages/:messageId/media
//
// Streams the binary content with the original Content-Type, so an <img> tag
// or <audio> tag can render it directly. The :lineUserId in the URL is for
// REST aesthetics — auth is the same as everything else (admin/staff JWT).
func (h *ChatInboxHandler) DownloadMedia(c *gin.Context) {
	messageID := c.Param("messageId")
	if messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messageId required"})
		return
	}
	media, err := h.mediaRepo.GetByMessageID(messageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if media == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
		return
	}
	data, _, err := h.mediaRepo.ReadBytes(media.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ct := media.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Content-Disposition", "inline; filename=\""+media.Filename+"\"")
	c.Data(http.StatusOK, ct, data)
}

// ── Manual AI extract on a media message ─────────────────────────────────────

// POST /api/admin/conversations/:lineUserId/messages/:messageId/extract
//
// Runs the appropriate AI extractor on the attached media and returns the
// preview ExtractedBill. Does NOT write anything to DB — admin then chooses
// to "ใช้ข้อมูลนี้สร้างบิล" which triggers the bill-create endpoint below.
func (h *ChatInboxHandler) ExtractFromMedia(c *gin.Context) {
	messageID := c.Param("messageId")
	if messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messageId required"})
		return
	}
	if h.aiClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI client not configured"})
		return
	}

	msg, err := h.msgRepo.Get(messageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if msg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	}

	media, err := h.mediaRepo.GetByMessageID(messageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if media == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message has no media to extract"})
		return
	}
	data, _, err := h.mediaRepo.ReadBytes(media.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var extracted *ai.ExtractedBill
	switch msg.Kind {
	case models.ChatKindImage:
		extracted, err = h.aiClient.ExtractImage(base64.StdEncoding.EncodeToString(data), media.ContentType)
	case models.ChatKindFile:
		// Treat as PDF — Mistral OCR if configured, else direct AI.
		if !strings.Contains(strings.ToLower(media.ContentType), "pdf") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "only PDF files supported for now"})
			return
		}
		if h.ocrClient != nil && h.ocrClient.IsConfigured() {
			ocrText, oerr := h.ocrClient.ExtractTextFromPDF(base64.StdEncoding.EncodeToString(data))
			if oerr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "OCR failed: " + oerr.Error()})
				return
			}
			extracted, err = h.aiClient.ExtractText(ocrText)
		} else {
			extracted, err = h.aiClient.ExtractPDF(base64.StdEncoding.EncodeToString(data))
		}
	case models.ChatKindAudio:
		text, terr := h.aiClient.TranscribeAudio(data)
		if terr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "transcription failed: " + terr.Error()})
			return
		}
		extracted, err = h.aiClient.ExtractText(text)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "media kind not extractable: " + msg.Kind})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if extracted == nil {
		c.JSON(http.StatusOK, gin.H{"extracted": nil, "note": "no items detected"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"extracted": extracted})
}

// ── Create bill from a conversation ──────────────────────────────────────────

type createBillFromChatRequest struct {
	CustomerName  string                  `json:"customer_name"`
	CustomerPhone string                  `json:"customer_phone"`
	Note          string                  `json:"note"`
	Items         []chatBillItemInputRow `json:"items" binding:"required,min=1"`
}

type chatBillItemInputRow struct {
	ItemCode string  `json:"item_code"`
	RawName  string  `json:"raw_name" binding:"required"`
	UnitCode string  `json:"unit_code"`
	Qty      float64 `json:"qty" binding:"required,gt=0"`
	Price    float64 `json:"price"`
}

// POST /api/admin/conversations/:lineUserId/bills
//
// Creates a Bill with source="line", status="pending", raw_data.line_user_id =
// the conversation's userID. Items are inserted as bill_items rows. The bill
// then shows up in /bills and the admin can use the existing Retry button to
// push to SML 248 saleorder via bills Retry flow.
func (h *ChatInboxHandler) CreateBill(c *gin.Context) {
	lineUserID := c.Param("lineUserId")
	if lineUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lineUserId required"})
		return
	}
	var req createBillFromChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	conv, _ := h.convRepo.Get(lineUserID)
	customerName := strings.TrimSpace(req.CustomerName)
	if customerName == "" && conv != nil {
		customerName = conv.DisplayName
	}

	rawData := map[string]interface{}{
		"flow":         "line_chat",
		"line_user_id": lineUserID,
		"customer_name": customerName,
	}
	if req.CustomerPhone != "" {
		rawData["customer_phone"] = req.CustomerPhone
	}
	if req.Note != "" {
		rawData["note"] = req.Note
	}
	rawJSON, _ := json.Marshal(rawData)

	conf := 1.0 // human-curated, not AI
	bill := &models.Bill{
		BillType:     "sale",
		Source:       "line",
		Status:       "pending",
		RawData:      rawJSON,
		AIConfidence: &conf,
	}
	if uid := c.GetString("user_id"); uid != "" {
		bill.CreatedBy = &uid
	}
	if err := h.billRepo.Create(bill); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, it := range req.Items {
		bi := &models.BillItem{
			BillID:  bill.ID,
			RawName: it.RawName,
			Qty:     it.Qty,
		}
		if it.ItemCode != "" {
			code := it.ItemCode
			bi.ItemCode = &code
			bi.Mapped = true
		}
		if it.UnitCode != "" {
			unit := it.UnitCode
			bi.UnitCode = &unit
		}
		if it.Price > 0 {
			price := it.Price
			bi.Price = &price
		}
		if err := h.billRepo.InsertItem(bi); err != nil {
			h.logger.Warn("insert bill item from chat",
				zap.String("bill_id", bill.ID), zap.Error(err))
		}
	}

	// Drop a system message in the chat thread so the conversation has a
	// breadcrumb pointing to the bill (helps admins picking up the thread later).
	systemText := "📄 สร้างบิลขายแล้ว — รอตรวจสอบและส่ง SML"
	sysMsg := &models.ChatMessage{
		LineUserID:  lineUserID,
		Direction:   models.ChatDirectionSystem,
		Kind:        models.ChatKindSystem,
		TextContent: systemText,
	}
	_ = h.msgRepo.Insert(sysMsg)
	_ = h.convRepo.TouchLastMessage(lineUserID, false)

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		billIDStr := bill.ID
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "bill_created",
			TargetID: &billIDStr,
			UserID:   userID,
			Source:   "line_chat",
			Level:    "info",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"line_user_id": lineUserID,
				"items_count":  len(req.Items),
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"bill_id": bill.ID,
		"message": "สร้างบิลแล้ว — กรุณาตรวจสอบและกดส่ง SML ใน /bills",
	})
}
