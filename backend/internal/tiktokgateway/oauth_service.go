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
	ErrOAuthServiceNotConfigured = errors.New("TikTok Shop OAuth service is not configured")
	ErrTenantNotAvailable        = errors.New("Nexflow tenant is not available for TikTok Shop authorization")
	ErrInvalidOAuthCallback      = errors.New("invalid or expired TikTok Shop OAuth callback")
	ErrAuthorizationDenied       = errors.New("TikTok Shop seller authorization was denied")
	ErrRequiredScopeMissing      = errors.New("required TikTok Shop API scope was not granted")
	ErrUnexpectedSellerUser      = errors.New("TikTok Shop authorization did not return a seller account")
)

var requiredOAuthScopes = []string{"seller.authorization.info", "seller.order.info"}

type OAuthStore interface {
	TenantBySlug(context.Context, string) (*Tenant, error)
	CreateOAuthState(context.Context, OAuthStateRecord) error
	ConsumeOAuthState(context.Context, string) (*OAuthStateRecord, error)
	UpsertConnections(context.Context, []EncryptedConnection) error
}

type TokenExchanger interface {
	ExchangeAuthCode(context.Context, string) (*tiktokshop.TokenSet, error)
}

type AuthorizedShopLister interface {
	GetAuthorizedShops(context.Context, string) ([]tiktokshop.AuthorizedShop, string, error)
}

type OAuthServiceConfig struct {
	ServiceID            string
	EncryptionKeyVersion int
	Now                  func() time.Time
}

type OAuthService struct {
	config      OAuthServiceConfig
	store       OAuthStore
	stateSigner *OAuthStateSigner
	tokenCipher *TokenCipher
	tokens      TokenExchanger
	shops       AuthorizedShopLister
}

type AuthorizationStart struct {
	AuthorizationURL string
	State            string
	ExpiresAt        time.Time
}

type ConnectedShop struct {
	ID         string
	Name       string
	Region     string
	SellerType string
	Code       string
}

type AuthorizationResult struct {
	TenantSlug string
	ReturnURL  string
	Shops      []ConnectedShop
}

func NewOAuthService(config OAuthServiceConfig, store OAuthStore, signer *OAuthStateSigner, cipher *TokenCipher, tokens TokenExchanger, shops AuthorizedShopLister) (*OAuthService, error) {
	if !serviceIDPattern.MatchString(strings.TrimSpace(config.ServiceID)) || config.EncryptionKeyVersion <= 0 || store == nil || signer == nil || cipher == nil || tokens == nil || shops == nil {
		return nil, ErrOAuthServiceNotConfigured
	}
	config.ServiceID = strings.TrimSpace(config.ServiceID)
	if config.Now == nil {
		config.Now = time.Now
	}
	return &OAuthService{config: config, store: store, stateSigner: signer, tokenCipher: cipher, tokens: tokens, shops: shops}, nil
}

func (s *OAuthService) BeginAuthorization(ctx context.Context, tenantSlug, userID, returnURL string) (*AuthorizationStart, error) {
	if s == nil {
		return nil, ErrOAuthServiceNotConfigured
	}
	tenant, err := s.store.TenantBySlug(ctx, strings.ToLower(strings.TrimSpace(tenantSlug)))
	if err != nil || tenant == nil || !tenant.Enabled {
		return nil, ErrTenantNotAvailable
	}
	if err := ValidateTenantReturnURL(tenant.PublicBaseURL, returnURL); err != nil {
		return nil, err
	}
	now := s.config.Now()
	state, claims, err := s.stateSigner.Create(tenant.Slug, userID, returnURL, now)
	if err != nil {
		return nil, err
	}
	record := OAuthStateRecord{
		StateHash: HashOAuthState(state), TenantID: tenant.ID, UserID: claims.UserID,
		ReturnURL: claims.ReturnURL, Nonce: claims.Nonce, ExpiresAt: time.Unix(claims.ExpiresAt, 0),
	}
	if err := s.store.CreateOAuthState(ctx, record); err != nil {
		return nil, fmt.Errorf("store TikTok Shop OAuth state: %w", err)
	}
	authorizationURL, err := BuildSellerAuthorizationURL(s.config.ServiceID, state)
	if err != nil {
		return nil, err
	}
	return &AuthorizationStart{AuthorizationURL: authorizationURL, State: state, ExpiresAt: record.ExpiresAt}, nil
}

