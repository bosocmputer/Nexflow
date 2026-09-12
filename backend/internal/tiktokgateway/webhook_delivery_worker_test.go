package tiktokgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nexflow/internal/services/gatewayauth"
)

type webhookDeliveryStoreFake struct {
	jobs           []WebhookDeliveryJob
	done           []WebhookDeliveryJob
	failed         []WebhookDeliveryJob
	auditNonce     string
	auditRequestID string
}

func (s *webhookDeliveryStoreFake) LeaseWebhookDeliveries(context.Context, int) ([]WebhookDeliveryJob, error) {
	return s.jobs, nil
}
func (s *webhookDeliveryStoreFake) MarkWebhookDeliveryDone(_ context.Context, job WebhookDeliveryJob) error {
	s.done = append(s.done, job)
	return nil
}
func (s *webhookDeliveryStoreFake) MarkWebhookDeliveryFailed(_ context.Context, job WebhookDeliveryJob, _ string, _ time.Time) error {
	s.failed = append(s.failed, job)
	return nil
}
func (s *webhookDeliveryStoreFake) RecordWebhookDeliveryResult(_ context.Context, _ WebhookDeliveryJob, nonce string, _ int, _ int, _ string, requestID string) error {
	s.auditNonce = nonce
	s.auditRequestID = requestID
	return nil
}

func TestWebhookDeliveryWorkerSignsTenantRequest(t *testing.T) {
	master := testEncodedKey(51)
	secret, err := DeriveTenantSecret(master, "aoy")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"gateway_event_id":"11111111-1111-4111-8111-111111111111","tts_notification_id":"1","shop_id":"2","order_id":"3","order_status":"UNPAID","timestamp":"2026-09-12T00:00:00Z","order_update_at":"2026-09-12T00:00:00Z"}`)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/internal/v1/tiktok-shop/webhooks/order-status" {
			t.Errorf("path = %q", request.URL.Path)
		}
		timestamp := request.Header.Get(gatewayauth.HeaderTimestamp)
		nonce := request.Header.Get(gatewayauth.HeaderNonce)
		want := gatewayauth.Sign(secret, request.Method, request.URL.RequestURI(), "aoy", timestamp, nonce, payload)
		if request.Header.Get(gatewayauth.HeaderSignature) != want {
			t.Error("invalid tenant signature")
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store := &webhookDeliveryStoreFake{jobs: []WebhookDeliveryJob{{
		ID: "job", WebhookEventID: "event", TenantID: "tenant", TenantSlug: "aoy",
		BackendURL: server.URL, Payload: payload, Attempts: 1,
	}}}
	worker := NewWebhookDeliveryWorker(Config{InternalMasterKey: master, TenantHTTPTimeout: time.Second}, store, nil)
	if count, err := worker.ProcessBatch(t.Context(), 1); err != nil || count != 1 {
		t.Fatalf("ProcessBatch() count=%d error=%v", count, err)
	}
	if len(store.done) != 1 || len(store.failed) != 0 {
		t.Fatalf("done=%d failed=%d", len(store.done), len(store.failed))
	}
}

func TestWebhookDeliveryWorkerAuditsDerivedSecretFailureWithRequestIdentity(t *testing.T) {
	store := &webhookDeliveryStoreFake{jobs: []WebhookDeliveryJob{{
		ID: "job", WebhookEventID: "event", TenantID: "tenant", TenantSlug: "aoy",
		BackendURL: "http://unused.invalid", Payload: json.RawMessage(`{}`), Attempts: 1,
	}}}
	worker := NewWebhookDeliveryWorker(Config{InternalMasterKey: "invalid"}, store, nil)
	if count, err := worker.ProcessBatch(t.Context(), 1); err != nil || count != 1 {
		t.Fatalf("ProcessBatch() count=%d error=%v", count, err)
	}
	if len(store.done) != 0 || len(store.failed) != 1 {
		t.Fatalf("done=%d failed=%d", len(store.done), len(store.failed))
	}
	if store.auditNonce == "" || store.auditRequestID == "" {
		t.Fatalf("audit identity missing: nonce=%q request_id=%q", store.auditNonce, store.auditRequestID)
	}
}
