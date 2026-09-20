package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nexflow/internal/models"
)

type TikTokCancellationRepo struct {
	db *sql.DB
}

func NewTikTokCancellationRepo(db *sql.DB) *TikTokCancellationRepo {
	return &TikTokCancellationRepo{db: db}
}

type TikTokCancellationEvidenceRow struct {
	ShopID               string
	OrderID              string
	OrderStatus          string
	SourceHash           string
	LastSyncedAt         time.Time
	BillID               string
	BillSource           string
	BillSourceAccountKey string
	BillSourceFlow       string
	BillStatus           string
	BillDocumentRoute    string
	BillSMLDocNo         string
	SMLAttemptID         string
	SMLAttemptState      string
	SMLAttemptRoute      string
	SMLAttemptDocNo      string
}

func (r *TikTokCancellationRepo) Evidence(ctx context.Context, shopID, orderID string) (TikTokCancellationEvidenceRow, error) {
	var out TikTokCancellationEvidenceRow
	shopID, orderID = strings.TrimSpace(shopID), strings.TrimSpace(orderID)
	if r == nil || r.db == nil || shopID == "" || orderID == "" {
		return out, sql.ErrNoRows
	}
	err := r.db.QueryRowContext(ctx, `
		SELECT s.shop_id,s.order_id,s.order_status,s.source_hash,s.last_synced_at,
		       COALESCE(b.id::text,''),COALESCE(b.source,''),COALESCE(b.source_account_key,''),
		       COALESCE(b.raw_data->>'flow',''),COALESCE(b.status,''),COALESCE(b.document_route,''),COALESCE(b.sml_doc_no,''),
		       COALESCE(a.id::text,''),COALESCE(a.state,''),COALESCE(a.route,''),COALESCE(a.doc_no,'')
		  FROM tiktok_shop_order_snapshots s
		  LEFT JOIN LATERAL (
		    SELECT bill.* FROM bills bill
		     WHERE bill.source='tiktok' AND bill.sml_order_id=s.order_id AND bill.archived_at IS NULL
		     ORDER BY bill.created_at DESC,bill.id DESC LIMIT 1
		  ) b ON TRUE
		  LEFT JOIN bill_sml_attempts a ON a.id=b.current_sml_attempt_id
		 WHERE s.shop_id=$1 AND s.order_id=$2`, shopID, orderID).Scan(
		&out.ShopID, &out.OrderID, &out.OrderStatus, &out.SourceHash, &out.LastSyncedAt,
		&out.BillID, &out.BillSource, &out.BillSourceAccountKey, &out.BillSourceFlow,
		&out.BillStatus, &out.BillDocumentRoute, &out.BillSMLDocNo,
		&out.SMLAttemptID, &out.SMLAttemptState, &out.SMLAttemptRoute, &out.SMLAttemptDocNo,
	)
	return out, err
}

func (r *TikTokCancellationRepo) Latest(ctx context.Context, shopID, orderID, smlAttemptID string) (*models.TikTokSMLCancellation, error) {
	if r == nil || r.db == nil {
		return nil, sql.ErrNoRows
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+tikTokCancellationSelectColumns()+`
		FROM tiktok_shop_sml_cancellations
		WHERE shop_id=$1 AND order_id=$2 AND sml_attempt_id=$3::uuid`,
		strings.TrimSpace(shopID), strings.TrimSpace(orderID), strings.TrimSpace(smlAttemptID))
	result, err := scanTikTokCancellation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return result, err
}

