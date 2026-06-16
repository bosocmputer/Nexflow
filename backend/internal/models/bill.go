package models

import (
	"encoding/json"
	"time"
)

type Bill struct {
	ID            string             `json:"id"`
	BillType      string             `json:"bill_type"`
	Source        string             `json:"source"`
	Status        string             `json:"status"`
	DocumentRoute string             `json:"document_route"`
	RawData       json.RawMessage    `json:"raw_data,omitempty"`
	SMLDocNo      *string            `json:"sml_doc_no,omitempty"`
	SMLOrderID    string             `json:"sml_order_id,omitempty"`
	SMLPayload    json.RawMessage    `json:"sml_payload,omitempty"`
	SMLResponse   json.RawMessage    `json:"sml_response,omitempty"`
	AIConfidence  *float64           `json:"ai_confidence,omitempty"`
	Anomalies     json.RawMessage    `json:"anomalies"`
	ErrorMsg      *string            `json:"error_msg,omitempty"`
	CreatedBy     *string            `json:"created_by,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	SentAt        *time.Time         `json:"sent_at,omitempty"`
	ArchivedAt    *time.Time         `json:"archived_at,omitempty"`
	ArchivedBy    *string            `json:"archived_by,omitempty"`
	ArchiveReason string             `json:"archive_reason,omitempty"`
	TotalAmount   *float64           `json:"total_amount,omitempty"`
	Remark        string             `json:"remark"`
	Items         []BillItem         `json:"items,omitempty"`
	EmailGroup    *BillEmailGroup    `json:"email_group,omitempty"`
	ShopeeStatus  *ShopeeOrderEvent  `json:"shopee_status,omitempty"`
	ShopeeEvents  []ShopeeOrderEvent `json:"shopee_events,omitempty"`
	// True when a Shopee Realtime snapshot currently points at this bill.
	// The UI uses this to hide destructive delete actions and direct users to
	// the route-change flow instead.
	ShopeeRealtimeLinked bool `json:"shopee_realtime_linked,omitempty"`
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
	ID             string          `json:"id"`
	BillID         string          `json:"bill_id"`
	RawName        string          `json:"raw_name"`
	SourceSKU      string          `json:"source_sku,omitempty"`
	SourceImageURL string          `json:"source_image_url,omitempty"`
	ItemCode       *string         `json:"item_code,omitempty"`
	HasHiddenChars bool            `json:"has_hidden_chars"`
	CleanItemCode  string          `json:"clean_item_code,omitempty"`
	Qty            float64         `json:"qty"`
	UnitCode       *string         `json:"unit_code,omitempty"`
	Price          *float64        `json:"price,omitempty"`
	DiscountAmount float64         `json:"discount_amount"`
	Mapped         bool            `json:"mapped"`
	MappingID      *string         `json:"mapping_id,omitempty"`
	Candidates     json.RawMessage `json:"candidates,omitempty"` // top-5 catalog matches
}

const ShopeeShippingSourceSKU = "__shopee_shipping__"

type BillListFilter struct {
	Status         string `form:"status"`
	Source         string `form:"source"`
	BillType       string `form:"bill_type"`
	DocumentRoute  string `form:"document_route"`
	EmailAccountID string `form:"email_account_id"`
	ShopeeStatus   string `form:"shopee_status"`
	ShopeeShopID   string `form:"shopee_shop_id"`
	Search         string `form:"search"`
	Archived       string `form:"archived"` // ""/"active" | "include" | "only"
	DateFrom       string `form:"date_from"`
	DateTo         string `form:"date_to"`
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
