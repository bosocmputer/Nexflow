package models

import (
	"encoding/json"
	"time"
)

type SMLBulkJobStatus string

const (
	SMLBulkJobQueued              SMLBulkJobStatus = "queued"
	SMLBulkJobRunning             SMLBulkJobStatus = "running"
	SMLBulkJobCompleted           SMLBulkJobStatus = "completed"
	SMLBulkJobCompletedWithErrors SMLBulkJobStatus = "completed_with_errors"
	SMLBulkJobFailed              SMLBulkJobStatus = "failed"
)

type SMLBulkJobItemStatus string

const (
	SMLBulkJobItemQueued  SMLBulkJobItemStatus = "queued"
	SMLBulkJobItemRunning SMLBulkJobItemStatus = "running"
	SMLBulkJobItemSent    SMLBulkJobItemStatus = "sent"
	SMLBulkJobItemFailed  SMLBulkJobItemStatus = "failed"
	SMLBulkJobItemSkipped SMLBulkJobItemStatus = "skipped"
)

type SMLBulkJob struct {
	ID              string           `json:"id"`
	ClientRequestID string           `json:"client_request_id"`
	Status          SMLBulkJobStatus `json:"status"`
	Source          string           `json:"source"`
	BillType        string           `json:"bill_type"`
	DocumentRoute   string           `json:"document_route"`
	Title           string           `json:"title"`
	RequestPayload  json.RawMessage  `json:"request_payload"`
	FilterSnapshot  json.RawMessage  `json:"filter_snapshot"`
	TotalCount      int              `json:"total_count"`
	SentCount       int              `json:"sent_count"`
	FailedCount     int              `json:"failed_count"`
	SkippedCount    int              `json:"skipped_count"`
	CreatedBy       *string          `json:"created_by,omitempty"`
	CreatedByEmail  string           `json:"created_by_email"`
	LastError       string           `json:"last_error"`
	CreatedAt       time.Time        `json:"created_at"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	FinishedAt      *time.Time       `json:"finished_at,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Items           []SMLBulkJobItem `json:"items,omitempty"`
}

type SMLBulkJobItem struct {
	ID             string               `json:"id"`
	JobID          string               `json:"job_id"`
	BillID         string               `json:"bill_id"`
	Sequence       int                  `json:"sequence"`
	Status         SMLBulkJobItemStatus `json:"status"`
	OrderNo        string               `json:"order_no"`
	DocNoAttempted string               `json:"doc_no_attempted"`
	DocNo          string               `json:"doc_no"`
	Error          string               `json:"error"`
	Attempts       int                  `json:"attempts"`
	StartedAt      *time.Time           `json:"started_at,omitempty"`
	FinishedAt     *time.Time           `json:"finished_at,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}
