package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
	"nexflow/internal/services/shopeeapi"
)

const (
	shopeeRealtimeDefaultPageSize = 20
	shopeeRealtimeMaxSyncPages    = 20
	shopeeRealtimeBulkCreateLimit = 50
)

var shopeeRealtimeSyncStatuses = []string{
	"UNPAID",
	"READY_TO_SHIP",
	"PROCESSED",
	"SHIPPED",
	"COMPLETED",
	"CANCELLED",
}

type ShopeeRealtimeHandler struct {
	repo             *repository.ShopeeRealtimeRepo
	notificationRepo *repository.NotificationRepo
	lineNotifier     lineOrderNotifier
	broker           *events.Broker
	importH          *ShopeeImportHandler
	billH            *BillHandler
	cfg              *config.Config
	logger           *zap.Logger
}

func NewShopeeRealtimeHandler(repo *repository.ShopeeRealtimeRepo, notificationRepo *repository.NotificationRepo, broker *events.Broker, importH *ShopeeImportHandler, billH *BillHandler, cfg *config.Config, logger *zap.Logger) *ShopeeRealtimeHandler {
	return &ShopeeRealtimeHandler{repo: repo, notificationRepo: notificationRepo, broker: broker, importH: importH, billH: billH, cfg: cfg, logger: logger}
}

type lineOrderNotifier interface {
	EnqueueShopeeNewOrder(ctx context.Context, snap *models.ShopeeOrderSnapshot, dedupeKey string) (int, error)
}

type shippingOrderRequest struct {
	Confirm       string                 `json:"confirm"`
	PackageNumber string                 `json:"package_number"`
	Pickup        map[string]interface{} `json:"pickup"`
	Dropoff       map[string]interface{} `json:"dropoff"`
	NonIntegrated map[string]interface{} `json:"non_integrated"`
}

type shopeeRealtimeOrderRef struct {
	ShopID  int64  `json:"shop_id"`
	OrderSN string `json:"order_sn"`
}

type shopeeRealtimeBulkCreatePreviewRequest struct {
	Orders []shopeeRealtimeOrderRef `json:"orders"`
}

type shopeeRealtimeBulkCreateRequest struct {
	Confirm        string                   `json:"confirm"`
	RouteSignature string                   `json:"route_signature"`
	Orders         []shopeeRealtimeOrderRef `json:"orders"`
}

type shopeeRealtimeBulkOrderResult struct {
	ShopID        int64   `json:"shop_id"`
	OrderSN       string  `json:"order_sn"`
	BuyerUsername string  `json:"buyer_username,omitempty"`
	OrderStatus   string  `json:"order_status,omitempty"`
	ERPStatus     string  `json:"erp_status,omitempty"`
	TotalAmount   float64 `json:"total_amount,omitempty"`
	ItemCount     int     `json:"item_count,omitempty"`
	BillID        string  `json:"bill_id,omitempty"`
	BillURL       string  `json:"bill_url,omitempty"`
	DocumentRoute string  `json:"document_route,omitempty"`
	DocNo         string  `json:"doc_no,omitempty"`
	Status        string  `json:"status"`
	Reason        string  `json:"reason,omitempty"`
	Message       string  `json:"message,omitempty"`
}

type shopeeCreateDocumentOutcome struct {
	ShopID        int64
	OrderSN       string
	Status        string
	ERPStatus     string
	BillID        string
	BillURL       string
	DocumentRoute string
	DocNo         string
	Message       string
	Reason        string
	Route         gin.H
	HTTPStatus    int
}

func (h *ShopeeRealtimeHandler) SetLineNotifier(notifier lineOrderNotifier) {
	if h != nil {
		h.lineNotifier = notifier
	}
}

func (h *ShopeeRealtimeHandler) enabled(c *gin.Context) bool {
	if h == nil || h.repo == nil || h.importH == nil || h.cfg == nil || !h.cfg.ShopeeRealtimeOpsEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shopee Realtime ยังไม่เปิดใช้งาน"})
		return false
	}
	return true
}

func (h *ShopeeRealtimeHandler) Readiness(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	status := h.importH.shopeeAPIStatus()
	conns, err := h.importH.listShopeeAPIConnections(c.Request.Context(), true)
	if err != nil {
		h.logger.Warn("shopee_realtime: list connections failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดร้าน Shopee ไม่สำเร็จ"})
		return
	}
	active := []ShopeeAPIConnectionView{}
	now := time.Now()
	for i := range conns {
		if !conns[i].DisabledAt.Valid {
			active = append(active, shopeeAPIConnectionView(&conns[i], now))
		}
	}
	status.Connected = len(active) > 0
	if len(active) > 0 {
		status.ShopID = active[0].ShopID
		status.ShopName = active[0].Label
		status.AccessExpiresAt = active[0].AccessExpiresAt
		status.RefreshExpiresAt = active[0].RefreshExpiresAt
		status.LastSyncStatus = active[0].LastSyncStatus
		status.LastSyncError = active[0].LastSyncError
		status.LastSyncAt = active[0].LastSyncAt
	}
	status.finalizeReadiness(now)
	route := h.realtimeRouteReadiness(c.Request.Context())
	pushReadiness := h.pushReadiness(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"enabled":     h.cfg.ShopeeRealtimeOpsEnabled,
		"api":         status,
		"connections": active,
		"push":        pushReadiness,
		"sml":         route,
	})
}

func (h *ShopeeRealtimeHandler) pushReadiness(ctx context.Context) gin.H {
	out := gin.H{
		"configured":                   strings.TrimSpace(h.cfg.ShopeeRealtimeWebhookSecret) != "",
		"url":                          strings.TrimRight(h.cfg.PublicBaseURL, "/") + "/webhook/shopee",
		"message":                      shopeePushReadinessMessage(h.cfg),
		"deployment_service_area_hint": "Singapore",
		"console_status":               "not_verified",
	}
	if h.repo == nil {
		return out
	}
	events, err := h.repo.RecentPushEvents(ctx, 1)
	if err != nil || len(events) == 0 {
		return out
	}
	latest := events[0]
	out["console_status"] = "receiving"
	out["latest_event"] = latest
	out["last_event_at"] = latest.ReceivedAt
	out["last_event_name"] = latest.PushName
	switch {
	case latest.IsVerificationEvent:
		out["message"] = fmt.Sprintf("Shopee Console verify callback สำเร็จเมื่อ %s ไม่ใช่ออเดอร์จริง", latest.ReceivedAt.Format("02/01/06 15:04"))
	case latest.Source == "shop_auth":
		out["message"] = fmt.Sprintf("รับ event สิทธิ์ร้านจาก Shopee: %s เมื่อ %s", latest.PushName, latest.ReceivedAt.Format("02/01/06 15:04"))
	default:
		out["message"] = fmt.Sprintf("รับ Shopee Push ล่าสุด: %s เมื่อ %s", latest.PushName, latest.ReceivedAt.Format("02/01/06 15:04"))
	}
	return out
}

func (h *ShopeeRealtimeHandler) realtimeRouteReadiness(ctx context.Context) gin.H {
	out := gin.H{
		"mode":                "create_document_then_manual_sml",
		"channel":             "shopee_realtime",
		"bill_type":           "sale",
		"can_create_document": false,
		"ready_to_send_sml":   false,
		"route":               "ยังไม่ได้ตั้งค่า",
		"message":             "ตั้งค่า Shopee Realtime / sale ในหน้าเส้นทางเอกสาร SML",
	}
	if h == nil || h.importH == nil || h.importH.channelDefaults == nil {
		return out
	}
	def, err := h.importH.channelDefaults.Get("shopee_realtime", "sale")
	if err != nil || def == nil {
		return out
	}
	cfg := h.importH.CurrentShopeeSaleConfigForChannel("shopee_realtime")
	destination := shopeeImportDocumentName(cfg)
	canCreate := strings.TrimSpace(def.Endpoint) != "" && strings.TrimSpace(def.DocFormatCode) != ""
	readyToSend := canCreate &&
		strings.TrimSpace(cfg.CustCode) != "" &&
		strings.TrimSpace(def.DocPrefix) != "" &&
		strings.TrimSpace(def.DocRunningFormat) != "" &&
		strings.TrimSpace(cfg.WHCode) != "" &&
		strings.TrimSpace(cfg.ShelfCode) != "" &&
		strings.TrimSpace(cfg.DocTime) != "" &&
		cfg.VATType >= 0 &&
		cfg.VATRate >= 0
	out["can_create_document"] = canCreate
	out["ready_to_send_sml"] = readyToSend
	out["route"] = destination
	out["document_route"] = shopeeImportRoute(cfg)
	out["endpoint"] = def.Endpoint
	out["doc_format_code"] = def.DocFormatCode
	out["doc_prefix"] = def.DocPrefix
	out["doc_running_format"] = def.DocRunningFormat
	if canCreate {
		out["message"] = "สร้างเอกสารใน Nexflow ได้ แล้วให้ผู้ใช้ส่ง SML จากหน้าคิวเอกสาร"
	} else {
		out["message"] = "กรุณาตั้งปลายทางและ doc format ของ Shopee Realtime ก่อนสร้างเอกสาร"
	}
	if readyToSend {
		out["message"] = "เส้นทางพร้อมสร้างเอกสารและพร้อมส่ง SML จากหน้าคิวเอกสาร"
	}
	_ = ctx
	return out
}

func (h *ShopeeRealtimeHandler) ListOrders(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	page := parsePositiveInt(c.Query("page"), 1)
	perPage := parsePositiveInt(c.Query("per_page"), shopeeRealtimeDefaultPageSize)
	if perPage > 100 {
		perPage = 100
	}
	shopID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("shop_id")), 10, 64)
	rows, total, err := h.repo.ListSnapshots(c.Request.Context(), models.ShopeeOrderSnapshotFilter{
		ShopID:      shopID,
		Status:      c.Query("status"),
		StatusGroup: c.Query("status_group"),
		ERPStatus:   c.Query("erp_status"),
		Search:      c.Query("search"),
		Page:        page,
		PageSize:    perPage,
	})
	if err != nil {
		h.logger.Warn("shopee_realtime: list snapshots failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด Shopee Realtime ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "per_page": perPage})
}

func (h *ShopeeRealtimeHandler) Counts(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("shop_id")), 10, 64)
	counts, err := h.repo.Counts(c.Request.Context(), shopID)
	if err != nil {
		h.logger.Warn("shopee_realtime: counts failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดตัวเลข Shopee Realtime ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, counts)
}

func (h *ShopeeRealtimeHandler) BulkCreateDocumentsPreview(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	var req shopeeRealtimeBulkCreatePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload ไม่ถูกต้อง"})
		return
	}
	refs, err := normalizeShopeeRealtimeOrderRefs(req.Orders)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ready, skipped, route, signature := h.previewBulkCreateDocuments(c.Request.Context(), refs)
	c.JSON(http.StatusOK, gin.H{
		"route":           route,
		"route_signature": signature,
		"ready":           ready,
		"skipped":         skipped,
		"ready_count":     len(ready),
		"skipped_count":   len(skipped),
		"max_batch":       shopeeRealtimeBulkCreateLimit,
	})
}

func (h *ShopeeRealtimeHandler) BulkCreateDocuments(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	var req shopeeRealtimeBulkCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload ไม่ถูกต้อง"})
		return
	}
	if req.Confirm != "CREATE_DOCUMENTS" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณายืนยันด้วย CREATE_DOCUMENTS"})
		return
	}
	refs, err := normalizeShopeeRealtimeOrderRefs(req.Orders)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_, _, routeErr := h.realtimeSaleConfig(c.Request.Context())
	if routeErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": routeErr.Error()})
		return
	}
	currentSignature := h.realtimeRouteSignature(c.Request.Context())
	if strings.TrimSpace(req.RouteSignature) == "" || strings.TrimSpace(req.RouteSignature) != currentSignature {
		c.JSON(http.StatusConflict, gin.H{
			"error":           "เส้นทาง Shopee Realtime เปลี่ยนไป กรุณาเปิด preview ใหม่ก่อนสร้างเอกสาร",
			"code":            "route_changed",
			"route_signature": currentSignature,
		})
		return
	}

	requestRaw, _ := json.Marshal(gin.H{"confirm": "CREATE_DOCUMENTS"})
	created := []shopeeRealtimeBulkOrderResult{}
	reused := []shopeeRealtimeBulkOrderResult{}
	skipped := []shopeeRealtimeBulkOrderResult{}
	failed := []shopeeRealtimeBulkOrderResult{}
	for _, ref := range refs {
		outcome := h.createDocumentForOrder(c.Request.Context(), ref.ShopID, ref.OrderSN, c.GetString("user_id"), c.GetString("trace_id"), requestRaw)
		row := outcome.toBulkResult()
		switch outcome.Status {
		case "created":
			created = append(created, row)
		case "reused":
			reused = append(reused, row)
		case "skipped":
			skipped = append(skipped, row)
		default:
			failed = append(failed, row)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"created":       created,
		"reused":        reused,
		"skipped":       skipped,
		"failed":        failed,
		"created_count": len(created),
		"reused_count":  len(reused),
		"skipped_count": len(skipped),
		"failed_count":  len(failed),
	})
}

