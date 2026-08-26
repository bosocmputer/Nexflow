package models

import "testing"

func TestNormalizeShopeeAutoSMLTriggerStatus(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: " ready_to_ship ", want: ShopeeAutoSMLTriggerReadyToShip},
		{in: "processed", want: ShopeeAutoSMLTriggerProcessed},
		{in: "SHIPPED", want: ""},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizeShopeeAutoSMLTriggerStatus(tt.in); got != tt.want {
			t.Fatalf("NormalizeShopeeAutoSMLTriggerStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShopeeAutoSMLTriggerAllowsForwardLifecycle(t *testing.T) {
	tests := []struct {
		trigger string
		status  string
		want    bool
	}{
		{ShopeeAutoSMLTriggerReadyToShip, "READY_TO_SHIP", true},
		{ShopeeAutoSMLTriggerReadyToShip, "PROCESSED", true},
		{ShopeeAutoSMLTriggerReadyToShip, "SHIPPED", true},
		{ShopeeAutoSMLTriggerReadyToShip, "TO_CONFIRM_RECEIVE", true},
		{ShopeeAutoSMLTriggerReadyToShip, "COMPLETED", true},
		{ShopeeAutoSMLTriggerProcessed, "READY_TO_SHIP", false},
		{ShopeeAutoSMLTriggerProcessed, "PROCESSED", true},
		{ShopeeAutoSMLTriggerProcessed, "SHIPPED", true},
		{ShopeeAutoSMLTriggerProcessed, "TO_CONFIRM_RECEIVE", true},
		{ShopeeAutoSMLTriggerProcessed, "COMPLETED", true},
		{ShopeeAutoSMLTriggerProcessed, "UNPAID", false},
		{ShopeeAutoSMLTriggerProcessed, "IN_CANCEL", false},
		{ShopeeAutoSMLTriggerProcessed, "CANCELLED", false},
		{ShopeeAutoSMLTriggerProcessed, "UNKNOWN_NEW_STATUS", false},
	}
	for _, tt := range tests {
		if got := ShopeeAutoSMLTriggerAllowsStatus(tt.trigger, tt.status); got != tt.want {
			t.Errorf("ShopeeAutoSMLTriggerAllowsStatus(%q, %q) = %v, want %v", tt.trigger, tt.status, got, tt.want)
		}
	}
}

func TestShopeeAutoSMLStopStatus(t *testing.T) {
	for _, status := range []string{"UNPAID", "IN_CANCEL", "CANCELLED"} {
		if !ShopeeAutoSMLStopStatus(status) {
			t.Errorf("%s must stop Auto SML", status)
		}
	}
	for _, status := range []string{"READY_TO_SHIP", "PROCESSED", "SHIPPED", "COMPLETED", "unknown"} {
		if ShopeeAutoSMLStopStatus(status) {
			t.Errorf("%s must not be classified as a terminal stop", status)
		}
	}
}
