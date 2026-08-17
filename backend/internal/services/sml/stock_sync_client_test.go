package sml

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStockSyncClientSendsTenantAndTypedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tenant") != "aoy" || r.Header.Get("X-Api-Key") != "secret" {
			t.Fatalf("missing tenant authentication headers")
		}
		var req StockBalanceBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Scopes) != 1 || req.Scopes[0].Locations[0].Warehouse != "01" || !req.Scopes[0].IncludeItemExcludedLocations {
			t.Fatalf("unexpected request: %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"as_of_date":"2026-08-10","scopes":[{"scope_id":"shop:1","items":[{"item_code":"SKU1","balance_qty":12,"excluded_locations":[{"warehouse_code":"W2","warehouse_name":"คลังสำรอง","location_code":"S2","location_name":"ชั้นสอง","unit_code":"ชิ้น","balance_qty":-2}]}],"excluded_locations":[{"warehouse_code":"W2","warehouse_name":"คลังสำรอง","location_code":"S2","location_name":"ชั้นสอง","unit_code":"ชิ้น","balance_qty":-3}]}],"checked_at":"2026-08-10T00:00:00Z"}}`))
	}))
	defer server.Close()

	client := NewStockSyncClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "aoy"})
	result, err := client.BalancesBatch(context.Background(), StockBalanceBatchRequest{
		AsOfDate: "2026-08-10",
		Scopes:   []StockBalanceScopeRequest{{ScopeID: "shop:1", ItemCodes: []string{"SKU1"}, ScopeMode: "selected", Locations: []StockLocationPair{{Warehouse: "01", Location: "01"}}, IncludeItemExcludedLocations: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scopes[0].Items[0].BalanceQty != 12 {
		t.Fatalf("unexpected balance: %#v", result)
	}
	if len(result.Scopes[0].Items[0].ExcludedLocations) != 1 || result.Scopes[0].Items[0].ExcludedLocations[0].BalanceQty != -2 {
		t.Fatalf("unexpected item excluded locations: %#v", result.Scopes[0].Items[0].ExcludedLocations)
	}
	if len(result.Scopes[0].ExcludedLocations) != 1 || result.Scopes[0].ExcludedLocations[0].WarehouseName != "คลังสำรอง" || result.Scopes[0].ExcludedLocations[0].UnitCode != "ชิ้น" || result.Scopes[0].ExcludedLocations[0].BalanceQty != -3 {
		t.Fatalf("unexpected excluded locations: %#v", result.Scopes[0].ExcludedLocations)
	}
}

func TestStockSyncClientCatalogRangeUsesOverlapWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/stock-catalog" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("size") != "500" {
			t.Fatalf("unexpected catalog request: %s", r.URL.String())
		}
		if r.URL.Query().Get("updated_from") != "2026-08-10T01:00:00Z" || r.URL.Query().Get("updated_to") != "2026-08-10T02:00:00Z" {
			t.Fatalf("missing catalog window: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[],"meta":{"total":0,"page":2,"size":500}}`))
	}))
	defer server.Close()
	client := NewStockSyncClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "demo"})
	from := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	if _, err := client.CatalogRange(context.Background(), 2, 500, &from, &to); err != nil {
		t.Fatal(err)
	}
}

func TestStockSyncClientRejectsFailedEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"stock_calculation_timeout","message":"timed out"}}`))
	}))
	defer server.Close()
	client := NewStockSyncClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "demo"})
	if _, err := client.Locations(context.Background(), "2026-08-10"); err == nil {
		t.Fatal("expected failed envelope error")
	}
}
