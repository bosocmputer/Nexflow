package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	Port string
	Env  string

	// Database
	DatabaseURL string
	DBUser      string
	DBPassword  string

	// JWT
	JWTSecret      string
	JWTExpireHours int

	// LINE
	LineChannelSecret      string
	LineChannelAccessToken string
	LineAdminUserID        string
	// LineGreeting is sent as a one-time auto-reply on a customer's FIRST
	// LINE message. Empty string = no greeting (default). Set this to e.g.
	// "ขอบคุณค่ะ ทางร้านจะติดต่อกลับเร็ว ๆ นี้นะคะ 🙏" if you want one.
	LineGreeting string

	// PublicBaseURL is the externally reachable HTTPS URL of this backend
	// (e.g. the Cloudflare Quick Tunnel URL). Used when constructing
	// originalContentUrl + previewImageUrl for LINE Push image messages —
	// LINE's servers fetch these URLs to deliver media to customers, so they
	// MUST be reachable from the public internet. Empty = admin can't send
	// images yet (UI hides the attach feature with a tooltip).
	PublicBaseURL string

	// MediaSigningKey signs short-lived public media URLs (HMAC-SHA256).
	// Defaults to JWT_SECRET when empty so single-secret deployments work.
	MediaSigningKey string

	// IMAP — moved to imap_accounts table (DB-driven, multi-account).
	// Manage via /settings/email instead of env vars.

	// OpenRouter
	OpenRouterAPIKey     string
	OpenRouterModel      string
	OpenRouterFallback   string
	OpenRouterAudioModel string
	OpenRouterAppTitle   string
	OpenRouterAppReferer string

	// Mistral
	MistralAPIKey string

	// Shopee SML (REST API — saleinvoice).
	// CustCode moved to channel_defaults table — manage via /settings/channels.
	ShopeeSMLURL        string
	ShopeeSMLGUID       string
	ShopeeSMLProvider   string
	ShopeeSMLConfigFile string
	ShopeeSMLDatabase   string
	ShopeeSMLDocFormat  string
	ShopeeSMLSaleCode   string
	ShopeeSMLBranchCode string
	ShopeeSMLWHCode     string
	ShopeeSMLShelfCode  string
	ShopeeSMLUnitCode   string
	ShopeeSMLVATType    int
	ShopeeSMLVATRate    float64
	ShopeeSMLDocTime    string

	// Shopee shipped email → SML purchaseorder.
	// Reuses all SHOPEE_SML_* fields above; only doc_format differs.
	ShippedSMLDocFormat string

	// Gemini (for text-embedding-004)
	GeminiAPIKey string

	// Shopee email detection moved to per-account `shopee_domains` column
	// in imap_accounts table.

	// Shopee Open API (direct order sync). Keep sandbox/live isolated by
	// environment and base URL; tokens live in shopee_api_connections.
	ShopeeOpenAPIEnabled              bool
	ShopeeOpenAPIEnv                  string
	ShopeeOpenAPIBaseURL              string
	ShopeeOpenAPIPartnerID            int64
	ShopeeOpenAPIPartnerKey           string
	ShopeeOpenAPIRedirect             string
	ShopeeRealtimeOpsEnabled          bool
	ShopeeAdvancedDropoffEnabled      bool
	ShopeeShippingActionsEnabled      bool
	ShopeeCancelAfterSMLAlertsEnabled bool
	ShopeeSMLCancelDocumentsEnabled   bool
	ShopeeRealtimeWebhookSecret       string
	ShopeeRealtimeSyncIntervalSeconds int

	// Auto-confirm
	AutoConfirmThreshold float64

	// Cron
	InsightCronHour       int
	BackupCronHour        int
	InsightLineNotify     bool
	DiskWarnPercent       int
	DataLifecycleEnabled  bool
	DataLifecycleCronHour int
	HotLogDays            int
	AutoArchiveDays       int
	SummaryRetentionDays  int
	PurgeBatchSize        int

	// Artifacts (original source files attached to each bill)
	ArtifactsDir      string
	ArtifactsMaxBytes int64
}

