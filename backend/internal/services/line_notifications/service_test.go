package linenotify

import (
	"encoding/json"
	"errors"
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

func TestBuildShopeeNewOrderRichLineFlexShowsPaymentShippingAndOmitsPII(t *testing.T) {
	altText, flex := BuildShopeeNewOrderRichLineFlex(&models.ShopeeOrderSnapshot{
		ShopID:        264993963,
		ShopLabel:     "Henna.milkford",
		OrderSN:       "260621NDVGSKMA",
		BuyerUsername: "buyer-secret",
		TotalAmount:   245,
		PaymentMethod: "Credit Card/Debit Card",
		ItemCount:     1,
		RawDetail: []byte(`{
		  "order_sn":"260621NDVGSKMA",
		  "payment_method":"Credit Card/Debit Card",
		  "cod":false,
		  "total_amount":245,
		  "estimated_shipping_fee":35,
		  "pay_time":1782036000,
		  "buyer_username":"buyer-secret",
		  "recipient_address":{"name":"secret-name","phone":"0999999999","full_address":"secret-address"},
		  "package_list":[{"package_number":"OFG235736492235190","logistics_status":"LOGISTICS_READY","shipping_carrier":"EMS - Thailand Post"}],
		  "item_list":[{"item_name":"ชุดใหญ่ 10 กรัม สีเพ้นคิ้วเฮนน่า","model_name":"B.น้ำตาลเข้ม","model_quantity_purchased":1,"model_original_price":300,"model_discounted_price":245}]
		}`),
	}, "https://animal-galvanize-tameness.ngrok-free.dev")

	if !strings.Contains(altText, "Henna.milkford") || !strings.Contains(altText, "฿245.00") {
		t.Fatalf("alt text not useful: %q", altText)
	}
	buf, err := json.Marshal(flex)
	if err != nil {
		t.Fatalf("marshal flex: %v", err)
	}
	body := string(buf)
	for _, want := range []string{
		"ยอดลูกค้าชำระ",
		"Credit Card/Debit Card",
		"ค่าส่งประมาณการ",
		"EMS - Thailand Post",
		"OFG235736492235190",
		"ชุดใหญ่ 10 กรัม",
		"เปิดใน Nexflow",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rich flex missing %q:\n%s", want, body)
		}
	}
	for _, leak := range []string{"buyer-secret", "secret-name", "0999999999", "secret-address"} {
		if strings.Contains(body, leak) {
			t.Fatalf("rich flex leaked %q:\n%s", leak, body)
		}
	}
}

func TestBuildShopeeNewOrderRichLineFlexWithPaymentShowsEscrowBreakdown(t *testing.T) {
	payment := &models.ShopeeOrderPaymentSnapshot{
		Status:                 "ready",
		BuyerTotalAmount:       245,
		EscrowAmount:           263,
		DeductionAmount:        -18,
		CommissionFee:          42,
		SellerTransactionFee:   10,
		VoucherFromShopee:      60,
		BuyerPaidShippingFee:   15,
		SellerShippingDiscount: 20,
	}
	altText, flex := BuildShopeeNewOrderRichLineFlexWithPayment(&models.ShopeeOrderSnapshot{
		ShopID:        264993963,
		ShopLabel:     "Henna.milkford",
		OrderSN:       "260621NDVGSKMA",
		BuyerUsername: "buyer-secret",
		TotalAmount:   245,
		PaymentMethod: "Credit Card/Debit Card",
		ItemCount:     1,
		RawDetail: []byte(`{
		  "order_sn":"260621NDVGSKMA",
		  "payment_method":"Credit Card/Debit Card",
		  "cod":false,
		  "total_amount":245,
		  "buyer_username":"buyer-secret",
		  "recipient_address":{"name":"secret-name","phone":"0999999999"},
		  "item_list":[{"item_name":"ชุดใหญ่ 10 กรัม","model_name":"B.น้ำตาลเข้ม","model_quantity_purchased":1}]
		}`),
	}, payment, "https://animal-galvanize-tameness.ngrok-free.dev")

	if !strings.Contains(altText, "Henna.milkford") || !strings.Contains(altText, "฿245.00") {
		t.Fatalf("alt text not useful: %q", altText)
	}
	buf, err := json.Marshal(flex)
	if err != nil {
		t.Fatalf("marshal flex: %v", err)
	}
	body := string(buf)
	for _, want := range []string{
		"ข้อมูลการชำระเงิน Shopee",
		"ยอดสุทธิตาม Shopee escrow",
		"ส่วนต่างจากยอดลูกค้าชำระ",
		"-฿18.00",
		"Commission",
		"Transaction fee",
		"Voucher Shopee",
		"ค่าส่งลูกค้าจ่าย",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payment flex missing %q:\n%s", want, body)
		}
	}
	for _, leak := range []string{"buyer-secret", "secret-name", "0999999999"} {
		if strings.Contains(body, leak) {
			t.Fatalf("payment flex leaked %q:\n%s", leak, body)
		}
	}
}

