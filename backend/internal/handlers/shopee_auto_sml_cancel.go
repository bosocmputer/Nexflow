package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
	"nexflow/internal/services/smlprofile"
)

const (
	shopeeAutoSMLCancellationWorkerEvery  = 5 * time.Second
	shopeeAutoSMLCancellationLease        = 2 * time.Minute
	shopeeAutoSMLCancellationBatchSize    = 2
	shopeeAutoSMLCancellationMaxAttempts  = 3
	shopeeSMLCancelStockRecalcMaxAttempts = 10
)

func (h *ShopeeRealtimeHandler) maybeEnqueueAutoSMLCancellation(ctx context.Context, before, after *models.ShopeeOrderSnapshot) bool {
	if h == nil || h.cfg == nil || h.repo == nil ||
		!shouldEnqueueAutoSMLCancellation(h.cfg.ShopeeAutoSMLCancelEnabled && h.cfg.ShopeeSMLCancelDocumentsEnabled, before, after) {
		return false
	}
	if h.autoSMLRepo == nil {
		return false
	}
	setting, err := h.autoSMLRepo.GetSetting(ctx, after.ShopID)
	if err != nil || !setting.Enabled || strings.TrimSpace(setting.PausedReason) != "" {
		return false
	}
	routeDef, routeMeta, _, err := h.cancelSaleRoute(ctx)
	if err != nil || routeDef == nil {
		if h.logger != nil {
			h.logger.Warn("shopee_auto_sml_cancel: route unavailable",
				zap.Int64("shop_id", after.ShopID), zap.String("order_sn", after.OrderSN), zap.Error(err))
		}
		return false
	}
	mainDef, mainErr := h.importH.channelDefaults.Get("shopee_realtime", "sale")
	mainRoute := routeNameFromEndpoint(mainDef, true)
	if mainErr != nil || mainRoute == "" || h.capabilityClient == nil {
		_ = h.autoSMLRepo.PauseForRouteChange(ctx, after.ShopID)
		return false
	}
	capability, capabilityErr := h.capabilityClient.Fetch(ctx)
	if capabilityErr != nil || sml.ValidateGatewayProfileCapability(capability, h.cfg.SMLDocumentProfileRouteModes,
		[]string{mainRoute, routeMeta.StockRoute}, true) != nil {
		_ = h.autoSMLRepo.PauseForRouteChange(ctx, after.ShopID)
		if h.logger != nil {
			h.logger.Warn("shopee_auto_sml_cancel: capability mismatch; automation paused",
				zap.Int64("shop_id", after.ShopID), zap.String("route", routeMeta.StockRoute))
		}
		return false
	}
	billID := strings.TrimSpace(stringPtrValue(after.BillID))
	inserted, err := h.repo.EnqueueAutoSMLCancellation(ctx, repository.ShopeeSMLCancellationInput{
		ShopID: after.ShopID, OrderSN: after.OrderSN, BillID: billID,
		SaleSMLDocNo: strings.TrimSpace(after.SMLDocNo), RouteEndpoint: strings.TrimSpace(routeDef.Endpoint),
		RouteSignature: shopeeSMLCancellationRouteSignature(routeDef, h.documentProfileRouteMode(routeMeta.StockRoute)),
	})
	if err != nil {
		if h.logger != nil {
			h.logger.Error("shopee_auto_sml_cancel: enqueue failed",
				zap.Int64("shop_id", after.ShopID), zap.String("order_sn", after.OrderSN), zap.Error(err))
		}
		return false
	}
	if inserted {
		h.publishNotification(ctx, models.NotificationInput{
			Source: "shopee_realtime", Severity: "info", Title: "กำลังสร้างเอกสารยกเลิก SML อัตโนมัติ",
			Body:      strings.TrimSpace(after.OrderSN) + " · ใบขาย " + strings.TrimSpace(after.SMLDocNo),
			ActionURL: shopeeNotificationActionURL(after.OrderSN), EntityType: "shopee_order",
			EntityID:  fmt.Sprintf("%d:%s", after.ShopID, after.OrderSN),
			DedupeKey: fmt.Sprintf("shopee:auto_sml_cancel:queued:%d:%s:%s", after.ShopID, after.OrderSN, strings.TrimSpace(after.SMLDocNo)),
		})
		h.publishShopeeRealtimeChanged(ctx, after.ShopID, after.OrderSN, "auto_sml_cancel_queued")
	}
	// A duplicate row means the transition is already durably handled.
	return true
}

