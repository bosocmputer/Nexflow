package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SMLProfileReconciliationJob struct {
	ID               string
	TenantKey        string
	SMLAttemptID     string
	BillID           string
	ProfileVersion   string
	PayloadHash      string
	Status           string
	RequiredChecks   []string
	CompletedChecks  []string
	AttemptCount     int
	ManualRetryCount int
	MaxAttempts      int
	LeaseOwner       string
	LeaseToken       int64
	LeaseUntil       time.Time
	CorrelationID    string
	DocNo            string
	Route            string
	PayloadBytes     []byte
	RouteSettings    json.RawMessage
}

var (
	ErrSMLProfileReconciliationNotFound = errors.New("SML document profile reconciliation is not available")
	ErrSMLProfileReconciliationBusy     = errors.New("SML document profile reconciliation is running")
	ErrSMLProfileReconciliationComplete = errors.New("SML document profile is already complete")
)

type SMLProfileManualRetry struct {
	BillID           string `json:"bill_id"`
	SMLAttemptID     string `json:"sml_attempt_id"`
	ProfileVersion   string `json:"profile_version"`
	Status           string `json:"status"`
	AttemptCount     int    `json:"attempt_count"`
	ManualRetryCount int    `json:"manual_retry_count"`
	MaxAttempts      int    `json:"max_attempts"`
	CorrelationID    string `json:"correlation_id,omitempty"`
}

type SMLProfileQueueMetrics struct {
	TenantKey            string  `json:"tenant"`
	QueueDepth           int64   `json:"queue_depth"`
	OldestAgeSeconds     float64 `json:"oldest_age_seconds"`
	QueueAgeP95Seconds   float64 `json:"queue_age_p95_seconds"`
	RetryCount           int64   `json:"retry_count"`
	TerminalCount        int64   `json:"terminal_count"`
	PayloadMismatchCount int64   `json:"payload_mismatch_count"`
}

