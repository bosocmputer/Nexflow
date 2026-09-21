package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"nexflow/internal/models"
)

var ErrTikTokAutoSMLConfigConflict = errors.New("TikTok Auto SML setting changed")

type TikTokAutoSMLRepo struct {
	db *sql.DB
}

type TikTokAutoSMLSettingUpdate struct {
	ShopID                string
	Enabled               bool
	ExpectedConfigVersion int64
	RouteSignature        string
	UserID                string
}

type TikTokAutoSMLEnqueueInput struct {
	ShopID              string
	OrderID             string
	OrderStatus         string
	TriggerTransitionAt time.Time
	SourceHash          string
	BillFingerprint     string
	RouteSignature      string
}

func NewTikTokAutoSMLRepo(db *sql.DB) *TikTokAutoSMLRepo {
	return &TikTokAutoSMLRepo{db: db}
}

func (r *TikTokAutoSMLRepo) ensure(ctx context.Context, shopID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO tiktok_shop_auto_sml_settings (shop_id) VALUES ($1) ON CONFLICT (shop_id) DO NOTHING`, strings.TrimSpace(shopID))
	return err
}

func (r *TikTokAutoSMLRepo) ListSettings(ctx context.Context) ([]models.TikTokAutoSMLSetting, error) {
	if r == nil || r.db == nil {
		return nil, sql.ErrConnDone
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.shop_id,c.shop_name,COALESCE(st.enabled,FALSE),COALESCE(st.trigger_status,'AWAITING_COLLECTION'),COALESCE(st.config_version,1),st.eligible_after,
		       COALESCE(st.route_signature,''),st.enabled_by::text,st.enabled_at,COALESCE(st.paused_reason,''),st.paused_at,
		       COALESCE(st.consecutive_system_failures,0),st.last_success_at,st.last_failure_at,
		       COUNT(j.id) FILTER (WHERE j.status IN ('queued','running','retry_wait')),
		       COUNT(j.id) FILTER (WHERE j.status='needs_review'),
		       COUNT(j.id) FILTER (WHERE j.status='failed'),COALESCE(st.updated_at,c.updated_at)
		  FROM tiktok_shop_connections c
		  LEFT JOIN tiktok_shop_auto_sml_settings st ON st.shop_id=c.shop_id
		  LEFT JOIN tiktok_shop_auto_sml_jobs j ON j.shop_id=st.shop_id
		 WHERE c.disabled_at IS NULL
		 GROUP BY c.shop_id,c.shop_name,c.updated_at,st.enabled,st.trigger_status,st.config_version,st.eligible_after,
		          st.route_signature,st.enabled_by,st.enabled_at,st.paused_reason,st.paused_at,
		          st.consecutive_system_failures,st.last_success_at,st.last_failure_at,st.updated_at
		 ORDER BY c.shop_name,st.shop_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make([]models.TikTokAutoSMLSetting, 0)
	for rows.Next() {
		setting, scanErr := scanTikTokAutoSMLSetting(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		settings = append(settings, setting)
	}
	return settings, rows.Err()
}

func (r *TikTokAutoSMLRepo) GetSetting(ctx context.Context, shopID string) (*models.TikTokAutoSMLSetting, error) {
	if r == nil || r.db == nil {
		return nil, sql.ErrConnDone
	}
	shopID = strings.TrimSpace(shopID)
	setting, err := scanTikTokAutoSMLSetting(r.db.QueryRowContext(ctx, `
		SELECT st.shop_id,c.shop_name,st.enabled,st.trigger_status,st.config_version,st.eligible_after,
		       st.route_signature,st.enabled_by::text,st.enabled_at,st.paused_reason,st.paused_at,
		       st.consecutive_system_failures,st.last_success_at,st.last_failure_at,
		       COUNT(j.id) FILTER (WHERE j.status IN ('queued','running','retry_wait')),
		       COUNT(j.id) FILTER (WHERE j.status='needs_review'),
		       COUNT(j.id) FILTER (WHERE j.status='failed'),st.updated_at
		  FROM tiktok_shop_auto_sml_settings st
		  JOIN tiktok_shop_connections c ON c.shop_id=st.shop_id AND c.disabled_at IS NULL
		  LEFT JOIN tiktok_shop_auto_sml_jobs j ON j.shop_id=st.shop_id
		 WHERE st.shop_id=$1
		 GROUP BY st.shop_id,c.shop_name,st.enabled,st.trigger_status,st.config_version,st.eligible_after,
		          st.route_signature,st.enabled_by,st.enabled_at,st.paused_reason,st.paused_at,
		          st.consecutive_system_failures,st.last_success_at,st.last_failure_at,st.updated_at`, shopID))
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func (r *TikTokAutoSMLRepo) UpdateSetting(ctx context.Context, input TikTokAutoSMLSettingUpdate) (*models.TikTokAutoSMLSetting, error) {
	if r == nil || r.db == nil || strings.TrimSpace(input.ShopID) == "" || input.ExpectedConfigVersion < 1 {
		return nil, sql.ErrNoRows
	}
	input.ShopID = strings.TrimSpace(input.ShopID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO tiktok_shop_auto_sml_settings (shop_id) VALUES ($1) ON CONFLICT (shop_id) DO NOTHING`, input.ShopID); err != nil {
		return nil, err
	}
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT config_version FROM tiktok_shop_auto_sml_settings WHERE shop_id=$1 FOR UPDATE`, input.ShopID).Scan(&currentVersion); err != nil {
		return nil, err
	}
	if currentVersion != input.ExpectedConfigVersion {
		return nil, ErrTikTokAutoSMLConfigConflict
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE tiktok_shop_auto_sml_settings
		   SET enabled=$2,config_version=config_version+1,
		       eligible_after=CASE WHEN $2 AND (enabled=FALSE OR paused_reason<>'') THEN NOW() ELSE eligible_after END,
		       route_signature=CASE WHEN $2 THEN $3 ELSE route_signature END,
		       enabled_by=CASE WHEN $2 THEN NULLIF($4,'')::uuid ELSE enabled_by END,
		       enabled_at=CASE WHEN $2 AND (enabled=FALSE OR paused_reason<>'') THEN NOW() ELSE enabled_at END,
		       paused_reason='',paused_at=NULL,consecutive_system_failures=0,updated_at=NOW()
		 WHERE shop_id=$1 AND config_version=$5`, input.ShopID, input.Enabled, strings.TrimSpace(input.RouteSignature), strings.TrimSpace(input.UserID), input.ExpectedConfigVersion)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, ErrTikTokAutoSMLConfigConflict
	}
	if !input.Enabled {
		if _, err := tx.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET status='cancelled',lease_until=NULL,last_error_code='automation_disabled',last_error_message='ปิด Auto SML ก่อนเริ่มส่ง',completed_at=NOW(),updated_at=NOW() WHERE shop_id=$1 AND status IN ('queued','retry_wait')`, input.ShopID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSetting(ctx, input.ShopID)
}

func (r *TikTokAutoSMLRepo) Enqueue(ctx context.Context, input TikTokAutoSMLEnqueueInput) (bool, error) {
	if r == nil || r.db == nil {
		return false, sql.ErrConnDone
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO tiktok_shop_auto_sml_jobs
		  (shop_id,order_id,trigger_status_snapshot,trigger_transition_at,trigger_config_version,source_hash,bill_fingerprint,route_signature)
		SELECT $1,$2,st.trigger_status,$4,st.config_version,$5,$6,$7
		  FROM tiktok_shop_auto_sml_settings st
		 WHERE st.shop_id=$1 AND st.enabled=TRUE AND st.paused_reason=''
		   AND st.trigger_status=$3 AND st.eligible_after IS NOT NULL AND $4 >= st.eligible_after
		   AND st.route_signature=$7
		ON CONFLICT (shop_id,order_id) DO NOTHING`,
		strings.TrimSpace(input.ShopID), strings.TrimSpace(input.OrderID), strings.TrimSpace(input.OrderStatus),
		input.TriggerTransitionAt.UTC(), strings.TrimSpace(input.SourceHash), strings.TrimSpace(input.BillFingerprint), strings.TrimSpace(input.RouteSignature))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *TikTokAutoSMLRepo) RecoverStaleJobs(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET status='retry_wait',lease_until=NULL,next_run_at=NOW(),last_error_code='lease_expired',last_error_message='งานเดิมหมดเวลาและถูกนำกลับเข้าคิว',updated_at=NOW() WHERE status='running' AND lease_until<NOW()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *TikTokAutoSMLRepo) LeaseJobs(ctx context.Context, limit int, lease time.Duration) ([]models.TikTokAutoSMLJob, error) {
	if limit < 1 || limit > 20 {
		limit = 2
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH due AS (
		  SELECT j.id FROM tiktok_shop_auto_sml_jobs j
		  JOIN tiktok_shop_auto_sml_settings st ON st.shop_id=j.shop_id
		  WHERE j.status IN ('queued','retry_wait') AND j.next_run_at<=NOW()
		    AND st.enabled=TRUE AND st.paused_reason=''
		  ORDER BY j.next_run_at,j.created_at FOR UPDATE OF j SKIP LOCKED LIMIT $1
		)
		UPDATE tiktok_shop_auto_sml_jobs j SET status='running',attempts=attempts+1,
		       lease_until=NOW()+($2*INTERVAL '1 second'),started_at=COALESCE(started_at,NOW()),updated_at=NOW()
		  FROM due WHERE j.id=due.id
		RETURNING j.id::text,j.shop_id,j.order_id,j.status,j.trigger_status_snapshot,j.trigger_transition_at,
		          j.trigger_config_version,j.source_hash,j.bill_fingerprint,j.route_signature,j.bill_id::text,j.sml_doc_no,j.review_digest,
		          j.document_time,j.attempts,j.next_run_at,j.lease_until,j.last_error_code,j.last_error_message,j.created_at,j.updated_at`,
		limit, int64(lease.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]models.TikTokAutoSMLJob, 0, limit)
	for rows.Next() {
		job, scanErr := scanTikTokAutoSMLJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *TikTokAutoSMLRepo) LinkBill(ctx context.Context, id, billID, reviewDigest string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET bill_id=NULLIF($2,'')::uuid,review_digest=$3,updated_at=NOW() WHERE id=$1::uuid`, strings.TrimSpace(id), strings.TrimSpace(billID), strings.TrimSpace(reviewDigest))
	return err
}

