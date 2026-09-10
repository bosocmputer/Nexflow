package tiktokgateway

import "testing"

func TestTokenCipherRoundTripIsTenantAndShopScoped(t *testing.T) {
	cipher, err := NewTokenCipher(testEncodedKey(7))
	if err != nil {
		t.Fatalf("NewTokenCipher() error = %v", err)
	}
	aad := tokenAAD("aoy", "749000000000000001", "access")
	ciphertext, nonce, err := cipher.Encrypt("access-token", aad)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := cipher.Decrypt(ciphertext, nonce, aad)
	if err != nil || plaintext != "access-token" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
	if _, err := cipher.Decrypt(ciphertext, nonce, tokenAAD("demo", "749000000000000001", "access")); err == nil {
		t.Fatal("expected cross-tenant decryption to fail")
	}
	if _, err := cipher.Decrypt(ciphertext, nonce, tokenAAD("aoy", "749000000000000002", "access")); err == nil {
		t.Fatal("expected cross-shop decryption to fail")
	}
}

func TestDeriveTenantSecretIsStableAndTenantScoped(t *testing.T) {
	master := testEncodedKey(8)
	aoy, err := DeriveTenantSecret(master, "AOY")
	if err != nil {
		t.Fatalf("DeriveTenantSecret() error = %v", err)
	}
	aoyAgain, _ := DeriveTenantSecret(master, "aoy")
	demo, _ := DeriveTenantSecret(master, "demo")
	if aoy != aoyAgain || aoy == demo {
		t.Fatalf("tenant secrets are not scoped: aoy=%q again=%q demo=%q", aoy, aoyAgain, demo)
	}
}
