package models

import "testing"

func TestNormalizeShopeeOrderStatus(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "TO_CONFIRM_RECEIVE", want: "SHIPPED"},
		{in: "to_confirm_receive", want: "SHIPPED"},
		{in: "IN_CANCEL", want: "CANCELLED"},
		{in: "ready_to_ship", want: "READY_TO_SHIP"},
		{in: " completed ", want: "COMPLETED"},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizeShopeeOrderStatus(tt.in); got != tt.want {
			t.Fatalf("NormalizeShopeeOrderStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
