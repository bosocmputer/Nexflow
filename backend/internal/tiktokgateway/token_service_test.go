package tiktokgateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"nexflow/internal/services/tiktokshop"
)

func TestTokenServiceReturnsUnexpiredAccessTokenWithoutRefresh(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	cipher := newTestTokenCipher(t)
	group := encryptedAuthorizationGroup(t, cipher, now.Add(time.Hour), now.Add(30*24*time.Hour), "old-access", "old-refresh")
	store := &fakeTokenStore{group: group}
	refresher := &fakeTokenRefresher{}
	service := newTestTokenService(t, now, store, cipher, refresher)

	credential, err := service.AccessCredential(context.Background(), "AOY", "shop-2")
	if err != nil {
		t.Fatalf("AccessCredential() error = %v", err)
	}
	if credential.AccessToken != "old-access" || credential.ShopCipher != "cipher-shop-2" || credential.ShopID != "shop-2" {
		t.Fatalf("credential = %+v", credential)
	}
	if refresher.calls != 0 || store.lockCalls != 0 || len(store.rotations) != 0 {
		t.Fatalf("refresh calls=%d lock calls=%d rotations=%d", refresher.calls, store.lockCalls, len(store.rotations))
	}
}

func TestTokenServiceRefreshesEverySiblingShopAtomically(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	cipher := newTestTokenCipher(t)
	group := encryptedAuthorizationGroup(t, cipher, now.Add(5*time.Minute), now.Add(30*24*time.Hour), "old-access", "old-refresh")
	store := &fakeTokenStore{group: group}
	refresher := &fakeTokenRefresher{tokens: &tiktokshop.TokenSet{
		AccessToken: "new-access", AccessTokenExpiresAt: now.Add(24 * time.Hour).Unix(),
		RefreshToken: "new-refresh", RefreshTokenExpiresAt: now.Add(60 * 24 * time.Hour).Unix(),
		OpenID: "seller-open-id", SellerName: "AOY Company", SellerBaseRegion: "TH",
		UserType: tiktokshop.SellerUserType, GrantedScopes: []string{"seller.authorization.info", "seller.order.info"},
	}}
	service := newTestTokenService(t, now, store, cipher, refresher)

	credential, err := service.AccessCredential(context.Background(), "aoy", "shop-2")
	if err != nil {
		t.Fatalf("AccessCredential() error = %v", err)
	}
	if credential.AccessToken != "new-access" || refresher.refreshToken != "old-refresh" || store.lockCalls != 1 || store.unlockCalls != 1 {
		t.Fatalf("credential=%+v refresher=%+v store=%+v", credential, refresher, store)
	}
	if len(store.rotations) != 2 {
		t.Fatalf("rotations = %+v", store.rotations)
	}
	for _, rotation := range store.rotations {
		access, err := cipher.Decrypt(rotation.AccessTokenCipher, rotation.AccessTokenNonce, tokenAAD("aoy", rotation.ShopID, "access"))
		if err != nil || access != "new-access" {
			t.Fatalf("shop %s access = %q, %v", rotation.ShopID, access, err)
		}
		refresh, err := cipher.Decrypt(rotation.RefreshTokenCipher, rotation.RefreshTokenNonce, tokenAAD("aoy", rotation.ShopID, "refresh"))
		if err != nil || refresh != "new-refresh" {
			t.Fatalf("shop %s refresh = %q, %v", rotation.ShopID, refresh, err)
		}
	}
}

func TestTokenServiceReloadsAfterLockAndSkipsRedundantRefresh(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	cipher := newTestTokenCipher(t)
	expiring := encryptedAuthorizationGroup(t, cipher, now.Add(5*time.Minute), now.Add(30*24*time.Hour), "old-access", "old-refresh")
	fresh := encryptedAuthorizationGroup(t, cipher, now.Add(24*time.Hour), now.Add(60*24*time.Hour), "already-refreshed", "already-rotated")
	store := &fakeTokenStore{group: expiring, groupAfterLock: fresh}
	refresher := &fakeTokenRefresher{}
	service := newTestTokenService(t, now, store, cipher, refresher)

	credential, err := service.AccessCredential(context.Background(), "aoy", "shop-1")
	if err != nil || credential.AccessToken != "already-refreshed" {
		t.Fatalf("AccessCredential() = %+v, %v", credential, err)
	}
	if refresher.calls != 0 || len(store.rotations) != 0 || store.lockCalls != 1 || store.unlockCalls != 1 {
		t.Fatalf("refresher=%+v store=%+v", refresher, store)
	}
}

func TestTokenServiceDoesNotOverwriteTokensAfterInvalidRefresh(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	cipher := newTestTokenCipher(t)
	group := encryptedAuthorizationGroup(t, cipher, now.Add(5*time.Minute), now.Add(30*24*time.Hour), "old-access", "old-refresh")
	store := &fakeTokenStore{group: group}
	refresher := &fakeTokenRefresher{tokens: &tiktokshop.TokenSet{
		AccessToken: "new-access", AccessTokenExpiresAt: now.Add(24 * time.Hour).Unix(),
		RefreshToken: "new-refresh", RefreshTokenExpiresAt: now.Add(60 * 24 * time.Hour).Unix(),
		OpenID: "different-seller", UserType: tiktokshop.SellerUserType,
		GrantedScopes: []string{"seller.authorization.info", "seller.order.info"},
	}}
	service := newTestTokenService(t, now, store, cipher, refresher)

	_, err := service.AccessCredential(context.Background(), "aoy", "shop-1")
	if !errors.Is(err, ErrInvalidTokenRefresh) {
		t.Fatalf("AccessCredential() error = %v", err)
	}
	if len(store.rotations) != 0 || store.unlockCalls != 1 {
		t.Fatalf("rotations=%d unlocks=%d", len(store.rotations), store.unlockCalls)
	}
}

