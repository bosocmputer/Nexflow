package gatewayauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

type fakeNonceStore struct {
	seen map[string]bool
}

func (f *fakeNonceStore) Consume(_ context.Context, tenant, nonce string, _ time.Time) error {
	key := tenant + ":" + nonce
	if f.seen[key] {
		return errors.New("duplicate nonce")
	}
	f.seen[key] = true
	return nil
}

func TestVerifierAcceptsSignedRequestOnce(t *testing.T) {
	now := time.Unix(1784070000, 0)
	body := []byte(`{"operation":"get_order_list"}`)
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/internal/v1/shopee/execute"}, Header: http.Header{}}
	if err := Apply(req, "aoy", "shared-secret", body, now, "nonce-1"); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	verifier := Verifier{
		ResolveSecret: func(_ context.Context, tenant string) (string, error) {
			if tenant != "aoy" {
				t.Fatalf("tenant = %q", tenant)
			}
			return "shared-secret", nil
		},
		Nonces:  &fakeNonceStore{seen: map[string]bool{}},
		MaxSkew: time.Minute,
		Now:     func() time.Time { return now },
	}
	identity, err := verifier.Verify(t.Context(), req, body)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if identity.Tenant != "aoy" || identity.Nonce != "nonce-1" {
		t.Fatalf("identity = %+v", identity)
	}
	if _, err := verifier.Verify(t.Context(), req, body); !errors.Is(err, ErrReplay) {
		t.Fatalf("second Verify() error = %v, want replay", err)
	}
}

func TestVerifierRejectsTamperedBody(t *testing.T) {
	now := time.Unix(1784070000, 0)
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/internal/v1/shopee/execute"}, Header: http.Header{}}
	if err := Apply(req, "demo", "shared-secret", []byte(`{"shop_id":1}`), now, "nonce-2"); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	verifier := Verifier{
		ResolveSecret: func(context.Context, string) (string, error) { return "shared-secret", nil },
		Now:           func() time.Time { return now },
	}
	if _, err := verifier.Verify(t.Context(), req, []byte(`{"shop_id":2}`)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() error = %v, want invalid signature", err)
	}
}

func TestVerifierRejectsExpiredTimestamp(t *testing.T) {
	signedAt := time.Unix(1784070000, 0)
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/internal/v1/shopee/execute"}, Header: http.Header{}}
	if err := Apply(req, "demo", "shared-secret", nil, signedAt, "nonce-3"); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	verifier := Verifier{
		ResolveSecret: func(context.Context, string) (string, error) { return "shared-secret", nil },
		MaxSkew:       time.Minute,
		Now:           func() time.Time { return signedAt.Add(2 * time.Minute) },
	}
	if _, err := verifier.Verify(t.Context(), req, nil); !errors.Is(err, ErrExpiredRequest) {
		t.Fatalf("Verify() error = %v, want expired request", err)
	}
}
