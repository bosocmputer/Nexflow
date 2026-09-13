package tiktokgateway

import (
	"context"
	"errors"
	"testing"

	"nexflow/internal/services/tiktokshop"
)

func TestProductServiceRequiresApprovedBasicScopeBeforeRead(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{
		AccessToken: "access-secret", ShopID: "7494619203789490654", ShopCipher: "cipher-1",
		GrantedScopes: []string{"seller.order.info"},
	}}
	reader := &fakeTikTokProductReader{}
	service, err := NewProductService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SearchProducts(context.Background(), "aoy", "7494619203789490654", tiktokshop.SearchProductsRequest{PageSize: 100})
	if !errors.Is(err, ErrProductBasicScopeUnavailable) || reader.searchCalls != 0 {
		t.Fatalf("error=%v calls=%d", err, reader.searchCalls)
	}
}

func TestProductServiceReadsCatalogWithTenantScopedCredential(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{
		AccessToken: "access-secret", ShopID: "7494619203789490654", ShopCipher: "cipher-1",
		GrantedScopes: []string{"seller.product.basic"},
	}}
	reader := &fakeTikTokProductReader{searchResult: &tiktokshop.SearchProductsResult{
		TotalCount: 1, Products: []tiktokshop.Product{{ID: "1729429119195974110", Title: "AOY Product", Status: tiktokshop.ProductStatusActivate}},
	}, requestID: "req-products"}
	service, err := NewProductService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SearchProducts(context.Background(), "AOY", "7494619203789490654", tiktokshop.SearchProductsRequest{PageSize: 100})
	if err != nil || result.UpstreamRequestID != "req-products" || len(result.Products) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if credentials.tenant != "aoy" || reader.accessToken != "access-secret" || reader.shopCipher != "cipher-1" {
		t.Fatalf("credentials=%+v reader=%+v", credentials, reader)
	}
}

func TestProductServiceRequiresModifyScopeAndReturnsPartialWriteFailures(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{
		AccessToken: "access-secret", ShopID: "7494619203789490654", ShopCipher: "cipher-1",
		GrantedScopes: []string{"seller.product.basic", "seller.product.write"},
	}}
	reader := &fakeTikTokProductReader{updateResult: &tiktokshop.UpdateInventoryResult{Errors: []tiktokshop.InventoryUpdateError{{
		Code: 12052990, Message: "Check failed", Detail: tiktokshop.InventoryUpdateErrorDetail{SKUID: "1729429119195974111"},
	}}}, requestID: "req-write"}
	service, err := NewProductService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.UpdateInventory(context.Background(), "aoy", "7494619203789490654", "1729429119195974110", tiktokshop.UpdateInventoryRequest{SKUs: []tiktokshop.InventorySKUUpdate{{
		ID: "1729429119195974111", Inventory: []tiktokshop.WarehouseInventoryUpdate{{WarehouseID: "7068517275539719942", Quantity: 7}},
	}}})
	if err != nil || result.UpstreamRequestID != "req-write" || len(result.Errors) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if reader.updateCalls != 1 || reader.productID != "1729429119195974110" {
		t.Fatalf("reader=%+v", reader)
	}
}

type fakeTikTokProductReader struct {
	searchResult *tiktokshop.SearchProductsResult
	product      *tiktokshop.Product
	inventory    *tiktokshop.InventorySearchResult
	updateResult *tiktokshop.UpdateInventoryResult
	requestID    string
	err          error
	accessToken  string
	shopCipher   string
	productID    string
	searchCalls  int
	updateCalls  int
}

func (f *fakeTikTokProductReader) SearchProducts(_ context.Context, accessToken, shopCipher string, _ tiktokshop.SearchProductsRequest) (*tiktokshop.SearchProductsResult, string, error) {
	f.searchCalls++
	f.accessToken, f.shopCipher = accessToken, shopCipher
	return f.searchResult, f.requestID, f.err
}

func (f *fakeTikTokProductReader) GetProduct(_ context.Context, accessToken, shopCipher, productID string) (*tiktokshop.Product, string, error) {
	f.accessToken, f.shopCipher, f.productID = accessToken, shopCipher, productID
	return f.product, f.requestID, f.err
}

func (f *fakeTikTokProductReader) SearchInventory(_ context.Context, accessToken, shopCipher string, _ tiktokshop.InventorySearchRequest) (*tiktokshop.InventorySearchResult, string, error) {
	f.accessToken, f.shopCipher = accessToken, shopCipher
	return f.inventory, f.requestID, f.err
}

func (f *fakeTikTokProductReader) UpdateInventory(_ context.Context, accessToken, shopCipher, productID string, _ tiktokshop.UpdateInventoryRequest) (*tiktokshop.UpdateInventoryResult, string, error) {
	f.updateCalls++
	f.accessToken, f.shopCipher, f.productID = accessToken, shopCipher, productID
	return f.updateResult, f.requestID, f.err
}
