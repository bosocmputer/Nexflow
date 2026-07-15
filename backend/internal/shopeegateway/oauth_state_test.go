package shopeegateway

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOAuthStateIsSignedTenantScopedAndExpires(t *testing.T) {
	now := time.Unix(1784070000, 0)
	signer, err := NewOAuthStateSigner(testEncodedKey(4))
	if err != nil {
		t.Fatalf("NewOAuthStateSigner() error = %v", err)
	}
	state, created, err := signer.Create("aoy", "user-1", "https://nexflow-aoy.nextstep-soft.com/settings/shopee-connections", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	verified, err := signer.Verify(state, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified != created {
		t.Fatalf("verified = %+v, created = %+v", verified, created)
	}
	if _, err := signer.Verify(state, now.Add(oauthStateTTL+time.Second)); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("expired Verify() error = %v", err)
	}
}

func TestOAuthStateRejectsTampering(t *testing.T) {
	signer, _ := NewOAuthStateSigner(testEncodedKey(4))
	state, _, _ := signer.Create("demo", "user-1", "https://nexflow.nextstep-soft.com/settings/shopee-connections", time.Now())
	tampered := strings.Replace(state, "a", "b", 1)
	if tampered == state {
		tampered += "x"
	}
	if _, err := signer.Verify(tampered, time.Now()); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestValidateTenantReturnURLRejectsCrossTenantRedirect(t *testing.T) {
	if err := ValidateTenantReturnURL(
		"https://nexflow-aoy.nextstep-soft.com",
		"https://nexflow.nextstep-soft.com/settings/shopee-connections",
	); err == nil {
		t.Fatal("expected cross-tenant redirect to be rejected")
	}
}