func (r *BillRepo) SMLProfileQueueMetrics(ctx context.Context) (*SMLProfileQueueMetrics, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("profile reconciliation repository is not configured")
	}
	result := &SMLProfileQueueMetrics{TenantKey: r.tenantKey}
	err := r.db.QueryRowContext(ctx, `SELECT
		COUNT(*) FILTER (WHERE status IN ('queued','running','retry_wait')),
		COALESCE(EXTRACT(EPOCH FROM (NOW()-MIN(created_at) FILTER (WHERE status IN ('queued','running','retry_wait')))),0),
		COALESCE(percentile_cont(0.95) WITHIN GROUP (
		  ORDER BY EXTRACT(EPOCH FROM (NOW()-created_at))
		) FILTER (WHERE status IN ('queued','running','retry_wait')),0),
		COALESCE(SUM(attempt_count),0),
		COUNT(*) FILTER (WHERE status IN ('terminal_failure','manual_reconciliation')),
		COUNT(*) FILTER (WHERE last_error_code IN ('doc_no_payload_mismatch','profile_hash_mismatch')) +
		(SELECT COUNT(*) FROM bills b JOIN bill_sml_attempts a ON a.id=b.current_sml_attempt_id
		  WHERE a.profile_status='terminal_failure' AND a.core_status='existing_document_conflict')
		FROM sml_document_profile_reconciliation_jobs WHERE tenant_key=$1`, r.tenantKey).
		Scan(&result.QueueDepth, &result.OldestAgeSeconds, &result.QueueAgeP95Seconds, &result.RetryCount, &result.TerminalCount, &result.PayloadMismatchCount)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *BillRepo) PauseShopeeAutoSMLForBill(ctx context.Context, billID, reason string) (int64, error) {
	if r == nil || r.db == nil || strings.TrimSpace(billID) == "" {
		return 0, errors.New("bill repository is not configured")
	}
	switch reason {
	case "profile_payload_mismatch", "profile_terminal_failure":
	default:
		return 0, errors.New("invalid Auto SML profile pause reason")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE shopee_auto_sml_settings st SET
		paused_reason=$2,paused_at=NOW(),updated_at=NOW()
		WHERE st.enabled=TRUE AND st.shop_id IN (
			SELECT DISTINCT shop_id FROM shopee_order_snapshots WHERE bill_id=$1
		)`, billID, reason)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// PauseShopeeAutoSMLAfterConsecutiveProfileFailures pauses only the shop linked
// to billID when its latest threshold distinct Profile jobs are all currently
// failed. A completed or clean queued job breaks the streak; retries of one job
// never count as multiple jobs.
func (r *BillRepo) PauseShopeeAutoSMLAfterConsecutiveProfileFailures(ctx context.Context, billID string, threshold int) (int64, error) {
	if r == nil || r.db == nil || strings.TrimSpace(billID) == "" || threshold < 2 || threshold > 10 {
		return 0, errors.New("invalid consecutive profile failure check")
	}
	result, err := r.db.ExecContext(ctx, `WITH target_shops AS (
		SELECT DISTINCT shop_id FROM shopee_order_snapshots WHERE bill_id=$1
	), eligible AS (
		SELECT target.shop_id
		FROM target_shops target
		CROSS JOIN LATERAL (
			SELECT COUNT(*) AS total,
			       COUNT(*) FILTER (
			         WHERE recent.status IN ('retry_wait','manual_reconciliation','terminal_failure')
			           AND recent.last_error_code<>''
			       ) AS failed
			FROM (
				SELECT job.status,job.last_error_code
				FROM sml_document_profile_reconciliation_jobs job
				JOIN bill_sml_attempts attempt ON attempt.id=job.sml_attempt_id
				WHERE EXISTS (
					SELECT 1 FROM shopee_order_snapshots snapshot
					WHERE snapshot.bill_id=attempt.bill_id AND snapshot.shop_id=target.shop_id
				)
				ORDER BY job.updated_at DESC,job.id DESC
				LIMIT $2
			) recent
		) streak
		WHERE streak.total=$2 AND streak.failed=$2
	)
	UPDATE shopee_auto_sml_settings setting SET
		paused_reason='profile_consecutive_failures',paused_at=NOW(),updated_at=NOW()
	FROM eligible WHERE setting.shop_id=eligible.shop_id AND setting.enabled=TRUE`, billID, threshold)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *BillRepo) ClaimSMLProfileReconciliationJob(ctx context.Context, owner string, leaseDuration time.Duration) (*SMLProfileReconciliationJob, error) {
	if r == nil || r.db == nil || owner == "" {
		return nil, errors.New("profile reconciliation repository is not configured")
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	row := r.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id FROM sml_document_profile_reconciliation_jobs
		 WHERE attempt_count < max_attempts
		   AND ((status IN ('queued','retry_wait') AND next_attempt_at<=NOW())
		     OR (status='running' AND lease_until<NOW()))
		 ORDER BY next_attempt_at,created_at,id
		 FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
		UPDATE sml_document_profile_reconciliation_jobs j SET
		 status='running',attempt_count=attempt_count+1,lease_owner=$1,
		 lease_token=lease_token+1,lease_until=NOW()+($2 * INTERVAL '1 second'),updated_at=NOW()
		 FROM candidate c WHERE j.id=c.id
		 RETURNING j.id,j.tenant_key,j.sml_attempt_id,j.profile_version,j.payload_hash,j.status,
		           j.required_checks,j.completed_checks,j.attempt_count,j.manual_retry_count,j.max_attempts,j.lease_owner,
		           j.lease_token,j.lease_until,j.correlation_id
	)
	SELECT c.id::text,c.tenant_key,c.sml_attempt_id::text,c.profile_version,c.payload_hash,c.status,
	       c.required_checks,c.completed_checks,c.attempt_count,c.manual_retry_count,c.max_attempts,c.lease_owner,
	       c.lease_token,c.lease_until,c.correlation_id,a.bill_id::text,a.doc_no,a.route,a.payload_bytes,a.route_settings
	  FROM claimed c JOIN bill_sml_attempts a ON a.id=c.sml_attempt_id`, owner, int64(leaseDuration/time.Second))
	var job SMLProfileReconciliationJob
	var required, completed []byte
	err := row.Scan(&job.ID, &job.TenantKey, &job.SMLAttemptID, &job.ProfileVersion, &job.PayloadHash, &job.Status,
		&required, &completed, &job.AttemptCount, &job.ManualRetryCount, &job.MaxAttempts, &job.LeaseOwner, &job.LeaseToken,
		&job.LeaseUntil, &job.CorrelationID, &job.BillID, &job.DocNo, &job.Route, &job.PayloadBytes, &job.RouteSettings)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(required, &job.RequiredChecks)
	_ = json.Unmarshal(completed, &job.CompletedChecks)
	return &job, nil
}

