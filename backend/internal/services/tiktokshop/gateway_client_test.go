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
