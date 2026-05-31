package models

import "time"

// Direction values for chat_messages.direction.
const (
	ChatDirectionIncoming = "incoming"
	ChatDirectionOutgoing = "outgoing"
	ChatDirectionSystem   = "system"
)

// Kind values for chat_messages.kind.
const (
	ChatKindText   = "text"
	ChatKindImage  = "image"
	ChatKindFile   = "file"
	ChatKindAudio  = "audio"
	ChatKindSystem = "system"
)

// Delivery status for outgoing messages.
const (
	ChatDeliverySent    = "sent"
	ChatDeliveryFailed  = "failed"
	ChatDeliveryPending = "pending"
)

// Delivery method for outgoing messages — "reply" uses LINE's free Reply API
// (single-use replyToken from a recent inbound webhook); "push" uses Push API
// which counts toward the monthly quota (200/month free OA plan).
const (
	ChatDeliveryMethodReply = "reply"
	ChatDeliveryMethodPush  = "push"
)

// ChatConversation is one LINE user we've talked to. PK is the LINE userID
// (Uxxxxxxxx). display_name + picture_url come from LINE's /v2/bot/profile
// endpoint and are refreshed when the row is first created (and manually by
// the admin via UI).
// Conversation lifecycle status (Phase 4.2)
const (
	ChatStatusOpen     = "open"
	ChatStatusResolved = "resolved"
	ChatStatusArchived = "archived"
)

type ChatConversation struct {
	LineUserID        string     `json:"line_user_id"`
	// LineOAID points at line_oa_accounts.id — tells backend which OA's
	// access_token to use when admin replies. Nullable for legacy rows
	// created before migration 014.
	LineOAID          *string    `json:"line_oa_id,omitempty"`
	DisplayName       string     `json:"display_name"`
	PictureURL        string     `json:"picture_url"`
	// Phone — saved by admin via "บันทึกเบอร์" button on detected phone in
	// incoming messages (Phase 4.7). Empty when not yet captured.
	Phone             string     `json:"phone"`
	// Status is the lifecycle state:
	//   open      — active, shows in default inbox
	//   resolved  — admin marked done; auto-reverts to open on new inbound
	//   archived  — sticky (no auto-revive); for spam/blocked threads
	Status            string     `json:"status"`
	LastMessageAt     time.Time  `json:"last_message_at"`
	LastInboundAt     *time.Time `json:"last_inbound_at,omitempty"`
	LastAdminReplyAt  *time.Time `json:"last_admin_reply_at,omitempty"`
	UnreadAdminCount  int        `json:"unread_admin_count"`
	CreatedAt         time.Time  `json:"created_at"`
}

// ChatMessage is one event in a conversation — incoming from the LINE user,
// outgoing reply pushed by an admin, or a system note (e.g. "บิลถูกสร้างแล้ว").
//
// For media messages (kind=image/file/audio) the binary lives in chat_media,
// joined by message_id; text_content is empty and line_message_id holds the
// LINE inbound ID we used to download it.
type ChatMessage struct {
	ID             string     `json:"id"`
	LineUserID     string     `json:"line_user_id"`
	Direction      string     `json:"direction"`
	Kind           string     `json:"kind"`
	TextContent    string     `json:"text_content"`
	LineMessageID  string     `json:"line_message_id,omitempty"`
	LineEventTS    *int64     `json:"line_event_ts,omitempty"`
	SenderAdminID  *string    `json:"sender_admin_id,omitempty"`
	DeliveryStatus string     `json:"delivery_status"`
	DeliveryMethod string     `json:"delivery_method,omitempty"`
	DeliveryError  string     `json:"delivery_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	// Media is populated by ListByUser when the message is an image/file/audio.
	Media          *ChatMedia `json:"media,omitempty"`
}

// ChatMedia stores attachment bytes as a file under cfg.ArtifactsDir/chat-media/...
// SHA-256 lets us dedupe identical uploads.
type ChatMedia struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"message_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	StoragePath string    `json:"storage_path"`
	CreatedAt   time.Time `json:"created_at"`
}
