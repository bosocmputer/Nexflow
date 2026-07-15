package shopeegateway

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"nexflow/internal/services/shopeeapi"
)

type Config struct {
	Port                string
	Environment         string
	DatabaseURL         string
	PublicBaseURL       string
	ShopeeBaseURL       string
	ShopeePartnerID     int64
	ShopeePartnerKey    string
	PushSecret          string
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
	environment := defaultValue(getenv("SHOPEE_OPEN_API_ENV"), "live")
	baseURL := strings.TrimRight(strings.TrimSpace(getenv("SHOPEE_OPEN_API_BASE_URL")), "/")
	if baseURL == "" {
		if environment == "sandbox" {
			baseURL = shopeeapi.DefaultSandboxBaseURL
		} else {
			baseURL = shopeeapi.DefaultLiveBaseURL
		}
	}
	partnerID, err := strconv.ParseInt(strings.TrimSpace(getenv("SHOPEE_OPEN_API_PARTNER_ID")), 10, 64)
	if err != nil || partnerID <= 0 {
		return Config{}, errors.New("SHOPEE_OPEN_API_PARTNER_ID must be a positive integer")
	}
	externalTimeout, err := parseDuration(getenv("SHOPEE_GATEWAY_EXTERNAL_TIMEOUT"), 20*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("SHOPEE_GATEWAY_EXTERNAL_TIMEOUT: %w", err)
	}
	tenantTimeout, err := parseDuration(getenv("SHOPEE_GATEWAY_TENANT_TIMEOUT"), 10*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("SHOPEE_GATEWAY_TENANT_TIMEOUT: %w", err)
	}
	publicBaseURL, err := normalizePublicBaseURL(getenv("PUBLIC_BASE_URL"))
	if err != nil {
		return Config{}, fmt.Errorf("PUBLIC_BASE_URL: %w", err)
	}
	cfg := Config{
		Port:                defaultValue(getenv("PORT"), "8091"),
		Environment:         strings.ToLower(strings.TrimSpace(environment)),
		DatabaseURL:         strings.TrimSpace(getenv("DATABASE_URL")),
		PublicBaseURL:       publicBaseURL,
		ShopeeBaseURL:       baseURL,
		ShopeePartnerID:     partnerID,
		ShopeePartnerKey:    strings.TrimSpace(getenv("SHOPEE_OPEN_API_PARTNER_KEY")),
		PushSecret:          strings.TrimSpace(getenv("SHOPEE_GATEWAY_PUSH_SECRET")),
		TokenEncryptionKey:  strings.TrimSpace(getenv("SHOPEE_GATEWAY_TOKEN_ENCRYPTION_KEY")),
		InternalMasterKey:   strings.TrimSpace(getenv("SHOPEE_GATEWAY_INTERNAL_MASTER_KEY")),
		OAuthSigningKey:     strings.TrimSpace(getenv("SHOPEE_GATEWAY_OAUTH_SIGNING_KEY")),
		TenantRegistryPath:  defaultValue(getenv("SHOPEE_GATEWAY_TENANT_REGISTRY"), "/app/config/nextstep-instances.json"),
		ExternalHTTPTimeout: externalTimeout,
		TenantHTTPTimeout:   tenantTimeout,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Environment != "sandbox" && c.Environment != "live" {
		return errors.New("SHOPEE_OPEN_API_ENV must be sandbox or live")
	}
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.ShopeePartnerID <= 0 || c.ShopeePartnerKey == "" {
		return errors.New("Shopee Partner ID and Partner Key are required")
	}
	if c.Environment == "live" && c.PushSecret == "" {
		return errors.New("SHOPEE_GATEWAY_PUSH_SECRET is required in live mode")
	}
	for name, value := range map[string]string{
		"SHOPEE_GATEWAY_TOKEN_ENCRYPTION_KEY": c.TokenEncryptionKey,
		"SHOPEE_GATEWAY_INTERNAL_MASTER_KEY":  c.InternalMasterKey,
		"SHOPEE_GATEWAY_OAUTH_SIGNING_KEY":    c.OAuthSigningKey,
	} {
		if _, err := decodeKey(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if c.TokenEncryptionKey == c.InternalMasterKey || c.TokenEncryptionKey == c.OAuthSigningKey || c.InternalMasterKey == c.OAuthSigningKey {
		return errors.New("gateway encryption, internal auth, and OAuth signing keys must be different")
	}
	if strings.TrimSpace(c.TenantRegistryPath) == "" {
		return errors.New("SHOPEE_GATEWAY_TENANT_REGISTRY is required")
	}
	return nil
}

func (c Config) OAuthCallbackURL() string {
	return strings.TrimRight(c.PublicBaseURL, "/") + "/api/shopee/callback"
}

func (c Config) PushCallbackURL() string {
	return strings.TrimRight(c.PublicBaseURL, "/") + "/webhook/shopee"
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
