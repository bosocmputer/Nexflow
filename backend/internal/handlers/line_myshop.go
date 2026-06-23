package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/linemyshop"
)

type lineMyShopNotifier interface {
	EnqueueLineMyShopOrder(ctx context.Context, snap *models.LineMyShopOrderSnapshot, dedupeKey string) (int, error)
}

type LineMyShopHandler struct {
	repo            *repository.LineMyShopRepo
	billRepo        *repository.BillRepo
	channelDefaults *repository.ChannelDefaultRepo
	auditRepo       *repository.AuditLogRepo
	lineNotifier    lineMyShopNotifier
	publicBaseURL   string
	logger          *zap.Logger
}

type lineMyShopSyncRequest struct {
	StartAt        string   `json:"start_at"`
	EndAt          string   `json:"end_at"`
	LookbackHours  int      `json:"lookback_hours"`
	PageLimit      int      `json:"page_limit"`
	PerPage        int      `json:"per_page"`
	OrderStatus    []string `json:"order_status"`
	PaymentStatus  []string `json:"payment_status"`
	ShipmentStatus []string `json:"shipment_status"`
}

type lineMyShopSyncResponse struct {
	ConnectionID        string   `json:"connection_id"`
	StartAt             string   `json:"start_at"`
	EndAt               string   `json:"end_at"`
	PagesFetched        int      `json:"pages_fetched"`
	OrdersScanned       int      `json:"orders_scanned"`
	DetailsFetched      int      `json:"details_fetched"`
	Snapshots           int      `json:"snapshots"`
	BillsCreated        int      `json:"bills_created"`
	BillsExisting       int      `json:"bills_existing"`
	NotificationsQueued int      `json:"notifications_queued"`
	Skipped             int      `json:"skipped"`
	Errors              []string `json:"errors,omitempty"`
}

func NewLineMyShopHandler(
	repo *repository.LineMyShopRepo,
	billRepo *repository.BillRepo,
	channelDefaults *repository.ChannelDefaultRepo,
	auditRepo *repository.AuditLogRepo,
	lineNotifier lineMyShopNotifier,
	publicBaseURL string,
	logger *zap.Logger,
) *LineMyShopHandler {
	return &LineMyShopHandler{
		repo:            repo,
		billRepo:        billRepo,
		channelDefaults: channelDefaults,
		auditRepo:       auditRepo,
		lineNotifier:    lineNotifier,
		publicBaseURL:   strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		logger:          logger,
	}
}

