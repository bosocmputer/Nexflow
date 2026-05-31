package models

import "time"

// MarketplaceItemAlias maps a marketplace raw_name / source_sku to an SML item_code.
// One row per (source, source_sku) or (source, normalized_key).
type MarketplaceItemAlias struct {
	ID            string     `json:"id"`
	Source        string     `json:"source"`
	SourceSKU     string     `json:"source_sku"`
	RawName       string     `json:"raw_name"`
	NormalizedKey string     `json:"normalized_key"`
	ItemCode      string     `json:"item_code"`
	UnitCode      string     `json:"unit_code"`
	Confidence    float64    `json:"confidence"`
	ConfirmedBy   *string    `json:"confirmed_by,omitempty"`
	UsageCount    int        `json:"usage_count"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// MarketplaceAliasReviewGroup groups unmatched bill items by their normalized key
// for bulk review in the admin UI.
type MarketplaceAliasReviewGroup struct {
	GroupKey       string        `json:"group_key"`
	Source         string        `json:"source"`
	BillType       string        `json:"bill_type"`
	SourceSKU      string        `json:"source_sku"`
	RawName        string        `json:"raw_name"`
	NormalizedKey  string        `json:"normalized_key"`
	ItemCount      int           `json:"item_count"`
	BillCount      int           `json:"bill_count"`
	Candidates     []CatalogMatch `json:"candidates"`
	SuggestedMatch *CatalogMatch  `json:"suggested_match,omitempty"`
}

// MarketplaceAliasReviewFilter controls pagination + filtering for ReviewGroupsPaged.
type MarketplaceAliasReviewFilter struct {
	BillType string
	Source   string
	Query    string
	Sort     string // "impact" | "source" | "name" | "score"
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
