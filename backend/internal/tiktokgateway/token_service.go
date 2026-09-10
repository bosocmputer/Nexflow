package tiktokgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nexflow/internal/services/tiktokshop"
)

var (
	ErrTokenServiceNotConfigured = errors.New("TikTok Shop token service is not configured")
	ErrInvalidTokenCredential    = errors.New("invalid TikTok Shop token credential")
	ErrInvalidTokenRefresh       = errors.New("invalid TikTok Shop token refresh")
	ErrRefreshTokenExpired       = errors.New("TikTok Shop refresh token is expired or too close to expiry")
)

type TokenRefresher interface {
	Refresh(context.Context, string) (*tiktokshop.TokenSet, error)
}

type TokenStore interface {
	AuthorizationTokenGroupByShop(context.Context, string, string) (*AuthorizationTokenGroup, error)
	LockAuthorizationRefresh(context.Context, string, string) (func(), error)
	RotateAuthorizationTokens(context.Context, string, string, []RotatedConnectionTokens, RefreshedAuthorizationMetadata) error
}

type TokenServiceConfig struct {
	EncryptionKeyVersion int
	RefreshSkew          time.Duration
	Now                  func() time.Time
}

type TokenService struct {
	config    TokenServiceConfig
	store     TokenStore
	cipher    *TokenCipher
	refresher TokenRefresher
}

type AuthorizationTokenGroup struct {
	TenantID         string
	TenantSlug       string
	OpenID           string
	GrantedScopes    []string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	Connections      []AuthorizationTokenConnection
}

type AuthorizationTokenConnection struct {
	ID                   string
	ShopID               string
	ShopCipher           string
	AccessTokenCipher    []byte
	AccessTokenNonce     []byte
	RefreshTokenCipher   []byte
	RefreshTokenNonce    []byte
	EncryptionKeyVersion int
}

type RotatedConnectionTokens struct {
	ConnectionID         string
	ShopID               string
	AccessTokenCipher    []byte
	AccessTokenNonce     []byte
	RefreshTokenCipher   []byte
	RefreshTokenNonce    []byte
	EncryptionKeyVersion int
}

type RefreshedAuthorizationMetadata struct {
	SellerName       string
	SellerBaseRegion string
	GrantedScopes    []string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	RefreshedAt      time.Time
}

type AccessCredential struct {
	AccessToken string
	ShopID      string
	ShopCipher  string
}

