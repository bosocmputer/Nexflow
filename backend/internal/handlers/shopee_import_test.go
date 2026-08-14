package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestConsumeShopeePreviewIsUserScopedExpiringAndOneTime(t *testing.T) {
	h := &ShopeeImportHandler{}
	now := time.Now()
	h.pendingUploads.Store("valid", &pendingUpload{importRunID: "run-1", userID: "user-1", uploadedAt: now})

	if _, ok := h.consumeShopeePreview("valid", "run-1", "user-2", now); ok {
		t.Fatal("preview must not be consumable by another user")
	}
	if _, ok := h.consumeShopeePreview("valid", "run-1", "user-1", now); ok {
		t.Fatal("failed ownership check must still consume the one-time token")
	}

	h.pendingUploads.Store("expired", &pendingUpload{importRunID: "run-1", userID: "user-1", uploadedAt: now.Add(-pendingUploadTTL - time.Second)})
	if _, ok := h.consumeShopeePreview("expired", "run-1", "user-1", now); ok {
		t.Fatal("expired preview must be rejected")
	}

	h.pendingUploads.Store("once", &pendingUpload{importRunID: "run-1", userID: "user-1", uploadedAt: now})
	if _, ok := h.consumeShopeePreview("once", "run-1", "user-1", now); !ok {
		t.Fatal("valid preview was rejected")
	}
	if _, ok := h.consumeShopeePreview("once", "run-1", "user-1", now); ok {
		t.Fatal("preview token must be one-time use")
	}
}

func TestParseShopeeExcelRequiresBuyerPaidShippingColumn(t *testing.T) {
	var buf bytes.Buffer
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{
		"หมายเลขคำสั่งซื้อ", "สถานะการสั่งซื้อ", "วันที่สั่งซื้อ", "ชื่อสินค้า",
		"ราคาตั้งต้น", "จำนวน", "ราคาสินค้าที่ชำระโดยผู้ซื้อ", "จำนวนเงินทั้งหมด",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, header)
	}
	values := []any{"S-1", "ที่ต้องจัดส่ง", "2026-08-14 09:00", "สินค้า", 100, 1, 100, 100}
	for i, value := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		_ = f.SetCellValue(sheet, cell, value)
	}
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	if _, _, _, err := parseShopeeExcel(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("missing buyer-paid shipping column must be rejected")
	}
}

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
		"ราคาตั้งต้น",
		"จำนวน",
		"ราคาสินค้าที่ชำระโดยผู้ซื้อ",
		"ค่าจัดส่งที่ชำระโดยผู้ซื้อ",
		"จำนวนเงินทั้งหมด",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	values := []any{"250001", "ที่ต้องจัดส่ง", "2026-05-12 09:00", "สินค้า A", 120, 2, 220, 10, 230}
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

func TestParseShopeeExcelGoldenAmounts(t *testing.T) {
	var buf bytes.Buffer
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{
		"หมายเลขคำสั่งซื้อ", "สถานะการสั่งซื้อ", "วันที่สั่งซื้อ", "ชื่อสินค้า", "ราคาตั้งต้น", "จำนวน",
		"ราคาสินค้าที่ชำระโดยผู้ซื้อ", "ค่าจัดส่งที่ชำระโดยผู้ซื้อ", "จำนวนเงินทั้งหมด",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, header)
	}
	rows := [][]any{
		{"S-1", "ที่ต้องจัดส่ง", "2026-08-11 09:00", "สินค้า A", 239, 1, 239, 9, 248},
		{"S-2", "ที่ต้องจัดส่ง", "2026-08-12 09:00", "สินค้า B", 239, 1, 239, 18, 257},
		{"S-3", "ที่ต้องจัดส่ง", "2026-08-14 09:00", "สินค้า C", 300, 1, 212, 18, 230},
	}
	for rowIndex, row := range rows {
		for colIndex, value := range row {
			cell, _ := excelize.CoordinatesToCellName(colIndex+1, rowIndex+2)
			_ = f.SetCellValue(sheet, cell, value)
		}
	}
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	orders, _, skipped, err := parseShopeeExcel(bytes.NewReader(buf.Bytes()))
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
	if len(orders) != 3 || skipped != 0 || centsFromMoney(gross) != 77800 || centsFromMoney(discount) != 8800 || centsFromMoney(shipping) != 4500 || centsFromMoney(paid) != 73500 {
		t.Fatalf("orders=%d skipped=%d gross=%.2f discount=%.2f shipping=%.2f paid=%.2f", len(orders), skipped, gross, discount, shipping, paid)
	}
}
