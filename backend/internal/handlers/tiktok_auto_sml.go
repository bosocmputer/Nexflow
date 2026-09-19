package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/tiktokshop"
)

const (
	tikTokAutoSMLWorkerEvery = 5 * time.Second
	tikTokAutoSMLLease       = 5 * time.Minute
	tikTokAutoSMLBatchSize   = 2
)

var tikTokAutoSMLBangkok = time.FixedZone("Asia/Bangkok", 7*60*60)

type TikTokAutoSMLSettingsStore interface {
	ListSettings(context.Context) ([]models.TikTokAutoSMLSetting, error)
	GetSetting(context.Context, string) (*models.TikTokAutoSMLSetting, error)
	UpdateSetting(context.Context, repository.TikTokAutoSMLSettingUpdate) (*models.TikTokAutoSMLSetting, error)
	RetryJob(context.Context, string, string, string, string) error
}

type TikTokAutoSMLWorkStore interface {
	TikTokAutoSMLSettingsStore
	Enqueue(context.Context, repository.TikTokAutoSMLEnqueueInput) (bool, error)
	RecoverStaleJobs(context.Context) (int64, error)
	LeaseJobs(context.Context, int, time.Duration) ([]models.TikTokAutoSMLJob, error)
	LinkBill(context.Context, string, string, string) error
	GetOrSetDocumentTime(context.Context, string, string) (string, error)
	MarkNeedsReview(context.Context, string, string, string, string) error
	MarkCancelled(context.Context, string, string, string) error
	MarkSucceeded(context.Context, string, string, string) error
	MarkTransientFailure(context.Context, string, string, string, int) error
	PauseForRouteChange(context.Context, string) error
}

type TikTokAutoSMLController struct {
	cfg       *config.Config
	repo      TikTokAutoSMLWorkStore
	previewer TikTokShopBillShadowPreviewer
	creator   TikTokShopReviewedBillCreator
	billH     *BillHandler
	audit     TikTokShopAuditLogger
	logger    *zap.Logger
}

func NewTikTokAutoSMLController(cfg *config.Config, repo TikTokAutoSMLWorkStore, previewer TikTokShopBillShadowPreviewer, creator TikTokShopReviewedBillCreator, billH *BillHandler, audit TikTokShopAuditLogger, logger *zap.Logger) *TikTokAutoSMLController {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokAutoSMLController{cfg: cfg, repo: repo, previewer: previewer, creator: creator, billH: billH, audit: audit, logger: logger}
}

func (c *TikTokAutoSMLController) ObserveTikTokOrderSnapshot(ctx context.Context, shopID string, record tiktokshop.TikTokOrderSnapshotRecord) error {
	if c == nil || c.cfg == nil || !c.cfg.TikTokShopAutoSMLEnabled || c.repo == nil || c.previewer == nil || record.LastOrderUpdateAt == nil ||
		string(record.OrderStatus) != models.TikTokAutoSMLTriggerAwaitingCollection {
		return nil
	}
	setting, err := c.repo.GetSetting(ctx, shopID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (setting == nil || !setting.Enabled || setting.PausedReason != "")) {
		return nil
	}
	if err != nil {
		return err
	}
	preview, err := c.previewer.Preview(ctx, shopID, record.OrderID)
	if err != nil || preview == nil {
		if err == nil {
			err = errors.New("empty TikTok Bill preview")
		}
		return err
	}
	billFingerprint, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil {
		return err
	}
	inserted, err := c.repo.Enqueue(ctx, repository.TikTokAutoSMLEnqueueInput{
		ShopID: shopID, OrderID: record.OrderID, OrderStatus: string(record.OrderStatus),
		TriggerTransitionAt: record.LastOrderUpdateAt.UTC(), SourceHash: record.SourceHash,
		BillFingerprint: billFingerprint, RouteSignature: setting.RouteSignature,
	})
	if err == nil && inserted {
		c.logger.Info("tiktok_auto_sml_queued", zap.String("shop_id", shopID), zap.String("order_id", record.OrderID), zap.Int64("config_version", setting.ConfigVersion))
		c.auditEvent("tiktok_auto_sml_queued", "info", models.TikTokAutoSMLJob{ShopID: shopID, OrderID: record.OrderID, TriggerConfigVersion: setting.ConfigVersion}, map[string]interface{}{"trigger_status": record.OrderStatus, "historical_backfill": false})
	}
	return err
}

