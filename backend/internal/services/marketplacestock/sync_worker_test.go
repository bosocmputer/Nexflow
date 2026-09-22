package marketplacestock

import (
	"testing"

	"nexflow/internal/services/tiktokshop"
)

func TestExactTikTokInventoryRequiresSingleExactWarehouse(t *testing.T) {
	response := &tiktokshop.GatewayInventorySearchResponse{Inventory: []tiktokshop.ProductInventoryRecord{{
		ProductID: "product-1", SKUs: []tiktokshop.InventorySearchSKU{{ID: "sku-1", WarehouseInventory: []tiktokshop.WarehouseInventory{{WarehouseID: "warehouse-a", AvailableQuantity: 7}, {WarehouseID: "warehouse-b", AvailableQuantity: 4}}}},
	}}}
	if _, _, ok := exactTikTokInventory(response, Member{ExternalProductID: "product-1", ExternalSKUID: "sku-1"}); ok {
		t.Fatal("inventory without selected warehouse must fail closed when TikTok returns multiple warehouses")
	}
	warehouse, qty, ok := exactTikTokInventory(response, Member{ExternalProductID: "product-1", ExternalSKUID: "sku-1", ExternalWarehouseID: "warehouse-b"})
	if !ok || warehouse != "warehouse-b" || qty != 4 {
		t.Fatalf("exactTikTokInventory() = %q,%d,%t", warehouse, qty, ok)
	}
}
