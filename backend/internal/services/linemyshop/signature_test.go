package linemyshop

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"orderNumber":"LMS-1"}`)
	secret := "secret-token"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !VerifySignature(secret, body, sig) {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature(secret, []byte(`{"orderNumber":"tampered"}`), sig) {
		t.Fatal("tampered body accepted")
	}
	if VerifySignature("", body, sig) {
		t.Fatal("empty secret accepted")
	}
	if VerifySignature(secret, body, "") {
		t.Fatal("empty signature accepted")
	}
}
