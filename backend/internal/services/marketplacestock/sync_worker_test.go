package marketplacestock

import (
	"context"
	"testing"

	"nexflow/internal/services/tiktokshop"
)

type syncWorkerGatewayFake struct {
	searches []tiktokshop.GatewayInventorySearchRequest
	quantity int64
}

func (f *syncWorkerGatewayFake) SearchInventory(_ context.Context, input tiktokshop.GatewayInventorySearchRequest) (*tiktokshop.GatewayInventorySearchResponse, error) {
	f.searches = append(f.searches, input)
	return &tiktokshop.GatewayInventorySearchResponse{Inventory: []tiktokshop.ProductInventoryRecord{{
		ProductID: "product-1", SKUs: []tiktokshop.InventorySearchSKU{{
			ID: "sku-1", WarehouseInventory: []tiktokshop.WarehouseInventory{{WarehouseID: "warehouse-1", AvailableQuantity: f.quantity}},
		}},
	}}}, nil
}

func (f *syncWorkerGatewayFake) UpdateInventory(_ context.Context, input tiktokshop.GatewayInventoryUpdateRequest) (*tiktokshop.GatewayInventoryUpdateResponse, error) {
	f.quantity = input.Update.SKUs[0].Inventory[0].Quantity
	return &tiktokshop.GatewayInventoryUpdateResponse{}, nil
}

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

func TestSyncTikTokReadsBySKUOnlyBeforeWriteAndReadBack(t *testing.T) {
	gateway := &syncWorkerGatewayFake{quantity: 4}
	worker := &SyncWorker{tiktok: gateway}
	status, previous, actual, code, message := worker.syncTikTok(context.Background(), Member{
		Source: "tiktok", AccountKey: "shop:shop-1", ExternalProductID: "product-1", ExternalSKUID: "sku-1", ExternalWarehouseID: "warehouse-1",
	}, 7)
	if status != "changed" || previous != 4 || actual != 7 || code != "" || message != "" {
		t.Fatalf("syncTikTok() = %q,%d,%d,%q,%q", status, previous, actual, code, message)
	}
	if len(gateway.searches) != 2 {
		t.Fatalf("inventory search calls = %d, want 2", len(gateway.searches))
	}
	for _, input := range gateway.searches {
		if len(input.Search.ProductIDs) != 0 || len(input.Search.SKUIDs) != 1 || input.Search.SKUIDs[0] != "sku-1" {
			t.Fatalf("inventory search must request the exact SKU only: %#v", input.Search)
		}
	}
}
