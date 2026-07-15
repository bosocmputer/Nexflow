package shopeegateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/shopeeapi"
)

type handlerServiceFake struct{}

func (handlerServiceFake) CreateAuthURL(context.Context, string, string, string) (*shopeeapi.GatewayAuthURLResponse, error) {
	return &shopeeapi.GatewayAuthURLResponse{AuthURL: "https://shopee.example/authorize", RedirectURL: "https://gateway.example/callback"}, nil
}
func (handlerServiceFake) CompleteOAuth(context.Context, string, string, int64) (*OAuthCallbackResult, error) {
	return nil, serviceError("invalid_oauth_callback", "missing state", 400, false, nil)
}
func (handlerServiceFake) Execute(context.Context, string, GatewayExecuteRequest) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

type verifierFake struct{ err error }

func (v verifierFake) Verify(context.Context, *http.Request, []byte) (*gatewayauth.Identity, error) {
	if v.err != nil {
		return nil, v.err
	}
	return &gatewayauth.Identity{Tenant: "aoy", Nonce: "nonce"}, nil
}

type pushReceiverFake struct{ result *PushEventResult }

func (p pushReceiverFake) AcceptPushEvent(context.Context, PushEventInput) (*PushEventResult, error) {
	if p.result == nil {
		return nil, errors.New("missing result")
	}
	return p.result, nil
}

func TestHandlerRejectsUnsignedInternalRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(handlerServiceFake{}, verifierFake{err: gatewayauth.ErrMissingAuthentication}, nil, nil, Config{}, nil)
	handler.Register(router)
	req := httptest.NewRequest(http.MethodPost, shopeeapi.GatewayExecutePath, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerMissingOAuthStateDoesNotFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(handlerServiceFake{}, verifierFake{}, nil, nil, Config{}, nil)
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/shopee/callback?code=x&shop_id=1", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPushWebhookUnknownShopAcknowledgesWithoutRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	config := Config{PushSecret: "push-secret", PublicBaseURL: "https://gateway.example"}
	handler := NewHandler(handlerServiceFake{}, verifierFake{}, nil, pushReceiverFake{result: &PushEventResult{Inserted: true}}, config, nil)
	handler.Register(router)
	body := `{"shop_id":99,"code":3,"data":{"ordersn":"ORDER-1"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook/shopee?token=push-secret", strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !json.Valid(response.Body.Bytes()) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
