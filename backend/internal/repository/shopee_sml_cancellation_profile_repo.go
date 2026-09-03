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

var (
	ErrSMLCancellationProfileNotFound = errors.New("SML cancellation Profile reconciliation is not available")
	ErrSMLCancellationProfileBusy     = errors.New("SML cancellation Profile reconciliation is running")
	ErrSMLCancellationProfileComplete = errors.New("SML cancellation Profile is already complete")
)

type SMLCancellationProfileResult struct {
	Version                string
	PayloadHash            string
	CoreStatus             string
	ProfileStatus          string
	RequiredChecks         []string
	CompletedChecks        []string
	ReconciliationRequired bool
	CorrelationID          string
	Route                  string
}

type SMLCancellationProfileReconciliationJob struct {
	ID, TenantKey, CancellationID, ProfileVersion, PayloadHash string
	Route                                                      string
	Status                                                     string
	RequiredChecks, CompletedChecks                            []string
	AttemptCount, ManualRetryCount, MaxAttempts                int
	LeaseOwner                                                 string
	LeaseToken                                                 int64
	LeaseUntil                                                 *time.Time
	CorrelationID                                              string
	ShopID                                                     int64
	OrderSN, BillID, SourceDocNo, CancelDocNo, RouteEndpoint   string
	RequestPayload                                             json.RawMessage
}

func (r *ShopeeRealtimeRepo) SMLCancellationProfileQueueMetrics(ctx context.Context) (*SMLProfileQueueMetrics, error) {
	if r == nil || r.db == nil || strings.TrimSpace(r.tenantKey) == "" {
		return nil, errors.New("cancellation profile reconciliation repository is not configured")
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
		COUNT(*) FILTER (WHERE last_error_code IN ('doc_no_payload_mismatch','profile_hash_mismatch'))
		FROM sml_cancellation_profile_reconciliation_jobs WHERE tenant_key=$1`, r.tenantKey).
		Scan(&result.QueueDepth, &result.OldestAgeSeconds, &result.QueueAgeP95Seconds, &result.RetryCount, &result.TerminalCount, &result.PayloadMismatchCount)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeSMLCancellationProfileResult(results ...SMLCancellationProfileResult) (SMLCancellationProfileResult, error) {
	if len(results) == 0 {
		return SMLCancellationProfileResult{RequiredChecks: []string{}, CompletedChecks: []string{}}, nil
	}
	if len(results) != 1 {
		return SMLCancellationProfileResult{}, fmt.Errorf("exactly one cancellation profile result is allowed")
	}
	result := results[0]
	result.Version = strings.TrimSpace(result.Version)
	result.PayloadHash = strings.TrimSpace(result.PayloadHash)
	result.CoreStatus = strings.TrimSpace(result.CoreStatus)
	result.ProfileStatus = strings.TrimSpace(result.ProfileStatus)
	result.CorrelationID = strings.TrimSpace(result.CorrelationID)
	result.Route = strings.ToLower(strings.TrimSpace(result.Route))
	if result.RequiredChecks == nil {
		result.RequiredChecks = []string{}
	}
	if result.CompletedChecks == nil {
		result.CompletedChecks = []string{}
	}
	if result.Version == "" {
		return SMLCancellationProfileResult{RequiredChecks: []string{}, CompletedChecks: []string{}}, nil
	}
	switch result.Route {
	case "saleordercancel", "saleinvoicecancel", "creditnote":
	default:
		return SMLCancellationProfileResult{}, fmt.Errorf("invalid cancellation profile route %q", result.Route)
	}
	switch result.ProfileStatus {
	case "pending", "complete", "needs_reconciliation", "terminal_failure":
	default:
		return SMLCancellationProfileResult{}, fmt.Errorf("invalid cancellation profile status %q", result.ProfileStatus)
	}
	if result.ProfileStatus == "complete" && result.ReconciliationRequired {
		return SMLCancellationProfileResult{}, fmt.Errorf("complete cancellation profile cannot require reconciliation")
	}
	if result.ProfileStatus == "complete" && result.PayloadHash == "" {
		result.ProfileStatus = "needs_reconciliation"
		result.ReconciliationRequired = true
	}
	if result.ProfileStatus != "complete" {
		result.ReconciliationRequired = true
	}
	return result, nil
}

func (r *ShopeeRealtimeRepo) persistSMLCancellationProfileJob(ctx context.Context, tx *sql.Tx, cancellationID string, profile SMLCancellationProfileResult) error {
	if profile.Version == "" {
		return nil
	}
	required, _ := json.Marshal(profile.RequiredChecks)
	completed, _ := json.Marshal(profile.CompletedChecks)
	if !profile.ReconciliationRequired && profile.ProfileStatus == "complete" {
		_, err := tx.ExecContext(ctx, `UPDATE sml_cancellation_profile_reconciliation_jobs SET
			status='complete',payload_hash=$3,completed_checks=$4::jsonb,
			lease_owner='',lease_until=NULL,last_error_code='',last_error_message='',
			completed_at=COALESCE(completed_at,NOW()),updated_at=NOW()
			WHERE cancellation_id=$1::uuid AND profile_version=$2 AND status<>'complete'`,
			cancellationID, profile.Version, profile.PayloadHash, string(completed))
		return err
	}
	if strings.TrimSpace(r.tenantKey) == "" {
		return fmt.Errorf("cancellation profile tenant is not configured")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO sml_cancellation_profile_reconciliation_jobs
		(tenant_key,cancellation_id,profile_version,route_name,payload_hash,status,required_checks,
		 completed_checks,correlation_id,next_attempt_at)
		VALUES ($1,$2::uuid,$3,$4,$5,'queued',$6::jsonb,$7::jsonb,$8,NOW())
		ON CONFLICT (cancellation_id,profile_version) DO NOTHING`,
		r.tenantKey, cancellationID, profile.Version, profile.Route, profile.PayloadHash,
		string(required), string(completed), profile.CorrelationID)
	return err
}

