package handlers

import "testing"

func TestShopeeImportRoute(t *testing.T) {
	tests := []struct {
		name string
		cfg  ShopeeConfigRequest
		want string
	}{
		{
			name: "sale invoice endpoint with hyphen",
			cfg:  ShopeeConfigRequest{Endpoint: "/api/v1/ic/sale-invoices", DocFormat: "BF-INV"},
			want: "saleinvoice",
		},
		{
			name: "sale invoice compact endpoint",
			cfg:  ShopeeConfigRequest{Endpoint: "/api/v1/ic/saleinvoice", DocFormat: "SI"},
			want: "saleinvoice",
		},
		{
			name: "sale order endpoint",
			cfg:  ShopeeConfigRequest{Endpoint: "/api/v1/ic/sale-orders", DocFormat: "BS"},
			want: "saleorder",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeImportRoute(tt.cfg); got != tt.want {
				t.Fatalf("shopeeImportRoute() = %q, want %q", got, tt.want)
			}
		})
	}
}