func (c *TikTokAutoSMLController) Start(ctx context.Context) {
	if c == nil || c.cfg == nil || !c.cfg.TikTokShopAutoSMLEnabled || c.repo == nil {
		return
	}
	if recovered, err := c.repo.RecoverStaleJobs(ctx); err != nil {
		c.logger.Warn("tiktok_auto_sml_recover_failed", zap.Error(err))
	} else if recovered > 0 {
		c.logger.Warn("tiktok_auto_sml_recovered", zap.Int64("jobs", recovered))
	}
	go func() {
		ticker := time.NewTicker(tikTokAutoSMLWorkerEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.processBatch(ctx)
			}
		}
	}()
}

func (c *TikTokAutoSMLController) processBatch(ctx context.Context) {
	jobs, err := c.repo.LeaseJobs(ctx, tikTokAutoSMLBatchSize, tikTokAutoSMLLease)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Warn("tiktok_auto_sml_lease_failed", zap.Error(err))
		}
		return
	}
	for _, job := range jobs {
		c.processJob(ctx, job)
	}
}

func (c *TikTokAutoSMLController) processJob(ctx context.Context, job models.TikTokAutoSMLJob) {
	log := c.logger.With(zap.String("job_id", job.ID), zap.String("shop_id", job.ShopID), zap.String("order_id", job.OrderID), zap.Int("attempt", job.Attempts))
	if c.previewer == nil || c.creator == nil {
		c.failTransient(ctx, job, "automation_unavailable", "ระบบสร้าง Bill TikTok Shop อัตโนมัติยังไม่พร้อม")
		return
	}
	setting, err := c.repo.GetSetting(ctx, job.ShopID)
	if err != nil || setting == nil || !setting.Enabled || setting.PausedReason != "" || setting.ConfigVersion != job.TriggerConfigVersion {
		c.markCancelled(ctx, job, "automation_changed", "การตั้งค่า Auto SML เปลี่ยนก่อนเริ่มส่ง")
		return
	}
	preview, err := c.previewer.Preview(ctx, job.ShopID, job.OrderID)
	if err != nil || preview == nil {
		c.failTransient(ctx, job, "preview_failed", "ตรวจข้อมูลออเดอร์ล่าสุดไม่สำเร็จ")
		return
	}
	status := string(preview.OrderStatus)
	if models.TikTokAutoSMLStopStatus(status) {
		c.markCancelled(ctx, job, "order_cancelled", "ออเดอร์ถูกยกเลิกก่อนส่ง SML")
		return
	}
	if !models.TikTokAutoSMLAllowsStatus(status) {
		c.markNeedsReview(ctx, job, "", "order_status_changed", "สถานะ TikTok Shop ไม่สอดคล้องกับจุดเริ่ม Auto SML: "+status)
		return
	}
	currentFingerprint, fingerprintErr := tikTokAutoSMLBillFingerprint(preview)
	if fingerprintErr != nil || currentFingerprint != job.BillFingerprint {
		c.markNeedsReview(ctx, job, "", "bill_evidence_changed", "สินค้า ยอดเงิน หรือ mapping เปลี่ยนหลังเข้าคิว กรุณาตรวจสอบก่อนส่ง")
		return
	}
	if signature := tikTokAutoSMLRouteSignature(preview.Route); signature == "" || signature != job.RouteSignature || signature != setting.RouteSignature {
		_ = c.repo.PauseForRouteChange(ctx, job.ShopID)
		c.markNeedsReview(ctx, job, "", "route_changed", "เส้นทาง SML เปลี่ยนหลังเข้าคิว กรุณาตรวจสอบก่อนส่ง")
		return
	}
	if preview.ExistingBill == nil && (!preview.ReadyForReviewedBill || len(preview.Blockers) > 0 || preview.ReviewDigest == "") {
		code, message := firstTikTokAutoSMLBlocker(preview.Blockers)
		c.markNeedsReview(ctx, job, "", code, message)
		return
	}
	if setting.EnabledBy == nil || strings.TrimSpace(*setting.EnabledBy) == "" {
		c.markNeedsReview(ctx, job, "", "operator_missing", "ไม่พบผู้เปิด Auto SML กรุณาปิดและเปิดใหม่")
		return
	}
	result, err := c.creator.Create(ctx, tiktokshop.TikTokReviewedBillInput{
		ShopID: job.ShopID, OrderID: job.OrderID, ReviewDigest: preview.ReviewDigest,
		ActorID: strings.TrimSpace(*setting.EnabledBy), TraceID: "tiktok-auto-sml-" + job.ID,
	})
	if err != nil || result == nil || strings.TrimSpace(result.BillID) == "" {
		if errors.Is(err, tiktokshop.ErrTikTokReviewedBillNotReady) || errors.Is(err, tiktokshop.ErrTikTokReviewedBillReviewChanged) || errors.Is(err, tiktokshop.ErrTikTokReviewedBillConflict) {
			c.markNeedsReview(ctx, job, "", "bill_review_required", "ข้อมูลบิลเปลี่ยน กรุณาตรวจสอบก่อนส่ง")
			return
		}
		c.failTransient(ctx, job, "bill_create_failed", "สร้าง Bill TikTok Shop ใน Nexflow ไม่สำเร็จ")
		return
	}
	if err := c.repo.LinkBill(ctx, job.ID, result.BillID, preview.ReviewDigest); err != nil {
		c.failTransient(ctx, job, "job_link_failed", "บันทึกการเชื่อมงานกับ Bill ไม่สำเร็จ")
		return
	}
	if c.billH == nil || c.billH.billRepo == nil {
		c.failTransient(ctx, job, "bill_sender_unavailable", "ระบบส่ง Bill ไป SML ยังไม่พร้อม")
		return
	}
	bill, err := c.billH.billRepo.FindByID(result.BillID)
	if err != nil || bill == nil {
		c.failTransient(ctx, job, "bill_load_failed", "โหลด Bill ก่อนส่ง SML ไม่สำเร็จ")
		return
	}
	if bill.Status == "sent" {
		c.complete(ctx, job, bill)
		return
	}
	if bill.Status != "pending" && bill.Status != "failed" {
		c.markNeedsReview(ctx, job, bill.ID, "bill_status_blocked", "สถานะ Bill ไม่พร้อมส่ง SML: "+bill.Status)
		return
	}
	if c.cfg == nil || !c.cfg.TikTokShopSMLSendEnabled {
		c.markNeedsReview(ctx, job, bill.ID, "sml_send_disabled", "การส่ง TikTok Shop เข้า SML ยังปิดในระดับเซิร์ฟเวอร์")
		return
	}
	latestSetting, settingErr := c.repo.GetSetting(ctx, job.ShopID)
	if settingErr != nil || latestSetting == nil || !latestSetting.Enabled || latestSetting.PausedReason != "" ||
		latestSetting.ConfigVersion != job.TriggerConfigVersion || latestSetting.RouteSignature != job.RouteSignature {
		c.markCancelled(ctx, job, "automation_changed", "การตั้งค่า Auto SML เปลี่ยนก่อนส่ง SML")
		return
	}
	documentTime, err := c.repo.GetOrSetDocumentTime(ctx, job.ID, time.Now().In(tikTokAutoSMLBangkok).Format("15:04"))
	if err != nil {
		c.failTransient(ctx, job, "document_time_failed", "บันทึกเวลาเอกสารไม่สำเร็จ")
		return
	}
	send := c.billH.sendBillToSML(bill, RetryRequest{DocTime: documentTime}, retrySendOptions{
		Context: ctx, TraceID: "tiktok-auto-sml-" + job.ID, Via: "tiktok_auto_sml", SuppressLineAlert: true,
	})
	switch {
	case send.HTTPStatus == http.StatusOK:
		reloaded, _ := c.billH.billRepo.FindByID(bill.ID)
		if reloaded != nil {
			bill = reloaded
		}
		c.complete(ctx, job, bill)
	case send.HTTPStatus == http.StatusAccepted || send.FailureClass == "user_action":
		c.markNeedsReview(ctx, job, bill.ID, "sml_review_required", firstNonEmpty(send.Error, send.Message, "Bill ต้องตรวจสอบก่อนส่ง SML"))
	default:
		c.failTransient(ctx, job, "sml_send_failed", firstNonEmpty(send.Error, send.Message, "ส่ง SML ไม่สำเร็จ"))
	}
	log.Info("tiktok_auto_sml_processed")
}

