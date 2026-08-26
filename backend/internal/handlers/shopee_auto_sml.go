package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/shopeeapi"
)

const (
	shopeeAutoSMLWorkerEvery = 5 * time.Second
	shopeeAutoSMLLease       = 5 * time.Minute
	shopeeAutoSMLBatchSize   = 2
	autoSMLLineItemLimit     = 5
)

var shopeeAutoSMLBangkokTimeZone = time.FixedZone("Asia/Bangkok", 7*60*60)

func (h *ShopeeRealtimeHandler) maybeEnqueueAutoSML(ctx context.Context, detail shopeeapi.OrderDetail, before, snap *models.ShopeeOrderSnapshot) {
	if h == nil || h.cfg == nil || !h.cfg.ShopeeAutoSMLEnabled || h.autoSMLRepo == nil || snap == nil {
		return
	}
	if strings.ToUpper(strings.TrimSpace(snap.OrderStatus)) != "READY_TO_SHIP" || detail.CreateTime <= 0 {
		return
	}
	createTime := time.Unix(detail.CreateTime, 0)
	var updateTime *time.Time
	if detail.UpdateTime > 0 {
		value := time.Unix(detail.UpdateTime, 0)
		updateTime = &value
	}
	readyToShipAt, err := h.autoSMLReadyToShipAt(ctx, detail, before, snap)
	if err != nil {
		h.logger.Warn("shopee_auto_sml: load ready transition failed", zap.Int64("shop_id", snap.ShopID), zap.String("order_sn", snap.OrderSN), zap.Error(err))
		return
	}
	if readyToShipAt == nil {
		return
	}
	inserted, err := h.autoSMLRepo.Enqueue(ctx, snap.ShopID, snap.OrderSN, createTime, updateTime, *readyToShipAt, h.realtimeRouteSignature(ctx))
	if err != nil {
		h.logger.Warn("shopee_auto_sml: enqueue failed", zap.Int64("shop_id", snap.ShopID), zap.String("order_sn", snap.OrderSN), zap.Error(err))
		return
	}
	if inserted {
		h.publishShopeeRealtimeChanged(ctx, snap.ShopID, snap.OrderSN, "auto_sml_queued")
	}
}

func (h *ShopeeRealtimeHandler) autoSMLReadyToShipAt(ctx context.Context, detail shopeeapi.OrderDetail, before, snap *models.ShopeeOrderSnapshot) (*time.Time, error) {
	if h == nil || h.repo == nil || snap == nil {
		return nil, nil
	}
	transition, err := h.repo.OrderStatusTransitionAt(ctx, snap.ShopID, snap.OrderSN, "READY_TO_SHIP")
	if err != nil || transition != nil {
		return transition, err
	}
	if before == nil || models.NormalizeShopeeOrderStatus(before.OrderStatus) == "READY_TO_SHIP" || detail.UpdateTime <= 0 {
		return nil, nil
	}
	value := time.Unix(detail.UpdateTime, 0)
	return &value, nil
}

