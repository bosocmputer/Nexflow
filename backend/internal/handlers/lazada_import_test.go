package handlers

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestConsumeLazadaPreviewIsUserScopedExpiringAndOneTime(t *testing.T) {
	h := &LazadaImportHandler{}
	now := time.Now()
	h.pendingUploads.Store("valid", &lazadaPendingPreview{importRunID: "run-1", userID: "user-1", uploadedAt: now})

	if _, ok := h.consumeLazadaPreview("valid", "run-1", "user-2", now); ok {
		t.Fatal("preview must not be consumable by another user")
	}
	if _, ok := h.consumeLazadaPreview("valid", "run-1", "user-1", now); ok {
		t.Fatal("failed ownership check must still consume the one-time token")
	}

	h.pendingUploads.Store("expired", &lazadaPendingPreview{importRunID: "run-1", userID: "user-1", uploadedAt: now.Add(-lazadaPreviewTTL - time.Second)})
	if _, ok := h.consumeLazadaPreview("expired", "run-1", "user-1", now); ok {
		t.Fatal("expired preview must be rejected")
	}

	h.pendingUploads.Store("once", &lazadaPendingPreview{importRunID: "run-1", userID: "user-1", uploadedAt: now})
	if _, ok := h.consumeLazadaPreview("once", "run-1", "user-1", now); !ok {
		t.Fatal("valid preview was rejected")
	}
	if _, ok := h.consumeLazadaPreview("once", "run-1", "user-1", now); ok {
		t.Fatal("preview token must be one-time use")
	}
}

func TestParseLazadaExcelGroupsOrdersAndSkipsReturnStatuses(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{
		"orderItemId", "sellerSku", "lazadaSku", "createTime", "orderNumber",
		"customerName", "payMethod", "paidPrice", "unitPrice", "shippingFee",
		"itemName", "variation", "trackingCode", "status", "sellerDiscountTotal",
		"platformDiscountTotal", "walletCredit", "bundleDiscount",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			t.Fatal(err)
		}
	}
	rows := [][]interface{}{
		{"LI-1", "SKU-A", "LZ-A", "10 May 2026 06:23", "LZ-100", "Buyer A", "COD", "130.00", "150.00", "10.00", "Serum", "30ml", "TH123", "shipped", "-30.00", 0, 0, 0},
		{"LI-2", "SKU-A", "LZ-A", "10 May 2026 06:23", "LZ-100", "Buyer A", "COD", "120.00", "150.00", "0.00", "Serum", "30ml", "TH123", "shipped", "-30.00", 0, 0, 0},
		{"LI-3", "SKU-B", "LZ-B", "09 May 2026 12:00", "LZ-101", "Buyer B", "Card", "88.50", "99.00", "0.00", "Mask", "", "TH456", "delivered", "-10.50", 0, 0, 0},
		{"LI-4", "SKU-C", "LZ-C", "08 May 2026 12:00", "LZ-102", "Buyer C", "Card", "77.00", "77.00", "0.00", "Return Item", "", "TH789", "In Transit: Returning to seller", "0.00", 0, 0, 0},
	}
	for r, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}

	orders, warnings, skipped, err := parseLazadaExcel(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseLazadaExcel() error = %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1; warnings=%v", skipped, warnings)
	}
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(orders))
	}
	if orders[0].OrderID != "LZ-100" || orders[0].DocDate != "2026-05-10" {
		t.Fatalf("first order = %#v", orders[0])
	}
	if len(orders[0].Items) != 1 || orders[0].Items[0].Qty != 2 {
		t.Fatalf("first order items = %#v, want one aggregated qty=2", orders[0].Items)
	}
	if orders[0].PaidAmount != 250 || orders[0].ItemGrossAmount != 300 || orders[0].DiscountAmount != 60 || orders[0].ShippingAmount != 10 {
		t.Fatalf("amounts = paid %.2f gross %.2f discount %.2f shipping %.2f", orders[0].PaidAmount, orders[0].ItemGrossAmount, orders[0].DiscountAmount, orders[0].ShippingAmount)
	}
}

func TestParseLazadaExcelGoldenAmounts(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{
		"orderItemId", "sellerSku", "lazadaSku", "createTime", "orderNumber", "paidPrice", "unitPrice", "shippingFee",
		"itemName", "status", "sellerDiscountTotal", "platformDiscountTotal", "walletCredit", "bundleDiscount",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, header)
	}
	// Seven eligible orders. The split adjustments reproduce the verified
	// accounting totals without retaining any buyer data from the source file.
	rows := [][]any{
		{"L-1", "SKU-1", "V-1", "10 Aug 2026 09:00", "LZ-1", 490.00, 500.00, 10.00, "สินค้า 1", "shipped", -10.00, -10.00, 0, 0},
		{"L-2", "SKU-2", "V-2", "10 Aug 2026 09:01", "LZ-2", 405.00, 400.00, 10.00, "สินค้า 2", "delivered", -5.00, 0, 0, 0},
		{"L-3", "SKU-3", "V-3", "10 Aug 2026 09:02", "LZ-3", 397.93, 400.00, 5.00, "สินค้า 3", "confirmed", 0, -7.07, 0, 0},
		{"L-4", "SKU-4", "V-4", "10 Aug 2026 09:03", "LZ-4", 340.00, 300.00, 40.00, "สินค้า 4", "shipped", 0, 0, 0, 0},
		{"L-5", "SKU-5", "V-5", "10 Aug 2026 09:04", "LZ-5", 350.00, 300.00, 50.00, "สินค้า 5", "delivered", 0, 0, 0, 0},
		{"L-6", "SKU-6", "V-6", "10 Aug 2026 09:05", "LZ-6", 300.00, 300.00, 0.00, "สินค้า 6", "shipped", 0, 0, 0, 0},
		{"L-7", "SKU-7", "V-7", "10 Aug 2026 09:06", "LZ-7", 346.00, 337.00, 15.00, "สินค้า 7", "shipped", 0, 0, -6.00, 0},
	}
	for rowIndex, row := range rows {
		for colIndex, value := range row {
			cell, _ := excelize.CoordinatesToCellName(colIndex+1, rowIndex+2)
			_ = f.SetCellValue(sheet, cell, value)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	orders, _, skipped, err := parseLazadaExcel(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var gross, discount, shipping, paid float64
	for _, order := range orders {
		gross += order.ItemGrossAmount
		discount += order.DiscountAmount
		shipping += order.ShippingAmount
		paid += order.PaidAmount
	}
	if len(orders) != 7 || skipped != 0 || centsFromMoney(gross) != 253700 || centsFromMoney(discount) != 3807 || centsFromMoney(shipping) != 13000 || centsFromMoney(paid) != 262893 {
		t.Fatalf("orders=%d skipped=%d gross=%.2f discount=%.2f shipping=%.2f paid=%.2f", len(orders), skipped, gross, discount, shipping, paid)
	}
}
