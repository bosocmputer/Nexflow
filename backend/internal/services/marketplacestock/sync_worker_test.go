package marketplacestock

import (
	"context"
	"testing"
	"time"

	"nexflow/internal/services/shopeeapi"
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

type syncWorkerShopeeGatewayFake struct {
	modelStock int64
	writes     []shopeeapi.UpdateStockRequest
}

func (f *syncWorkerShopeeGatewayFake) GetItemBaseInfo(_ context.Context, _ string, _ int64, _ []int64) (*shopeeapi.ItemBaseInfoResponse, error) {
	return nil, nil
}

func (f *syncWorkerShopeeGatewayFake) GetModelList(_ context.Context, _ string, _ int64, itemID int64) (*shopeeapi.ModelListResponse, error) {
	if itemID != 4940107291 {
		return &shopeeapi.ModelListResponse{}, nil
	}
	response := &shopeeapi.ModelListResponse{}
	response.Response.Model = []shopeeapi.ProductModel{{
		ModelID: 42891916896,
		StockInfoV2: shopeeapi.StockInfoV2{SellerStock: []shopeeapi.SellerStock{{
			Stock: f.modelStock,
		}}},
	}}
	return response, nil
}

func (f *syncWorkerShopeeGatewayFake) UpdateStock(_ context.Context, _ string, _ int64, request shopeeapi.UpdateStockRequest) (*shopeeapi.UpdateStockResponse, error) {
	f.writes = append(f.writes, request)
	f.modelStock = request.StockList[0].SellerStock[0].Stock
	response := &shopeeapi.UpdateStockResponse{}
	response.Response.SuccessList = []shopeeapi.StockUpdateSuccess{{ModelID: request.StockList[0].ModelID}}
	return response, nil
}

func TestSyncShopeeReadsExactModelWritesAbsoluteTargetAndReadsBack(t *testing.T) {
	gateway := &syncWorkerShopeeGatewayFake{modelStock: 4}
	worker := &SyncWorker{shopee: gateway}
	status, previous, actual, code, message := worker.syncShopee(context.Background(), Member{
		Source: "shopee", AccountKey: "shop:264993963", ExternalProductID: "4940107291", ExternalSKUID: "42891916896",
	}, 7)
	if status != "changed" || previous != 4 || actual != 7 || code != "" || message != "" {
		t.Fatalf("syncShopee() = %q,%d,%d,%q,%q", status, previous, actual, code, message)
	}
	if len(gateway.writes) != 1 {
		t.Fatalf("write count = %d, want 1", len(gateway.writes))
	}
	write := gateway.writes[0]
	if write.ItemID != 4940107291 || len(write.StockList) != 1 || write.StockList[0].ModelID != 42891916896 || len(write.StockList[0].SellerStock) != 1 || write.StockList[0].SellerStock[0].Stock != 7 {
		t.Fatalf("unexpected absolute Shopee write: %#v", write)
	}
}

type syncWorkerShopeeReadBackMismatchFake struct{ syncWorkerShopeeGatewayFake }

func (f *syncWorkerShopeeReadBackMismatchFake) UpdateStock(_ context.Context, _ string, _ int64, request shopeeapi.UpdateStockRequest) (*shopeeapi.UpdateStockResponse, error) {
	f.writes = append(f.writes, request)
	response := &shopeeapi.UpdateStockResponse{}
	response.Response.SuccessList = []shopeeapi.StockUpdateSuccess{{ModelID: request.StockList[0].ModelID}}
	return response, nil
}

func TestSyncShopeeFailsClosedWhenReadBackDoesNotMatchTarget(t *testing.T) {
	gateway := &syncWorkerShopeeReadBackMismatchFake{syncWorkerShopeeGatewayFake{modelStock: 4}}
	worker := &SyncWorker{shopee: gateway}
	status, previous, actual, code, _ := worker.syncShopee(context.Background(), Member{
		Source: "shopee", AccountKey: "shop:264993963", ExternalProductID: "4940107291", ExternalSKUID: "42891916896",
	}, 7)
	if status != "failed" || previous != 4 || actual != 4 || code != "read_back_mismatch" {
		t.Fatalf("syncShopee() = %q,%d,%d,%q", status, previous, actual, code)
	}
}

type syncWorkerShopeeEventuallyConsistentFake struct {
	stock      int64
	target     int64
	staleReads int
}

func (f *syncWorkerShopeeEventuallyConsistentFake) GetItemBaseInfo(_ context.Context, _ string, _ int64, _ []int64) (*shopeeapi.ItemBaseInfoResponse, error) {
	return nil, nil
}

func (f *syncWorkerShopeeEventuallyConsistentFake) GetModelList(_ context.Context, _ string, _ int64, _ int64) (*shopeeapi.ModelListResponse, error) {
	if f.target != 0 {
		if f.staleReads > 0 {
			f.staleReads--
		} else {
			f.stock = f.target
		}
	}
	response := &shopeeapi.ModelListResponse{}
	response.Response.Model = []shopeeapi.ProductModel{{
		ModelID:     42891916896,
		StockInfoV2: shopeeapi.StockInfoV2{SellerStock: []shopeeapi.SellerStock{{Stock: f.stock}}},
	}}
	return response, nil
}

func (f *syncWorkerShopeeEventuallyConsistentFake) UpdateStock(_ context.Context, _ string, _ int64, request shopeeapi.UpdateStockRequest) (*shopeeapi.UpdateStockResponse, error) {
	f.target = request.StockList[0].SellerStock[0].Stock
	response := &shopeeapi.UpdateStockResponse{}
	response.Response.SuccessList = []shopeeapi.StockUpdateSuccess{{ModelID: request.StockList[0].ModelID}}
	return response, nil
}

func TestSyncShopeeAcceptsOnlyConfirmedEventuallyConsistentReadBack(t *testing.T) {
	gateway := &syncWorkerShopeeEventuallyConsistentFake{stock: 110, staleReads: 1}
	worker := &SyncWorker{shopee: gateway, shopeeReadBackAttempts: 2, shopeeReadBackInterval: time.Millisecond}
	status, previous, actual, code, message := worker.syncShopee(context.Background(), Member{
		Source: "shopee", AccountKey: "shop:264993963", ExternalProductID: "4940107291", ExternalSKUID: "42891916896",
	}, 47)
	if status != "changed" || previous != 110 || actual != 47 || code != "" || message != "" {
		t.Fatalf("syncShopee() = %q,%d,%d,%q,%q", status, previous, actual, code, message)
	}
}