func shopeeSMLCancellationRouteSignature(def *models.ChannelDefault, profileMode ...string) string {
	if def == nil {
		return ""
	}
	mode := smlprofile.ModeOff
	if len(profileMode) > 0 && strings.TrimSpace(profileMode[0]) != "" {
		mode = strings.TrimSpace(profileMode[0])
	}
	return smlprofile.RouteSignature(*def, mode) + ":" + sml.SalesProfileContractRevision
}

func (h *ShopeeRealtimeHandler) StartSMLCancellationWorkers(ctx context.Context) {
	if h == nil || h.cfg == nil || h.repo == nil {
		return
	}
	legacyWorkersEnabled := h.cfg.ShopeeSMLCancelDocumentsEnabled
	profileWorkerEnabled := h.cancelClient != nil && h.cancelClient.IsConfigured() && h.cancellationProfileReconciliationEnabled()
	if !legacyWorkersEnabled && !profileWorkerEnabled {
		return
	}
	if legacyWorkersEnabled && h.cfg.ShopeeAutoSMLCancelEnabled {
		if recovered, err := h.repo.RecoverStaleAutoSMLCancellations(ctx); err != nil {
			h.logger.Warn("shopee_auto_sml_cancel: recover stale jobs failed", zap.Error(err))
		} else if recovered > 0 {
			h.logger.Warn("shopee_auto_sml_cancel: recovered stale jobs", zap.Int64("jobs", recovered))
		}
	}
	if legacyWorkersEnabled {
		if recovered, err := h.repo.RecoverStaleSMLCancellationStockRecalcs(ctx); err != nil {
			h.logger.Warn("shopee_sml_cancel_stock_recalc: recover stale jobs failed", zap.Error(err))
		} else if recovered > 0 {
			h.logger.Warn("shopee_sml_cancel_stock_recalc: recovered stale jobs", zap.Int64("jobs", recovered))
		}
	}
	go func() {
		h.processSMLCancellationWorkers(ctx)
		ticker := time.NewTicker(shopeeAutoSMLCancellationWorkerEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.processSMLCancellationWorkers(ctx)
			}
		}
	}()
}

func (h *ShopeeRealtimeHandler) processSMLCancellationWorkers(ctx context.Context) {
	if h.cfg != nil && h.cfg.ShopeeSMLCancelDocumentsEnabled && h.cfg.ShopeeAutoSMLCancelEnabled {
		h.processAutoSMLCancellationBatch(ctx)
	}
	if h.cfg != nil && h.cfg.ShopeeSMLCancelDocumentsEnabled {
		h.processSMLCancellationStockRecalcBatch(ctx)
	}
	if h.cancellationProfileReconciliationEnabled() {
		h.processSMLCancellationProfileReconciliationBatch(ctx)
	}
}

func (h *ShopeeRealtimeHandler) cancellationProfileReconciliationEnabled() bool {
	if h == nil || h.cfg == nil || h.cancelClient == nil || !h.cancelClient.IsConfigured() {
		return false
	}
	for _, route := range []string{"saleordercancel", "saleinvoicecancel", "creditnote"} {
		if h.documentProfileRouteMode(route) == smlprofile.ModeActive {
			return true
		}
	}
	return false
}

