package linemyshop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListOrdersSendsAuthAndQuery(t *testing.T) {
	var gotAPIKey, gotUA, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-KEY")
		gotUA = r.Header.Get("User-Agent")
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/myshop/v1/orders" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := NewClient("api-key", WithBaseURL(srv.URL), WithUserAgent("Nexflow Test"))
	if _, err := c.ListOrders(context.Background(), ListOrdersQuery{Page: 2, PerPage: 100, PaymentStatus: []string{"PAID"}, ShipmentStatus: []string{"SHIPMENT_READY"}}); err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if gotAPIKey != "api-key" {
		t.Fatalf("X-API-KEY = %q", gotAPIKey)
	}
	if gotUA != "Nexflow Test" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	for _, want := range []string{"page=2", "perPage=100", "paymentStatus=PAID", "shipmentStatus=SHIPMENT_READY"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestClientReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("api-key", WithBaseURL(srv.URL))
	_, err := c.GetOrder(context.Background(), "LMS-1")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
}