func (c *TikTokAutoSMLController) complete(ctx context.Context, job models.TikTokAutoSMLJob, bill *models.Bill) {
	docNo := ""
	if bill != nil && bill.SMLDocNo != nil {
		docNo = strings.TrimSpace(*bill.SMLDocNo)
	}
	if bill == nil || docNo == "" {
		c.failTransient(ctx, job, "sml_result_missing", "SML ตอบว่าสำเร็จแต่ไม่พบเลขเอกสาร")
		return
	}
	if err := c.repo.MarkSucceeded(ctx, job.ID, bill.ID, docNo); err != nil {
		c.logger.Warn("tiktok_auto_sml_mark_success_failed", zap.String("job_id", job.ID), zap.Error(err))
	}
	c.auditEvent("tiktok_auto_sml_succeeded", "info", job, map[string]interface{}{"bill_id": bill.ID, "sml_doc_no": docNo})
}

func (c *TikTokAutoSMLController) failTransient(ctx context.Context, job models.TikTokAutoSMLJob, code, message string) {
	if err := c.repo.MarkTransientFailure(ctx, job.ID, code, message, 3); err != nil {
		c.logger.Warn("tiktok_auto_sml_record_failure_failed", zap.String("job_id", job.ID), zap.Error(err))
	}
	c.auditEvent("tiktok_auto_sml_retry_or_failed", "error", job, map[string]interface{}{"error_code": code, "error_message": message, "attempt": job.Attempts})
}

