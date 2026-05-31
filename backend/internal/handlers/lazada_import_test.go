package handlers

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseLazadaExcelGroupsOrdersAndSkipsReturnStatuses(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{
		"orderItemId", "sellerSku", "lazadaSku", "createTime", "orderNumber",
		"customerName", "payMethod", "paidPrice", "unitPrice", "shippingFee",
		"itemName", "variation", "trackingCode", "status",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			t.Fatal(err)
		}
	}
	rows := [][]interface{}{
		{"LI-1", "SKU-A", "LZ-A", "10 May 2026 06:23", "LZ-100", "Buyer A", "COD", "120.00", "150.00", "10.00", "Serum", "30ml", "TH123", "shipped"},
		{"LI-2", "SKU-A", "LZ-A", "10 May 2026 06:23", "LZ-100", "Buyer A", "COD", "120.00", "150.00", "10.00", "Serum", "30ml", "TH123", "shipped"},
		{"LI-3", "SKU-B", "LZ-B", "09 May 2026 12:00", "LZ-101", "Buyer B", "Card", "88.50", "99.00", "0.00", "Mask", "", "TH456", "delivered"},
		{"LI-4", "SKU-C", "LZ-C", "08 May 2026 12:00", "LZ-102", "Buyer C", "Card", "77.00", "77.00", "0.00", "Return Item", "", "TH789", "In Transit: Returning to seller"},
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
	if orders[0].PaidAmount != 240 {
		t.Fatalf("paid amount = %.2f, want 240.00", orders[0].PaidAmount)
	}
}
