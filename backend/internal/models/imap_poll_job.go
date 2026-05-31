package models

import (
	"encoding/json"
	"time"
)

type IMAPPollJobStatus string

const (
	IMAPPollJobQueued              IMAPPollJobStatus = "queued"
	IMAPPollJobRunning             IMAPPollJobStatus = "running"
	IMAPPollJobCompleted           IMAPPollJobStatus = "completed"
	IMAPPollJobCompletedWithErrors IMAPPollJobStatus = "completed_with_errors"
	IMAPPollJobFailed              IMAPPollJobStatus = "failed"
)

type IMAPPollJob struct {
	ID             string            `json:"id"`
	AccountID      string            `json:"account_id"`
	AccountName    string            `json:"account_name,omitempty"`
	AccountEmail   string            `json:"account_email,omitempty"`
	Status         IMAPPollJobStatus `json:"status"`
	TotalCount     int               `json:"total_count"`
	ScannedCount   int               `json:"scanned_count"`
	CreatedCount   int               `json:"created_count"`
	SkippedCount   int               `json:"skipped_count"`
	FailedCount    int               `json:"failed_count"`
	BacklogCount   int               `json:"backlog_count"`
	ReasonCounts   json.RawMessage   `json:"reason_counts"`
	LatestDetails  []IMAPPollDetail  `json:"latest_details,omitempty"`
	LastError      string            `json:"last_error"`
	CreatedBy      *string           `json:"created_by,omitempty"`
	CreatedByEmail string            `json:"created_by_email"`
	StartedAt      *time.Time        `json:"started_at,omitempty"`
	FinishedAt     *time.Time        `json:"finished_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}