func (h *ShopeeRealtimeHandler) processSMLCancellationProfileReconciliationBatch(ctx context.Context) {
	job, err := h.repo.ClaimSMLCancellationProfileReconciliationJob(ctx, h.cancellationProfileWorkerID(), shopeeAutoSMLCancellationLease)
	if err != nil {
		if ctx.Err() == nil && h.logger != nil {
			h.logger.Warn("sml_cancellation_profile: lease failed", zap.Error(err))
		}
		return
	}
	if job == nil {
		return
	}
	if h.documentProfileRouteMode(job.Route) != smlprofile.ModeActive {
		if err := h.repo.DeferSMLCancellationProfileReconciliationJob(ctx, job, time.Minute); err != nil && h.logger != nil {
			h.logger.Warn("sml_cancellation_profile: defer inactive route failed", zap.String("route", job.Route), zap.Error(err))
		}
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	failure := h.processSMLCancellationProfileReconciliation(runCtx, job)
	cancel()
	if failure == nil {
		return
	}
	recordCtx, recordCancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = h.repo.FailSMLCancellationProfileReconciliationJob(recordCtx, job, failure.Code, failure.Message, failure.Terminal)
	recordCancel()
	if err != nil {
		if h.logger != nil {
			h.logger.Error("sml_cancellation_profile: persist failure failed", zap.String("job_id", job.ID), zap.Error(err))
		}
		return
	}
	if h.logger != nil {
		h.logger.Warn("sml_cancellation_profile: reconciliation failed",
			zap.String("tenant", job.TenantKey), zap.String("route", job.Route),
			zap.String("code", failure.Code), zap.Int("attempt", job.AttemptCount))
	}
}

func (h *ShopeeRealtimeHandler) cancellationProfileWorkerID() string {
	tenant := "nexflow"
	if h != nil && h.cfg != nil {
		tenant = firstNonEmpty(strings.TrimSpace(h.cfg.ShopeeGatewayTenant), strings.TrimSpace(h.cfg.ShopeeSMLDatabase), tenant)
	}
	return tenant + "-sml-cancel-profile"
}

type smlCancellationProfileFailure struct {
	Code, Message string
	Terminal      bool
}

func (h *ShopeeRealtimeHandler) processSMLCancellationProfileReconciliation(ctx context.Context, job *repository.SMLCancellationProfileReconciliationJob) *smlCancellationProfileFailure {
	if job == nil {
		return &smlCancellationProfileFailure{Code: "job_invalid", Message: "cancellation Profile job is missing", Terminal: true}
	}
	instanceTenant := ""
	if h != nil && h.cfg != nil {
		instanceTenant = firstNonEmpty(strings.TrimSpace(h.cfg.ShopeeGatewayTenant), strings.TrimSpace(h.cfg.ShopeeSMLDatabase))
	}
	if err := repository.ValidateSMLProfileJobTenant(job.TenantKey, instanceTenant); err != nil {
		return &smlCancellationProfileFailure{Code: "tenant_mismatch", Message: err.Error(), Terminal: true}
	}
	var req sml.SaleInvoiceCancelRequest
	if err := json.Unmarshal(job.RequestPayload, &req); err != nil || req.DocumentProfileVersion != job.ProfileVersion || req.DocumentProfileVersion != sml.InvoiceDocumentProfileVersion {
		return &smlCancellationProfileFailure{Code: "immutable_payload_invalid", Message: "immutable cancellation Profile payload is invalid", Terminal: true}
	}
	meta, err := resolveShopeeSMLCancellationRoute(job.RouteEndpoint)
	if err != nil || meta.StockRoute != job.Route {
		return &smlCancellationProfileFailure{Code: "route_snapshot_invalid", Message: "immutable cancellation route snapshot is invalid", Terminal: true}
	}
	req.Kind = meta.Kind
	if strings.TrimSpace(req.DocNo) == "" || strings.TrimSpace(req.DocNo) != strings.TrimSpace(job.CancelDocNo) || strings.TrimSpace(job.SourceDocNo) == "" {
		return &smlCancellationProfileFailure{Code: "immutable_payload_invalid", Message: "immutable cancellation document identity is invalid", Terminal: true}
	}
	statusCode, response, callErr := h.cancelClient.CreateBytes(ctx, job.SourceDocNo, req.Kind, job.RequestPayload, job.CorrelationID)
	if callErr != nil {
		return &smlCancellationProfileFailure{Code: "gateway_unavailable", Message: "SML Gateway is unavailable"}
	}
	if response == nil {
		return &smlCancellationProfileFailure{Code: "gateway_response_missing", Message: "SML Gateway response is missing"}
	}
	responseCode := strings.TrimSpace(response.GetCode())
	if responseCode == "doc_no_payload_mismatch" || responseCode == "existing_document_profile_unverifiable" || responseCode == "source_already_cancelled_externally" {
		return &smlCancellationProfileFailure{Code: responseCode, Message: "SML cancellation Profile cannot be reconciled automatically", Terminal: true}
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices || !response.IsSuccess() {
		terminal := statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError && statusCode != http.StatusTooManyRequests && responseCode != "document_busy"
		if responseCode == "" {
			responseCode = "gateway_rejected"
		}
		return &smlCancellationProfileFailure{Code: responseCode, Message: fmt.Sprintf("SML Gateway rejected cancellation Profile reconciliation (HTTP %d)", statusCode), Terminal: terminal}
	}
	result := response.DocumentProfileResult(job.ProfileVersion)
	if result.PayloadHash == "" {
		return &smlCancellationProfileFailure{Code: "profile_hash_missing", Message: "SML Gateway did not return the cancellation Profile hash"}
	}
	if job.PayloadHash != "" && result.PayloadHash != job.PayloadHash {
		return &smlCancellationProfileFailure{Code: "profile_hash_mismatch", Message: "SML Gateway cancellation Profile hash changed", Terminal: true}
	}
	if result.ProfileStatus != "complete" || result.ReconciliationRequired {
		return &smlCancellationProfileFailure{Code: "profile_incomplete", Message: "SML cancellation Profile is still incomplete"}
	}
	if err := h.repo.CompleteSMLCancellationProfileReconciliationJob(ctx, job, result.PayloadHash, result.CompletedChecks); err != nil {
		if errors.Is(err, repository.ErrSMLAttemptLeaseLost) {
			return &smlCancellationProfileFailure{Code: "lease_lost", Message: "cancellation Profile lease was lost"}
		}
		return &smlCancellationProfileFailure{Code: "completion_persist_failed", Message: "persist cancellation Profile completion failed"}
	}
	if h.billH != nil && h.billH.auditRepo != nil && strings.TrimSpace(job.BillID) != "" {
		billID := strings.TrimSpace(job.BillID)
		_ = h.billH.auditRepo.Log(models.AuditEntry{
			Action: "profile_complete", TargetID: &billID, Source: "sml", Level: "info",
			TraceID: job.CorrelationID, Detail: map[string]any{
				"profile_version": job.ProfileVersion, "route": job.Route,
				"attempt_count": job.AttemptCount, "completed_checks": result.CompletedChecks,
			},
		})
	}
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "sml_cancellation_profile_complete")
	if h.logger != nil {
		h.logger.Info("profile_complete", zap.String("tenant", job.TenantKey), zap.String("route", job.Route),
			zap.Int("attempt_count", job.AttemptCount), zap.String("correlation_id", job.CorrelationID))
	}
	return nil
}

func (h *ShopeeRealtimeHandler) processAutoSMLCancellationBatch(ctx context.Context) {
	jobs, err := h.repo.LeaseAutoSMLCancellations(ctx, shopeeAutoSMLCancellationBatchSize, shopeeAutoSMLCancellationLease)
	if err != nil {
		if ctx.Err() == nil && h.logger != nil {
			h.logger.Warn("shopee_auto_sml_cancel: lease jobs failed", zap.Error(err))
		}
		return
	}
	var wg sync.WaitGroup
	for i := range jobs {
		job := jobs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
			defer cancel()
			h.processAutoSMLCancellationJob(jobCtx, job)
		}()
	}
	wg.Wait()
}