func (h *LineMyShopHandler) ListConnections(c *gin.Context) {
	rows, err := h.repo.ListConnections(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range rows {
		h.decorateConnection(&rows[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *LineMyShopHandler) CreateConnection(c *gin.Context) {
	var in models.LineMyShopConnectionUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if msg := validateLineMyShopConnectionUpsert(in, true); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	conn, err := h.repo.CreateConnection(c.Request.Context(), in, c.GetString("user_id"))
	if isLineMyShopChannelIDConflict(err) {
		c.JSON(http.StatusConflict, gin.H{"error": "Channel ID นี้ถูกใช้กับบัญชี LINE MyShop อื่นแล้ว"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.decorateConnection(conn)
	h.audit(c, "line_myshop_connection_created", map[string]interface{}{"connection_id": conn.ID, "name": conn.Name})
	c.JSON(http.StatusCreated, conn)
}

func (h *LineMyShopHandler) UpdateConnection(c *gin.Context) {
	var in models.LineMyShopConnectionUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if msg := validateLineMyShopConnectionUpsert(in, false); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	conn, err := h.repo.UpdateConnection(c.Request.Context(), c.Param("id"), in, c.GetString("user_id"))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบบัญชี LINE MyShop"})
		return
	}
	if isLineMyShopChannelIDConflict(err) {
		c.JSON(http.StatusConflict, gin.H{"error": "Channel ID นี้ถูกใช้กับบัญชี LINE MyShop อื่นแล้ว"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.decorateConnection(conn)
	h.audit(c, "line_myshop_connection_updated", map[string]interface{}{"connection_id": conn.ID, "name": conn.Name})
	c.JSON(http.StatusOK, conn)
}

func (h *LineMyShopHandler) DeleteConnection(c *gin.Context) {
	if err := h.repo.DeleteConnection(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "line_myshop_connection_deleted", map[string]interface{}{"connection_id": c.Param("id")})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LineMyShopHandler) SyncConnection(c *gin.Context) {
	var req lineMyShopSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	conn, err := h.repo.GetConnectionSecret(ctx, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if conn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบบัญชี LINE MyShop"})
		return
	}
	if !conn.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "บัญชี LINE MyShop นี้ปิดใช้งานอยู่"})
		return
	}
	if strings.TrimSpace(conn.APIKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "บัญชีนี้ยังไม่มี API key"})
		return
	}

	startAt, endAt := lineMyShopSyncWindow(req)
	perPage := req.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}
	pageLimit := req.PageLimit
	if pageLimit <= 0 {
		pageLimit = 3
	}
	if pageLimit > 10 {
		pageLimit = 10
	}

	client := linemyshop.NewClient(conn.APIKey)
	out := lineMyShopSyncResponse{
		ConnectionID: conn.ID,
		StartAt:      startAt.Format(time.RFC3339),
		EndAt:        endAt.Format(time.RFC3339),
	}
	syncNow := time.Now().UTC()
	for page := 1; page <= pageLimit; page++ {
		rawList, err := client.ListOrders(ctx, linemyshop.ListOrdersQuery{
			Page:           page,
			PerPage:        perPage,
			SortBy:         "UPDATED_AT",
			OrderBy:        "DESC",
			OrderStatus:    normalizeLineMyShopStatuses(req.OrderStatus),
			PaymentStatus:  normalizeLineMyShopStatuses(req.PaymentStatus),
			ShipmentStatus: normalizeLineMyShopStatuses(req.ShipmentStatus),
			StartAt:        startAt.Format(time.RFC3339),
			EndAt:          endAt.Format(time.RFC3339),
		})
		if err != nil {
			_ = h.repo.MarkConnectionSync(ctx, conn.ID, err.Error())
			c.JSON(http.StatusBadGateway, gin.H{"error": "LINE MyShop order sync failed", "detail": err.Error(), "data": out})
			return
		}
		list, err := linemyshop.DecodeOrderList(rawList)
		if err != nil {
			_ = h.repo.MarkConnectionSync(ctx, conn.ID, err.Error())
			c.JSON(http.StatusBadGateway, gin.H{"error": "LINE MyShop order list response invalid", "detail": err.Error(), "data": out})
			return
		}
		out.PagesFetched++
		if len(list.Data) == 0 {
			break
		}
		for _, summary := range list.Data {
			orderNo := strings.TrimSpace(summary.OrderNumber)
			if orderNo == "" {
				out.Skipped++
				continue
			}
			out.OrdersScanned++
			rawDetail, err := client.GetOrder(ctx, orderNo)
			if err != nil {
				out.Errors = append(out.Errors, orderNo+": "+err.Error())
				continue
			}
			out.DetailsFetched++
			payload, err := linemyshop.DecodeWebhookPayload(rawDetail)
			if err != nil {
				out.Errors = append(out.Errors, orderNo+": detail response invalid")
				continue
			}
			if payload.OrderNumber == "" {
				payload.OrderNumber = orderNo
			}
			payload.Event.Name = "POLLING_SYNC"
			payload.Event.Timestamp = syncNow.Format(time.RFC3339)
			snap, err := h.repo.UpsertSnapshot(ctx, repository.LineMyShopSnapshotUpsert{
				ConnectionID:   conn.ID,
				OrderNo:        payload.OrderNumber,
				OrderStatus:    payload.OrderStatus,
				PaymentStatus:  payload.PaymentStatus,
				ShipmentStatus: payload.ShipmentStatus,
				PaymentMethod:  payload.PaymentMethod,
				TotalAmount:    payload.TotalPrice,
				SubtotalPrice:  payload.SubtotalPrice,
				ShipmentPrice:  payload.ShipmentPrice,
				DiscountAmount: payload.DiscountAmount,
				ItemCount:      payload.ItemCount(),
				RawDetail:      rawDetail,
				LastEventName:  payload.Event.Name,
				LastEventAt:    &syncNow,
			})
			if err != nil {
				out.Errors = append(out.Errors, orderNo+": snapshot update failed")
				continue
			}
			out.Snapshots++
			snap.ConnectionName = conn.Name
			if !payload.EligibleForBill() {
				out.Skipped++
				continue
			}
			billID, created, err := h.ensureBillFromWebhook(ctx, conn, payload)
			if err != nil {
				out.Errors = append(out.Errors, orderNo+": create bill failed")
				continue
			}
			if billID != "" {
				snap.BillID = &billID
				_ = h.repo.LinkBill(ctx, conn.ID, payload.OrderNumber, billID, "")
			}
			if created {
				out.BillsCreated++
			} else {
				out.BillsExisting++
			}
			if h.lineNotifier != nil {
				queued, err := h.lineNotifier.EnqueueLineMyShopOrder(ctx, snap, "line_myshop:order:"+conn.ID+":"+payload.OrderNumber)
				if err != nil {
					out.Errors = append(out.Errors, orderNo+": notification enqueue failed")
				} else {
					out.NotificationsQueued += queued
				}
			}
		}
		if list.TotalPage > 0 && page >= list.TotalPage {
			break
		}
	}
	errMsg := strings.Join(lineMyShopSyncErrorsForStorage(out.Errors), "; ")
	_ = h.repo.MarkConnectionSync(ctx, conn.ID, errMsg)
	h.audit(c, "line_myshop_connection_synced", map[string]interface{}{
		"connection_id":  conn.ID,
		"name":           conn.Name,
		"start_at":       out.StartAt,
		"end_at":         out.EndAt,
		"pages_fetched":  out.PagesFetched,
		"orders_scanned": out.OrdersScanned,
		"bills_created":  out.BillsCreated,
		"bills_existing": out.BillsExisting,
		"errors_count":   len(out.Errors),
	})
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *LineMyShopHandler) Webhook(c *gin.Context) {
	if h == nil || h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "line myshop handler is not configured"})
		return
	}
	ctx := c.Request.Context()
	connectionID := strings.TrimSpace(c.Param("connection_id"))
	conn, err := h.repo.GetConnectionSecret(ctx, connectionID)
	if err != nil {
		h.logWarn("line_myshop_webhook_connection_lookup_failed", zap.String("connection_id", connectionID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "connection lookup failed"})
		return
	}
	if conn == nil || !conn.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "line myshop connection not found or disabled"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read request failed"})
		return
	}
	secret := strings.TrimSpace(conn.WebhookSecret)
	if secret == "" {
		secret = strings.TrimSpace(conn.APIKey)
	}
	if !linemyshop.VerifySignature(secret, body, c.GetHeader("x-myshop-signature")) {
		h.logWarn("line_myshop_webhook_bad_signature", zap.String("connection_id", connectionID))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}
	payload, err := linemyshop.DecodeWebhookPayload(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
		return
	}
	eventTime := payload.EventTime()
	requestID := strings.TrimSpace(c.GetHeader("x-request-id"))
	dedupeKey := linemyshop.DedupeKey(conn.ID, requestID, payload, body)
	event, inserted, err := h.repo.RecordWebhookEvent(ctx, repository.LineMyShopWebhookEventInput{
		ConnectionID:   conn.ID,
		OrderNo:        payload.OrderNumber,
		RequestID:      requestID,
		EventName:      payload.Event.Name,
		EventAt:        eventTime,
		DedupeKey:      dedupeKey,
		SignatureValid: true,
		RawPayload:     json.RawMessage(body),
	})
	if err != nil {
		h.logWarn("line_myshop_webhook_record_failed", zap.String("connection_id", conn.ID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "record webhook failed"})
		return
	}
	if !inserted && event != nil && event.ProcessingStatus == "processed" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "duplicate": true})
		return
	}

	snap, err := h.repo.UpsertSnapshot(ctx, repository.LineMyShopSnapshotUpsert{
		ConnectionID:   conn.ID,
		OrderNo:        payload.OrderNumber,
		OrderStatus:    payload.OrderStatus,
		PaymentStatus:  payload.PaymentStatus,
		ShipmentStatus: payload.ShipmentStatus,
		PaymentMethod:  payload.PaymentMethod,
		TotalAmount:    payload.TotalPrice,
		SubtotalPrice:  payload.SubtotalPrice,
		ShipmentPrice:  payload.ShipmentPrice,
		DiscountAmount: payload.DiscountAmount,
		ItemCount:      payload.ItemCount(),
		RawWebhook:     json.RawMessage(body),
		LastEventName:  payload.Event.Name,
		LastEventAt:    eventTime,
	})
	if err != nil {
		_ = h.repo.MarkWebhookEvent(ctx, dedupeKey, "failed", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "snapshot update failed"})
		return
	}
	snap.ConnectionName = conn.Name

	var billID string
	if payload.EligibleForBill() {
		billID, _, err = h.ensureBillFromWebhook(ctx, conn, payload)
		if err != nil {
			_ = h.repo.MarkWebhookEvent(ctx, dedupeKey, "failed", err.Error())
			h.logWarn("line_myshop_create_bill_failed", zap.String("connection_id", conn.ID), zap.String("order_no", payload.OrderNumber), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create bill failed"})
			return
		}
		if billID != "" {
			snap.BillID = &billID
			_ = h.repo.LinkBill(ctx, conn.ID, payload.OrderNumber, billID, "")
		}
		if h.lineNotifier != nil {
			if _, err := h.lineNotifier.EnqueueLineMyShopOrder(ctx, snap, "line_myshop:order:"+conn.ID+":"+payload.OrderNumber); err != nil {
				h.logWarn("line_myshop_line_notification_enqueue_failed", zap.String("connection_id", conn.ID), zap.String("order_no", payload.OrderNumber), zap.Error(err))
			}
		}
		_ = h.repo.MarkWebhookEvent(ctx, dedupeKey, "processed", "")
	} else {
		_ = h.repo.MarkWebhookEvent(ctx, dedupeKey, "skipped", "order is not eligible for bill creation")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_no": payload.OrderNumber, "bill_id": billID})
}

func (h *LineMyShopHandler) ensureBillFromWebhook(ctx context.Context, conn *models.LineMyShopConnectionSecret, payload linemyshop.WebhookPayload) (string, bool, error) {
	if existing, ok, err := h.repo.FindBillIDForOrder(ctx, conn.ID, payload.OrderNumber); err != nil {
		return "", false, err
	} else if ok {
		return existing, false, nil
	}
	if len(payload.OrderItems) == 0 {
		return "", false, fmt.Errorf("line myshop order has no items")
	}
	documentRoute := ""
	if h.channelDefaults != nil {
		if def, _ := h.channelDefaults.Get(models.LineMyShopSource, "sale"); def != nil {
			documentRoute, _ = resolveEndpoint(def, models.LineMyShopSource, "sale")
		}
	}
	aiConf := 1.0
	rawData, _ := json.Marshal(lineMyShopBillRawData(conn, payload, documentRoute))
	bill := &models.Bill{
		BillType:      "sale",
		Source:        models.LineMyShopSource,
		Status:        "needs_review",
		DocumentRoute: documentRoute,
		AIConfidence:  &aiConf,
		RawData:       rawData,
		SMLOrderID:    payload.OrderNumber,
	}
	if err := h.billRepo.Create(bill); err != nil {
		if pqErr := new(pq.Error); errors.As(err, &pqErr) && pqErr.Code == "23505" {
			if existing, ok, findErr := h.repo.FindBillIDForOrder(ctx, conn.ID, payload.OrderNumber); findErr == nil && ok {
				return existing, false, nil
			}
		}
		return "", false, err
	}
	for _, item := range payload.OrderItems {
		price := item.Price
		if item.DiscountedPrice != nil {
			price = *item.DiscountedPrice
		}
		rawName := linemyshop.RawItemName(item)
		if strings.TrimSpace(rawName) == "" {
			rawName = strings.TrimSpace(item.SKU)
		}
		bi := &models.BillItem{
			BillID:         bill.ID,
			RawName:        rawName,
			SourceSKU:      strings.TrimSpace(item.SKU),
			SourceImageURL: strings.TrimSpace(item.ImageURL),
			Qty:            item.Quantity,
			Price:          &price,
			Mapped:         false,
		}
		if bi.Qty <= 0 {
			bi.Qty = 1
		}
		if err := h.billRepo.InsertItemWithCandidates(bi, []byte(`[]`)); err != nil {
			h.logWarn("line_myshop_insert_bill_item_failed", zap.String("bill_id", bill.ID), zap.String("order_no", payload.OrderNumber), zap.Error(err))
		}
	}
	traceID := "line_myshop_webhook"
	if lineMyShopPayloadFlow(payload) == "line_myshop_sync" {
		traceID = "line_myshop_sync"
	}
	h.auditSystem("bill_created", bill.ID, traceID, map[string]interface{}{
		"line_myshop_connection_id": conn.ID,
		"line_myshop_order_no":      payload.OrderNumber,
		"items_count":               len(payload.OrderItems),
		"status":                    bill.Status,
		"document_route":            documentRoute,
	})
	return bill.ID, true, nil
}

func lineMyShopBillRawData(conn *models.LineMyShopConnectionSecret, payload linemyshop.WebhookPayload, documentRoute string) map[string]interface{} {
	raw := map[string]interface{}{
		"flow":                       lineMyShopPayloadFlow(payload),
		"line_myshop_connection_id":  conn.ID,
		"line_myshop_account_name":   conn.Name,
		"line_myshop_order_no":       payload.OrderNumber,
		"order_id":                   payload.OrderNumber,
		"doc_date":                   time.Now().Format("2006-01-02"),
		"event_name":                 payload.Event.Name,
		"event_timestamp":            payload.Event.Timestamp,
		"order_status":               payload.OrderStatus,
		"payment_status":             payload.PaymentStatus,
		"payment_method":             payload.PaymentMethod,
		"shipment_status":            payload.ShipmentStatus,
		"tracking_no":                payload.ShipmentDetail.TrackingNumber,
		"shipping_carrier":           firstNonEmptyLineMyShop(payload.ShipmentDetail.ShipmentCompanyNameTh, payload.ShipmentDetail.ShipmentCompanyNameEn, payload.ShipmentDetail.Name),
		"item_count":                 len(payload.OrderItems),
		"paid_total_amount":          payload.TotalPrice,
		"order_total_amount":         payload.TotalPrice,
		"shipping_amount":            payload.ShipmentPrice,
		"discount_amount":            payload.DiscountAmount,
		"document_route":             documentRoute,
		"contains_shipping_pii":      true,
		"shipping_pii_redacted_here": true,
	}
	if conn.ChannelID != nil {
		raw["line_myshop_channel_id"] = *conn.ChannelID
	}
	if conn.PremiumID != "" {
		raw["line_myshop_premium_id"] = conn.PremiumID
	}
	if conn.RandomID != "" {
		raw["line_myshop_random_id"] = conn.RandomID
	}
	return raw
}

func lineMyShopPayloadFlow(payload linemyshop.WebhookPayload) string {
	if strings.EqualFold(strings.TrimSpace(payload.Event.Name), "POLLING_SYNC") {
		return "line_myshop_sync"
	}
	return "line_myshop_webhook"
}

func (h *LineMyShopHandler) decorateConnection(conn *models.LineMyShopConnection) {
	if conn == nil {
		return
	}
	path := "/webhook/line-myshop/" + conn.ID
	if h.publicBaseURL != "" {
		conn.WebhookURL = h.publicBaseURL + path
	} else {
		conn.WebhookURL = path
	}
}

func firstNonEmptyLineMyShop(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func lineMyShopSyncWindow(req lineMyShopSyncRequest) (time.Time, time.Time) {
	now := time.Now().UTC()
	endAt := now
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EndAt)); err == nil {
		endAt = parsed.UTC()
	}
	lookback := req.LookbackHours
	if lookback <= 0 {
		lookback = 48
	}
	if lookback > 168 {
		lookback = 168
	}
	startAt := endAt.Add(-time.Duration(lookback) * time.Hour)
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartAt)); err == nil {
		startAt = parsed.UTC()
	}
	if !startAt.Before(endAt) {
		startAt = endAt.Add(-48 * time.Hour)
	}
	return startAt, endAt
}

