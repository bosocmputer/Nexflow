package models

import (
	"encoding/json"
	"time"
)

const LineMyShopSource = "line_myshop"

type LineMyShopConnection struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	ChannelID        *int64     `json:"channel_id,omitempty"`
	PremiumID        string     `json:"premium_id,omitempty"`
	RandomID         string     `json:"random_id,omitempty"`
	Enabled          bool       `json:"enabled"`
	HasAPIKey        bool       `json:"has_api_key"`
	HasWebhookSecret bool       `json:"has_webhook_secret"`
	WebhookURL       string     `json:"webhook_url,omitempty"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type LineMyShopConnectionSecret struct {
	LineMyShopConnection
	APIKey        string
	WebhookSecret string
}

type LineMyShopConnectionUpsert struct {
	Name               string `json:"name" binding:"required"`
	APIKey             string `json:"api_key"`
	WebhookSecret      string `json:"webhook_secret"`
	ClearWebhookSecret bool   `json:"clear_webhook_secret"`
	ChannelID          *int64 `json:"channel_id"`
	PremiumID          string `json:"premium_id"`
	RandomID           string `json:"random_id"`
	Enabled            *bool  `json:"enabled"`
}

type LineMyShopOrderSnapshot struct {
	ID             string          `json:"id"`
	ConnectionID   string          `json:"connection_id"`
	ConnectionName string          `json:"connection_name,omitempty"`
	OrderNo        string          `json:"order_no"`
	OrderStatus    string          `json:"order_status"`
	PaymentStatus  string          `json:"payment_status"`
	ShipmentStatus string          `json:"shipment_status"`
	PaymentMethod  string          `json:"payment_method"`
	TotalAmount    float64         `json:"total_amount"`
	SubtotalPrice  float64         `json:"subtotal_price"`
	ShipmentPrice  float64         `json:"shipment_price"`
	DiscountAmount float64         `json:"discount_amount"`
	ItemCount      int             `json:"item_count"`
	RawDetail      json.RawMessage `json:"raw_detail,omitempty"`
	RawWebhook     json.RawMessage `json:"raw_webhook,omitempty"`
	BillID         *string         `json:"bill_id,omitempty"`
	SMLDocNo       string          `json:"sml_doc_no,omitempty"`
	LastEventName  string          `json:"last_event_name,omitempty"`
	LastEventAt    *time.Time      `json:"last_event_at,omitempty"`
	LastSyncedAt   *time.Time      `json:"last_synced_at,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type LineMyShopWebhookEvent struct {
	ID               string          `json:"id"`
	ConnectionID     string          `json:"connection_id"`
	OrderNo          string          `json:"order_no"`
	RequestID        string          `json:"request_id"`
	EventName        string          `json:"event_name"`
	EventAt          *time.Time      `json:"event_at,omitempty"`
	DedupeKey        string          `json:"dedupe_key"`
	SignatureValid   bool            `json:"signature_valid"`
	RawPayload       json.RawMessage `json:"raw_payload,omitempty"`
	ProcessingStatus string          `json:"processing_status"`
	Error            string          `json:"error,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	ProcessedAt      *time.Time      `json:"processed_at,omitempty"`
}
