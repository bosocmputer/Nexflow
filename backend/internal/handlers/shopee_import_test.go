package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseShopeeExcelAprilExportWithoutSKU(t *testing.T) {
	path := filepath.Join("..", "..", "..", "Order.all.20260401_20260430.xlsx")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("real Shopee sample file is not present")
		}
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	orders, warnings, skipped, err := parseShopeeExcel(f)
	if err != nil {
		t.Fatalf("parse sample: %v", err)
	}
	if got, want := len(orders), 53; got != want {
		t.Fatalf("orders = %d, want %d; warnings=%v", got, want, warnings)
	}
	itemCount := 0
	noSKUItems := 0
	multiLineOrders := 0
	for _, order := range orders {
		itemCount += len(order.Items)
		if order.HasNoSKU {
			noSKUItems += order.NoSKUItemCount
		}
		if order.MultiLine {
			multiLineOrders++
		}
		for _, item := range order.Items {
			if item.RawName == "" {
				t.Fatalf("order %s has item without raw_name", order.OrderID)
			}
		}
	}
	if got, want := itemCount, 58; got != want {
		t.Fatalf("items = %d, want %d", got, want)
	}
	if got, want := noSKUItems, 58; got != want {
		t.Fatalf("no sku items = %d, want %d", got, want)
	}
	if got, want := multiLineOrders, 5; got != want {
		t.Fatalf("multi-line orders = %d, want %d", got, want)
	}
	if got, want := skipped, 6; got != want {
		t.Fatalf("skipped rows = %d, want %d", got, want)
	}
}

func TestParseShopeeExcelKeepsReadyToShipStatus(t *testing.T) {
	var buf bytes.Buffer
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{
		"หมายเลขคำสั่งซื้อ",
		"สถานะการสั่งซื้อ",
		"วันที่สั่งซื้อ",
		"ชื่อสินค้า",
		"ราคาขาย",
		"จำนวน",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	values := []any{"250001", "ที่ต้องจัดส่ง", "2026-05-12 09:00", "สินค้า A", 120, 2}
	for i, v := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		_ = f.SetCellValue(sheet, cell, v)
	}
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close workbook: %v", err)
	}

	orders, warnings, skipped, err := parseShopeeExcel(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parse workbook: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0; warnings=%v", skipped, warnings)
	}
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1; warnings=%v", len(orders), warnings)
	}
	if orders[0].Status != "ที่ต้องจัดส่ง" {
		t.Fatalf("status = %q, want ที่ต้องจัดส่ง", orders[0].Status)
	}
}