// RetrySMLProfileReconciliation requeues only the supplement/audit repair for
// an already-created core document. It never mutates or resends core payload
// state. A terminal job receives a fresh bounded 10-attempt cycle while the
// manual retry counter preserves operator-recovery history.
func (r *BillRepo) RetrySMLProfileReconciliation(ctx context.Context, billID, correlationID string) (*SMLProfileManualRetry, error) {
	if r == nil || r.db == nil || billID == "" {
		return nil, ErrSMLProfileReconciliationNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var result SMLProfileManualRetry
	var profileStatus, jobStatus string
	err = tx.QueryRowContext(ctx, `SELECT b.id::text,a.id::text,a.profile_version,a.profile_status,
		j.status,j.attempt_count,j.manual_retry_count,j.max_attempts
		FROM bills b
		JOIN bill_sml_attempts a ON a.id=b.current_sml_attempt_id
		JOIN sml_document_profile_reconciliation_jobs j
		  ON j.sml_attempt_id=a.id AND j.profile_version=a.profile_version
		WHERE b.id=$1 AND b.status='sent' AND a.state='sent'
		FOR UPDATE OF a,j`, billID).Scan(&result.BillID, &result.SMLAttemptID, &result.ProfileVersion,
		&profileStatus, &jobStatus, &result.AttemptCount, &result.ManualRetryCount, &result.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSMLProfileReconciliationNotFound
	}
	if err != nil {
		return nil, err
	}
	if profileStatus == "complete" || jobStatus == "complete" {
		return nil, ErrSMLProfileReconciliationComplete
	}
	if jobStatus == "running" {
		return nil, ErrSMLProfileReconciliationBusy
	}
	if profileStatus != "needs_reconciliation" && profileStatus != "terminal_failure" {
		return nil, ErrSMLProfileReconciliationNotFound
	}
	terminal := profileStatus == "terminal_failure" || jobStatus == "manual_reconciliation" || jobStatus == "terminal_failure"
	err = tx.QueryRowContext(ctx, `UPDATE sml_document_profile_reconciliation_jobs SET
		status='queued',attempt_count=CASE WHEN $3 THEN 0 ELSE attempt_count END,
		manual_retry_count=manual_retry_count+1,next_attempt_at=NOW(),lease_owner='',lease_until=NULL,
		correlation_id=$2,last_error_code='',last_error_message='',completed_at=NULL,updated_at=NOW()
		WHERE sml_attempt_id=$1 AND profile_version=$4
		RETURNING status,attempt_count,manual_retry_count,max_attempts,correlation_id`,
		result.SMLAttemptID, strings.TrimSpace(correlationID), terminal, result.ProfileVersion).
		Scan(&result.Status, &result.AttemptCount, &result.ManualRetryCount, &result.MaxAttempts, &result.CorrelationID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bill_sml_attempts SET
		profile_status='needs_reconciliation',profile_reconciliation_required=TRUE,updated_at=NOW()
		WHERE id=$1 AND profile_version=$2`, result.SMLAttemptID, result.ProfileVersion); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *BillRepo) CompleteSMLProfileReconciliationJob(ctx context.Context, job *SMLProfileReconciliationJob, payloadHash string, completedChecks []string) error {
	if r == nil || r.db == nil || job == nil || job.ID == "" || job.LeaseOwner == "" || job.LeaseToken <= 0 {
		return errors.New("invalid profile reconciliation completion")
	}
	checks, _ := json.Marshal(completedChecks)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sml_document_profile_reconciliation_jobs SET
		status='complete',payload_hash=$4,completed_checks=$5,lease_owner='',lease_until=NULL,
		last_error_code='',last_error_message='',completed_at=NOW(),updated_at=NOW()
		WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND status='running'`,
		job.ID, job.LeaseOwner, job.LeaseToken, payloadHash, checks)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bill_sml_attempts SET
		profile_status='complete',profile_payload_hash=$2,profile_completed_checks=$3,
		profile_reconciliation_required=FALSE,updated_at=NOW()
		WHERE id=$1 AND profile_version=$4`, job.SMLAttemptID, payloadHash, checks, job.ProfileVersion); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *BillRepo) FailSMLProfileReconciliationJob(ctx context.Context, job *SMLProfileReconciliationJob, errorCode, errorMessage string, terminal bool) error {
	if r == nil || r.db == nil || job == nil || job.ID == "" || job.LeaseOwner == "" || job.LeaseToken <= 0 {
		return errors.New("invalid profile reconciliation failure")
	}
	if job.AttemptCount >= job.MaxAttempts {
		terminal = true
	}
	status := "retry_wait"
	profileStatus := "needs_reconciliation"
	if terminal {
		status = "manual_reconciliation"
		profileStatus = "terminal_failure"
	}
	delay := smlProfileRetryDelay(job.AttemptCount)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sml_document_profile_reconciliation_jobs SET
		status=$4,next_attempt_at=NOW()+($5 * INTERVAL '1 second'),lease_owner='',lease_until=NULL,
		last_error_code=$6,last_error_message=$7,updated_at=NOW()
		WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND status='running'`,
		job.ID, job.LeaseOwner, job.LeaseToken, status, int64(delay/time.Second), errorCode, errorMessage)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bill_sml_attempts SET
		profile_status=$2,profile_reconciliation_required=TRUE,updated_at=NOW()
		WHERE id=$1 AND profile_version=$3`, job.SMLAttemptID, profileStatus, job.ProfileVersion); err != nil {
		return err
	}
	return tx.Commit()
}

func smlProfileRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	delay := time.Duration(1<<(attempt-1)) * 5 * time.Second
	if delay > 10*time.Minute {
		return 10 * time.Minute
	}
	return delay
}

func ValidateSMLProfileJobTenant(jobTenant, instanceTenant string) error {
	if jobTenant == "" || instanceTenant == "" || jobTenant != instanceTenant {
		return fmt.Errorf("profile reconciliation tenant mismatch")
	}
	return nil
}