func (r *ShopeeRealtimeRepo) ClaimSMLCancellationProfileReconciliationJob(ctx context.Context, owner string, leaseDuration time.Duration) (*SMLCancellationProfileReconciliationJob, error) {
	if r == nil || r.db == nil || strings.TrimSpace(r.tenantKey) == "" {
		return nil, fmt.Errorf("cancellation profile reconciliation repository is not configured")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("cancellation profile lease owner is required")
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	row := r.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT job.id
		  FROM sml_cancellation_profile_reconciliation_jobs job
		  JOIN shopee_sml_cancellations cancellation ON cancellation.id=job.cancellation_id
		 WHERE job.tenant_key=$3
		   AND cancellation.status IN ('created','already_exists')
		   AND (
		     (job.status IN ('queued','retry_wait') AND job.next_attempt_at<=NOW()) OR
		     (job.status='running' AND job.lease_until<NOW())
		   )
		 ORDER BY job.next_attempt_at,job.created_at,job.id
		 LIMIT 1 FOR UPDATE OF job SKIP LOCKED
	)
	UPDATE sml_cancellation_profile_reconciliation_jobs job SET
		status='running',attempt_count=attempt_count+1,lease_owner=$1,
		lease_token=lease_token+1,lease_until=NOW()+($2*INTERVAL '1 second'),updated_at=NOW()
	FROM candidate,shopee_sml_cancellations cancellation
	WHERE job.id=candidate.id AND cancellation.id=job.cancellation_id
	RETURNING job.id::text,job.tenant_key,job.cancellation_id::text,job.profile_version,
		job.route_name,job.payload_hash,job.status,job.required_checks,job.completed_checks,
		job.attempt_count,job.manual_retry_count,job.max_attempts,job.lease_owner,
		job.lease_token,job.lease_until,job.correlation_id,cancellation.shop_id,
		cancellation.order_sn,COALESCE(cancellation.bill_id::text,''),
		cancellation.sale_sml_doc_no,cancellation.cancel_sml_doc_no,
		cancellation.route_endpoint,
		CASE WHEN octet_length(cancellation.request_payload_bytes)>0
		     THEN cancellation.request_payload_bytes
		     ELSE convert_to(cancellation.request_payload::text,'UTF8') END`,
		owner, int64(leaseDuration/time.Second), r.tenantKey)
	job, err := scanSMLCancellationProfileReconciliationJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func scanSMLCancellationProfileReconciliationJob(row snapshotScanner) (SMLCancellationProfileReconciliationJob, error) {
	var job SMLCancellationProfileReconciliationJob
	var required, completed []byte
	var leaseUntil sql.NullTime
	err := row.Scan(&job.ID, &job.TenantKey, &job.CancellationID, &job.ProfileVersion,
		&job.Route, &job.PayloadHash, &job.Status, &required, &completed,
		&job.AttemptCount, &job.ManualRetryCount, &job.MaxAttempts, &job.LeaseOwner,
		&job.LeaseToken, &leaseUntil, &job.CorrelationID, &job.ShopID, &job.OrderSN,
		&job.BillID, &job.SourceDocNo, &job.CancelDocNo, &job.RouteEndpoint, &job.RequestPayload)
	if err != nil {
		return job, err
	}
	if err := json.Unmarshal(required, &job.RequiredChecks); err != nil {
		return job, fmt.Errorf("decode cancellation profile required checks: %w", err)
	}
	if err := json.Unmarshal(completed, &job.CompletedChecks); err != nil {
		return job, fmt.Errorf("decode cancellation profile completed checks: %w", err)
	}
	if leaseUntil.Valid {
		job.LeaseUntil = &leaseUntil.Time
	}
	return job, nil
}

func (r *ShopeeRealtimeRepo) CompleteSMLCancellationProfileReconciliationJob(ctx context.Context, job *SMLCancellationProfileReconciliationJob, payloadHash string, completedChecks []string) error {
	if r == nil || r.db == nil || job == nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.CancellationID) == "" {
		return fmt.Errorf("invalid cancellation profile reconciliation completion")
	}
	completed, _ := json.Marshal(completedChecks)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sml_cancellation_profile_reconciliation_jobs SET
		status='complete',payload_hash=$4,completed_checks=$5::jsonb,
		lease_owner='',lease_until=NULL,last_error_code='',last_error_message='',
		completed_at=NOW(),updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND lease_token=$3 AND status='running'`,
		job.ID, job.LeaseOwner, job.LeaseToken, strings.TrimSpace(payloadHash), string(completed))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	result, err = tx.ExecContext(ctx, `UPDATE shopee_sml_cancellations SET
		profile_status='complete',profile_payload_hash=$2,
		profile_completed_checks=$3::jsonb,profile_reconciliation_required=FALSE,updated_at=NOW()
		WHERE id=$1::uuid AND status IN ('created','already_exists') AND profile_version=$4`,
		job.CancellationID, strings.TrimSpace(payloadHash), string(completed), job.ProfileVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("cancellation core/profile state changed before reconciliation completion")
	}
	return tx.Commit()
}