func (c *TikTokAutoSMLController) markNeedsReview(ctx context.Context, job models.TikTokAutoSMLJob, billID, code, message string) {
	if err := c.repo.MarkNeedsReview(ctx, job.ID, billID, code, message); err != nil {
		c.logger.Warn("tiktok_auto_sml_mark_review_failed", zap.String("job_id", job.ID), zap.Error(err))
		return
	}
	c.auditEvent("tiktok_auto_sml_needs_review", "warning", job, map[string]interface{}{"bill_id": billID, "error_code": code, "error_message": message})
}

func (c *TikTokAutoSMLController) markCancelled(ctx context.Context, job models.TikTokAutoSMLJob, code, message string) {
	if err := c.repo.MarkCancelled(ctx, job.ID, code, message); err != nil {
		c.logger.Warn("tiktok_auto_sml_mark_cancelled_failed", zap.String("job_id", job.ID), zap.Error(err))
		return
	}
	c.auditEvent("tiktok_auto_sml_cancelled", "info", job, map[string]interface{}{"reason_code": code, "message": message})
}

func (c *TikTokAutoSMLController) auditEvent(action, level string, job models.TikTokAutoSMLJob, detail map[string]interface{}) {
	if c == nil || c.audit == nil {
		return
	}
	target := strings.TrimSpace(job.OrderID)
	if detail == nil {
		detail = map[string]interface{}{}
	}
	if billID, ok := detail["bill_id"].(string); ok && strings.TrimSpace(billID) != "" {
		target = strings.TrimSpace(billID)
	}
	detail["shop_id"] = job.ShopID
	detail["order_id"] = job.OrderID
	if job.ID != "" {
		detail["job_id"] = job.ID
	}
	if job.TriggerConfigVersion > 0 {
		detail["config_version"] = job.TriggerConfigVersion
	}
	if err := c.audit.Log(models.AuditEntry{Action: action, TargetID: &target, Source: "tiktok_shop", Level: level, Detail: detail}); err != nil {
		c.logger.Warn("tiktok_auto_sml_audit_failed", zap.String("action", action), zap.String("order_id", job.OrderID), zap.Error(err))
	}
}

