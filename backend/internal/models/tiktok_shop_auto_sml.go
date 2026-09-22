package models

import "time"

const (
	TikTokAutoSMLTriggerAwaitingCollection = "AWAITING_COLLECTION"

	TikTokAutoSMLQueued      = "queued"
	TikTokAutoSMLRunning     = "running"
	TikTokAutoSMLRetryWait   = "retry_wait"
	TikTokAutoSMLNeedsReview = "needs_review"
	TikTokAutoSMLSucceeded   = "succeeded"
	TikTokAutoSMLFailed      = "failed"
	TikTokAutoSMLCancelled   = "cancelled"
)

type TikTokAutoSMLSetting struct {
	ShopID                    string     `json:"shop_id"`
	ShopName                  string     `json:"shop_name"`
	Enabled                   bool       `json:"enabled"`
	TriggerStatus             string     `json:"trigger_status"`
	ConfigVersion             int64      `json:"config_version"`
	EligibleAfter             *time.Time `json:"eligible_after,omitempty"`
	RouteSignature            string     `json:"-"`
	EnabledBy                 *string    `json:"-"`
	EnabledAt                 *time.Time `json:"enabled_at,omitempty"`
	PausedReason              string     `json:"paused_reason,omitempty"`
	PausedAt                  *time.Time `json:"paused_at,omitempty"`
	ConsecutiveSystemFailures int        `json:"consecutive_system_failures"`
	LastSuccessAt             *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt             *time.Time `json:"last_failure_at,omitempty"`
	QueuedCount               int64      `json:"queued_count"`
	NeedsReviewCount          int64      `json:"needs_review_count"`
	FailedCount               int64      `json:"failed_count"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type TikTokAutoSMLJob struct {
	ID                    string
	ShopID                string
	OrderID               string
	Status                string
	TriggerStatusSnapshot string
	TriggerTransitionAt   time.Time
	TriggerConfigVersion  int64
	SourceHash            string
	BillFingerprint       string
	RouteSignature        string
	BillID                *string
	SMLDocNo              string
	ReviewDigest          string
	DocumentTime          string
	Attempts              int
	NextRunAt             time.Time
	LeaseUntil            *time.Time
	LastErrorCode         string
	LastErrorMessage      string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// TikTokAutoSMLNotification contains only the order and document evidence
// required for an internal LINE notification. Buyer and shipment PII must not
// be copied into this payload.
type TikTokAutoSMLNotification struct {
	ShopID       string
	ShopName     string
	OrderID      string
	BillID       string
	SMLDocNo     string
	Currency     string
	TotalAmount  float64
	ItemCount    int
	Items        []TikTokShopNewOrderNotificationItem
	ErrorCode    string
	ErrorMessage string
}

func TikTokAutoSMLAllowsStatus(status string) bool {
	switch status {
	case "AWAITING_COLLECTION", "IN_TRANSIT", "DELIVERED", "COMPLETED":
		return true
	default:
		return false
	}
}

func TikTokAutoSMLStopStatus(status string) bool {
	return status == "CANCELLED"
}
