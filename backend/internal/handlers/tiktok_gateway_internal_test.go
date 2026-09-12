package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/tiktokshop"
)

type tikTokWebhookIngesterFake struct {
	delivery tiktokshop.GatewayWebhookDelivery
	inserted bool
	err      error
	calls    int
}

func (f *tikTokWebhookIngesterFake) Ingest(_ context.Context, delivery tiktokshop.GatewayWebhookDelivery) (bool, error) {
	f.calls++
	f.delivery = delivery
	return f.inserted, f.err
}

func TestTikTokGatewayInternalHandlerAuthenticatesAndQueues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ingester := &tikTokWebhookIngesterFake{inserted: true}
	handler := NewTikTokGatewayInternalHandler(database, &config.Config{
		TikTokShopWebhookEnabled: true, TikTokShopOpenAPIEnabled: true,
		TikTokShopGatewayTenant: "aoy", TikTokShopGatewayInternalSecret: "secret",
	}, ingester, nil)
	handler.verify = gatewayauth.Verifier{ResolveSecret: func(context.Context, string) (string, error) { return "secret", nil }, Now: func() time.Time { return time.Unix(1, 0) }}
	router := gin.New()
	handler.Register(router)
	body := `{"gateway_event_id":"11111111-1111-4111-8111-111111111111","tts_notification_id":"1","shop_id":"2","order_id":"3","order_status":"UNPAID","timestamp":"2026-09-12T00:00:00Z","order_update_at":"2026-09-12T00:00:00Z"}`
	request := httptest.NewRequest(http.MethodPost, tiktokshop.GatewayWebhookDeliveryPath, strings.NewReader(body))
	request.Header.Set(gatewayauth.HeaderTenant, "aoy")
	request.Header.Set(gatewayauth.HeaderTimestamp, "1")
	request.Header.Set(gatewayauth.HeaderNonce, "nonce")
	request.Header.Set(gatewayauth.HeaderSignature, gatewayauth.Sign("secret", request.Method, request.URL.RequestURI(), "aoy", "1", "nonce", []byte(body)))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || ingester.calls != 1 || ingester.delivery.OrderID != "3" {
		t.Fatalf("status=%d body=%s calls=%d delivery=%+v", response.Code, response.Body.String(), ingester.calls, ingester.delivery)
	}
}

func TestTikTokGatewayInternalHandlerIsHiddenWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler := NewTikTokGatewayInternalHandler(database, &config.Config{}, &tikTokWebhookIngesterFake{}, nil)
	router := gin.New()
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tiktokshop.GatewayWebhookDeliveryPath, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokGatewayInternalHandlerRejectsReplayedNonce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, time.September, 12, 10, 0, 0, 0, time.UTC)
	mock.ExpectExec("INSERT INTO tiktok_gateway_request_nonces").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM tiktok_gateway_request_nonces").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO tiktok_gateway_request_nonces").WillReturnResult(sqlmock.NewResult(0, 0))
	ingester := &tikTokWebhookIngesterFake{inserted: true}
	handler := NewTikTokGatewayInternalHandler(database, &config.Config{
		TikTokShopWebhookEnabled: true, TikTokShopOpenAPIEnabled: true,
		TikTokShopGatewayTenant: "aoy", TikTokShopGatewayInternalSecret: "secret",
	}, ingester, nil)
	handler.verify.Now = func() time.Time { return now }
	router := gin.New()
	handler.Register(router)
	body := `{"gateway_event_id":"11111111-1111-4111-8111-111111111111","tts_notification_id":"1","shop_id":"2","order_id":"3","order_status":"UNPAID","timestamp":"2026-09-12T00:00:00Z","order_update_at":"2026-09-12T00:00:00Z"}`
	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, tiktokshop.GatewayWebhookDeliveryPath, strings.NewReader(body))
		if err := gatewayauth.Apply(request, "aoy", "secret", []byte(body), now, "same-nonce"); err != nil {
			t.Fatal(err)
		}
		return request
	}
	first := httptest.NewRecorder()
	router.ServeHTTP(first, newRequest())
	second := httptest.NewRecorder()
	router.ServeHTTP(second, newRequest())
	if first.Code != http.StatusOK || second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "replayed_request") || ingester.calls != 1 {
		t.Fatalf("first=%d second=%d body=%s calls=%d", first.Code, second.Code, second.Body.String(), ingester.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