func firstTikTokAutoSMLBlocker(blockers []tiktokshop.TikTokBillShadowBlocker) (string, string) {
	if len(blockers) == 0 {
		return "bill_not_ready", "Bill ยังไม่พร้อม กรุณาตรวจสอบ mapping ยอดเงิน และเส้นทาง SML"
	}
	return strings.TrimSpace(blockers[0].Code), strings.TrimSpace(blockers[0].Message)
}

func tikTokAutoSMLRouteSignature(route tiktokshop.TikTokBillShadowRoute) string {
	if !route.Ready || !route.ShippingReady || strings.TrimSpace(route.SemanticRoute) == "" || strings.TrimSpace(route.DocFormatCode) == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(route.SemanticRoute), strings.TrimSpace(route.DocFormatCode),
		fmt.Sprint(route.ConfigVersion), strings.TrimSpace(route.ShippingItemCode), strings.TrimSpace(route.ShippingItemUnitCode),
		fmt.Sprint(route.Ready), fmt.Sprint(route.ShippingReady),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func tikTokAutoSMLBillFingerprint(preview *tiktokshop.TikTokBillShadowPreview) (string, error) {
	if preview == nil || strings.TrimSpace(preview.OrderID) == "" || len(preview.Items) == 0 {
		return "", errors.New("TikTok Bill preview is incomplete")
	}
	type fingerprintItem struct {
		ProductID       string `json:"product_id"`
		SKUID           string `json:"sku_id"`
		SellerSKU       string `json:"seller_sku"`
		Quantity        int    `json:"quantity"`
		UnitSalePrice   string `json:"unit_sale_price"`
		LineTotal       string `json:"line_total"`
		MappingStatus   string `json:"mapping_status"`
		ItemCode        string `json:"item_code"`
		UnitCode        string `json:"unit_code"`
		SMLQuantity     string `json:"sml_quantity"`
		MappingRevision int64  `json:"mapping_revision"`
	}
	items := make([]fingerprintItem, 0, len(preview.Items))
	for _, item := range preview.Items {
		items = append(items, fingerprintItem{
			ProductID: item.ProductID, SKUID: item.SKUID, SellerSKU: item.SellerSKU, Quantity: item.Quantity,
			UnitSalePrice: item.UnitSalePrice, LineTotal: item.LineTotal, MappingStatus: item.Mapping.Status,
			ItemCode: item.Mapping.ItemCode, UnitCode: item.Mapping.UnitCode, SMLQuantity: item.Mapping.SMLQuantity,
			MappingRevision: item.Mapping.MappingRevision,
		})
	}
	payload, err := json.Marshal(struct {
		ShopID   string                             `json:"shop_id"`
		OrderID  string                             `json:"order_id"`
		Currency string                             `json:"currency"`
		Amounts  tiktokshop.TikTokBillShadowAmounts `json:"amounts"`
		Items    []fingerprintItem                  `json:"items"`
	}{ShopID: preview.ShopID, OrderID: preview.OrderID, Currency: preview.Currency, Amounts: preview.Amounts, Items: items})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func (h *TikTokShopAPIHandler) AutoSMLSettings(c *gin.Context) {
	enabled, _ := h.readiness()
	if !enabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if h.autoSML == nil {
		h.error(c, http.StatusServiceUnavailable, "auto_sml_not_configured", "ระบบตั้งค่า Auto SML ยังไม่พร้อม")
		return
	}
	settings, err := h.autoSML.ListSettings(c.Request.Context())
	if err != nil {
		h.error(c, http.StatusInternalServerError, "auto_sml_settings_failed", "โหลดการตั้งค่า Auto SML ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"global_enabled":      h.config != nil && h.config.TikTokShopAutoSMLEnabled,
		"trigger_status":      models.TikTokAutoSMLTriggerAwaitingCollection,
		"historical_backfill": false, "settings": settings,
	})
}

func (h *TikTokShopAPIHandler) UpdateAutoSMLSetting(c *gin.Context) {
	shopID := strings.TrimSpace(c.Param("shop_id"))
	if !tiktokshop.ValidTikTokShopID(shopID) || h.autoSML == nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "Shop ID หรือระบบตั้งค่า Auto SML ไม่ถูกต้อง")
		return
	}
	var request struct {
		Enabled               *bool  `json:"enabled"`
		ExpectedConfigVersion int64  `json:"expected_config_version"`
		Confirm               string `json:"confirm"`
	}
	if c.ShouldBindJSON(&request) != nil || request.Enabled == nil || request.ExpectedConfigVersion < 1 {
		h.error(c, http.StatusBadRequest, "invalid_request", "ข้อมูลตั้งค่า Auto SML ไม่ถูกต้อง")
		return
	}
	if *request.Enabled && (h.config == nil || !h.config.TikTokShopAutoSMLEnabled) {
		h.error(c, http.StatusConflict, "auto_sml_global_disabled", "Auto SML ยังปิดในระดับเซิร์ฟเวอร์")
		return
	}
	if *request.Enabled && strings.TrimSpace(request.Confirm) != "ENABLE_TIKTOK_AUTO_SML" {
		h.error(c, http.StatusBadRequest, "confirmation_required", "กรุณายืนยันเปิด Auto SML อีกครั้ง")
		return
	}
	routeSignature := ""
	if *request.Enabled {
		var code, message string
		routeSignature, code, message = h.tikTokAutoSMLPreflight(c.Request.Context(), shopID)
		if code != "" {
			h.error(c, http.StatusConflict, code, message)
			return
		}
	}
	setting, err := h.autoSML.UpdateSetting(c.Request.Context(), repository.TikTokAutoSMLSettingUpdate{
		ShopID: shopID, Enabled: *request.Enabled, ExpectedConfigVersion: request.ExpectedConfigVersion,
		RouteSignature: routeSignature, UserID: strings.TrimSpace(c.GetString("user_id")),
	})
	if errors.Is(err, repository.ErrTikTokAutoSMLConfigConflict) {
		h.error(c, http.StatusConflict, "auto_sml_config_changed", "การตั้งค่าถูกแก้ไข กรุณาโหลดค่าล่าสุด")
		return
	}
	if err != nil {
		h.error(c, http.StatusInternalServerError, "auto_sml_update_failed", "บันทึกการตั้งค่า Auto SML ไม่สำเร็จ")
		return
	}
	if h.audit != nil {
		userID, target := strings.TrimSpace(c.GetString("user_id")), shopID
		_ = h.audit.Log(models.AuditEntry{Action: "tiktok_auto_sml_setting_updated", TargetID: &target, UserID: &userID, Source: "tiktok_shop", Detail: gin.H{"enabled": setting.Enabled, "config_version": setting.ConfigVersion, "eligible_after": setting.EligibleAfter, "historical_backfill": false}})
	}
	c.JSON(http.StatusOK, gin.H{"setting": setting, "message": map[bool]string{true: "เปิด Auto SML สำหรับออเดอร์ใหม่แล้ว", false: "ปิด Auto SML แล้ว"}[*request.Enabled]})
}

