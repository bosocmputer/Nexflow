package tiktokshop

import (
	"net/url"
	"testing"
)

func TestSignRequestMatchesStableTikTokShopVector(t *testing.T) {
	query := url.Values{
		"timestamp":    {"1710000000"},
		"app_key":      {"app-key"},
		"sign":         {"ignored"},
		"access_token": {"ignored"},
	}
	body := []byte(`{"event_type":"ORDER_STATUS_CHANGE","address":"https://gateway.example/webhooks/tiktok-shop"}`)

	sign, err := SignRequest("app-secret", "/event/202309/webhooks", query, body, false)
	if err != nil {
		t.Fatalf("SignRequest() error = %v", err)
	}
	const want = "99d03cc414957c997b9e9a3ef0b7055e984707746562fc0d3ab5598e22b4c985"
	if sign != want {
		t.Fatalf("SignRequest() = %q, want %q", sign, want)
	}
}

func TestSignRequestUsesExactJSONBytes(t *testing.T) {
	query := url.Values{"app_key": {"app-key"}, "timestamp": {"1710000000"}}
	compact, err := SignRequest("app-secret", "/event/202309/webhooks", query, []byte(`{"a":1,"b":2}`), false)
	if err != nil {
		t.Fatalf("SignRequest(compact) error = %v", err)
	}
	spaced, err := SignRequest("app-secret", "/event/202309/webhooks", query, []byte(`{"a": 1, "b": 2}`), false)
	if err != nil {
		t.Fatalf("SignRequest(spaced) error = %v", err)
	}
	if compact == spaced {
		t.Fatal("SignRequest() must sign the exact request body bytes")
	}
}

func TestSignRequestRejectsAmbiguousParameters(t *testing.T) {
	query := url.Values{"app_key": {"first", "second"}, "timestamp": {"1710000000"}}
	if _, err := SignRequest("app-secret", "/authorization/202309/shops", query, nil, false); err == nil {
		t.Fatal("SignRequest() error = nil, want error for repeated parameter")
	}
}

func TestVerifyWebhookSignatureAcceptsOnlyTikTokSignature(t *testing.T) {
	body := []byte(`{"tts_notification_id":"event-1","shop_id":"123"}`)
	const signature = "8752dd907e24b7665f7a6fe7e6cfc9132d6e93318ed0357ca931fbbbe5ebd789"

	if err := VerifyWebhookSignature("app-key", "app-secret", signature, body); err != nil {
		t.Fatalf("VerifyWebhookSignature() error = %v", err)
	}
	if err := VerifyWebhookSignature("app-key", "app-secret", "Bearer "+signature, body); err == nil {
		t.Fatal("VerifyWebhookSignature() accepted a non-TikTok Authorization header")
	}
	if err := VerifyWebhookSignature("app-key", "app-secret", signature, []byte(`{"tts_notification_id":"event-2","shop_id":"123"}`)); err == nil {
		t.Fatal("VerifyWebhookSignature() accepted a signature for a different body")
	}
}