type shopeeRealtimeSyncRequest struct {
	ConnectionID string `json:"connection_id"`
	Days         int    `json:"days"`
}

func (h *ShopeeRealtimeHandler) SyncNow(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	var req shopeeRealtimeSyncRequest
	_ = c.ShouldBindJSON(&req)
	conn, err := h.importH.ensureShopeeAPIAccessToken(c.Request.Context(), req.ConnectionID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	days := req.Days
	if days <= 0 || days > 15 {
		days = 14
	}
	to := time.Now()
	from := to.AddDate(0, 0, -days)
	summary, err := h.syncConnection(c.Request.Context(), conn, from, to)
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "ซิงก์ Shopee Realtime ไม่สำเร็จ").Message
		h.repo.MarkConnectionSync(c.Request.Context(), conn.ShopID, "error", msg)
		h.notifyShopeeIssue(c.Request.Context(), conn.ShopID, conn.DisplayLabel(), "error", "ซิงก์ Shopee Realtime ไม่สำเร็จ", msg, fmt.Sprintf("sync_error:%d:%s", conn.ShopID, time.Now().Format("2006010215")))
		h.logger.Warn("shopee_realtime: sync failed", zap.Int64("shop_id", conn.ShopID), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": msg})
		return
	}
	h.repo.MarkConnectionSync(c.Request.Context(), conn.ShopID, "ok", "")
	c.JSON(http.StatusOK, summary)
}

func (h *ShopeeRealtimeHandler) SyncAllActive(ctx context.Context, days int) (int, error) {
	if h == nil || h.repo == nil || h.importH == nil || h.cfg == nil || !h.cfg.ShopeeRealtimeOpsEnabled {
		return 0, nil
	}
	if days <= 0 || days > 15 {
		days = 14
	}
	conns, err := h.importH.listShopeeAPIConnections(ctx, false)
	if err != nil {
		return 0, err
	}
	total := 0
	to := time.Now()
	from := to.AddDate(0, 0, -days)
	for i := range conns {
		conn, err := h.importH.ensureShopeeAPIAccessToken(ctx, conns[i].ID)
		if err != nil {
			h.repo.MarkConnectionSync(ctx, conns[i].ShopID, "error", err.Error())
			h.notifyShopeeIssue(ctx, conns[i].ShopID, conns[i].DisplayLabel(), "error", "เชื่อมต่อร้าน Shopee ไม่สำเร็จ", err.Error(), fmt.Sprintf("token_error:%d:%s", conns[i].ShopID, time.Now().Format("2006010215")))
			continue
		}
		summary, err := h.syncConnection(ctx, conn, from, to)
		if err != nil {
			msg := shopeeAPIErrorMessage(err, "ซิงก์ Shopee Realtime ไม่สำเร็จ").Message
			h.repo.MarkConnectionSync(ctx, conn.ShopID, "error", msg)
			h.notifyShopeeIssue(ctx, conn.ShopID, conn.DisplayLabel(), "error", "ซิงก์ Shopee Realtime ไม่สำเร็จ", msg, fmt.Sprintf("sync_error:%d:%s", conn.ShopID, time.Now().Format("2006010215")))
			continue
		}
		h.repo.MarkConnectionSync(ctx, conn.ShopID, "ok", "")
		if n, ok := summary["synced_orders"].(int); ok {
			total += n
		}
	}
	return total, nil
}

