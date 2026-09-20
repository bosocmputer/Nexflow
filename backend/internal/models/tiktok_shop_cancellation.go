package models

import (
	"encoding/json"
	"time"
)

type TikTokSMLCancellation struct {
	ID                    string          `json:"id"`
	ShopID                string          `json:"shop_id"`
	OrderID               string          `json:"order_id"`
	BillID                string          `json:"bill_id"`
	SMLAttemptID          string          `json:"sml_attempt_id"`
	SaleSMLDocNo          string          `json:"sale_sml_doc_no"`
	CancelSMLDocNo        string          `json:"cancel_sml_doc_no,omitempty"`
	Status                string          `json:"status"`
	SourceHash            string          `json:"-"`
	ReviewDigest          string          `json:"review_digest,omitempty"`
	RouteEndpoint         string          `json:"-"`
	RouteConfigVersion    int64           `json:"route_config_version"`
	RouteSignature        string          `json:"-"`
	RequestPayload        json.RawMessage `json:"-"`
	Response              json.RawMessage `json:"-"`
	ErrorCode             string          `json:"error_code,omitempty"`
	ErrorMessage          string          `json:"error_message,omitempty"`
	CreatedBy             *string         `json:"created_by,omitempty"`
	StockRecalcStatus     string          `json:"stock_recalc_status"`
	StockRecalcError      string          `json:"stock_recalc_error,omitempty"`
	StockRecalcAttempts   int             `json:"stock_recalc_attempts"`
	StockRecalcNextRunAt  *time.Time      `json:"stock_recalc_next_run_at,omitempty"`
	StockRecalcLeaseUntil *time.Time      `json:"stock_recalc_lease_until,omitempty"`
	CompletedAt           *time.Time      `json:"completed_at,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}
