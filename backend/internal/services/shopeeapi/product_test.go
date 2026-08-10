package shopeeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductBusinessErrorPreservesSafeMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":      "no_permission",
			"message":    "Product API is not authorized",
			"request_id": "shopee-request-1",
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1, PartnerKey: "secret"})
	_, err := client.GetWarehouseDetail(t.Context(), "access", 2)
	var businessErr *BusinessError
	if !errors.As(err, &businessErr) {
		t.Fatalf("error = %#v, want BusinessError", err)
	}
	if businessErr.Operation != "get_warehouse_detail" || businessErr.Code != "no_permission" || businessErr.RequestID != "shopee-request-1" {
		t.Fatalf("business error = %+v", businessErr)
	}
}

func TestWarehouseDetailResponseAcceptsCurrentArrayContract(t *testing.T) {
	var response WarehouseDetailResponse
	if err := json.Unmarshal([]byte(`{
		"response":[{"warehouse_id":6,"warehouse_name":"Main","warehouse_type":1,"location_id":"TH-1"}]
	}`), &response); err != nil {
		t.Fatal(err)
	}
	locations := response.Locations()
	if len(locations) != 1 || locations[0].LocationID != "TH-1" || locations[0].WarehouseType != 1 {
		t.Fatalf("locations = %+v", locations)
	}
}

func TestWarehouseDetailResponseAcceptsLegacyObjectContract(t *testing.T) {
	var response WarehouseDetailResponse
	if err := json.Unmarshal([]byte(`{
		"response":{"location_list":[{"warehouse_id":7,"location_id":"TH-2"}]}
	}`), &response); err != nil {
		t.Fatal(err)
	}
	locations := response.Locations()
	if len(locations) != 1 || locations[0].LocationID != "TH-2" {
		t.Fatalf("locations = %+v", locations)
	}
}

func TestClientGetItemListUsesBoundedPageAndUpdateWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathProductGetItemList {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("page_size") != "100" || r.URL.Query().Get("offset") != "0" {
			t.Fatalf("paging query = %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("update_time_from") != "100" || r.URL.Query().Get("update_time_to") != "200" {
			t.Fatalf("update query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{"item": []map[string]any{{"item_id": 10, "item_status": "NORMAL", "update_time": 150}}},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1, PartnerKey: "secret"})
	out, err := client.GetItemList(t.Context(), "access", 2, ItemListRequest{Offset: -1, PageSize: 500, UpdateTimeFrom: 100, UpdateTimeTo: 200})
	if err != nil {
		t.Fatalf("GetItemList() error = %v", err)
	}
	if len(out.Response.Item) != 1 || out.Response.Item[0].ItemID != 10 {
		t.Fatalf("response = %+v", out.Response)
	}
}

func TestClientUpdateStockSendsSellerStockOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != PathProductUpdateStock || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req UpdateStockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.ItemID != 10 || len(req.StockList) != 1 || req.StockList[0].SellerStock[0].Stock != 80 {
			t.Fatalf("payload = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{"success_list": []map[string]any{{"model_id": 0}}},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1, PartnerKey: "secret"})
	out, err := client.UpdateStock(t.Context(), "access", 2, UpdateStockRequest{
		ItemID:    10,
		StockList: []ModelStock{{ModelID: 0, SellerStock: []SellerStock{{Stock: 80}}}},
	})
	if err != nil {
		t.Fatalf("UpdateStock() error = %v", err)
	}
	if requests != 1 || len(out.Response.SuccessList) != 1 {
		t.Fatalf("requests=%d response=%+v", requests, out.Response)
	}
}

func TestValidateUpdateStockRejectsUnsafePayload(t *testing.T) {
	tests := []UpdateStockRequest{
		{},
		{ItemID: 1},
		{ItemID: 1, StockList: []ModelStock{{ModelID: 1, SellerStock: []SellerStock{{Stock: -1}}}}},
		{ItemID: 1, StockList: []ModelStock{{ModelID: 1, SellerStock: []SellerStock{{Stock: 1}}}, {ModelID: 1, SellerStock: []SellerStock{{Stock: 2}}}}},
	}
	for _, req := range tests {
		if err := ValidateUpdateStockRequest(req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
}

func TestStringIDAcceptsShopeeStringAndNumber(t *testing.T) {
	var payload struct {
		StringID StringID `json:"string_id"`
		NumberID StringID `json:"number_id"`
	}
	if err := json.Unmarshal([]byte(`{"string_id":"TH-01","number_id":123}`), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.StringID != "TH-01" || payload.NumberID != "123" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestSellerStockAtLocationUsesWritableSellerStock(t *testing.T) {
	var info StockInfoV2
	info.SummaryInfo.TotalAvailableStock = 7
	info.SellerStock = []SellerStock{
		{LocationID: "A", Stock: 10},
		{LocationID: "B", Stock: 20},
	}
	if got, ok := SellerStockAtLocation(info, "B"); !ok || got != 20 {
		t.Fatalf("SellerStockAtLocation = %d/%v, want 20/true", got, ok)
	}
	if got := CurrentSellerStock(info, "A"); got != 10 {
		t.Fatalf("CurrentSellerStock = %d, want seller stock 10", got)
	}
	if got := CurrentSellerStock(info, "missing"); got != 7 {
		t.Fatalf("CurrentSellerStock fallback = %d, want available summary 7", got)
	}
}
