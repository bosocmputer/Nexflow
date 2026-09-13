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

func TestGatewayClientReadsTikTokProductsAndInventoryThroughTenantGateway(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case GatewayProductSearchPath:
			_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"req-products","total_count":1,"products":[{"id":"1729429119195974110","title":"AOY Product","status":"ACTIVATE","skus":[{"id":"1729429119195974111","seller_sku":"AOY-001","inventory":[]}] }]}}`))
		case GatewayInventorySearchPath:
			_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"req-inventory","inventory":[{"product_id":"1729429119195974110","skus":[{"id":"1729429119195974111","seller_sku":"AOY-001","total_available_quantity":7,"total_committed_quantity":1,"warehouse_inventory":[]}] }]}}`))
		default:
			t.Fatalf("path=%s body=%s", r.URL.Path, body)
		}
	}))
	defer server.Close()
	client := NewGatewayClient(GatewayClientConfig{BaseURL: server.URL, Tenant: "aoy", SharedSecret: "tenant-secret", HTTPClient: server.Client()})
	products, err := client.SearchProducts(context.Background(), GatewayProductSearchRequest{
		ShopID: "7494619203789490654", Search: SearchProductsRequest{PageSize: 100},
	})
	if err != nil || products.TotalCount != 1 || products.Products[0].SKUs[0].SellerSKU != "AOY-001" {
		t.Fatalf("SearchProducts=%+v err=%v", products, err)
	}
	inventory, err := client.SearchInventory(context.Background(), GatewayInventorySearchRequest{
		ShopID: "7494619203789490654", Search: InventorySearchRequest{SKUIDs: []string{"1729429119195974111"}},
	})
	if err != nil || inventory.Inventory[0].SKUs[0].TotalAvailableQuantity != 7 {
		t.Fatalf("SearchInventory=%+v err=%v", inventory, err)
	}
}

func TestGatewayClientPreservesPerSKURejectionFromInventoryUpdate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"data":{"upstream_request_id":"req-write","errors":[{"code":12052990,"message":"Check failed","detail":{"sku_id":"1729429119195974111","extra_errors":[]}}]},"error":{"code":"inventory_item_rejected","message":"rejected","request_id":"gateway-request"}}`))
	}))
	defer server.Close()
	client := NewGatewayClient(GatewayClientConfig{BaseURL: server.URL, Tenant: "aoy", SharedSecret: "tenant-secret", HTTPClient: server.Client()})
	result, err := client.UpdateInventory(context.Background(), GatewayInventoryUpdateRequest{
		ShopID: "7494619203789490654", ProductID: "1729429119195974110",
		Update: UpdateInventoryRequest{SKUs: []InventorySKUUpdate{{
			ID: "1729429119195974111",
			Inventory: []WarehouseInventoryUpdate{{
				WarehouseID: "7068517275539719942", Quantity: 7,
			}},
		}}},
	})
	var gatewayError *GatewayError
	if !errors.As(err, &gatewayError) || gatewayError.Code != "inventory_item_rejected" || result == nil || len(result.Errors) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
