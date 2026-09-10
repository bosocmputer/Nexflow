package tiktokshop

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
)

var (
	ErrInvalidSigningInput     = errors.New("invalid TikTok Shop signing input")
	ErrInvalidWebhookSignature = errors.New("invalid TikTok Shop webhook signature")
)

// SignRequest generates the signature required by TikTok Shop business APIs.
// The caller must send body unchanged after this function returns: non-multipart
// bodies are part of TikTok's signature input byte-for-byte.
func SignRequest(appSecret, path string, query url.Values, body []byte, multipart bool) (string, error) {
	appSecret = strings.TrimSpace(appSecret)
	path = strings.TrimSpace(path)
	if appSecret == "" || !strings.HasPrefix(path, "/") {
		return "", ErrInvalidSigningInput
	}

	keys := make([]string, 0, len(query))
	for key, values := range query {
		if key == "sign" || key == "access_token" {
			continue
		}
		if len(values) != 1 {
			return "", ErrInvalidSigningInput
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var input strings.Builder
	input.Grow(len(path) + len(appSecret)*2 + len(body))
	input.WriteString(appSecret)
	input.WriteString(path)
	for _, key := range keys {
		input.WriteString(key)
		input.WriteString(query.Get(key))
	}
	if !multipart {
		input.Write(body)
	}
	input.WriteString(appSecret)

	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write([]byte(input.String()))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyWebhookSignature verifies the Authorization signature TikTok Shop adds
// to webhook deliveries. TikTok signs app_key + raw request body, so callers
// must invoke it before parsing or re-serializing the body.
func VerifyWebhookSignature(appKey, appSecret, authorization string, rawBody []byte) error {
	appKey = strings.TrimSpace(appKey)
	appSecret = strings.TrimSpace(appSecret)
	authorization = strings.TrimSpace(authorization)
	if appKey == "" || appSecret == "" || authorization == "" || authorization != strings.ToLower(authorization) || len(authorization) != sha256.Size*2 {
		return ErrInvalidWebhookSignature
	}
	if _, err := hex.DecodeString(authorization); err != nil {
		return ErrInvalidWebhookSignature
	}

	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write([]byte(appKey))
	_, _ = mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(authorization), []byte(expected)) != 1 {
		return ErrInvalidWebhookSignature
	}
	return nil
}
