package linenotify

import (
	"encoding/json"
	"strings"
	"testing"

	"nexflow/internal/models"
)

func TestBuildShopeeNewOrderLineTextShowsProductAndOmitsStatusNoise(t *testing.T) {
	msg := BuildShopeeNewOrderLineText(&models.ShopeeOrderSnapshot{
		ShopID:        264993963,
		ShopLabel:     "Henna.milkford",
		OrderSN:       "26060232BJHG4E",
		OrderStatus:   "READY_TO_SHIP",
		ERPStatus:     "pending_erp",
		BuyerUsername: "buyer-secret",
		TotalAmount:   165,
		PaymentMethod: "COD",
		ItemCount:     1,
		RawDetail: []byte(`{
		  "payment_method":"COD",
		  "cod":true,
		  "buyer_username":"buyer-secret",
		  "recipient_address":{"name":"secret-name","phone":"0999999999"},
		  "item_list":[
		    {
		      "item_name":"สีเฟ้นคิ้วเฮนน่า สีเฟ้นท์คิ้วมิวฟอร์ด ทั้งชุดพร้อมแปรงและบล็อคคิ้ว 15 คู่",
		      "model_name":"2.น้ำตาลเข้ม",
		      "model_quantity_purchased":1
		    }
		  ]
		}`),
	}, "https://animal-galvanize-tameness.ngrok-free.dev")

	for _, want := range []string{
		"มีออเดอร์ Shopee ใหม่",
		"ร้าน: Henna.milkford",
		"Order SN: 26060232BJHG4E",
		"ยอดรวม: ฿165.00",
		"ชำระเงิน: เก็บเงินปลายทาง",
		"สินค้า:",
		"สีเฟ้นคิ้วเฮนน่า",
		"(2.น้ำตาลเข้ม) x1",
		"https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=26060232BJHG4E",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "buyer-secret") {
		t.Fatalf("message leaked buyer username:\n%s", msg)
	}
	if strings.Contains(msg, "secret-name") || strings.Contains(msg, "0999999999") {
		t.Fatalf("message leaked recipient PII:\n%s", msg)
	}
	if strings.Contains(msg, "สถานะ Shopee") || strings.Contains(msg, "สถานะ ERP") {
		t.Fatalf("message still includes noisy statuses:\n%s", msg)
	}
}

func TestBuildShopeeNewOrderLineTextFallsBackToItemCount(t *testing.T) {
	msg := BuildShopeeNewOrderLineText(&models.ShopeeOrderSnapshot{
		ShopID:      264993963,
		ShopLabel:   "Henna.milkford",
		OrderSN:     "2606156E5D03GX",
		TotalAmount: 257,
		ItemCount:   2,
	}, "")

	for _, want := range []string{
		"มีออเดอร์ Shopee ใหม่",
		"สินค้า: 2 รายการ",
		"เปิดใน Nexflow: /shopee-operations?order=2606156E5D03GX",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildShopeeCancelledAfterSMLLineTextIsFocusedAndTextOnly(t *testing.T) {
	msg := BuildShopeeCancelledAfterSMLLineText(&models.ShopeeOrderSnapshot{
		ShopID:        264993963,
		ShopLabel:     "Henna.milkford",
		OrderSN:       "2606156E5D03GX",
		SMLDocNo:      "BF-SO260600001",
		BuyerUsername: "buyer-secret",
		TotalAmount:   257,
		RawDetail: []byte(`{
		  "buyer_username":"buyer-secret",
		  "recipient_address":{"name":"secret-name","phone":"0999999999"}
		}`),
	}, "https://animal-galvanize-tameness.ngrok-free.dev")

	for _, want := range []string{
		"Shopee ยกเลิกหลังส่ง SML",
		"Order SN: 2606156E5D03GX",
		"ใบขาย SML: BF-SO260600001",
		"ยอดรวม: ฿257.00",
		"ต้องสร้างเอกสารยกเลิก SML ใน Nexflow",
		"https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=2606156E5D03GX",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	for _, leak := range []string{"buyer-secret", "secret-name", "0999999999"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("message leaked %q:\n%s", leak, msg)
		}
	}
	if shouldSendShopeeOrderFlex(models.LineNotificationDeliveryJob{
		LineNotificationDelivery: models.LineNotificationDelivery{
			Source:     "shopee_realtime",
			EntityType: "shopee_order",
			Title:      "ออเดอร์ Shopee ถูกยกเลิกหลังส่ง SML",
		},
	}) {
		t.Fatalf("cancelled-after-SML alert should be sent as text, not new-order flex")
	}
}

func TestBuildShopeeNewOrderLineFlexContainsReadableSalesDetails(t *testing.T) {
	msg := BuildShopeeNewOrderLineText(&models.ShopeeOrderSnapshot{
		ShopLabel:     "Henna.milkford",
		OrderSN:       "26060232BJHG4E",
		BuyerUsername: "buyer-secret",
		TotalAmount:   165,
		PaymentMethod: "COD",
		ItemCount:     1,
		RawDetail: []byte(`{
		  "payment_method":"COD",
		  "cod":true,
		  "item_list":[
		    {
		      "item_name":"สีเฟ้นคิ้วเฮนน่า",
		      "model_name":"2.น้ำตาลเข้ม",
		      "model_quantity_purchased":1
		    }
		  ]
		}`),
	}, "https://animal-galvanize-tameness.ngrok-free.dev")

	altText, flex := BuildShopeeNewOrderLineFlex(models.LineNotificationDeliveryJob{
		LineNotificationDelivery: models.LineNotificationDelivery{
			Source:      "shopee_realtime",
			EntityType:  "shopee_order",
			EntityID:    "264993963:26060232BJHG4E",
			ActionURL:   "https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=26060232BJHG4E",
			MessageText: msg,
		},
	})
	if !strings.Contains(altText, "Henna.milkford") || !strings.Contains(altText, "฿165.00") {
		t.Fatalf("alt text not useful: %q", altText)
	}
	buf, err := json.Marshal(flex)
	if err != nil {
		t.Fatalf("marshal flex: %v", err)
	}
	body := string(buf)
	for _, want := range []string{
		"ออเดอร์ Shopee ใหม่",
		"Henna.milkford",
		"฿165.00",
		"สีเฟ้นคิ้วเฮนน่า",
		"เก็บเงินปลายทาง",
		"เปิดใน Nexflow",
		"https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=26060232BJHG4E",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("flex missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "buyer-secret") || strings.Contains(body, "สถานะ Shopee") || strings.Contains(body, "สถานะ ERP") {
		t.Fatalf("flex leaked PII or noisy status:\n%s", body)
	}
}
