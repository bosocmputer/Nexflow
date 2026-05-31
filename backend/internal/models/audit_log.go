package models

import (
	"encoding/json"
	"time"
)

type AuditLog struct {
	ID         string          `json:"id"`
	UserID     *string         `json:"user_id,omitempty"`
	Actor      *AuditActor     `json:"actor,omitempty"`
	Action     string          `json:"action"`
	TargetID   *string         `json:"target_id,omitempty"`
	Source     string          `json:"source,omitempty"`
	Level      string          `json:"level,omitempty"`
	DurationMs *int            `json:"duration_ms,omitempty"`
	TraceID    string          `json:"trace_id,omitempty"`
	Detail     json.RawMessage `json:"detail,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type AuditActor struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Role  string `json:"role,omitempty"`
	Type  string `json:"type"` // user, system, worker
}

// AuditEntry is the input for writing a new audit log entry.
type AuditEntry struct {
	Action     string
	TargetID   *string
	UserID     *string
	Source     string // "line", "email", "shopee_excel", "sml", "system"
	Level      string // "info", "warn", "error" — defaults to "info" if empty
	DurationMs *int   // optional performance metric in milliseconds
	TraceID    string // per-request or per-job trace ID
	Detail     interface{}
}

type AuditLogFilter struct {
	Source       string `form:"source"`        // line, email, shopee_excel, etc.
	Level        string `form:"level"`         // info, warn, error
	Action       string `form:"action"`        // e.g. bill_created
	UserID       string `form:"user_id"`       // actor user id
	DateFrom     string `form:"date_from"`     // YYYY-MM-DD
	DateTo       string `form:"date_to"`       // YYYY-MM-DD
	Cursor       string `form:"cursor"`        // opaque cursor for keyset pagination
	Limit        int    `form:"limit"`         // preferred keyset page size
	CursorMode   bool   `form:"-"`             // set by handler when cursor/limit query is present
	IncludeTotal bool   `form:"include_total"` // opt-in COUNT(*) for admin/export
	Page         int    `form:"page,default=1"`
	PageSize     int    `form:"page_size,default=50"`
}
