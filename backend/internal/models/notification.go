package models

import "time"

type Notification struct {
	ID          string     `json:"id"`
	RecipientID string     `json:"recipient_id,omitempty"`
	Source      string     `json:"source"`
	Severity    string     `json:"severity"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	ActionURL   string     `json:"action_url"`
	EntityType  string     `json:"entity_type"`
	EntityID    string     `json:"entity_id"`
	DedupeKey   string     `json:"dedupe_key,omitempty"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type NotificationInput struct {
	Source     string
	Severity   string
	Title      string
	Body       string
	ActionURL  string
	EntityType string
	EntityID   string
	DedupeKey  string
}

type NotificationFilter struct {
	UnreadOnly bool
	Limit      int
}