func (h *ShopeeRealtimeHandler) decorateAutoSMLManualReasons(ctx context.Context, snapshots []models.ShopeeOrderSnapshot) {
	if h == nil || h.cfg == nil || !h.cfg.ShopeeAutoSMLEnabled || h.autoSMLRepo == nil || h.repo == nil || len(snapshots) == 0 {
		return
	}
	settings, err := h.autoSMLRepo.ListSettings(ctx)
	if err != nil {
		return
	}
	byShop := make(map[int64]models.ShopeeAutoSMLSetting, len(settings))
	for _, setting := range settings {
		byShop[setting.ShopID] = setting
	}
	refs := make([]repository.ShopeeSnapshotRef, 0, len(snapshots))
	for i := range snapshots {
		snap := &snapshots[i]
		setting, ok := byShop[snap.ShopID]
		if snap.AutoSML.Status == "" && models.NormalizeShopeeOrderStatus(snap.OrderStatus) == "READY_TO_SHIP" &&
			ok && setting.Enabled && setting.PausedReason == "" && setting.EligibleAfter != nil {
			refs = append(refs, repository.ShopeeSnapshotRef{ShopID: snap.ShopID, OrderSN: snap.OrderSN})
		}
	}
	transitions, err := h.repo.OrderStatusTransitionTimes(ctx, refs, "READY_TO_SHIP")
	if err != nil {
		return
	}
	for i := range snapshots {
		snap := &snapshots[i]
		if snap.AutoSML.Status != "" || strings.ToUpper(strings.TrimSpace(snap.OrderStatus)) != "READY_TO_SHIP" {
			continue
		}
		setting, ok := byShop[snap.ShopID]
		if !ok || !setting.Enabled || setting.PausedReason != "" || setting.EligibleAfter == nil {
			continue
		}
		var detail shopeeapi.OrderDetail
		if err := json.Unmarshal(snap.RawDetail, &detail); err != nil || detail.CreateTime <= 0 {
			snap.AutoSML = models.ShopeeAutoSMLJobView{Status: "manual_required", ErrorCode: "missing_create_time", ErrorMessage: "Shopee ไม่มี create_time จึงต้องสร้างและส่ง SML ด้วยมือ"}
			continue
		}
		readyToShipAt, found := transitions[repository.ShopeeSnapshotRef{ShopID: snap.ShopID, OrderSN: snap.OrderSN}]
		if !found {
			snap.AutoSML = models.ShopeeAutoSMLJobView{Status: "manual_required", ErrorCode: "missing_ready_transition", ErrorMessage: "ไม่พบเวลาเข้า READY_TO_SHIP จึงต้องสร้างและส่ง SML ด้วยมือ"}
			continue
		}
		if readyToShipAt.Before(*setting.EligibleAfter) {
			snap.AutoSML = models.ShopeeAutoSMLJobView{Status: "manual_required", ErrorCode: "before_eligible_after", ErrorMessage: "ออเดอร์นี้เข้า READY_TO_SHIP ก่อนเปิด Auto SML จึงไม่ประมวลผลย้อนหลัง"}
		}
	}
}

func (h *ShopeeRealtimeHandler) StartAutoSMLWorker(ctx context.Context) {
	if h == nil || h.cfg == nil || !h.cfg.ShopeeAutoSMLEnabled || h.autoSMLRepo == nil {
		return
	}
	if recovered, err := h.autoSMLRepo.RecoverStaleJobs(ctx); err != nil {
		h.logger.Warn("shopee_auto_sml: recover stale jobs failed", zap.Error(err))
	} else if recovered > 0 {
		h.logger.Warn("shopee_auto_sml: recovered stale jobs", zap.Int64("jobs", recovered))
	}
	go func() {
		ticker := time.NewTicker(shopeeAutoSMLWorkerEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.processAutoSMLBatch(ctx)
			}
		}
	}()
}

func (h *ShopeeRealtimeHandler) processAutoSMLBatch(ctx context.Context) {
	jobs, err := h.autoSMLRepo.LeaseJobs(ctx, shopeeAutoSMLBatchSize, shopeeAutoSMLLease)
	if err != nil {
		if ctx.Err() == nil {
			h.logger.Warn("shopee_auto_sml: lease jobs failed", zap.Error(err))
		}
		return
	}
	var wg sync.WaitGroup
	for i := range jobs {
		job := jobs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.processAutoSMLJob(ctx, job)
		}()
	}
	wg.Wait()
}

