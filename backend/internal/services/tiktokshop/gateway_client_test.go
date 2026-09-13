package tiktokshop

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

func TestGatewayClientCreatesAuthURLAndListsTenantConnections(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tenant := r.Header.Get(gatewayauth.HeaderTenant)
		timestamp := r.Header.Get(gatewayauth.HeaderTimestamp)
		nonce := r.Header.Get(gatewayauth.HeaderNonce)
		want := gatewayauth.Sign("tenant-secret", r.Method, r.URL.RequestURI(), tenant, timestamp, nonce, body)
		if tenant != "aoy" || r.Header.Get(gatewayauth.HeaderSignature) != want {
			t.Fatalf("invalid signed request headers: %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case GatewayOAuthPath:
			var input GatewayAuthURLRequest
			if err := json.Unmarshal(body, &input); err != nil || input.UserID != "admin-1" {
				t.Fatalf("OAuth input = %+v, %v", input, err)
			}
			_, _ = w.Write([]byte(`{"data":{"auth_url":"https://services.tiktokshop.com/open/authorize?state=signed","redirect_url":"https://gateway.example/api/tiktok-shop/callback","expires_at":"2026-09-10T12:15:00Z"}}`))
		case GatewayConnectionsPath:
			_, _ = w.Write([]byte(`{"data":[{"gateway_connection_id":"connection-1","shop_id":"7000714532876273420","shop_name":"AOY Main","shop_region":"TH","seller_type":"LOCAL","shop_code":"THAOY1","granted_scopes":["seller.authorization.info","seller.order.info"],"access_expires_at":"2026-09-11T10:00:00Z","refresh_expires_at":"2026-10-10T10:00:00Z","disabled":false,"connected_at":"2026-09-10T10:00:00Z","updated_at":"2026-09-10T10:00:00Z"}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewGatewayClient(GatewayClientConfig{
		BaseURL: server.URL, Tenant: "aoy", SharedSecret: "tenant-secret",
		HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	auth, err := client.CreateAuthURL(context.Background(), GatewayAuthURLRequest{UserID: "admin-1", ReturnURL: "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop"})
	if err != nil || auth.AuthURL == "" || auth.RedirectURL == "" {
		t.Fatalf("CreateAuthURL() = %+v, %v", auth, err)
	}
	connections, err := client.ListConnections(context.Background())
	if err != nil || len(connections) != 1 || connections[0].ShopID != "7000714532876273420" || connections[0].ShopName != "AOY Main" {
		t.Fatalf("ListConnections() = %+v, %v", connections, err)
	}
}

func TestGatewayClientRejectsUseWhenNotConfigured(t *testing.T) {
	client := NewGatewayClient(GatewayClientConfig{})
	if client.Configured() {
		t.Fatal("empty gateway client must not be configured")
	}
	if _, err := client.ListConnections(context.Background()); !errors.Is(err, ErrGatewayNotConfigured) {
		t.Fatalf("ListConnections() error = %v, want ErrGatewayNotConfigured", err)
	}
}

func TestGatewayClientRejectsInvalidBaseURLAsNotConfigured(t *testing.T) {
	client := NewGatewayClient(GatewayClientConfig{
		BaseURL: "https://gateway.example.test?unexpected=1", Tenant: "aoy", SharedSecret: "tenant-secret",
	})
	if client.Configured() {
		t.Fatal("gateway client with query-bearing base URL must not be configured")
	}
	if _, err := client.ListConnections(context.Background()); !errors.Is(err, ErrGatewayNotConfigured) {
		t.Fatalf("ListConnections() error = %v, want ErrGatewayNotConfigured", err)
	}
}

func TestGatewayClientReadsOrdersThroughTenantScopedGateway(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tenant := r.Header.Get(gatewayauth.HeaderTenant)
		timestamp := r.Header.Get(gatewayauth.HeaderTimestamp)
		nonce := r.Header.Get(gatewayauth.HeaderNonce)
		want := gatewayauth.Sign("tenant-secret", r.Method, r.URL.RequestURI(), tenant, timestamp, nonce, body)
		if tenant != "aoy" || r.Header.Get(gatewayauth.HeaderSignature) != want {
			t.Fatalf("invalid signed request headers: %v", r.Header)
		}
		switch r.URL.Path {
		case GatewayOrderSearchPath:
			var input GatewayOrderSearchRequest
			if err := json.Unmarshal(body, &input); err != nil || input.ShopID != "shop-1" || input.Search.PageSize != 20 {
				t.Fatalf("search input = %+v, %v", input, err)
			}
			_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"tts-list","next_page_token":"next","total_count":1,"orders":[{"id":"order-1","status":"AWAITING_SHIPMENT"}]}}`))
		case GatewayOrderDetailsPath:
			var input GatewayOrderDetailsRequest
			if err := json.Unmarshal(body, &input); err != nil || input.ShopID != "shop-1" || len(input.OrderIDs) != 1 {
				t.Fatalf("details input = %+v, %v", input, err)
			}
			_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"tts-detail","orders":[{"id":"order-1","status":"COMPLETED"}]}}`))
		case GatewayShipmentRecipientPath:
			var input GatewayShipmentRecipientRequest
			if err := json.Unmarshal(body, &input); err != nil || input.ShopID != "shop-1" || input.OrderID != "order-1" {
				t.Fatalf("shipment recipient input = %+v, %v", input, err)
			}
			_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"tts-recipient","recipient":{"order_id":"order-1","name":"Recipient","address":"Bangkok","telephone":"0900000000"}}}`))
		case GatewayOrderPriceDetailPath:
			var input GatewayOrderPriceDetailRequest
			if err := json.Unmarshal(body, &input); err != nil || input.ShopID != "shop-1" || input.OrderID != "order-1" {
				t.Fatalf("price detail input = %+v, %v", input, err)
			}
			_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"tts-price","price_detail":{"currency":"THB","payment":"307.49","sku_sale_price":"300.00"}}}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewGatewayClient(GatewayClientConfig{
		BaseURL: server.URL, Tenant: "aoy", SharedSecret: "tenant-secret", HTTPClient: server.Client(), Now: func() time.Time { return now },
	})

	search, err := client.SearchOrders(context.Background(), GatewayOrderSearchRequest{
		ShopID: "shop-1", Search: SearchOrdersRequest{PageSize: 20, Filters: OrderSearchFilters{OrderStatus: OrderStatusAwaitingShipment}},
	})
	if err != nil || search.UpstreamRequestID != "tts-list" || search.TotalCount != 1 || len(search.Orders) != 1 {
		t.Fatalf("SearchOrders() = %+v, %v", search, err)
	}
	details, err := client.GetOrderDetails(context.Background(), GatewayOrderDetailsRequest{ShopID: "shop-1", OrderIDs: []string{"order-1"}})
	if err != nil || details.UpstreamRequestID != "tts-detail" || len(details.Orders) != 1 || details.Orders[0].Status != OrderStatusCompleted {
		t.Fatalf("GetOrderDetails() = %+v, %v", details, err)
	}
	recipient, err := client.GetShipmentRecipient(context.Background(), GatewayShipmentRecipientRequest{ShopID: "shop-1", OrderID: "order-1"})
	if err != nil || recipient.UpstreamRequestID != "tts-recipient" || recipient.Recipient == nil || recipient.Recipient.Telephone != "0900000000" {
		t.Fatalf("GetShipmentRecipient() = %+v, %v", recipient, err)
	}
	price, err := client.GetPriceDetail(context.Background(), GatewayOrderPriceDetailRequest{ShopID: "shop-1", OrderID: "order-1"})
	if err != nil || price.UpstreamRequestID != "tts-price" || price.PriceDetail == nil || price.PriceDetail.Payment != "307.49" {
		t.Fatalf("GetPriceDetail() = %+v, %v", price, err)
	}
}

