package shopeegateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/shopeeapi"
)

type fakeRouteSyncStore struct {
	tenants []Tenant
	synced  map[string][]int64
}

func (f *fakeRouteSyncStore) ActiveTenants(context.Context) ([]Tenant, error) {
	return f.tenants, nil
}

func (f *fakeRouteSyncStore) SyncTenantRoutes(_ context.Context, tenantID string, shopIDs []int64) error {
	if f.synced == nil {
		f.synced = make(map[string][]int64)
	}
	f.synced[tenantID] = append([]int64(nil), shopIDs...)
	return nil
}

func TestRouteSyncWorkerSignsAndDeduplicatesTenantRoutes(t *testing.T) {
	master := testEncodedKey(9)
	secret, err := DeriveTenantSecret(master, "aoy")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1783526400, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != shopeeapi.GatewayTenantRoutesPath {
			t.Fatalf("path=%s", r.URL.Path)
		}
		want := gatewayauth.Sign(secret, r.Method, r.URL.RequestURI(), "aoy", r.Header.Get(gatewayauth.HeaderTimestamp), r.Header.Get(gatewayauth.HeaderNonce), body)
		if got := r.Header.Get(gatewayauth.HeaderSignature); got != want {
			t.Fatalf("signature=%s want=%s", got, want)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"shop_ids": []int64{22, 11, 22}}})
	}))
	defer server.Close()
	store := &fakeRouteSyncStore{tenants: []Tenant{{ID: "tenant-id", Slug: "aoy", BackendURL: server.URL, Enabled: true}}}
	worker := NewRouteSyncWorker(Config{InternalMasterKey: master, TenantHTTPTimeout: time.Second}, store, nil)
	worker.now = func() time.Time { return now }
	result, err := worker.SyncOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Synced != 1 || result.Routes != 2 || result.Failed != 0 {
		t.Fatalf("result=%+v", result)
	}
	got := store.synced["tenant-id"]
	if len(got) != 2 || got[0] != 22 || got[1] != 11 {
		t.Fatalf("routes=%v", got)
	}
}

func TestRouteSyncWorkerContinuesAfterTenantFailure(t *testing.T) {
	master := testEncodedKey(7)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"shop_ids":[]}}`)
	}))
	defer working.Close()
	store := &fakeRouteSyncStore{tenants: []Tenant{
		{ID: "bad-id", Slug: "bad", BackendURL: failing.URL, Enabled: true},
		{ID: "good-id", Slug: "good", BackendURL: working.URL, Enabled: true},
	}}
	worker := NewRouteSyncWorker(Config{InternalMasterKey: master, TenantHTTPTimeout: time.Second}, store, nil)
	result, err := worker.SyncOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("error=%v", err)
	}
	if result.Synced != 1 || result.Failed != 1 {
		t.Fatalf("result=%+v", result)
	}
	if _, exists := store.synced["good-id"]; !exists {
		t.Fatal("working tenant was not synchronized")
	}
}

func TestRouteSyncWorkerDoesNotClearRoutesOnMalformedSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{}}`)
	}))
	defer server.Close()
	store := &fakeRouteSyncStore{tenants: []Tenant{{ID: "tenant-id", Slug: "aoy", BackendURL: server.URL, Enabled: true}}}
	worker := NewRouteSyncWorker(Config{InternalMasterKey: testEncodedKey(3), TenantHTTPTimeout: time.Second}, store, nil)
	result, err := worker.SyncOnce(t.Context())
	if err == nil || result.Failed != 1 || result.Synced != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if len(store.synced) != 0 {
		t.Fatalf("malformed response changed routes: %v", store.synced)
	}
}