func (s *OAuthService) CompleteAuthorization(ctx context.Context, authCode, state, callbackError string) (*AuthorizationResult, error) {
	if s == nil {
		return nil, ErrOAuthServiceNotConfigured
	}
	now := s.config.Now()
	claims, err := s.stateSigner.Verify(state, now)
	if err != nil {
		return nil, ErrInvalidOAuthCallback
	}
	tenant, err := s.store.TenantBySlug(ctx, claims.Tenant)
	if err != nil || tenant == nil || !tenant.Enabled {
		return nil, ErrTenantNotAvailable
	}
	record, err := s.store.ConsumeOAuthState(ctx, HashOAuthState(state))
	if err != nil || !oauthStateMatches(record, tenant, claims) {
		return nil, ErrInvalidOAuthCallback
	}
	if strings.TrimSpace(callbackError) != "" {
		return nil, ErrAuthorizationDenied
	}
	authCode = strings.TrimSpace(authCode)
	if authCode == "" {
		return nil, ErrInvalidOAuthCallback
	}
	tokens, err := s.tokens.ExchangeAuthCode(ctx, authCode)
	if err != nil {
		return nil, fmt.Errorf("exchange TikTok Shop authorization code: %w", err)
	}
	if tokens == nil || tokens.UserType != tiktokshop.SellerUserType {
		return nil, ErrUnexpectedSellerUser
	}
	if !containsEveryScope(tokens.GrantedScopes, requiredOAuthScopes) {
		return nil, ErrRequiredScopeMissing
	}
	accessExpiresAt := time.Unix(tokens.AccessTokenExpiresAt, 0)
	refreshExpiresAt := time.Unix(tokens.RefreshTokenExpiresAt, 0)
	if strings.TrimSpace(tokens.OpenID) == "" || !accessExpiresAt.After(now) || !refreshExpiresAt.After(accessExpiresAt) {
		return nil, ErrInvalidOAuthCallback
	}
	authorizedShops, _, err := s.shops.GetAuthorizedShops(ctx, tokens.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("get TikTok Shop authorized shops: %w", err)
	}
	connections := make([]EncryptedConnection, 0, len(authorizedShops))
	resultShops := make([]ConnectedShop, 0, len(authorizedShops))
	seen := make(map[string]struct{}, len(authorizedShops))
	for _, shop := range authorizedShops {
		shop.ID = strings.TrimSpace(shop.ID)
		if shop.ID == "" || strings.TrimSpace(shop.Cipher) == "" {
			return nil, tiktokshop.ErrInvalidShopResponse
		}
		if _, exists := seen[shop.ID]; exists {
			return nil, tiktokshop.ErrInvalidShopResponse
		}
		seen[shop.ID] = struct{}{}
		accessCipher, accessNonce, err := s.tokenCipher.Encrypt(tokens.AccessToken, tokenAAD(tenant.Slug, shop.ID, "access"))
		if err != nil {
			return nil, fmt.Errorf("encrypt TikTok Shop access token: %w", err)
		}
		refreshCipher, refreshNonce, err := s.tokenCipher.Encrypt(tokens.RefreshToken, tokenAAD(tenant.Slug, shop.ID, "refresh"))
		if err != nil {
			return nil, fmt.Errorf("encrypt TikTok Shop refresh token: %w", err)
		}
		connections = append(connections, EncryptedConnection{
			TenantID: tenant.ID, TenantSlug: tenant.Slug, ShopID: shop.ID, ShopCipher: strings.TrimSpace(shop.Cipher),
			ShopName: strings.TrimSpace(shop.Name), ShopRegion: strings.TrimSpace(shop.Region),
			SellerType: strings.TrimSpace(shop.SellerType), ShopCode: strings.TrimSpace(shop.Code),
			OpenID: strings.TrimSpace(tokens.OpenID), SellerName: strings.TrimSpace(tokens.SellerName),
			SellerBaseRegion: strings.TrimSpace(tokens.SellerBaseRegion), GrantedScopes: append([]string(nil), tokens.GrantedScopes...),
			AccessTokenCipher: accessCipher, AccessTokenNonce: accessNonce,
			RefreshTokenCipher: refreshCipher, RefreshTokenNonce: refreshNonce,
			EncryptionKeyVersion: s.config.EncryptionKeyVersion,
			AccessExpiresAt:      accessExpiresAt, RefreshExpiresAt: refreshExpiresAt,
		})
		resultShops = append(resultShops, ConnectedShop{ID: shop.ID, Name: strings.TrimSpace(shop.Name), Region: strings.TrimSpace(shop.Region), SellerType: strings.TrimSpace(shop.SellerType), Code: strings.TrimSpace(shop.Code)})
	}
	if err := s.store.UpsertConnections(ctx, connections); err != nil {
		return nil, fmt.Errorf("save TikTok Shop connections: %w", err)
	}
	return &AuthorizationResult{TenantSlug: tenant.Slug, ReturnURL: record.ReturnURL, Shops: resultShops}, nil
}

func oauthStateMatches(record *OAuthStateRecord, tenant *Tenant, claims OAuthStateClaims) bool {
	if record == nil || tenant == nil {
		return false
	}
	return record.TenantID == tenant.ID && record.UserID == claims.UserID && record.ReturnURL == claims.ReturnURL && record.Nonce == claims.Nonce && record.ExpiresAt.Unix() == claims.ExpiresAt
}

func containsEveryScope(granted, required []string) bool {
	set := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		set[strings.TrimSpace(scope)] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := set[scope]; !ok {
			return false
		}
	}
	return true
}