func TestBuildShopeeSettlementLineFlexShowsNetDeductionsAndEscrowFees(t *testing.T) {
	rawEscrow := json.RawMessage(`{
	  "order_sn":"260426RC617UT2",
	  "order_income":{
	    "commission_fee":12,
	    "service_fee":5,
	    "seller_transaction_fee":3,
	    "actual_shipping_fee":35
	  }
	}`)
	run := models.ShopeeSettlementLineRun{
		ID:              "run-1",
		ShopID:          264993963,
		ShopLabel:       "Henna.milkford",
		ReleaseDateFrom: "2026-05-01",
		ReleaseDateTo:   "2026-05-15",
		TotalCount:      1,
		BlockedCount:    1,
		Items: []models.ShopeeSettlementLineItem{
			{
				OrderSN:          "260426RC617UT2",
				BuyerTotalAmount: 284,
				PayoutAmount:     204,
				DeductionAmount:  80,
				Status:           "blocked",
				RawEscrow:        rawEscrow,
			},
		},
	}

	altText, flex := BuildShopeeSettlementLineFlex(run, "https://animal-galvanize-tameness.ngrok-free.dev")
	if !strings.Contains(altText, "Henna.milkford") || !strings.Contains(altText, "฿204.00") {
		t.Fatalf("alt text not useful: %q", altText)
	}
	buf, err := json.Marshal(flex)
	if err != nil {
		t.Fatalf("marshal flex: %v", err)
	}
	body := string(buf)
	for _, want := range []string{
		"ยอดสุทธิร้านได้",
		"ยอดลูกค้าชำระ",
		"ยอดหัก/ส่วนต่าง",
		"฿284.00",
		"฿204.00",
		"฿80.00",
		"Commission",
		"Service fee",
		"Transaction",
		"260426RC617UT2",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settlement flex missing %q:\n%s", want, body)
		}
	}
}

type fakeLinePusher struct {
	flexErr   error
	flexCalls int
	textCalls int
	lastText  string
}

func (f *fakeLinePusher) PushFlex(to, altText string, contents map[string]any) error {
	f.flexCalls++
	return f.flexErr
}

func (f *fakeLinePusher) PushText(to, text string) error {
	f.textCalls++
	f.lastText = text
	return nil
}

func TestServicePushDeliveryUsesPayloadAndFallsBackToText(t *testing.T) {
	job := models.LineNotificationDeliveryJob{
		LineNotificationDelivery: models.LineNotificationDelivery{
			ID:             "delivery-1",
			Source:         "shopee_realtime",
			EntityType:     "shopee_order",
			EntityID:       "264993963:ORDER1",
			MessageText:    "fallback text",
			AltText:        "alt text",
			PayloadVersion: 1,
			FlexPayload:    json.RawMessage(`{"type":"bubble","body":{"type":"box","layout":"vertical","contents":[]}}`),
		},
		DestinationID: "Uxxxx",
	}
	svc := &Service{richFlexEnabled: true}
	pusher := &fakeLinePusher{}
	if err := svc.pushDelivery(t.Context(), pusher, job); err != nil {
		t.Fatalf("pushDelivery rich: %v", err)
	}
	if pusher.flexCalls != 1 || pusher.textCalls != 0 {
		t.Fatalf("rich push calls flex=%d text=%d, want flex only", pusher.flexCalls, pusher.textCalls)
	}

	pusher = &fakeLinePusher{flexErr: errors.New("line flex failed")}
	if err := svc.pushDelivery(t.Context(), pusher, job); err != nil {
		t.Fatalf("pushDelivery fallback: %v", err)
	}
	if pusher.flexCalls != 1 || pusher.textCalls != 1 || pusher.lastText != "fallback text" {
		t.Fatalf("fallback calls flex=%d text=%d text=%q", pusher.flexCalls, pusher.textCalls, pusher.lastText)
	}

	pusher = &fakeLinePusher{}
	svc = &Service{richFlexEnabled: false}
	if err := svc.pushDelivery(t.Context(), pusher, job); err != nil {
		t.Fatalf("pushDelivery flag off: %v", err)
	}
	if pusher.flexCalls != 0 || pusher.textCalls != 1 || pusher.lastText != "fallback text" {
		t.Fatalf("flag-off calls flex=%d text=%d text=%q", pusher.flexCalls, pusher.textCalls, pusher.lastText)
	}
}
