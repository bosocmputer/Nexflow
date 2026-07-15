package shopeegateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/services/shopeeapi"
)

type fakeGatewayStore struct {
	tenant      Tenant
	oauth       map[string]OAuthStateRecord
	connection  EncryptedConnection
	deliveries  []shopeeapi.GatewayConnectionPayload
	updateCount int
}

func (f *fakeGatewayStore) TenantBySlug(_ context.Context, slug string) (*Tenant, error) {
	if slug != f.tenant.Slug {
		return nil, sql.ErrNoRows
	}
	out := f.tenant
	return &out, nil
}

func (f *fakeGatewayStore) TenantByID(_ context.Context, id string) (*Tenant, error) {
	if id != f.tenant.ID {
		return nil, sql.ErrNoRows
	}
	out := f.tenant
	return &out, nil
}

func (f *fakeGatewayStore) CreateOAuthState(_ context.Context, record OAuthStateRecord) error {
	if f.oauth == nil {
		f.oauth = map[string]OAuthStateRecord{}
	}
	f.oauth[record.StateHash] = record
	return nil
}

func (f *fakeGatewayStore) ConsumeOAuthState(_ context.Context, stateHash string) (*OAuthStateRecord, error) {
	record, ok := f.oauth[stateHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	delete(f.oauth, stateHash)
	return &record, nil
}

func (f *fakeGatewayStore) Connection(_ context.Context, tenantSlug string, shopID int64) (*EncryptedConnection, error) {
	if tenantSlug != f.tenant.Slug || shopID != f.connection.ShopID {
		return nil, sql.ErrNoRows
	}
	out := f.connection
	return &out, nil
}

func (f *fakeGatewayStore) UpsertConnection(_ context.Context, conn EncryptedConnection) error {
	conn.ID = "gateway-connection-1"
	f.connection = conn
	return nil
}

func (f *fakeGatewayStore) UpdateConnectionTokens(_ context.Context, conn EncryptedConnection) error {
	f.connection = conn
	f.updateCount++
	return nil
}

func (f *fakeGatewayStore) EnqueueDelivery(_ context.Context, _ string, eventType, _ string, payload interface{}) error {
	if eventType != "connection_upsert" {
		return errors.New("unexpected event type")
	}
	value, ok := payload.(shopeeapi.GatewayConnectionPayload)
	if !ok {
		return errors.New("unexpected payload type")
	}
	f.deliveries = append(f.deliveries, value)
	return nil
}

func testGatewayConfig(shopeeBaseURL string) Config {
	return Config{
		Environment:        "live",
		PublicBaseURL:      "https://shopee-gateway.nextstep-soft.com",
		ShopeeBaseURL:      shopeeBaseURL,
		ShopeePartnerID:    2034838,
		ShopeePartnerKey:   "partner-secret",
		TokenEncryptionKey: testEncodedKey(1),
		InternalMasterKey:  testEncodedKey(2),
		OAuthSigningKey:    testEncodedKey(3),
	}
}

func TestServiceOAuthStoresEncryptedTokensAndQueuesMetadataOnly(t *testing.T) {
	shopee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case shopeeapi.PathTokenGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "access-token-secret",
				"refresh_token": "refresh-token-secret",
				"expire_in":     14400,
				"shop_id":       264993963,
				"merchant_id":   888,
			})
		case shopeeapi.PathShopInfo:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]string{"shop_name": "AOY Shop", "region": "TH", "status": "NORMAL"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer shopee.Close()

	store := &fakeGatewayStore{tenant: Tenant{
		ID: "11111111-1111-1111-1111-111111111111", Slug: "aoy", Enabled: true,
		PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com", BackendURL: "http://172.17.0.1:8111",
	}}
	cfg := testGatewayConfig(shopee.URL)
	provider := shopeeapi.New(shopeeapi.Config{BaseURL: shopee.URL, PartnerID: cfg.ShopeePartnerID, PartnerKey: cfg.ShopeePartnerKey})
	service, err := NewService(cfg, store, provider, zap.NewNop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Unix(1784070000, 0)
	service.now = func() time.Time { return now }
	auth, err := service.CreateAuthURL(t.Context(), "aoy", "user-1", "https://nexflow-aoy.nextstep-soft.com/settings/shopee-connections")
	if err != nil {
		t.Fatalf("CreateAuthURL() error = %v", err)
	}
	state := queryValue(t, auth.AuthURL, "state")
	result, err := service.CompleteOAuth(t.Context(), "one-time-code", state, 264993963)
	if err != nil {
		t.Fatalf("CompleteOAuth() error = %v", err)
	}
	if result.TenantSlug != "aoy" || !strings.Contains(result.ReturnURL, "shopee=connected") {
		t.Fatalf("callback result = %+v", result)
	}
	if string(store.connection.AccessTokenCipher) == "access-token-secret" || string(store.connection.RefreshTokenCipher) == "refresh-token-secret" {
		t.Fatal("tokens were stored without encryption")
	}
	access, err := service.cipher.Decrypt(store.connection.AccessTokenCipher, store.connection.AccessTokenNonce, tokenAAD("aoy", 264993963, "access"))
	if err != nil || access != "access-token-secret" {
		t.Fatalf("decrypted access token = %q, error = %v", access, err)
	}
	if len(store.deliveries) != 1 || store.deliveries[0].GatewayConnectionID != "gateway-connection-1" {
		t.Fatalf("deliveries = %+v", store.deliveries)
	}
	payloadJSON, _ := json.Marshal(store.deliveries[0])
	if strings.Contains(string(payloadJSON), "access-token") || strings.Contains(string(payloadJSON), "refresh-token") {
		t.Fatalf("connection metadata leaked a token: %s", payloadJSON)
	}
}

func TestServiceExecuteUsesGatewayOwnedAccessToken(t *testing.T) {
	shopee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != shopeeapi.PathOrderList {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("access_token") != "gateway-access-token" {
			t.Fatalf("access token query = %q", r.URL.Query().Get("access_token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{
				"more":       false,
				"order_list": []map[string]string{{"order_sn": "ORDER-1", "order_status": "READY_TO_SHIP"}},
			},
		})
	}))
	defer shopee.Close()

	cfg := testGatewayConfig(shopee.URL)
	store := &fakeGatewayStore{tenant: Tenant{ID: "tenant-1", Slug: "demo", Enabled: true}}
	provider := shopeeapi.New(shopeeapi.Config{BaseURL: shopee.URL, PartnerID: cfg.ShopeePartnerID, PartnerKey: cfg.ShopeePartnerKey})
	service, err := NewService(cfg, store, provider, zap.NewNop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return time.Unix(1784070000, 0) }
	accessCipher, accessNonce, _ := service.cipher.Encrypt("gateway-access-token", tokenAAD("demo", 123, "access"))
	refreshCipher, refreshNonce, _ := service.cipher.Encrypt("gateway-refresh-token", tokenAAD("demo", 123, "refresh"))
	store.connection = EncryptedConnection{
		ID: "conn-1", TenantSlug: "demo", ShopID: 123, Environment: "live",
		AccessTokenCipher: accessCipher, AccessTokenNonce: accessNonce,
		RefreshTokenCipher: refreshCipher, RefreshTokenNonce: refreshNonce,
		AccessExpiresAt: service.now().Add(time.Hour), RefreshExpiresAt: service.now().Add(24 * time.Hour),
	}
	payload, _ := json.Marshal(shopeeapi.OrderListRequest{TimeFrom: 1, TimeTo: 2, PageSize: 50})
	raw, err := service.Execute(t.Context(), "demo", GatewayExecuteRequest{Operation: "get_order_list", ShopID: 123, Payload: payload})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var response shopeeapi.OrderListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Response.OrderList) != 1 || response.Response.OrderList[0].OrderSN != "ORDER-1" {
		t.Fatalf("response = %+v", response.Response)
	}
}

