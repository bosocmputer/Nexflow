package tiktokgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexflow/internal/services/gatewayauth"
)

type handlerOAuthServiceFake struct {
	start       *AuthorizationStart
	result      *AuthorizationResult
	err         error
	tenant      string
	userID      string
	returnURL   string
	authCode    string
	state       string
	callbackErr string
	connections []ConnectionView
}

func (f *handlerOAuthServiceFake) ListConnections(_ context.Context, tenant string) ([]ConnectionView, error) {
	f.tenant = tenant
	return append([]ConnectionView(nil), f.connections...), f.err
}

func (f *handlerOAuthServiceFake) BeginAuthorization(_ context.Context, tenant, userID, returnURL string) (*AuthorizationStart, error) {
	f.tenant, f.userID, f.returnURL = tenant, userID, returnURL
	return f.start, f.err
}

func (f *handlerOAuthServiceFake) CompleteAuthorization(_ context.Context, authCode, state, callbackErr string) (*AuthorizationResult, error) {
	f.authCode, f.state, f.callbackErr = authCode, state, callbackErr
	return f.result, f.err
}

type handlerVerifierFake struct{ err error }

func (v handlerVerifierFake) Verify(context.Context, *http.Request, []byte) (*gatewayauth.Identity, error) {
	if v.err != nil {
		return nil, v.err
	}
	return &gatewayauth.Identity{Tenant: "aoy", Nonce: "nonce-1"}, nil
}

type handlerAuditFake struct {
	tenant, nonce, operation, errorCode, requestID string
	status, calls                                  int
}

func (f *handlerAuditFake) RecordAPIResult(_ context.Context, tenant, nonce, operation string, statusCode, _ int, errorCode, requestID string) error {
	f.tenant, f.nonce, f.operation, f.errorCode, f.requestID = tenant, nonce, operation, errorCode, requestID
	f.status, f.calls = statusCode, f.calls+1
	return nil
}

func TestTikTokGatewayHandlerCreatesTenantScopedAuthorizationURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerOAuthServiceFake{start: &AuthorizationStart{
		AuthorizationURL: "https://services.tiktokshop.com/open/authorize?service_id=1&state=signed",
		State:            "signed", ExpiresAt: time.Date(2026, time.September, 10, 11, 0, 0, 0, time.UTC),
	}}
	audit := &handlerAuditFake{}
	handler := NewHandler(service, handlerVerifierFake{}, audit, Config{PublicBaseURL: "https://tiktok-shop-gateway.nextstep-soft.com"}, nil)
	router := gin.New()
	handler.Register(router)

	request := httptest.NewRequest(http.MethodPost, GatewayOAuthPath, strings.NewReader(`{"user_id":"admin-1","return_url":"https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			AuthURL     string `json:"auth_url"`
			RedirectURL string `json:"redirect_url"`
			ExpiresAt   string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.AuthURL != service.start.AuthorizationURL || payload.Data.RedirectURL != "https://tiktok-shop-gateway.nextstep-soft.com/api/tiktok-shop/callback" || payload.Data.ExpiresAt == "" {
		t.Fatalf("payload = %+v", payload)
	}
	if service.tenant != "aoy" || service.userID != "admin-1" || audit.operation != "oauth_auth_url" || audit.status != http.StatusOK {
		t.Fatalf("service = %+v, audit = %+v", service, audit)
	}
}

func TestTikTokGatewayHandlerRejectsUnsignedInternalRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{err: gatewayauth.ErrMissingAuthentication}, nil, Config{}, nil)
	router := gin.New()
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, GatewayOAuthPath, strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "invalid_internal_auth") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestTikTokGatewayHandlerListsConnectionsWithoutCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerOAuthServiceFake{connections: []ConnectionView{{
		GatewayConnectionID: "connection-1", ShopID: "7000714532876273420", ShopName: "AOY Main", ShopRegion: "TH",
		GrantedScopes: []string{"seller.authorization.info", "seller.order.info"}, AccessExpiresAt: "2026-09-11T10:00:00Z",
	}}}
	handler := NewHandler(service, handlerVerifierFake{}, nil, Config{}, nil)
	router := gin.New()
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, GatewayConnectionsPath, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "7000714532876273420") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "token") || service.tenant != "aoy" {
		t.Fatalf("response exposed credentials or tenant mismatch: %s", response.Body.String())
	}
}

func TestTikTokGatewayHandlerRedirectsSuccessfulCallbackToSavedTenantURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerOAuthServiceFake{result: &AuthorizationResult{
		TenantSlug: "aoy", ReturnURL: "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop?connected=1",
		Shops: []ConnectedShop{{ID: "shop-1", Name: "AOY"}},
	}}
	handler := NewHandler(service, handlerVerifierFake{}, nil, Config{}, nil)
	router := gin.New()
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/tiktok-shop/callback?code=auth-code&state=signed-state", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != service.result.ReturnURL {
		t.Fatalf("status = %d, location = %q", response.Code, response.Header().Get("Location"))
	}
	if service.authCode != "auth-code" || service.state != "signed-state" || service.callbackErr != "" {
		t.Fatalf("callback inputs = %+v", service)
	}
}

func TestTikTokGatewayHandlerDoesNotExposeCallbackInternals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerOAuthServiceFake{err: errors.Join(ErrInvalidOAuthCallback, errors.New("upstream contains access-token-secret"))}
	handler := NewHandler(service, handlerVerifierFake{}, nil, Config{}, nil)
	router := gin.New()
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/tiktok-shop/callback?error=access_denied&state=signed-state", nil))
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "access-token-secret") || strings.Contains(response.Body.String(), "access_denied") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