func (h *ShopeeRealtimeHandler) processAutoSMLCancellationJob(ctx context.Context, job models.ShopeeSMLCancellation) {
	log := h.logger.With(zap.String("job_id", job.ID), zap.Int64("shop_id", job.ShopID),
		zap.String("order_sn", job.OrderSN), zap.Int("attempt", job.Attempts))
	if h.cfg == nil || !h.cfg.ShopeeAutoSMLCancelEnabled || !h.cfg.ShopeeSMLCancelDocumentsEnabled {
		h.blockAutoSMLCancellation(ctx, job, nil, "automation_disabled", "ระบบยกเลิก SML อัตโนมัติถูกปิด", nil)
		return
	}
	snap, err := h.reconcileOrder(ctx, job.ShopID, job.OrderSN, "auto_sml_cancel_preflight", false)
	if err != nil {
		h.retryAutoSMLCancellation(ctx, job, nil, "shopee_reconcile_failed", shopeeAPIErrorMessage(err, "ตรวจสถานะล่าสุดจาก Shopee ไม่สำเร็จ").Message)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(snap.OrderStatus), "CANCELLED") {
		h.blockAutoSMLCancellation(ctx, job, snap, "status_not_final_cancelled", "Shopee ยังไม่ยืนยันสถานะ CANCELLED จึงไม่สร้างเอกสารยกเลิก SML", nil)
		return
	}
	cancelCtx, status, payload := h.cancelSMLDocumentContext(ctx, job.ShopID, job.OrderSN)
	if status >= 400 || cancelCtx == nil {
		h.blockAutoSMLCancellation(ctx, job, snap, "cancel_preflight_blocked", stringFromGinPayload(payload, "error"), nil)
		return
	}
	if cancelCtx.Existing != nil && cancellationStatusIsSuccess(cancelCtx.Existing.Status) {
		cancelDocNo := strings.TrimSpace(cancelCtx.Existing.CancelSMLDocNo)
		if _, err := h.repo.CompleteSMLCancellation(ctx, cancelCtx.Existing.ID, cancelCtx.Existing.Status, cancelDocNo, cancelCtx.Existing.Response, ""); err != nil {
			h.retryAutoSMLCancellation(ctx, job, snap, "existing_success_recalc_queue_failed", "พบเอกสารยกเลิก SML แล้ว แต่เข้าคิวคำนวณสต๊อกไม่สำเร็จ")
			return
		}
		if err := h.repo.SupersedeAutoSMLCancellation(ctx, job.ID, cancelCtx.Existing.ID); err != nil {
			h.retryAutoSMLCancellation(ctx, job, snap, "supersede_failed", "ปิดงานซ้ำหลังพบเอกสารยกเลิก SML สำเร็จไม่สำเร็จ")
			return
		}
		h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_cancel_superseded")
		return
	}
	if cancelCtx.Existing != nil && cancelCtx.Existing.ID != job.ID {
		if strings.EqualFold(strings.TrimSpace(cancelCtx.Existing.Status), "creating") {
			h.retryAutoSMLCancellation(ctx, job, snap, "concurrent_attempt", "มีผู้ใช้อื่นกำลังสร้างเอกสารยกเลิก SML สำหรับใบขายนี้")
			return
		}
		if (strings.EqualFold(strings.TrimSpace(cancelCtx.Existing.Status), "failed") ||
			strings.EqualFold(strings.TrimSpace(cancelCtx.Existing.Status), "blocked")) &&
			strings.TrimSpace(cancelCtx.Existing.CancelSMLDocNo) != "" {
			h.blockAutoSMLCancellation(ctx, job, snap, "existing_attempt_requires_reconciliation", "พบ attempt เอกสารยกเลิกเดิมที่ต้องตรวจสอบก่อน ระบบจึงไม่ออกเลขเอกสารใหม่", nil)
			return
		}
	}
	currentSignature := shopeeSMLCancellationRouteSignature(cancelCtx.RouteDef, h.documentProfileRouteMode(cancelCtx.RouteMeta.StockRoute))
	if currentSignature == "" || currentSignature != job.RouteSignature || strings.TrimSpace(cancelCtx.RouteDef.Endpoint) != strings.TrimSpace(job.RouteEndpoint) {
		h.blockAutoSMLCancellation(ctx, job, snap, "route_changed", "เส้นทางเอกสารยกเลิก SML เปลี่ยนหลังเข้าคิว กรุณาตรวจสอบก่อนลองใหม่", nil)
		return
	}
	if h.cancelClient == nil || !h.cancelClient.IsConfigured() {
		h.retryAutoSMLCancellation(ctx, job, snap, "sml_cancel_client_not_configured", "ยังไม่ได้ตั้งค่า SML cancel client")
		return
	}

	req, err := h.smlCancellationRequestForAttempt(ctx, cancelCtx, job)
	if err != nil {
		h.blockAutoSMLCancellation(ctx, job, snap, "payload_prepare_failed", err.Error(), nil)
		return
	}
	statusCode, resp, callErr := h.cancelClient.Create(ctx, cancelCtx.SaleDocNo, req)
	if callErr != nil || resp == nil || (!resp.IsSuccess() && !smlCancelAlreadyExists(resp)) || statusCode >= 500 {
		message := smlCancelErrorMessage(statusCode, resp, callErr)
		if classifySMLCancellationFailure(statusCode, callErr) == "transient" {
			h.retryAutoSMLCancellation(ctx, job, snap, "sml_cancel_transient", message)
			return
		}
		h.blockAutoSMLCancellation(ctx, job, snap, "sml_cancel_rejected", message, responseRaw(resp))
		return
	}
	finalStatus := "created"
	if smlCancelAlreadyExists(resp) {
		finalStatus = "already_exists"
	}
	cancelDocNo := strings.TrimSpace(resp.CancelDocNo())
	if cancelDocNo == "" {
		cancelDocNo = strings.TrimSpace(req.DocNo)
	}
	profileResult := smlCancellationProfileResult(req, resp, job.ID)
	completed, err := h.repo.CompleteSMLCancellation(ctx, job.ID, finalStatus, cancelDocNo, resp.Raw(), "", profileResult)
	if err != nil {
		// Replaying the same immutable doc_no is safe; do not invent a new attempt.
		h.retryAutoSMLCancellation(ctx, job, snap, "tracking_complete_failed", "SML สำเร็จแต่บันทึกผลใน Nexflow ไม่สำเร็จ")
		return
	}
	requestRaw, _ := json.Marshal(req)
	_ = h.repo.RecordAction(ctx, job.ShopID, job.OrderSN, "cancel_sml_document", "", "done", requestRaw, resp.Raw(), "")
	h.auditShopeeSMLCancel("", cancelCtx, completed, "info", "shopee_sml_cancel_created_auto", "")
	h.notifySMLCancellationCreated(ctx, cancelCtx, cancelDocNo)
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "sml_cancel_document_created")
	log.Info("shopee_auto_sml_cancel: succeeded", zap.String("cancel_doc_no", cancelDocNo))
}

