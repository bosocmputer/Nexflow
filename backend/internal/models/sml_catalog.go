package models

import (
	"time"
)

// CatalogItem represents one row in sml_catalog
type CatalogItem struct {
	ItemCode             string                      `json:"item_code"`
	ItemName             string                      `json:"item_name"`
	ItemName2            string                      `json:"item_name2"`
	UnitCode             string                      `json:"unit_code"`
	WHCode               string                      `json:"wh_code"`
	ShelfCode            string                      `json:"shelf_code"`
	GroupCode            string                      `json:"group_code"`
	BalanceQty           *float64                    `json:"balance_qty"`
	ItemType             int                         `json:"item_type"`
	SetComponentCount    int                         `json:"set_component_count"`
	SetDefinitionHash    string                      `json:"set_definition_hash,omitempty"`
	SetDocumentValid     bool                        `json:"set_document_valid"`
	SetStockValid        bool                        `json:"set_stock_valid"`
	SetWarningCodes      []string                    `json:"set_warning_codes,omitempty"`
	SetComponents        []CatalogSetComponent       `json:"set_components,omitempty"`
	MarketplaceSummaries []CatalogMarketplaceSummary `json:"marketplace_summaries"`
	EmbeddingStatus      string                      `json:"embedding_status"` // disabled in sales-only mode
	IsActive             bool                        `json:"is_active"`
	EmbeddedAt           *time.Time                  `json:"embedded_at"`
	ImageCount           int                         `json:"image_count"`
	PrimaryImageRoworder *int                        `json:"primary_image_roworder,omitempty"`
	PrimaryImageGuid     string                      `json:"primary_image_guid,omitempty"`
	PrimaryImageBytes    *int64                      `json:"primary_image_bytes,omitempty"`
	ImageSyncedAt        *time.Time                  `json:"image_synced_at,omitempty"`
	ImageURL             string                      `json:"image_url,omitempty"`
	HasHiddenChars       bool                        `json:"has_hidden_chars"`
	CleanItemCode        string                      `json:"clean_item_code,omitempty"`
	HiddenCharKinds      []string                    `json:"hidden_char_kinds,omitempty"`
	ImageMetadataSynced  bool                        `json:"-"`
	SyncedAt             time.Time                   `json:"synced_at"`
	CreatedAt            time.Time                   `json:"created_at"`
}

// CatalogMarketplaceSummary is the bounded per-platform count attached to a
// catalog page. Marketplace product names are loaded only when the user opens
// the detail dialog so the main catalog response stays small.
type CatalogMarketplaceSummary struct {
	Source       string `json:"source"`
	MappingCount int    `json:"mapping_count"`
	ProductCount int    `json:"product_count"`
	AccountCount int    `json:"account_count"`
}

// CatalogMarketplaceLink is one active Product Master mapping that points to
// an SML catalog item. It contains seller product metadata only, never buyer PII.
type CatalogMarketplaceLink struct {
	ID                 string    `json:"id"`
	Source             string    `json:"source"`
	AccountKey         string    `json:"account_key"`
	AccountName        string    `json:"account_name,omitempty"`
	ProductName        string    `json:"product_name"`
	VariantName        string    `json:"variant_name"`
	SourceSKU          string    `json:"source_sku,omitempty"`
	ExternalItemID     string    `json:"external_item_id,omitempty"`
	ExternalVariantID  string    `json:"external_variant_id,omitempty"`
	UnitCode           string    `json:"unit_code"`
	QuantityMultiplier int64     `json:"quantity_multiplier"`
	ConversionStatus   string    `json:"conversion_status"`
	ScopeConfirmed     bool      `json:"scope_confirmed"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CatalogSetComponent struct {
	LineNumber int     `json:"line_number"`
	RowOrder   int     `json:"row_order"`
	ItemCode   string  `json:"item_code"`
	ItemName   string  `json:"item_name"`
	ItemType   int     `json:"item_type"`
	UnitCode   string  `json:"unit_code"`
	Qty        float64 `json:"qty"`
	Price      float64 `json:"price"`
	SumAmount  float64 `json:"sum_amount"`
	PriceRatio float64 `json:"price_ratio"`
	UnitFactor float64 `json:"unit_factor"`
	Active     bool    `json:"active"`
	UnitValid  bool    `json:"unit_valid"`
}

// CatalogMatch is one deterministic database search result.
type CatalogMatch struct {
	ItemCode             string                `json:"item_code"`
	ItemName             string                `json:"item_name"`
	ItemName2            string                `json:"item_name2"`
	UnitCode             string                `json:"unit_code"`
	WHCode               string                `json:"wh_code"`
	ShelfCode            string                `json:"shelf_code"`
	ItemType             int                   `json:"item_type"`
	SetComponentCount    int                   `json:"set_component_count"`
	SetDefinitionHash    string                `json:"set_definition_hash,omitempty"`
	SetDocumentValid     bool                  `json:"set_document_valid"`
	SetWarningCodes      []string              `json:"set_warning_codes,omitempty"`
	SetComponents        []CatalogSetComponent `json:"set_components,omitempty"`
	ImageCount           int                   `json:"image_count"`
	PrimaryImageRoworder *int                  `json:"primary_image_roworder,omitempty"`
	PrimaryImageGuid     string                `json:"primary_image_guid,omitempty"`
	PrimaryImageBytes    *int64                `json:"primary_image_bytes,omitempty"`
	ImageURL             string                `json:"image_url,omitempty"`
	HasHiddenChars       bool                  `json:"has_hidden_chars"`
	CleanItemCode        string                `json:"clean_item_code,omitempty"`
	HiddenCharKinds      []string              `json:"hidden_char_kinds,omitempty"`
	Score                float64               `json:"score"` // deterministic rank retained for response compatibility
	Method               string                `json:"method,omitempty"`
	MatchType            string                `json:"match_type,omitempty"`
}

// CatalogUnit is an exact decimal snapshot from SML ic_unit_use. String
// decimals prevent rational conversion values from being rounded in transit.
type CatalogUnit struct {
	ItemCode     string `json:"item_code"`
	UnitCode     string `json:"unit_code"`
	UnitName     string `json:"unit_name"`
	StandValue   string `json:"stand_value"`
	DivideValue  string `json:"divide_value"`
	IsDefault    bool   `json:"is_default"`
	UnitOrder    int    `json:"unit_order"`
	GenerationID string `json:"generation_id"`
}
