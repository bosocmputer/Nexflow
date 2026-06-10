package models

import (
	"encoding/json"
	"strings"
	"time"
)

type ShopeeOrderSnapshot struct {
	ID                string                        `json:"id"`
	ConnectionID      *string                       `json:"connection_id,omitempty"`
	ShopID            int64                         `json:"shop_id"`
	ShopLabel         string                        `json:"shop_label"`
	OrderSN           string                        `json:"order_sn"`
	OrderStatus       string                        `json:"order_status"`
	ERPStatus         string                        `json:"erp_status"`
	BillID            *string                       `json:"bill_id,omitempty"`
	SMLDocNo          string                        `json:"sml_doc_no,omitempty"`
	DocumentRoute     string                        `json:"document_route,omitempty"`
	BillSourceFlow    string                        `json:"bill_source_flow,omitempty"`
	BuyerUsername     string                        `json:"buyer_username,omitempty"`
	TotalAmount       float64                       `json:"total_amount"`
	Currency          string                        `json:"currency,omitempty"`
	ItemCount         int                           `json:"item_count"`
	PackageNumber     string                        `json:"package_number,omitempty"`
	LogisticsStatus   string                        `json:"logistics_status,omitempty"`
	TrackingNumber    string                        `json:"tracking_number,omitempty"`
	ShippingCarrier   string                        `json:"shipping_carrier,omitempty"`
	CheckoutCarrier   string                        `json:"checkout_shipping_carrier,omitempty"`
	PaymentMethod     string                        `json:"payment_method,omitempty"`
	RawDetail         json.RawMessage               `json:"raw_detail,omitempty"`
	ShippingTracking  []ShopeeShippingTrackingEvent `json:"shipping_tracking,omitempty"`
	ShipActionStatus  string                        `json:"ship_action_status,omitempty"`
	LastUpdateSource  string                        `json:"last_update_source,omitempty"`
	LastOrderUpdateAt *time.Time                    `json:"last_order_update_at,omitempty"`
	LastSyncedAt      time.Time                     `json:"last_synced_at"`
	LastError         string                        `json:"last_error,omitempty"`
	CreatedAt         time.Time                     `json:"created_at"`
	UpdatedAt         time.Time                     `json:"updated_at"`
}

type ShopeeOrderSnapshotFilter struct {
	ShopID      int64
	Status      string
	StatusGroup string
	ERPStatus   string
	Search      string
	Page        int
	PageSize    int
}

type ShopeeRealtimeCounts struct {
	Total       int            `json:"total"`
	NewOrders   int            `json:"new_orders"`
	PendingERP  int            `json:"pending_erp"`
	NeedsReview int            `json:"needs_review"`
	ERPSaved    int            `json:"erp_saved"`
	WaitingShip int            `json:"waiting_ship"`
	Shipped     int            `json:"shipped"`
	Cancelled   int            `json:"cancelled"`
	Failed      int            `json:"failed"`
	Tabs        map[string]int `json:"tabs,omitempty"`
}

type ShopeeRealtimeDiagnosticEvent struct {
	ID                  string     `json:"id"`
	ShopID              int64      `json:"shop_id"`
	ShopLabel           string     `json:"shop_label,omitempty"`
	OrderSN             string     `json:"order_sn"`
	PushCode            int        `json:"push_code"`
	PushName            string     `json:"push_name"`
	Source              string     `json:"source"`
	EventStatus         string     `json:"event_status"`
	ProcessingStatus    string     `json:"processing_status"`
	ReconcileStatus     string     `json:"reconcile_status,omitempty"`
	ReconcileError      string     `json:"reconcile_error,omitempty"`
	Error               string     `json:"error,omitempty"`
	IsVerificationEvent bool       `json:"is_verification_event"`
	ReceivedAt          time.Time  `json:"received_at"`
	ProcessedAt         *time.Time `json:"processed_at,omitempty"`
}

type ShopeeReconcileJob struct {
	ID        string    `json:"id"`
	ShopID    int64     `json:"shop_id"`
	OrderSN   string    `json:"order_sn"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ShopeeActionOutbox struct {
	ID             string     `json:"id"`
	ShopID         int64      `json:"shop_id"`
	OrderSN        string     `json:"order_sn"`
	Action         string     `json:"action"`
	IdempotencyKey string     `json:"idempotency_key"`
	Status         string     `json:"status"`
	BillID         *string    `json:"bill_id,omitempty"`
	SMLDocNo       string     `json:"sml_doc_no,omitempty"`
	Error          string     `json:"error,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedBy      *string    `json:"created_by,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type ShopeeShippingTrackingEvent struct {
	UpdateTime      int64  `json:"update_time"`
	Description     string `json:"description"`
	LogisticsStatus string `json:"logistics_status"`
	ReturnCode      string `json:"return_code,omitempty"`
}

type ShopeeOrderTimelineEvent struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ShopeeOrderStatusTimelineStep struct {
	Key        string     `json:"key"`
	Status     string     `json:"status"`
	Label      string     `json:"label"`
	Detail     string     `json:"detail,omitempty"`
	State      string     `json:"state"`
	Source     string     `json:"source"`
	Confidence string     `json:"confidence"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
	Current    bool       `json:"current"`
	Terminal   bool       `json:"terminal,omitempty"`
}

type ShopeeOrderERPMilestone struct {
	Key        string     `json:"key"`
	Label      string     `json:"label"`
	Detail     string     `json:"detail,omitempty"`
	State      string     `json:"state"`
	Source     string     `json:"source"`
	Confidence string     `json:"confidence"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

func NormalizeShopeeOrderStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "TO_CONFIRM_RECEIVE":
		return "SHIPPED"
	case "IN_CANCEL":
		return "CANCELLED"
	default:
		return strings.ToUpper(strings.TrimSpace(status))
	}
}