func (r *TikTokAutoSMLRepo) GetOrSetDocumentTime(ctx context.Context, id, value string) (string, error) {
	var persisted string
	err := r.db.QueryRowContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET document_time=CASE WHEN document_time='' THEN $2 ELSE document_time END,updated_at=NOW() WHERE id=$1::uuid RETURNING document_time`, strings.TrimSpace(id), strings.TrimSpace(value)).Scan(&persisted)
	return persisted, err
}

func (r *TikTokAutoSMLRepo) MarkNeedsReview(ctx context.Context, id, billID, code, message string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET status='needs_review',bill_id=COALESCE(NULLIF($2,'')::uuid,bill_id),lease_until=NULL,last_error_code=$3,last_error_message=$4,completed_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, id, billID, trimTikTokAutoSMLError(code, 100), trimTikTokAutoSMLError(message, 800))
	return err
}

func (r *TikTokAutoSMLRepo) MarkCancelled(ctx context.Context, id, code, message string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET status='cancelled',lease_until=NULL,last_error_code=$2,last_error_message=$3,completed_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, id, trimTikTokAutoSMLError(code, 100), trimTikTokAutoSMLError(message, 800))
	return err
}

func (r *TikTokAutoSMLRepo) MarkSucceeded(ctx context.Context, id, billID, docNo string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var shopID string
	if err := tx.QueryRowContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET status='succeeded',bill_id=COALESCE(NULLIF($2,'')::uuid,bill_id),sml_doc_no=$3,lease_until=NULL,last_error_code='',last_error_message='',completed_at=NOW(),updated_at=NOW() WHERE id=$1::uuid RETURNING shop_id`, id, billID, strings.TrimSpace(docNo)).Scan(&shopID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_settings SET consecutive_system_failures=0,last_success_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TikTokAutoSMLRepo) MarkTransientFailure(ctx context.Context, id, code, message string, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 3
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var shopID string
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT shop_id,attempts FROM tiktok_shop_auto_sml_jobs WHERE id=$1::uuid FOR UPDATE`, id).Scan(&shopID, &attempts); err != nil {
		return err
	}
	terminal := attempts >= maxAttempts
	if _, err := tx.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_jobs SET status=CASE WHEN $2 THEN 'failed' ELSE 'retry_wait' END,next_run_at=NOW()+(CASE attempts WHEN 1 THEN INTERVAL '1 minute' WHEN 2 THEN INTERVAL '5 minutes' ELSE INTERVAL '15 minutes' END),lease_until=NULL,last_error_code=$3,last_error_message=$4,completed_at=CASE WHEN $2 THEN NOW() ELSE NULL END,updated_at=NOW() WHERE id=$1::uuid`, id, terminal, trimTikTokAutoSMLError(code, 100), trimTikTokAutoSMLError(message, 800)); err != nil {
		return err
	}
	if terminal {
		if _, err := tx.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_settings SET consecutive_system_failures=consecutive_system_failures+1,last_failure_at=NOW(),paused_reason=CASE WHEN consecutive_system_failures+1>=3 THEN 'system_failures' ELSE paused_reason END,paused_at=CASE WHEN consecutive_system_failures+1>=3 THEN NOW() ELSE paused_at END,updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *TikTokAutoSMLRepo) PauseForRouteChange(ctx context.Context, shopID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_auto_sml_settings SET paused_reason='route_changed',paused_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, strings.TrimSpace(shopID))
	return err
}