func (h *ShopeeRealtimeHandler) processSMLCancellationStockRecalcBatch(ctx context.Context) {
	jobs, err := h.repo.LeaseSMLCancellationStockRecalcs(ctx, 1, shopeeAutoSMLCancellationLease)
	if err != nil {
		if ctx.Err() == nil && h.logger != nil {
			h.logger.Warn("shopee_sml_cancel_stock_recalc: lease jobs failed", zap.Error(err))
		}
		return
	}
	for i := range jobs {
		jobCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		h.processSMLCancellationStockRecalc(jobCtx, jobs[i])
		cancel()
	}
}

func (h *ShopeeRealtimeHandler) processSMLCancellationStockRecalc(ctx context.Context, job models.ShopeeSMLCancellation) {
	fail := func(err error) {
		message := "คำนวณสต๊อก SML หลังสร้างเอกสารยกเลิกไม่สำเร็จ"
		if err != nil {
			message += ": " + err.Error()
		}
		recordCtx, recordCancel := context.WithTimeout(context.Background(), 5*time.Second)
		terminal, markErr := h.repo.FailSMLCancellationStockRecalc(recordCtx, job.ID, message, shopeeSMLCancelStockRecalcMaxAttempts)
		recordCancel()
		if markErr != nil {
			h.logger.Error("shopee_sml_cancel_stock_recalc: record failure", zap.String("job_id", job.ID), zap.Error(markErr))
			return
		}
		if terminal {
			h.publishNotification(context.Background(), models.NotificationInput{
				Source: "shopee_realtime", Severity: "error", Title: "ต้องตรวจการคำนวณสต๊อก SML หลังยกเลิก",
				Body:      job.OrderSN + " · " + job.CancelSMLDocNo + " · " + message,
				ActionURL: shopeeNotificationActionURL(job.OrderSN), EntityType: "shopee_order",
				EntityID:  fmt.Sprintf("%d:%s", job.ShopID, job.OrderSN),
				DedupeKey: fmt.Sprintf("shopee:sml_cancel_stock_recalc_failed:%s", job.ID),
			})
			if h.billH != nil && h.billH.auditRepo != nil && job.BillID != nil {
				billID := strings.TrimSpace(*job.BillID)
				_ = h.billH.auditRepo.Log(models.AuditEntry{
					Action: "shopee_sml_cancel_stock_recalc_failed", TargetID: &billID, Source: "sml", Level: "error",
					Detail: map[string]any{"job_id": job.ID, "shop_id": job.ShopID, "order_sn": job.OrderSN, "cancel_doc_no": job.CancelSMLDocNo, "error": message},
				})
			}
		}
	}
	if h.billH == nil || h.billH.billRepo == nil || job.BillID == nil || strings.TrimSpace(*job.BillID) == "" {
		fail(fmt.Errorf("ไม่พบ bill สำหรับงานคำนวณสต๊อก"))
		return
	}
	bill, err := h.billH.billRepo.FindByID(strings.TrimSpace(*job.BillID))
	if err != nil || bill == nil {
		fail(fmt.Errorf("โหลด bill ไม่สำเร็จ"))
		return
	}
	itemCodes := smlCancellationItemCodes(bill)
	if len(itemCodes) == 0 {
		fail(fmt.Errorf("ไม่พบรหัสสินค้า SML ใน bill"))
		return
	}
	stockCfg, err := h.billH.resolveStockRecalcConfig()
	if err != nil || strings.TrimSpace(stockCfg.StockRequestURL) == "" || strings.TrimSpace(stockCfg.Provider) == "" || strings.TrimSpace(stockCfg.Database) == "" {
		fail(fmt.Errorf("SML processstockrequest runtime ไม่พร้อม"))
		return
	}
	client := sml.NewStockRequestClient(stockCfg.StockRequestURL, stockCfg.Provider, stockCfg.Database, h.logger)
	if err := client.ProcessStockRequest(ctx, itemCodes); err != nil {
		fail(err)
		return
	}
	if err := h.repo.CompleteSMLCancellationStockRecalc(ctx, job.ID); err != nil {
		fail(err)
		return
	}
	if h.billH.auditRepo != nil {
		billID := bill.ID
		_ = h.billH.auditRepo.Log(models.AuditEntry{
			Action: "shopee_sml_cancel_stock_recalc_ok", TargetID: &billID, Source: "sml", Level: "info",
			Detail: map[string]any{"shop_id": job.ShopID, "order_sn": job.OrderSN, "cancel_doc_no": job.CancelSMLDocNo, "item_count": len(itemCodes)},
		})
	}
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "sml_cancel_stock_recalculated")
	h.logger.Info("shopee_sml_cancel_stock_recalc: succeeded", zap.String("job_id", job.ID), zap.String("cancel_doc_no", job.CancelSMLDocNo), zap.Int("item_count", len(itemCodes)))
}

