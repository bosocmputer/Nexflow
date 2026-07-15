package shopeegateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

type TokenCipher struct {
	aead cipher.AEAD
}

func NewTokenCipher(encodedKey string) (*TokenCipher, error) {
	key, err := decodeKey(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("token encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &TokenCipher{aead: aead}, nil
}

func (c *TokenCipher) Encrypt(plaintext string, aad []byte) (ciphertext, nonce []byte, err error) {
	if c == nil || c.aead == nil {
		return nil, nil, errors.New("token cipher is not configured")
	}
	if strings.TrimSpace(plaintext) == "" {
		return nil, nil, errors.New("token is empty")
	}
	nonce = make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate token nonce: %w", err)
	}
	ciphertext = c.aead.Seal(nil, nonce, []byte(plaintext), aad)
	return ciphertext, nonce, nil
}

func (c *TokenCipher) Decrypt(ciphertext, nonce, aad []byte) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("token cipher is not configured")
	}
	if len(nonce) != c.aead.NonceSize() || len(ciphertext) == 0 {
		return "", errors.New("encrypted token is invalid")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", errors.New("decrypt token failed")
	}
	return string(plaintext), nil
}

func DeriveTenantSecret(encodedMasterKey, tenant string) (string, error) {
	master, err := decodeKey(encodedMasterKey)
	if err != nil {
		return "", fmt.Errorf("gateway internal master key: %w", err)
	}
	tenant = strings.ToLower(strings.TrimSpace(tenant))
	if tenant == "" {
		return "", errors.New("tenant is required")
	}
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("nexflow-shopee-gateway/tenant/" + tenant))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func decodeKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("key is required")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("key must be base64 encoded")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}
