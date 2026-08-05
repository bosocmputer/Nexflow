package sml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

func TestPartyCacheSalesOnlyDoesNotFetchSuppliers(t *testing.T) {
	var customerCalls atomic.Int32
	var supplierCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ar/customers":
			customerCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":[{"code":"AR001","name":"ลูกค้าทดสอบ"}],"meta":{"total":1,"page":1,"size":200}}`))
		case "/api/v1/ap/suppliers":
			supplierCalls.Add(1)
			http.Error(w, "supplier endpoint must not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewPartyClient(PartyConfig{BaseURL: srv.URL, GUID: "test", Database: "demo"}, zap.NewNop())
	cache := NewPartyCache(client, zap.NewNop()).SalesOnly()
	if err := cache.RefreshNow(context.Background()); err != nil {
		t.Fatalf("RefreshNow() error = %v", err)
	}
	customers, suppliers := cache.Counts()
	if customers != 1 || suppliers != 0 {
		t.Fatalf("Counts() = (%d, %d), want (1, 0)", customers, suppliers)
	}
	if customerCalls.Load() != 1 || supplierCalls.Load() != 0 {
		t.Fatalf("calls customer=%d supplier=%d, want 1 and 0", customerCalls.Load(), supplierCalls.Load())
	}
}
