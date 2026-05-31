package models

import "time"

type IMAPPollDetail struct {
	UID         uint32 `json:"uid,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	Subject     string `json:"subject,omitempty"`
	From        string `json:"from,omitempty"`
	EmailDate   string `json:"email_date,omitempty"`
	Status      string `json:"status"` // processed | skipped
	ReasonCode  string `json:"reason_code,omitempty"`
	ReasonLabel string `json:"reason_label,omitempty"`
}

type IMAPPollSummary struct {
	Scanned          int  `json:"scanned"`
	Created          int  `json:"created"`
	AlreadyProcessed int  `json:"already_processed"`
	SkippedUser      int  `json:"skipped_user"`
	Failed           int  `json:"failed"`
	Interrupted      bool `json:"interrupted,omitempty"`
}

// IMAPAccount is one mailbox the email coordinator polls.
//
// Channel routes a poll's processed messages to the right body handler:
//   - "general" → attachment pipeline (PDF/Excel attached files)
//   - "shopee"  → Shopee email order/shipped flow (subject decides which)
//   - "lazada"  → reserved (Phase 4b WIP, currently routes like general)
type IMAPAccount struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	Host                string           `json:"host"`
	Port                int              `json:"port"`
	Username            string           `json:"username"`
	Password            string           `json:"password,omitempty"`
	Mailbox             string           `json:"mailbox"`
	FilterFrom          string           `json:"filter_from"`
	FilterSubjects      string           `json:"filter_subjects"`
	Channel             string           `json:"channel"`
	ShopeeDomains       string           `json:"shopee_domains"`
	LookbackDays        int              `json:"lookback_days"`
	PollIntervalSeconds int              `json:"poll_interval_seconds"`
	Enabled             bool             `json:"enabled"`
	LastPolledAt        *time.Time       `json:"last_polled_at"`
	LastPollStatus      *string          `json:"last_poll_status"`
	LastPollError       *string          `json:"last_poll_error"`
	LastPollMessages    *int             `json:"last_poll_messages"`
	LastPollFound       *int             `json:"last_poll_found"`
	LastPollProcessed   *int             `json:"last_poll_processed"`
	LastPollSkipped     *int             `json:"last_poll_skipped"`
	LastPollDetails     []IMAPPollDetail `json:"last_poll_details,omitempty"`
	LastPollSummary     IMAPPollSummary  `json:"last_poll_summary"`
	LastSeenUID         int64            `json:"last_seen_uid"`
	LastPollLimited     bool             `json:"last_poll_limited"`
	LastPollBacklog     *int             `json:"last_poll_backlog"`
	PollRunning         bool             `json:"poll_running,omitempty"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	LastAdminAlertAt    *time.Time       `json:"last_admin_alert_at"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

// IMAPAccountUpsert is the user-editable subset (no runtime status fields).
type IMAPAccountUpsert struct {
	Name                string `json:"name" binding:"required"`
	Host                string `json:"host" binding:"required"`
	Port                int    `json:"port" binding:"required,min=1,max=65535"`
	Username            string `json:"username" binding:"required"`
	Password            string `json:"password"`
	Mailbox             string `json:"mailbox"`
	FilterFrom          string `json:"filter_from"`
	FilterSubjects      string `json:"filter_subjects"`
	Channel             string `json:"channel" binding:"required,oneof=general shopee lazada"`
	ShopeeDomains       string `json:"shopee_domains"`
	LookbackDays        int    `json:"lookback_days" binding:"required,min=1,max=90"`
	PollIntervalSeconds int    `json:"poll_interval_seconds" binding:"required,min=300"`
	Enabled             bool   `json:"enabled"`
}