func (r *TikTokCancellationRepo) CancelDocNoExists(ctx context.Context, docNo string) (bool, error) {
	if r == nil || r.db == nil {
		return false, sql.ErrNoRows
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM tiktok_shop_sml_cancellations WHERE cancel_sml_doc_no=$1
	)`, strings.TrimSpace(docNo)).Scan(&exists)
	return exists, err
}

type TikTokCancellationAttemptInput struct {
	ShopID, OrderID, BillID, SMLAttemptID string
	SaleSMLDocNo, CancelSMLDocNo          string
	SourceHash, ReviewDigest              string
	RouteEndpoint, RouteSignature         string
	RouteConfigVersion                    int64
	RequestPayload                        json.RawMessage
	CreatedBy                             string
}

const (
	TikTokCancellationStartStarted        = "started"
	TikTokCancellationStartDone           = "done"
	TikTokCancellationStartBusy           = "busy"
	TikTokCancellationStartStale          = "stale"
	TikTokCancellationStartReconciliation = "reconciliation"
)

func (r *TikTokCancellationRepo) UpsertPreview(ctx context.Context, input TikTokCancellationAttemptInput, response json.RawMessage) (*models.TikTokSMLCancellation, error) {
	if err := validateTikTokCancellationAttemptInput(input, false); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO tiktok_shop_sml_cancellations
		 (shop_id,order_id,bill_id,sml_attempt_id,sale_sml_doc_no,status,source_hash,review_digest,
		  route_endpoint,route_config_version,route_signature,response,created_by)
		VALUES ($1,$2,$3::uuid,$4::uuid,$5,'previewed',$6,$7,$8,$9,$10,
		        COALESCE(NULLIF($11,'')::jsonb,'{}'::jsonb),NULLIF($12,'')::uuid)
		ON CONFLICT (shop_id,order_id,sml_attempt_id) DO UPDATE SET
		 status='previewed',source_hash=EXCLUDED.source_hash,review_digest=EXCLUDED.review_digest,
		 route_endpoint=EXCLUDED.route_endpoint,route_config_version=EXCLUDED.route_config_version,
		 route_signature=EXCLUDED.route_signature,response=EXCLUDED.response,error_code='',error_message='',
		 cancel_sml_doc_no='',request_payload='{}'::jsonb,
		 created_by=COALESCE(EXCLUDED.created_by,tiktok_shop_sml_cancellations.created_by),updated_at=NOW()
		WHERE tiktok_shop_sml_cancellations.status IN ('previewed','failed','blocked')
		RETURNING `+tikTokCancellationSelectColumns(),
		input.ShopID, input.OrderID, input.BillID, input.SMLAttemptID, input.SaleSMLDocNo,
		input.SourceHash, input.ReviewDigest, input.RouteEndpoint, input.RouteConfigVersion,
		input.RouteSignature, rawJSONForDB(response), input.CreatedBy)
	return scanTikTokCancellation(row)
}

func (r *TikTokCancellationRepo) StartCreate(ctx context.Context, input TikTokCancellationAttemptInput) (*models.TikTokSMLCancellation, string, error) {
	if err := validateTikTokCancellationAttemptInput(input, true); err != nil {
		return nil, "", err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	lockKey := "tiktok_sml_cancel:" + input.ShopID + ":" + input.OrderID + ":" + input.SMLAttemptID
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return nil, "", err
	}
	existing, err := scanTikTokCancellation(tx.QueryRowContext(ctx, `
		SELECT `+tikTokCancellationSelectColumns()+`
		  FROM tiktok_shop_sml_cancellations
		 WHERE shop_id=$1 AND order_id=$2 AND sml_attempt_id=$3::uuid
		 FOR UPDATE`, input.ShopID, input.OrderID, input.SMLAttemptID))
	if err != nil {
		return nil, "", err
	}
	switch existing.Status {
	case "created", "already_exists":
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		return existing, TikTokCancellationStartDone, nil
	case "creating":
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		return existing, TikTokCancellationStartBusy, nil
	case "unknown":
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		return existing, TikTokCancellationStartReconciliation, nil
	}
	if existing.ReviewDigest != input.ReviewDigest || existing.SourceHash != input.SourceHash ||
		existing.RouteEndpoint != input.RouteEndpoint || existing.RouteConfigVersion != input.RouteConfigVersion ||
		existing.RouteSignature != input.RouteSignature || existing.BillID != input.BillID ||
		existing.SaleSMLDocNo != input.SaleSMLDocNo {
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		return existing, TikTokCancellationStartStale, nil
	}
	requestPayload := rawJSONForDB(input.RequestPayload)
	if existing.CancelSMLDocNo != "" {
		if existing.CancelSMLDocNo != input.CancelSMLDocNo || !jsonMessagesEqual(existing.RequestPayload, input.RequestPayload) {
			if err := tx.Commit(); err != nil {
				return nil, "", err
			}
			return existing, TikTokCancellationStartReconciliation, nil
		}
	}
	updated, err := scanTikTokCancellation(tx.QueryRowContext(ctx, `
		UPDATE tiktok_shop_sml_cancellations
		   SET status='creating',cancel_sml_doc_no=$2,request_payload=$3::jsonb,
		       error_code='',error_message='',completed_at=NULL,
		       created_by=COALESCE(NULLIF($4,'')::uuid,created_by),updated_at=NOW()
		 WHERE id=$1::uuid
		 RETURNING `+tikTokCancellationSelectColumns(), existing.ID, input.CancelSMLDocNo, requestPayload, input.CreatedBy))
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return updated, TikTokCancellationStartStarted, nil
}

