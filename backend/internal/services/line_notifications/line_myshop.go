package linenotify

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"nexflow/internal/models"
)

func (s *Service) EnqueueLineMyShopOrder(ctx context.Context, snap *models.LineMyShopOrderSnapshot, dedupeKey string) (int, error) {
	if s == nil || s.repo == nil || snap == nil {
		return 0, nil
	}
	orderNo := strings.TrimSpace(snap.OrderNo)
	if orderNo == "" {
		return 0, nil
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = fmt.Sprintf("line_myshop:order:%s:%s", strings.TrimSpace(snap.ConnectionID), orderNo)
	}
	return s.repo.Enqueue(ctx, models.LineNotificationMessageInput{
		Source:      models.LineMyShopSource,
		Severity:    lineMyShopNotificationSeverity(snap),
		Title:       "มีออเดอร์ LINE MyShop ใหม่",
		Body:        lineMyShopNotificationBody(snap),
		ActionURL:   LineMyShopOrderActionURL(s.publicBaseURL, orderNo, lineMyShopBillID(snap)),
		EntityType:  "line_myshop_order",
		EntityID:    fmt.Sprintf("%s:%s", strings.TrimSpace(snap.ConnectionID), orderNo),
		DedupeKey:   dedupeKey,
		MessageText: BuildLineMyShopOrderLineText(snap, s.publicBaseURL),
	})
}

func BuildLineMyShopOrderLineText(snap *models.LineMyShopOrderSnapshot, publicBaseURL string) string {
	if snap == nil {
		return ""
	}
	lines := []string{
		"LINE MyShop: มีออเดอร์ใหม่",
		"Order: " + strings.TrimSpace(snap.OrderNo),
	}
	if account := strings.TrimSpace(snap.ConnectionName); account != "" {
		lines = append(lines, "Account: "+account)
	}
	status := lineMyShopStatusLine(snap)
	if status != "" {
		lines = append(lines, status)
	}
	if snap.ItemCount > 0 {
		lines = append(lines, fmt.Sprintf("Items: %d", snap.ItemCount))
	}
	if snap.TotalAmount > 0 {
		lines = append(lines, fmt.Sprintf("Total: %.2f", snap.TotalAmount))
	}
	action := LineMyShopOrderActionURL(publicBaseURL, strings.TrimSpace(snap.OrderNo), lineMyShopBillID(snap))
	if action != "" {
		lines = append(lines, "เปิดใน Nexflow: "+action)
	}
	return strings.Join(lines, "\n")
}

func LineMyShopOrderActionURL(publicBaseURL, orderNo, billID string) string {
	path := "/sale-invoices?source=line_myshop"
	if billID = strings.TrimSpace(billID); billID != "" {
		path = "/bills/" + url.PathEscape(billID)
	} else if orderNo = strings.TrimSpace(orderNo); orderNo != "" {
		path += "&search=" + url.QueryEscape(orderNo)
	}
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
}

func lineMyShopBillID(snap *models.LineMyShopOrderSnapshot) string {
	if snap == nil || snap.BillID == nil {
		return ""
	}
	return strings.TrimSpace(*snap.BillID)
}

func lineMyShopNotificationBody(snap *models.LineMyShopOrderSnapshot) string {
	if snap == nil {
		return ""
	}
	parts := []string{}
	if snap.PaymentStatus != "" {
		parts = append(parts, "ชำระเงิน "+snap.PaymentStatus)
	}
	if snap.ShipmentStatus != "" {
		parts = append(parts, "จัดส่ง "+snap.ShipmentStatus)
	}
	if snap.ItemCount > 0 {
		parts = append(parts, fmt.Sprintf("%d รายการ", snap.ItemCount))
	}
	if snap.TotalAmount > 0 {
		parts = append(parts, fmt.Sprintf("%.2f บาท", snap.TotalAmount))
	}
	return strings.Join(parts, " · ")
}

func lineMyShopNotificationSeverity(snap *models.LineMyShopOrderSnapshot) string {
	if snap == nil {
		return "info"
	}
	if strings.EqualFold(strings.TrimSpace(snap.OrderStatus), "CANCELED") {
		return "warning"
	}
	return "info"
}

func lineMyShopStatusLine(snap *models.LineMyShopOrderSnapshot) string {
	parts := []string{}
	if snap.OrderStatus != "" {
		parts = append(parts, "order="+snap.OrderStatus)
	}
	if snap.PaymentStatus != "" {
		parts = append(parts, "payment="+snap.PaymentStatus)
	}
	if snap.ShipmentStatus != "" {
		parts = append(parts, "shipment="+snap.ShipmentStatus)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Status: " + strings.Join(parts, " / ")
}
