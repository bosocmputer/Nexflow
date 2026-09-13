package tiktokshop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProductClientSearchesCurrentCatalogWithExactSignedBody(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != PathSearchProducts {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		provided := query.Get("sign")
		query.Del("sign")
		expected, err := SignRequest("app-secret", PathSearchProducts, query, body, false)
		if err != nil || provided != expected {
			t.Fatalf("signature=%q want=%q err=%v", provided, expected, err)
		}
		if query.Get("page_size") != "100" || query.Get("shop_cipher") != "shop-cipher" || r.Header.Get("x-tts-access-token") != "seller-token" {
			t.Fatalf("query=%v headers=%v", query, r.Header)
		}
		if string(body) != `{"status":"ALL"}` {
			t.Fatalf("body=%s", body)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-products","data":{"next_page_token":"next","total_count":1,"products":[{"id":"1729429119195974110","title":"AOY Product","status":"ACTIVATE","skus":[{"id":"1729429119195974111","seller_sku":"AOY-001","inventory":[{"warehouse_id":"7068517275539719942","quantity":9}]}]}]}}`))
	}))
	defer server.Close()

	client, err := NewProductClient(ProductClientConfig{
		BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.SearchProducts(context.Background(), "seller-token", "shop-cipher", SearchProductsRequest{PageSize: 100, Status: ProductStatusAll})
	if err != nil || requestID != "req-products" || result.TotalCount != 1 || len(result.Products) != 1 || result.Products[0].SKUs[0].Inventory[0].Quantity != 9 {
		t.Fatalf("SearchProducts=%+v requestID=%q err=%v", result, requestID, err)
	}
}

func TestProductClientUpdatesInventoryAndPreservesPerSKUFailures(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		wantPath := "/product/202309/products/1729429119195974110/inventory/update"
		if r.Method != http.MethodPost || r.URL.Path != wantPath {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		var input UpdateInventoryRequest
		if err := json.Unmarshal(body, &input); err != nil || len(input.SKUs) != 1 || input.SKUs[0].Inventory[0].Quantity != 7 {
			t.Fatalf("input=%+v err=%v", input, err)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-write","data":{"errors":[{"code":12052990,"message":"Check failed","detail":{"sku_id":"1729429119195974111","extra_errors":[{"warehouse_id":"bad","code":12052097,"message":"The warehouse does not exist"}]}}]}}`))
	}))
	defer server.Close()
	client, err := NewProductClient(ProductClientConfig{BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.UpdateInventory(context.Background(), "seller-token", "shop-cipher", "1729429119195974110", UpdateInventoryRequest{SKUs: []InventorySKUUpdate{{
		ID: "1729429119195974111", Inventory: []WarehouseInventoryUpdate{{WarehouseID: "7068517275539719942", Quantity: 7}},
	}}})
	if err != nil || requestID != "req-write" || len(result.Errors) != 1 || result.Errors[0].Detail.SKUID != "1729429119195974111" {
		t.Fatalf("UpdateInventory=%+v requestID=%q err=%v", result, requestID, err)
	}
}

func TestProductClientReadsBackInventoryByExactSKU(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != PathInventorySearch || string(body) != `{"sku_ids":["1729429119195974111"]}` {
			t.Fatalf("path=%s body=%s", r.URL.Path, body)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-readback","data":{"inventory":[{"product_id":"1729429119195974110","skus":[{"id":"1729429119195974111","seller_sku":"AOY-001","total_available_quantity":7,"total_committed_quantity":1,"warehouse_inventory":[{"warehouse_id":"7068517275539719942","available_quantity":7,"committed_quantity":1}]}]}]}}`))
	}))
	defer server.Close()
	client, err := NewProductClient(ProductClientConfig{BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.SearchInventory(context.Background(), "seller-token", "shop-cipher", InventorySearchRequest{SKUIDs: []string{"1729429119195974111"}})
	if err != nil || requestID != "req-readback" || len(result.Inventory) != 1 || result.Inventory[0].SKUs[0].TotalAvailableQuantity != 7 {
		t.Fatalf("SearchInventory=%+v requestID=%q err=%v", result, requestID, err)
	}
}

func TestProductClientRejectsAmbiguousOrUnsafeInventoryInputBeforeNetwork(t *testing.T) {
	client, err := NewProductClient(ProductClientConfig{BaseURL: "https://example.test", AppKey: "app-key", AppSecret: "app-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.SearchInventory(context.Background(), "token", "cipher", InventorySearchRequest{ProductIDs: []string{"product-1"}, SKUIDs: []string{"sku-1"}}); err == nil {
		t.Fatal("expected product_ids + sku_ids to fail closed")
	}
	if _, _, err := client.UpdateInventory(context.Background(), "token", "cipher", "../product", UpdateInventoryRequest{}); err == nil {
		t.Fatal("expected unsafe product id to fail closed")
	}
}
