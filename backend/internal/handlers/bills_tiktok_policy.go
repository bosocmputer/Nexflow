package handlers

import (
	"context"
	"encoding/json"
	"strings"

	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/services/tiktokshop"
)

type smlSendPolicy struct {
	Allowed     bool
	Code        string
	Message     string
	OrderStatus string
}

func billSMLSendPolicy(cfg *config.Config, bill *models.Bill) smlSendPolicy {
	if !tikTokShopSMLSendBlocked(cfg, bill) {
		return smlSendPolicy{Allowed: true}
	}
	return smlSendPolicy{
		Allowed: false,
		Code:    "tiktok_shop_sml_send_disabled",
		Message: "Bill TikTok Shop ใบนี้อยู่ระหว่างตรวจ UAT และยังไม่อนุญาตให้ส่งเข้า SML",
	}
}

// A reviewed Bill stores the order state at creation time. Sending must use
// the current local snapshot, which webhook/poll reconciliation updates.
func (h *BillHandler) currentTikTokShopSendPolicy(ctx context.Context, bill *models.Bill) smlSendPolicy {
	policy := billSMLSendPolicy(h.cfg, bill)
	if !isTikTokShopReviewedBill(bill) {
		return policy
	}
	unavailable := smlSendPolicy{Code: "tiktok_shop_order_status_unavailable", Message: "ตรวจสถานะคำสั่งซื้อ TikTok Shop ล่าสุดไม่ได้ จึงยังไม่ส่งใบขายเข้า SML"}
	shopID, orderID := tikTokShopBillAuditIdentity(bill)
	if h.billRepo == nil || shopID == "" || orderID == "" || bill.SourceAccountKey != "shop:"+shopID ||
		(bill.SMLOrderID != "" && strings.TrimSpace(bill.SMLOrderID) != orderID) {
		if !policy.Allowed {
			return policy
		}
		return unavailable
	}
	status, err := h.billRepo.TikTokShopOrderStatus(ctx, shopID, orderID)
	if err != nil || status == "" {
		if !policy.Allowed {
			return policy
		}
		return unavailable
	}
	if status == "CANCELLED" {
		message := "คำสั่งซื้อ TikTok Shop ยกเลิกแล้ว ใบขายเดิมยังไม่ส่ง SML จึงไม่ต้องสร้างเอกสารยกเลิก และห้ามส่งใบขายเดิม"
		if bill.Status == "sent" {
			message = "คำสั่งซื้อ TikTok Shop ยกเลิกหลังส่ง SML แล้ว ให้ตรวจเอกสารยกเลิกจากคิวยกเลิก TikTok Shop"
		} else if bill.CurrentSMLAttemptID != nil {
			message = "คำสั่งซื้อ TikTok Shop ยกเลิกแล้ว แต่มีประวัติเริ่มส่ง SML ต้องตรวจผลเดิมก่อน ห้ามส่งซ้ำหรือสร้างเอกสารยกเลิกทันที"
		}
		return smlSendPolicy{Code: "tiktok_shop_order_cancelled", OrderStatus: status,
			Message: message}
	}
	if !policy.Allowed {
		policy.OrderStatus = status
		return policy
	}
	if !tiktokshop.TikTokBillLifecycleReady(tiktokshop.OrderStatus(status)) {
		return smlSendPolicy{Code: "tiktok_shop_order_not_sendable", OrderStatus: status,
			Message: "สถานะคำสั่งซื้อ TikTok Shop ล่าสุดยังไม่พร้อมส่งใบขายเข้า SML"}
	}
	return smlSendPolicy{Allowed: true, OrderStatus: status}
}

func tikTokShopBillAuditIdentity(bill *models.Bill) (shopID, orderID string) {
	if !isTikTokShopReviewedBill(bill) {
		return "", ""
	}
	var raw struct {
		ShopID        string `json:"tiktok_shop_id"`
		OrderID       string `json:"tiktok_order_id"`
		FallbackOrder string `json:"order_id"`
	}
	if err := json.Unmarshal(bill.RawData, &raw); err != nil {
		return "", ""
	}
	orderID = strings.TrimSpace(raw.OrderID)
	if orderID == "" {
		orderID = strings.TrimSpace(raw.FallbackOrder)
	}
	return strings.TrimSpace(raw.ShopID), orderID
}

func billTotalForAudit(bill *models.Bill) float64 {
	if bill == nil {
		return 0
	}
	var total float64
	for _, item := range bill.Items {
		amount := 0.0
		if item.Price != nil {
			amount = item.Qty * *item.Price
		}
		if item.GrossAmount != nil {
			amount = *item.GrossAmount
		}
		amount -= item.DiscountAmount
		if amount > 0 {
			total += amount
		}
	}
	return total
}

func (h *BillHandler) logTikTokShopSMLSendBlocked(bill *models.Bill, opts retrySendOptions, reason string) {
	if h == nil || bill == nil {
		return
	}
	shopID, orderID := tikTokShopBillAuditIdentity(bill)
	if h.log != nil {
		h.log.Warn("tiktok_shop_sml_send_blocked",
			zap.String("bill_id", bill.ID), zap.String("shop_id", shopID), zap.String("order_id", orderID),
			zap.String("via", opts.Via), zap.String("actor_id", opts.UserID), zap.String("trace_id", opts.TraceID))
	}
	if h.auditRepo == nil {
		return
	}
	billID := bill.ID
	var userID *string
	if value := strings.TrimSpace(opts.UserID); value != "" {
		userID = &value
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action: "tiktok_shop_sml_send_blocked", TargetID: &billID, UserID: userID,
		Source: "tiktok_shop", Level: "warn", TraceID: opts.TraceID,
		Detail: map[string]interface{}{
			"shop_id": shopID, "order_id": orderID, "items_count": len(bill.Items),
			"total_amount": billTotalForAudit(bill), "via": opts.Via,
			"reason": reason,
		},
	})
}

func (h *BillHandler) logTikTokShopSMLProfileBlocked(bill *models.Bill, opts retrySendOptions, stage string) {
	if h == nil || bill == nil || !isTikTokShopReviewedBill(bill) {
		return
	}
	shopID, orderID := tikTokShopBillAuditIdentity(bill)
	if h.log != nil {
		h.log.Warn("tiktok_shop_sml_profile_blocked", zap.String("bill_id", bill.ID),
			zap.String("shop_id", shopID), zap.String("order_id", orderID), zap.String("stage", stage),
			zap.String("via", opts.Via), zap.String("trace_id", opts.TraceID))
	}
	if h.auditRepo == nil {
		return
	}
	billID := bill.ID
	var userID *string
	if value := strings.TrimSpace(opts.UserID); value != "" {
		userID = &value
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action: "tiktok_shop_sml_profile_blocked", TargetID: &billID, UserID: userID,
		Source: "tiktok_shop", Level: "warn", TraceID: opts.TraceID,
		Detail: map[string]interface{}{
			"shop_id": shopID, "order_id": orderID, "via": opts.Via,
			"stage": stage, "reason": "shipment_recipient_unavailable",
		},
	})
}
