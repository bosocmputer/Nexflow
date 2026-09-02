package models

import (
	"encoding/json"
	"time"
)

type Bill struct {
	ID                      string             `json:"id"`
	BillType                string             `json:"bill_type"`
	Source                  string             `json:"source"`
	SourceAccountKey        string             `json:"source_account_key"`
	Status                  string             `json:"status"`
	DocumentRoute           string             `json:"document_route"`
	RawData                 json.RawMessage    `json:"raw_data,omitempty"`
	SMLDocNo                *string            `json:"sml_doc_no,omitempty"`
	SMLOrderID              string             `json:"sml_order_id,omitempty"`
	SMLPayload              json.RawMessage    `json:"sml_payload,omitempty"`
	SMLResponse             json.RawMessage    `json:"sml_response,omitempty"`
	CurrentSMLAttemptID     *string            `json:"current_sml_attempt_id,omitempty"`
	SMLAttemptState         string             `json:"sml_attempt_state,omitempty"`
	SMLCoreStatus           string             `json:"sml_core_status,omitempty"`
	SMLProfileVersion       string             `json:"sml_profile_version,omitempty"`
	SMLProfileStatus        string             `json:"sml_profile_status,omitempty"`
	SMLProfilePayloadHash   string             `json:"sml_profile_payload_hash,omitempty"`
	SMLProfileRequired      []string           `json:"sml_profile_required_checks,omitempty"`
	SMLProfileCompleted     []string           `json:"sml_profile_completed_checks,omitempty"`
	SMLProfileNeedsRepair   bool               `json:"sml_profile_reconciliation_required,omitempty"`
	SMLProfileJobStatus     string             `json:"sml_profile_job_status,omitempty"`
	SMLProfileRetryCount    int                `json:"sml_profile_retry_count,omitempty"`
	SMLProfileManualRetries int                `json:"sml_profile_manual_retries,omitempty"`
	SMLProfileMaxRetries    int                `json:"sml_profile_max_retries,omitempty"`
	SMLProfileErrorCode     string             `json:"sml_profile_error_code,omitempty"`
	SMLProfileErrorMessage  string             `json:"sml_profile_error_message,omitempty"`
	SMLProfileCorrelationID string             `json:"sml_profile_correlation_id,omitempty"`
	SMLStockJobStatus       string             `json:"sml_stock_job_status,omitempty"`
	SMLStockRetryCount      int                `json:"sml_stock_retry_count,omitempty"`
	SMLStockErrorMessage    string             `json:"sml_stock_error_message,omitempty"`
	MutationRevision        int64              `json:"mutation_revision"`
	AIConfidence            *float64           `json:"-"` // retained in storage for historical audit only
	Anomalies               json.RawMessage    `json:"anomalies"`
	ErrorMsg                *string            `json:"error_msg,omitempty"`
	CreatedBy               *string            `json:"created_by,omitempty"`
	CreatedAt               time.Time          `json:"created_at"`
	SentAt                  *time.Time         `json:"sent_at,omitempty"`
	ArchivedAt              *time.Time         `json:"archived_at,omitempty"`
	ArchivedBy              *string            `json:"archived_by,omitempty"`
	ArchiveReason           string             `json:"archive_reason,omitempty"`
	TotalAmount             *float64           `json:"total_amount,omitempty"`
	Remark                  string             `json:"remark"`
	AmountReviewedAt        *time.Time         `json:"amount_reviewed_at,omitempty"`
	AmountReviewedBy        *string            `json:"amount_reviewed_by,omitempty"`
	AmountReviewFingerprint string             `json:"amount_review_fingerprint,omitempty"`
	Items                   []BillItem         `json:"items,omitempty"`
	EmailGroup              *BillEmailGroup    `json:"email_group,omitempty"`
	ShopeeStatus            *ShopeeOrderEvent  `json:"shopee_status,omitempty"`
	ShopeeEvents            []ShopeeOrderEvent `json:"shopee_events,omitempty"`
	// True when a Shopee Realtime snapshot currently points at this bill.
	// The UI uses this to hide destructive delete actions and direct users to
	// the route-change flow instead.
	ShopeeRealtimeLinked bool `json:"shopee_realtime_linked,omitempty"`
}

