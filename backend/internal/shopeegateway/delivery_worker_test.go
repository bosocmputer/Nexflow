package shopeegateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nexflow/internal/services/gatewayauth"
)

type deliveryStoreFake struct {
	jobs   []DeliveryJob
	done   []DeliveryJob
	failed []DeliveryJob
}

func (s *deliveryStoreFake) LeaseDeliveries(context.Context, int) ([]DeliveryJob, error) {
	return s.jobs, nil
}
func (s *deliveryStoreFake) MarkDeliveryDone(_ context.Context, job DeliveryJob) error {
	s.done = append(s.done, job)
	return nil
}
func (s *deliveryStoreFake) MarkDeliveryFailed(_ context.Context, job DeliveryJob, _ string, _ time.Time) error {
	s.failed = append(s.failed, job)
	return nil
}
func (s *deliveryStoreFake) RecordOutboundAPIResult(context.Context, DeliveryJob, string, string, int, int, string, string) error {
	return nil
}

func TestDeliveryWorkerSignsTenantRequest(t *testing.T) {
	master := testEncodedKey(51)
	secret, err := DeriveTenantSecret(master, "aoy")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte(`{"shop_id":99}`)
		timestamp := r.Header.Get(gatewayauth.HeaderTimestamp)
		nonce := r.Header.Get(gatewayauth.HeaderNonce)
		want := gatewayauth.Sign(secret, r.Method, r.URL.RequestURI(), "aoy", timestamp, nonce, body)
		if r.Header.Get(gatewayauth.HeaderSignature) != want {
			t.Errorf("invalid signature")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store := &deliveryStoreFake{jobs: []DeliveryJob{{
		ID: "job", TenantID: "tenant", TenantSlug: "aoy", BackendURL: server.URL,
		EventType: "push_event", Payload: json.RawMessage(`{"shop_id":99}`), Attempts: 1,
	}}}
	worker := NewDeliveryWorker(Config{InternalMasterKey: master, TenantHTTPTimeout: time.Second}, store, nil)
	if count, err := worker.ProcessBatch(context.Background(), 1); err != nil || count != 1 {
		t.Fatalf("ProcessBatch count=%d err=%v", count, err)
	}
	if len(store.done) != 1 || len(store.failed) != 0 {
		t.Fatalf("done=%d failed=%d", len(store.done), len(store.failed))
	}
}
