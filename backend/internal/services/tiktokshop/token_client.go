package tiktokshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultTokenBaseURL = "https://auth.tiktok-shops.com"
	SellerUserType      = 0
)

var (
	ErrInvalidTokenInput = errors.New("invalid TikTok Shop token input")
	ErrTokenTransport    = errors.New("TikTok Shop token transport failed")
)

type TokenClientConfig struct {
	BaseURL    string
	AppKey     string
	AppSecret  string
	HTTPClient *http.Client
}

type TokenClient struct {
	baseURL   *url.URL
	appKey    string
	appSecret string
	http      *http.Client
}

type TokenSet struct {
	AccessToken           string
	AccessTokenExpiresAt  int64
	RefreshToken          string
	RefreshTokenExpiresAt int64
	OpenID                string
	SellerName            string
	SellerBaseRegion      string
	UserType              int
	GrantedScopes         []string
	RequestID             string
}

// APIError deliberately contains only TikTok's machine code and request ID.
// Token errors can include seller identifiers, so the upstream message is not
// propagated into logs or UI responses.
type APIError struct {
	Code      int
	RequestID string
	Message   string
}

func (e *APIError) Error() string {
	if e == nil {
		return "TikTok Shop API error"
	}
	if e.RequestID == "" {
		return fmt.Sprintf("TikTok Shop API error code %d", e.Code)
	}
	return fmt.Sprintf("TikTok Shop API error code %d (request_id %s)", e.Code, e.RequestID)
}

func NewTokenClient(config TokenClientConfig) (*TokenClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultTokenBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("TikTok Shop token base URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return nil, ErrInvalidTokenInput
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &TokenClient{baseURL: parsed, appKey: strings.TrimSpace(config.AppKey), appSecret: strings.TrimSpace(config.AppSecret), http: client}, nil
}

func (c *TokenClient) ExchangeAuthCode(ctx context.Context, authCode string) (*TokenSet, error) {
	authCode = strings.TrimSpace(authCode)
	if c == nil || c.baseURL == nil || authCode == "" {
		return nil, ErrInvalidTokenInput
	}
	return c.request(ctx, "/api/v2/token/get", url.Values{
		"app_key":    []string{c.appKey},
		"app_secret": []string{c.appSecret},
		"auth_code":  []string{authCode},
		"grant_type": []string{"authorized_code"},
	})
}

func (c *TokenClient) Refresh(ctx context.Context, refreshToken string) (*TokenSet, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if c == nil || c.baseURL == nil || refreshToken == "" {
		return nil, ErrInvalidTokenInput
	}
	return c.request(ctx, "/api/v2/token/refresh", url.Values{
		"app_key":       []string{c.appKey},
		"app_secret":    []string{c.appSecret},
		"refresh_token": []string{refreshToken},
		"grant_type":    []string{"refresh_token"},
	})
}

func (c *TokenClient) request(ctx context.Context, path string, query url.Values) (*TokenSet, error) {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + path
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, ErrTokenTransport
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		// net/http transport errors can include the full request URL. Token
		// endpoints carry app_secret and refresh_token in that URL, so never
		// wrap or propagate the original transport error.
		return nil, ErrTokenTransport
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, ErrTokenTransport
	}
	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode TikTok Shop token response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || payload.Code != 0 {
		return nil, &APIError{Code: payload.Code, RequestID: strings.TrimSpace(payload.RequestID), Message: "TikTok Shop rejected the token request"}
	}
	tokens := payload.Data.toTokenSet(payload.RequestID)
	if err := tokens.validate(); err != nil {
		return nil, fmt.Errorf("TikTok Shop token response: %w", err)
	}
	return tokens, nil
}

type tokenResponse struct {
	Code      int       `json:"code"`
	RequestID string    `json:"request_id"`
	Data      tokenData `json:"data"`
}

type tokenData struct {
	AccessToken          string   `json:"access_token"`
	AccessTokenExpireIn  int64    `json:"access_token_expire_in"`
	RefreshToken         string   `json:"refresh_token"`
	RefreshTokenExpireIn int64    `json:"refresh_token_expire_in"`
	OpenID               string   `json:"open_id"`
	SellerName           string   `json:"seller_name"`
	SellerBaseRegion     string   `json:"seller_base_region"`
	UserType             int      `json:"user_type"`
	GrantedScopes        []string `json:"granted_scopes"`
}

func (d tokenData) toTokenSet(requestID string) *TokenSet {
	return &TokenSet{
		AccessToken:           strings.TrimSpace(d.AccessToken),
		AccessTokenExpiresAt:  d.AccessTokenExpireIn,
		RefreshToken:          strings.TrimSpace(d.RefreshToken),
		RefreshTokenExpiresAt: d.RefreshTokenExpireIn,
		OpenID:                strings.TrimSpace(d.OpenID),
		SellerName:            strings.TrimSpace(d.SellerName),
		SellerBaseRegion:      strings.TrimSpace(d.SellerBaseRegion),
		UserType:              d.UserType,
		GrantedScopes:         append([]string(nil), d.GrantedScopes...),
		RequestID:             strings.TrimSpace(requestID),
	}
}

func (t *TokenSet) validate() error {
	if t == nil || t.AccessToken == "" || t.RefreshToken == "" || t.AccessTokenExpiresAt <= 0 || t.RefreshTokenExpiresAt <= 0 || t.OpenID == "" {
		return ErrInvalidTokenInput
	}
	return nil
}

func (t TokenSet) String() string {
	return "TikTokShopTokenSet{user_type=" + strconv.Itoa(t.UserType) + ", request_id=" + t.RequestID + "}"
}