func (h *ShopeeRealtimeHandler) StartReconcileWorker(ctx context.Context, interval time.Duration, batchSize int) {
	if h == nil || h.repo == nil || h.importH == nil || h.cfg == nil || !h.cfg.ShopeeRealtimeOpsEnabled {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if batchSize <= 0 || batchSize > 50 {
		batchSize = 10
	}
	if n, err := h.repo.RecoverStaleReconcileJobs(ctx, 5*time.Minute); err != nil {
		h.logger.Warn("shopee_realtime: recover stale reconcile jobs failed", zap.Error(err))
	} else if n > 0 {
		h.logger.Info("shopee_realtime: recovered stale reconcile jobs", zap.Int64("jobs", n))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := h.ProcessReconcileBatch(ctx, batchSize); err != nil && ctx.Err() == nil {
			h.logger.Warn("shopee_realtime: reconcile batch failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *ShopeeRealtimeHandler) ProcessReconcileBatch(ctx context.Context, batchSize int) (int, error) {
	if h == nil || h.repo == nil || h.importH == nil || h.cfg == nil || !h.cfg.ShopeeRealtimeOpsEnabled {
		return 0, nil
	}
	jobs, err := h.repo.LeaseReconcileJobs(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, job := range jobs {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		if _, err := h.reconcileOrder(ctx, job.ShopID, job.OrderSN, job.Reason, false); err != nil {
			msg := shopeeAPIErrorMessage(err, "reconcile Shopee order ไม่สำเร็จ").Message
			_ = h.repo.MarkReconcileJobFailed(ctx, job.ID, msg)
			_ = h.repo.MarkPushEventsForOrder(ctx, job.ShopID, job.OrderSN, "failed", msg)
			h.notifyShopeeIssue(ctx, job.ShopID, "", "error", "Shopee Realtime reconcile ไม่สำเร็จ", fmt.Sprintf("%s: %s", job.OrderSN, msg), fmt.Sprintf("reconcile_error:%d:%s:%s", job.ShopID, job.OrderSN, time.Now().Format("2006010215")))
			h.logger.Warn("shopee_realtime: reconcile job failed", zap.String("job_id", job.ID), zap.Int64("shop_id", job.ShopID), zap.String("order_sn", job.OrderSN), zap.Error(err))
			continue
		}
		_ = h.repo.MarkReconcileJobDone(ctx, job.ID)
		_ = h.repo.MarkPushEventsForOrder(ctx, job.ShopID, job.OrderSN, "processed", "")
		processed++
	}
	return processed, nil
}

func (h *ShopeeRealtimeHandler) SaveERP(c *gin.Context) {
	h.createDocument(c, "SAVE_TO_ERP")
}

func (h *ShopeeRealtimeHandler) CreateDocument(c *gin.Context) {
	h.createDocument(c, "CREATE_DOCUMENT")
}

func (h *ShopeeRealtimeHandler) createDocument(c *gin.Context, legacyConfirm string) {
	if !h.enabled(c) {
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Confirm != "CREATE_DOCUMENT" && req.Confirm != legacyConfirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณายืนยันด้วย CREATE_DOCUMENT"})
		return
	}

	requestRaw, _ := json.Marshal(req)
	outcome := h.createDocumentForOrder(c.Request.Context(), shopID, orderSN, c.GetString("user_id"), c.GetString("trace_id"), requestRaw)
	c.JSON(outcome.HTTPStatus, outcome.toSinglePayload())
}

func (h *ShopeeRealtimeHandler) createDocumentForOrder(ctx context.Context, shopID int64, orderSN, userID, traceID string, requestRaw json.RawMessage) shopeeCreateDocumentOutcome {
	orderSN = strings.TrimSpace(orderSN)
	out := shopeeCreateDocumentOutcome{ShopID: shopID, OrderSN: orderSN, HTTPStatus: http.StatusOK}
	action, actionState, err := h.repo.StartAction(ctx, shopID, orderSN, "create_document", userID, requestRaw)
	if err != nil {
		h.logger.Warn("shopee_realtime: start create-document action failed", zap.Int64("shop_id", shopID), zap.String("order_sn", orderSN), zap.Error(err))
		out.Status = "failed"
		out.Reason = "เริ่ม action สร้างเอกสารไม่สำเร็จ"
		out.Message = out.Reason
		out.HTTPStatus = http.StatusInternalServerError
		return out
	}
	if actionState == "done" {
		billID := stringPtrValue(action.BillID)
		route := ""
		status := "pending_erp"
		docNo := action.SMLDocNo
		if billID != "" && h.billH != nil && h.billH.billRepo != nil {
			if bill, err := h.billH.billRepo.FindByID(billID); err == nil && bill != nil {
				route = bill.DocumentRoute
				if bill.Status == "sent" {
					status = "sent"
					docNo = stringPtrValue(bill.SMLDocNo)
				} else if bill.Status == "needs_review" {
					status = "needs_review"
				}
			}
		}
		out.Status = "reused"
		out.ERPStatus = status
		out.BillID = billID
		out.BillURL = billURLFromRoute(route, billID)
		out.DocumentRoute = route
		out.DocNo = docNo
		out.Message = "order นี้สร้างเอกสารใน Nexflow แล้ว"
		return out
	}
	if actionState != "started" {
		out.Status = "skipped"
		out.ERPStatus = actionState
		out.Reason = "order นี้กำลังสร้างเอกสารอยู่ กรุณารอสักครู่แล้ว refresh"
		out.Message = out.Reason
		out.HTTPStatus = http.StatusConflict
		return out
	}
	completeAction := func(status, billID, docNo string, payload any, errMsg string) {
		resp, _ := json.Marshal(payload)
		_ = h.repo.CompleteAction(ctx, action.IdempotencyKey, status, billID, docNo, resp, errMsg)
	}

	snap, err := h.reconcileOrder(ctx, shopID, orderSN, "erp_action", false)
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "ดึงรายละเอียดล่าสุดจาก Shopee ไม่สำเร็จ").Message
		completeAction("failed", "", "", gin.H{"error": msg}, msg)
		out.Status = "failed"
		out.Reason = msg
		out.Message = msg
		out.HTTPStatus = http.StatusBadGateway
		return out
	}
	switch strings.ToUpper(snap.OrderStatus) {
	case "UNPAID":
		completeAction("blocked", stringPtrValue(snap.BillID), snap.SMLDocNo, gin.H{"status": "blocked", "reason": "unpaid"}, "order ยังไม่ชำระเงิน")
		out.Status = "skipped"
		out.OrderSN = snap.OrderSN
		out.ERPStatus = snap.ERPStatus
		out.Reason = "order ยังไม่ชำระเงิน จึงยังสร้างเอกสารไม่ได้"
		out.Message = out.Reason
		out.HTTPStatus = http.StatusBadRequest
		return out
	case "CANCELLED", "IN_CANCEL":
		completeAction("blocked", stringPtrValue(snap.BillID), snap.SMLDocNo, gin.H{"status": "blocked", "reason": "cancelled"}, "order ถูกยกเลิกแล้ว")
		out.Status = "skipped"
		out.OrderSN = snap.OrderSN
		out.ERPStatus = snap.ERPStatus
		out.Reason = "order ถูกยกเลิกแล้ว จึงไม่ควรสร้างเอกสาร"
		out.Message = out.Reason
		out.HTTPStatus = http.StatusBadRequest
		return out
	}
	cfg, routeDef, err := h.realtimeSaleConfig(ctx)
	if err != nil {
		msg := err.Error()
		completeAction("blocked", stringPtrValue(snap.BillID), snap.SMLDocNo, gin.H{"status": "blocked", "reason": "route_missing"}, msg)
		out.Status = "skipped"
		out.OrderSN = snap.OrderSN
		out.ERPStatus = snap.ERPStatus
		out.Reason = msg
		out.Message = msg
		out.HTTPStatus = http.StatusBadRequest
		return out
	}
	if snap.BillID == nil || strings.TrimSpace(*snap.BillID) == "" {
		result, err := h.createBillFromRealtimeSnapshot(ctx, snap, cfg, userID, traceID)
		if err != nil {
			msg := result.Message
			if strings.TrimSpace(msg) == "" {
				msg = err.Error()
			}
			completeAction("failed", result.BillID, "", gin.H{"status": "failed", "message": msg}, msg)
			out.Status = "failed"
			out.BillID = result.BillID
			out.Reason = msg
			out.Message = msg
			out.HTTPStatus = http.StatusInternalServerError
			return out
		}
		if strings.TrimSpace(result.BillID) != "" {
			status := "pending_erp"
			if h.billH != nil && h.billH.billRepo != nil {
				if bill, err := h.billH.billRepo.FindByID(result.BillID); err == nil && bill != nil {
					switch bill.Status {
					case "needs_review":
						status = "needs_review"
					case "sent":
						status = "sent"
					}
				}
			}
			_ = h.repo.LinkSnapshotBill(ctx, shopID, orderSN, result.BillID, "", status)
			snap.BillID = &result.BillID
			snap.ERPStatus = status
			snap.DocumentRoute = shopeeImportRoute(cfg)
		}
	}
	if snap.BillID == nil || strings.TrimSpace(*snap.BillID) == "" {
		msg := "สร้างหรือผูก bill จาก Shopee Realtime ไม่สำเร็จ"
		completeAction("failed", "", "", gin.H{"status": "failed", "message": msg}, msg)
		out.Status = "failed"
		out.Reason = msg
		out.Message = msg
		out.HTTPStatus = http.StatusInternalServerError
		return out
	}
	billID := strings.TrimSpace(*snap.BillID)
	if snap.DocumentRoute == "" {
		snap.DocumentRoute = shopeeImportRoute(cfg)
	}
	status := snap.ERPStatus
	if status == "" || status == "pending" {
		status = "pending_erp"
	}
	_ = h.repo.LinkSnapshotBill(ctx, shopID, orderSN, billID, "", status)
	completeAction("done", billID, "", gin.H{"status": status, "bill_id": billID, "message": "created_document"}, "")
	h.publishShopeeRealtimeChanged(ctx, shopID, orderSN, "document_created")
	out.Status = "created"
	out.ERPStatus = status
	out.BillID = billID
	out.BillURL = billURLFromRoute(snap.DocumentRoute, billID)
	out.DocumentRoute = snap.DocumentRoute
	out.Message = "สร้างเอกสารใน Nexflow แล้ว ยังไม่ได้ส่งเข้า SML"
	out.DocNo = ""
	out.Route = shopeeRealtimeRoutePayload(cfg, routeDef)
	return out
}

func normalizeShopeeRealtimeOrderRefs(in []shopeeRealtimeOrderRef) ([]shopeeRealtimeOrderRef, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("กรุณาเลือก order อย่างน้อย 1 รายการ")
	}
	if len(in) > shopeeRealtimeBulkCreateLimit {
		return nil, fmt.Errorf("สร้างเอกสารแบบกลุ่มจำกัดที่ %d order ต่อรอบ", shopeeRealtimeBulkCreateLimit)
	}
	out := make([]shopeeRealtimeOrderRef, 0, len(in))
	seen := map[string]bool{}
	for _, ref := range in {
		orderSN := strings.TrimSpace(ref.OrderSN)
		if ref.ShopID <= 0 || orderSN == "" {
			return nil, fmt.Errorf("orders ต้องมี shop_id และ order_sn ครบ")
		}
		key := fmt.Sprintf("%d:%s", ref.ShopID, orderSN)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, shopeeRealtimeOrderRef{ShopID: ref.ShopID, OrderSN: orderSN})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("กรุณาเลือก order อย่างน้อย 1 รายการ")
	}
	return out, nil
}

func (h *ShopeeRealtimeHandler) previewBulkCreateDocuments(ctx context.Context, refs []shopeeRealtimeOrderRef) ([]shopeeRealtimeBulkOrderResult, []shopeeRealtimeBulkOrderResult, gin.H, string) {
	cfg, routeDef, routeErr := h.realtimeSaleConfig(ctx)
	routeReady := routeErr == nil
	routeMessage := ""
	if routeErr != nil {
		routeMessage = routeErr.Error()
	}
	route := shopeeRealtimeRoutePayload(cfg, routeDef)
	if !routeReady {
		route["ready"] = false
		route["message"] = routeMessage
	} else {
		route["ready"] = true
	}
	signature := ""
	if routeReady {
		signature = shopeeRealtimeRouteSignature(cfg, routeDef)
	}

	ready := []shopeeRealtimeBulkOrderResult{}
	skipped := []shopeeRealtimeBulkOrderResult{}
	for _, ref := range refs {
		snap, err := h.repo.FindSnapshot(ctx, ref.ShopID, ref.OrderSN)
		if err != nil {
			reason := "โหลด order ไม่สำเร็จ"
			if err == sql.ErrNoRows {
				reason = "ไม่พบ order ใน Shopee Realtime"
			}
			skipped = append(skipped, shopeeRealtimeBulkOrderResult{
				ShopID: ref.ShopID, OrderSN: ref.OrderSN, Status: "skipped", Reason: reason, Message: reason,
			})
			continue
		}
		row := bulkRowFromSnapshot(snap)
		if reason := bulkCreateDisabledReason(snap, routeReady, routeMessage); reason != "" {
			row.Status = "skipped"
			row.Reason = reason
			row.Message = reason
			skipped = append(skipped, row)
			continue
		}
		row.Status = "ready"
		row.Message = "พร้อมสร้างเอกสาร"
		ready = append(ready, row)
	}
	return ready, skipped, route, signature
}

func bulkRowFromSnapshot(snap *models.ShopeeOrderSnapshot) shopeeRealtimeBulkOrderResult {
	if snap == nil {
		return shopeeRealtimeBulkOrderResult{}
	}
	billID := stringPtrValue(snap.BillID)
	return shopeeRealtimeBulkOrderResult{
		ShopID:        snap.ShopID,
		OrderSN:       snap.OrderSN,
		BuyerUsername: snap.BuyerUsername,
		OrderStatus:   snap.OrderStatus,
		ERPStatus:     snap.ERPStatus,
		TotalAmount:   snap.TotalAmount,
		ItemCount:     snap.ItemCount,
		BillID:        billID,
		BillURL:       billURLFromRoute(snap.DocumentRoute, billID),
		DocumentRoute: snap.DocumentRoute,
		DocNo:         snap.SMLDocNo,
	}
}

func bulkCreateDisabledReason(snap *models.ShopeeOrderSnapshot, routeReady bool, routeMessage string) string {
	if snap == nil {
		return "ไม่พบ order ใน Shopee Realtime"
	}
	if !routeReady {
		if strings.TrimSpace(routeMessage) != "" {
			return routeMessage
		}
		return "ยังไม่ได้ตั้งค่าเส้นทาง Shopee Realtime"
	}
	if snap.BillID != nil && strings.TrimSpace(*snap.BillID) != "" {
		return "สร้างเอกสารแล้ว"
	}
	switch strings.ToUpper(strings.TrimSpace(snap.OrderStatus)) {
	case "UNPAID":
		return "order ยังไม่ชำระเงิน"
	case "CANCELLED", "IN_CANCEL":
		return "order ถูกยกเลิกแล้ว"
	}
	switch strings.TrimSpace(snap.ERPStatus) {
	case "", "pending", "failed":
		return ""
	default:
		return "สถานะ ERP ไม่พร้อมสร้างเอกสาร"
	}
}

func (h *ShopeeRealtimeHandler) realtimeRouteSignature(ctx context.Context) string {
	cfg, def, err := h.realtimeSaleConfig(ctx)
	if err != nil {
		return ""
	}
	return shopeeRealtimeRouteSignature(cfg, def)
}

func shopeeRealtimeRouteSignature(cfg ShopeeConfigRequest, def *models.ChannelDefault) string {
	parts := []string{
		"shopee_realtime",
		"sale",
		shopeeImportRoute(cfg),
		strings.TrimSpace(cfg.Endpoint),
		strings.TrimSpace(cfg.DocFormat),
		strings.TrimSpace(cfg.CustCode),
		strings.TrimSpace(cfg.SaleCode),
		strings.TrimSpace(cfg.BranchCode),
		strings.TrimSpace(cfg.WHCode),
		strings.TrimSpace(cfg.ShelfCode),
		strings.TrimSpace(cfg.UnitCode),
		strconv.Itoa(cfg.VATType),
		fmt.Sprintf("%.4f", cfg.VATRate),
		strings.TrimSpace(cfg.DocTime),
	}
	if def != nil {
		parts = append(parts,
			strings.TrimSpace(def.Endpoint),
			strings.TrimSpace(def.DocFormatCode),
			strings.TrimSpace(def.PartyCode),
		)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func shopeeRealtimeRoutePayload(cfg ShopeeConfigRequest, def *models.ChannelDefault) gin.H {
	out := gin.H{
		"channel":         "shopee_realtime",
		"bill_type":       "sale",
		"document_route":  shopeeImportRoute(cfg),
		"endpoint":        strings.TrimSpace(cfg.Endpoint),
		"doc_format_code": strings.TrimSpace(cfg.DocFormat),
		"destination":     shopeeImportDocumentName(cfg),
	}
	if def != nil {
		out["endpoint"] = strings.TrimSpace(def.Endpoint)
		out["doc_format_code"] = strings.TrimSpace(def.DocFormatCode)
	}
	return out
}

func (o shopeeCreateDocumentOutcome) toSinglePayload() gin.H {
	status := o.ERPStatus
	if strings.TrimSpace(status) == "" {
		status = o.Status
	}
	payload := gin.H{
		"status":         status,
		"bill_id":        o.BillID,
		"bill_url":       o.BillURL,
		"document_route": o.DocumentRoute,
		"doc_no":         o.DocNo,
		"message":        o.Message,
	}
	if strings.TrimSpace(o.Reason) != "" && o.HTTPStatus >= 400 {
		payload["error"] = o.Reason
	}
	if o.Route != nil {
		payload["route"] = o.Route
	}
	return payload
}

func (o shopeeCreateDocumentOutcome) toBulkResult() shopeeRealtimeBulkOrderResult {
	reason := o.Reason
	if reason == "" && o.HTTPStatus >= 400 {
		reason = o.Message
	}
	return shopeeRealtimeBulkOrderResult{
		ShopID:        o.ShopID,
		OrderSN:       o.OrderSN,
		ERPStatus:     o.ERPStatus,
		BillID:        o.BillID,
		BillURL:       o.BillURL,
		DocumentRoute: o.DocumentRoute,
		DocNo:         o.DocNo,
		Status:        o.Status,
		Reason:        reason,
		Message:       o.Message,
	}
}

func (h *ShopeeRealtimeHandler) createBillFromRealtimeSnapshot(ctx context.Context, snap *models.ShopeeOrderSnapshot, cfg ShopeeConfigRequest, userID, traceID string) (ConfirmResult, error) {
	if h == nil || h.importH == nil || snap == nil {
		return ConfirmResult{Message: "Shopee Realtime handler ยังไม่พร้อม"}, fmt.Errorf("shopee realtime handler is not ready")
	}
	var detail shopeeapi.OrderDetail
	if len(snap.RawDetail) == 0 {
		return ConfirmResult{OrderID: snap.OrderSN, Message: "snapshot ไม่มี raw_detail จาก Shopee"}, fmt.Errorf("snapshot raw_detail is empty")
	}
	if err := json.Unmarshal(snap.RawDetail, &detail); err != nil {
		return ConfirmResult{OrderID: snap.OrderSN, Message: "อ่านรายละเอียด Shopee order ไม่สำเร็จ: " + err.Error()}, err
	}
	if strings.TrimSpace(detail.OrderSN) == "" {
		detail.OrderSN = snap.OrderSN
	}
	orders, warnings := h.importH.shopeeAPIOrdersToPreview([]shopeeapi.OrderDetail{detail})
	if len(orders) == 0 {
		msg := "รายละเอียด Shopee order ยังสร้าง bill ไม่ได้"
		if len(warnings) > 0 {
			msg = strings.Join(warnings, "; ")
		}
		return ConfirmResult{OrderID: snap.OrderSN, Message: msg}, fmt.Errorf("no importable shopee order detail")
	}
	conn, err := h.connectionForShop(ctx, snap.ShopID)
	if err != nil {
		return ConfirmResult{OrderID: snap.OrderSN, Message: "โหลดร้าน Shopee ไม่สำเร็จ: " + err.Error()}, err
	}
	order := orders[0]
	order.ShopeeShopID = strconv.FormatInt(conn.ShopID, 10)
	order.ShopeeConnectionID = conn.ID
	order.ShopeeShopLabel = conn.DisplayLabel()
	var userIDPtr *string
	if strings.TrimSpace(userID) != "" {
		userIDPtr = &userID
	}
	return h.importH.CreateBillFromShopeeOrder(ctx, order, ShopeeBillCreateOptions{
		Config:     cfg,
		SourceFlow: "shopee_realtime",
		Connection: conn,
		UserID:     userIDPtr,
		TraceID:    traceID,
		StartedAt:  time.Now(),
	})
}

func (h *ShopeeRealtimeHandler) realtimeSaleConfig(ctx context.Context) (ShopeeConfigRequest, *models.ChannelDefault, error) {
	cfg := h.importH.CurrentShopeeSaleConfigForChannel("shopee_realtime")
	if h.importH.channelDefaults == nil {
		return cfg, nil, fmt.Errorf("ยังไม่ได้ตั้งค่าเส้นทาง Shopee Realtime ใน /settings/channels")
	}
	def, err := h.importH.channelDefaults.Get("shopee_realtime", "sale")
	if err != nil {
		return cfg, nil, fmt.Errorf("โหลดเส้นทาง Shopee Realtime ไม่สำเร็จ: %w", err)
	}
	if def == nil {
		return cfg, nil, fmt.Errorf("ยังไม่ได้ตั้งค่า Shopee Realtime / sale ในหน้าเส้นทางเอกสาร SML")
	}
	if strings.TrimSpace(def.Endpoint) == "" || strings.TrimSpace(def.DocFormatCode) == "" {
		return cfg, def, fmt.Errorf("กรุณาตั้งปลายทางและ doc format ของ Shopee Realtime ก่อนสร้างเอกสาร")
	}
	_ = ctx
	return cfg, def, nil
}

func billURLFromRoute(route, billID string) string {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(route)) {
	case "saleinvoice":
		return "/sale-invoices/" + url.PathEscape(billID)
	case "saleorder":
		return "/sales-orders/" + url.PathEscape(billID)
	default:
		return "/bills/" + url.PathEscape(billID)
	}
}

func (h *ShopeeRealtimeHandler) ShippingParameters(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	snap, err := h.repo.FindSnapshot(c.Request.Context(), shopID, orderSN)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ order ใน Shopee Realtime"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด order ไม่สำเร็จ"})
		return
	}
	if !canCheckShippingParameters(snap) {
		c.JSON(http.StatusBadRequest, gin.H{"error": shippingBlockedReason(snap)})
		return
	}
	conn, err := h.importH.ensureShopeeAPIAccessToken(c.Request.Context(), snapshotConnectionID(snap))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.importH.shopeeAPIClient().GetShippingParameter(c.Request.Context(), conn.AccessToken, conn.ShopID, snap.OrderSN, snap.PackageNumber)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": shopeeAPIErrorMessage(err, "ตรวจเงื่อนไขจัดส่ง Shopee ไม่สำเร็จ").Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": resp.Response})
}

