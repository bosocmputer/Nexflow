package tiktokgateway

import (
	"encoding/base64"
	"testing"
)

func TestLoadConfigBuildsTikTokShopGatewayCallbacks(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":                              "postgres://gateway:secret@postgres/tiktok_gateway",
		"PUBLIC_BASE_URL":                           "https://tiktok-shop-gateway.nextstep-soft.com/",
		"TIKTOK_SHOP_GATEWAY_SERVICE_ID":            "7683174272727025429",
		"TIKTOK_SHOP_GATEWAY_APP_KEY":               "app-key",
		"TIKTOK_SHOP_GATEWAY_APP_SECRET":            "app-secret",
		"TIKTOK_SHOP_GATEWAY_TOKEN_ENCRYPTION_KEY":  testEncodedKey(1),
		"TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY":   testEncodedKey(2),
		"TIKTOK_SHOP_GATEWAY_OAUTH_SIGNING_KEY":     testEncodedKey(3),
		"TIKTOK_SHOP_GATEWAY_TENANT_REGISTRY":       "/app/config/nextstep-instances.json",
		"TIKTOK_SHOP_GATEWAY_EXTERNAL_HTTP_TIMEOUT": "20s",
		"TIKTOK_SHOP_GATEWAY_TENANT_HTTP_TIMEOUT":   "10s",
		"TIKTOK_SHOP_API_BASE_URL":                  "https://open-api.tiktokglobalshop.com",
	}
	cfg, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.OAuthCallbackURL() != "https://tiktok-shop-gateway.nextstep-soft.com/api/tiktok-shop/callback" {
		t.Fatalf("OAuthCallbackURL() = %q", cfg.OAuthCallbackURL())
	}
	if cfg.WebhookCallbackURL() != "https://tiktok-shop-gateway.nextstep-soft.com/webhook/tiktok-shop" {
		t.Fatalf("WebhookCallbackURL() = %q", cfg.WebhookCallbackURL())
	}
	if cfg.TikTokShopBaseURL != defaultTikTokShopBaseURL {
		t.Fatalf("TikTokShopBaseURL = %q", cfg.TikTokShopBaseURL)
	}
	if cfg.ServiceID != "7683174272727025429" {
		t.Fatalf("ServiceID = %q", cfg.ServiceID)
	}
}

func TestLoadConfigRejectsReusedSecurityKeys(t *testing.T) {
	key := testEncodedKey(9)
	values := map[string]string{
		"DATABASE_URL":                             "postgres://gateway:secret@postgres/tiktok_gateway",
		"PUBLIC_BASE_URL":                          "https://tiktok-shop-gateway.nextstep-soft.com",
		"TIKTOK_SHOP_GATEWAY_SERVICE_ID":           "7683174272727025429",
		"TIKTOK_SHOP_GATEWAY_APP_KEY":              "app-key",
		"TIKTOK_SHOP_GATEWAY_APP_SECRET":           "app-secret",
		"TIKTOK_SHOP_GATEWAY_TOKEN_ENCRYPTION_KEY": key,
		"TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY":  key,
		"TIKTOK_SHOP_GATEWAY_OAUTH_SIGNING_KEY":    testEncodedKey(3),
	}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected reused gateway key error")
	}
}

func TestLoadConfigRequiresTikTokShopServiceID(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":                             "postgres://gateway:secret@postgres/tiktok_gateway",
		"PUBLIC_BASE_URL":                          "https://tiktok-shop-gateway.nextstep-soft.com",
		"TIKTOK_SHOP_GATEWAY_APP_KEY":              "app-key",
		"TIKTOK_SHOP_GATEWAY_APP_SECRET":           "app-secret",
		"TIKTOK_SHOP_GATEWAY_TOKEN_ENCRYPTION_KEY": testEncodedKey(1),
		"TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY":  testEncodedKey(2),
		"TIKTOK_SHOP_GATEWAY_OAUTH_SIGNING_KEY":    testEncodedKey(3),
	}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected missing TikTok Shop Service ID error")
	}
}

func testEncodedKey(seed byte) string {
	key := make([]byte, 32)
	for index := range key {
		key[index] = seed
	}
	return base64.StdEncoding.EncodeToString(key)
}
