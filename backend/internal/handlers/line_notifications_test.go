package handlers

import (
	"strings"
	"testing"

	"nexflow/internal/config"
)

func TestLineNotificationSampleMessageMatchesRichShopeeFallback(t *testing.T) {
	h := &LineNotificationHandler{cfg: &config.Config{PublicBaseURL: "https://animal-galvanize-tameness.ngrok-free.dev"}}

	msg := h.sampleMessage()

	for _, want := range []string{
		"มีออเดอร์ Shopee ใหม่",
		"Henna.milkford",
		"260621NDVGSKMA",
		"ยอดรวม: ฿245.00",
		"Credit Card/Debit Card",
		"ยอดสุทธิตาม Shopee escrow: ฿263.00",
		"ส่วนต่างจากยอดลูกค้าชำระ: -฿18.00",
		"ค่าส่งประมาณการ: ฿35.00",
		"EMS - Thailand Post",
		"OFG235736492235190",
		"21/06/2026 17:21",
		"เปิดใน Nexflow: https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=260621NDVGSKMA",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("sample message missing %q:\n%s", want, msg)
		}
	}
	for _, leak := range []string{"buyer-secret", "secret-name", "0999999999", "full_address", "buyer_username"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("sample message leaked %q:\n%s", leak, msg)
		}
	}
}
