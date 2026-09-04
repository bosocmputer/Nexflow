package handlers

import "testing"

func TestShopeeStockMutationRawNameMatchesImpactPreviewContract(t *testing.T) {
	if got := shopeeStockMutationRawName("  สินค้าหลัก  ", "  ตัวเลือก  "); got != "ตัวเลือก" {
		t.Fatalf("shopeeStockMutationRawName() = %q, want %q", got, "ตัวเลือก")
	}
	if got := shopeeStockMutationRawName("  สินค้าหลัก  ", "  "); got != "สินค้าหลัก" {
		t.Fatalf("shopeeStockMutationRawName() fallback = %q, want %q", got, "สินค้าหลัก")
	}
}

func TestShopeeStockMutationSourceSKUMatchesImpactPreviewContract(t *testing.T) {
	if got := shopeeStockMutationSourceSKU("  ITEM-SKU  ", "  MODEL-SKU  "); got != "MODEL-SKU" {
		t.Fatalf("shopeeStockMutationSourceSKU() = %q, want %q", got, "MODEL-SKU")
	}
	if got := shopeeStockMutationSourceSKU("  ITEM-SKU  ", "   "); got != "ITEM-SKU" {
		t.Fatalf("shopeeStockMutationSourceSKU() fallback = %q, want %q", got, "ITEM-SKU")
	}
}