func (h *ShopeeRealtimeHandler) processAutoSMLJob(ctx context.Context, job models.ShopeeAutoSMLJob) {
	started := time.Now()
	log := h.logger.With(zap.String("job_id", job.ID), zap.Int64("shop_id", job.ShopID), zap.String("order_sn", job.OrderSN), zap.Int("attempt", job.Attempts))
	defer func() {
		log.Info("shopee_auto_sml: processed", zap.Duration("duration", time.Since(started)))
	}()
	setting, err := h.autoSMLRepo.GetSetting(ctx, job.ShopID)
	if err != nil || !setting.Enabled || setting.PausedReason != "" {
		_ = h.autoSMLRepo.MarkCancelled(ctx, job.ID, "automation_unavailable", "ระบบอัตโนมัติของร้านถูกปิดหรือหยุดชั่วคราว")
		return
	}
	currentRoute := h.realtimeRouteSignature(ctx)
	if currentRoute == "" || currentRoute != setting.RouteSignature || currentRoute != job.RouteSignature {
		_ = h.autoSMLRepo.PauseForRouteChange(ctx, job.ShopID)
		h.markAutoSMLNeedsReview(ctx, job, "route_changed", "เส้นทางเอกสาร SML เปลี่ยน กรุณาตรวจสอบและเปิดระบบอัตโนมัติใหม่", nil)
		return
	}

	snap, err := h.reconcileOrder(ctx, job.ShopID, job.OrderSN, "auto_sml_preflight", false)
	if err != nil {
		h.handleAutoSMLTransient(ctx, job, "shopee_reconcile_failed", shopeeAPIErrorMessage(err, "ตรวจสถานะล่าสุดจาก Shopee ไม่สำเร็จ").Message, nil)
		return
	}
	if strings.ToUpper(strings.TrimSpace(snap.OrderStatus)) != "READY_TO_SHIP" {
		_ = h.autoSMLRepo.MarkCancelled(ctx, job.ID, "status_changed", "สถานะ Shopee เปลี่ยนก่อนเริ่มส่ง SML")
		h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_cancelled")
		return
	}

	requestRaw, _ := json.Marshal(gin.H{"confirm": "AUTO_SML", "job_id": job.ID})
	outcome := h.createDocumentForOrderMode(ctx, job.ShopID, job.OrderSN, "", "auto-sml-"+job.ID, requestRaw, true)
	if outcome.HTTPStatus >= 400 && strings.TrimSpace(outcome.BillID) == "" {
		if outcome.HTTPStatus == http.StatusConflict && strings.Contains(outcome.Reason, "READY_TO_SHIP") {
			_ = h.autoSMLRepo.MarkCancelled(ctx, job.ID, "status_changed", outcome.Reason)
			return
		}
		if outcome.HTTPStatus == http.StatusConflict && strings.Contains(outcome.Reason, "กำลังสร้างเอกสาร") {
			_ = h.autoSMLRepo.MarkContentionRetry(ctx, job.ID, outcome.Reason)
			h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_retry")
			return
		}
		if (outcome.HTTPStatus >= http.StatusBadRequest && outcome.HTTPStatus < http.StatusInternalServerError) || autoSMLUserActionError(outcome.Reason) {
			h.markAutoSMLNeedsReview(ctx, job, autoSMLReviewCode(outcome.Reason), outcome.Reason, snap)
			return
		}
		h.handleAutoSMLTransient(ctx, job, "bill_create_failed", outcome.Reason, snap)
		return
	}
	billID := strings.TrimSpace(outcome.BillID)
	if billID == "" && snap.BillID != nil {
		billID = strings.TrimSpace(*snap.BillID)
	}
	if billID == "" {
		h.handleAutoSMLTransient(ctx, job, "bill_missing", "สร้างเอกสาร Nexflow แล้วแต่ไม่พบ Bill ID", snap)
		return
	}
	bill, err := h.billH.billRepo.FindByID(billID)
	if err != nil || bill == nil {
		h.handleAutoSMLTransient(ctx, job, "bill_load_failed", "โหลดบิล Nexflow ก่อนส่ง SML ไม่สำเร็จ", snap)
		return
	}
	if bill.Status == "sent" && bill.SMLDocNo != nil && strings.TrimSpace(*bill.SMLDocNo) != "" {
		h.completeAutoSMLSuccess(ctx, job, bill, snap)
		return
	}
	if bill.Status == "needs_review" {
		h.markAutoSMLNeedsReview(ctx, job, "bill_needs_review", "บิลยังมีสินค้าหรือยอดเงินที่ต้องตรวจสอบก่อนส่ง SML", snap)
		return
	}
	if bill.Status != "pending" && bill.Status != "failed" {
		h.markAutoSMLNeedsReview(ctx, job, "bill_status_blocked", "สถานะบิลไม่พร้อมส่ง SML: "+bill.Status, snap)
		return
	}
	fingerprint := tikTokAmountFingerprint(bill.Items)
	if job.BillFingerprint != "" && job.BillFingerprint != fingerprint {
		h.markAutoSMLNeedsReview(ctx, job, "bill_changed", "ข้อมูลสินค้าในบิลเปลี่ยนหลังเข้าคิว กรุณาตรวจสอบแล้วลองใหม่", snap)
		return
	}
	if err := h.autoSMLRepo.LinkBill(ctx, job.ID, bill.ID, fingerprint); err != nil {
		h.handleAutoSMLTransient(ctx, job, "job_link_failed", "บันทึกการเชื่อมงานกับบิลไม่สำเร็จ", snap)
		return
	}

	setting, err = h.autoSMLRepo.GetSetting(ctx, job.ShopID)
	if err != nil || !setting.Enabled || setting.PausedReason != "" {
		_ = h.autoSMLRepo.MarkCancelled(ctx, job.ID, "automation_stopped", "ระบบอัตโนมัติถูกปิดก่อนส่ง SML")
		return
	}
	if current := h.realtimeRouteSignature(ctx); current == "" || current != setting.RouteSignature {
		_ = h.autoSMLRepo.PauseForRouteChange(ctx, job.ShopID)
		h.markAutoSMLNeedsReview(ctx, job, "route_changed", "เส้นทางเอกสาร SML เปลี่ยน กรุณาตรวจสอบและเปิดระบบอัตโนมัติใหม่", snap)
		return
	}
	bill, err = h.billH.billRepo.FindByID(bill.ID)
	if err != nil || bill == nil {
		h.handleAutoSMLTransient(ctx, job, "bill_reload_failed", "โหลดบิลล่าสุดก่อนส่ง SML ไม่สำเร็จ", snap)
		return
	}
	if bill.Status == "sent" {
		h.completeAutoSMLSuccess(ctx, job, bill, snap)
		return
	}
	if tikTokAmountFingerprint(bill.Items) != fingerprint {
		h.markAutoSMLNeedsReview(ctx, job, "bill_changed", "ข้อมูลบิลเปลี่ยนระหว่างทำงาน กรุณาตรวจสอบแล้วลองใหม่", snap)
		return
	}
	documentTime, err := h.autoSMLRepo.GetOrSetDocumentTime(ctx, job.ID, autoSMLDocumentTime(time.Now()))
	if err != nil {
		h.handleAutoSMLTransient(ctx, job, "document_time_persist_failed", "บันทึกเวลาเอกสารก่อนส่ง SML ไม่สำเร็จ", snap)
		return
	}
	result := h.billH.sendBillToSML(bill, RetryRequest{DocTime: documentTime}, retrySendOptions{
		Context: ctx, TraceID: "auto-sml-" + job.ID, Via: "shopee_auto_sml", SuppressLineAlert: true,
	})
	switch {
	case result.HTTPStatus == http.StatusOK:
		reloaded, _ := h.billH.billRepo.FindByID(bill.ID)
		if reloaded != nil {
			bill = reloaded
		}
		h.completeAutoSMLSuccess(ctx, job, bill, snap)
	case result.HTTPStatus == http.StatusConflict && strings.Contains(result.Error, "กำลังถูกส่ง"):
		_ = h.autoSMLRepo.MarkContentionRetry(ctx, job.ID, result.Error)
		h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_retry")
	case result.HTTPStatus == http.StatusAccepted || result.FailureClass == "user_action" || autoSMLUserActionError(result.Error):
		h.markAutoSMLNeedsReview(ctx, job, autoSMLReviewCode(result.Error), firstNonEmpty(result.Error, result.Message, "บิลต้องตรวจสอบก่อนส่ง SML"), snap)
	default:
		h.handleAutoSMLTransient(ctx, job, "sml_send_failed", firstNonEmpty(result.Error, result.Message, "ส่ง SML ไม่สำเร็จ"), snap)
	}
}