func (r *TikTokCancellationRepo) Complete(ctx context.Context, id, status, cancelDocNo string, response json.RawMessage, errorCode, errorMessage string) (*models.TikTokSMLCancellation, error) {
	id, status = strings.TrimSpace(id), strings.TrimSpace(status)
	if id == "" || (status != "created" && status != "already_exists" && status != "failed" && status != "unknown") {
		return nil, fmt.Errorf("invalid TikTok cancellation completion")
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE tiktok_shop_sml_cancellations
		   SET status=$2,cancel_sml_doc_no=COALESCE(NULLIF($3,''),cancel_sml_doc_no),
		       response=COALESCE(NULLIF($4,'')::jsonb,'{}'::jsonb),error_code=$5,error_message=$6,
		       stock_recalc_status=CASE WHEN $2 IN ('created','already_exists') THEN 'pending' ELSE stock_recalc_status END,
		       stock_recalc_next_run_at=CASE WHEN $2 IN ('created','already_exists') THEN COALESCE(stock_recalc_next_run_at,NOW()) ELSE stock_recalc_next_run_at END,
		       completed_at=CASE WHEN $2 IN ('created','already_exists') THEN NOW() ELSE completed_at END,updated_at=NOW()
		 WHERE id=$1::uuid AND status='creating'
		 RETURNING `+tikTokCancellationSelectColumns(), id, status, strings.TrimSpace(cancelDocNo), rawJSONForDB(response), strings.TrimSpace(errorCode), truncateDBText(errorMessage, 800))
	return scanTikTokCancellation(row)
}

func (r *TikTokCancellationRepo) RecoverStaleStockRecalculations(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_sml_cancellations
		SET stock_recalc_status='failed',stock_recalc_error='Previous stock recalculation lease expired',
		    stock_recalc_next_run_at=NOW(),stock_recalc_lease_until=NULL,updated_at=NOW()
		WHERE stock_recalc_status='running' AND stock_recalc_lease_until<NOW()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *TikTokCancellationRepo) RecoverStaleCreates(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = 5 * time.Minute
	}
	result, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_sml_cancellations
		SET status='unknown',error_code='stale_create',
		    error_message='Previous SML cancellation request did not record a final result',updated_at=NOW()
		WHERE status='creating' AND updated_at<NOW()-($1*INTERVAL '1 second')`, int64(olderThan.Seconds()))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *TikTokCancellationRepo) ClaimStockRecalculation(ctx context.Context, lease time.Duration) (*models.TikTokSMLCancellation, error) {
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	row := r.db.QueryRowContext(ctx, `WITH picked AS (
		SELECT id FROM tiktok_shop_sml_cancellations
		WHERE stock_recalc_status IN ('pending','failed') AND stock_recalc_next_run_at<=NOW()
		ORDER BY stock_recalc_next_run_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE tiktok_shop_sml_cancellations c
	SET stock_recalc_status='running',stock_recalc_attempts=stock_recalc_attempts+1,
	    stock_recalc_lease_until=NOW()+($1*INTERVAL '1 second'),updated_at=NOW()
	FROM picked WHERE c.id=picked.id
	RETURNING `+tikTokCancellationSelectColumns(), int64(lease.Seconds()))
	result, err := scanTikTokCancellation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return result, err
}

func (r *TikTokCancellationRepo) CompleteStockRecalculation(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE tiktok_shop_sml_cancellations
		SET stock_recalc_status='succeeded',stock_recalc_error='',stock_recalc_next_run_at=NULL,
		    stock_recalc_lease_until=NULL,updated_at=NOW()
		WHERE id=$1::uuid AND stock_recalc_status='running'`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("TikTok cancellation stock recalculation lease was lost")
	}
	return nil
}

func (r *TikTokCancellationRepo) FailStockRecalculation(ctx context.Context, id, message string, maxAttempts int) (bool, error) {
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT stock_recalc_attempts FROM tiktok_shop_sml_cancellations
		WHERE id=$1::uuid AND stock_recalc_status='running' FOR UPDATE`, strings.TrimSpace(id)).Scan(&attempts); err != nil {
		return false, err
	}
	terminal := attempts >= maxAttempts
	delaySeconds := int64(15 * (1 << min(attempts, 8)))
	if delaySeconds > 3600 {
		delaySeconds = 3600
	}
	status, nextRunSQL := "failed", "NOW()+($3*INTERVAL '1 second')"
	if terminal {
		status, nextRunSQL = "manual_reconciliation", "NULL"
	}
	query := `UPDATE tiktok_shop_sml_cancellations
		SET stock_recalc_status=$2,stock_recalc_error=$4,stock_recalc_next_run_at=` + nextRunSQL + `,
		    stock_recalc_lease_until=NULL,updated_at=NOW()
		WHERE id=$1::uuid AND stock_recalc_status='running'`
	result, err := tx.ExecContext(ctx, query, strings.TrimSpace(id), status, delaySeconds, truncateDBText(message, 800))
	if err != nil {
		return false, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return false, fmt.Errorf("TikTok cancellation stock recalculation lease was lost")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return terminal, nil
}

