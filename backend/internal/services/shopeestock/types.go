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
	ShopID                       int64                   `json:"shop_id"`
	ItemID                       int64                   `json:"item_id"`
	ModelID                      int64                   `json:"model_id"`
	ItemName                     string                  `json:"item_name"`
	ModelName                    string                  `json:"model_name"`
	ItemSKU                      string                  `json:"item_sku"`
	ModelSKU                     string                  `json:"model_sku"`
	ShopeeAvailable              int64                   `json:"shopee_available"`
	ShopeeReserved               int64                   `json:"shopee_reserved"`
	SMLItemCode                  string                  `json:"sml_item_code"`
	SMLItemName                  string                  `json:"sml_item_name"`
	SMLUnitCode                  string                  `json:"sml_unit_code"`
	SMLUnitName                  string                  `json:"sml_unit_name"`
	SMLBaseUnitCode              string                  `json:"sml_base_unit_code"`
	SMLBaseUnitName              string                  `json:"sml_base_unit_name"`
	SMLItemType                  int                     `json:"sml_item_type"`
	SetComponentCount            int                     `json:"set_component_count"`
	SetDefinitionHash            string                  `json:"set_definition_hash,omitempty"`
	MappingSetDefinitionHash     string                  `json:"mapping_set_definition_hash,omitempty"`
	SetDocumentValid             bool                    `json:"set_document_valid"`
	SetStockValid                bool                    `json:"set_stock_valid"`
	SetComponents                []sml.StockSetComponent `json:"set_components,omitempty"`
	UnitFactor                   float64                 `json:"unit_factor"`
	ManualUnitFactor             *float64                `json:"manual_unit_factor,omitempty"`
	MatchSource                  string                  `json:"match_source"`
	SharedPoolEnabled            bool                    `json:"shared_pool_enabled"`
	PoolAllocationPct            float64                 `json:"pool_allocation_pct"`
	MarketplaceAliasID           string                  `json:"marketplace_alias_id,omitempty"`
	MarketplaceAliasUpdatedAt    *time.Time              `json:"marketplace_alias_updated_at,omitempty"`
	Excluded                     bool                    `json:"excluded"`
	WarningCodes                 []string                `json:"warning_codes"`
	LastPreviewBalance           *float64                `json:"last_preview_balance,omitempty"`
	LastPreviewExcludedBalance   *float64                `json:"last_preview_excluded_balance,omitempty"`
	LastPreviewExcludedLocations []ExcludedStockLocation `json:"last_preview_excluded_locations,omitempty"`
	LastPreviewMinQty            *float64                `json:"last_preview_min_qty,omitempty"`
	LastPreviewMaxQty            *float64                `json:"last_preview_max_qty,omitempty"`
	LastPreviewTarget            *int64                  `json:"last_preview_target,omitempty"`
	LastPreviewPendingQty        *float64                `json:"last_preview_pending_qty,omitempty"`
	LastPreviewPoolBaseTarget    *int64                  `json:"last_preview_pool_base_target,omitempty"`
	LastSuccessTarget            *int64                  `json:"last_success_target,omitempty"`
	UpdatedAt                    time.Time               `json:"updated_at"`
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

