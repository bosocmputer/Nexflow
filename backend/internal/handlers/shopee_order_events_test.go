package handlers

import "testing"

func TestShopeeOrderEventFromSubject(t *testing.T) {
	tests := []struct {
		name      string
		subject   string
		eventType string
		label     string
		orderID   string
		ok        bool
	}{
		{
			name:      "payment confirmed",
			subject:   "ยืนยันการชำระเงินคำสั่งซื้อหมายเลข #260519RU2R6CK4",
			eventType: shopeeEventPaymentConfirmed,
			label:     "ยืนยันการชำระเงินแล้ว",
			orderID:   "260519RU2R6CK4",
			ok:        true,
		},
		{
			name:      "shipped",
			subject:   "คำสั่งซื้อ #260504H4YJMFW1 ถูกจัดส่งแล้ว",
			eventType: shopeeEventShipped,
			label:     "ถูกจัดส่งแล้ว",
			orderID:   "260504H4YJMFW1",
			ok:        true,
		},
		{
			name:    "unsupported delivered prompt",
			subject: "pd.tss0202 ได้รับรายการสินค้า #2604294CNC6CNU แล้วหรือยัง?",
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventType, label, orderID, ok := shopeeOrderEventFromSubject(tt.subject)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if eventType != tt.eventType || label != tt.label || orderID != tt.orderID {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", eventType, label, orderID, tt.eventType, tt.label, tt.orderID)
			}
		})
	}
}

func TestNormalizeShopeeOrderID(t *testing.T) {
	if got := normalizeShopeeOrderID(" #260504H4YJMFW1 "); got != "260504H4YJMFW1" {
		t.Fatalf("normalizeShopeeOrderID() = %q", got)
	}
}
