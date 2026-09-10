package tiktokgateway

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"nexflow/internal/services/tiktokshop"
)

func TestOAuthServiceConnectsEveryAuthorizedShopToOneTenant(t *testing.T) {
	now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	store := &fakeOAuthStore{tenant: &Tenant{
		ID: "11111111-1111-1111-1111-111111111111", Slug: "aoy",
		PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com", Enabled: true,
	}}
	tokens := &tiktokshop.TokenSet{
		AccessToken: "access-secret", AccessTokenExpiresAt: now.Add(24 * time.Hour).Unix(),
		RefreshToken: "refresh-secret", RefreshTokenExpiresAt: now.Add(30 * 24 * time.Hour).Unix(),
		OpenID: "seller-open-id", SellerName: "AOY Company", SellerBaseRegion: "TH",
		UserType: tiktokshop.SellerUserType, GrantedScopes: []string{"seller.order.info", "seller.authorization.info"},
	}
	tokenClient := &fakeTokenExchanger{tokens: tokens}
	shopClient := &fakeAuthorizedShopLister{shops: []tiktokshop.AuthorizedShop{
		{ID: "7000714532876273420", Name: "AOY Main", Region: "TH", SellerType: "LOCAL", Cipher: "TTP_abc", Code: "THAOY1"},
		{ID: "7000714532876273421", Name: "AOY Outlet", Region: "TH", SellerType: "LOCAL", Cipher: "TTP_def", Code: "THAOY2"},
	}}
	signer, err := NewOAuthStateSigner(testEncodedKey(4))
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := NewTokenCipher(testEncodedKey(7))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewOAuthService(OAuthServiceConfig{
		ServiceID: "7683174272727025429", EncryptionKeyVersion: 1, Now: func() time.Time { return now },
	}, store, signer, cipher, tokenClient, shopClient)
	if err != nil {
		t.Fatalf("NewOAuthService() error = %v", err)
	}

	start, err := service.BeginAuthorization(context.Background(), "AOY", "admin-user", "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop?source=settings")
	if err != nil {
		t.Fatalf("BeginAuthorization() error = %v", err)
	}
	parsed, _ := url.Parse(start.AuthorizationURL)
	if parsed.Query().Get("service_id") != "7683174272727025429" || parsed.Query().Get("state") != start.State {
		t.Fatalf("authorization URL = %q", start.AuthorizationURL)
	}
	if store.createdState == nil || store.createdState.StateHash != HashOAuthState(start.State) || store.createdState.TenantID != store.tenant.ID {
		t.Fatalf("stored state = %+v", store.createdState)
	}

	result, err := service.CompleteAuthorization(context.Background(), "one-time-code", start.State, "")
	if err != nil {
		t.Fatalf("CompleteAuthorization() error = %v", err)
	}
	if result.TenantSlug != "aoy" || result.ReturnURL != "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop?source=settings" || len(result.Shops) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if tokenClient.authCode != "one-time-code" || shopClient.accessToken != "access-secret" || len(store.connections) != 2 {
		t.Fatalf("token auth code = %q, shop token = %q, connections = %+v", tokenClient.authCode, shopClient.accessToken, store.connections)
	}
	first := store.connections[0]
	if first.TenantID != store.tenant.ID || first.ShopName != "AOY Main" || first.ShopCode != "THAOY1" || first.ShopRegion != "TH" || first.SellerType != "LOCAL" {
		t.Fatalf("first connection = %+v", first)
	}
	access, err := cipher.Decrypt(first.AccessTokenCipher, first.AccessTokenNonce, tokenAAD("aoy", first.ShopID, "access"))
	if err != nil || access != "access-secret" {
		t.Fatalf("decrypted access token = %q, %v", access, err)
	}
	refresh, err := cipher.Decrypt(first.RefreshTokenCipher, first.RefreshTokenNonce, tokenAAD("aoy", first.ShopID, "refresh"))
	if err != nil || refresh != "refresh-secret" {
		t.Fatalf("decrypted refresh token = %q, %v", refresh, err)
	}

	if _, err := service.CompleteAuthorization(context.Background(), "one-time-code", start.State, ""); !errors.Is(err, ErrInvalidOAuthCallback) {
		t.Fatalf("replayed CompleteAuthorization() error = %v", err)
	}
}

