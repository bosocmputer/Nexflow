package models

import "time"

// MarketplaceItemAlias maps a marketplace raw_name / source_sku to an SML item_code.
// One row per (source, source_sku) or (source, normalized_key).
type MarketplaceItemAlias struct {
	ID                    string     `json:"id"`
	Source                string     `json:"source"`
	AccountKey            string     `json:"account_key"`
	AccountName           string     `json:"account_name,omitempty"`
	ExternalItemID        string     `json:"external_item_id"`
	ExternalVariantID     string     `json:"external_variant_id"`
	ExternalParentID      string     `json:"external_parent_id"`
	ParentKey             string     `json:"parent_key"`
	ParentKeyKind         string     `json:"parent_key_kind"`
	SourceProductName     string     `json:"source_product_name"`
	SourceVariantName     string     `json:"source_variant_name"`
	SourceSKU             string     `json:"source_sku"`
	RawName               string     `json:"raw_name"`
	NormalizedKey         string     `json:"normalized_key"`
	ItemCode              string     `json:"item_code"`
	UnitCode              string     `json:"unit_code"`
	Confidence            float64    `json:"confidence"`
	ConfirmedBy           *string    `json:"confirmed_by,omitempty"`
	UsageCount            int        `json:"usage_count"`
	LastUsedAt            *time.Time `json:"last_used_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	IsActive              bool       `json:"is_active"`
	MatchMethod           string     `json:"match_method"`
	ScopeConfirmed        bool       `json:"scope_confirmed"`
	MappingRevision       int64      `json:"mapping_revision"`
	MetadataUpdatedAt     *time.Time `json:"metadata_updated_at,omitempty"`
	QuantityMultiplier    int64      `json:"quantity_multiplier"`
	UnitStandValue        *string    `json:"unit_stand_value,omitempty"`
	UnitDivideValue       *string    `json:"unit_divide_value,omitempty"`
	UnitCatalogGeneration *string    `json:"unit_catalog_generation,omitempty"`
	ConversionStatus      string     `json:"conversion_status"`
	SalesEnabled          bool       `json:"sales_enabled"`
	StockPolicy           string     `json:"stock_policy"`
	ItemName              string     `json:"item_name,omitempty"`
	ConfirmedName         string     `json:"confirmed_name,omitempty"`
	ProductActive         bool       `json:"product_active"`
	OpenItemCount         int        `json:"open_item_count"`
	StockMappingCount     int        `json:"stock_mapping_count"`
}

// MarketplaceAliasReviewGroup groups unmatched bill items by their normalized key
// for bulk review in the admin UI.
type MarketplaceAliasReviewGroup struct {
	GroupKey          string `json:"group_key"`
	Source            string `json:"source"`
	AccountKey        string `json:"account_key"`
	AccountName       string `json:"account_name,omitempty"`
	ExternalItemID    string `json:"external_item_id"`
	ExternalVariantID string `json:"external_variant_id"`
	BillType          string `json:"bill_type"`
	SourceSKU         string `json:"source_sku"`
	RawName           string `json:"raw_name"`
	NormalizedKey     string `json:"normalized_key"`
	ItemCount         int    `json:"item_count"`
	BillCount         int    `json:"bill_count"`
	// CatalogProduct is true when the row came from a connected Shopee shop's
	// local product snapshot. Catalog-only rows deliberately keep bill/item
	// counts at zero so the UI never presents them as pending orders.
	CatalogProduct bool `json:"catalog_product"`
}

type MarketplaceAliasIdentity struct {
	Source            string `json:"source"`
	AccountKey        string `json:"account_key"`
	ExternalItemID    string `json:"external_item_id"`
	ExternalVariantID string `json:"external_variant_id"`
	SourceSKU         string `json:"source_sku"`
	RawName           string `json:"raw_name"`
	NormalizedKey     string `json:"normalized_key"`
}

type MarketplaceAliasImpact struct {
	CurrentMappingRevision int64            `json:"current_mapping_revision"`
	OpenItems              int              `json:"open_items"`
	OpenBills              int              `json:"open_bills"`
	AttemptedItems         int              `json:"attempted_items"`
	ArchivedItems          int              `json:"archived_items"`
	ManualOverrideItems    int              `json:"manual_override_items"`
	Reservations           int              `json:"reservations"`
	ReservationMoves       int              `json:"reservation_moves"`
	StockMappings          int              `json:"stock_mappings"`
	StockConflicts         int              `json:"stock_conflicts"`
	LegacyManualFactors    int              `json:"legacy_manual_factors"`
	LegacyExclusions       int              `json:"legacy_exclusions"`
	AffectedShopIDs        []int64          `json:"affected_shop_ids"`
	StockConfigVersions    map[string]int64 `json:"stock_config_versions"`
	BeforeFormula          string           `json:"before_formula"`
	AfterFormula           string           `json:"after_formula"`
	ConversionStatus       string           `json:"conversion_status"`
	DryRunRequired         bool             `json:"dry_run_required"`
	ImpactDigest           string           `json:"impact_digest"`
}

type MarketplaceMappingJob struct {
	ID             string         `json:"id"`
	AliasID        string         `json:"alias_id"`
	TargetRevision int64          `json:"target_revision"`
	JobType        string         `json:"job_type"`
	Status         string         `json:"status"`
	ProcessedCount int64          `json:"processed_count"`
	SkippedCount   int64          `json:"skipped_count"`
	FailedCount    int64          `json:"failed_count"`
	ResultSummary  map[string]any `json:"result_summary"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
}