func autoSMLDocumentTime(startedAt time.Time) string {
	return startedAt.In(shopeeAutoSMLBangkokTimeZone).Format("15:04")
}

func (h *ShopeeRealtimeHandler) completeAutoSMLSuccess(ctx context.Context, job models.ShopeeAutoSMLJob, bill *models.Bill, snap *models.ShopeeOrderSnapshot) {
	if bill == nil {
		h.handleAutoSMLTransient(ctx, job, "bill_missing_after_send", "ส่ง SML แล้วแต่โหลดบิลยืนยันผลไม่สำเร็จ", snap)
		return
	}
	docNo := ""
	if bill != nil && bill.SMLDocNo != nil {
		docNo = strings.TrimSpace(*bill.SMLDocNo)
	}
	if docNo == "" {
		docNo = strings.TrimSpace(job.SMLDocNo)
	}
	if err := h.autoSMLRepo.MarkSucceeded(ctx, job.ID, bill.ID, docNo); err != nil {
		h.logger.Warn("shopee_auto_sml: mark success failed", zap.String("job_id", job.ID), zap.Error(err))
		return
	}
	n := h.autoSMLNotification(job, bill, snap, "", "")
	n.SMLDocNo = docNo
	h.enqueueAutoSMLLine(ctx, "success", n)
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_succeeded")
}

