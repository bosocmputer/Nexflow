package models

import "time"

// ChatQuickReply is one saved reply template the admin can inject into the
// composer (Phase 4.4). Keep it small — label + body + sort.
type ChatQuickReply struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Body      string    `json:"body"`
	SortOrder int       `json:"sort_order"`
	CreatedBy *string   `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChatQuickReplyUpsert is the admin-supplied payload for POST/PUT.
type ChatQuickReplyUpsert struct {
	Label     string `json:"label" binding:"required"`
	Body      string `json:"body" binding:"required"`
	SortOrder int    `json:"sort_order"`
}