func (h *TikTokShopAPIHandler) RetryAutoSML(c *gin.Context) {
	shopID, orderID := strings.TrimSpace(c.Param("shop_id")), strings.TrimSpace(c.Param("order_id"))
	if !tiktokshop.ValidTikTokShopID(shopID) || !tiktokshop.ValidTikTokShopID(orderID) || h.autoSML == nil {
		h.error(c, http.StatusBadRequest, "invalid_request", "Shop ID, Order ID หรือระบบ Auto SML ไม่ถูกต้อง")
		return
	}
	if h.config == nil || !h.config.TikTokShopAutoSMLEnabled {
		h.error(c, http.StatusConflict, "auto_sml_global_disabled", "Auto SML ยังปิดในระดับเซิร์ฟเวอร์")
		return
	}
	if h.billShadow == nil {
		h.error(c, http.StatusServiceUnavailable, "auto_sml_not_configured", "ระบบตรวจข้อมูล Auto SML ยังไม่พร้อม")
		return
	}
	preview, err := h.billShadow.Preview(c.Request.Context(), shopID, orderID)
	if err != nil || preview == nil {
		h.error(c, http.StatusConflict, "order_evidence_unavailable", "ตรวจข้อมูลออเดอร์ล่าสุดไม่สำเร็จ")
		return
	}
	routeSignature := tikTokAutoSMLRouteSignature(preview.Route)
	billFingerprint, fingerprintErr := tikTokAutoSMLBillFingerprint(preview)
	if routeSignature == "" || fingerprintErr != nil {
		h.error(c, http.StatusConflict, "auto_sml_review_required", "ยอดสินค้า mapping หรือเส้นทาง SML ยังไม่พร้อม")
		return
	}
	for _, blocker := range preview.Blockers {
		if blocker.Code != tiktokshop.TikTokBillShadowBlockerExistingBill {
			h.error(c, http.StatusConflict, blocker.Code, blocker.Message)
			return
		}
	}
	if err := h.autoSML.RetryJob(c.Request.Context(), shopID, orderID, billFingerprint, routeSignature); errors.Is(err, sql.ErrNoRows) {
		h.error(c, http.StatusConflict, "auto_sml_retry_blocked", "งานนี้ยังลองใหม่ไม่ได้ กรุณาตรวจว่าร้านเปิด Auto SML และแก้ปัญหาแล้ว")
		return
	} else if err != nil {
		h.error(c, http.StatusInternalServerError, "auto_sml_retry_failed", "นำงานกลับเข้าคิว Auto SML ไม่สำเร็จ")
		return
	}
	if h.audit != nil {
		userID, target := strings.TrimSpace(c.GetString("user_id")), orderID
		_ = h.audit.Log(models.AuditEntry{Action: "tiktok_auto_sml_retried", TargetID: &target, UserID: &userID, Source: "tiktok_shop", Detail: gin.H{"shop_id": shopID, "order_id": orderID}})
	}
	c.JSON(http.StatusOK, gin.H{"message": "นำออเดอร์กลับเข้าคิว Auto SML แล้ว"})
}