func (h *ShopeeRealtimeHandler) markAutoSMLNeedsReview(ctx context.Context, job models.ShopeeAutoSMLJob, code, message string, snap *models.ShopeeOrderSnapshot) {
	billID := ""
	if job.BillID != nil {
		billID = *job.BillID
	}
	if snap != nil && snap.BillID != nil {
		billID = *snap.BillID
	}
	_ = h.autoSMLRepo.MarkNeedsReview(ctx, job.ID, billID, code, message)
	_ = h.repo.MarkOrderERPStatus(ctx, job.ShopID, job.OrderSN, "needs_review", message)
	n := h.autoSMLNotification(job, nil, snap, code, message)
	n.BillID = billID
	h.publishNotification(ctx, models.NotificationInput{
		Source: "shopee_realtime", Severity: "warning", Title: "ออเดอร์ Shopee ต้องตรวจสอบก่อนส่ง SML",
		Body: message, ActionURL: autoSMLActionURL(billID, job.OrderSN), EntityType: "shopee_order",
		EntityID:  fmt.Sprintf("%d:%s", job.ShopID, job.OrderSN),
		DedupeKey: fmt.Sprintf("shopee:auto_sml:review:%d:%s:%s", job.ShopID, job.OrderSN, code),
	})
	h.enqueueAutoSMLLine(ctx, "review", n)
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_needs_review")
}

func (h *ShopeeRealtimeHandler) handleAutoSMLTransient(ctx context.Context, job models.ShopeeAutoSMLJob, code, message string, snap *models.ShopeeOrderSnapshot) {
	paused, terminal, err := h.autoSMLRepo.MarkTransientFailure(ctx, job.ID, code, message, 3)
	if err != nil {
		h.logger.Warn("shopee_auto_sml: record transient failure failed", zap.String("job_id", job.ID), zap.Error(err))
		return
	}
	if terminal || paused {
		n := h.autoSMLNotification(job, nil, snap, code, message)
		h.publishNotification(ctx, models.NotificationInput{
			Source: "shopee_realtime", Severity: "error", Title: "ส่งออเดอร์ Shopee เข้า SML ไม่สำเร็จ",
			Body: message, ActionURL: shopeeNotificationActionURL(job.OrderSN), EntityType: "shopee_order",
			EntityID:  fmt.Sprintf("%d:%s", job.ShopID, job.OrderSN),
			DedupeKey: fmt.Sprintf("shopee:auto_sml:failure:%d:%s:%s", job.ShopID, job.OrderSN, code),
		})
		h.enqueueAutoSMLLine(ctx, "failure", n)
		if paused {
			h.notifyShopeeIssue(ctx, job.ShopID, n.ShopLabel, "error", "หยุด Auto SML ชั่วคราว", "SML หรือระบบเชื่อมต่อล้มเหลวติดต่อกัน 3 ครั้ง กรุณาตรวจสอบแล้วเปิดต่อ", fmt.Sprintf("auto_sml_paused:%d", job.ShopID))
		}
	}
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_retry")
}

