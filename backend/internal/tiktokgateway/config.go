package tiktokgateway

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultTikTokShopBaseURL = "https://open-api.tiktokglobalshop.com"

// Config contains only central-gateway settings. TikTok app credentials and
// seller tokens must never be configured in, or exposed by, a tenant service.
type Config struct {
	Port                string
	DatabaseURL         string
	PublicBaseURL       string
	TikTokShopBaseURL   string
	ServiceID           string
	AppKey              string
	AppSecret           string
	TokenEncryptionKey  string
	InternalMasterKey   string
	OAuthSigningKey     string
	TenantRegistryPath  string
	ExternalHTTPTimeout time.Duration
	TenantHTTPTimeout   time.Duration
}

func LoadConfig() (Config, error) {
	return loadConfig(os.Getenv)
}

func loadConfig(getenv func(string) string) (Config, error) {
	publicBaseURL, err := normalizePublicBaseURL(getenv("PUBLIC_BASE_URL"))
	if err != nil {
		return Config{}, fmt.Errorf("PUBLIC_BASE_URL: %w", err)
	}
	externalTimeout, err := parseDuration(getenv("TIKTOK_SHOP_GATEWAY_EXTERNAL_HTTP_TIMEOUT"), 20*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("TIKTOK_SHOP_GATEWAY_EXTERNAL_HTTP_TIMEOUT: %w", err)
	}
	tenantTimeout, err := parseDuration(getenv("TIKTOK_SHOP_GATEWAY_TENANT_HTTP_TIMEOUT"), 10*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("TIKTOK_SHOP_GATEWAY_TENANT_HTTP_TIMEOUT: %w", err)
	}
	cfg := Config{
		Port:                defaultValue(getenv("PORT"), "8092"),
		DatabaseURL:         strings.TrimSpace(getenv("DATABASE_URL")),
		PublicBaseURL:       publicBaseURL,
		TikTokShopBaseURL:   strings.TrimRight(defaultValue(getenv("TIKTOK_SHOP_GATEWAY_BASE_URL"), defaultTikTokShopBaseURL), "/"),
		ServiceID:           strings.TrimSpace(getenv("TIKTOK_SHOP_GATEWAY_SERVICE_ID")),
		AppKey:              strings.TrimSpace(getenv("TIKTOK_SHOP_GATEWAY_APP_KEY")),
		AppSecret:           strings.TrimSpace(getenv("TIKTOK_SHOP_GATEWAY_APP_SECRET")),
		TokenEncryptionKey:  strings.TrimSpace(getenv("TIKTOK_SHOP_GATEWAY_TOKEN_ENCRYPTION_KEY")),
		InternalMasterKey:   strings.TrimSpace(getenv("TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY")),
		OAuthSigningKey:     strings.TrimSpace(getenv("TIKTOK_SHOP_GATEWAY_OAUTH_SIGNING_KEY")),
		TenantRegistryPath:  defaultValue(getenv("TIKTOK_SHOP_GATEWAY_TENANT_REGISTRY"), "/app/config/nextstep-instances.json"),
		ExternalHTTPTimeout: externalTimeout,
		TenantHTTPTimeout:   tenantTimeout,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.AppKey == "" || c.AppSecret == "" {
		return errors.New("TikTok Shop app key and app secret are required")
	}
	if !serviceIDPattern.MatchString(c.ServiceID) {
		return errors.New("TIKTOK_SHOP_GATEWAY_SERVICE_ID must be a numeric service ID")
	}
	if err := validateHTTPSURL(c.TikTokShopBaseURL); err != nil {
		return fmt.Errorf("TIKTOK_SHOP_GATEWAY_BASE_URL: %w", err)
	}
	for name, value := range map[string]string{
		"TIKTOK_SHOP_GATEWAY_TOKEN_ENCRYPTION_KEY": c.TokenEncryptionKey,
		"TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY":  c.InternalMasterKey,
		"TIKTOK_SHOP_GATEWAY_OAUTH_SIGNING_KEY":    c.OAuthSigningKey,
	} {
		if err := validateKey(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if c.TokenEncryptionKey == c.InternalMasterKey || c.TokenEncryptionKey == c.OAuthSigningKey || c.InternalMasterKey == c.OAuthSigningKey {
		return errors.New("gateway encryption, internal auth, and OAuth signing keys must be different")
	}
	if strings.TrimSpace(c.TenantRegistryPath) == "" {
		return errors.New("TIKTOK_SHOP_GATEWAY_TENANT_REGISTRY is required")
	}
	return nil
}

func (c Config) OAuthCallbackURL() string {
	return strings.TrimRight(c.PublicBaseURL, "/") + "/api/tiktok-shop/callback"
}

func (c Config) WebhookCallbackURL() string {
	return strings.TrimRight(c.PublicBaseURL, "/") + "/webhook/tiktok-shop"
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, errors.New("must be a positive duration such as 10s")
	}
	return value, nil
}

func normalizePublicBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if err := validateHTTPSURL(value); err != nil {
		return "", err
	}
	return strings.TrimRight(value, "/"), nil
}

func validateHTTPSURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("must be an absolute HTTPS URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must not include query or fragment")
	}
	return nil
}

func validateKey(encoded string) error {
	if strings.TrimSpace(encoded) == "" {
		return errors.New("key is required")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return errors.New("key must be base64 encoded")
	}
	if len(key) != 32 {
		return fmt.Errorf("key must decode to 32 bytes, got %d", len(key))
	}
	return nil
}