func (h *ShopeeRealtimeHandler) shippingActionsDisabled(c *gin.Context) bool {
	if h == nil || h.cfg == nil || h.cfg.ShopeeShippingActionsEnabled {
		return false
	}
	c.JSON(http.StatusForbidden, gin.H{
		"error":  "การจัดส่งและใบปะหน้าทำใน Seller Center, Nexflow ติดตามสถานะกลับมา",
		"reason": "shipping_actions_disabled",
	})
	return true
}

func (h *ShopeeRealtimeHandler) ShipOrder(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if h.shippingActionsDisabled(c) {
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	var req shippingOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง: " + err.Error()})
		return
	}
	if req.Confirm != "SHIP_ORDER" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณายืนยันด้วย SHIP_ORDER"})
		return
	}
	requestRaw, _ := json.Marshal(req)
	action, actionState, err := h.repo.StartAction(c.Request.Context(), shopID, orderSN, "ship_order", c.GetString("user_id"), requestRaw)
	if err != nil {
		h.logger.Warn("shopee_realtime: start ship action failed", zap.Int64("shop_id", shopID), zap.String("order_sn", orderSN), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เริ่ม action จัดส่ง Shopee ไม่สำเร็จ"})
		return
	}
	if actionState == "done" {
		c.JSON(http.StatusOK, gin.H{"message": "order นี้เคยส่งคำสั่งจัดส่งให้ Shopee แล้ว ระบบจะรอ push/detail sync เพื่อยืนยันสถานะ", "status": "requested"})
		return
	}
	if actionState != "started" {
		c.JSON(http.StatusConflict, gin.H{"error": "order นี้กำลังส่งคำสั่งจัดส่งอยู่ กรุณารอสักครู่แล้ว refresh", "status": actionState})
		return
	}
	completeAction := func(status string, payload any, errMsg string) {
		resp, _ := json.Marshal(payload)
		_ = h.repo.CompleteAction(c.Request.Context(), action.IdempotencyKey, status, stringPtrValue(action.BillID), action.SMLDocNo, resp, errMsg)
	}
	snap, err := h.repo.FindSnapshot(c.Request.Context(), shopID, orderSN)
	if err != nil {
		if err == sql.ErrNoRows {
			completeAction("failed", gin.H{"error": "snapshot not found"}, "ไม่พบ order ใน Shopee Realtime")
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ order ใน Shopee Realtime"})
			return
		}
		completeAction("failed", gin.H{"error": "load snapshot failed"}, "โหลด order ไม่สำเร็จ")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด order ไม่สำเร็จ"})
		return
	}
	if !canCheckShippingParameters(snap) {
		completeAction("blocked", gin.H{"status": "blocked", "reason": shippingBlockedReason(snap)}, shippingBlockedReason(snap))
		c.JSON(http.StatusBadRequest, gin.H{"error": shippingBlockedReason(snap)})
		return
	}
	if len(req.Pickup) == 0 && len(req.Dropoff) == 0 && len(req.NonIntegrated) == 0 {
		completeAction("blocked", gin.H{"status": "blocked", "reason": "shipping method missing"}, "ต้องเลือก pickup, dropoff หรือ non_integrated จาก shipping parameter ก่อนจัดส่ง")
		c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องเลือก pickup, dropoff หรือ non_integrated จาก shipping parameter ก่อนจัดส่ง"})
		return
	}
	conn, err := h.importH.ensureShopeeAPIAccessToken(c.Request.Context(), snapshotConnectionID(snap))
	if err != nil {
		completeAction("failed", gin.H{"error": err.Error()}, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	packageNumber := strings.TrimSpace(req.PackageNumber)
	if packageNumber == "" {
		packageNumber = snap.PackageNumber
	}
	params, err := h.importH.shopeeAPIClient().GetShippingParameter(c.Request.Context(), conn.AccessToken, conn.ShopID, snap.OrderSN, packageNumber)
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "Shopee ยังไม่พร้อมให้จัดส่ง order นี้").Message
		completeAction("blocked", gin.H{"error": msg}, msg)
		c.JSON(http.StatusBadGateway, gin.H{"error": shopeeAPIErrorMessage(err, "Shopee ยังไม่พร้อมให้จัดส่ง order นี้").Message})
		return
	}
	if err := validateShippingSelection(params, req); err != nil {
		msg := err.Error()
		completeAction("blocked", gin.H{"status": "blocked", "reason": msg}, msg)
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	if reason, msg := validateDropoffShippingGuard(params, req, h.cfg != nil && h.cfg.ShopeeAdvancedDropoffEnabled); reason != "" {
		completeAction("blocked", gin.H{"status": "blocked", "reason": reason, "message": msg}, msg)
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "reason": reason})
		return
	}
	resp, err := h.importH.shopeeAPIClient().ShipOrder(c.Request.Context(), conn.AccessToken, conn.ShopID, shopeeapi.ShipOrderRequest{
		OrderSN:       snap.OrderSN,
		PackageNumber: packageNumber,
		Pickup:        req.Pickup,
		Dropoff:       req.Dropoff,
		NonIntegrated: req.NonIntegrated,
	})
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "สั่งจัดส่ง Shopee ไม่สำเร็จ").Message
		completeAction("failed", gin.H{"error": msg}, msg)
		h.notifySnapshotIssue(c.Request.Context(), snap, "error", "จัดส่ง Shopee ไม่สำเร็จ", msg, "ship_failed")
		c.JSON(http.StatusBadGateway, gin.H{"error": msg})
		return
	}
	completeAction("done", gin.H{"message": "ship_order requested", "data": resp.Response}, "")
	latest, recErr := h.reconcileShippingFromShopee(c.Request.Context(), shopID, orderSN, "ship_order_requested", false, false)
	if recErr != nil {
		h.logger.Warn("shopee_realtime: shipping reconcile after ship_order failed", zap.Int64("shop_id", shopID), zap.String("order_sn", orderSN), zap.Error(recErr))
	}
	h.publishShopeeRealtimeChanged(c.Request.Context(), shopID, orderSN, "ship_order_requested")
	payload := gin.H{
		"message": "ส่งคำสั่งจัดส่งให้ Shopee แล้ว ระบบจะรอ push/detail sync เพื่อยืนยันสถานะ",
		"data":    resp.Response,
	}
	if latest != nil {
		payload["snapshot"] = latest
		payload["tracking"] = shippingTrackingView(latest)
	}
	c.JSON(http.StatusOK, payload)
}

func (h *ShopeeRealtimeHandler) ReconcileShipping(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	silent := parseBoolQuery(c.Query("silent"))
	source := "manual_refresh"
	reason := "manual_shipping_refresh"
	if silent {
		source = "dialog_refresh"
		reason = "dialog_shipping_refresh"
	}
	requestRaw, _ := json.Marshal(gin.H{"source": source, "silent": silent})
	snap, err := h.reconcileShippingFromShopee(c.Request.Context(), shopID, orderSN, reason, silent, silent)
	if err != nil {
		msg := shopeeAPIErrorMessage(err, "รีเฟรชสถานะจัดส่งจาก Shopee ไม่สำเร็จ").Message
		resp, _ := json.Marshal(gin.H{"error": msg})
		_ = h.repo.RecordAction(c.Request.Context(), shopID, orderSN, "reconcile_shipping", c.GetString("user_id"), "failed", requestRaw, resp, msg)
		if silent && isCriticalShopeeAccessError(err) {
			h.notifyShopeeIssue(c.Request.Context(), shopID, "", "error", "Shopee Realtime ตรวจสถานะไม่สำเร็จ", msg, fmt.Sprintf("shipping_reconcile_access:%d", shopID))
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": msg})
		return
	}
	tracking := shippingTrackingView(snap)
	resp, _ := json.Marshal(gin.H{"tracking": tracking})
	_ = h.repo.RecordAction(c.Request.Context(), shopID, orderSN, "reconcile_shipping", c.GetString("user_id"), "done", requestRaw, resp, "")
	if !silent {
		h.publishShopeeRealtimeChanged(c.Request.Context(), shopID, orderSN, "shipping_reconciled")
	}
	c.JSON(http.StatusOK, gin.H{"data": snap, "tracking": tracking})
}

func (h *ShopeeRealtimeHandler) Tracking(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	snap, err := h.repo.FindSnapshot(c.Request.Context(), shopID, orderSN)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ order ใน Shopee Realtime"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดข้อมูลจัดส่งไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": shippingTrackingView(snap), "snapshot": snap})
}

func (h *ShopeeRealtimeHandler) Timeline(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	snap, err := h.repo.FindSnapshot(c.Request.Context(), shopID, orderSN)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ order ใน Shopee Realtime"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด order timeline ไม่สำเร็จ"})
		return
	}
	events, err := h.repo.OrderTimeline(c.Request.Context(), shopID, orderSN)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("shopee_realtime: order timeline failed", zap.Int64("shop_id", shopID), zap.String("order_sn", orderSN), zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด order timeline ไม่สำเร็จ"})
		return
	}
	statusTimeline, erpMilestones, err := h.repo.OrderLifecycleTimeline(c.Request.Context(), snap)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("shopee_realtime: order lifecycle timeline failed", zap.Int64("shop_id", shopID), zap.String("order_sn", orderSN), zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด order timeline ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"snapshot":        snap,
		"status_timeline": statusTimeline,
		"erp_milestones":  erpMilestones,
		"events":          events,
	})
}

func (h *ShopeeRealtimeHandler) ShippingDocumentCreate(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if h.shippingActionsDisabled(c) {
		return
	}
	doc, ok := h.shippingDocumentContext(c, "shipping_document_create")
	if !ok {
		return
	}
	client := h.importH.shopeeAPIClient()
	param, err := client.GetShippingDocumentParameter(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_create", err, nil)
		return
	}
	documentType := pickShippingDocumentType(param.Response)
	if documentType == "" {
		msg := "Shopee ยังไม่ส่งประเภทใบปะหน้าที่สร้างได้ กรุณาพิมพ์จาก Seller Center"
		resp, _ := json.Marshal(gin.H{"status": "seller_center_required", "message": msg, "parameter": param.Response})
		_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_create", c.GetString("user_id"), "blocked", nil, resp, msg)
		c.JSON(http.StatusOK, gin.H{
			"status":    "seller_center_required",
			"message":   msg,
			"parameter": param.Response,
			"tracking":  doc.tracking,
		})
		return
	}
	create, err := client.CreateShippingDocument(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber, documentType, doc.snap.TrackingNumber)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_create", err, param.Response)
		return
	}
	result, err := client.GetShippingDocumentResult(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber, documentType)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_create", err, param.Response)
		return
	}
	status, message := shippingDocumentResultStatus(result.Response)
	payload := gin.H{
		"status":        status,
		"message":       message,
		"document_type": documentType,
		"parameter":     param.Response,
		"create":        create.Response,
		"result":        result.Response,
		"tracking":      doc.tracking,
	}
	resp, _ := json.Marshal(payload)
	_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_create", c.GetString("user_id"), "done", nil, resp, "")
	c.JSON(http.StatusOK, payload)
}

