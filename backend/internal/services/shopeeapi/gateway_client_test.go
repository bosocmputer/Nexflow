package shopeeapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nexflow/internal/services/gatewayauth"
)

func TestGatewayClientGetOrderListUsesSignedTenantRequestWithoutToken(t *testing.T) {
	now := time.Unix(1784070000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != GatewayExecutePath {
			t.Fatalf("path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		verifier := gatewayauth.Verifier{
			ResolveSecret: func(context.Context, string) (string, error) { return "tenant-secret", nil },
			Now:           func() time.Time { return now },
		}
		if _, err := verifier.Verify(r.Context(), r, body); err != nil {
			t.Fatalf("gateway signature error = %v", err)
		}
		if string(body) == "" || string(body) == "access-token" {
			t.Fatalf("unexpected request body %q", string(body))
		}
		var req gatewayExecuteRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Operation != "get_order_list" || req.ShopID != 987654 {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"response": map[string]interface{}{
					"more":       false,
					"order_list": []map[string]string{{"order_sn": "ORDER-1", "order_status": "READY_TO_SHIP"}},
				},
			},
		})
	}))
	defer server.Close()

	client := NewGateway(GatewayConfig{BaseURL: server.URL, Tenant: "aoy", SharedSecret: "tenant-secret", Now: func() time.Time { return now }})
	out, err := client.GetOrderList(t.Context(), "must-not-be-forwarded", 987654, OrderListRequest{TimeFrom: 1, TimeTo: 2})
	if err != nil {
		t.Fatalf("GetOrderList() error = %v", err)
	}
	if len(out.Response.OrderList) != 1 || out.Response.OrderList[0].OrderSN != "ORDER-1" {
		t.Fatalf("response = %+v", out.Response)
	}
}

func TestGatewayClientCreateAuthURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{
				"auth_url":     "https://partner.shopeemobile.com/api/v2/shop/auth_partner?state=signed",
				"redirect_url": "https://shopee-gateway.nextstep-soft.com/api/shopee/callback",
			},
		})
	}))
	defer server.Close()
	client := NewGateway(GatewayConfig{BaseURL: server.URL, Tenant: "demo", SharedSecret: "secret"})
	out, err := client.CreateAuthURL(t.Context(), GatewayAuthURLRequest{UserID: "user-1", ReturnURL: "https://nexflow.nextstep-soft.com/settings/shopee-connections"})
	if err != nil {
		t.Fatalf("CreateAuthURL() error = %v", err)
	}
	if out.AuthURL == "" || out.RedirectURL == "" {
		t.Fatalf("response = %+v", out)
	}
}

func TestGatewayClientRejectsGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"code": "shopee_timeout", "message": "Shopee timeout", "retryable": true},
		})
	}))
	defer server.Close()
	client := NewGateway(GatewayConfig{BaseURL: server.URL, Tenant: "demo", SharedSecret: "secret"})
	_, err := client.GetShopInfo(t.Context(), "", 123)
	var gatewayErr *GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "shopee_timeout" || !gatewayErr.Retryable {
		t.Fatalf("error = %#v", err)
	}
}

func TestGatewayClientUpdateStockUsesDedicatedOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gatewayExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Operation != "update_stock" || req.ShopID != 123 {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"response": map[string]any{"success_list": []map[string]any{{"model_id": 0}}}},
		})
	}))
	defer server.Close()
	client := NewGateway(GatewayConfig{BaseURL: server.URL, Tenant: "aoy", SharedSecret: "secret"})
	out, err := client.UpdateStock(t.Context(), "", 123, UpdateStockRequest{
		ItemID: 1, StockList: []ModelStock{{ModelID: 0, SellerStock: []SellerStock{{Stock: 10}}}},
	})
	if err != nil || len(out.Response.SuccessList) != 1 {
		t.Fatalf("response=%+v error=%v", out, err)
	}
}
