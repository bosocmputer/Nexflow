package linenotify

import (
	"encoding/json"
	"time"

	"nexflow/internal/models"
	"nexflow/internal/services/sml"
)

// BuildLineNotificationSample returns the same Flex builders used by the
// delivery path with synthetic, PII-free data. Rendering a preview never
// enqueues or sends a LINE message.
func BuildLineNotificationSample(eventKey, publicBaseURL string) (models.LineNotificationSample, bool) {
	shopeeOrder := previewShopeeOrder()
	shopeePayment := previewShopeePayment()
	tikTokOrder := previewTikTokOrder()
	var alt, text string
	var contents map[string]any

	switch eventKey {
	case models.LineNotificationEventShopeeOrderNew:
		text = BuildShopeeNewOrderLineTextWithPayment(shopeeOrder, shopeePayment, publicBaseURL)
		alt, contents = BuildShopeeNewOrderRichLineFlexWithPayment(shopeeOrder, shopeePayment, publicBaseURL)
	case models.LineNotificationEventTikTokOrderNew:
		text = BuildTikTokShopNewOrderLineText(tikTokOrder, publicBaseURL)
		alt, contents = BuildTikTokShopNewOrderLineFlex(tikTokOrder, publicBaseURL)
	case models.LineNotificationEventNextStepOrderNew:
		order := sml.NextStepMarketplaceOrder{DocNo: "MQT20261004-SAMPLE", DocDate: "2026-10-04", DocTime: "09:30", Status: "pending", TotalAmount: 1280}
		text = BuildNextStepMarketplaceNewOrderLineText(order, publicBaseURL)
		alt, contents = BuildNextStepMarketplaceNewOrderLineFlex(order, publicBaseURL)
	case models.LineNotificationEventShopeeOrderCancelled:
		shopeeOrder.SMLDocNo = "BF-INV26100001"
		text = BuildShopeeOrderCancelledLineText(shopeeOrder, publicBaseURL)
		alt, contents = BuildShopeeOrderCancelledLineFlex(shopeeOrder)
	case models.LineNotificationEventTikTokOrderCancelled:
		cancel := models.TikTokShopOrderCancellationNotification{ShopID: tikTokOrder.ShopID, ShopName: tikTokOrder.ShopName, OrderID: tikTokOrder.OrderID, Currency: "THB", PaymentTotalAmount: "307.49", ItemCount: 1, SKUCount: 1, SMLDocNo: "BF-INV26100002", OrderUpdatedAt: time.Now()}
		text = BuildTikTokShopOrderCancelledLineText(cancel, publicBaseURL)
		alt, contents = BuildTikTokShopOrderCancelledLineFlex(cancel)
	case models.LineNotificationEventShopeeSMLSuccess, models.LineNotificationEventShopeeSMLReview, models.LineNotificationEventShopeeSMLFailed:
		kind, title := previewSMLKindAndTitle(eventKey, "Shopee")
		in := models.ShopeeAutoSMLNotification{ShopID: shopeeOrder.ShopID, ShopLabel: shopeeOrder.ShopLabel, OrderSN: shopeeOrder.OrderSN, BillID: "sample-bill", SMLDocNo: "BF-INV26100001", TotalAmount: 245, ItemCount: 1, Items: []models.ShopeeAutoSMLNotificationItem{{Name: "ชุดสีเพ้นท์คิ้วเฮนน่า", Variant: "B.น้ำตาลเข้ม", Qty: 1}}}
		if kind != "success" {
			in.SMLDocNo = ""
			in.ErrorMessage = previewSMLError(kind)
		}
		text = buildShopeeAutoSMLText(title, in, shopeeAutoSMLActionURL(publicBaseURL, in))
		alt, contents = buildShopeeAutoSMLFlex(title, kind, in, "")
	case models.LineNotificationEventTikTokSMLSuccess, models.LineNotificationEventTikTokSMLReview, models.LineNotificationEventTikTokSMLFailed:
		kind, title := previewSMLKindAndTitle(eventKey, "TikTok Shop")
		in := models.TikTokAutoSMLNotification{ShopID: tikTokOrder.ShopID, ShopName: tikTokOrder.ShopName, OrderID: tikTokOrder.OrderID, BillID: "sample-bill", SMLDocNo: "BF-INV26100002", Currency: "THB", TotalAmount: 307.49, ItemCount: 1, Items: tikTokOrder.Items}
		if kind != "success" {
			in.SMLDocNo = ""
			in.ErrorMessage = previewSMLError(kind)
		}
		text = buildTikTokShopAutoSMLText(title, in, tikTokShopAutoSMLActionURL(publicBaseURL, in))
		alt, contents = buildTikTokShopAutoSMLFlex(title, kind, in)
	case models.LineNotificationEventShopeeSMLCancellationCreated:
		shopeeOrder.SMLDocNo = "BF-INV26100001"
		text = BuildShopeeSMLCancellationCreatedLineText(shopeeOrder, "CN26100001", "เอกสารรับคืนสินค้า/ลดหนี้", publicBaseURL)
		alt, contents = BuildShopeeSMLCancellationCreatedLineFlex(shopeeOrder, "CN26100001", "เอกสารรับคืนสินค้า/ลดหนี้")
	case models.LineNotificationEventShopeeSettlementReady:
		run := previewShopeeSettlement()
		text = BuildShopeeSettlementLineText(run, publicBaseURL)
		alt, contents = BuildShopeeSettlementLineFlex(run, publicBaseURL)
	default:
		return models.LineNotificationSample{}, false
	}

	raw, err := json.Marshal(contents)
	if err != nil || contents == nil {
		return models.LineNotificationSample{}, false
	}
	return models.LineNotificationSample{EventKey: eventKey, AltText: alt, MessageText: text, FlexPayload: raw}, true
}

