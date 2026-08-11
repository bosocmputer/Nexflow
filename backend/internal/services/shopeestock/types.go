package shopeestock

import (
	"time"

	"nexflow/internal/services/sml"
)

type LocationPair struct {
	Warehouse string `json:"warehouse"`
	Location  string `json:"location"`
}

type Settings struct {
	ShopID                      int64          `json:"shop_id"`
	ShopName                    string         `json:"shop_name"`
	ConnectionID                string         `json:"connection_id"`
	CredentialMode              string         `json:"credential_mode"`
	Enabled                     bool           `json:"enabled"`
	StockPct                    float64        `json:"stock_pct"`
	IntervalSeconds             int            `json:"interval_seconds"`
	ScopeMode                   string         `json:"scope_mode"`
	Locations                   []LocationPair `json:"locations"`
	AllScopeWarningAcknowledged bool           `json:"all_scope_warning_acknowledged"`
	DryRunRequired              bool           `json:"dry_run_required"`
	PausedReason                string         `json:"paused_reason"`
	LastCatalogSyncAt           *time.Time     `json:"last_catalog_sync_at,omitempty"`
	LastFullCatalogSyncAt       *time.Time     `json:"last_full_catalog_sync_at,omitempty"`
	LastCatalogAttemptAt        *time.Time     `json:"last_catalog_attempt_at,omitempty"`
	LastPreviewAt               *time.Time     `json:"last_preview_at,omitempty"`
	LastSyncAt                  *time.Time     `json:"last_sync_at,omitempty"`
	LastSuccessAt               *time.Time     `json:"last_success_at,omitempty"`
	LastError                   string         `json:"last_error,omitempty"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

type Location struct {
	WarehouseCode string `json:"warehouse_code"`
	WarehouseName string `json:"warehouse_name"`
	LocationCode  string `json:"location_code"`
	LocationName  string `json:"location_name"`
}

type LocationDiagnostic struct {
	Warehouse string  `json:"warehouse"`
	Location  string  `json:"location"`
	Balance   float64 `json:"balance_qty"`
	Code      string  `json:"code"`
}

type ProductRow struct {
	ShopID                     int64      `json:"shop_id"`
	ItemID                     int64      `json:"item_id"`
	ModelID                    int64      `json:"model_id"`
	ItemName                   string     `json:"item_name"`
	ModelName                  string     `json:"model_name"`
	ItemSKU                    string     `json:"item_sku"`
	ModelSKU                   string     `json:"model_sku"`
	ShopeeAvailable            int64      `json:"shopee_available"`
	ShopeeReserved             int64      `json:"shopee_reserved"`
	SMLItemCode                string     `json:"sml_item_code"`
	SMLItemName                string     `json:"sml_item_name"`
	SMLUnitCode                string     `json:"sml_unit_code"`
	SMLUnitName                string     `json:"sml_unit_name"`
	SMLBaseUnitCode            string     `json:"sml_base_unit_code"`
	SMLBaseUnitName            string     `json:"sml_base_unit_name"`
	UnitFactor                 float64    `json:"unit_factor"`
	ManualUnitFactor           *float64   `json:"manual_unit_factor,omitempty"`
	MatchSource                string     `json:"match_source"`
	MarketplaceAliasID         string     `json:"marketplace_alias_id,omitempty"`
	MarketplaceAliasUpdatedAt  *time.Time `json:"marketplace_alias_updated_at,omitempty"`
	Excluded                   bool       `json:"excluded"`
	WarningCodes               []string   `json:"warning_codes"`
	LastPreviewBalance         *float64   `json:"last_preview_balance,omitempty"`
	LastPreviewExcludedBalance *float64   `json:"last_preview_excluded_balance,omitempty"`
	LastPreviewMinQty          *float64   `json:"last_preview_min_qty,omitempty"`
	LastPreviewMaxQty          *float64   `json:"last_preview_max_qty,omitempty"`
	LastPreviewTarget          *int64     `json:"last_preview_target,omitempty"`
	LastSuccessTarget          *int64     `json:"last_success_target,omitempty"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type ProductCounts struct {
	Ready    int `json:"ready"`
	Fix      int `json:"fix"`
	Excluded int `json:"excluded"`
}

type Run struct {
	ID            string     `json:"id"`
	ShopID        int64      `json:"shop_id"`
	RunType       string     `json:"run_type"`
	TriggerSource string     `json:"trigger_source"`
	Status        string     `json:"status"`
	AsOfDate      string     `json:"as_of_date,omitempty"`
	TotalCount    int        `json:"total_count"`
	ChangedCount  int        `json:"changed_count"`
	SkippedCount  int        `json:"skipped_count"`
	BlockedCount  int        `json:"blocked_count"`
	ErrorCount    int        `json:"error_count"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type Overview struct {
	Available        bool                 `json:"available"`
	AvailabilityCode string               `json:"availability_code,omitempty"`
	AvailabilityText string               `json:"availability_text,omitempty"`
	GatewayMode      bool                 `json:"gateway_mode"`
	Settings         []Settings           `json:"settings"`
	Locations        []Location           `json:"locations"`
	Diagnostics      []LocationDiagnostic `json:"diagnostics"`
	Products         []ProductRow         `json:"products"`
	ProductsTotal    int                  `json:"products_total"`
	ProductCounts    ProductCounts        `json:"product_counts"`
	ProductsPage     int                  `json:"products_page"`
	ProductsSize     int                  `json:"products_size"`
	Runs             []Run                `json:"runs"`
	CheckedAt        string               `json:"checked_at"`
}

type ProductFilter struct {
	Status string
	Query  string
	Page   int
	Size   int
}

type MappingUpdate struct {
	SMLItemCode               string     `json:"sml_item_code"`
	SMLUnitCode               string     `json:"sml_unit_code"`
	ManualUnitFactor          *float64   `json:"manual_unit_factor,omitempty"`
	Excluded                  bool       `json:"excluded"`
	UpdatedAt                 time.Time  `json:"updated_at"`
	MarketplaceAliasID        string     `json:"marketplace_alias_id,omitempty"`
	MarketplaceAliasUpdatedAt *time.Time `json:"marketplace_alias_updated_at,omitempty"`
}

type CatalogDue struct {
	ShopID int64
	Full   bool
}

type CatalogOption struct {
	ItemCode     string                 `json:"item_code"`
	ItemName     string                 `json:"item_name"`
	StandardUnit string                 `json:"standard_unit"`
	Units        []sml.StockCatalogUnit `json:"units"`
}

type SettingsUpdate struct {
	Enabled                     bool           `json:"enabled"`
	StockPct                    float64        `json:"stock_pct"`
	IntervalSeconds             int            `json:"interval_seconds"`
	ScopeMode                   string         `json:"scope_mode"`
	Locations                   []LocationPair `json:"locations"`
	AcknowledgeAllScopeWarnings bool           `json:"acknowledge_all_scope_warnings"`
}

type PreviewLine struct {
	ItemID          int64    `json:"item_id"`
	ModelID         int64    `json:"model_id"`
	SMLItemCode     string   `json:"sml_item_code"`
	ScopeBalance    float64  `json:"scope_balance"`
	ExcludedBalance float64  `json:"excluded_balance"`
	MinQty          float64  `json:"min_qty"`
	MaxQty          float64  `json:"max_qty"`
	UnitFactor      float64  `json:"unit_factor"`
	CurrentStock    int64    `json:"current_stock"`
	ReservedStock   int64    `json:"reserved_stock"`
	TargetStock     int64    `json:"target_stock"`
	Changed         bool     `json:"changed"`
	Blocked         bool     `json:"blocked"`
	WarningCodes    []string `json:"warning_codes"`
}

type PreviewResult struct {
	RunID           string        `json:"run_id"`
	ShopID          int64         `json:"shop_id"`
	AsOfDate        string        `json:"as_of_date"`
	TotalCount      int           `json:"total_count"`
	ChangedCount    int           `json:"changed_count"`
	SkippedCount    int           `json:"skipped_count"`
	BlockedCount    int           `json:"blocked_count"`
	ExcludedBalance float64       `json:"excluded_balance"`
	CircuitBreaker  string        `json:"circuit_breaker,omitempty"`
	Lines           []PreviewLine `json:"lines"`
}

type SyncResult struct {
	RunID        string `json:"run_id"`
	ShopID       int64  `json:"shop_id"`
	ChangedCount int    `json:"changed_count"`
	BlockedCount int    `json:"blocked_count"`
	ErrorCount   int    `json:"error_count"`
	UnknownCount int    `json:"unknown_count"`
}
