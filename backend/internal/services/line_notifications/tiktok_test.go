package linenotify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

func TestBuildTikTokShopNewOrderMessagesAreBoundedAndPIIFree(t *testing.T) {
	in := models.TikTokShopNewOrderNotification{
		ShopID: "7494619203789490654", ShopName: "henna_milkford", OrderID: "586030483469993439",
		OrderStatus: "AWAITING_SHIPMENT", Currency: "THB", PaymentTotalAmount: "307.49",
		ProductSubtotalAmount: "300.00", ShippingFeeAmount: "0.00", ItemCount: 2, SKUCount: 1,
		CreatedAt: time.Date(2026, 9, 21, 3, 4, 5, 0, time.UTC),
		Items: []models.TikTokShopNewOrderNotificationItem{{
			ProductName: "สีเพ้นท์คิ้วมิวฟอร์ด\nรุ่นพิเศษ", VariantName: "No.5 สีฟ้า", Quantity: 2,
		}},
	}
	text := BuildTikTokShopNewOrderLineText(in, "https://nexflow-aoy.nextstep-soft.com")
	for _, want := range []string{
		"มีออเดอร์ TikTok Shop ใหม่", "ร้าน: henna_milkford", "Order ID: 586030483469993439",
		"สถานะ: รอจัดส่ง", "ยอดสินค้า: 300.00 THB", "ยอดลูกค้าชำระ: 307.49 THB",
		"สีเพ้นท์คิ้วมิวฟอร์ด รุ่นพิเศษ (No.5 สีฟ้า) x2",
		"https://nexflow-aoy.nextstep-soft.com/tiktok-shop-operations?order_id=586030483469993439",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("TikTok text missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"buyer", "recipient", "phone", "address", "0900000000"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("TikTok text leaked %q:\n%s", forbidden, text)
		}
	}

	alt, flex := BuildTikTokShopNewOrderLineFlex(in, "https://nexflow-aoy.nextstep-soft.com")
	if alt == "" || flex == nil {
		t.Fatal("expected TikTok rich Flex payload")
	}
	raw, err := json.Marshal(flex)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := flex["footer"]; ok || strings.Contains(string(raw), "เปิดใน Nexflow") {
		t.Fatalf("TikTok Flex must follow Shopee and omit the footer button: %s", raw)
	}
	for _, want := range []string{"มีออเดอร์ TikTok Shop ใหม่", "henna_milkford", "586030483469993439", "รายการสินค้า"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("TikTok Flex missing %q: %s", want, raw)
		}
	}
}

func TestEnqueueTikTokShopNewOrderUsesDurableRecipientDedupe(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO line_notification_deliveries").
		WithArgs(
			"tiktok_shop", "info", "มีออเดอร์ TikTok Shop ใหม่", sqlmock.AnyArg(),
			"https://nexflow-aoy.nextstep-soft.com/tiktok-shop-operations?order_id=586030483469993439",
			"tiktok_shop_order", "7494619203789490654:586030483469993439",
			"tiktok_shop:new_order:7494619203789490654:586030483469993439",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), 1,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("delivery-1"))

	svc := &Service{
		repo:            repository.NewLineNotificationRepo(db),
		publicBaseURL:   "https://nexflow-aoy.nextstep-soft.com",
		richFlexEnabled: true,
	}
	inserted, err := svc.EnqueueTikTokShopNewOrder(t.Context(), models.TikTokShopNewOrderNotification{
		ShopID: "7494619203789490654", ShopName: "henna_milkford", OrderID: "586030483469993439",
		OrderStatus: "AWAITING_SHIPMENT", Currency: "THB", PaymentTotalAmount: "300.00",
		ProductSubtotalAmount: "300.00", ShippingFeeAmount: "0.00", ItemCount: 1, SKUCount: 1,
		CreatedAt: time.Date(2026, 9, 21, 3, 4, 5, 0, time.UTC),
		Items:     []models.TikTokShopNewOrderNotificationItem{{ProductName: "สินค้า A", Quantity: 1}},
	}, "")
	if err != nil {
		t.Fatalf("EnqueueTikTokShopNewOrder: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted=%d, want 1", inserted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
