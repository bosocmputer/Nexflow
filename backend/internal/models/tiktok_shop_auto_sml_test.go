package models

import "testing"

func TestTikTokAutoSMLStatusPolicyFailsClosed(t *testing.T) {
	for _, status := range []string{"AWAITING_COLLECTION", "IN_TRANSIT", "DELIVERED", "COMPLETED"} {
		if !TikTokAutoSMLAllowsStatus(status) {
			t.Fatalf("expected %s to remain eligible after trigger", status)
		}
	}
	for _, status := range []string{"UNPAID", "ON_HOLD", "AWAITING_SHIPMENT", "PARTIALLY_SHIPPING", "UNKNOWN"} {
		if TikTokAutoSMLAllowsStatus(status) || TikTokAutoSMLStopStatus(status) {
			t.Fatalf("expected %s to fail closed without being treated as final cancellation", status)
		}
	}
	if !TikTokAutoSMLStopStatus("CANCELLED") {
		t.Fatal("CANCELLED must stop an unstarted Auto SML job")
	}
}
