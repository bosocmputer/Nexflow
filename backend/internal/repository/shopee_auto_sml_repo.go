package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"nexflow/internal/models"
)

type ShopeeAutoSMLRepo struct {
	db *sql.DB
}

func NewShopeeAutoSMLRepo(db *sql.DB) *ShopeeAutoSMLRepo {
	return &ShopeeAutoSMLRepo{db: db}
}

func (r *ShopeeAutoSMLRepo) EnsureSettings(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO shopee_auto_sml_settings (shop_id)
		SELECT shop_id FROM shopee_api_connections WHERE disabled_at IS NULL
		ON CONFLICT (shop_id) DO NOTHING`)
	return err
}

func (r *ShopeeAutoSMLRepo) ListSettings(ctx context.Context) ([]models.ShopeeAutoSMLSetting, error) {
	if err := r.EnsureSettings(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT st.shop_id,
		       COALESCE(NULLIF(c.label, ''), NULLIF(c.shop_name, ''), 'Shop ' || st.shop_id::text),
		       st.enabled, st.eligible_after, st.route_signature, st.enabled_by::text,
		       st.enabled_at, st.paused_reason, st.paused_at,
		       st.consecutive_system_failures, st.last_success_at, st.last_failure_at,
		       COUNT(j.id) FILTER (WHERE j.status IN ('queued','running','retry_wait'))::int,
		       COUNT(j.id) FILTER (WHERE j.status = 'needs_review')::int,
		       COUNT(j.id) FILTER (WHERE j.status = 'failed')::int,
		       MIN(j.created_at) FILTER (WHERE j.status IN ('queued','running','retry_wait')),
		       st.updated_at
		  FROM shopee_auto_sml_settings st
		  JOIN shopee_api_connections c ON c.shop_id = st.shop_id AND c.disabled_at IS NULL
		  LEFT JOIN shopee_auto_sml_jobs j ON j.shop_id = st.shop_id
		 GROUP BY st.shop_id, c.label, c.shop_name
		 ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ShopeeAutoSMLSetting{}
	for rows.Next() {
		setting, err := scanShopeeAutoSMLSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, setting)
	}
	return out, rows.Err()
}

func (r *ShopeeAutoSMLRepo) GetSetting(ctx context.Context, shopID int64) (*models.ShopeeAutoSMLSetting, error) {
	if shopID <= 0 {
		return nil, fmt.Errorf("shop_id is required")
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO shopee_auto_sml_settings (shop_id)
		SELECT shop_id FROM shopee_api_connections WHERE shop_id=$1 AND disabled_at IS NULL
		ON CONFLICT (shop_id) DO NOTHING`, shopID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT st.shop_id,
		       COALESCE(NULLIF(c.label, ''), NULLIF(c.shop_name, ''), 'Shop ' || st.shop_id::text),
		       st.enabled, st.eligible_after, st.route_signature, st.enabled_by::text,
		       st.enabled_at, st.paused_reason, st.paused_at,
		       st.consecutive_system_failures, st.last_success_at, st.last_failure_at,
		       COUNT(j.id) FILTER (WHERE j.status IN ('queued','running','retry_wait'))::int,
		       COUNT(j.id) FILTER (WHERE j.status = 'needs_review')::int,
		       COUNT(j.id) FILTER (WHERE j.status = 'failed')::int,
		       MIN(j.created_at) FILTER (WHERE j.status IN ('queued','running','retry_wait')),
		       st.updated_at
		  FROM shopee_auto_sml_settings st
		  JOIN shopee_api_connections c ON c.shop_id = st.shop_id AND c.disabled_at IS NULL
		  LEFT JOIN shopee_auto_sml_jobs j ON j.shop_id = st.shop_id
		 WHERE st.shop_id=$1
		 GROUP BY st.shop_id, c.label, c.shop_name`, shopID)
	setting, err := scanShopeeAutoSMLSetting(row)
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

type autoSMLSettingScanner interface {
	Scan(dest ...any) error
}

func scanShopeeAutoSMLSetting(s autoSMLSettingScanner) (models.ShopeeAutoSMLSetting, error) {
	var out models.ShopeeAutoSMLSetting
	var eligibleAfter, enabledAt, pausedAt, lastSuccessAt, lastFailureAt, oldestQueuedAt sql.NullTime
	var enabledBy sql.NullString
	err := s.Scan(
		&out.ShopID, &out.ShopLabel, &out.Enabled, &eligibleAfter, &out.RouteSignature, &enabledBy,
		&enabledAt, &out.PausedReason, &pausedAt, &out.ConsecutiveSystemFailures,
		&lastSuccessAt, &lastFailureAt, &out.QueuedCount, &out.NeedsReviewCount,
		&out.FailedCount, &oldestQueuedAt, &out.UpdatedAt,
	)
	if err != nil {
		return out, err
	}
	if eligibleAfter.Valid {
		out.EligibleAfter = &eligibleAfter.Time
	}
	if enabledAt.Valid {
		out.EnabledAt = &enabledAt.Time
	}
	if pausedAt.Valid {
		out.PausedAt = &pausedAt.Time
	}
	if lastSuccessAt.Valid {
		out.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastFailureAt.Valid {
		out.LastFailureAt = &lastFailureAt.Time
	}
	if enabledBy.Valid {
		out.EnabledBy = &enabledBy.String
	}
	if oldestQueuedAt.Valid {
		out.OldestQueuedAt = &oldestQueuedAt.Time
		if time.Since(oldestQueuedAt.Time) > 15*time.Minute {
			out.OperationalWarning = "คิว Auto SML รอนานเกิน 15 นาที"
		}
	}
	if out.QueuedCount > 100 {
		out.OperationalWarning = "คิว Auto SML มากกว่า 100 งาน"
	}
	return out, nil
}

func (r *ShopeeAutoSMLRepo) SetEnabled(ctx context.Context, shopID int64, enabled bool, userID, routeSignature string) (*models.ShopeeAutoSMLSetting, error) {
	if shopID <= 0 {
		return nil, fmt.Errorf("shop_id is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if enabled {
		if strings.TrimSpace(routeSignature) == "" {
			return nil, fmt.Errorf("route signature is required")
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO shopee_auto_sml_settings
			  (shop_id,enabled,eligible_after,route_signature,enabled_by,enabled_at,
			   paused_reason,paused_at,consecutive_system_failures,updated_at)
			SELECT shop_id,true,NOW(),$2,NULLIF($3,'')::uuid,NOW(),'',NULL,0,NOW()
			  FROM shopee_api_connections WHERE shop_id=$1 AND disabled_at IS NULL
			ON CONFLICT (shop_id) DO UPDATE
			  SET enabled=true,eligible_after=NOW(),route_signature=EXCLUDED.route_signature,
			      enabled_by=EXCLUDED.enabled_by,enabled_at=NOW(),paused_reason='',paused_at=NULL,
			      consecutive_system_failures=0,updated_at=NOW()`, shopID, strings.TrimSpace(routeSignature), strings.TrimSpace(userID))
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, sql.ErrNoRows
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shopee_auto_sml_settings (shop_id,enabled,updated_at)
			SELECT shop_id,false,NOW() FROM shopee_api_connections WHERE shop_id=$1 AND disabled_at IS NULL
			ON CONFLICT (shop_id) DO UPDATE SET enabled=false,updated_at=NOW()`, shopID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE shopee_auto_sml_jobs
			   SET status='cancelled',lease_until=NULL,completed_at=NOW(),
			       last_error_code='automation_disabled',
			       last_error_message='ปิดการสร้างบิล SML อัตโนมัติแล้ว',updated_at=NOW()
			 WHERE shop_id=$1 AND status IN ('queued','retry_wait')`, shopID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSetting(ctx, shopID)
}

