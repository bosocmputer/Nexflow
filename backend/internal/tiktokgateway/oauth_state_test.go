package tiktokgateway

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOAuthStateIsSignedAndExpires(t *testing.T) {
	now := time.Unix(1784070000, 0)
	signer, err := NewOAuthStateSigner(testEncodedKey(4))
	if err != nil {
		t.Fatalf("NewOAuthStateSigner() error = %v", err)
	}
	state, created, err := signer.Create("aoy", "user-1", "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	verified, err := signer.Verify(state, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.Nonce != created.Nonce || verified.ExpiresAt != created.ExpiresAt {
		t.Fatalf("verified = %+v, created = %+v", verified, created)
	}
	if created.Tenant != "aoy" || created.UserID != "user-1" || created.ReturnURL != "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop" {
		t.Fatalf("server-stored claims = %+v", created)
	}
	if _, err := signer.Verify(state, now.Add(oauthStateTTL+time.Second)); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("expired Verify() error = %v", err)
	}
}

func TestOAuthStateRejectsTampering(t *testing.T) {
	now := time.Unix(1784070000, 0)
	signer, err := NewOAuthStateSigner(testEncodedKey(4))
	if err != nil {
		t.Fatalf("NewOAuthStateSigner() error = %v", err)
	}
	state, _, err := signer.Create("aoy", "user-1", "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tampered := "A" + state[1:]
	if tampered == state {
		tampered = "B" + state[1:]
	}
	if _, err := signer.Verify(tampered, now); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("tampered Verify() error = %v", err)
	}
}

func TestOAuthStateLengthDoesNotGrowWithServerStoredMetadata(t *testing.T) {
	now := time.Unix(1784070000, 0)
	signer, err := NewOAuthStateSigner(testEncodedKey(4))
	if err != nil {
		t.Fatalf("NewOAuthStateSigner() error = %v", err)
	}
	returnURL := "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop?connected=1&context=" + strings.Repeat("x", 2048)
	state, created, err := signer.Create("aoy", strings.Repeat("user", 64), returnURL, now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(state) > 128 {
		t.Fatalf("state length = %d, want <= 128", len(state))
	}
	if created.UserID == "" || created.ReturnURL != returnURL {
		t.Fatalf("server-stored claims = %+v", created)
	}
}

func TestValidateTenantReturnURLRejectsCrossTenantRedirect(t *testing.T) {
	if err := ValidateTenantReturnURL(
		"https://nexflow-aoy.nextstep-soft.com",
		"https://nexflow.nextstep-soft.com/settings/tiktok-shop",
	); err == nil {
		t.Fatal("expected cross-tenant redirect to be rejected")
	}
}
