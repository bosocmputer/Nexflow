package models

import "time"

type LineNotificationRecipient struct {
	ID              string     `json:"id"`
	LineOAID        string     `json:"line_oa_id"`
	LineOAName      string     `json:"line_oa_name,omitempty"`
	Name            string     `json:"name"`
	DestinationType string     `json:"destination_type"`
	DestinationID   string     `json:"destination_id"`
	Enabled         bool       `json:"enabled"`
	LastTestAt      *time.Time `json:"last_test_at,omitempty"`
	LastTestStatus  string     `json:"last_test_status"`
	LastTestError   string     `json:"last_test_error"`
	LastSentAt      *time.Time `json:"last_sent_at,omitempty"`
	LastError       string     `json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type LineNotificationRecipientUpsert struct {
	LineOAID        string `json:"line_oa_id" binding:"required"`
	Name            string `json:"name" binding:"required"`
	DestinationType string `json:"destination_type"`
	DestinationID   string `json:"destination_id" binding:"required"`
	Enabled         *bool  `json:"enabled"`
}

type LineNotificationDelivery struct {
	ID          string     `json:"id"`
	RecipientID string     `json:"recipient_id"`
	Recipient   string     `json:"recipient,omitempty"`
	LineOAID    string     `json:"line_oa_id"`
	LineOAName  string     `json:"line_oa_name,omitempty"`
	Source      string     `json:"source"`
	Severity    string     `json:"severity"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	ActionURL   string     `json:"action_url"`
	EntityType  string     `json:"entity_type"`
	EntityID    string     `json:"entity_id"`
	DedupeKey   string     `json:"dedupe_key,omitempty"`
	MessageText string     `json:"message_text,omitempty"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error"`
	NextRunAt   time.Time  `json:"next_run_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type LineNotificationMessageInput struct {
	Source      string
	Severity    string
	Title       string
	Body        string
	ActionURL   string
	EntityType  string
	EntityID    string
	DedupeKey   string
	MessageText string
}

type LineNotificationDeliveryJob struct {
	LineNotificationDelivery
	DestinationType    string
	DestinationID      string
	ChannelSecret      string
	ChannelAccessToken string
}