type MarketplaceStockPolicyJob struct {
	ID               string     `json:"id"`
	ShopID           int64      `json:"shop_id"`
	MarketplaceAlias string     `json:"marketplace_alias_id,omitempty"`
	TargetRevision   int64      `json:"target_revision"`
	ItemID           int64      `json:"item_id"`
	ModelID          int64      `json:"model_id"`
	PolicyAction     string     `json:"policy_action"`
	Status           string     `json:"status"`
	AttemptCount     int        `json:"attempt_count"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type MarketplaceBackfillJobStatus struct {
	ID             string     `json:"id"`
	JobType        string     `json:"job_type"`
	Status         string     `json:"status"`
	ProcessedCount int64      `json:"processed_count"`
	FailedCount    int64      `json:"failed_count"`
	AttemptCount   int        `json:"attempt_count"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type MarketplaceConversionReadiness struct {
	CatalogGenerationReady bool                           `json:"catalog_generation_ready"`
	MappingBackfillReady   bool                           `json:"mapping_backfill_ready"`
	ReservationLedgerReady bool                           `json:"reservation_ledger_ready"`
	ReconciliationSummary  map[string]any                 `json:"reconciliation_summary"`
	UpdatedAt              time.Time                      `json:"updated_at"`
	Jobs                   []MarketplaceBackfillJobStatus `json:"jobs"`
}

// MarketplaceAliasReviewFilter controls pagination + filtering for ReviewGroupsPaged.
type MarketplaceAliasReviewFilter struct {
	BillType string
	Source   string
	Query    string
	Sort     string // "impact" | "source" | "name"
	Page     int
	PerPage  int
}

// MarketplaceAliasReviewResult is the paginated response from ReviewGroupsPaged.
type MarketplaceAliasReviewResult struct {
	Groups  []MarketplaceAliasReviewGroup `json:"groups"`
	Total   int                           `json:"total"`
	Page    int                           `json:"page"`
	PerPage int                           `json:"per_page"`
}

// MarketplaceProductGroup is a parent-only summary. Variants are deliberately
// loaded from a separate cursor endpoint when the operator expands a row.
type MarketplaceProductGroup struct {
	Source        string    `json:"source"`
	AccountKey    string    `json:"account_key"`
	AccountName   string    `json:"account_name,omitempty"`
	ParentKey     string    `json:"parent_key"`
	ParentKeyKind string    `json:"parent_key_kind"`
	ProductName   string    `json:"product_name"`
	VariantCount  int       `json:"variant_count"`
	ReadyCount    int       `json:"ready_count"`
	FixCount      int       `json:"fix_count"`
	DisabledCount int       `json:"disabled_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}