func Load() *Config {
	// Load .env if present (ignore error — production uses OS env)
	_ = godotenv.Load()

	c := &Config{
		Port:                              getEnv("PORT", "8090"),
		Env:                               getEnv("ENV", "development"),
		DatabaseURL:                       getEnv("DATABASE_URL", ""),
		DBUser:                            getEnv("DB_USER", "nexflow"),
		DBPassword:                        getEnv("DB_PASSWORD", "changeme"),
		JWTSecret:                         getEnv("JWT_SECRET", ""),
		JWTExpireHours:                    getEnvInt("JWT_EXPIRE_HOURS", 24),
		LineChannelSecret:                 getEnv("LINE_CHANNEL_SECRET", ""),
		LineChannelAccessToken:            getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
		LineAdminUserID:                   getEnv("LINE_ADMIN_USER_ID", ""),
		LineGreeting:                      getEnv("LINE_GREETING", ""),
		PublicBaseURL:                     getEnv("PUBLIC_BASE_URL", ""),
		MediaSigningKey:                   getEnv("MEDIA_SIGNING_KEY", ""),
		OpenRouterAPIKey:                  getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:                   getEnv("OPENROUTER_MODEL", "google/gemini-2.5-flash"),
		OpenRouterFallback:                getEnv("OPENROUTER_FALLBACK_MODEL", "google/gemini-flash-1.5"),
		OpenRouterAudioModel:              getEnv("OPENROUTER_AUDIO_MODEL", "openai/whisper-1"),
		OpenRouterAppTitle:                getEnv("OPENROUTER_APP_TITLE", "Nexflow"),
		OpenRouterAppReferer:              getEnv("OPENROUTER_APP_REFERER", getEnv("PUBLIC_BASE_URL", "")),
		MistralAPIKey:                     getEnv("MISTRAL_API_KEY", ""),
		ShopeeSMLURL:                      getEnv("SHOPEE_SML_URL", "http://192.168.2.248:8080"),
		ShopeeSMLGUID:                     getEnv("SHOPEE_SML_GUID", "SMLX"),
		ShopeeSMLProvider:                 getEnv("SHOPEE_SML_PROVIDER", "SML1"),
		ShopeeSMLConfigFile:               getEnv("SHOPEE_SML_CONFIG_FILE", "SMLConfigSML1.xml"),
		ShopeeSMLDatabase:                 getEnv("SHOPEE_SML_DATABASE", "SMLPLOY"),
		ShopeeSMLDocFormat:                getEnv("SHOPEE_SML_DOC_FORMAT", ""),
		ShopeeSMLSaleCode:                 getEnv("SHOPEE_SML_SALE_CODE", ""),
		ShopeeSMLBranchCode:               getEnv("SHOPEE_SML_BRANCH_CODE", ""),
		ShopeeSMLWHCode:                   getEnv("SHOPEE_SML_WH_CODE", ""),
		ShopeeSMLShelfCode:                getEnv("SHOPEE_SML_SHELF_CODE", ""),
		ShopeeSMLUnitCode:                 getEnv("SHOPEE_SML_UNIT_CODE", ""),
		ShopeeSMLVATType:                  getEnvInt("SHOPEE_SML_VAT_TYPE", -1),
		ShopeeSMLVATRate:                  getEnvFloat("SHOPEE_SML_VAT_RATE", -1),
		ShopeeSMLDocTime:                  getEnv("SHOPEE_SML_DOC_TIME", ""),
		ShippedSMLDocFormat:               getEnv("SHIPPED_SML_DOC_FORMAT", ""),
		GeminiAPIKey:                      getEnv("GEMINI_API_KEY", ""),
		ShopeeOpenAPIEnabled:              getEnvBool("SHOPEE_OPEN_API_ENABLED", false),
		ShopeeOpenAPIEnv:                  getEnv("SHOPEE_OPEN_API_ENV", "sandbox"),
		ShopeeOpenAPIBaseURL:              getEnv("SHOPEE_OPEN_API_BASE_URL", "https://openplatform.sandbox.test-stable.shopee.sg"),
		ShopeeOpenAPIPartnerID:            getEnvInt64("SHOPEE_OPEN_API_PARTNER_ID", 0),
		ShopeeOpenAPIPartnerKey:           getEnv("SHOPEE_OPEN_API_PARTNER_KEY", ""),
		ShopeeOpenAPIRedirect:             getEnv("SHOPEE_OPEN_API_REDIRECT_URL", ""),
		ShopeeRealtimeOpsEnabled:          getEnvBool("ENABLE_SHOPEE_REALTIME_OPS", false),
		ShopeeAdvancedDropoffEnabled:      getEnvBool("ENABLE_SHOPEE_ADVANCED_DROPOFF", false),
		ShopeeShippingActionsEnabled:      getEnvBool("ENABLE_SHOPEE_SHIPPING_ACTIONS", false),
		ShopeeCancelAfterSMLAlertsEnabled: getEnvBool("ENABLE_SHOPEE_CANCEL_AFTER_SML_ALERTS", true),
		ShopeeSMLCancelDocumentsEnabled:   getEnvBool("ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS", false),
		ShopeeRealtimeWebhookSecret:       getEnv("SHOPEE_REALTIME_WEBHOOK_SECRET", ""),
		ShopeeRealtimeSyncIntervalSeconds: getEnvInt("SHOPEE_REALTIME_SYNC_INTERVAL_SECONDS", 0),
		AutoConfirmThreshold:              getEnvFloat("AUTO_CONFIRM_THRESHOLD", 0.85),
		InsightCronHour:                   getEnvInt("INSIGHT_CRON_HOUR", 8),
		BackupCronHour:                    getEnvInt("BACKUP_CRON_HOUR", 0),
		InsightLineNotify:                 getEnvBool("INSIGHT_LINE_NOTIFY", true),
		DiskWarnPercent:                   getEnvInt("DISK_WARN_PERCENT", 90),
		DataLifecycleEnabled:              getEnvBool("DATA_LIFECYCLE_ENABLED", true),
		DataLifecycleCronHour:             getEnvInt("DATA_LIFECYCLE_CRON_HOUR", 2),
		HotLogDays:                        getEnvInt("HOT_LOG_DAYS", 90),
		AutoArchiveDays:                   getEnvInt("AUTO_ARCHIVE_DAYS", 180),
		SummaryRetentionDays:              getEnvInt("SUMMARY_RETENTION_DAYS", 730),
		PurgeBatchSize:                    getEnvInt("PURGE_BATCH_SIZE", 1000),
		ArtifactsDir:                      getEnv("ARTIFACTS_DIR", "/app/artifacts"),
		ArtifactsMaxBytes:                 int64(getEnvInt("ARTIFACTS_MAX_BYTES", 10*1024*1024)), // 10 MB
	}

	if c.JWTSecret == "" {
		log.Fatal("JWT_SECRET must be set (min 32 chars)")
	}
	if len(c.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must be at least 32 characters")
	}
	if c.DatabaseURL == "" {
		log.Fatal("DATABASE_URL must be set")
	}

	return c
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