func (h *TikTokShopAPIHandler) tikTokAutoSMLPreflight(ctx context.Context, shopID string) (string, string, string) {
	if h.config == nil || !h.config.TikTokShopOrderSyncEnabled || !h.config.TikTokShopWebhookEnabled || !h.config.TikTokShopSMLSendEnabled {
		return "", "automation_dependency_disabled", "Order sync, Webhook หรือการส่ง SML ยังไม่พร้อม"
	}
	if h.syncSettings == nil || h.orderReader == nil || h.billShadow == nil {
		return "", "automation_not_configured", "ระบบตรวจความพร้อม Auto SML ยังตั้งค่าไม่ครบ"
	}
	settings, err := h.syncSettings.ListSettings(ctx)
	if err != nil {
		return "", "order_sync_unavailable", "ตรวจการตั้งค่าซิงก์ออเดอร์ไม่สำเร็จ"
	}
	syncEnabled := false
	for _, setting := range settings {
		if setting.ShopID == shopID && setting.Enabled && setting.LastErrorCode == "" {
			syncEnabled = true
		}
	}
	if !syncEnabled {
		return "", "order_sync_disabled", "ร้านนี้ยังไม่ได้เปิดซิงก์ออเดอร์หรือมีข้อผิดพลาด"
	}
	orders, err := h.orderReader.List(ctx, tiktokshop.TikTokOrderSnapshotListFilter{ShopID: shopID, Page: 1, PageSize: 20})
	if err != nil {
		return "", "order_evidence_unavailable", "อ่านตัวอย่างออเดอร์ไม่สำเร็จ"
	}
	for _, order := range orders.Data {
		if order.BillID != "" {
			continue
		}
		preview, previewErr := h.billShadow.Preview(ctx, shopID, order.OrderID)
		if previewErr != nil || preview == nil || !preview.ReadyForReviewedBill || len(preview.Blockers) > 0 {
			continue
		}
		if signature := tikTokAutoSMLRouteSignature(preview.Route); signature != "" {
			return signature, "", ""
		}
	}
	return "", "controlled_sample_not_ready", "ยังไม่พบออเดอร์ตัวอย่างที่ mapping ยอดเงิน และเส้นทาง SML พร้อมครบ"
}
