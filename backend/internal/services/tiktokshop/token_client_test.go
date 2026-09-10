package tiktokshop

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenClientExchangesAuthCodeWithoutExposingCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/token/get" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("app_key") != "app-key" || query.Get("app_secret") != "app-secret" || query.Get("auth_code") != "one-time-code" || query.Get("grant_type") != "authorized_code" {
			t.Fatalf("unexpected token query: %v", query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","request_id":"request-1","data":{"access_token":"access-token","access_token_expire_in":1784073600,"refresh_token":"refresh-token","refresh_token_expire_in":1784678400,"open_id":"seller-open-id","seller_name":"AOY","seller_base_region":"TH","user_type":0,"granted_scopes":["seller.authorization.info","seller.order.info"]}}`))
	}))
	defer server.Close()

	client, err := NewTokenClient(TokenClientConfig{
		BaseURL:    server.URL,
		AppKey:     "app-key",
		AppSecret:  "app-secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewTokenClient() error = %v", err)
	}
	tokens, err := client.ExchangeAuthCode(context.Background(), "one-time-code")
	if err != nil {
		t.Fatalf("ExchangeAuthCode() error = %v", err)
	}
	if tokens.AccessToken != "access-token" || tokens.RefreshToken != "refresh-token" || tokens.UserType != SellerUserType || tokens.RequestID != "request-1" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if len(tokens.GrantedScopes) != 2 || tokens.GrantedScopes[1] != "seller.order.info" {
		t.Fatalf("granted scopes = %#v", tokens.GrantedScopes)
	}
}

func TestTokenClientRejectsMissingAuthCodeBeforeNetwork(t *testing.T) {
	client, err := NewTokenClient(TokenClientConfig{
		BaseURL:   "https://auth.tiktok-shops.com",
		AppKey:    "app-key",
		AppSecret: "app-secret",
		HTTPClient: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}},
	})
	if err != nil {
		t.Fatalf("NewTokenClient() error = %v", err)
	}
	if _, err := client.ExchangeAuthCode(context.Background(), " "); !errors.Is(err, ErrInvalidTokenInput) {
		t.Fatalf("ExchangeAuthCode() error = %v", err)
	}
}

func TestTokenClientReturnsSanitizedTikTokError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":105005,"message":"scope missing for seller@example.com","request_id":"request-2"}`))
	}))
	defer server.Close()

	client, err := NewTokenClient(TokenClientConfig{BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewTokenClient() error = %v", err)
	}
	_, err = client.ExchangeAuthCode(context.Background(), "one-time-code")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Code != 105005 || apiErr.RequestID != "request-2" || apiErr.Message != "TikTok Shop rejected the token request" {
		t.Fatalf("APIError = %+v", apiErr)
	}
}

func TestTokenClientDoesNotExposeSecretsFromTransportError(t *testing.T) {
	client, err := NewTokenClient(TokenClientConfig{
		BaseURL: "https://auth.tiktok-shops.com", AppKey: "app-key-secret", AppSecret: "app-secret-value",
		HTTPClient: &http.Client{Transport: tokenRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network failed for refresh-token-secret")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Refresh(context.Background(), "refresh-token-secret")
	if !errors.Is(err, ErrTokenTransport) {
		t.Fatalf("Refresh() error = %v", err)
	}
	for _, secret := range []string{"app-key-secret", "app-secret-value", "refresh-token-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

type tokenRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f tokenRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
