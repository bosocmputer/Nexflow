package handlers

import (
	"encoding/json"
	"testing"

	"nexflow/internal/models"
)

func TestMarketplaceAmountReviewAuditDetailExplainsSMLAuthority(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"order_total_amount":        256.42,
		"item_gross_amount":         250.00,
		"platform_discount_amount":  0.00,
		"seller_discount_amount":    0.00,
		"net_product_amount":        250.00,
		"shipping_amount":           0.00,
		"taxes_amount":              0.00,
		"payment_discount_amount":   0.00,
		"amount_difference":         6.42,
		"amount_review_reason":      "Order Amount ไม่ตรงกับยอดสินค้า ค่าส่ง ภาษี และส่วนลดชำระเงิน",
		"amount_review_required":    true,
		"amount_source_fingerprint": "source-fingerprint",
	})
	if err != nil {
		t.Fatal(err)
	}
	gross := 250.00
	price := 250.00
	bill := &models.Bill{Source: "tiktok", RawData: raw, Items: []models.BillItem{{
		Qty: 1, Price: &price, GrossAmount: &gross,
	}}}

	detail := marketplaceAmountReviewAuditDetail(bill, "bill-fingerprint")
	assertAmountReviewNumber(t, detail, "marketplace_order_amount", 256.42)
	assertAmountReviewNumber(t, detail, "marketplace_itemized_amount", 250.00)
	assertAmountReviewNumber(t, detail, "sml_document_amount", 250.00)
	assertAmountReviewNumber(t, detail, "unallocated_marketplace_amount", 6.42)
	if detail["sml_amount_authority"] != "bill_items" {
		t.Fatalf("authority=%v, want bill_items", detail["sml_amount_authority"])
	}
	if detail["unallocated_amount_kind"] != "tiktok_buyer_protection_or_unitemized_charge" {
		t.Fatalf("unallocated kind=%v", detail["unallocated_amount_kind"])
	}
	if detail["fingerprint"] != "bill-fingerprint" {
		t.Fatalf("fingerprint=%v", detail["fingerprint"])
	}
}

func TestMarketplaceAmountReviewAuditDetailUsesCurrentEditedBillItems(t *testing.T) {
	raw := json.RawMessage(`{"order_total_amount":256.42,"net_product_amount":250,"shipping_amount":0,"taxes_amount":0,"payment_discount_amount":0,"amount_difference":6.42}`)
	productGross, productPrice := 250.0, 250.0
	shippingGross, shippingPrice := 15.0, 15.0
	bill := &models.Bill{Source: "tiktok", RawData: raw, Items: []models.BillItem{
		{Qty: 1, Price: &productPrice, GrossAmount: &productGross},
		{Qty: 1, Price: &shippingPrice, GrossAmount: &shippingGross},
	}}

	detail := marketplaceAmountReviewAuditDetail(bill, "edited")
	assertAmountReviewNumber(t, detail, "sml_document_amount", 265.00)
	assertAmountReviewNumber(t, detail, "marketplace_itemized_amount", 250.00)
}

func assertAmountReviewNumber(t *testing.T, detail map[string]any, key string, want float64) {
	t.Helper()
	got, ok := detail[key].(float64)
	if !ok || got != want {
		t.Fatalf("%s=%v, want %.2f", key, detail[key], want)
	}
}