func (r *ShopeeRealtimeRepo) DeferSMLCancellationProfileReconciliationJob(ctx context.Context, job *SMLCancellationProfileReconciliationJob, delay time.Duration) error {
	if r == nil || r.db == nil || job == nil || strings.TrimSpace(job.ID) == "" {
		return fmt.Errorf("invalid cancellation profile reconciliation defer")
	}
	if delay <= 0 {
		delay = time.Minute
	}
	result, err := r.db.ExecContext(ctx, `UPDATE sml_cancellation_profile_reconciliation_jobs SET
		status='queued',attempt_count=GREATEST(attempt_count-1,0),
		next_attempt_at=NOW()+($4*INTERVAL '1 second'),lease_owner='',lease_until=NULL,updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND lease_token=$3 AND status='running'`,
		job.ID, job.LeaseOwner, job.LeaseToken, int64(delay/time.Second))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	return nil
}

func (r *ShopeeRealtimeRepo) RetrySMLCancellationProfileReconciliation(ctx context.Context, cancellationID, correlationID string) (*SMLCancellationProfileReconciliationJob, error) {
	if r == nil || r.db == nil || strings.TrimSpace(cancellationID) == "" {
		return nil, ErrSMLCancellationProfileNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var job SMLCancellationProfileReconciliationJob
	var leaseUntil sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT job.id::text,job.profile_version,job.status,
		job.attempt_count,job.manual_retry_count,job.max_attempts,job.lease_until
		FROM sml_cancellation_profile_reconciliation_jobs job
		JOIN shopee_sml_cancellations cancellation ON cancellation.id=job.cancellation_id
		WHERE cancellation.id=$1::uuid AND cancellation.status IN ('created','already_exists')
		ORDER BY job.created_at DESC LIMIT 1 FOR UPDATE OF job,cancellation`, strings.TrimSpace(cancellationID)).
		Scan(&job.ID, &job.ProfileVersion, &job.Status, &job.AttemptCount, &job.ManualRetryCount, &job.MaxAttempts, &leaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSMLCancellationProfileNotFound
	}
	if err != nil {
		return nil, err
	}
	if job.Status == "complete" {
		return nil, ErrSMLCancellationProfileComplete
	}
	if job.Status == "running" && leaseUntil.Valid && leaseUntil.Time.After(time.Now()) {
		return nil, ErrSMLCancellationProfileBusy
	}
	manual := job.Status == "manual_reconciliation" || job.Status == "terminal_failure"
	row := tx.QueryRowContext(ctx, `UPDATE sml_cancellation_profile_reconciliation_jobs SET
		status='queued',attempt_count=CASE WHEN $3 THEN 0 ELSE attempt_count END,
		manual_retry_count=manual_retry_count+CASE WHEN $3 THEN 1 ELSE 0 END,
		next_attempt_at=NOW(),lease_owner='',lease_until=NULL,correlation_id=$2,
		last_error_code='',last_error_message='',completed_at=NULL,updated_at=NOW()
		WHERE id=$1::uuid
		RETURNING status,attempt_count,manual_retry_count,max_attempts,correlation_id`,
		job.ID, strings.TrimSpace(correlationID), manual)
	if err := row.Scan(&job.Status, &job.AttemptCount, &job.ManualRetryCount, &job.MaxAttempts, &job.CorrelationID); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE shopee_sml_cancellations SET
		profile_status='needs_reconciliation',profile_reconciliation_required=TRUE,updated_at=NOW()
		WHERE id=$1::uuid AND status IN ('created','already_exists') AND profile_version=$2`,
		strings.TrimSpace(cancellationID), job.ProfileVersion)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, ErrSMLCancellationProfileNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.CancellationID = strings.TrimSpace(cancellationID)
	return &job, nil
}