func previewShopeeOrder() *models.ShopeeOrderSnapshot {
	return &models.ShopeeOrderSnapshot{ShopID: 264993963, ShopLabel: "Henna.milkford", OrderSN: "261004SAMPLE", PaymentMethod: "Credit Card/Debit Card", TotalAmount: 245, ItemCount: 1, PackageNumber: "OFG-SAMPLE", LogisticsStatus: "LOGISTICS_READY", ShippingCarrier: "EMS - Thailand Post", RawDetail: []byte(`{"order_sn":"261004SAMPLE","payment_method":"Credit Card/Debit Card","cod":false,"total_amount":245,"create_time":1791081000,"pay_time":1791081060,"item_list":[{"item_name":"ชุดสีเพ้นท์คิ้วเฮนน่า","model_name":"B.น้ำตาลเข้ม","model_quantity_purchased":1,"model_original_price":300,"model_discounted_price":245}]}`)}
}

func previewShopeePayment() *models.ShopeeOrderPaymentSnapshot {
	return &models.ShopeeOrderPaymentSnapshot{ShopID: 264993963, OrderSN: "261004SAMPLE", Status: "ready", BuyerTotalAmount: 245, EscrowAmount: 228, DeductionAmount: -17, CommissionFee: 12, SellerTransactionFee: 5}
}

func previewTikTokOrder() models.TikTokShopNewOrderNotification {
	return models.TikTokShopNewOrderNotification{ShopID: "7494619203789490654", ShopName: "henna_milkford", OrderID: "586000000000000001", OrderStatus: "AWAITING_SHIPMENT", Currency: "THB", PaymentTotalAmount: "307.49", ProductSubtotalAmount: "300.00", ShippingFeeAmount: "7.49", ItemCount: 1, SKUCount: 1, CreatedAt: time.Now(), Items: []models.TikTokShopNewOrderNotificationItem{{ProductName: "สีเพ้นท์คิ้วมิวฟอร์ด", VariantName: "No.5 สีฟ้า", Quantity: 1}}}
}

func previewShopeeSettlement() models.ShopeeSettlementLineRun {
	return models.ShopeeSettlementLineRun{ID: "sample-settlement", ShopID: 264993963, ShopLabel: "Henna.milkford", Status: "ready", ReleaseDateFrom: "2026-10-01", ReleaseDateTo: "2026-10-04", TotalCount: 2, ReadyCount: 2, BuyerTotalAmountTotal: 500, PayoutAmountTotal: 462, DeductionAmountTotal: -38, Items: []models.ShopeeSettlementLineItem{{OrderSN: "261004SAMPLE", PayoutAmount: 228, BuyerTotalAmount: 245, DeductionAmount: -17, Status: "ready"}}}
}

func previewSMLKindAndTitle(eventKey, channel string) (string, string) {
	if eventKey == models.LineNotificationEventShopeeSMLSuccess || eventKey == models.LineNotificationEventTikTokSMLSuccess {
		return "success", "สร้างบิล SML จาก " + channel + " สำเร็จ"
	}
	if eventKey == models.LineNotificationEventShopeeSMLReview || eventKey == models.LineNotificationEventTikTokSMLReview {
		return "review", "ออเดอร์ " + channel + " ต้องตรวจสอบก่อนส่ง SML"
	}
	return "failure", "ส่งออเดอร์ " + channel + " เข้า SML ไม่สำเร็จ"
}

func previewSMLError(kind string) string {
	if kind == "review" {
		return "ยังไม่ได้จับคู่สินค้า SML กรุณาตรวจข้อมูลก่อนส่ง"
	}
	return "SML ไม่ตอบกลับ ระบบหยุด retry และรอให้ผู้ใช้ตรวจ"
}
