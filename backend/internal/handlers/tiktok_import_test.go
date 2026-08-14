package handlers

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestConsumeTikTokPreviewIsUserScopedExpiringAndOneTime(t *testing.T) {
	now := time.Now()
	newHandler := func(uploadedAt time.Time) *TikTokImportHandler {
		h := &TikTokImportHandler{}
		h.pendingUploads.Store("token", &tiktokPendingPreview{
			importRunID: "run-1", userID: "user-1", uploadedAt: uploadedAt,
		})
		return h
	}

	h := newHandler(now)
	if _, ok := h.consumeTikTokPreview("token", "run-1", "user-2", now); ok {
		t.Fatal("preview token must not cross users")
	}
	h = newHandler(now.Add(-tiktokPreviewTTL - time.Second))
	if _, ok := h.consumeTikTokPreview("token", "run-1", "user-1", now); ok {
		t.Fatal("expired preview token must be rejected")
	}
	h = newHandler(now)
	if _, ok := h.consumeTikTokPreview("token", "run-1", "user-1", now); !ok {
		t.Fatal("valid preview token was rejected")
	}
	if _, ok := h.consumeTikTokPreview("token", "run-1", "user-1", now); ok {
		t.Fatal("preview token must be one-time")
	}
}

func TestParseDecimalCentsDoesNotDependOnBinaryFloat(t *testing.T) {
	tests := map[string]int64{
		"4,990.00": 499000,
		"0.105":    11,
		"-18.00":   -1800,
		"421":      42100,
	}
	for input, want := range tests {
		got, err := parseDecimalCents(strings.ReplaceAll(input, ",", ""))
		if err != nil || got != want {
			t.Fatalf("parseDecimalCents(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
}

func TestParseTikTokCSVGroupsOrdersAndSkipsNonShippedStatuses(t *testing.T) {
	csvData := "\ufeffOrder ID,Order Status,SKU ID,Seller SKU,Product Name,Variation,Quantity,SKU Subtotal Before Discount,SKU Platform Discount,SKU Seller Discount,SKU Subtotal After Discount,Shipping Fee After Discount,Order Amount,Created Time,Paid Time,Tracking ID,Payment Method,Buyer Username\n" +
		"583870900000000001\t,จัดส่งแล้ว,SKU-001\t,AH001,Serum A,30ml,2,180,30,0,150,38,238,10/05/2026 22:05:43\t,10/05/2026 22:06:01\t,TH123,COD,buyer1\n" +
		"583870900000000001\t,จัดส่งแล้ว,SKU-002\t,AH002,Serum B,50ml,1,70,20,0,50,38,238,10/05/2026 22:05:43\t,10/05/2026 22:06:01\t,TH123,COD,buyer1\n" +
		"583870900000000002\t,ยกเลิกแล้ว,SKU-003\t,AH003,Canceled,Default,1,29,0,0,29,38,67,10/05/2026 12:00:00\t,,TH999,COD,buyer2\n" +
		"583870900000000003\t,ที่จะจัดส่ง,SKU-004\t,AH004,Pending,Default,1,29,0,0,29,38,67,10/05/2026 13:00:00\t,,TH888,COD,buyer3\n"

	orders, warnings, skipped, err := parseTikTokCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("parseTikTokCSV() error = %v", err)
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2", skipped)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "ข้าม 2 แถว") {
		t.Fatalf("warnings = %#v, want skip warning", warnings)
	}
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
	o := orders[0]
	if o.OrderID != "583870900000000001" {
		t.Fatalf("OrderID = %q", o.OrderID)
	}
	if o.DocDate != "2026-05-10" {
		t.Fatalf("DocDate = %q, want 2026-05-10", o.DocDate)
	}
	if o.ItemCount != 2 || o.TotalQty != 3 {
		t.Fatalf("ItemCount/TotalQty = %d/%v, want 2/3", o.ItemCount, o.TotalQty)
	}
	if o.PaidAmount != 238 {
		t.Fatalf("PaidAmount = %v, want 238 (order amount must not double count multi-row orders)", o.PaidAmount)
	}
	if o.ShippingAmount != 38 {
		t.Fatalf("ShippingAmount = %v, want 38", o.ShippingAmount)
	}
	if o.Items[0].SKU != "AH001" || o.Items[0].TikTokSKU != "SKU-001" {
		t.Fatalf("first item SKU/TikTokSKU = %q/%q", o.Items[0].SKU, o.Items[0].TikTokSKU)
	}
	if o.Items[0].Price != 90 || o.Items[0].GrossAmount != 180 || o.Items[0].DiscountAmount != 30 {
		t.Fatalf("first item amounts = price %v gross %v discount %v", o.Items[0].Price, o.Items[0].GrossAmount, o.Items[0].DiscountAmount)
	}
}

func TestTikTokGoldenSalesInvoiceTotals(t *testing.T) {
	file, err := os.Open("testdata/tiktok_sales_invoice_golden.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	orders, _, skipped, err := parseTikTokCSV(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 20 || skipped != 1 {
		t.Fatalf("eligible/skipped = %d/%d, want 20/1", len(orders), skipped)
	}
	var gross, discount, net, mismatchTotal float64
	mismatches := 0
	for _, order := range orders {
		gross += order.ItemGrossAmount
		discount += order.DiscountAmount
		net += order.NetProductAmount
		if order.AmountMismatch {
			mismatches++
			mismatchTotal += order.AmountDifference
		}
	}
	if roundFloat(gross, 2) != 4990 || roundFloat(discount, 2) != 421 || roundFloat(net, 2) != 4569 {
		t.Fatalf("totals = gross %.2f discount %.2f net %.2f", gross, discount, net)
	}
	if mismatches != 2 || roundFloat(mismatchTotal, 2) != 14.34 {
		t.Fatalf("mismatches = %d total %.2f, want 2/14.34", mismatches, mismatchTotal)
	}
}

func TestParseTikTokPreservesGrossWhenQtyDoesNotDivideEvenly(t *testing.T) {
	csvData := "Order ID,Order Status,SKU ID,Seller SKU,Product Name,Variation,Quantity,SKU Subtotal Before Discount,SKU Platform Discount,SKU Seller Discount,SKU Subtotal After Discount,Shipping Fee After Discount,Order Amount,Created Time\n" +
		"TT-ROUND,Completed,V-1,AH001,สินค้า,Default,3,100.00,0.01,0,99.99,0,99.99,14/08/2026 10:00:00\n"
	orders, _, _, err := parseTikTokCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || len(orders[0].Items) != 1 {
		t.Fatalf("orders/items = %d/%d", len(orders), len(orders[0].Items))
	}
	item := orders[0].Items[0]
	if item.GrossAmount != 100 || item.DiscountAmount != 0.01 {
		t.Fatalf("gross source lost: price %.8f qty %.2f gross %.2f discount %.2f", item.Price, item.Qty, item.GrossAmount, item.DiscountAmount)
	}
}

func TestParseTikTokBlocksInconsistentRepeatedOrderAmounts(t *testing.T) {
	csvData := "Order ID,Order Status,SKU ID,Seller SKU,Product Name,Variation,Quantity,SKU Subtotal Before Discount,SKU Platform Discount,SKU Seller Discount,SKU Subtotal After Discount,Shipping Fee After Discount,Order Amount,Created Time\n" +
		"TT-INCONSISTENT,Completed,V-1,AH001,สินค้า A,Default,1,100,0,0,100,10,110,14/08/2026 10:00:00\n" +
		"TT-INCONSISTENT,Completed,V-2,AH002,สินค้า B,Default,1,50,0,0,50,20,170,14/08/2026 10:00:00\n"
	orders, _, _, err := parseTikTokCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || !strings.Contains(orders[0].BlockedReason, "ไม่ตรงกันหลายแถว") {
		t.Fatalf("blocked reason = %q", orders[0].BlockedReason)
	}
}

func TestParseTikTokBlocksDiscountGreaterThanGross(t *testing.T) {
	csvData := "Order ID,Order Status,SKU ID,Seller SKU,Product Name,Variation,Quantity,SKU Subtotal Before Discount,SKU Platform Discount,SKU Seller Discount,SKU Subtotal After Discount,Shipping Fee After Discount,Order Amount,Created Time\n" +
		"TT-DISCOUNT,Completed,V-1,AH001,สินค้า,Default,1,100,101,0,0,0,0,14/08/2026 10:00:00\n"
	orders, _, _, err := parseTikTokCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || !strings.Contains(orders[0].BlockedReason, "ส่วนลดสินค้ามากกว่ายอดเต็ม") {
		t.Fatalf("blocked reason = %q", orders[0].BlockedReason)
	}
}

func TestParseTikTokBlocksMixedEligibleAndCanceledRowsInOneOrder(t *testing.T) {
	csvData := "Order ID,Order Status,SKU ID,Seller SKU,Product Name,Variation,Quantity,SKU Subtotal Before Discount,SKU Platform Discount,SKU Seller Discount,SKU Subtotal After Discount,Shipping Fee After Discount,Order Amount,Created Time\n" +
		"TT-MIXED,Completed,V-1,AH001,สินค้า A,Default,1,100,0,0,100,0,100,14/08/2026 10:00:00\n" +
		"TT-MIXED,Canceled,V-2,AH002,สินค้า B,Default,1,50,0,0,50,0,100,14/08/2026 10:00:00\n"
	orders, _, skipped, err := parseTikTokCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(orders) != 1 || !strings.Contains(orders[0].BlockedReason, "ยกเลิก/คืนสินค้า") {
		t.Fatalf("skipped/orders/reason = %d/%d/%q", skipped, len(orders), orders[0].BlockedReason)
	}
}
