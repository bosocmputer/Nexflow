package tiktokshop

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type catalogGatewayFake struct {
	productPages     map[string]*GatewayProductSearchResponse
	inventoryBySKU   map[string]InventorySearchSKU
	productCalls     []GatewayProductSearchRequest
	inventoryCalls   []GatewayInventorySearchRequest
	err              error
	missingInventory bool
}

func (f *catalogGatewayFake) SearchProducts(_ context.Context, input GatewayProductSearchRequest) (*GatewayProductSearchResponse, error) {
	f.productCalls = append(f.productCalls, input)
	if f.err != nil {
		return nil, f.err
	}
	return f.productPages[input.Search.PageToken], nil
}

func (f *catalogGatewayFake) SearchInventory(_ context.Context, input GatewayInventorySearchRequest) (*GatewayInventorySearchResponse, error) {
	f.inventoryCalls = append(f.inventoryCalls, input)
	if f.err != nil {
		return nil, f.err
	}
	records := make(map[string][]InventorySearchSKU)
	for _, skuID := range input.Search.SKUIDs {
		inventory, ok := f.inventoryBySKU[skuID]
		if !ok || f.missingInventory {
			continue
		}
		productID := ""
		for _, page := range f.productPages {
			for _, product := range page.Products {
				for _, sku := range product.SKUs {
					if sku.ID == skuID {
						productID = product.ID
					}
				}
			}
		}
		records[productID] = append(records[productID], inventory)
	}
	result := &GatewayInventorySearchResponse{UpstreamRequestID: fmt.Sprintf("inventory-%d", len(f.inventoryCalls))}
	for productID, skus := range records {
		result.Inventory = append(result.Inventory, ProductInventoryRecord{ProductID: productID, SKUs: skus})
	}
	return result, nil
}

type catalogStoreFake struct {
	createdShop    string
	createdTrigger string
	replaced       []TikTokCatalogProduct
	finished       []TikTokCatalogRunResult
	err            error
}

func (f *catalogStoreFake) CreateRun(_ context.Context, shopID, trigger string) (string, error) {
	f.createdShop, f.createdTrigger = shopID, trigger
	return "0d0615a1-789c-44ca-95eb-56343232dd3d", f.err
}

func (f *catalogStoreFake) ReplaceCatalog(_ context.Context, result TikTokCatalogRunResult, products []TikTokCatalogProduct) error {
	f.replaced = products
	if f.err == nil {
		f.finished = append(f.finished, result)
	}
	return f.err
}

func (f *catalogStoreFake) FinishRun(_ context.Context, result TikTokCatalogRunResult) error {
	f.finished = append(f.finished, result)
	return nil
}

func TestProductCatalogSyncPaginatesAndReplacesOnlyAfterExactInventory(t *testing.T) {
	productOne := Product{ID: "1001", Title: "One", Status: ProductStatusActivate, SKUs: []ProductSKU{{ID: "2001", SellerSKU: "AOY-001"}}}
	productTwo := Product{ID: "1002", Title: "Two", Status: ProductStatusDraft, SKUs: []ProductSKU{{ID: "2002", SellerSKU: "AOY-002"}}}
	gateway := &catalogGatewayFake{
		productPages: map[string]*GatewayProductSearchResponse{
			"":     {UpstreamRequestID: "products-1", NextPageToken: "next", TotalCount: 2, Products: []Product{productOne}},
			"next": {UpstreamRequestID: "products-2", TotalCount: 2, Products: []Product{productTwo}},
		},
		inventoryBySKU: map[string]InventorySearchSKU{
			"2001": {ID: "2001", TotalAvailableQuantity: 7, TotalCommittedQuantity: 1, WarehouseInventory: []WarehouseInventory{{WarehouseID: "3001", AvailableQuantity: 7, CommittedQuantity: 1}}},
			"2002": {ID: "2002", TotalAvailableQuantity: 2, WarehouseInventory: []WarehouseInventory{{WarehouseID: "3001", AvailableQuantity: 2}}},
		},
	}
	store := &catalogStoreFake{}
	service := NewProductCatalogService(gateway, store)
	result, err := service.Sync(context.Background(), "7494619203789490654", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.PageCount != 2 || result.ProductCount != 2 || result.SKUCount != 2 || result.WarehouseCount != 2 {
		t.Fatalf("result=%+v", result)
	}
	if len(gateway.productCalls) != 2 || len(gateway.inventoryCalls) != 1 || len(store.replaced) != 2 || len(store.finished) != 1 {
		t.Fatalf("gateway=%+v store=%+v", gateway, store)
	}
	if store.replaced[0].Inventory["2001"].TotalAvailableQuantity != 7 || store.finished[0].Status != "succeeded" {
		t.Fatalf("replaced=%+v finished=%+v", store.replaced, store.finished)
	}
}

func TestProductCatalogSyncRejectsMissingInventoryAndKeepsExistingSnapshot(t *testing.T) {
	gateway := &catalogGatewayFake{
		productPages: map[string]*GatewayProductSearchResponse{"": {
			UpstreamRequestID: "products-1", TotalCount: 1,
			Products: []Product{{ID: "1001", Title: "One", Status: ProductStatusActivate, SKUs: []ProductSKU{{ID: "2001"}}}},
		}},
		inventoryBySKU:   map[string]InventorySearchSKU{"2001": {ID: "2001"}},
		missingInventory: true,
	}
	store := &catalogStoreFake{}
	_, err := NewProductCatalogService(gateway, store).Sync(context.Background(), "7494619203789490654", "manual")
	if !errors.Is(err, ErrProductCatalogSourceInvalid) || len(store.replaced) != 0 || len(store.finished) != 1 || store.finished[0].Status != "failed" {
		t.Fatalf("err=%v store=%+v", err, store)
	}
}

func TestProductCatalogSyncRejectsPaginationCycleBeforeReplacement(t *testing.T) {
	gateway := &catalogGatewayFake{productPages: map[string]*GatewayProductSearchResponse{
		"":     {UpstreamRequestID: "products-1", NextPageToken: "next", TotalCount: 2, Products: []Product{{ID: "1001", Title: "One", Status: ProductStatusActivate}}},
		"next": {UpstreamRequestID: "products-2", NextPageToken: "next", TotalCount: 2, Products: []Product{{ID: "1002", Title: "Two", Status: ProductStatusActivate}}},
	}}
	store := &catalogStoreFake{}
	_, err := NewProductCatalogService(gateway, store).Sync(context.Background(), "7494619203789490654", "manual")
	if !errors.Is(err, ErrProductCatalogSourceInvalid) || len(store.replaced) != 0 || store.finished[0].ErrorCode != "source_invalid" {
		t.Fatalf("err=%v store=%+v", err, store)
	}
}