func (h *ShopeeRealtimeHandler) ShippingDocumentResult(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if h.shippingActionsDisabled(c) {
		return
	}
	doc, ok := h.shippingDocumentContext(c, "shipping_document_result")
	if !ok {
		return
	}
	param, err := h.importH.shopeeAPIClient().GetShippingDocumentParameter(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_result", err, nil)
		return
	}
	documentType := pickShippingDocumentType(param.Response)
	if documentType == "" {
		msg := "Shopee ยังไม่ส่งประเภทใบปะหน้าที่ตรวจผลได้ กรุณาพิมพ์จาก Seller Center"
		resp, _ := json.Marshal(gin.H{"status": "seller_center_required", "message": msg, "parameter": param.Response})
		_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_result", c.GetString("user_id"), "blocked", nil, resp, msg)
		c.JSON(http.StatusOK, gin.H{"status": "seller_center_required", "message": msg, "parameter": param.Response, "tracking": doc.tracking})
		return
	}
	result, err := h.importH.shopeeAPIClient().GetShippingDocumentResult(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber, documentType)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_result", err, param.Response)
		return
	}
	status, message := shippingDocumentResultStatus(result.Response)
	payload := gin.H{"status": status, "message": message, "document_type": documentType, "result": result.Response, "tracking": doc.tracking}
	resp, _ := json.Marshal(payload)
	_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_result", c.GetString("user_id"), "done", nil, resp, "")
	c.JSON(http.StatusOK, gin.H{
		"status":        status,
		"message":       message,
		"document_type": documentType,
		"result":        result.Response,
		"tracking":      doc.tracking,
	})
}

func (h *ShopeeRealtimeHandler) ShippingDocumentDownload(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if h.shippingActionsDisabled(c) {
		return
	}
	doc, ok := h.shippingDocumentContext(c, "shipping_document_download")
	if !ok {
		return
	}
	param, err := h.importH.shopeeAPIClient().GetShippingDocumentParameter(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_download", err, nil)
		return
	}
	documentType := pickShippingDocumentType(param.Response)
	if documentType == "" {
		msg := "Shopee ยังไม่ส่งประเภทใบปะหน้าที่ดาวน์โหลดได้ กรุณาพิมพ์จาก Seller Center"
		resp, _ := json.Marshal(gin.H{"status": "seller_center_required", "message": msg, "parameter": param.Response})
		_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_download", c.GetString("user_id"), "blocked", nil, resp, msg)
		c.JSON(http.StatusOK, gin.H{"status": "seller_center_required", "message": msg, "parameter": param.Response, "tracking": doc.tracking})
		return
	}
	result, err := h.importH.shopeeAPIClient().GetShippingDocumentResult(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber, documentType)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_download", err, param.Response)
		return
	}
	status, _ := shippingDocumentResultStatus(result.Response)
	if status != "ready" {
		msg := "ใบปะหน้ายังสร้างไม่เสร็จ กรุณาลองใหม่อีกครั้ง หรือพิมพ์จาก Seller Center"
		resp, _ := json.Marshal(gin.H{"status": status, "message": msg, "result": result.Response})
		_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_download", c.GetString("user_id"), "blocked", nil, resp, msg)
		c.JSON(http.StatusOK, gin.H{"status": status, "message": msg, "result": result.Response, "tracking": doc.tracking})
		return
	}
	data, contentType, err := h.importH.shopeeAPIClient().DownloadShippingDocument(c.Request.Context(), doc.conn.AccessToken, doc.conn.ShopID, doc.snap.OrderSN, doc.snap.PackageNumber, documentType)
	if err != nil {
		h.replyShippingDocumentFallback(c, doc, "shipping_document_download", err, param.Response)
		return
	}
	if contentType == "" {
		contentType = "application/pdf"
	}
	resp, _ := json.Marshal(gin.H{"status": "downloaded", "content_type": contentType, "bytes": len(data)})
	_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, "shipping_document_download", c.GetString("user_id"), "done", nil, resp, "")
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="shopee-label-%s.pdf"`, safeFilename(doc.orderSN)))
	c.Data(http.StatusOK, contentType, data)
}

type shippingDocumentContext struct {
	shopID   int64
	orderSN  string
	snap     *models.ShopeeOrderSnapshot
	conn     *ShopeeAPIConnection
	tracking gin.H
}

func (h *ShopeeRealtimeHandler) shippingDocumentContext(c *gin.Context, action string) (shippingDocumentContext, bool) {
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return shippingDocumentContext{}, false
	}
	snap, err := h.repo.FindSnapshot(c.Request.Context(), shopID, orderSN)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบ order ใน Shopee Realtime"})
			return shippingDocumentContext{}, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดข้อมูลจัดส่งไม่สำเร็จ"})
		return shippingDocumentContext{}, false
	}
	if !shipmentStarted(snap) {
		msg := "ยังไม่มีข้อมูล shipment จาก Shopee กรุณาจัดส่งจาก Seller Center หรือรอ Shopee ยืนยันสถานะก่อนพิมพ์ใบปะหน้า"
		tracking := shippingTrackingView(snap)
		resp, _ := json.Marshal(gin.H{"status": "not_ready", "message": msg, "tracking": tracking})
		_ = h.repo.RecordAction(c.Request.Context(), shopID, orderSN, action, c.GetString("user_id"), "blocked", nil, resp, msg)
		c.JSON(http.StatusOK, gin.H{"status": "not_ready", "message": msg, "tracking": tracking})
		return shippingDocumentContext{}, false
	}
	conn, err := h.importH.ensureShopeeAPIAccessToken(c.Request.Context(), snapshotConnectionID(snap))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return shippingDocumentContext{}, false
	}
	return shippingDocumentContext{
		shopID:   shopID,
		orderSN:  orderSN,
		snap:     snap,
		conn:     conn,
		tracking: shippingTrackingView(snap),
	}, true
}

func (h *ShopeeRealtimeHandler) replyShippingDocumentFallback(c *gin.Context, doc shippingDocumentContext, action string, err error, parameter json.RawMessage) {
	msg := "ยังใช้ API พิมพ์ใบปะหน้าพัสดุไม่ได้ในรอบนี้ กรุณาพิมพ์จาก Seller Center"
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
		lower := strings.ToLower(errMsg)
		if strings.Contains(lower, "403") || strings.Contains(lower, "permission") || strings.Contains(lower, "access") {
			msg = "Shopee ยังไม่เปิดสิทธิ์ API ใบปะหน้าพัสดุให้แอปนี้ กรุณาพิมพ์จาก Seller Center"
		}
	}
	payload := gin.H{
		"status":   "seller_center_required",
		"message":  msg,
		"tracking": doc.tracking,
	}
	if len(parameter) > 0 {
		payload["parameter"] = parameter
	}
	resp, _ := json.Marshal(payload)
	_ = h.repo.RecordAction(c.Request.Context(), doc.shopID, doc.orderSN, action, c.GetString("user_id"), "blocked", nil, resp, errMsg)
	c.JSON(http.StatusOK, payload)
}

func (h *ShopeeRealtimeHandler) reconcileShippingFromShopee(ctx context.Context, shopID int64, orderSN, reason string, suppressNewOrderNotifications bool, silent bool) (*models.ShopeeOrderSnapshot, error) {
	conn, err := h.connectionForShop(ctx, shopID)
	if err != nil {
		return nil, err
	}
	before, beforeErr := h.repo.FindSnapshot(ctx, shopID, orderSN)
	if beforeErr != nil && beforeErr != sql.ErrNoRows {
		return nil, beforeErr
	}
	detail, err := h.importH.shopeeAPIClient().GetOrderDetail(ctx, conn.AccessToken, conn.ShopID, []string{strings.TrimSpace(orderSN)}, shopeeAPIOrderDetailFields())
	if err != nil {
		return nil, err
	}
	if len(detail.Response.OrderList) == 0 {
		return nil, fmt.Errorf("Shopee ไม่ส่งรายละเอียด order %s กลับมา", strings.TrimSpace(orderSN))
	}
	order := detail.Response.OrderList[0]
	packageNumber := orderPackageNumber(order.PackageList)
	if strings.TrimSpace(packageNumber) == "" && before != nil {
		packageNumber = before.PackageNumber
	}
	var tracking *shopeeapi.TrackingNumberResponse
	var trackingInfo *shopeeapi.TrackingInfoResponse
	var trackingErrs []string
	if strings.TrimSpace(packageNumber) != "" {
		if out, err := h.importH.shopeeAPIClient().GetTrackingNumber(ctx, conn.AccessToken, conn.ShopID, order.OrderSN, packageNumber); err == nil {
			tracking = out
		} else {
			trackingErrs = append(trackingErrs, shopeeAPIErrorMessage(err, "ดึง tracking number ไม่สำเร็จ").Message)
		}
		if out, err := h.importH.shopeeAPIClient().GetTrackingInfo(ctx, conn.AccessToken, conn.ShopID, order.OrderSN, packageNumber); err == nil {
			trackingInfo = out
		} else {
			trackingErrs = append(trackingErrs, shopeeAPIErrorMessage(err, "ดึง timeline จัดส่งไม่สำเร็จ").Message)
		}
	}
	applyShippingReconcileToDetail(&order, packageNumber, tracking, trackingInfo)
	after, err := h.repo.UpsertSnapshotFromDetail(ctx, repository.ShopeeSnapshotUpsert{
		ConnectionID: conn.ID,
		ShopID:       conn.ShopID,
		ShopLabel:    conn.DisplayLabel(),
		Detail:       order,
		Source:       "shipping",
	})
	if err != nil {
		return nil, err
	}
	if tracking != nil || trackingInfo != nil {
		after, err = h.repo.MergeSnapshotShippingMetadata(ctx, conn.ShopID, order.OrderSN, tracking, trackingInfo)
		if err != nil {
			return nil, err
		}
	}
	if beforeErr == sql.ErrNoRows {
		before = nil
	}
	if !silent {
		h.notifySnapshotChange(ctx, before, after, suppressNewOrderNotifications)
	}
	if after != nil && len(trackingErrs) > 0 {
		after.LastError = strings.Join(trackingErrs, "; ")
	}
	_ = reason
	return after, nil
}

func parseBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isCriticalShopeeAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"token", "access", "auth", "authorize", "authorization", "permission", "403", "401"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func orderPackageNumber(packages []shopeeapi.OrderPackage) string {
	for _, p := range packages {
		if number := strings.TrimSpace(p.PackageNumber); number != "" {
			return number
		}
	}
	return ""
}

func applyShippingReconcileToDetail(detail *shopeeapi.OrderDetail, packageNumber string, tracking *shopeeapi.TrackingNumberResponse, info *shopeeapi.TrackingInfoResponse) {
	if detail == nil {
		return
	}
	trackingNumber := ""
	if tracking != nil {
		trackingNumber = strings.TrimSpace(tracking.Response.TrackingNumber)
	}
	logisticsStatus := ""
	if info != nil {
		logisticsStatus = strings.TrimSpace(info.Response.LogisticsStatus)
		if strings.TrimSpace(packageNumber) == "" {
			packageNumber = strings.TrimSpace(info.Response.PackageNumber)
		}
	}
	if trackingNumber != "" {
		detail.TrackingNumber = trackingNumber
	}
	if strings.TrimSpace(packageNumber) == "" && len(detail.PackageList) > 0 {
		packageNumber = strings.TrimSpace(detail.PackageList[0].PackageNumber)
	}
	if strings.TrimSpace(packageNumber) == "" && (trackingNumber != "" || logisticsStatus != "") {
		detail.PackageList = append(detail.PackageList, shopeeapi.OrderPackage{})
	}
	for i := range detail.PackageList {
		if strings.TrimSpace(packageNumber) != "" && strings.TrimSpace(detail.PackageList[i].PackageNumber) != "" && strings.TrimSpace(detail.PackageList[i].PackageNumber) != strings.TrimSpace(packageNumber) {
			continue
		}
		if strings.TrimSpace(detail.PackageList[i].PackageNumber) == "" {
			detail.PackageList[i].PackageNumber = strings.TrimSpace(packageNumber)
		}
		if trackingNumber != "" {
			detail.PackageList[i].TrackingNumber = trackingNumber
		}
		if logisticsStatus != "" {
			detail.PackageList[i].LogisticsStatus = logisticsStatus
		}
		return
	}
}

func shippingTrackingView(snap *models.ShopeeOrderSnapshot) gin.H {
	if snap == nil {
		return gin.H{}
	}
	external := shipmentStarted(snap) && strings.TrimSpace(snap.ShipActionStatus) != "done"
	return gin.H{
		"order_sn":           snap.OrderSN,
		"order_status":       snap.OrderStatus,
		"erp_status":         snap.ERPStatus,
		"package_number":     snap.PackageNumber,
		"logistics_status":   snap.LogisticsStatus,
		"tracking_number":    snap.TrackingNumber,
		"shipping_carrier":   snap.ShippingCarrier,
		"checkout_carrier":   snap.CheckoutCarrier,
		"ship_action_status": snap.ShipActionStatus,
		"external_shipment":  external,
		"timeline":           snap.ShippingTracking,
	}
}

func shipmentStarted(snap *models.ShopeeOrderSnapshot) bool {
	if snap == nil {
		return false
	}
	if strings.TrimSpace(snap.TrackingNumber) != "" {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(snap.LogisticsStatus)) {
	case "LOGISTICS_REQUEST_CREATED", "LOGISTICS_PICKUP_DONE", "LOGISTICS_DELIVERY_DONE", "LOGISTICS_DELIVERY_FAILED", "LOGISTICS_REQUEST_CANCELED":
		return true
	default:
		return false
	}
}

func pickShippingDocumentType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var body interface{}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return ""
	}
	keys := []string{
		"suggest_shipping_document_type",
		"suggested_shipping_document_type",
		"recommend_shipping_document_type",
		"recommended_shipping_document_type",
		"shipping_document_type",
	}
	for _, key := range keys {
		if v := findStringByKey(body, key); v != "" {
			return v
		}
	}
	for _, key := range []string{"selectable_shipping_document_type", "available_shipping_document_type"} {
		if v := findFirstStringInArrayByKey(body, key); v != "" {
			return v
		}
	}
	return ""
}

func shippingDocumentResultStatus(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "processing", "Shopee รับคำสั่งสร้างใบปะหน้าแล้ว กำลังรอผลลัพธ์"
	}
	var body interface{}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return "processing", "Shopee รับคำสั่งสร้างใบปะหน้าแล้ว แต่ผลลัพธ์ยังอ่านไม่ได้"
	}
	status := strings.ToUpper(findStringByKey(body, "status"))
	failError := findStringByKey(body, "fail_error")
	failMessage := findStringByKey(body, "fail_message")
	if failError != "" || failMessage != "" {
		msg := strings.TrimSpace(strings.Join([]string{failError, failMessage}, " "))
		return "seller_center_required", "Shopee ยังสร้างใบปะหน้าผ่าน API ไม่สำเร็จ: " + strings.TrimSpace(msg)
	}
	switch status {
	case "READY", "SUCCESS", "DONE", "COMPLETED", "AVAILABLE":
		return "ready", "ใบปะหน้าพร้อมดาวน์โหลดจาก Shopee"
	case "FAILED", "ERROR":
		return "seller_center_required", "Shopee สร้างใบปะหน้าผ่าน API ไม่สำเร็จ กรุณาพิมพ์จาก Seller Center"
	default:
		return "processing", "Shopee รับคำสั่งสร้างใบปะหน้าแล้ว กำลังรอผลลัพธ์"
	}
}

func findStringByKey(v interface{}, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch node := v.(type) {
	case map[string]interface{}:
		for k, child := range node {
			if strings.ToLower(strings.TrimSpace(k)) == key {
				if s := stringFromJSONValue(child); s != "" {
					return s
				}
			}
		}
		for _, child := range node {
			if s := findStringByKey(child, key); s != "" {
				return s
			}
		}
	case []interface{}:
		for _, child := range node {
			if s := findStringByKey(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func findFirstStringInArrayByKey(v interface{}, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch node := v.(type) {
	case map[string]interface{}:
		for k, child := range node {
			if strings.ToLower(strings.TrimSpace(k)) == key {
				if values, ok := child.([]interface{}); ok {
					for _, item := range values {
						if s := stringFromJSONValue(item); s != "" {
							return s
						}
					}
				}
			}
		}
		for _, child := range node {
			if s := findFirstStringInArrayByKey(child, key); s != "" {
				return s
			}
		}
	case []interface{}:
		for _, child := range node {
			if s := findFirstStringInArrayByKey(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func stringFromJSONValue(v interface{}) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64))
	default:
		return ""
	}
}

func safeFilename(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "document"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", `"`, "", "'", "", " ", "-")
	return replacer.Replace(v)
}

