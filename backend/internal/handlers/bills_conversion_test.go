package handlers

import (
	"testing"

	"nexflow/internal/models"
)

func TestEffectiveSMLQuantityUsesSnapshotOnlyInActiveMode(t *testing.T) {
	smlQty := 4.0
	item := models.BillItem{Qty: 2, SMLQty: &smlQty}

	for _, mode := range []string{"off", "shadow", ""} {
		got, err := effectiveSMLQuantity(item, mode)
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		if got != 2 {
			t.Fatalf("mode %q qty = %v, want marketplace qty 2", mode, got)
		}
	}

	got, err := effectiveSMLQuantity(item, "active")
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("active qty = %v, want snapshotted SML qty 4", got)
	}
}

func TestEffectiveSMLQuantityFailsClosedForUnsafeActiveConversion(t *testing.T) {
	for name, item := range map[string]models.BillItem{
		"missing snapshot": {Qty: 2, SourceSKU: "MP-SKU"},
		"conversion issue": {Qty: 2, SourceSKU: "MP-SKU", ConversionIssueCode: "conversion_unit_stale"},
		"non positive": func() models.BillItem {
			qty := 0.0
			return models.BillItem{Qty: 2, SourceSKU: "MP-SKU", SMLQty: &qty}
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := effectiveSMLQuantity(item, "active"); err == nil {
				t.Fatal("expected unsafe conversion to be rejected")
			}
		})
	}
}

func TestEffectiveSMLQuantityKeepsMarketplaceShippingLineUnconverted(t *testing.T) {
	item := models.BillItem{Qty: 1, SourceSKU: models.ShopeeShippingSourceSKU}
	got, err := effectiveSMLQuantity(item, "active")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("shipping qty = %v, want 1", got)
	}
}
