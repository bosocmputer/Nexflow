package shopeegateway

import (
	"testing"
)

func TestLoadConfigBuildsCentralCallbackURLs(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":                        "postgres://gateway:secret@postgres/gateway",
		"PUBLIC_BASE_URL":                     "https://shopee-gateway.nextstep-soft.com/",
		"SHOPEE_OPEN_API_ENV":                 "live",
		"SHOPEE_OPEN_API_PARTNER_ID":          "2034838",
		"SHOPEE_OPEN_API_PARTNER_KEY":         "partner-secret",
		"SHOPEE_GATEWAY_PUSH_SECRET":          "push-secret",
		"SHOPEE_GATEWAY_TOKEN_ENCRYPTION_KEY": testEncodedKey(1),
		"SHOPEE_GATEWAY_INTERNAL_MASTER_KEY":  testEncodedKey(2),
		"SHOPEE_GATEWAY_OAUTH_SIGNING_KEY":    testEncodedKey(3),
	}
	cfg, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.OAuthCallbackURL() != "https://shopee-gateway.nextstep-soft.com/api/shopee/callback" {
		t.Fatalf("callback = %q", cfg.OAuthCallbackURL())
	}
	if cfg.PushCallbackURL() != "https://shopee-gateway.nextstep-soft.com/webhook/shopee" {
		t.Fatalf("push callback = %q", cfg.PushCallbackURL())
	}
}

func TestLoadConfigRejectsReusedSecurityKeys(t *testing.T) {
	key := testEncodedKey(9)
	values := map[string]string{
		"DATABASE_URL":                        "postgres://gateway:secret@postgres/gateway",
		"PUBLIC_BASE_URL":                     "https://shopee-gateway.nextstep-soft.com",
		"SHOPEE_OPEN_API_ENV":                 "live",
		"SHOPEE_OPEN_API_PARTNER_ID":          "2034838",
		"SHOPEE_OPEN_API_PARTNER_KEY":         "partner-secret",
		"SHOPEE_GATEWAY_PUSH_SECRET":          "push-secret",
		"SHOPEE_GATEWAY_TOKEN_ENCRYPTION_KEY": key,
		"SHOPEE_GATEWAY_INTERNAL_MASTER_KEY":  key,
		"SHOPEE_GATEWAY_OAUTH_SIGNING_KEY":    testEncodedKey(3),
	}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected reused gateway key error")
	}
}