func (h *ShopeeRealtimeHandler) autoSMLNotification(job models.ShopeeAutoSMLJob, bill *models.Bill, snap *models.ShopeeOrderSnapshot, code, message string) models.ShopeeAutoSMLNotification {
	out := models.ShopeeAutoSMLNotification{ShopID: job.ShopID, OrderSN: job.OrderSN, ErrorCode: code, ErrorMessage: message}
	if snap != nil {
		out.ShopLabel = snap.ShopLabel
		out.TotalAmount = snap.TotalAmount
	}
	out.Items, out.ItemCount = autoSMLNotificationItems(snap, bill)
	if bill != nil {
		out.BillID = bill.ID
		if bill.SMLDocNo != nil {
			out.SMLDocNo = *bill.SMLDocNo
		}
	}
	return out
}

func autoSMLNotificationItems(snap *models.ShopeeOrderSnapshot, bill *models.Bill) ([]models.ShopeeAutoSMLNotificationItem, int) {
	if snap != nil && len(snap.RawDetail) > 0 {
		var detail shopeeapi.OrderDetail
		if err := json.Unmarshal(snap.RawDetail, &detail); err == nil && len(detail.ItemList) > 0 {
			total := len(detail.ItemList)
			items := make([]models.ShopeeAutoSMLNotificationItem, 0, min(total, autoSMLLineItemLimit))
			for _, item := range detail.ItemList {
				name := strings.TrimSpace(item.ItemName)
				if name == "" {
					continue
				}
				if len(items) < autoSMLLineItemLimit {
					items = append(items, models.ShopeeAutoSMLNotificationItem{
						Name: name, Variant: strings.TrimSpace(item.ModelName), Qty: item.ModelQuantityPurchased,
					})
				}
			}
			if len(items) > 0 {
				return items, total
			}
		}
	}
	if bill == nil {
		return nil, 0
	}
	items := make([]models.ShopeeAutoSMLNotificationItem, 0, min(len(bill.Items), autoSMLLineItemLimit))
	total := 0
	for _, item := range bill.Items {
		if item.SourceSKU == models.ShopeeShippingSourceSKU {
			continue
		}
		name := strings.TrimSpace(item.RawName)
		if name == "" {
			continue
		}
		total++
		if len(items) < autoSMLLineItemLimit {
			items = append(items, models.ShopeeAutoSMLNotificationItem{Name: name, Qty: item.Qty})
		}
	}
	return items, total
}

func (h *ShopeeRealtimeHandler) enqueueAutoSMLLine(ctx context.Context, kind string, in models.ShopeeAutoSMLNotification) {
	notifier, ok := h.lineNotifier.(lineAutoSMLNotifier)
	if !ok || notifier == nil {
		return
	}
	var err error
	switch kind {
	case "success":
		_, err = notifier.EnqueueShopeeAutoSMLSuccess(ctx, in, fmt.Sprintf("shopee:auto_sml:success:%d:%s:%s", in.ShopID, in.OrderSN, in.SMLDocNo))
	case "review":
		_, err = notifier.EnqueueShopeeAutoSMLReview(ctx, in, fmt.Sprintf("shopee:auto_sml:review:%d:%s:%s", in.ShopID, in.OrderSN, in.ErrorCode))
	case "failure":
		_, err = notifier.EnqueueShopeeAutoSMLFailure(ctx, in, fmt.Sprintf("shopee:auto_sml:failure:%d:%s:%s", in.ShopID, in.OrderSN, in.ErrorCode))
	}
	if err != nil {
		h.logger.Warn("shopee_auto_sml: enqueue line failed", zap.String("kind", kind), zap.Int64("shop_id", in.ShopID), zap.String("order_sn", in.OrderSN), zap.Error(err))
	}
}

