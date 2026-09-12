package tiktokgateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const webhookTestBody = `{"type":1,"tts_notification_id":"7327112393057371910","shop_id":"7494619203789490654","timestamp":1644412885,"data":{"order_id":"576486316948490001","order_status":"UNPAID","is_on_hold_order":false,"update_time":1644412885}}`

type webhookReceiverFake struct {
	input  WebhookEventInput
	result *WebhookEventResult
	err    error
	calls  int
}

func (f *webhookReceiverFake) AcceptWebhookEvent(_ context.Context, input WebhookEventInput) (*WebhookEventResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func TestParseOrderStatusWebhookUsesTypedIdentifiers(t *testing.T) {
	event, err := ParseOrderStatusWebhook([]byte(webhookTestBody))
	if err != nil {
		t.Fatalf("ParseOrderStatusWebhook() error = %v", err)
	}
	if event.NotificationID != "7327112393057371910" || event.ShopID != "7494619203789490654" ||
		event.OrderID != "576486316948490001" || event.OrderStatus != "UNPAID" || event.NotificationType != 1 {
		t.Fatalf("event = %+v", event)
	}
}

func TestParseOrderStatusWebhookRejectsWrongTopicAndMissingIdentifiers(t *testing.T) {
	for _, body := range []string{
		`{"type":11,"tts_notification_id":"1","shop_id":"2","timestamp":3,"data":{"order_id":"4","order_status":"UNPAID","update_time":3}}`,
		`{"type":1,"tts_notification_id":"","shop_id":"2","timestamp":3,"data":{"order_id":"4","order_status":"UNPAID","update_time":3}}`,
		`{"type":1,"tts_notification_id":"1","shop_id":"other","timestamp":3,"data":{"order_id":"4","order_status":"UNPAID","update_time":3}}`,
	} {
		if _, err := ParseOrderStatusWebhook([]byte(body)); err == nil {
			t.Fatalf("ParseOrderStatusWebhook(%s) error = nil", body)
		}
	}
}

func TestWebhookHandlerAuthenticatesRawBodyAndAcknowledgesEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receiver := &webhookReceiverFake{result: &WebhookEventResult{Inserted: true, Tenant: &Tenant{Slug: "aoy"}}}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil,
		Config{AppKey: "app-key", AppSecret: "app-secret", WebhookEnabled: true}, nil,
		WithWebhookReceiver(receiver))
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodPost, "/webhook/tiktok-shop", strings.NewReader(webhookTestBody))
	req.Header.Set("Authorization", signWebhookTestBody("app-key", "app-secret", webhookTestBody))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if receiver.calls != 1 || receiver.input.OrderID != "576486316948490001" || len(receiver.input.BodySHA256) != 64 {
		t.Fatalf("receiver = %+v calls=%d", receiver.input, receiver.calls)
	}
}

func TestWebhookHandlerRejectsInvalidSignatureWithEmpty401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receiver := &webhookReceiverFake{}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil,
		Config{AppKey: "app-key", AppSecret: "app-secret", WebhookEnabled: true}, nil,
		WithWebhookReceiver(receiver))
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodPost, "/webhook/tiktok-shop", strings.NewReader(webhookTestBody))
	req.Header.Set("Authorization", strings.Repeat("0", 64))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusUnauthorized || response.Body.Len() != 0 || receiver.calls != 0 {
		t.Fatalf("status=%d body=%q calls=%d", response.Code, response.Body.String(), receiver.calls)
	}
}

func TestWebhookHandlerAcknowledgesDuplicateWithoutRequeue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receiver := &webhookReceiverFake{result: &WebhookEventResult{Inserted: false, Tenant: &Tenant{Slug: "aoy"}}}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil,
		Config{AppKey: "app-key", AppSecret: "app-secret", WebhookEnabled: true}, nil,
		WithWebhookReceiver(receiver))
	router := gin.New()
	handler.Register(router)
	req := httptest.NewRequest(http.MethodPost, "/webhook/tiktok-shop", strings.NewReader(webhookTestBody))
	req.Header.Set("Authorization", signWebhookTestBody("app-key", "app-secret", webhookTestBody))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || receiver.calls != 1 {
		t.Fatalf("status=%d body=%q calls=%d", response.Code, response.Body.String(), receiver.calls)
	}
}

func TestWebhookHandlerRejectsOversizedBodyBeforeReceiver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receiver := &webhookReceiverFake{}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil,
		Config{AppKey: "app-key", AppSecret: "app-secret", WebhookEnabled: true}, nil,
		WithWebhookReceiver(receiver))
	router := gin.New()
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/webhook/tiktok-shop", strings.NewReader(strings.Repeat("x", maxWebhookBodySize+1))))
	if response.Code != http.StatusRequestEntityTooLarge || response.Body.Len() != 0 || receiver.calls != 0 {
		t.Fatalf("status=%d body=%q calls=%d", response.Code, response.Body.String(), receiver.calls)
	}
}

func TestWebhookHandlerFailsClosedWhenDisabledOrStorageFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	disabled := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil, Config{}, nil,
		WithWebhookReceiver(&webhookReceiverFake{}))
	disabledRouter := gin.New()
	disabled.Register(disabledRouter)
	disabledResponse := httptest.NewRecorder()
	disabledRouter.ServeHTTP(disabledResponse, httptest.NewRequest(http.MethodPost, "/webhook/tiktok-shop", strings.NewReader(webhookTestBody)))
	if disabledResponse.Code != http.StatusNotFound || disabledResponse.Body.Len() != 0 {
		t.Fatalf("disabled status=%d body=%q", disabledResponse.Code, disabledResponse.Body.String())
	}

	receiver := &webhookReceiverFake{err: errors.New("database unavailable")}
	enabled := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil,
		Config{AppKey: "app-key", AppSecret: "app-secret", WebhookEnabled: true}, nil,
		WithWebhookReceiver(receiver))
	enabledRouter := gin.New()
	enabled.Register(enabledRouter)
	req := httptest.NewRequest(http.MethodPost, "/webhook/tiktok-shop", strings.NewReader(webhookTestBody))
	req.Header.Set("Authorization", signWebhookTestBody("app-key", "app-secret", webhookTestBody))
	failedResponse := httptest.NewRecorder()
	enabledRouter.ServeHTTP(failedResponse, req)
	if failedResponse.Code != http.StatusServiceUnavailable || failedResponse.Body.Len() != 0 {
		t.Fatalf("failed status=%d body=%q", failedResponse.Code, failedResponse.Body.String())
	}
}

func signWebhookTestBody(appKey, appSecret, body string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write([]byte(appKey))
	_, _ = mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}
