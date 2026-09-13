package handlers

import (
	"encoding/json"
	"strings"

	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
)

type smlSendPolicy struct {
	Allowed bool
	Code    string
	Message string
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

func (h *BillHandler) logTikTokShopSMLSendBlocked(bill *models.Bill, opts retrySendOptions) {
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
			"reason": "tiktok_shop_sml_send_disabled",
		},
	})
}
