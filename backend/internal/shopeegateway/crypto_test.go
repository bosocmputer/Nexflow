package shopeegateway

import (
	"encoding/base64"
	"testing"
)

func testEncodedKey(seed byte) string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = seed + byte(i)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestTokenCipherRoundTripAndAADIsolation(t *testing.T) {
	cipher, err := NewTokenCipher(testEncodedKey(1))
	if err != nil {
		t.Fatalf("NewTokenCipher() error = %v", err)
	}
	ciphertext, nonce, err := cipher.Encrypt("access-token-value", []byte("aoy:123:access"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := cipher.Decrypt(ciphertext, nonce, []byte("aoy:123:access"))
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "access-token-value" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := cipher.Decrypt(ciphertext, nonce, []byte("demo:123:access")); err == nil {
		t.Fatal("expected decrypt with wrong tenant AAD to fail")
	}
}

func TestTokenCipherRejectsInvalidKeyLength(t *testing.T) {
	if _, err := NewTokenCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected invalid key length error")
	}
}

func TestDeriveTenantSecretIsStableAndTenantScoped(t *testing.T) {
	master := testEncodedKey(7)
	a, err := DeriveTenantSecret(master, "aoy")
	if err != nil {
		t.Fatalf("DeriveTenantSecret() error = %v", err)
	}
	again, _ := DeriveTenantSecret(master, "AOY")
	demo, _ := DeriveTenantSecret(master, "demo")
	if a != again {
		t.Fatalf("tenant secret is not stable: %q != %q", a, again)
	}
	if a == demo {
		t.Fatal("different tenants must not share derived secrets")
	}
}
