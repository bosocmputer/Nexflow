package repository

import (
	"testing"

	"nexflow/internal/models"
)

func TestSortBillItemsForDisplayMovesShopeeShippingLast(t *testing.T) {
	items := []models.BillItem{
		{ID: "ship", SourceSKU: models.ShopeeShippingSourceSKU, RawName: "ค่าบริการส่งสินค้า"},
		{ID: "sku-1", SourceSKU: "SKU-1", RawName: "สินค้า 1"},
		{ID: "sku-2", SourceSKU: "SKU-2", RawName: "สินค้า 2"},
	}

	sortBillItemsForDisplay(items)

	got := []string{items[0].ID, items[1].ID, items[2].ID}
	want := []string{"sku-1", "sku-2", "ship"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSortBillItemsForDisplayKeepsRegularItemOrder(t *testing.T) {
	items := []models.BillItem{
		{ID: "sku-2", SourceSKU: "SKU-2"},
		{ID: "sku-1", SourceSKU: "SKU-1"},
		{ID: "manual", SourceSKU: ""},
	}

	sortBillItemsForDisplay(items)

	got := []string{items[0].ID, items[1].ID, items[2].ID}
	want := []string{"sku-2", "sku-1", "manual"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