func validateShippingSelection(params *shopeeapi.ShippingParameterResponse, req shippingOrderRequest) error {
	if params == nil {
		return fmt.Errorf("ยังไม่ได้ตรวจเงื่อนไขจัดส่งจาก Shopee")
	}
	methods := 0
	if len(req.Pickup) > 0 {
		methods++
	}
	if len(req.Dropoff) > 0 {
		methods++
	}
	if len(req.NonIntegrated) > 0 {
		methods++
	}
	if methods == 0 {
		return fmt.Errorf("ต้องเลือก pickup, dropoff หรือ non_integrated จาก shipping parameter ก่อนจัดส่ง")
	}
	if methods > 1 {
		return fmt.Errorf("เลือกวิธีจัดส่งได้ครั้งละ 1 วิธีเท่านั้น")
	}

	if len(req.Pickup) > 0 {
		if len(params.Response.InfoNeeded.Pickup) == 0 && len(params.Response.Pickup.AddressList) == 0 {
			return fmt.Errorf("Shopee ไม่เปิดวิธี pickup สำหรับ order นี้")
		}
		if missing := missingShippingFields(params.Response.InfoNeeded.Pickup, req.Pickup); missing != "" {
			return fmt.Errorf("กรุณากรอกข้อมูล pickup ให้ครบ: %s", missing)
		}
		addressID := shippingValueString(req.Pickup["address_id"])
		if addressID != "" && !shippingPickupAddressExists(params, addressID) {
			return fmt.Errorf("pickup address ที่เลือกไม่อยู่ในตัวเลือกล่าสุดจาก Shopee")
		}
		timeID := shippingValueString(req.Pickup["pickup_time_id"])
		if timeID != "" && !shippingPickupTimeExists(params, addressID, timeID) {
			return fmt.Errorf("pickup time ที่เลือกไม่อยู่ในตัวเลือกล่าสุดจาก Shopee")
		}
		return nil
	}

	if len(req.Dropoff) > 0 {
		if len(params.Response.InfoNeeded.Dropoff) == 0 && len(params.Response.Dropoff.BranchList) == 0 {
			return fmt.Errorf("Shopee ไม่เปิดวิธี dropoff สำหรับ order นี้")
		}
		if missing := missingShippingFields(params.Response.InfoNeeded.Dropoff, req.Dropoff); missing != "" {
			return fmt.Errorf("กรุณากรอกข้อมูล dropoff ให้ครบ: %s", missing)
		}
		branchID := shippingValueString(req.Dropoff["branch_id"])
		if branchID != "" && !shippingDropoffBranchExists(params, branchID) {
			return fmt.Errorf("dropoff branch ที่เลือกไม่อยู่ในตัวเลือกล่าสุดจาก Shopee")
		}
		return nil
	}

	if len(params.Response.InfoNeeded.NonIntegrated) == 0 {
		return fmt.Errorf("Shopee ไม่เปิดวิธี non_integrated สำหรับ order นี้")
	}
	if missing := missingShippingFields(params.Response.InfoNeeded.NonIntegrated, req.NonIntegrated); missing != "" {
		return fmt.Errorf("กรุณากรอกข้อมูลจัดส่งให้ครบ: %s", missing)
	}
	return nil
}

func validateDropoffShippingGuard(params *shopeeapi.ShippingParameterResponse, req shippingOrderRequest, advancedDropoffEnabled bool) (string, string) {
	if len(req.Dropoff) == 0 {
		return "", ""
	}
	if !advancedDropoffEnabled {
		return "advanced_dropoff_disabled", "Shopee Open API ส่งข้อมูลสาขา Dropoff ไม่พอสำหรับเลือกใน Nexflow กรุณาจัดส่งจาก Seller Center แล้ว Nexflow จะติดตามสถานะกลับมา"
	}
	branchID := shippingValueString(req.Dropoff["branch_id"])
	if !shippingDropoffBranchHasUsableDetail(params, branchID) {
		return "insufficient_dropoff_branch_detail", "Shopee Open API ส่งข้อมูลสาขา Dropoff ไม่พอสำหรับเลือกใน Nexflow กรุณาจัดส่งจาก Seller Center แล้ว Nexflow จะติดตามสถานะกลับมา"
	}
	return "", ""
}

func missingShippingFields(required []string, payload map[string]interface{}) string {
	missing := []string{}
	for _, field := range required {
		key := strings.TrimSpace(field)
		if key == "" {
			continue
		}
		if shippingValueString(payload[key]) == "" {
			missing = append(missing, key)
		}
	}
	return strings.Join(missing, ", ")
}

func shippingPickupAddressExists(params *shopeeapi.ShippingParameterResponse, addressID string) bool {
	addressID = strings.TrimSpace(addressID)
	if params == nil || addressID == "" {
		return false
	}
	for _, address := range params.Response.Pickup.AddressList {
		if address.AddressID.String() == addressID {
			return true
		}
	}
	return false
}

func shippingPickupTimeExists(params *shopeeapi.ShippingParameterResponse, addressID, pickupTimeID string) bool {
	pickupTimeID = strings.TrimSpace(pickupTimeID)
	if params == nil || pickupTimeID == "" {
		return false
	}
	for _, address := range params.Response.Pickup.AddressList {
		if strings.TrimSpace(addressID) != "" && address.AddressID.String() != strings.TrimSpace(addressID) {
			continue
		}
		for _, slot := range address.TimeSlotList {
			if slot.PickupTimeID.String() == pickupTimeID {
				return true
			}
		}
	}
	return false
}

func shippingDropoffBranchExists(params *shopeeapi.ShippingParameterResponse, branchID string) bool {
	branchID = strings.TrimSpace(branchID)
	if params == nil || branchID == "" {
		return false
	}
	for _, branch := range params.Response.Dropoff.BranchList {
		if branch.BranchID.String() == branchID {
			return true
		}
	}
	return false
}

func shippingDropoffBranchHasUsableDetail(params *shopeeapi.ShippingParameterResponse, branchID string) bool {
	branchID = strings.TrimSpace(branchID)
	if params == nil || branchID == "" {
		return false
	}
	for _, branch := range params.Response.Dropoff.BranchList {
		if branch.BranchID.String() != branchID {
			continue
		}
		hasNameAndAddress := strings.TrimSpace(branch.Name) != "" && strings.TrimSpace(branch.Address) != ""
		hasCoordinates := (branch.Latitude.String() != "" && branch.Longitude.String() != "") || (branch.Lat.String() != "" && branch.Lng.String() != "")
		hasDistance := branch.Distance.String() != ""
		return hasNameAndAddress && (hasCoordinates || hasDistance)
	}
	return false
}

func shippingValueString(v interface{}) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(value), 'f', -1, 32))
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case uint32:
		return strconv.FormatUint(uint64(value), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func (h *ShopeeRealtimeHandler) Diagnostics(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	events, err := h.repo.RecentPushEvents(c.Request.Context(), 30)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลด push events ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"push_events": events})
}

