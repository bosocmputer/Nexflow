package linenotify

import (
	"strings"
	"testing"

	"nexflow/internal/models"
)

func TestBuildShopeeNewOrderLineTextOmitsBuyerPII(t *testing.T) {
	msg := BuildShopeeNewOrderLineText(&models.ShopeeOrderSnapshot{
		ShopID:        264993963,
		ShopLabel:     "Henna.milkford",
		OrderSN:       "26060232BJHG4E",
		OrderStatus:   "READY_TO_SHIP",
		ERPStatus:     "pending_erp",
		BuyerUsername: "buyer-secret",
		TotalAmount:   165,
	}, "https://animal-galvanize-tameness.ngrok-free.dev")

	for _, want := range []string{
		"มีออเดอร์ Shopee ใหม่",
		"ร้าน: Henna.milkford",
		"Order SN: 26060232BJHG4E",
		"สถานะ Shopee: READY_TO_SHIP",
		"สถานะ ERP: สร้างเอกสารแล้ว รอส่ง SML",
		"ยอดรวม: ฿165.00",
		"https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=26060232BJHG4E",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "buyer-secret") {
		t.Fatalf("message leaked buyer username:\n%s", msg)
	}
}
