package linemyshop

import "testing"

func TestWebhookEligibility(t *testing.T) {
	tests := []struct {
		name string
		in   WebhookPayload
		want bool
	}{
		{
			name: "paid order eligible",
			in:   WebhookPayload{OrderNumber: "1", OrderStatus: OrderStatusFinalized, PaymentStatus: PaymentStatusPaid},
			want: true,
		},
		{
			name: "ready to ship eligible",
			in:   WebhookPayload{OrderNumber: "1", OrderStatus: OrderStatusFinalized, PaymentStatus: PaymentStatusPending, ShipmentStatus: ShipmentStatusReadyToShip},
			want: true,
		},
		{
			name: "pending payment blocked",
			in:   WebhookPayload{OrderNumber: "1", OrderStatus: OrderStatusFinalized, PaymentStatus: PaymentStatusPending},
			want: false,
		},
		{
			name: "canceled blocked",
			in:   WebhookPayload{OrderNumber: "1", OrderStatus: OrderStatusCanceled, PaymentStatus: PaymentStatusPaid, Event: WebhookEvent{Name: EventCanceled}},
			want: false,
		},
		{
			name: "missing order blocked",
			in:   WebhookPayload{PaymentStatus: PaymentStatusPaid},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.EligibleForBill(); got != tt.want {
				t.Fatalf("EligibleForBill() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeWebhookPayloadNormalizesStatuses(t *testing.T) {
	got, err := DecodeWebhookPayload([]byte(`{
		"orderNumber": " LMS-1 ",
		"event": {"name":"ORDER.READY_TO_SHIP","timestamp":"2026-06-22T10:00:00Z"},
		"orderStatus": "finalized",
		"paymentStatus": "paid",
		"shipmentStatus": "shipment_ready",
		"paymentMethod": "cod"
	}`))
	if err != nil {
		t.Fatalf("DecodeWebhookPayload: %v", err)
	}
	if got.OrderNumber != "LMS-1" || got.OrderStatus != "FINALIZED" || got.PaymentStatus != "PAID" || got.ShipmentStatus != "SHIPMENT_READY" || got.PaymentMethod != "COD" {
		t.Fatalf("payload not normalized: %#v", got)
	}
	if got.EventTime() == nil {
		t.Fatal("event time not parsed")
	}
}

func TestDedupeKeyUsesRequestIDWhenPresent(t *testing.T) {
	payload := WebhookPayload{OrderNumber: "LMS-1", Event: WebhookEvent{Name: EventReadyToShip}}
	got := DedupeKey("conn-1", "req-1", payload, []byte(`{}`))
	want := "line_myshop:webhook:conn-1:req-1"
	if got != want {
		t.Fatalf("DedupeKey() = %q, want %q", got, want)
	}
}
