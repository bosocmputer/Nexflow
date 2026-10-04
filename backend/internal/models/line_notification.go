package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	LineNotificationEventShopeeOrderNew               = "shopee.order.new"
	LineNotificationEventTikTokOrderNew               = "tiktok.order.new"
	LineNotificationEventNextStepOrderNew             = "nextstep.order.new"
	LineNotificationEventShopeeOrderCancelled         = "shopee.order.cancelled"
	LineNotificationEventTikTokOrderCancelled         = "tiktok.order.cancelled"
	LineNotificationEventShopeeSMLSuccess             = "shopee.sml.auto_success"
	LineNotificationEventTikTokSMLSuccess             = "tiktok.sml.auto_success"
	LineNotificationEventShopeeSMLReview              = "shopee.sml.needs_review"
	LineNotificationEventTikTokSMLReview              = "tiktok.sml.needs_review"
	LineNotificationEventShopeeSMLFailed              = "shopee.sml.failed"
	LineNotificationEventTikTokSMLFailed              = "tiktok.sml.failed"
	LineNotificationEventShopeeSMLCancellationCreated = "shopee.sml.cancellation_created"
	LineNotificationEventShopeeSettlementReady        = "shopee.settlement.ready"
)

type LineNotificationEventDefinition struct {
	Key            string `json:"key"`
	Source         string `json:"source"`
	Group          string `json:"group"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	DefaultEnabled bool   `json:"default_enabled"`
	SupportsFlex   bool   `json:"supports_flex"`
}

type LineNotificationSample struct {
	EventKey    string          `json:"event_key"`
	AltText     string          `json:"alt_text"`
	MessageText string          `json:"message_text"`
	FlexPayload json.RawMessage `json:"flex_payload"`
}

var lineNotificationEventCatalog = []LineNotificationEventDefinition{
	{Key: LineNotificationEventShopeeOrderNew, Source: "shopee", Group: "orders", Label: "ออเดอร์ Shopee ใหม่", Description: "แจ้งเมื่อ Nexflow รับคำสั่งซื้อใหม่จาก Shopee", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventTikTokOrderNew, Source: "tiktok_shop", Group: "orders", Label: "ออเดอร์ TikTok ใหม่", Description: "แจ้งเมื่อ Nexflow รับคำสั่งซื้อใหม่จาก TikTok Shop", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventNextStepOrderNew, Source: "nextstep_marketplace", Group: "orders", Label: "ออเดอร์ NextStep ใหม่", Description: "แจ้งเมื่อ Nexflow พบคำสั่งซื้อใหม่จาก NextStep Marketplace", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventShopeeOrderCancelled, Source: "shopee", Group: "cancellations", Label: "Shopee ยกเลิกออเดอร์", Description: "เตือนเมื่อ Shopee ยืนยันว่าออเดอร์ถูกยกเลิก", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventTikTokOrderCancelled, Source: "tiktok_shop", Group: "cancellations", Label: "TikTok ยกเลิกออเดอร์", Description: "เตือนเมื่อ TikTok Shop ยืนยันว่าออเดอร์ถูกยกเลิก", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventShopeeSMLSuccess, Source: "shopee", Group: "sml", Label: "Shopee ส่ง SML สำเร็จ", Description: "แจ้งผลเมื่อ Auto SML สร้างเอกสารสำเร็จ", SupportsFlex: true},
	{Key: LineNotificationEventTikTokSMLSuccess, Source: "tiktok_shop", Group: "sml", Label: "TikTok ส่ง SML สำเร็จ", Description: "แจ้งผลเมื่อ Auto SML สร้างเอกสารสำเร็จ", SupportsFlex: true},
	{Key: LineNotificationEventShopeeSMLReview, Source: "shopee", Group: "sml", Label: "Shopee ต้องตรวจ SML", Description: "เตือนเมื่อข้อมูลยังไม่พร้อมและต้องให้ผู้ใช้ตรวจ", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventTikTokSMLReview, Source: "tiktok_shop", Group: "sml", Label: "TikTok ต้องตรวจ SML", Description: "เตือนเมื่อข้อมูลยังไม่พร้อมและต้องให้ผู้ใช้ตรวจ", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventShopeeSMLFailed, Source: "shopee", Group: "sml", Label: "Shopee ส่ง SML ไม่สำเร็จ", Description: "แจ้งข้อผิดพลาดปลายทางที่ต้องดำเนินการ", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventTikTokSMLFailed, Source: "tiktok_shop", Group: "sml", Label: "TikTok ส่ง SML ไม่สำเร็จ", Description: "แจ้งข้อผิดพลาดปลายทางที่ต้องดำเนินการ", DefaultEnabled: true, SupportsFlex: true},
	{Key: LineNotificationEventShopeeSMLCancellationCreated, Source: "shopee", Group: "cancellations", Label: "สร้างเอกสารหลังยกเลิก Shopee", Description: "แจ้งเมื่อสร้างเอกสารยกเลิกหรือรับคืนใน SML สำเร็จ", SupportsFlex: true},
	{Key: LineNotificationEventShopeeSettlementReady, Source: "shopee", Group: "settlements", Label: "รับชำระ Shopee พร้อมตรวจ", Description: "แจ้งเมื่อรอบรับชำระ Shopee พร้อมให้ตรวจยอด", SupportsFlex: true},
}

func LineNotificationEventCatalog() []LineNotificationEventDefinition {
	out := make([]LineNotificationEventDefinition, len(lineNotificationEventCatalog))
	copy(out, lineNotificationEventCatalog)
	return out
}

func AllLineNotificationEventKeys() []string {
	out := make([]string, 0, len(lineNotificationEventCatalog))
	for _, event := range lineNotificationEventCatalog {
		out = append(out, event.Key)
	}
	return out
}

func DefaultLineNotificationEventKeys() []string {
	out := make([]string, 0, len(lineNotificationEventCatalog))
	for _, event := range lineNotificationEventCatalog {
		if event.DefaultEnabled {
			out = append(out, event.Key)
		}
	}
	return out
}

func NormalizeLineNotificationEventKeys(keys []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(lineNotificationEventCatalog))
	for _, event := range lineNotificationEventCatalog {
		allowed[event.Key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, value := range keys {
		key := strings.TrimSpace(value)
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("unsupported LINE notification event %q", key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out, nil
}

type LineNotificationRecipient struct {
	ID              string     `json:"id"`
	LineOAID        string     `json:"line_oa_id"`
	LineOAName      string     `json:"line_oa_name,omitempty"`
	Name            string     `json:"name"`
	DestinationType string     `json:"destination_type"`
	DestinationID   string     `json:"destination_id"`
	Enabled         bool       `json:"enabled"`
	EventKeys       []string   `json:"event_keys"`
	LastTestAt      *time.Time `json:"last_test_at,omitempty"`
	LastTestStatus  string     `json:"last_test_status"`
	LastTestError   string     `json:"last_test_error"`
	LastSentAt      *time.Time `json:"last_sent_at,omitempty"`
	LastError       string     `json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type LineNotificationRecipientUpsert struct {
	LineOAID        string    `json:"line_oa_id" binding:"required"`
	Name            string    `json:"name" binding:"required"`
	DestinationType string    `json:"destination_type"`
	DestinationID   string    `json:"destination_id" binding:"required"`
	Enabled         *bool     `json:"enabled"`
	EventKeys       *[]string `json:"event_keys"`
}

type LineNotificationContactCandidate struct {
	ID                 string    `json:"id"`
	LineOAID           string    `json:"line_oa_id"`
	LineOAName         string    `json:"line_oa_name,omitempty"`
	DestinationType    string    `json:"destination_type"`
	DestinationID      string    `json:"destination_id"`
	DisplayName        string    `json:"display_name"`
	LastMessagePreview string    `json:"last_message_preview"`
	LastWebhookEventID string    `json:"last_webhook_event_id"`
	IsRecipient        bool      `json:"is_recipient"`
	RecipientID        string    `json:"recipient_id,omitempty"`
	LastSeenAt         time.Time `json:"last_seen_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type LineNotificationContactCandidateUpsert struct {
	LineOAID           string
	DestinationType    string
	DestinationID      string
	DisplayName        string
	LastMessagePreview string
	LastWebhookEventID string
	LastSeenAt         time.Time
}

type LineNotificationCandidateAddRecipientInput struct {
	Name      string    `json:"name"`
	Enabled   *bool     `json:"enabled"`
	EventKeys *[]string `json:"event_keys"`
}

type LineNotificationDelivery struct {
	ID             string          `json:"id"`
	RecipientID    string          `json:"recipient_id"`
	Recipient      string          `json:"recipient,omitempty"`
	LineOAID       string          `json:"line_oa_id"`
	LineOAName     string          `json:"line_oa_name,omitempty"`
	EventKey       string          `json:"event_key"`
	Source         string          `json:"source"`
	Severity       string          `json:"severity"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	ActionURL      string          `json:"action_url"`
	EntityType     string          `json:"entity_type"`
	EntityID       string          `json:"entity_id"`
	DedupeKey      string          `json:"dedupe_key,omitempty"`
	MessageText    string          `json:"message_text,omitempty"`
	AltText        string          `json:"alt_text,omitempty"`
	FlexPayload    json.RawMessage `json:"flex_payload,omitempty"`
	PayloadVersion int             `json:"payload_version,omitempty"`
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"last_error"`
	NextRunAt      time.Time       `json:"next_run_at"`
	SentAt         *time.Time      `json:"sent_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type LineNotificationMessageInput struct {
	EventKey       string
	Source         string
	Severity       string
	Title          string
	Body           string
	ActionURL      string
	EntityType     string
	EntityID       string
	DedupeKey      string
	MessageText    string
	AltText        string
	FlexPayload    json.RawMessage
	PayloadVersion int
}

// TikTokShopNewOrderNotification is deliberately PII-free. It contains only
// the order, amount, and product evidence already stored in the typed TikTok
// snapshot so LINE delivery can never inherit recipient or buyer fields.
type TikTokShopNewOrderNotification struct {
	ShopID                string
	ShopName              string
	OrderID               string
	OrderStatus           string
	Currency              string
	PaymentTotalAmount    string
	ProductSubtotalAmount string
	ShippingFeeAmount     string
	ItemCount             int
	SKUCount              int
	CreatedAt             time.Time
	ObservationSource     string
	Items                 []TikTokShopNewOrderNotificationItem
}

type TikTokShopNewOrderNotificationItem struct {
	ProductName string
	VariantName string
	Quantity    int
}

// TikTokShopOrderCancellationNotification intentionally excludes buyer data.
// It carries only the operational evidence needed for a cancellation alert.
type TikTokShopOrderCancellationNotification struct {
	ShopID             string
	ShopName           string
	OrderID            string
	Currency           string
	PaymentTotalAmount string
	ItemCount          int
	SKUCount           int
	SMLDocNo           string
	OrderUpdatedAt     time.Time
}

type LineNotificationDeliveryJob struct {
	LineNotificationDelivery
	DestinationType    string
	DestinationID      string
	ChannelSecret      string
	ChannelAccessToken string
}