func (r *ShopeeAutoSMLRepo) Enqueue(ctx context.Context, shopID int64, orderSN string, createTime time.Time, updateTime *time.Time, readyToShipAt time.Time, routeSignature string) (bool, error) {
	orderSN = strings.TrimSpace(orderSN)
	if shopID <= 0 || orderSN == "" || createTime.IsZero() || readyToShipAt.IsZero() {
		return false, nil
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO shopee_auto_sml_jobs
		  (shop_id,order_sn,order_create_time,order_update_time,route_signature)
		SELECT st.shop_id,$2,$3,$4,st.route_signature
		  FROM shopee_auto_sml_settings st
		  JOIN shopee_api_connections c ON c.shop_id=st.shop_id AND c.disabled_at IS NULL
		 WHERE st.shop_id=$1 AND st.enabled=true AND st.paused_reason=''
		   AND st.eligible_after IS NOT NULL AND $5 >= st.eligible_after
		   AND st.route_signature=$6
		ON CONFLICT (shop_id,order_sn) DO NOTHING`,
		shopID, orderSN, createTime, updateTime, readyToShipAt, strings.TrimSpace(routeSignature))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *ShopeeAutoSMLRepo) RecoverStaleJobs(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET status='queued',next_run_at=NOW(),lease_until=NULL,
		       last_error_code='worker_lease_expired',
		       last_error_message='กู้คืนงานหลัง worker หยุดทำงาน',updated_at=NOW()
		 WHERE status='running' AND lease_until < NOW()`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (r *ShopeeAutoSMLRepo) LeaseJobs(ctx context.Context, limit int, lease time.Duration) ([]models.ShopeeAutoSMLJob, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH picked AS (
		  SELECT j.id
		    FROM shopee_auto_sml_jobs j
		    JOIN shopee_auto_sml_settings st ON st.shop_id=j.shop_id
		   WHERE j.status IN ('queued','retry_wait') AND j.next_run_at<=NOW()
		     AND st.enabled=true AND st.paused_reason=''
		     AND NOT EXISTS (
		       SELECT 1 FROM shopee_auto_sml_jobs active
		        WHERE active.shop_id=j.shop_id AND active.status='running'
		          AND active.lease_until>NOW()
		     )
		     AND j.id=(
		       SELECT next_job.id FROM shopee_auto_sml_jobs next_job
		        WHERE next_job.shop_id=j.shop_id
		          AND next_job.status IN ('queued','retry_wait')
		          AND next_job.next_run_at<=NOW()
		        ORDER BY next_job.next_run_at,next_job.created_at
		        LIMIT 1
		     )
		   ORDER BY j.next_run_at,j.created_at
		   LIMIT $1
		   FOR UPDATE OF j SKIP LOCKED
		)
		UPDATE shopee_auto_sml_jobs j
		   SET status='running',attempts=attempts+1,started_at=COALESCE(started_at,NOW()),
		       lease_until=NOW()+($2*INTERVAL '1 second'),updated_at=NOW()
		  FROM picked WHERE j.id=picked.id
		RETURNING j.id::text,j.shop_id,j.order_sn,j.bill_id::text,j.sml_doc_no,j.status,
		          j.attempts,j.next_run_at,j.lease_until,j.order_create_time,j.order_update_time,
		          j.bill_fingerprint,j.route_signature,j.document_time,j.last_error_code,j.last_error_message,
		          j.started_at,j.completed_at,j.created_at,j.updated_at`, limit, int64(lease.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []models.ShopeeAutoSMLJob{}
	for rows.Next() {
		job, err := scanShopeeAutoSMLJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

type autoSMLJobScanner interface {
	Scan(dest ...any) error
}

func scanShopeeAutoSMLJob(s autoSMLJobScanner) (models.ShopeeAutoSMLJob, error) {
	var out models.ShopeeAutoSMLJob
	var billID sql.NullString
	var leaseUntil, orderUpdate, startedAt, completedAt sql.NullTime
	err := s.Scan(&out.ID, &out.ShopID, &out.OrderSN, &billID, &out.SMLDocNo, &out.Status,
		&out.Attempts, &out.NextRunAt, &leaseUntil, &out.OrderCreateTime, &orderUpdate,
		&out.BillFingerprint, &out.RouteSignature, &out.DocumentTime, &out.LastErrorCode, &out.LastErrorMessage,
		&startedAt, &completedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	if billID.Valid {
		out.BillID = &billID.String
	}
	if leaseUntil.Valid {
		out.LeaseUntil = &leaseUntil.Time
	}
	if orderUpdate.Valid {
		out.OrderUpdateTime = &orderUpdate.Time
	}
	if startedAt.Valid {
		out.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		out.CompletedAt = &completedAt.Time
	}
	return out, nil
}

func (r *ShopeeAutoSMLRepo) GetJob(ctx context.Context, shopID int64, orderSN string) (*models.ShopeeAutoSMLJob, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text,shop_id,order_sn,bill_id::text,sml_doc_no,status,attempts,next_run_at,
		       lease_until,order_create_time,order_update_time,bill_fingerprint,route_signature,
		       document_time,last_error_code,last_error_message,started_at,completed_at,created_at,updated_at
		  FROM shopee_auto_sml_jobs WHERE shop_id=$1 AND order_sn=$2`, shopID, strings.TrimSpace(orderSN))
	job, err := scanShopeeAutoSMLJob(row)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *ShopeeAutoSMLRepo) GetOrSetDocumentTime(ctx context.Context, id, documentTime string) (string, error) {
	var persisted string
	err := r.db.QueryRowContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET document_time=CASE WHEN document_time='' THEN $2 ELSE document_time END,
		       updated_at=NOW()
		 WHERE id=$1::uuid
		 RETURNING document_time`, strings.TrimSpace(id), strings.TrimSpace(documentTime)).Scan(&persisted)
	return persisted, err
}

func (r *ShopeeAutoSMLRepo) LinkBill(ctx context.Context, id, billID, fingerprint string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs SET bill_id=NULLIF($2,'')::uuid,bill_fingerprint=$3,updated_at=NOW()
		 WHERE id=$1::uuid`, strings.TrimSpace(id), strings.TrimSpace(billID), strings.TrimSpace(fingerprint))
	return err
}

