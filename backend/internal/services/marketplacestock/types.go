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
	ID                  string     `json:"id"`
	Source              string     `json:"source"`
	AccountKey          string     `json:"account_key"`
	ExternalProductID   string     `json:"external_product_id"`
	ExternalSKUID       string     `json:"external_sku_id"`
	ExternalWarehouseID string     `json:"external_warehouse_id,omitempty"`
	AliasID             string     `json:"marketplace_alias_id,omitempty"`
	ProductName         string     `json:"product_name"`
	VariantName         string     `json:"variant_name"`
	UnitFactor          float64    `json:"unit_factor"`
	AllocationPct       float64    `json:"allocation_pct"`
	Enabled             bool       `json:"enabled"`
	LastCatalogSeenAt   *time.Time `json:"last_catalog_seen_at,omitempty"`
	LastTargetQty       *int64     `json:"last_target_qty,omitempty"`
	LastActualQty       *int64     `json:"last_actual_qty,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}

type Pool struct {
	ID                      string         `json:"id"`
	SMLItemCode             string         `json:"sml_item_code"`
	SMLItemName             string         `json:"sml_item_name,omitempty"`
	SMLUnitCode             string         `json:"sml_unit_code"`
	AllocationMode          AllocationMode `json:"allocation_mode"`
	BufferPctOverride       *float64       `json:"buffer_pct_override,omitempty"`
	SharedRiskAcknowledged  bool           `json:"shared_risk_acknowledged"`
	Status                  string         `json:"status"`
	AutoEnabled             bool           `json:"auto_enabled"`
	KillSwitch              bool           `json:"kill_switch_enabled"`
	ScheduleIntervalSeconds int            `json:"schedule_interval_seconds"`
	DryRunRequired          bool           `json:"dry_run_required"`
	PausedReason            string         `json:"paused_reason,omitempty"`
	ConfigVersion           int64          `json:"config_version"`
	LastPreviewAt           *time.Time     `json:"last_preview_at,omitempty"`
	LastSuccessAt           *time.Time     `json:"last_success_at,omitempty"`
	LastScheduleAt          *time.Time     `json:"last_schedule_at,omitempty"`
	LastError               string         `json:"last_error,omitempty"`
	UpdatedAt               time.Time      `json:"updated_at"`
	Members                 []Member       `json:"members"`
}

type Overview struct {
	Available bool      `json:"available"`
	Settings  Settings  `json:"settings"`
	Pools     []Pool    `json:"pools"`
	CheckedAt time.Time `json:"checked_at"`
}

// Candidate is a Product Master mapping eligible to be added to a draft pool.
// It is local data only; catalog freshness is checked again before any future
// external stock write.
type Candidate struct {
	MemberInput
	SMLItemCode string `json:"sml_item_code"`
	SMLItemName string `json:"sml_item_name,omitempty"`
	SMLUnitCode string `json:"sml_unit_code"`
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
	Source              string  `json:"source"`
	AccountKey          string  `json:"account_key"`
	ExternalProductID   string  `json:"external_product_id"`
	ExternalSKUID       string  `json:"external_sku_id"`
	ExternalWarehouseID string  `json:"external_warehouse_id,omitempty"`
	AliasID             string  `json:"marketplace_alias_id,omitempty"`
	ProductName         string  `json:"product_name"`
	VariantName         string  `json:"variant_name"`
	UnitFactor          float64 `json:"unit_factor"`
	AllocationPct       float64 `json:"allocation_pct"`
	Enabled             bool    `json:"enabled"`
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

type PreviewRequest struct {
	ExpectedConfigVersion int64  `json:"expected_config_version"`
	ConfirmAction         string `json:"confirm_action"`
}

type PreviewPlan struct {
	RunID          string
	Settings       Settings
	Pool           Pool
	UnitBaseFactor float64
	PendingBaseQty float64
	RequestedBy    string
}

type PreviewLine struct {
	MemberID  string `json:"member_id"`
	TargetQty int64  `json:"target_qty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

type PreviewResult struct {
	RunID            string        `json:"run_id"`
	Status           string        `json:"status"`
	SMLAvailableQty  float64       `json:"sml_available_qty"`
	ReservationQty   float64       `json:"reservation_qty"`
	UsableQty        float64       `json:"usable_qty"`
	BufferQty        float64       `json:"buffer_qty"`
	DistributableQty float64       `json:"distributable_qty"`
	Lines            []PreviewLine `json:"lines"`
	ExpiresAt        time.Time     `json:"expires_at"`
}

// AutoUpdate is intentionally separate from PoolUpdate: turning automatic
// writes on or off is a high-impact operation and always has its own explicit
// confirmation phrase and optimistic configuration version.
type AutoUpdate struct {
	Enabled               bool   `json:"enabled"`
	ExpectedConfigVersion int64  `json:"expected_config_version"`
	ConfirmAction         string `json:"confirm_action"`
}

type SyncRequest struct {
	ExpectedConfigVersion int64  `json:"expected_config_version"`
	ConfirmAction         string `json:"confirm_action"`
}

type Run struct {
	ID            string     `json:"id"`
	PoolID        string     `json:"pool_id"`
	TriggerSource string     `json:"trigger_source"`
	RunType       string     `json:"run_type"`
	Status        string     `json:"status"`
	ConfigVersion int64      `json:"config_version"`
	TotalCount    int        `json:"total_count"`
	ChangedCount  int        `json:"changed_count"`
	BlockedCount  int        `json:"blocked_count"`
	ErrorCount    int        `json:"error_count"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	PlanExpiresAt time.Time  `json:"plan_expires_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}