func validateTikTokCancellationAttemptInput(input TikTokCancellationAttemptInput, requireCreate bool) error {
	if strings.TrimSpace(input.ShopID) == "" || strings.TrimSpace(input.OrderID) == "" ||
		strings.TrimSpace(input.BillID) == "" || strings.TrimSpace(input.SMLAttemptID) == "" ||
		strings.TrimSpace(input.SaleSMLDocNo) == "" || strings.TrimSpace(input.SourceHash) == "" ||
		strings.TrimSpace(input.ReviewDigest) == "" || strings.TrimSpace(input.RouteEndpoint) == "" ||
		input.RouteConfigVersion < 1 || strings.TrimSpace(input.RouteSignature) == "" {
		return fmt.Errorf("invalid TikTok cancellation attempt input")
	}
	if requireCreate && (strings.TrimSpace(input.CancelSMLDocNo) == "" || len(input.RequestPayload) == 0 || !json.Valid(input.RequestPayload)) {
		return fmt.Errorf("invalid TikTok cancellation create payload")
	}
	return nil
}

func tikTokCancellationSelectColumns() string {
	return `id::text,shop_id,order_id,bill_id::text,sml_attempt_id::text,sale_sml_doc_no,
	        cancel_sml_doc_no,status,source_hash,review_digest,route_endpoint,route_config_version,
	        route_signature,request_payload,response,error_code,error_message,created_by::text,
	        stock_recalc_status,stock_recalc_error,stock_recalc_attempts,stock_recalc_next_run_at,
	        stock_recalc_lease_until,completed_at,created_at,updated_at`
}

func tikTokCancellationColumns() []string {
	return []string{"id", "shop_id", "order_id", "bill_id", "sml_attempt_id", "sale_sml_doc_no",
		"cancel_sml_doc_no", "status", "source_hash", "review_digest", "route_endpoint", "route_config_version",
		"route_signature", "request_payload", "response", "error_code", "error_message", "created_by",
		"stock_recalc_status", "stock_recalc_error", "stock_recalc_attempts", "stock_recalc_next_run_at",
		"stock_recalc_lease_until", "completed_at", "created_at", "updated_at"}
}

func scanTikTokCancellation(row interface{ Scan(...any) error }) (*models.TikTokSMLCancellation, error) {
	var out models.TikTokSMLCancellation
	var createdBy sql.NullString
	var nextRun, leaseUntil, completedAt sql.NullTime
	var requestPayload, response []byte
	if err := row.Scan(
		&out.ID, &out.ShopID, &out.OrderID, &out.BillID, &out.SMLAttemptID, &out.SaleSMLDocNo,
		&out.CancelSMLDocNo, &out.Status, &out.SourceHash, &out.ReviewDigest, &out.RouteEndpoint,
		&out.RouteConfigVersion, &out.RouteSignature, &requestPayload, &response, &out.ErrorCode,
		&out.ErrorMessage, &createdBy, &out.StockRecalcStatus, &out.StockRecalcError, &out.StockRecalcAttempts,
		&nextRun, &leaseUntil, &completedAt, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	out.RequestPayload, out.Response = append(json.RawMessage(nil), requestPayload...), append(json.RawMessage(nil), response...)
	if createdBy.Valid {
		out.CreatedBy = &createdBy.String
	}
	if nextRun.Valid {
		value := nextRun.Time.UTC()
		out.StockRecalcNextRunAt = &value
	}
	if leaseUntil.Valid {
		value := leaseUntil.Time.UTC()
		out.StockRecalcLeaseUntil = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		out.CompletedAt = &value
	}
	return &out, nil
}

func rawJSONForDB(value json.RawMessage) string {
	if len(value) == 0 || !json.Valid(value) {
		return "{}"
	}
	return string(value)
}

func jsonMessagesEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftJSON, leftErr := json.Marshal(leftValue)
	rightJSON, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