func (h *ShopeeRealtimeHandler) Webhook(c *gin.Context) {
	if h == nil || h.repo == nil || h.cfg == nil || !h.cfg.ShopeeRealtimeOpsEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shopee Realtime ยังไม่เปิดใช้งาน"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "อ่าน webhook payload ไม่สำเร็จ"})
		return
	}
	if !h.verifyWebhook(c, body) {
		return
	}
	headers, _ := json.Marshal(safeShopeeWebhookHeaders(c))
	event, err := parseShopeePushPayload(body)
	if err != nil {
		inserted, storeErr := h.repo.InsertPushEvent(c.Request.Context(), repository.ShopeePushEventInput{
			ShopID:      0,
			OrderSN:     "",
			PushCode:    0,
			PushName:    "verification_or_unknown",
			EventStatus: "parse_error",
			DedupeKey:   "unparsed:" + sha256Hex(body),
			RawPayload:  shopeePushRawPayloadForStorage(body),
			Headers:     headers,
		})
		if storeErr != nil {
			h.logger.Warn("shopee_realtime: store unparsed push failed", zap.Error(storeErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึก push diagnostic ไม่สำเร็จ"})
			return
		}
		h.logger.Warn("shopee_realtime: accepted authenticated unparsed push", zap.String("error", err.Error()), zap.Bool("inserted", inserted))
		c.JSON(http.StatusOK, gin.H{"success": true, "queued": false, "diagnostic": true})
		return
	}
	inserted, err := h.repo.InsertPushEvent(c.Request.Context(), repository.ShopeePushEventInput{
		ShopID:      event.ShopID,
		OrderSN:     event.OrderSN,
		PushCode:    event.Code,
		PushName:    event.PushName,
		EventStatus: event.Status,
		UpdateTime:  event.UpdateTime,
		Timestamp:   event.Timestamp,
		DedupeKey:   event.DedupeKey,
		RawPayload:  body,
		Headers:     headers,
	})
	if err != nil {
		h.logger.Warn("shopee_realtime: store push failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึก push event ไม่สำเร็จ"})
		return
	}
	if inserted && isShopeeOrderReconcilePush(event.Code) && strings.TrimSpace(event.OrderSN) != "" {
		_ = h.repo.EnqueueReconcileJob(c.Request.Context(), event.ShopID, event.OrderSN, fmt.Sprintf("push:%d", event.Code))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := h.ProcessReconcileBatch(ctx, 5); err != nil {
				h.logger.Warn("shopee_realtime: immediate reconcile batch failed", zap.Error(err))
			}
		}()
	} else if inserted && isShopeeShopLevelPush(event.Code) {
		severity := "warning"
		title := "Shopee แจ้งเตือนสิทธิ์ร้าน"
		if event.Code == 1 {
			severity = "info"
			title = "ร้าน Shopee เชื่อมต่อสิทธิ์แล้ว"
		}
		if event.Code == 2 {
			severity = "error"
			title = "ร้าน Shopee ยกเลิกสิทธิ์เชื่อมต่อ"
		}
		h.notifyShopeeIssue(c.Request.Context(), event.ShopID, "", severity, title, event.PushName, fmt.Sprintf("shop_push:%d:%d:%s", event.ShopID, event.Code, time.Now().Format("20060102")))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "queued": inserted})
}

func (h *ShopeeRealtimeHandler) syncConnection(ctx context.Context, conn *ShopeeAPIConnection, from, to time.Time) (gin.H, error) {
	client := h.importH.shopeeAPIClient()
	seen := map[string]bool{}
	orderSNs := []string{}
	statusCounts := map[string]int{}
	counts, _ := h.repo.Counts(ctx, conn.ShopID)
	suppressNewOrderNotifications := counts.Total == 0
	for _, status := range shopeeRealtimeSyncStatuses {
		cursor := ""
		for page := 0; page < shopeeRealtimeMaxSyncPages; page++ {
			list, err := client.GetOrderList(ctx, conn.AccessToken, conn.ShopID, shopeeapi.OrderListRequest{
				TimeRangeField: "update_time",
				TimeFrom:       from.Unix(),
				TimeTo:         to.Unix(),
				PageSize:       100,
				Cursor:         cursor,
				OrderStatus:    status,
			})
			if err != nil {
				return nil, err
			}
			for _, item := range list.Response.OrderList {
				sn := strings.TrimSpace(item.OrderSN)
				if sn == "" {
					continue
				}
				listedStatus := models.NormalizeShopeeOrderStatus(item.OrderStatus)
				if listedStatus == "" {
					listedStatus = models.NormalizeShopeeOrderStatus(status)
				}
				statusCounts[listedStatus]++
				if !seen[sn] {
					seen[sn] = true
					orderSNs = append(orderSNs, sn)
				}
			}
			if !list.Response.More || strings.TrimSpace(list.Response.NextCursor) == "" {
				break
			}
			cursor = list.Response.NextCursor
		}
	}
	synced := 0
	for start := 0; start < len(orderSNs); start += shopeeAPIMaxDetailBatchSize {
		end := start + shopeeAPIMaxDetailBatchSize
		if end > len(orderSNs) {
			end = len(orderSNs)
		}
		detail, err := client.GetOrderDetail(ctx, conn.AccessToken, conn.ShopID, orderSNs[start:end], shopeeAPIOrderDetailFields())
		if err != nil {
			return nil, err
		}
		for _, d := range detail.Response.OrderList {
			before, beforeErr := h.repo.FindSnapshot(ctx, conn.ShopID, d.OrderSN)
			if beforeErr == sql.ErrNoRows {
				before = nil
			} else if beforeErr != nil {
				return nil, beforeErr
			}
			after, err := h.repo.UpsertSnapshotFromDetail(ctx, repository.ShopeeSnapshotUpsert{
				ConnectionID: conn.ID,
				ShopID:       conn.ShopID,
				ShopLabel:    conn.DisplayLabel(),
				Detail:       d,
				Source:       "sync",
			})
			if err != nil {
				return nil, err
			}
			h.notifySnapshotChange(ctx, before, after, suppressNewOrderNotifications)
			synced++
		}
	}
	return gin.H{
		"shop_id":       conn.ShopID,
		"shop_label":    conn.DisplayLabel(),
		"time_from":     from.Format(time.RFC3339),
		"time_to":       to.Format(time.RFC3339),
		"order_sns":     len(orderSNs),
		"synced_orders": synced,
		"status_counts": statusCounts,
	}, nil
}

func (h *ShopeeRealtimeHandler) reconcileOrder(ctx context.Context, shopID int64, orderSN, reason string, suppressNewOrderNotifications bool) (*models.ShopeeOrderSnapshot, error) {
	if h == nil || h.repo == nil || h.importH == nil || shopID <= 0 || strings.TrimSpace(orderSN) == "" {
		return nil, fmt.Errorf("shop_id/order_sn ไม่ถูกต้อง")
	}
	conn, err := h.connectionForShop(ctx, shopID)
	if err != nil {
		return nil, err
	}
	detail, err := h.importH.shopeeAPIClient().GetOrderDetail(ctx, conn.AccessToken, conn.ShopID, []string{strings.TrimSpace(orderSN)}, shopeeAPIOrderDetailFields())
	if err != nil {
		return nil, err
	}
	if len(detail.Response.OrderList) == 0 {
		return nil, fmt.Errorf("Shopee ไม่ส่งรายละเอียด order %s กลับมา", strings.TrimSpace(orderSN))
	}
	var latest *models.ShopeeOrderSnapshot
	for _, d := range detail.Response.OrderList {
		if strings.TrimSpace(d.OrderSN) == "" {
			continue
		}
		before, beforeErr := h.repo.FindSnapshot(ctx, conn.ShopID, d.OrderSN)
		if beforeErr == sql.ErrNoRows {
			before = nil
		} else if beforeErr != nil {
			return nil, beforeErr
		}
		after, err := h.repo.UpsertSnapshotFromDetail(ctx, repository.ShopeeSnapshotUpsert{
			ConnectionID: conn.ID,
			ShopID:       conn.ShopID,
			ShopLabel:    conn.DisplayLabel(),
			Detail:       d,
			Source:       snapshotSourceFromReconcileReason(reason),
		})
		if err != nil {
			return nil, err
		}
		h.notifySnapshotChange(ctx, before, after, suppressNewOrderNotifications)
		latest = after
	}
	if latest == nil {
		return nil, fmt.Errorf("ไม่พบรายละเอียด order %s ที่นำมา reconcile ได้", strings.TrimSpace(orderSN))
	}
	h.logger.Debug("shopee_realtime: reconciled order",
		zap.Int64("shop_id", shopID),
		zap.String("order_sn", orderSN),
		zap.String("reason", reason),
	)
	return latest, nil
}

func (h *ShopeeRealtimeHandler) reconcilePushedOrder(event parsedShopeePushEvent) {
	if h == nil || h.repo == nil || h.importH == nil || event.ShopID <= 0 || strings.TrimSpace(event.OrderSN) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := h.reconcileOrder(ctx, event.ShopID, event.OrderSN, fmt.Sprintf("push:%d", event.Code), false)
	if err != nil {
		h.notifyShopeeIssue(ctx, event.ShopID, "", "error", "รับ push Shopee แล้วแต่ดึงรายละเอียดไม่สำเร็จ", shopeeAPIErrorMessage(err, "get_order_detail ไม่สำเร็จ").Message, fmt.Sprintf("push_detail_error:%d:%s:%s", event.ShopID, event.OrderSN, time.Now().Format("2006010215")))
		h.logger.Warn("shopee_realtime: push get_order_detail failed", zap.Int64("shop_id", event.ShopID), zap.String("order_sn", event.OrderSN), zap.Error(err))
		return
	}
}

func (h *ShopeeRealtimeHandler) connectionForShop(ctx context.Context, shopID int64) (*ShopeeAPIConnection, error) {
	conns, err := h.importH.listShopeeAPIConnections(ctx, false)
	if err != nil {
		return nil, err
	}
	for i := range conns {
		if conns[i].ShopID == shopID {
			return h.importH.ensureShopeeAPIAccessToken(ctx, conns[i].ID)
		}
	}
	return nil, fmt.Errorf("ไม่พบร้าน Shopee shop_id=%d ใน Nexflow", shopID)
}

func (h *ShopeeRealtimeHandler) notifySnapshotChange(ctx context.Context, before, after *models.ShopeeOrderSnapshot, suppressNewOrder bool) {
	if after == nil {
		return
	}
	statusChanged := before == nil || before.OrderStatus != after.OrderStatus || before.ERPStatus != after.ERPStatus || before.SMLDocNo != after.SMLDocNo
	if statusChanged {
		h.publishShopeeRealtimeChanged(ctx, after.ShopID, after.OrderSN, "snapshot_changed")
	}
	if shouldNotifyShopeeNewOrder(before, after, suppressNewOrder) {
		h.notifySnapshotIssue(ctx, after, "info", "มีออเดอร์ Shopee ใหม่รอสร้างเอกสาร", shopeeNotificationBody(after), "new_order")
	}
	if after.ERPStatus == "needs_review" && (before == nil || before.ERPStatus != "needs_review") {
		h.notifySnapshotIssue(ctx, after, "warning", "ออเดอร์ Shopee ต้องตรวจสอบ", shopeeNotificationBody(after), "needs_review")
	}
	if after.ERPStatus == "failed" && (before == nil || before.ERPStatus != "failed") {
		h.notifySnapshotIssue(ctx, after, "error", "บันทึก Shopee เข้า ERP ไม่สำเร็จ", shopeeNotificationBody(after), "erp_failed")
	}
	if after.OrderStatus == "CANCELLED" && strings.TrimSpace(after.SMLDocNo) != "" && (before == nil || before.OrderStatus != "CANCELLED") {
		h.notifySnapshotIssue(ctx, after, "error", "ออเดอร์ Shopee ถูกยกเลิกหลังมีเอกสาร ERP", "ต้องตรวจเอกสารใน SML และตัดสินใจเรื่องบัญชีด้วยคนทำงาน", "cancelled_after_erp")
	}
}

func shouldNotifyShopeeNewOrder(before, after *models.ShopeeOrderSnapshot, suppressNewOrder bool) bool {
	if after == nil || !shopeeSnapshotReadyForDocumentNotification(after) {
		return false
	}
	if before == nil {
		return !suppressNewOrder
	}
	return !shopeeSnapshotReadyForDocumentNotification(before)
}

func shopeeSnapshotReadyForDocumentNotification(snap *models.ShopeeOrderSnapshot) bool {
	if snap == nil {
		return false
	}
	switch strings.TrimSpace(snap.ERPStatus) {
	case "pending", "pending_erp":
		return true
	default:
		return false
	}
}

func (h *ShopeeRealtimeHandler) notifySnapshotIssue(ctx context.Context, snap *models.ShopeeOrderSnapshot, severity, title, body, kind string) {
	if snap == nil {
		return
	}
	key := fmt.Sprintf("shopee:%s:%d:%s", strings.TrimSpace(kind), snap.ShopID, snap.OrderSN)
	created := h.publishNotification(ctx, models.NotificationInput{
		Source:     "shopee_realtime",
		Severity:   severity,
		Title:      title,
		Body:       body,
		ActionURL:  shopeeNotificationActionURL(snap.OrderSN),
		EntityType: "shopee_order",
		EntityID:   fmt.Sprintf("%d:%s", snap.ShopID, snap.OrderSN),
		DedupeKey:  key,
	})
	if kind == "new_order" && created > 0 && h.lineNotifier != nil {
		if _, err := h.lineNotifier.EnqueueShopeeNewOrder(ctx, snap, key); err != nil && h.logger != nil {
			h.logger.Warn("shopee_realtime: enqueue line notification failed",
				zap.Int64("shop_id", snap.ShopID),
				zap.String("order_sn", snap.OrderSN),
				zap.Error(err),
			)
		}
	}
}

