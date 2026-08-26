package models

import "time"

const (
	ShopeeAutoSMLQueued      = "queued"
	ShopeeAutoSMLRunning     = "running"
	ShopeeAutoSMLRetryWait   = "retry_wait"
	ShopeeAutoSMLNeedsReview = "needs_review"
	ShopeeAutoSMLSucceeded   = "succeeded"
	ShopeeAutoSMLFailed      = "failed"
	ShopeeAutoSMLCancelled   = "cancelled"
)

type ShopeeAutoSMLSetting struct {
	ShopID                    int64      `json:"shop_id"`
	ShopLabel                 string     `json:"shop_label,omitempty"`
	Enabled                   bool       `json:"enabled"`
	EligibleAfter             *time.Time `json:"eligible_after,omitempty"`
	RouteSignature            string     `json:"-"`
	EnabledBy                 *string    `json:"enabled_by,omitempty"`
	EnabledAt                 *time.Time `json:"enabled_at,omitempty"`
	PausedReason              string     `json:"paused_reason,omitempty"`
	PausedAt                  *time.Time `json:"paused_at,omitempty"`
	ConsecutiveSystemFailures int        `json:"consecutive_system_failures"`
	LastSuccessAt             *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt             *time.Time `json:"last_failure_at,omitempty"`
	QueuedCount               int        `json:"queued_count"`
	NeedsReviewCount          int        `json:"needs_review_count"`
	FailedCount               int        `json:"failed_count"`
	OldestQueuedAt            *time.Time `json:"oldest_queued_at,omitempty"`
	OperationalWarning        string     `json:"operational_warning,omitempty"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type ShopeeAutoSMLJob struct {
	ID               string     `json:"id"`
	ShopID           int64      `json:"shop_id"`
	OrderSN          string     `json:"order_sn"`
	BillID           *string    `json:"bill_id,omitempty"`
	SMLDocNo         string     `json:"sml_doc_no,omitempty"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	NextRunAt        time.Time  `json:"next_run_at"`
	LeaseUntil       *time.Time `json:"lease_until,omitempty"`
	OrderCreateTime  time.Time  `json:"order_create_time"`
	OrderUpdateTime  *time.Time `json:"order_update_time,omitempty"`
	BillFingerprint  string     `json:"-"`
	RouteSignature   string     `json:"-"`
	DocumentTime     string     `json:"document_time,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ShopeeAutoSMLJobView struct {
	Status           string     `json:"status,omitempty"`
	ErrorCode        string     `json:"error_code,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	ManualSendLocked bool       `json:"manual_send_locked"`
}

type ShopeeAutoSMLNotification struct {
	ShopID       int64
	ShopLabel    string
	OrderSN      string
	BillID       string
	SMLDocNo     string
	TotalAmount  float64
	ItemCount    int
	Items        []ShopeeAutoSMLNotificationItem
	ErrorCode    string
	ErrorMessage string
}

// ShopeeAutoSMLNotificationItem contains only the non-PII order fields needed
// by the LINE notification. Marketplace quantity is intentionally preserved;
// conversion into SML quantity is a separate document concern.
type ShopeeAutoSMLNotificationItem struct {
	Name    string
	Variant string
	Qty     float64
}
