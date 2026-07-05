package sml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestNextStepMarketplaceClientFetch(t *testing.T) {
	includeOrders := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/marketplace/nextstep/orders" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant"); got != "aoy" {
			t.Fatalf("X-Tenant = %q", got)
		}
		if got := r.Header.Get("X-Api-Key"); got != "smlx" {
			t.Fatalf("X-Api-Key = %q", got)
		}
		if got := r.URL.Query().Get("cust_code"); got != "C001" {
			t.Fatalf("cust_code = %q", got)
		}
		if got := r.URL.Query().Get("date_from"); got != "2026-07-01" {
			t.Fatalf("date_from = %q", got)
		}
		if got := r.URL.Query().Get("search"); got != "MQT2607" {
			t.Fatalf("search = %q", got)
		}
		if got := r.URL.Query().Get("include_orders"); got != "false" {
			t.Fatalf("include_orders = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"summary":{"total_orders":1,"total_amount":1200,"status_counts":{"success":1}},"orders":[{"doc_no":"MQT26070001","doc_date":"2026-07-03","total_amount":1200,"status":"success"}],"trend":[{"date":"2026-07-01","total_amount":0},{"date":"2026-07-02","total_amount":0},{"date":"2026-07-03","total_amount":1200}],"meta":{"tenant":"aoy","cust_code":"C001","doc_prefix":"MQT","date_from":"2026-07-01","date_to":"2026-07-03","date_basis":"ic_qt.doc_date","source":"sml.ic_trans","search":"MQT2607","page":1,"size":5,"total":1}}}`))
	}))
	defer srv.Close()

	client := NewNextStepMarketplaceClient(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Database: "aoy",
	}, zap.NewNop()).WithHTTPClient(srv.Client())

	got, err := client.Fetch(context.Background(), NextStepMarketplaceRequest{
		CustCode:      "C001",
		DateFrom:      "2026-07-01",
		DateTo:        "2026-07-03",
		Search:        "MQT2607",
		Page:          1,
		Size:          5,
		IncludeOrders: &includeOrders,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.TotalOrders != 1 || got.Summary.StatusCounts["success"] != 1 {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if len(got.Orders) != 1 || got.Orders[0].DocNo != "MQT26070001" {
		t.Fatalf("orders = %+v", got.Orders)
	}
	if len(got.Trend) != 3 || got.Trend[2].TotalAmount != 1200 {
		t.Fatalf("trend = %+v", got.Trend)
	}
}

func TestNextStepMarketplaceClientRejectsMissingConfig(t *testing.T) {
	client := NewNextStepMarketplaceClient(PartyConfig{}, zap.NewNop())
	if _, err := client.Fetch(context.Background(), NextStepMarketplaceRequest{
		CustCode: "C001",
		DateFrom: "2026-07-01",
		DateTo:   "2026-07-03",
	}); err == nil {
		t.Fatal("expected missing config error")
	}
}
