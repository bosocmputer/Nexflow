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
	OpenItems      int  `json:"open_items"`
	OpenBills      int  `json:"open_bills"`
	StockMappings  int  `json:"stock_mappings"`
	StockConflicts int  `json:"stock_conflicts"`
	DryRunRequired bool `json:"dry_run_required"`
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