func TestGatewayClientRejectsInvalidOrderReadBeforeNetwork(t *testing.T) {
	client := NewGatewayClient(GatewayClientConfig{BaseURL: "https://gateway.example", Tenant: "aoy", SharedSecret: "tenant-secret"})
	if _, err := client.SearchOrders(context.Background(), GatewayOrderSearchRequest{ShopID: "shop-1", Search: SearchOrdersRequest{PageSize: 0}}); !errors.Is(err, ErrInvalidGatewayInput) {
		t.Fatalf("SearchOrders() error = %v", err)
	}
	if _, err := client.GetOrderDetails(context.Background(), GatewayOrderDetailsRequest{ShopID: "shop-1", OrderIDs: nil}); !errors.Is(err, ErrInvalidGatewayInput) {
		t.Fatalf("GetOrderDetails() error = %v", err)
	}
	if _, err := client.GetShipmentRecipient(context.Background(), GatewayShipmentRecipientRequest{ShopID: "shop-1", OrderID: " "}); !errors.Is(err, ErrInvalidGatewayInput) {
		t.Fatalf("GetShipmentRecipient() error = %v", err)
	}
	if _, err := client.GetPriceDetail(context.Background(), GatewayOrderPriceDetailRequest{ShopID: "shop-1", OrderID: " "}); !errors.Is(err, ErrInvalidGatewayInput) {
		t.Fatalf("GetPriceDetail() error = %v", err)
	}
}