type SharedPoolMember struct {
	ItemID                    int64     `json:"item_id"`
	ModelID                   int64     `json:"model_id"`
	ItemName                  string    `json:"item_name"`
	ModelName                 string    `json:"model_name"`
	ItemSKU                   string    `json:"item_sku"`
	ModelSKU                  string    `json:"model_sku"`
	ShopeeAvailable           int64     `json:"shopee_available"`
	ShopeeReserved            int64     `json:"shopee_reserved"`
	SMLUnitCode               string    `json:"sml_unit_code"`
	SMLUnitName               string    `json:"sml_unit_name"`
	SMLBaseUnitCode           string    `json:"sml_base_unit_code"`
	SMLBaseUnitName           string    `json:"sml_base_unit_name"`
	UnitFactor                float64   `json:"unit_factor"`
	SharedPoolEnabled         bool      `json:"shared_pool_enabled"`
	PoolAllocationPct         float64   `json:"pool_allocation_pct"`
	LastPreviewBalance        *float64  `json:"last_preview_balance,omitempty"`
	LastPreviewPendingQty     *float64  `json:"last_preview_pending_qty,omitempty"`
	LastPreviewTarget         *int64    `json:"last_preview_target,omitempty"`
	LastPreviewPoolBaseTarget *int64    `json:"last_preview_pool_base_target,omitempty"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type SharedPool struct {
	ShopID          int64              `json:"shop_id"`
	SMLItemCode     string             `json:"sml_item_code"`
	SMLItemName     string             `json:"sml_item_name"`
	StockPct        float64            `json:"stock_pct"`
	Configured      bool               `json:"configured"`
	AllocationTotal float64            `json:"allocation_total"`
	Members         []SharedPoolMember `json:"members"`
}

type SharedPoolMemberUpdate struct {
	ItemID        int64     `json:"item_id"`
	ModelID       int64     `json:"model_id"`
	AllocationPct float64   `json:"allocation_pct"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SharedPoolUpdate struct {
	SMLItemCode string                   `json:"sml_item_code"`
	Members     []SharedPoolMemberUpdate `json:"members"`
}

type CatalogDue struct {
	ShopID int64
	Full   bool
}

type CatalogOption struct {
	ItemCode          string                  `json:"item_code"`
	ItemName          string                  `json:"item_name"`
	ItemType          int                     `json:"item_type"`
	StandardUnit      string                  `json:"standard_unit"`
	SetComponentCount int                     `json:"set_component_count"`
	SetDefinitionHash string                  `json:"set_definition_hash,omitempty"`
	SetDocumentValid  bool                    `json:"set_document_valid"`
	SetStockValid     bool                    `json:"set_stock_valid"`
	SetWarningCodes   []string                `json:"set_warning_codes,omitempty"`
	SetComponents     []sml.StockSetComponent `json:"set_components,omitempty"`
	Units             []sml.StockCatalogUnit  `json:"units"`
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
	ItemID             int64                      `json:"item_id"`
	ModelID            int64                      `json:"model_id"`
	SMLItemCode        string                     `json:"sml_item_code"`
	ScopeBalance       float64                    `json:"scope_balance"`
	ExcludedBalance    float64                    `json:"excluded_balance"`
	ExcludedLocations  []ExcludedStockLocation    `json:"excluded_locations,omitempty"`
	MinQty             float64                    `json:"min_qty"`
	MaxQty             float64                    `json:"max_qty"`
	UnitFactor         float64                    `json:"unit_factor"`
	CurrentStock       int64                      `json:"current_stock"`
	ReservedStock      int64                      `json:"reserved_stock"`
	PendingNexflowQty  float64                    `json:"pending_nexflow_qty"`
	TargetStock        int64                      `json:"target_stock"`
	SharedPoolEnabled  bool                       `json:"shared_pool_enabled"`
	PoolAllocationPct  float64                    `json:"pool_allocation_pct"`
	PoolBaseTarget     int64                      `json:"pool_base_target"`
	Changed            bool                       `json:"changed"`
	Blocked            bool                       `json:"blocked"`
	WarningCodes       []string                   `json:"warning_codes"`
	ItemType           int                        `json:"item_type"`
	SetDefinitionHash  string                     `json:"set_definition_hash,omitempty"`
	SetComponents      []SetStockComponentPreview `json:"set_components,omitempty"`
	BottleneckItemCode string                     `json:"bottleneck_item_code,omitempty"`
}

type ExcludedStockLocation struct {
	ItemCode      string  `json:"item_code,omitempty"`
	WarehouseCode string  `json:"warehouse_code"`
	WarehouseName string  `json:"warehouse_name"`
	LocationCode  string  `json:"location_code"`
	LocationName  string  `json:"location_name"`
	UnitCode      string  `json:"unit_code"`
	BalanceQty    float64 `json:"balance_qty"`
}

type SetStockComponentPreview struct {
	ItemCode        string  `json:"item_code"`
	ItemName        string  `json:"item_name"`
	ComponentQty    float64 `json:"component_qty"`
	UnitCode        string  `json:"unit_code"`
	BalanceUnitCode string  `json:"balance_unit_code"`
	BalanceQty      float64 `json:"balance_qty"`
	RequiredBase    float64 `json:"required_base"`
	PossibleSets    int64   `json:"possible_sets"`
	Bottleneck      bool    `json:"bottleneck"`
}

type PreviewResult struct {
	RunID             string                  `json:"run_id"`
	ShopID            int64                   `json:"shop_id"`
	AsOfDate          string                  `json:"as_of_date"`
	TotalCount        int                     `json:"total_count"`
	ChangedCount      int                     `json:"changed_count"`
	SkippedCount      int                     `json:"skipped_count"`
	BlockedCount      int                     `json:"blocked_count"`
	ExcludedBalance   float64                 `json:"excluded_balance"`
	ExcludedLocations []ExcludedStockLocation `json:"excluded_locations"`
	CircuitBreaker    string                  `json:"circuit_breaker,omitempty"`
	Lines             []PreviewLine           `json:"lines"`
}

type SyncResult struct {
	RunID        string `json:"run_id"`
	ShopID       int64  `json:"shop_id"`
	ChangedCount int    `json:"changed_count"`
	BlockedCount int    `json:"blocked_count"`
	ErrorCount   int    `json:"error_count"`
	UnknownCount int    `json:"unknown_count"`
}