func autoSMLUserActionError(message string) bool {
	value := strings.ToLower(strings.TrimSpace(message))
	for _, part := range []string{"catalog", "สินค้า sml", "product master", "mapping", "sku", "จับคู่", "ค่าขนส่ง", "ส่วนต่าง", "ยอด", "หน่วย", "สินค้าชุด", "เส้นทาง", "ตั้งค่า"} {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}

func autoSMLReviewCode(message string) string {
	value := strings.ToLower(message)
	switch {
	case strings.Contains(value, "catalog") || strings.Contains(value, "สินค้า sml"):
		return "catalog_not_ready"
	case strings.Contains(value, "ค่าขนส่ง"):
		return "shipping_config_missing"
	case strings.Contains(value, "ส่วนต่าง") || strings.Contains(value, "ยอด"):
		return "amount_review_required"
	case strings.Contains(value, "สินค้าชุด"):
		return "set_definition_invalid"
	default:
		return "mapping_incomplete"
	}
}

func autoSMLActionURL(billID, orderSN string) string {
	if strings.TrimSpace(billID) != "" {
		return "/sale-invoices/" + strings.TrimSpace(billID)
	}
	return shopeeNotificationActionURL(orderSN)
}

func (h *ShopeeRealtimeHandler) AutoSMLSettings(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if h.autoSMLRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Auto SML repository ยังไม่พร้อม"})
		return
	}
	settings, err := h.autoSMLRepo.ListSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดการตั้งค่า Auto SML ไม่สำเร็จ"})
		return
	}
	lineTotal, lineEnabled := 0, 0
	lineStatus := "unavailable"
	if h.lineRecipientRepo != nil {
		if total, enabled, err := h.lineRecipientRepo.CountRecipientStatus(c.Request.Context()); err == nil {
			lineTotal, lineEnabled, lineStatus = total, enabled, "ok"
		}
	}
	routeReadiness := h.realtimeRouteReadiness(c.Request.Context())
	route := gin.H{
		"ready_to_send_sml": routeReadiness["ready_to_send_sml"],
		"route":             routeReadiness["route"],
		"document_route":    routeReadiness["document_route"],
		"doc_format_code":   routeReadiness["doc_format_code"],
		"missing_fields":    routeReadiness["missing_fields"],
		"message":           routeReadiness["message"],
	}
	for i := range settings {
		settings[i].RouteSignature = ""
	}
	c.JSON(http.StatusOK, gin.H{
		"global_enabled": h.cfg.ShopeeAutoSMLEnabled,
		"settings":       settings,
		"route":          route,
		"line":           gin.H{"status": lineStatus, "total_recipients": lineTotal, "enabled_recipients": lineEnabled},
		"checked_at":     time.Now(),
	})
}

