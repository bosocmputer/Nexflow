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
		if req.AvailabilityMode != "net_sale_order_v1" {
			t.Fatalf("availability mode = %q", req.AvailabilityMode)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"as_of_date":"2026-08-10","mode_applied":"net_sale_order_v1","schema_version":"stock-availability-v1","source_snapshot_at":"2026-08-10T00:00:00Z","source_semantics_fingerprint":"sha256:test","scopes":[{"scope_id":"shop:1","items":[{"item_code":"SKU1","item_name":"สินค้า A","unit_code":"กล่อง","physical_balance_qty":20,"outstanding_sales_order_qty":8,"available_balance_qty":12,"physical_balance_qty_exact":"20","outstanding_sales_order_qty_exact":"8","available_balance_qty_exact":"12","balance_qty_exact":"12","availability_status":"ready","balance_qty":12,"excluded_locations":[{"warehouse_code":"W2","warehouse_name":"คลังสำรอง","location_code":"S2","location_name":"ชั้นสอง","unit_code":"กล่อง","balance_qty":-2}]}],"excluded_locations":[{"warehouse_code":"W2","warehouse_name":"คลังสำรอง","location_code":"S2","location_name":"ชั้นสอง","unit_code":"กล่อง","balance_qty":-3}]}],"checked_at":"2026-08-10T00:00:00Z"}}`))
	}))
	defer server.Close()

	client := NewStockSyncClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "aoy"})
	result, err := client.BalancesBatch(context.Background(), StockBalanceBatchRequest{
		AsOfDate: "2026-08-10", AvailabilityMode: "net_sale_order_v1",
		Scopes: []StockBalanceScopeRequest{{ScopeID: "shop:1", ItemCodes: []string{"SKU1"}, ScopeMode: "selected", Locations: []StockLocationPair{{Warehouse: "01", Location: "01"}}, IncludeItemExcludedLocations: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scopes[0].Items[0].ItemName != "สินค้า A" || result.Scopes[0].Items[0].UnitCode != "กล่อง" || result.Scopes[0].Items[0].BalanceQty != 12 {
		t.Fatalf("unexpected balance: %#v", result)
	}
	if result.ModeApplied != "net_sale_order_v1" || result.SourceSemanticsFingerprint != "sha256:test" || result.Scopes[0].Items[0].OutstandingSalesOrderQtyExact != "8" {
		t.Fatalf("unexpected net availability evidence: %#v", result)
	}
	if len(result.Scopes[0].Items[0].ExcludedLocations) != 1 || result.Scopes[0].Items[0].ExcludedLocations[0].BalanceQty != -2 {
		t.Fatalf("unexpected item excluded locations: %#v", result.Scopes[0].Items[0].ExcludedLocations)
	}
	if len(result.Scopes[0].ExcludedLocations) != 1 || result.Scopes[0].ExcludedLocations[0].WarehouseName != "คลังสำรอง" || result.Scopes[0].ExcludedLocations[0].UnitCode != "กล่อง" || result.Scopes[0].ExcludedLocations[0].BalanceQty != -3 {
		t.Fatalf("unexpected excluded locations: %#v", result.Scopes[0].ExcludedLocations)
	}
}

func TestStockSyncClientLoadsAvailabilityCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/stock-capabilities" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"availability_modes":["physical_v1","net_sale_order_v1"],"schema_version":"stock-availability-v1","source_semantics_fingerprint":"sha256:test","decimal_quantity_format":"string","max_item_codes":5000}}`))
	}))
	defer server.Close()
	client := NewStockSyncClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "aoy"})
	capabilities, err := client.StockCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.SchemaVersion != "stock-availability-v1" || capabilities.SourceSemanticsFingerprint != "sha256:test" || capabilities.MaxItemCodes != 5000 {
		t.Fatalf("capabilities = %#v", capabilities)
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

func TestStockCatalogUnitPreservesExactDecimalFactors(t *testing.T) {
	var unit StockCatalogUnit
	if err := json.Unmarshal([]byte(`{"code":"แพ็ค","stand_value":0.100000000000000001,"divide_value":"3.000","ratio":0.03333333333333333}`), &unit); err != nil {
		t.Fatalf("decode unit: %v", err)
	}
	if unit.StandValueExact != "0.100000000000000001" || unit.DivideValueExact != "3.000" {
		t.Fatalf("exact factors = %q/%q", unit.StandValueExact, unit.DivideValueExact)
	}
	if unit.StandValue <= 0 || unit.DivideValue != 3 {
		t.Fatalf("numeric factors = %v/%v", unit.StandValue, unit.DivideValue)
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