type BillSMLAttempt struct {
	ID                       string          `json:"id"`
	TenantKey                string          `json:"tenant_key"`
	BillID                   string          `json:"bill_id"`
	DocNo                    string          `json:"doc_no"`
	State                    string          `json:"state"`
	Route                    string          `json:"route"`
	PayloadBytes             []byte          `json:"-"`
	PayloadJSON              json.RawMessage `json:"payload_json,omitempty"`
	PayloadHash              string          `json:"payload_hash"`
	RouteSettings            json.RawMessage `json:"route_settings"`
	MappingRevisions         json.RawMessage `json:"mapping_revisions"`
	UnitCatalogGeneration    *string         `json:"unit_catalog_generation,omitempty"`
	SetDefinitionHashes      json.RawMessage `json:"set_definition_hashes"`
	LeaseOwner               string          `json:"-"`
	LeaseUntil               *time.Time      `json:"lease_until,omitempty"`
	ExternalRequestStartedAt *time.Time      `json:"external_request_started_at,omitempty"`
	ExternalRequestEndedAt   *time.Time      `json:"external_request_finished_at,omitempty"`
	ResponseBytes            []byte          `json:"-"`
	ResponseHash             string          `json:"response_hash,omitempty"`
	ErrorMessage             string          `json:"error_message,omitempty"`
	CreatedBy                *string         `json:"created_by,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

type BillEmailGroup struct {
	MessageID          string                 `json:"message_id"`
	GroupKey           string                 `json:"group_key"`
	Subject            string                 `json:"subject"`
	From               string                 `json:"from"`
	OrderCount         int                    `json:"order_count"`
	HasPrintableEmail  bool                   `json:"has_printable_email"`
	PrintCount         int                    `json:"print_count"`
	LastPrintedAt      *time.Time             `json:"last_printed_at,omitempty"`
	LastPrintedByEmail string                 `json:"last_printed_by_email,omitempty"`
	LastPrintedByName  string                 `json:"last_printed_by_name,omitempty"`
	RelatedBills       []BillEmailRelatedBill `json:"related_bills,omitempty"`
	PrintEvents        []EmailPrintEvent      `json:"print_events,omitempty"`
}

type BillEmailRelatedBill struct {
	ID            string    `json:"id"`
	OrderID       string    `json:"order_id"`
	PartyName     string    `json:"party_name"`
	Source        string    `json:"source"`
	BillType      string    `json:"bill_type"`
	DocumentRoute string    `json:"document_route"`
	Status        string    `json:"status"`
	SMLDocNo      string    `json:"sml_doc_no,omitempty"`
	TotalAmount   float64   `json:"total_amount"`
	CreatedAt     time.Time `json:"created_at"`
	IsCurrent     bool      `json:"is_current"`
}

type EmailPrintEvent struct {
	ID               string    `json:"id"`
	BillID           string    `json:"bill_id"`
	ArtifactID       string    `json:"artifact_id,omitempty"`
	EmailMessageID   string    `json:"email_message_id"`
	EmailGroupKey    string    `json:"email_group_key"`
	Subject          string    `json:"subject"`
	From             string    `json:"from"`
	RequestedBy      string    `json:"requested_by,omitempty"`
	RequestedByEmail string    `json:"requested_by_email,omitempty"`
	RequestedByName  string    `json:"requested_by_name,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type ShopeeOrderEvent struct {
	ID          string          `json:"id"`
	BillID      *string         `json:"bill_id,omitempty"`
	OrderID     string          `json:"order_id"`
	EventType   string          `json:"event_type"`
	StatusLabel string          `json:"status_label"`
	Subject     string          `json:"subject"`
	FromAddr    string          `json:"from_addr"`
	MessageID   string          `json:"message_id"`
	EmailDate   *time.Time      `json:"email_date,omitempty"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type DailyInsight struct {
	ID        string    `json:"id"`
	Date      string    `json:"date"`
	StatsJSON string    `json:"stats_json,omitempty"`
	Insight   string    `json:"insight"`
	CreatedAt time.Time `json:"created_at"`
}

type BillItem struct {
	ID                            string          `json:"id"`
	BillID                        string          `json:"bill_id"`
	RawName                       string          `json:"raw_name"`
	SourceSKU                     string          `json:"source_sku,omitempty"`
	SourceItemID                  string          `json:"source_item_id,omitempty"`
	SourceVariantID               string          `json:"source_variant_id,omitempty"`
	SourceLineID                  string          `json:"source_line_id,omitempty"`
	MarketplaceAliasID            *string         `json:"marketplace_alias_id,omitempty"`
	SourceImageURL                string          `json:"source_image_url,omitempty"`
	ItemCode                      *string         `json:"item_code,omitempty"`
	HasHiddenChars                bool            `json:"has_hidden_chars"`
	CleanItemCode                 string          `json:"clean_item_code,omitempty"`
	Qty                           float64         `json:"qty"`
	SourceQty                     *float64        `json:"source_qty,omitempty"`
	SMLQty                        *float64        `json:"sml_qty,omitempty"`
	QuantityMultiplierSnapshot    *int64          `json:"quantity_multiplier_snapshot,omitempty"`
	UnitStandValueSnapshot        *string         `json:"unit_stand_value_snapshot,omitempty"`
	UnitDivideValueSnapshot       *string         `json:"unit_divide_value_snapshot,omitempty"`
	BaseQtySnapshot               *string         `json:"base_qty_snapshot,omitempty"`
	MappingRevisionSnapshot       *int64          `json:"mapping_revision_snapshot,omitempty"`
	UnitCatalogGenerationSnapshot *string         `json:"unit_catalog_generation_snapshot,omitempty"`
	SetDefinitionHashSnapshot     string          `json:"set_definition_hash_snapshot,omitempty"`
	ConversionOverrideFields      json.RawMessage `json:"conversion_override_fields,omitempty"`
	ConversionIssueCode           string          `json:"conversion_issue_code,omitempty"`
	UnitCode                      *string         `json:"unit_code,omitempty"`
	Price                         *float64        `json:"price,omitempty"`
	GrossAmount                   *float64        `json:"gross_amount,omitempty"`
	DiscountAmount                float64         `json:"discount_amount"`
	Mapped                        bool            `json:"mapped"`
	MappingID                     *string         `json:"mapping_id,omitempty"`
	Candidates                    json.RawMessage `json:"-"` // retained in storage for migration compatibility only
}

const ShopeeShippingSourceSKU = "__shopee_shipping__"
const LazadaShippingSourceSKU = "__lazada_shipping__"
const TikTokShippingSourceSKU = "__tiktok_shipping__"

type BillListFilter struct {
	Status         string `form:"status"`
	Source         string `form:"source"`
	InputChannel   string `form:"input_channel"`
	BillType       string `form:"bill_type"`
	DocumentRoute  string `form:"document_route"`
	EmailAccountID string `form:"email_account_id"`
	ShopeeStatus   string `form:"shopee_status"`
	ShopeeShopID   string `form:"shopee_shop_id"`
	Search         string `form:"search"`
	Archived       string `form:"archived"` // ""/"active" | "include" | "only"
	DateFrom       string `form:"date_from"`
	DateTo         string `form:"date_to"`
	Sort           string `form:"sort"`
	Cursor         string `form:"cursor"`
	Limit          int    `form:"limit"`
	CursorMode     bool   `form:"-"`
	IncludeTotal   bool   `form:"include_total"`
	Page           int    `form:"page,default=1"`
	PageSize       int    `form:"page_size,default=20"`
	PerPage        int    `form:"per_page"`
}

type Anomaly struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // "block" | "warn"
	Message  string `json:"message"`
}
