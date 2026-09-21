package tiktokshop

import "testing"

func TestTikTokProductSKUVariantNameUsesReadableSalesAttributes(t *testing.T) {
	sku := ProductSKU{SalesAttributes: []ProductSalesAttribute{
		{Name: "สี", ValueName: "น้ำตาลเข้ม"},
		{Name: "ขนาด", ValueName: "M"},
	}}
	if got, want := TikTokProductSKUVariantName(sku), "สี: น้ำตาลเข้ม · ขนาด: M"; got != want {
		t.Fatalf("TikTokProductSKUVariantName() = %q, want %q", got, want)
	}
}

func TestTikTokProductSKUVariantNameOmitsEmptyAttributes(t *testing.T) {
	sku := ProductSKU{SalesAttributes: []ProductSalesAttribute{{Name: "สี"}, {ValueName: "แดง"}, {Name: "สี", ValueName: "แดง"}}}
	if got, want := TikTokProductSKUVariantName(sku), "แดง · สี: แดง"; got != want {
		t.Fatalf("TikTokProductSKUVariantName() = %q, want %q", got, want)
	}
}