func (r *ShopeeAutoSMLRepo) MarkNeedsReview(ctx context.Context, id, billID, code, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET status='needs_review',bill_id=COALESCE(NULLIF($2,'')::uuid,bill_id),lease_until=NULL,
		       last_error_code=$3,last_error_message=$4,completed_at=NOW(),updated_at=NOW()
		 WHERE id=$1::uuid`, strings.TrimSpace(id), strings.TrimSpace(billID), trimAutoSMLError(code, 100), trimAutoSMLError(message, 800))
	return err
}

func (r *ShopeeAutoSMLRepo) MarkCancelled(ctx context.Context, id, code, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET status='cancelled',lease_until=NULL,last_error_code=$2,last_error_message=$3,
		       completed_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`,
		strings.TrimSpace(id), trimAutoSMLError(code, 100), trimAutoSMLError(message, 800))
	return err
}

func (r *ShopeeAutoSMLRepo) MarkSucceeded(ctx context.Context, id, billID, docNo string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var shopID int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET status='succeeded',bill_id=COALESCE(NULLIF($2,'')::uuid,bill_id),sml_doc_no=$3,
		       lease_until=NULL,last_error_code='',last_error_message='',completed_at=NOW(),updated_at=NOW()
		 WHERE id=$1::uuid RETURNING shop_id`, strings.TrimSpace(id), strings.TrimSpace(billID), strings.TrimSpace(docNo)).Scan(&shopID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE shopee_auto_sml_settings SET consecutive_system_failures=0,last_success_at=NOW(),updated_at=NOW()
		 WHERE shop_id=$1`, shopID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ShopeeAutoSMLRepo) MarkTransientFailure(ctx context.Context, id, code, message string, maxAttempts int) (paused bool, terminal bool, err error) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()
	var shopID int64
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT shop_id,attempts FROM shopee_auto_sml_jobs WHERE id=$1::uuid FOR UPDATE`, strings.TrimSpace(id)).Scan(&shopID, &attempts); err != nil {
		return false, false, err
	}
	terminal = attempts >= maxAttempts
	status := models.ShopeeAutoSMLRetryWait
	if terminal {
		status = models.ShopeeAutoSMLFailed
	}
	delay := autoSMLRetryDelay(attempts)
	if _, err := tx.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET status=$2,next_run_at=NOW()+($3*INTERVAL '1 second'),lease_until=NULL,
		       last_error_code=$4,last_error_message=$5,
		       completed_at=CASE WHEN $2='failed' THEN NOW() ELSE NULL END,updated_at=NOW()
		 WHERE id=$1::uuid`, id, status, int64(delay.Seconds()), trimAutoSMLError(code, 100), trimAutoSMLError(message, 800)); err != nil {
		return false, false, err
	}
	if terminal {
		var failures int
		if err := tx.QueryRowContext(ctx, `
			UPDATE shopee_auto_sml_settings
			   SET consecutive_system_failures=consecutive_system_failures+1,last_failure_at=NOW(),updated_at=NOW()
			 WHERE shop_id=$1 RETURNING consecutive_system_failures`, shopID).Scan(&failures); err != nil {
			return false, false, err
		}
		if failures >= 3 {
			paused = true
			if _, err := tx.ExecContext(ctx, `
				UPDATE shopee_auto_sml_settings
				   SET paused_reason='system_failures',paused_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
				return false, false, err
			}
		}
	}
	return paused, terminal, tx.Commit()
}

func (r *ShopeeAutoSMLRepo) MarkContentionRetry(ctx context.Context, id, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs
		   SET status='retry_wait',attempts=GREATEST(attempts-1,0),
		       next_run_at=NOW()+INTERVAL '1 minute',lease_until=NULL,
		       last_error_code='erp_send_busy',last_error_message=$2,updated_at=NOW()
		 WHERE id=$1::uuid`, strings.TrimSpace(id), trimAutoSMLError(message, 800))
	return err
}

func (r *ShopeeAutoSMLRepo) RetryJob(ctx context.Context, shopID int64, orderSN string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_jobs j
		   SET status='queued',attempts=0,next_run_at=NOW(),lease_until=NULL,
		       bill_fingerprint='',route_signature=st.route_signature,
		       last_error_code='',last_error_message='',completed_at=NULL,updated_at=NOW()
		  FROM shopee_auto_sml_settings st
		 WHERE j.shop_id=$1 AND j.order_sn=$2 AND j.shop_id=st.shop_id
		   AND st.enabled=true AND st.paused_reason=''
		   AND j.status IN ('needs_review','failed')`, shopID, strings.TrimSpace(orderSN))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *ShopeeAutoSMLRepo) DecorateSnapshots(ctx context.Context, snapshots []models.ShopeeOrderSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	args := make([]any, 0, len(snapshots)*2)
	pairs := make([]string, 0, len(snapshots))
	for i := range snapshots {
		args = append(args, snapshots[i].ShopID, snapshots[i].OrderSN)
		pairs = append(pairs, fmt.Sprintf("($%d,$%d)", len(args)-1, len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT j.shop_id,j.order_sn,
		       CASE WHEN st.paused_reason<>'' AND j.status IN ('queued','retry_wait') THEN 'paused' ELSE j.status END,
		       CASE WHEN st.paused_reason<>'' AND j.status IN ('queued','retry_wait') THEN st.paused_reason ELSE j.last_error_code END,
		       CASE
		         WHEN st.paused_reason='route_changed' AND j.status IN ('queued','retry_wait') THEN 'หยุดชั่วคราวเพราะเส้นทาง SML เปลี่ยน'
		         WHEN st.paused_reason='system_failures' AND j.status IN ('queued','retry_wait') THEN 'หยุดชั่วคราวเพราะ SML หรือระบบเชื่อมต่อล้มเหลวต่อเนื่อง'
		         ELSE j.last_error_message
		       END,
		       j.updated_at
		  FROM shopee_auto_sml_jobs j
		  JOIN shopee_auto_sml_settings st ON st.shop_id=j.shop_id
		 WHERE (j.shop_id,j.order_sn) IN (`+strings.Join(pairs, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct {
		shopID  int64
		orderSN string
	}
	views := map[key]models.ShopeeAutoSMLJobView{}
	for rows.Next() {
		var shopID int64
		var orderSN string
		var view models.ShopeeAutoSMLJobView
		var updatedAt time.Time
		if err := rows.Scan(&shopID, &orderSN, &view.Status, &view.ErrorCode, &view.ErrorMessage, &updatedAt); err != nil {
			return err
		}
		view.UpdatedAt = &updatedAt
		view.ManualSendLocked = view.Status == models.ShopeeAutoSMLRunning
		views[key{shopID: shopID, orderSN: orderSN}] = view
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range snapshots {
		snapshots[i].AutoSML = views[key{shopID: snapshots[i].ShopID, orderSN: snapshots[i].OrderSN}]
	}
	return nil
}

func (r *ShopeeAutoSMLRepo) PauseForRouteChange(ctx context.Context, shopID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE shopee_auto_sml_settings
		   SET paused_reason='route_changed',paused_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, shopID)
	return err
}

func autoSMLRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

func trimAutoSMLError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