func TestTokenServiceFailsClosedWhenRefreshTokenIsNearExpiry(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	cipher := newTestTokenCipher(t)
	group := encryptedAuthorizationGroup(t, cipher, now.Add(5*time.Minute), now.Add(9*time.Minute), "old-access", "old-refresh")
	store := &fakeTokenStore{group: group}
	refresher := &fakeTokenRefresher{}
	service := newTestTokenService(t, now, store, cipher, refresher)

	_, err := service.AccessCredential(context.Background(), "aoy", "shop-1")
	if !errors.Is(err, ErrRefreshTokenExpired) || refresher.calls != 0 || len(store.rotations) != 0 {
		t.Fatalf("error=%v refresher=%+v rotations=%d", err, refresher, len(store.rotations))
	}
}

func newTestTokenCipher(t *testing.T) *TokenCipher {
	t.Helper()
	cipher, err := NewTokenCipher(testEncodedKey(7))
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func newTestTokenService(t *testing.T, now time.Time, store TokenStore, cipher *TokenCipher, refresher TokenRefresher) *TokenService {
	t.Helper()
	service, err := NewTokenService(TokenServiceConfig{
		EncryptionKeyVersion: 1, RefreshSkew: 10 * time.Minute, Now: func() time.Time { return now },
	}, store, cipher, refresher)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func encryptedAuthorizationGroup(t *testing.T, cipher *TokenCipher, accessExpiry, refreshExpiry time.Time, access, refresh string) *AuthorizationTokenGroup {
	t.Helper()
	group := &AuthorizationTokenGroup{
		TenantID: "11111111-1111-1111-1111-111111111111", TenantSlug: "aoy", OpenID: "seller-open-id",
		GrantedScopes:   []string{"seller.authorization.info", "seller.order.info"},
		AccessExpiresAt: accessExpiry, RefreshExpiresAt: refreshExpiry,
	}
	for _, shopID := range []string{"shop-1", "shop-2"} {
		accessCipher, accessNonce, err := cipher.Encrypt(access, tokenAAD("aoy", shopID, "access"))
		if err != nil {
			t.Fatal(err)
		}
		refreshCipher, refreshNonce, err := cipher.Encrypt(refresh, tokenAAD("aoy", shopID, "refresh"))
		if err != nil {
			t.Fatal(err)
		}
		group.Connections = append(group.Connections, AuthorizationTokenConnection{
			ID: "connection-" + shopID, ShopID: shopID, ShopCipher: "cipher-" + shopID,
			AccessTokenCipher: accessCipher, AccessTokenNonce: accessNonce,
			RefreshTokenCipher: refreshCipher, RefreshTokenNonce: refreshNonce,
			EncryptionKeyVersion: 1,
		})
	}
	return group
}

type fakeTokenStore struct {
	mu             sync.Mutex
	group          *AuthorizationTokenGroup
	groupAfterLock *AuthorizationTokenGroup
	locked         bool
	lockCalls      int
	unlockCalls    int
	rotations      []RotatedConnectionTokens
}

func (s *fakeTokenStore) AuthorizationTokenGroupByShop(context.Context, string, string) (*AuthorizationTokenGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked && s.groupAfterLock != nil {
		return cloneAuthorizationTokenGroup(s.groupAfterLock), nil
	}
	return cloneAuthorizationTokenGroup(s.group), nil
}

func (s *fakeTokenStore) LockAuthorizationRefresh(context.Context, string, string) (func(), error) {
	s.mu.Lock()
	s.lockCalls++
	s.locked = true
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.unlockCalls++
		s.locked = false
	}, nil
}

func (s *fakeTokenStore) RotateAuthorizationTokens(_ context.Context, tenantID, openID string, rotations []RotatedConnectionTokens, _ RefreshedAuthorizationMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tenantID != s.group.TenantID || openID != s.group.OpenID {
		return errors.New("wrong authorization")
	}
	s.rotations = append([]RotatedConnectionTokens(nil), rotations...)
	return nil
}

func cloneAuthorizationTokenGroup(input *AuthorizationTokenGroup) *AuthorizationTokenGroup {
	if input == nil {
		return nil
	}
	output := *input
	output.GrantedScopes = append([]string(nil), input.GrantedScopes...)
	output.Connections = append([]AuthorizationTokenConnection(nil), input.Connections...)
	return &output
}

type fakeTokenRefresher struct {
	tokens       *tiktokshop.TokenSet
	err          error
	refreshToken string
	calls        int
}

func (f *fakeTokenRefresher) Refresh(_ context.Context, refreshToken string) (*tiktokshop.TokenSet, error) {
	f.calls++
	f.refreshToken = refreshToken
	return f.tokens, f.err
}