func TestServiceRejectsMissingOAuthStateWithoutFallback(t *testing.T) {
	store := &fakeGatewayStore{tenant: Tenant{ID: "tenant-1", Slug: "aoy", Enabled: true}}
	cfg := testGatewayConfig("https://partner.shopeemobile.com")
	provider := shopeeapi.New(shopeeapi.Config{BaseURL: cfg.ShopeeBaseURL, PartnerID: cfg.ShopeePartnerID, PartnerKey: cfg.ShopeePartnerKey})
	service, err := NewService(cfg, store, provider, zap.NewNop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.CompleteOAuth(t.Context(), "code", "", 123)
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code != "invalid_oauth_callback" {
		t.Fatalf("CompleteOAuth() error = %#v", err)
	}
}

func TestServiceRetriesTransientReadOnlyOperation(t *testing.T) {
	var attempts atomic.Int32
	shopee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != shopeeapi.PathShopInfo {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) < 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]string{"shop_name": "Retry Shop", "region": "TH", "status": "NORMAL"},
		})
	}))
	defer shopee.Close()

	service := newGatewayTestService(t, shopee.URL)
	result, err := service.executeOperationWithRetry(t.Context(), "get_shop_info", 123, "access-token", nil)
	if err != nil {
		t.Fatalf("executeOperationWithRetry() error = %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
	info, ok := result.(*shopeeapi.ShopInfoResponse)
	if !ok || info.Response.ShopName != "Retry Shop" {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceDoesNotRetryShipOrder(t *testing.T) {
	var attempts atomic.Int32
	shopee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != shopeeapi.PathLogisticsShipOrder {
			http.NotFound(w, r)
			return
		}
		attempts.Add(1)
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer shopee.Close()

	service := newGatewayTestService(t, shopee.URL)
	payload, _ := json.Marshal(shopeeapi.ShipOrderRequest{OrderSN: "ORDER-1"})
	_, err := service.executeOperationWithRetry(t.Context(), "ship_order", 123, "access-token", payload)
	if err == nil {
		t.Fatal("executeOperationWithRetry() error = nil, want transient error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}

func TestServiceRateLimitsEachTenantIndependently(t *testing.T) {
	service := newGatewayTestService(t, "https://partner.shopeemobile.com")
	service.now = func() time.Time { return time.Unix(1784070000, 0) }
	for i := 0; i < tenantRequestsPerSecond; i++ {
		if !service.allowTenantRequest("aoy") {
			t.Fatalf("AOY request %d was limited early", i+1)
		}
	}
	if service.allowTenantRequest("aoy") {
		t.Fatal("AOY request above per-second cap was allowed")
	}
	if !service.allowTenantRequest("demo") {
		t.Fatal("AOY traffic incorrectly limited demo")
	}
}

func newGatewayTestService(t *testing.T, shopeeBaseURL string) *Service {
	t.Helper()
	cfg := testGatewayConfig(shopeeBaseURL)
	store := &fakeGatewayStore{tenant: Tenant{ID: "tenant-1", Slug: "aoy", Enabled: true}}
	provider := shopeeapi.New(shopeeapi.Config{BaseURL: shopeeBaseURL, PartnerID: cfg.ShopeePartnerID, PartnerKey: cfg.ShopeePartnerKey})
	service, err := NewService(cfg, store, provider, zap.NewNop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func queryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	value := request.URL.Query().Get(key)
	if value == "" {
		t.Fatalf("missing query %s in %s", key, rawURL)
	}
	return value
}
