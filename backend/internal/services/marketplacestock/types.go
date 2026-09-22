package marketplacestock

import "time"

type Settings struct {
	WarehouseCode    string    `json:"warehouse_code"`
	LocationCode     string    `json:"location_code"`
	DefaultBufferPct float64   `json:"default_buffer_pct"`
	KillSwitch       bool      `json:"kill_switch_enabled"`
	ConfigVersion    int64     `json:"config_version"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Member struct {
	ID                string     `json:"id"`
	Source            string     `json:"source"`
	AccountKey        string     `json:"account_key"`
	ExternalProductID string     `json:"external_product_id"`
	ExternalSKUID     string     `json:"external_sku_id"`
	AliasID           string     `json:"marketplace_alias_id,omitempty"`
	ProductName       string     `json:"product_name"`
	VariantName       string     `json:"variant_name"`
	UnitFactor        float64    `json:"unit_factor"`
	AllocationPct     float64    `json:"allocation_pct"`
	Enabled           bool       `json:"enabled"`
	LastCatalogSeenAt *time.Time `json:"last_catalog_seen_at,omitempty"`
	LastTargetQty     *int64     `json:"last_target_qty,omitempty"`
	LastActualQty     *int64     `json:"last_actual_qty,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
}

type Pool struct {
	ID                     string         `json:"id"`
	SMLItemCode            string         `json:"sml_item_code"`
	SMLUnitCode            string         `json:"sml_unit_code"`
	AllocationMode         AllocationMode `json:"allocation_mode"`
	BufferPctOverride      *float64       `json:"buffer_pct_override,omitempty"`
	SharedRiskAcknowledged bool           `json:"shared_risk_acknowledged"`
	Status                 string         `json:"status"`
	AutoEnabled            bool           `json:"auto_enabled"`
	DryRunRequired         bool           `json:"dry_run_required"`
	PausedReason           string         `json:"paused_reason,omitempty"`
	ConfigVersion          int64          `json:"config_version"`
	LastPreviewAt          *time.Time     `json:"last_preview_at,omitempty"`
	LastSuccessAt          *time.Time     `json:"last_success_at,omitempty"`
	LastError              string         `json:"last_error,omitempty"`
	UpdatedAt              time.Time      `json:"updated_at"`
	Members                []Member       `json:"members"`
}

type Overview struct {
	Available bool      `json:"available"`
	Settings  Settings  `json:"settings"`
	Pools     []Pool    `json:"pools"`
	CheckedAt time.Time `json:"checked_at"`
}

type PoolInput struct {
	SMLItemCode            string         `json:"sml_item_code"`
	SMLUnitCode            string         `json:"sml_unit_code"`
	AllocationMode         AllocationMode `json:"allocation_mode"`
	BufferPctOverride      *float64       `json:"buffer_pct_override,omitempty"`
	SharedRiskAcknowledged bool           `json:"shared_risk_acknowledged"`
	Members                []MemberInput  `json:"members"`
}

type MemberInput struct {
	Source            string  `json:"source"`
	AccountKey        string  `json:"account_key"`
	ExternalProductID string  `json:"external_product_id"`
	ExternalSKUID     string  `json:"external_sku_id"`
	AliasID           string  `json:"marketplace_alias_id,omitempty"`
	ProductName       string  `json:"product_name"`
	VariantName       string  `json:"variant_name"`
	UnitFactor        float64 `json:"unit_factor"`
	AllocationPct     float64 `json:"allocation_pct"`
	Enabled           bool    `json:"enabled"`
}

type PoolUpdate struct {
	PoolInput
	ExpectedConfigVersion int64  `json:"expected_config_version"`
	ConfirmAction         string `json:"confirm_action"`
}

type SettingsUpdate struct {
	WarehouseCode         string  `json:"warehouse_code"`
	LocationCode          string  `json:"location_code"`
	DefaultBufferPct      float64 `json:"default_buffer_pct"`
	KillSwitch            bool    `json:"kill_switch_enabled"`
	ExpectedConfigVersion int64   `json:"expected_config_version"`
	ConfirmAction         string  `json:"confirm_action"`
}
