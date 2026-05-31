package models

import "time"

// Phase 4.8 — chat_notes — admin-only annotations on a conversation.
// Never sent to LINE; visible to all admin/staff users (no per-admin
// privacy in v1).
type ChatNote struct {
	ID         string    `json:"id"`
	LineUserID string    `json:"line_user_id"`
	Body       string    `json:"body"`
	CreatedBy  *string   `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ChatNoteUpsert struct {
	Body string `json:"body" binding:"required"`
}

// Phase 4.9 — chat_tags — global label list (admin-curated).
type ChatTag struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

type ChatTagUpsert struct {
	Label string `json:"label" binding:"required"`
	Color string `json:"color"` // gray/red/orange/yellow/green/blue/purple
}