func NewTokenService(config TokenServiceConfig, store TokenStore, cipher *TokenCipher, refresher TokenRefresher) (*TokenService, error) {
	if config.EncryptionKeyVersion <= 0 || store == nil || cipher == nil || refresher == nil {
		return nil, ErrTokenServiceNotConfigured
	}
	if config.RefreshSkew <= 0 {
		config.RefreshSkew = 10 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &TokenService{config: config, store: store, cipher: cipher, refresher: refresher}, nil
}

func (s *TokenService) AccessCredential(ctx context.Context, tenantSlug, shopID string) (*AccessCredential, error) {
	if s == nil {
		return nil, ErrTokenServiceNotConfigured
	}
	tenantSlug = strings.ToLower(strings.TrimSpace(tenantSlug))
	shopID = strings.TrimSpace(shopID)
	if tenantSlug == "" || shopID == "" {
		return nil, ErrInvalidTokenCredential
	}

	group, connection, err := s.loadGroup(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	now := s.config.Now()
	if group.AccessExpiresAt.After(now.Add(s.config.RefreshSkew)) {
		return s.decryptAccess(group, connection)
	}

	unlock, err := s.store.LockAuthorizationRefresh(ctx, group.TenantID, group.OpenID)
	if err != nil {
		return nil, fmt.Errorf("lock TikTok Shop token refresh: %w", err)
	}
	defer unlock()

	lockedGroup, lockedConnection, err := s.loadGroup(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	if lockedGroup.TenantID != group.TenantID || lockedGroup.OpenID != group.OpenID {
		return nil, ErrInvalidTokenCredential
	}
	now = s.config.Now()
	if lockedGroup.AccessExpiresAt.After(now.Add(s.config.RefreshSkew)) {
		return s.decryptAccess(lockedGroup, lockedConnection)
	}
	if !lockedGroup.RefreshExpiresAt.After(now.Add(s.config.RefreshSkew)) {
		return nil, ErrRefreshTokenExpired
	}

	refreshToken, err := s.cipher.Decrypt(
		lockedConnection.RefreshTokenCipher,
		lockedConnection.RefreshTokenNonce,
		tokenAAD(lockedGroup.TenantSlug, lockedConnection.ShopID, "refresh"),
	)
	if err != nil {
		return nil, ErrInvalidTokenCredential
	}
	tokens, err := s.refresher.Refresh(ctx, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh TikTok Shop seller token: %w", err)
	}
	if err := validateRefreshedTokens(tokens, lockedGroup.OpenID, now); err != nil {
		return nil, err
	}

	rotations := make([]RotatedConnectionTokens, 0, len(lockedGroup.Connections))
	for _, sibling := range lockedGroup.Connections {
		accessCipher, accessNonce, err := s.cipher.Encrypt(tokens.AccessToken, tokenAAD(lockedGroup.TenantSlug, sibling.ShopID, "access"))
		if err != nil {
			return nil, fmt.Errorf("encrypt refreshed TikTok Shop access token: %w", err)
		}
		refreshCipher, refreshNonce, err := s.cipher.Encrypt(tokens.RefreshToken, tokenAAD(lockedGroup.TenantSlug, sibling.ShopID, "refresh"))
		if err != nil {
			return nil, fmt.Errorf("encrypt refreshed TikTok Shop refresh token: %w", err)
		}
		rotations = append(rotations, RotatedConnectionTokens{
			ConnectionID: sibling.ID, ShopID: sibling.ShopID,
			AccessTokenCipher: accessCipher, AccessTokenNonce: accessNonce,
			RefreshTokenCipher: refreshCipher, RefreshTokenNonce: refreshNonce,
			EncryptionKeyVersion: s.config.EncryptionKeyVersion,
		})
	}
	metadata := RefreshedAuthorizationMetadata{
		SellerName: strings.TrimSpace(tokens.SellerName), SellerBaseRegion: strings.TrimSpace(tokens.SellerBaseRegion),
		GrantedScopes:   append([]string(nil), tokens.GrantedScopes...),
		AccessExpiresAt: time.Unix(tokens.AccessTokenExpiresAt, 0), RefreshExpiresAt: time.Unix(tokens.RefreshTokenExpiresAt, 0),
		RefreshedAt: now,
	}
	if err := s.store.RotateAuthorizationTokens(ctx, lockedGroup.TenantID, lockedGroup.OpenID, rotations, metadata); err != nil {
		return nil, fmt.Errorf("store refreshed TikTok Shop seller token: %w", err)
	}
	return &AccessCredential{AccessToken: tokens.AccessToken, ShopID: lockedConnection.ShopID, ShopCipher: lockedConnection.ShopCipher}, nil
}

func (s *TokenService) loadGroup(ctx context.Context, tenantSlug, shopID string) (*AuthorizationTokenGroup, AuthorizationTokenConnection, error) {
	group, err := s.store.AuthorizationTokenGroupByShop(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, AuthorizationTokenConnection{}, fmt.Errorf("load TikTok Shop token credential: %w", err)
	}
	if err := validateAuthorizationTokenGroup(group, s.config.EncryptionKeyVersion); err != nil {
		return nil, AuthorizationTokenConnection{}, err
	}
	for _, connection := range group.Connections {
		if connection.ShopID == shopID {
			return group, connection, nil
		}
	}
	return nil, AuthorizationTokenConnection{}, ErrInvalidTokenCredential
}

func (s *TokenService) decryptAccess(group *AuthorizationTokenGroup, connection AuthorizationTokenConnection) (*AccessCredential, error) {
	accessToken, err := s.cipher.Decrypt(connection.AccessTokenCipher, connection.AccessTokenNonce, tokenAAD(group.TenantSlug, connection.ShopID, "access"))
	if err != nil {
		return nil, ErrInvalidTokenCredential
	}
	return &AccessCredential{AccessToken: accessToken, ShopID: connection.ShopID, ShopCipher: connection.ShopCipher}, nil
}

func validateAuthorizationTokenGroup(group *AuthorizationTokenGroup, keyVersion int) error {
	if group == nil || strings.TrimSpace(group.TenantID) == "" || strings.TrimSpace(group.TenantSlug) == "" || strings.TrimSpace(group.OpenID) == "" ||
		!containsEveryScope(group.GrantedScopes, requiredOAuthScopes) || group.AccessExpiresAt.IsZero() || group.RefreshExpiresAt.IsZero() ||
		len(group.Connections) == 0 || len(group.Connections) > 1000 {
		return ErrInvalidTokenCredential
	}
	seenIDs := make(map[string]struct{}, len(group.Connections))
	seenShops := make(map[string]struct{}, len(group.Connections))
	for _, connection := range group.Connections {
		if strings.TrimSpace(connection.ID) == "" || strings.TrimSpace(connection.ShopID) == "" || strings.TrimSpace(connection.ShopCipher) == "" ||
			len(connection.AccessTokenCipher) == 0 || len(connection.AccessTokenNonce) == 0 ||
			len(connection.RefreshTokenCipher) == 0 || len(connection.RefreshTokenNonce) == 0 || connection.EncryptionKeyVersion != keyVersion {
			return ErrInvalidTokenCredential
		}
		if _, exists := seenIDs[connection.ID]; exists {
			return ErrInvalidTokenCredential
		}
		if _, exists := seenShops[connection.ShopID]; exists {
			return ErrInvalidTokenCredential
		}
		seenIDs[connection.ID] = struct{}{}
		seenShops[connection.ShopID] = struct{}{}
	}
	return nil
}

func validateRefreshedTokens(tokens *tiktokshop.TokenSet, expectedOpenID string, now time.Time) error {
	if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" || strings.TrimSpace(tokens.RefreshToken) == "" ||
		strings.TrimSpace(tokens.OpenID) != expectedOpenID || tokens.UserType != tiktokshop.SellerUserType ||
		!containsEveryScope(tokens.GrantedScopes, requiredOAuthScopes) {
		return ErrInvalidTokenRefresh
	}
	accessExpiry := time.Unix(tokens.AccessTokenExpiresAt, 0)
	refreshExpiry := time.Unix(tokens.RefreshTokenExpiresAt, 0)
	if !accessExpiry.After(now) || !refreshExpiry.After(accessExpiry) {
		return ErrInvalidTokenRefresh
	}
	return nil
}