func (h *ShopeeRealtimeHandler) UpdateAutoSMLSetting(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, err := strconv.ParseInt(strings.TrimSpace(c.Param("shop_id")), 10, 64)
	if err != nil || shopID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shop_id ไม่ถูกต้อง"})
		return
	}
	var req struct {
		Enabled bool   `json:"enabled"`
		Confirm string `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ถูกต้อง"})
		return
	}
	if req.Enabled {
		if req.Confirm != "ENABLE_AUTO_SML" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณายืนยันด้วย ENABLE_AUTO_SML"})
			return
		}
		if code, message := h.autoSMLPreflight(c.Request.Context(), shopID); code != "" {
			c.JSON(http.StatusConflict, gin.H{"error": message, "code": code})
			return
		}
	}
	setting, err := h.autoSMLRepo.SetEnabled(c.Request.Context(), shopID, req.Enabled, c.GetString("user_id"), h.realtimeRouteSignature(c.Request.Context()))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบร้าน Shopee"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกการตั้งค่า Auto SML ไม่สำเร็จ"})
		return
	}
	if h.billH != nil && h.billH.auditRepo != nil {
		target := strconv.FormatInt(shopID, 10)
		userID := c.GetString("user_id")
		h.billH.auditRepo.Log(models.AuditEntry{Action: "shopee_auto_sml_setting_updated", TargetID: &target, UserID: &userID, Source: "shopee_realtime", Detail: gin.H{"enabled": req.Enabled}})
	}
	h.publishShopeeRealtimeChanged(c.Request.Context(), shopID, "", "auto_sml_setting_updated")
	c.JSON(http.StatusOK, gin.H{"setting": setting, "message": map[bool]string{true: "เปิดสร้างบิล SML อัตโนมัติแล้ว", false: "ปิดสร้างบิล SML อัตโนมัติแล้ว"}[req.Enabled]})
}

func (h *ShopeeRealtimeHandler) RetryAutoSML(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if h.cfg == nil || !h.cfg.ShopeeAutoSMLEnabled || h.autoSMLRepo == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Auto SML ยังปิดในระดับเซิร์ฟเวอร์"})
		return
	}
	shopID, orderSN, ok := parseShopOrderParams(c)
	if !ok {
		return
	}
	if err := h.autoSMLRepo.RetryJob(c.Request.Context(), shopID, orderSN); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusConflict, gin.H{"error": "งานนี้ยังลองใหม่ไม่ได้ กรุณาตรวจว่าระบบอัตโนมัติเปิดอยู่และแก้ปัญหาแล้ว"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "นำงานกลับเข้าคิวไม่สำเร็จ"})
		return
	}
	h.publishShopeeRealtimeChanged(c.Request.Context(), shopID, orderSN, "auto_sml_retried")
	c.JSON(http.StatusOK, gin.H{"message": "นำออเดอร์กลับเข้าคิว Auto SML แล้ว"})
}

func (h *ShopeeRealtimeHandler) autoSMLPreflight(ctx context.Context, shopID int64) (string, string) {
	if h.cfg == nil || !h.cfg.ShopeeAutoSMLEnabled {
		return "feature_disabled", "ระบบ Auto SML ยังปิดในระดับเซิร์ฟเวอร์"
	}
	if _, err := h.connectionForShop(ctx, shopID); err != nil {
		return "shopee_not_ready", "ร้าน Shopee หรือ token ยังไม่พร้อม: " + err.Error()
	}
	push := h.pushReadiness(ctx)
	pushConfigured, _ := push["configured"].(bool)
	if !pushConfigured {
		return "realtime_not_ready", "Shopee Realtime Push ยังไม่พร้อม กรุณาตรวจการเชื่อมต่อก่อน"
	}
	route := h.realtimeRouteReadiness(ctx)
	ready, _ := route["ready_to_send_sml"].(bool)
	if !ready || h.realtimeRouteSignature(ctx) == "" {
		return "route_not_ready", "เส้นทาง Shopee Realtime ไป SML ยังตั้งค่าไม่ครบ"
	}
	if h.billH == nil || !h.billH.checkSMLReadiness(ctx, true).Ready {
		return "sml_not_ready", "SML ยังไม่พร้อมใช้งาน"
	}
	if h.importH == nil || h.importH.catalogRepo == nil {
		return "catalog_not_ready", "Catalog SML ยังไม่พร้อม"
	}
	if count, err := h.importH.catalogRepo.CountActive(); err != nil || count == 0 {
		return "catalog_not_ready", "ยังไม่มีรายการสินค้า SML กรุณาอัปเดตรายการสินค้าก่อน"
	}
	if h.lineRecipientRepo == nil {
		return "line_not_ready", "LINE แจ้งเตือนยังไม่พร้อม"
	}
	_, enabled, err := h.lineRecipientRepo.CountRecipientStatus(ctx)
	if err != nil || enabled == 0 {
		return "line_not_ready", "กรุณาเพิ่มและเปิดผู้รับ LINE แจ้งเตือนอย่างน้อย 1 ราย"
	}
	return "", ""
}