func normalizeLineMyShopStatuses(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if v := strings.ToUpper(strings.TrimSpace(value)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func validateLineMyShopConnectionUpsert(in models.LineMyShopConnectionUpsert, requireAPIKey bool) string {
	if strings.TrimSpace(in.Name) == "" {
		return "กรุณากรอกชื่อบัญชี LINE MyShop"
	}
	if requireAPIKey && strings.TrimSpace(in.APIKey) == "" {
		return "กรุณากรอก LINE MyShop API key"
	}
	if strings.TrimSpace(in.APIKey) != "" && lineMyShopSecretHasWhitespaceOrControl(in.APIKey) {
		return "API key ต้องไม่มีช่องว่างหรืออักขระขึ้นบรรทัดใหม่ กรุณาตรวจการ copy/paste จาก OA Plus"
	}
	if strings.TrimSpace(in.WebhookSecret) != "" && lineMyShopSecretHasWhitespaceOrControl(in.WebhookSecret) {
		return "Webhook secret ต้องไม่มีช่องว่างหรืออักขระขึ้นบรรทัดใหม่ กรุณาตรวจการ copy/paste จาก OA Plus"
	}
	if in.ChannelID != nil && *in.ChannelID <= 0 {
		return "Channel ID ต้องเป็นตัวเลขจำนวนเต็มบวก"
	}
	return ""
}

func lineMyShopSecretHasWhitespaceOrControl(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func isLineMyShopChannelIDConflict(err error) bool {
	if err == nil {
		return false
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	return pqErr.Code == "23505" && pqErr.Constraint == "line_myshop_connections_channel_id_unique"
}

func lineMyShopSyncErrorsForStorage(errs []string) []string {
	if len(errs) <= 3 {
		return errs
	}
	return errs[:3]
}

func (h *LineMyShopHandler) audit(c *gin.Context, action string, detail map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:  action,
		UserID:  userID,
		Source:  models.LineMyShopSource,
		Level:   "info",
		TraceID: c.GetString("trace_id"),
		Detail:  detail,
	})
}

func (h *LineMyShopHandler) auditSystem(action, targetID, traceID string, detail map[string]interface{}) {
	if h.auditRepo == nil {
		return
	}
	var target *string
	if targetID != "" {
		target = &targetID
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   action,
		TargetID: target,
		Source:   models.LineMyShopSource,
		Level:    "info",
		TraceID:  traceID,
		Detail:   detail,
	})
}

func (h *LineMyShopHandler) logWarn(msg string, fields ...zap.Field) {
	if h.logger != nil {
		h.logger.Warn(msg, fields...)
	}
}