func (r *TikTokAutoSMLRepo) RetryJob(ctx context.Context, shopID, orderID, billFingerprint, routeSignature string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE tiktok_shop_auto_sml_jobs j
		   SET status='queued',attempts=0,next_run_at=NOW(),lease_until=NULL,
		       trigger_config_version=st.config_version,bill_fingerprint=$3,route_signature=$4,
		       last_error_code='',last_error_message='',completed_at=NULL,updated_at=NOW()
		  FROM tiktok_shop_auto_sml_settings st
		 WHERE j.shop_id=$1 AND j.order_id=$2 AND j.shop_id=st.shop_id
		   AND st.enabled=TRUE AND st.paused_reason='' AND st.route_signature=$4
		   AND j.status IN ('needs_review','failed')`,
		strings.TrimSpace(shopID), strings.TrimSpace(orderID), strings.TrimSpace(billFingerprint), strings.TrimSpace(routeSignature))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

type tikTokAutoSMLScanner interface{ Scan(...any) error }

func scanTikTokAutoSMLSetting(scanner tikTokAutoSMLScanner) (models.TikTokAutoSMLSetting, error) {
	var out models.TikTokAutoSMLSetting
	var eligibleAfter, enabledAt, pausedAt, lastSuccess, lastFailure sql.NullTime
	var enabledBy sql.NullString
	err := scanner.Scan(&out.ShopID, &out.ShopName, &out.Enabled, &out.TriggerStatus, &out.ConfigVersion, &eligibleAfter,
		&out.RouteSignature, &enabledBy, &enabledAt, &out.PausedReason, &pausedAt, &out.ConsecutiveSystemFailures,
		&lastSuccess, &lastFailure, &out.QueuedCount, &out.NeedsReviewCount, &out.FailedCount, &out.UpdatedAt)
	if eligibleAfter.Valid {
		value := eligibleAfter.Time.UTC()
		out.EligibleAfter = &value
	}
	if enabledBy.Valid {
		value := enabledBy.String
		out.EnabledBy = &value
	}
	if enabledAt.Valid {
		value := enabledAt.Time.UTC()
		out.EnabledAt = &value
	}
	if pausedAt.Valid {
		value := pausedAt.Time.UTC()
		out.PausedAt = &value
	}
	if lastSuccess.Valid {
		value := lastSuccess.Time.UTC()
		out.LastSuccessAt = &value
	}
	if lastFailure.Valid {
		value := lastFailure.Time.UTC()
		out.LastFailureAt = &value
	}
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, err
}

func scanTikTokAutoSMLJob(scanner tikTokAutoSMLScanner) (models.TikTokAutoSMLJob, error) {
	var out models.TikTokAutoSMLJob
	var billID sql.NullString
	var leaseUntil sql.NullTime
	err := scanner.Scan(&out.ID, &out.ShopID, &out.OrderID, &out.Status, &out.TriggerStatusSnapshot, &out.TriggerTransitionAt,
		&out.TriggerConfigVersion, &out.SourceHash, &out.BillFingerprint, &out.RouteSignature, &billID, &out.SMLDocNo, &out.ReviewDigest,
		&out.DocumentTime, &out.Attempts, &out.NextRunAt, &leaseUntil, &out.LastErrorCode, &out.LastErrorMessage, &out.CreatedAt, &out.UpdatedAt)
	if billID.Valid {
		value := billID.String
		out.BillID = &value
	}
	if leaseUntil.Valid {
		value := leaseUntil.Time.UTC()
		out.LeaseUntil = &value
	}
	return out, err
}

func trimTikTokAutoSMLError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