func TestOAuthServiceFailsClosedWhenOrderScopeWasNotGranted(t *testing.T) {
	now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	store := &fakeOAuthStore{tenant: &Tenant{ID: "11111111-1111-1111-1111-111111111111", Slug: "aoy", PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com", Enabled: true}}
	tokenClient := &fakeTokenExchanger{tokens: &tiktokshop.TokenSet{
		AccessToken: "access-secret", AccessTokenExpiresAt: now.Add(time.Hour).Unix(), RefreshToken: "refresh-secret",
		RefreshTokenExpiresAt: now.Add(24 * time.Hour).Unix(), OpenID: "seller-open-id", UserType: tiktokshop.SellerUserType,
		GrantedScopes: []string{"seller.authorization.info"},
	}}
	shopClient := &fakeAuthorizedShopLister{shops: []tiktokshop.AuthorizedShop{{ID: "shop-1", Cipher: "cipher-1"}}}
	service := newTestOAuthService(t, now, store, tokenClient, shopClient)
	start, err := service.BeginAuthorization(context.Background(), "aoy", "admin-user", "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CompleteAuthorization(context.Background(), "one-time-code", start.State, "")
	if !errors.Is(err, ErrRequiredScopeMissing) {
		t.Fatalf("CompleteAuthorization() error = %v", err)
	}
	if shopClient.calls != 0 || len(store.connections) != 0 {
		t.Fatalf("shop calls = %d, connections = %d", shopClient.calls, len(store.connections))
	}
}

func TestOAuthServiceConsumesDeniedAuthorizationWithoutCallingTikTok(t *testing.T) {
	now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	store := &fakeOAuthStore{tenant: &Tenant{ID: "11111111-1111-1111-1111-111111111111", Slug: "aoy", PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com", Enabled: true}}
	tokenClient := &fakeTokenExchanger{}
	shopClient := &fakeAuthorizedShopLister{}
	service := newTestOAuthService(t, now, store, tokenClient, shopClient)
	start, err := service.BeginAuthorization(context.Background(), "aoy", "admin-user", "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CompleteAuthorization(context.Background(), "", start.State, "access_denied")
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("CompleteAuthorization() error = %v", err)
	}
	if tokenClient.calls != 0 || shopClient.calls != 0 || !store.consumed {
		t.Fatalf("token calls = %d, shop calls = %d, consumed = %v", tokenClient.calls, shopClient.calls, store.consumed)
	}
}

func newTestOAuthService(t *testing.T, now time.Time, store OAuthStore, tokens TokenExchanger, shops AuthorizedShopLister) *OAuthService {
	t.Helper()
	signer, err := NewOAuthStateSigner(testEncodedKey(4))
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := NewTokenCipher(testEncodedKey(7))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewOAuthService(OAuthServiceConfig{ServiceID: "7683174272727025429", EncryptionKeyVersion: 1, Now: func() time.Time { return now }}, store, signer, cipher, tokens, shops)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type fakeOAuthStore struct {
	tenant       *Tenant
	createdState *OAuthStateRecord
	connections  []EncryptedConnection
	consumed     bool
}

func (s *fakeOAuthStore) TenantBySlug(context.Context, string) (*Tenant, error) {
	if s.tenant == nil {
		return nil, sql.ErrNoRows
	}
	copy := *s.tenant
	return &copy, nil
}

func (s *fakeOAuthStore) CreateOAuthState(_ context.Context, state OAuthStateRecord) error {
	copy := state
	s.createdState = &copy
	return nil
}

func (s *fakeOAuthStore) ConsumeOAuthState(_ context.Context, hash string) (*OAuthStateRecord, error) {
	if s.createdState == nil || s.consumed || s.createdState.StateHash != hash {
		return nil, sql.ErrNoRows
	}
	s.consumed = true
	copy := *s.createdState
	return &copy, nil
}

func (s *fakeOAuthStore) UpsertConnections(_ context.Context, connections []EncryptedConnection) error {
	s.connections = append([]EncryptedConnection(nil), connections...)
	return nil
}

type fakeTokenExchanger struct {
	tokens   *tiktokshop.TokenSet
	err      error
	authCode string
	calls    int
}

func (f *fakeTokenExchanger) ExchangeAuthCode(_ context.Context, authCode string) (*tiktokshop.TokenSet, error) {
	f.calls++
	f.authCode = authCode
	return f.tokens, f.err
}

type fakeAuthorizedShopLister struct {
	shops       []tiktokshop.AuthorizedShop
	err         error
	accessToken string
	calls       int
}

func (f *fakeAuthorizedShopLister) GetAuthorizedShops(_ context.Context, accessToken string) ([]tiktokshop.AuthorizedShop, string, error) {
	f.calls++
	f.accessToken = accessToken
	return append([]tiktokshop.AuthorizedShop(nil), f.shops...), "request-shops", f.err
}