func (r *ShopeeRealtimeRepo) FailSMLCancellationProfileReconciliationJob(ctx context.Context, job *SMLCancellationProfileReconciliationJob, errorCode, errorMessage string, forceTerminal bool) error {
	if r == nil || r.db == nil || job == nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.CancellationID) == "" {
		return fmt.Errorf("invalid cancellation profile reconciliation failure")
	}
	terminal := forceTerminal || job.AttemptCount >= job.MaxAttempts
	jobStatus, profileStatus := "retry_wait", "needs_reconciliation"
	if terminal {
		jobStatus, profileStatus = "manual_reconciliation", "terminal_failure"
	}
	delay := smlProfileRetryDelay(job.AttemptCount)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sml_cancellation_profile_reconciliation_jobs SET
		status=$4,next_attempt_at=NOW()+($5*INTERVAL '1 second'),lease_owner='',lease_until=NULL,
		last_error_code=$6,last_error_message=$7,updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND lease_token=$3 AND status='running'`,
		job.ID, job.LeaseOwner, job.LeaseToken, jobStatus, int64(delay/time.Second),
		truncateDBText(errorCode, 100), truncateDBText(errorMessage, 800))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	result, err = tx.ExecContext(ctx, `UPDATE shopee_sml_cancellations SET
		profile_status=$2,profile_reconciliation_required=TRUE,updated_at=NOW()
		WHERE id=$1::uuid AND status IN ('created','already_exists') AND profile_version=$3`,
		job.CancellationID, profileStatus, job.ProfileVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("cancellation core/profile state changed before reconciliation failure")
	}
	return tx.Commit()
}
