package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/tiktokshop"
)

type tikTokCancellationCreateRequest struct {
	Confirm      string `json:"confirm"`
	ReviewDigest string `json:"review_digest"`
}

func (h *TikTokShopAPIHandler) PreviewCancellation(c *gin.Context) {
	shopID, orderID := strings.TrimSpace(c.Param("shop_id")), strings.TrimSpace(c.Param("order_id"))
	if h == nil || h.config == nil || !h.config.TikTokShopOpenAPIEnabled {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิด TikTok Shop Open API")
		return
	}
	if !tiktokshop.ValidTikTokShopID(shopID) || !tiktokshop.ValidTikTokShopID(orderID) {
		h.error(c, http.StatusBadRequest, "invalid_request", "Shop ID หรือ Order ID ของ TikTok Shop ไม่ถูกต้อง")
		return
	}
	if h.cancellation == nil {
		h.error(c, http.StatusServiceUnavailable, "cancellation_not_configured", "ระบบตรวจเอกสารยกเลิก TikTok Shop ยังไม่พร้อม")
		return
	}
	result, err := h.cancellation.Preview(c.Request.Context(), shopID, orderID, c.GetString("user_id"))
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			h.error(c, http.StatusNotFound, "snapshot_not_found", "ไม่พบ Snapshot ของออเดอร์ TikTok Shop นี้")
		case errors.Is(err, errTikTokCancellationEvidenceBlocked):
			h.error(c, http.StatusUnprocessableEntity, "cancellation_not_eligible", "หลักฐานใบขายเดิมยังไม่พร้อมสำหรับเอกสารยกเลิก SML")
		case errors.Is(err, errTikTokCancellationRouteBlocked):
			h.error(c, http.StatusUnprocessableEntity, "cancellation_route_not_ready", "เส้นทางเอกสารยกเลิก TikTok Shop ยังไม่พร้อม")
		default:
			h.logger.Error("tiktok_shop_cancellation_preview_failed", zap.String("shop_id", shopID), zap.String("order_id", orderID), zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
			h.error(c, http.StatusBadGateway, "cancellation_preview_failed", "ตรวจตัวอย่างเอกสารยกเลิก TikTok Shop ไม่สำเร็จ")
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TikTokShopAPIHandler) CreateCancellation(c *gin.Context) {
	shopID, orderID := strings.TrimSpace(c.Param("shop_id")), strings.TrimSpace(c.Param("order_id"))
	var request tikTokCancellationCreateRequest
	if !tiktokshop.ValidTikTokShopID(shopID) || !tiktokshop.ValidTikTokShopID(orderID) ||
		c.ShouldBindJSON(&request) != nil || request.Confirm != "CREATE_TIKTOK_SML_CANCEL_DOCUMENT" || !validLowerSHA256(request.ReviewDigest) {
		h.error(c, http.StatusBadRequest, "invalid_request", "กรุณาตรวจตัวอย่างล่าสุดและยืนยันสร้างเอกสารยกเลิกอีกครั้ง")
		return
	}
	actorID := strings.TrimSpace(c.GetString("user_id"))
	if actorID == "" {
		h.error(c, http.StatusUnauthorized, "session_expired", "กรุณาเข้าสู่ระบบใหม่")
		return
	}
	if h == nil || h.config == nil || !h.config.TikTokShopOpenAPIEnabled || h.cancellation == nil {
		h.error(c, http.StatusNotFound, "feature_disabled", "Tenant นี้ยังไม่ได้เปิดการสร้างเอกสารยกเลิก TikTok Shop")
		return
	}
	result, err := h.cancellation.Create(c.Request.Context(), TikTokCancellationCreateInput{
		ShopID: shopID, OrderID: orderID, ReviewDigest: request.ReviewDigest,
		ActorID: actorID, TraceID: c.GetString("trace_id"),
	})
	if err != nil {
		status, code, message := http.StatusInternalServerError, "cancellation_create_failed", "สร้างเอกสารยกเลิก TikTok Shop ไม่สำเร็จ"
		switch {
		case errors.Is(err, errTikTokCancellationFeatureDisabled):
			status, code, message = http.StatusForbidden, "feature_flag_disabled", "การสร้างเอกสารยกเลิก TikTok Shop ยังปิดระหว่างรอ App Review และ Webhook"
		case errors.Is(err, errTikTokCancellationReviewChanged):
			status, code, message = http.StatusConflict, "cancellation_review_changed", "หลักฐานออเดอร์ ใบขาย หรือเส้นทาง SML เปลี่ยน กรุณาตรวจตัวอย่างใหม่"
		case errors.Is(err, errTikTokCancellationBusy):
			status, code, message = http.StatusConflict, "cancellation_already_running", "รายการนี้กำลังสร้างเอกสารยกเลิก กรุณารอสักครู่"
		case errors.Is(err, errTikTokCancellationReconciliation):
			status, code, message = http.StatusConflict, "cancellation_requires_reconciliation", "ผลจาก SML ยังไม่แน่ชัด ระบบจะไม่ออกเลขใหม่จนกว่าจะตรวจสอบเอกสารเดิม"
		case errors.Is(err, errTikTokCancellationEvidenceBlocked):
			status, code, message = http.StatusUnprocessableEntity, "cancellation_not_eligible", "หลักฐานใบขายเดิมยังไม่พร้อมสำหรับเอกสารยกเลิก SML"
		case errors.Is(err, errTikTokCancellationRouteBlocked):
			status, code, message = http.StatusUnprocessableEntity, "cancellation_route_not_ready", "เส้นทางเอกสารยกเลิก TikTok Shop ยังไม่พร้อม"
		}
		h.auditTikTokCancellation(actorID, shopID, orderID, "tiktok_shop_sml_cancel_blocked", "warning", map[string]any{"code": code})
		if status >= 500 {
			h.logger.Error("tiktok_shop_cancellation_create_failed", zap.String("shop_id", shopID), zap.String("order_id", orderID), zap.String("trace_id", c.GetString("trace_id")), zap.Error(err))
		}
		h.error(c, status, code, message)
		return
	}
	h.auditTikTokCancellation(actorID, shopID, orderID, "tiktok_shop_sml_cancel_created", "info", map[string]any{
		"attempt_id": result.ID, "bill_id": result.BillID, "sale_sml_doc_no": result.SaleSMLDocNo,
		"cancel_sml_doc_no": result.CancelSMLDocNo, "status": result.Status,
	})
	c.JSON(http.StatusOK, gin.H{"data": tikTokCancellationResult(result), "stock_recalculation_queued": result.StockRecalcStatus == "pending"})
}

func (h *TikTokShopAPIHandler) auditTikTokCancellation(actorID, shopID, orderID, action, level string, detail map[string]any) {
	if h == nil || h.audit == nil {
		return
	}
	detail["shop_id"], detail["order_id"] = shopID, orderID
	var userID *string
	if actorID = strings.TrimSpace(actorID); actorID != "" {
		userID = &actorID
	}
	_ = h.audit.Log(models.AuditEntry{Action: action, UserID: userID, Source: "tiktok_shop_api", Level: level, Detail: detail})
}