func (h *ShopeeRealtimeHandler) notifyShopeeIssue(ctx context.Context, shopID int64, shopLabel, severity, title, body, dedupe string) {
	label := strings.TrimSpace(shopLabel)
	if label == "" && shopID > 0 {
		label = fmt.Sprintf("shop_id %d", shopID)
	}
	if label != "" && strings.TrimSpace(body) != "" {
		body = label + ": " + body
	}
	h.publishNotification(ctx, models.NotificationInput{
		Source:     "shopee_realtime",
		Severity:   severity,
		Title:      title,
		Body:       body,
		ActionURL:  "/shopee-operations",
		EntityType: "shopee_shop",
		EntityID:   fmt.Sprint(shopID),
		DedupeKey:  "shopee:" + strings.TrimSpace(dedupe),
	})
}

func (h *ShopeeRealtimeHandler) publishNotification(ctx context.Context, in models.NotificationInput) int {
	if h == nil || h.notificationRepo == nil {
		return 0
	}
	created, err := h.notificationRepo.CreateForRoles(ctx, []string{"admin", "staff"}, in)
	if err != nil {
		h.logger.Warn("shopee_realtime: create notification failed", zap.Error(err))
		return 0
	}
	for _, n := range created {
		unread, _ := h.notificationRepo.UnreadCount(ctx, n.RecipientID)
		if h.broker == nil {
			continue
		}
		h.broker.Publish(events.Event{
			Type:         events.TypeNotificationCreated,
			TargetUserID: n.RecipientID,
			Payload:      map[string]any{"notification": n, "unread_count": unread},
		})
		h.broker.Publish(events.Event{
			Type:         events.TypeNotificationUnreadChanged,
			TargetUserID: n.RecipientID,
			Payload:      map[string]any{"total": unread},
		})
	}
	return len(created)
}

func (h *ShopeeRealtimeHandler) publishShopeeRealtimeChanged(ctx context.Context, shopID int64, orderSN, reason string) {
	if h == nil || h.broker == nil {
		return
	}
	h.broker.Publish(events.Event{
		Type: events.TypeShopeeRealtimeChanged,
		Payload: map[string]any{
			"shop_id":  shopID,
			"order_sn": strings.TrimSpace(orderSN),
			"reason":   strings.TrimSpace(reason),
		},
	})
}

func shopeeNotificationBody(snap *models.ShopeeOrderSnapshot) string {
	if snap == nil {
		return ""
	}
	parts := []string{snap.OrderSN}
	if strings.TrimSpace(snap.BuyerUsername) != "" {
		parts = append(parts, snap.BuyerUsername)
	}
	if snap.TotalAmount > 0 {
		parts = append(parts, fmt.Sprintf("ยอด %.2f", snap.TotalAmount))
	}
	if strings.TrimSpace(snap.OrderStatus) != "" {
		parts = append(parts, snap.OrderStatus)
	}
	return strings.Join(parts, " · ")
}

func shopeeNotificationActionURL(orderSN string) string {
	orderSN = strings.TrimSpace(orderSN)
	if orderSN == "" {
		return "/shopee-operations"
	}
	return "/shopee-operations?order=" + url.QueryEscape(orderSN)
}

func shopeePushReadinessMessage(cfg *config.Config) string {
	if strings.TrimSpace(cfg.ShopeeRealtimeWebhookSecret) == "" && cfg.Env == "production" {
		return "ยังไม่ได้ตั้งค่า SHOPEE_REALTIME_WEBHOOK_SECRET จึงควรใช้ sync fallback ก่อนเปิด push จริง"
	}
	if strings.TrimSpace(cfg.PublicBaseURL) == "" {
		return "PUBLIC_BASE_URL ยังไม่พร้อม จึงยังสร้าง callback URL ให้ Shopee ไม่ได้"
	}
	return "พร้อมรับ push แต่ยังไม่พบ event จาก Shopee Console"
}

func snapshotSourceFromReconcileReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if strings.HasPrefix(reason, "push:") {
		return "push"
	}
	if strings.Contains(reason, "shipping") || strings.Contains(reason, "ship_order") {
		return "shipping"
	}
	return "sync"
}

func parseShopOrderParams(c *gin.Context) (int64, string, bool) {
	shopID, err := strconv.ParseInt(strings.TrimSpace(c.Param("shop_id")), 10, 64)
	orderSN := strings.TrimSpace(c.Param("order_sn"))
	if err != nil || shopID <= 0 || orderSN == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shop_id/order_sn ไม่ถูกต้อง"})
		return 0, "", false
	}
	return shopID, orderSN, true
}

func parsePositiveInt(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func snapshotConnectionID(snap *models.ShopeeOrderSnapshot) string {
	if snap == nil || snap.ConnectionID == nil {
		return ""
	}
	return *snap.ConnectionID
}

func canCheckShippingParameters(snap *models.ShopeeOrderSnapshot) bool {
	if snap == nil {
		return false
	}
	if strings.TrimSpace(snap.ERPStatus) != "sent" || strings.TrimSpace(snap.SMLDocNo) == "" {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(snap.OrderStatus)) {
	case "READY_TO_SHIP", "PROCESSED":
		return true
	default:
		return false
	}
}

func shippingBlockedReason(snap *models.ShopeeOrderSnapshot) string {
	if snap == nil {
		return "ไม่พบข้อมูล order"
	}
	if strings.TrimSpace(snap.ERPStatus) != "sent" || strings.TrimSpace(snap.SMLDocNo) == "" {
		return "ต้องส่งเอกสารเข้า SML จากหน้าคิวเอกสารให้สำเร็จก่อนจัดส่ง Shopee"
	}
	switch strings.ToUpper(strings.TrimSpace(snap.OrderStatus)) {
	case "UNPAID":
		return "order ยังไม่ชำระเงิน จึงยังจัดส่งไม่ได้"
	case "CANCELLED", "IN_CANCEL":
		return "order ถูกยกเลิกแล้ว จึงจัดส่งไม่ได้"
	case "SHIPPED", "COMPLETED":
		return "order ถูกส่งหรือปิดงานแล้ว ไม่ต้องเรียกจัดส่งซ้ำ"
	default:
		return "Shopee ยังไม่อยู่ในสถานะพร้อมจัดส่ง"
	}
}

func (h *ShopeeRealtimeHandler) verifyWebhook(c *gin.Context, body []byte) bool {
	secret := strings.TrimSpace(h.cfg.ShopeeRealtimeWebhookSecret)
	if secret == "" {
		if h.cfg.Env == "production" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Shopee push secret ยังไม่ได้ตั้งค่า"})
			return false
		}
		return true
	}
	got := strings.TrimSpace(c.Query("token"))
	if got == "" {
		got = strings.TrimSpace(c.GetHeader("X-Nexflow-Shopee-Webhook-Token"))
	}
	if got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1 {
		return true
	}
	if h.verifyShopeeWebhookSignature(c, body, secret) {
		return true
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook token"})
	return false
}

func (h *ShopeeRealtimeHandler) verifyShopeeWebhookSignature(c *gin.Context, body []byte, secret string) bool {
	got := normalizeShopeeWebhookSignature(c.GetHeader("Authorization"))
	if got == "" {
		got = normalizeShopeeWebhookSignature(c.GetHeader("X-Shopee-Signature"))
	}
	if got == "" {
		return false
	}
	for _, callbackURL := range h.shopeeWebhookSignatureURLs(c) {
		if callbackURL == "" {
			continue
		}
		expected := hmacSHA256Hex(secret, []byte(callbackURL+"|"+string(body)))
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}

func (h *ShopeeRealtimeHandler) shopeeWebhookSignatureURLs(c *gin.Context) []string {
	uri := c.Request.URL.RequestURI()
	urls := make([]string, 0, 2)
	if base := strings.TrimRight(strings.TrimSpace(h.cfg.PublicBaseURL), "/"); base != "" {
		urls = append(urls, base+uri)
	}
	scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "https"
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host != "" {
		urls = append(urls, scheme+"://"+host+uri)
	}
	return urls
}

func normalizeShopeeWebhookSignature(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	lower := strings.ToLower(v)
	for _, prefix := range []string{"sha256=", "hmac-sha256 ", "bearer "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(v[len(prefix):])
		}
	}
	return v
}

func hmacSHA256Hex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func shopeePushRawPayloadForStorage(body []byte) json.RawMessage {
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"_raw_sha256": sha256Hex(body),
		"_raw_size":   len(body),
	})
	return json.RawMessage(payload)
}

type parsedShopeePushEvent struct {
	ShopID     int64
	OrderSN    string
	Code       int
	PushName   string
	Status     string
	UpdateTime time.Time
	Timestamp  time.Time
	DedupeKey  string
}

type shopeePushMeta struct {
	Name            string
	RequiresOrderSN bool
	ShopLevel       bool
}

var shopeePushCodeMeta = map[int]shopeePushMeta{
	1:  {Name: "shop_authorization_push", ShopLevel: true},
	2:  {Name: "shop_authorization_canceled_push", ShopLevel: true},
	3:  {Name: "order_status_push", RequiresOrderSN: true},
	4:  {Name: "order_trackingno_push", RequiresOrderSN: true},
	12: {Name: "open_api_authorization_expiry", ShopLevel: true},
	15: {Name: "shipping_document_status_push", RequiresOrderSN: true},
	23: {Name: "booking_status_push", RequiresOrderSN: true},
	24: {Name: "booking_trackingno_push", RequiresOrderSN: true},
	25: {Name: "booking_shipping_document_status_push", RequiresOrderSN: true},
	30: {Name: "package_fulfillment_status_push", RequiresOrderSN: true},
	47: {Name: "package_info_push", RequiresOrderSN: true},
}

func parseShopeePushPayload(body []byte) (parsedShopeePushEvent, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return parsedShopeePushEvent{}, fmt.Errorf("payload ไม่ใช่ JSON")
	}
	data, _ := raw["data"].(map[string]interface{})
	shopID := int64FromAny(raw["shop_id"])
	if shopID <= 0 {
		shopID = int64FromAny(data["shop_id"])
	}
	orderSN := firstNonEmptyString(data["ordersn"], data["order_sn"], raw["ordersn"], raw["order_sn"])
	code := int(int64FromAny(raw["code"]))
	meta := shopeePushCodeMeta[code]
	status := firstNonEmptyString(data["status"], data["order_status"], raw["status"], raw["order_status"])
	updateTime := unixTimeFromAny(data["update_time"])
	timestamp := unixTimeFromAny(raw["timestamp"])
	if shopID <= 0 {
		return parsedShopeePushEvent{}, fmt.Errorf("payload ไม่มี shop_id")
	}
	if orderSN == "" && meta.RequiresOrderSN {
		return parsedShopeePushEvent{}, fmt.Errorf("payload ไม่มี order_sn")
	}
	sum := sha256.Sum256(body)
	baseKey := fmt.Sprintf("%d:%s:%d:%s:%d:%d:%s", shopID, orderSN, code, status, updateTime.Unix(), timestamp.Unix(), hex.EncodeToString(sum[:]))
	return parsedShopeePushEvent{
		ShopID:     shopID,
		OrderSN:    orderSN,
		Code:       code,
		PushName:   shopeePushName(code),
		Status:     strings.ToUpper(status),
		UpdateTime: updateTime,
		Timestamp:  timestamp,
		DedupeKey:  baseKey,
	}, nil
}

func int64FromAny(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		out, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return out
	default:
		return 0
	}
}

func unixTimeFromAny(v interface{}) time.Time {
	n := int64FromAny(v)
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func firstNonEmptyString(values ...interface{}) string {
	for _, v := range values {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func isShopeeShopLevelPush(code int) bool {
	return shopeePushCodeMeta[code].ShopLevel
}

func isShopeeOrderReconcilePush(code int) bool {
	return shopeePushCodeMeta[code].RequiresOrderSN
}

func shopeePushName(code int) string {
	if meta, ok := shopeePushCodeMeta[code]; ok {
		return meta.Name
	}
	return "unknown"
}

func safeShopeeWebhookHeaders(c *gin.Context) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"User-Agent", "Content-Type", "X-Shopee-Signature", "X-Shopee-Request-Id"} {
		if v := c.GetHeader(key); v != "" {
			out[key] = v
		}
	}
	return out
}