func smlCancellationItemCodes(bill *models.Bill) []string {
	if bill == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(bill.Items))
	out := make([]string, 0, len(bill.Items))
	for _, item := range bill.Items {
		if item.ItemCode == nil {
			continue
		}
		code := strings.TrimSpace(*item.ItemCode)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func (h *ShopeeRealtimeHandler) smlCancellationRequestForAttempt(ctx context.Context, cancelCtx *shopeeSMLCancelDocumentContext, job models.ShopeeSMLCancellation) (sml.SaleInvoiceCancelRequest, error) {
	if len(job.RequestPayload) > 0 && string(job.RequestPayload) != "{}" {
		var req sml.SaleInvoiceCancelRequest
		if err := json.Unmarshal(job.RequestPayload, &req); err != nil {
			return req, fmt.Errorf("payload เอกสารยกเลิกที่บันทึกไว้ไม่ถูกต้อง")
		}
		meta, err := resolveShopeeSMLCancellationRoute(job.RouteEndpoint)
		if err != nil {
			return req, err
		}
		req.Kind = meta.Kind
		if strings.TrimSpace(req.DocNo) == "" || strings.TrimSpace(req.DocNo) != strings.TrimSpace(job.CancelSMLDocNo) {
			return req, fmt.Errorf("เลขเอกสารใน payload ไม่ตรงกับ attempt ที่บันทึกไว้")
		}
		return req, nil
	}
	if strings.TrimSpace(job.CancelSMLDocNo) != "" {
		return sml.SaleInvoiceCancelRequest{}, fmt.Errorf("พบเลขเอกสารที่ไม่มี immutable payload จึงหยุดเพื่อป้องกันเอกสารซ้ำ")
	}
	now := time.Now().In(shopeeAutoSMLBangkokTimeZone)
	docNo, err := h.allocateSMLCancellationDocNo(ctx, cancelCtx, now, true)
	if err != nil {
		return sml.SaleInvoiceCancelRequest{}, err
	}
	req, err := h.saleInvoiceCancelRequest(cancelCtx, docNo, now)
	if err != nil {
		return sml.SaleInvoiceCancelRequest{}, err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return sml.SaleInvoiceCancelRequest{}, err
	}
	if err := h.repo.PrepareSMLCancellationCreate(ctx, job.ID, docNo, payload); err != nil {
		return sml.SaleInvoiceCancelRequest{}, err
	}
	return req, nil
}

func (h *ShopeeRealtimeHandler) retryAutoSMLCancellation(ctx context.Context, job models.ShopeeSMLCancellation, snap *models.ShopeeOrderSnapshot, code, message string) {
	terminal, err := h.repo.MarkAutoSMLCancellationRetry(ctx, job.ID, code, message, shopeeAutoSMLCancellationMaxAttempts)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("shopee_auto_sml_cancel: record retry failed", zap.String("job_id", job.ID), zap.Error(err))
		}
		return
	}
	if terminal {
		job.Status = "failed"
		job.Error = message
		h.notifyAutoSMLCancellationFailure(ctx, job, snap, message)
		h.auditAutoSMLCancellationFailure(job, code, message)
	}
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_cancel_retry")
}

