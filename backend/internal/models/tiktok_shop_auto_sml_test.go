package models

import "testing"

func TestTikTokAutoSMLStatusPolicyFailsClosed(t *testing.T) {
	tests := []struct {
		trigger string
		status  string
		want    bool
	}{
		{TikTokAutoSMLTriggerAwaitingShipment, "AWAITING_SHIPMENT", true},
		{TikTokAutoSMLTriggerAwaitingShipment, "PARTIALLY_SHIPPING", true},
		{TikTokAutoSMLTriggerAwaitingShipment, "COMPLETED", true},
		{TikTokAutoSMLTriggerAwaitingCollection, "AWAITING_SHIPMENT", false},
		{TikTokAutoSMLTriggerAwaitingCollection, "AWAITING_COLLECTION", true},
		{TikTokAutoSMLTriggerAwaitingCollection, "IN_TRANSIT", true},
		{TikTokAutoSMLTriggerInTransit, "AWAITING_COLLECTION", false},
		{TikTokAutoSMLTriggerInTransit, "IN_TRANSIT", true},
		{TikTokAutoSMLTriggerInTransit, "DELIVERED", true},
		{TikTokAutoSMLTriggerInTransit, "COMPLETED", true},
		{TikTokAutoSMLTriggerCompleted, "DELIVERED", false},
		{TikTokAutoSMLTriggerCompleted, "COMPLETED", true},
		{TikTokAutoSMLTriggerCompleted, "CANCELLED", false},
	}
	for _, test := range tests {
		if got := TikTokAutoSMLAllowsStatus(test.trigger, test.status); got != test.want {
			t.Fatalf("trigger=%s status=%s got=%v want=%v", test.trigger, test.status, got, test.want)
		}
	}
	for _, status := range []string{"UNPAID", "ON_HOLD", "UNKNOWN"} {
		if TikTokAutoSMLAllowsStatus(TikTokAutoSMLTriggerAwaitingShipment, status) || TikTokAutoSMLStopStatus(status) {
			t.Fatalf("expected %s to fail closed without being treated as final cancellation", status)
		}
	}
	if !TikTokAutoSMLStopStatus("CANCELLED") {
		t.Fatal("CANCELLED must stop an unstarted Auto SML job")
	}
}

func TestNormalizeTikTokAutoSMLTriggerStatus(t *testing.T) {
	for input, want := range map[string]string{
		" awaiting_shipment ": TikTokAutoSMLTriggerAwaitingShipment,
		"AWAITING_COLLECTION": TikTokAutoSMLTriggerAwaitingCollection,
		"in_transit":          TikTokAutoSMLTriggerInTransit,
		"completed":           TikTokAutoSMLTriggerCompleted,
		"UNPAID":              "",
	} {
		if got := NormalizeTikTokAutoSMLTriggerStatus(input); got != want {
			t.Fatalf("NormalizeTikTokAutoSMLTriggerStatus(%q)=%q want=%q", input, got, want)
		}
	}
}
