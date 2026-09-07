package diagnostics

import (
	"encoding/json"
	"time"
)

const DiagnosticPackageVersion = 1

// RequestMetadata intentionally contains only routing facts. Authentication
// headers and credentials never enter the diagnostics model.
type RequestMetadata struct {
	Method        string `json:"method"`
	CanonicalPath string `json:"canonical_path"`
	ContentType   string `json:"content_type,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

type ExchangeEvidence struct {
	ID                  string            `json:"id"`
	Sequence            int               `json:"sequence"`
	TraceID             string            `json:"trace_id,omitempty"`
	Status              string            `json:"status"`
	StartedAt           time.Time         `json:"started_at"`
	FinishedAt          *time.Time        `json:"finished_at,omitempty"`
	DurationMS          *int64            `json:"duration_ms,omitempty"`
	Request             RequestMetadata   `json:"request"`
	ResponseStatus      *int              `json:"response_status,omitempty"`
	ResponseHeaders     map[string]string `json:"response_headers,omitempty"`
	ResponseJSON        json.RawMessage   `json:"response_json,omitempty"`
	ResponseHash        string            `json:"response_hash,omitempty"`
	ResponseSize        int64             `json:"response_size"`
	ResponseTruncated   bool              `json:"response_truncated"`
	ErrorCode           string            `json:"error_code,omitempty"`
	ErrorClass          string            `json:"error_class,omitempty"`
	SafeErrorSummary    string            `json:"safe_error_summary,omitempty"`
	DiagnosticAvailable bool              `json:"diagnostic_available"`
}

type StaffSMLSummary struct {
	AttemptID        string `json:"attempt_id,omitempty"`
	DocumentNumber   string `json:"document_number,omitempty"`
	Route            string `json:"route,omitempty"`
	CoreStatus       string `json:"core_status,omitempty"`
	ProfileStatus    string `json:"profile_status,omitempty"`
	StockStatus      string `json:"stock_status,omitempty"`
	ResolutionStatus string `json:"resolution_status,omitempty"`
	FailureCount     int    `json:"failure_count"`
	CanRetry         bool   `json:"can_retry"`
}

// DiagnosticPackage is always sanitized before serialization, including for
// admins. RequestPayload is the immutable business payload; credentials and
// unsafe response fields are never part of the package.
type DiagnosticPackage struct {
	Version        int                `json:"diagnostic_package_version"`
	GeneratedAt    time.Time          `json:"generated_at"`
	BillID         string             `json:"bill_id"`
	Summary        StaffSMLSummary    `json:"sml_summary"`
	RequestPayload json.RawMessage    `json:"request_payload,omitempty"`
	Exchanges      []ExchangeEvidence `json:"exchanges"`
}