func (h *ShopeeRealtimeHandler) blockAutoSMLCancellation(ctx context.Context, job models.ShopeeSMLCancellation, snap *models.ShopeeOrderSnapshot, code, message string, response json.RawMessage) {
	if strings.TrimSpace(message) == "" {
		message = "สร้างเอกสารยกเลิก SML อัตโนมัติไม่ได้"
	}
	if err := h.repo.BlockAutoSMLCancellation(ctx, job.ID, code, message, response); err != nil {
		if h.logger != nil {
			h.logger.Error("shopee_auto_sml_cancel: record blocked state failed", zap.String("job_id", job.ID), zap.Error(err))
		}
		return
	}
	job.Status = "blocked"
	job.Error = message
	h.notifyAutoSMLCancellationFailure(ctx, job, snap, message)
	h.auditAutoSMLCancellationFailure(job, code, message)
	h.publishShopeeRealtimeChanged(ctx, job.ShopID, job.OrderSN, "auto_sml_cancel_blocked")
}

func (h *ShopeeRealtimeHandler) notifyAutoSMLCancellationFailure(ctx context.Context, job models.ShopeeSMLCancellation, snap *models.ShopeeOrderSnapshot, message string) {
	if snap == nil {
		snap, _ = h.repo.FindSnapshot(ctx, job.ShopID, job.OrderSN)
	}
	if snap == nil {
		return
	}
	h.notifySnapshotIssue(ctx, snap, nil, "error", "สร้างเอกสารยกเลิก SML อัตโนมัติไม่สำเร็จ", message, "cancelled_after_sml")
}

func (h *ShopeeRealtimeHandler) auditAutoSMLCancellationFailure(job models.ShopeeSMLCancellation, code, message string) {
	if h == nil || h.billH == nil || h.billH.auditRepo == nil || job.BillID == nil {
		return
	}
	billID := strings.TrimSpace(*job.BillID)
	_ = h.billH.auditRepo.Log(models.AuditEntry{
		Action: "shopee_sml_cancel_auto_failed", TargetID: &billID, Source: "shopee_realtime", Level: "error",
		Detail: map[string]any{"job_id": job.ID, "shop_id": job.ShopID, "order_sn": job.OrderSN, "sale_sml_doc_no": job.SaleSMLDocNo, "code": code, "error": message},
	})
}
