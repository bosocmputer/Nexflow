package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"nexflow/internal/models"
	"nexflow/internal/services/shopeeapi"
)

type ShopeeRealtimeRepo struct {
	db *sql.DB
}

type ShopeeSnapshotUpsert struct {
	ConnectionID string
	ShopID       int64
	ShopLabel    string
	Detail       shopeeapi.OrderDetail
	Source       string
}

type ShopeePushEventInput struct {
	ShopID      int64
	OrderSN     string
	PushCode    int
	PushName    string
	EventStatus string
	UpdateTime  time.Time
	Timestamp   time.Time
	DedupeKey   string
	RawPayload  json.RawMessage
	Headers     json.RawMessage
}

func NewShopeeRealtimeRepo(db *sql.DB) *ShopeeRealtimeRepo {
	return &ShopeeRealtimeRepo{db: db}
}

func (r *ShopeeRealtimeRepo) DB() *sql.DB { return r.db }

func (r *ShopeeRealtimeRepo) UpsertSnapshotFromDetail(ctx context.Context, in ShopeeSnapshotUpsert) (*models.ShopeeOrderSnapshot, error) {
	orderSN := strings.TrimSpace(in.Detail.OrderSN)
	if orderSN == "" || in.ShopID <= 0 {
		return nil, fmt.Errorf("shop_id and order_sn are required")
	}
	raw, err := json.Marshal(in.Detail)
	if err != nil {
		return nil, err
	}
	billID, billStatus, smlDocNo, _ := r.findShopeeBill(ctx, in.ShopID, orderSN)
	orderStatus := models.NormalizeShopeeOrderStatus(in.Detail.OrderStatus)
	erpStatus := deriveShopeeERPStatus(orderStatus, billStatus, smlDocNo)
	packageNumber, logisticsStatus, packageTracking, packageCarrier := firstPackageFields(in.Detail.PackageList)
	tracking := strings.TrimSpace(in.Detail.TrackingNumber)
	if tracking == "" {
		tracking = packageTracking
	}
	carrier := strings.TrimSpace(in.Detail.ShippingCarrier)
	if carrier == "" {
		carrier = strings.TrimSpace(in.Detail.CheckoutShippingCarrier)
	}
	if carrier == "" {
		carrier = packageCarrier
	}
	connectionIDArg := strings.TrimSpace(in.ConnectionID)
	billIDArg := strings.TrimSpace(billID)
	var updatedAt interface{} = nil
	if in.Detail.UpdateTime > 0 {
		updatedAt = time.Unix(in.Detail.UpdateTime, 0)
	}
	source := normalizeShopeeSnapshotSource(in.Source)

	var out models.ShopeeOrderSnapshot
	var connID sql.NullString
	var scannedBillID sql.NullString
	var lastOrderUpdate sql.NullTime
	err = r.db.QueryRowContext(ctx,
		`INSERT INTO shopee_order_snapshots
		  (connection_id, shop_id, shop_label, order_sn, order_status, erp_status,
		   bill_id, sml_doc_no, buyer_username, total_amount, currency, item_count,
		   package_number, logistics_status, tracking_number, shipping_carrier,
		   payment_method, raw_detail, last_order_update_at, last_update_source, last_synced_at, updated_at)
		 VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6,
		         NULLIF($7, '')::uuid, $8, $9, $10, $11, $12,
		         $13, $14, $15, $16, $17, $18, $19, $20, NOW(), NOW())
		 ON CONFLICT (shop_id, order_sn) DO UPDATE
		    SET connection_id = COALESCE(EXCLUDED.connection_id, shopee_order_snapshots.connection_id),
		        shop_label = COALESCE(NULLIF(EXCLUDED.shop_label, ''), shopee_order_snapshots.shop_label),
		        order_status = EXCLUDED.order_status,
		        erp_status = EXCLUDED.erp_status,
		        bill_id = COALESCE(EXCLUDED.bill_id, shopee_order_snapshots.bill_id),
		        sml_doc_no = COALESCE(NULLIF(EXCLUDED.sml_doc_no, ''), shopee_order_snapshots.sml_doc_no),
		        buyer_username = EXCLUDED.buyer_username,
		        total_amount = EXCLUDED.total_amount,
		        currency = EXCLUDED.currency,
		        item_count = EXCLUDED.item_count,
		        package_number = EXCLUDED.package_number,
		        logistics_status = EXCLUDED.logistics_status,
		        tracking_number = EXCLUDED.tracking_number,
		        shipping_carrier = EXCLUDED.shipping_carrier,
		        payment_method = EXCLUDED.payment_method,
		        raw_detail = EXCLUDED.raw_detail,
		        last_order_update_at = COALESCE(EXCLUDED.last_order_update_at, shopee_order_snapshots.last_order_update_at),
		        last_update_source = EXCLUDED.last_update_source,
		        last_synced_at = NOW(),
		        last_error = '',
		        updated_at = NOW()
		  RETURNING id::text, connection_id::text, shop_id, shop_label, order_sn, order_status,
		            erp_status, bill_id::text, sml_doc_no, buyer_username, total_amount::float8,
		            currency, item_count, package_number, logistics_status, tracking_number,
		            shipping_carrier, payment_method, raw_detail, last_order_update_at,
		            last_update_source, last_synced_at, last_error, created_at, updated_at`,
		connectionIDArg, in.ShopID, strings.TrimSpace(in.ShopLabel), orderSN, orderStatus,
		erpStatus, billIDArg, smlDocNo, strings.TrimSpace(in.Detail.BuyerUsername), in.Detail.TotalAmount,
		in.Detail.Currency, len(in.Detail.ItemList), packageNumber, logisticsStatus, tracking, carrier,
		strings.TrimSpace(in.Detail.PaymentMethod), raw, updatedAt, source,
	).Scan(
		&out.ID, &connID, &out.ShopID, &out.ShopLabel, &out.OrderSN, &out.OrderStatus,
		&out.ERPStatus, &scannedBillID, &out.SMLDocNo, &out.BuyerUsername, &out.TotalAmount,
		&out.Currency, &out.ItemCount, &out.PackageNumber, &out.LogisticsStatus, &out.TrackingNumber,
		&out.ShippingCarrier, &out.PaymentMethod, &out.RawDetail, &lastOrderUpdate,
		&out.LastUpdateSource, &out.LastSyncedAt, &out.LastError, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if connID.Valid {
		out.ConnectionID = &connID.String
	}
	if scannedBillID.Valid {
		out.BillID = &scannedBillID.String
	}
	if out.BillID != nil {
		out.DocumentRoute, _ = r.documentRouteForBill(ctx, *out.BillID)
	}
	if lastOrderUpdate.Valid {
		out.LastOrderUpdateAt = &lastOrderUpdate.Time
	}
	decorateShopeeSnapshotShippingMetadata(&out)
	return &out, nil
}

func (r *ShopeeRealtimeRepo) ListSnapshots(ctx context.Context, f models.ShopeeOrderSnapshotFilter) ([]models.ShopeeOrderSnapshot, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}
	where, args := shopeeSnapshotWhere(f)
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM shopee_order_snapshots s "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.id::text, s.connection_id::text, s.shop_id, s.shop_label, s.order_sn, s.order_status,
		        s.erp_status, s.bill_id::text, s.sml_doc_no,
		        COALESCE(c.cancel_sml_doc_no, '') AS sml_cancel_doc_no,
		        COALESCE(c.status, '') AS sml_cancel_status,
		        COALESCE(c.error, '') AS sml_cancel_error,
		        COALESCE(b.document_route, '') AS document_route,
		        COALESCE(b.raw_data->>'flow', '') AS bill_source_flow,
		        s.buyer_username, s.total_amount::float8, s.currency, s.item_count,
		        s.package_number, s.logistics_status, s.tracking_number, s.shipping_carrier,
		        s.payment_method, COALESCE(p.status, '') AS payment_breakdown_status,
		        s.raw_detail, s.last_order_update_at,
		        s.last_update_source, s.last_synced_at, s.last_error, s.created_at, s.updated_at,
		        COALESCE((
		          SELECT a.status
		            FROM shopee_action_outbox a
		           WHERE a.shop_id = s.shop_id
		             AND a.order_sn = s.order_sn
		             AND a.action = 'ship_order'
		           ORDER BY a.created_at DESC
		           LIMIT 1
		        ), '') AS ship_action_status
		   FROM shopee_order_snapshots s
		   LEFT JOIN bills b ON b.id = s.bill_id
		   LEFT JOIN shopee_order_payment_snapshots p
		          ON p.shop_id = s.shop_id
		         AND p.order_sn = s.order_sn
		   LEFT JOIN LATERAL (
		     SELECT cancel_sml_doc_no, status, error
		       FROM shopee_sml_cancellations c
		      WHERE c.shop_id = s.shop_id
		        AND c.order_sn = s.order_sn
		      ORDER BY c.updated_at DESC
		      LIMIT 1
		   ) c ON TRUE `+where+`
		  ORDER BY COALESCE(s.last_order_update_at, s.updated_at) DESC, s.updated_at DESC
		  LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.ShopeeOrderSnapshot{}
	for rows.Next() {
		row, err := scanShopeeSnapshot(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

func (r *ShopeeRealtimeRepo) Counts(ctx context.Context, shopID int64) (models.ShopeeRealtimeCounts, error) {
	args := []interface{}{}
	where := ""
	if shopID > 0 {
		where = "WHERE shop_id = $1"
		args = append(args, shopID)
	}
	var out models.ShopeeRealtimeCounts
	var unpaid, toShip, shipping, completed, cancelled int
	err := r.db.QueryRowContext(ctx,
		`SELECT
		    COUNT(*)::int,
		    COUNT(*) FILTER (WHERE erp_status = 'pending')::int,
		    COUNT(*) FILTER (WHERE erp_status = 'pending_erp')::int,
		    COUNT(*) FILTER (WHERE erp_status = 'needs_review')::int,
		    COUNT(*) FILTER (WHERE erp_status = 'sent')::int,
		    COUNT(*) FILTER (WHERE erp_status = 'sent' AND order_status IN ('READY_TO_SHIP','PROCESSED'))::int,
		    COUNT(*) FILTER (WHERE order_status IN ('SHIPPED','COMPLETED'))::int,
		    COUNT(*) FILTER (WHERE order_status = 'CANCELLED')::int,
		    COUNT(*) FILTER (WHERE erp_status = 'failed')::int,
		    COUNT(*) FILTER (WHERE order_status = 'UNPAID')::int,
		    COUNT(*) FILTER (WHERE order_status = 'READY_TO_SHIP')::int,
		    COUNT(*) FILTER (WHERE order_status IN ('PROCESSED','SHIPPED'))::int,
		    COUNT(*) FILTER (WHERE order_status = 'COMPLETED')::int,
		    COUNT(*) FILTER (WHERE order_status IN ('CANCELLED','IN_CANCEL'))::int
		   FROM shopee_order_snapshots `+where,
		args...,
	).Scan(&out.Total, &out.NewOrders, &out.PendingERP, &out.NeedsReview, &out.ERPSaved, &out.WaitingShip, &out.Shipped, &out.Cancelled, &out.Failed,
		&unpaid, &toShip, &shipping, &completed, &cancelled)
	out.Tabs = map[string]int{
		"all":       out.Total,
		"unpaid":    unpaid,
		"to_ship":   toShip,
		"shipping":  shipping,
		"completed": completed,
		"cancelled": cancelled,
	}
	return out, err
}

func (r *ShopeeRealtimeRepo) FindSnapshot(ctx context.Context, shopID int64, orderSN string) (*models.ShopeeOrderSnapshot, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.id::text, s.connection_id::text, s.shop_id, s.shop_label, s.order_sn, s.order_status,
		        s.erp_status, s.bill_id::text, s.sml_doc_no,
		        COALESCE(c.cancel_sml_doc_no, '') AS sml_cancel_doc_no,
		        COALESCE(c.status, '') AS sml_cancel_status,
		        COALESCE(c.error, '') AS sml_cancel_error,
		        COALESCE(b.document_route, '') AS document_route,
		        COALESCE(b.raw_data->>'flow', '') AS bill_source_flow,
		        s.buyer_username, s.total_amount::float8, s.currency, s.item_count,
		        s.package_number, s.logistics_status, s.tracking_number, s.shipping_carrier,
		        s.payment_method, COALESCE(p.status, '') AS payment_breakdown_status,
		        s.raw_detail, s.last_order_update_at,
		        s.last_update_source, s.last_synced_at, s.last_error, s.created_at, s.updated_at,
		        COALESCE((
		          SELECT a.status
		            FROM shopee_action_outbox a
		           WHERE a.shop_id = s.shop_id
		             AND a.order_sn = s.order_sn
		             AND a.action = 'ship_order'
		           ORDER BY a.created_at DESC
		           LIMIT 1
		        ), '') AS ship_action_status
		   FROM shopee_order_snapshots s
		   LEFT JOIN bills b ON b.id = s.bill_id
		   LEFT JOIN shopee_order_payment_snapshots p
		          ON p.shop_id = s.shop_id
		         AND p.order_sn = s.order_sn
		   LEFT JOIN LATERAL (
		     SELECT cancel_sml_doc_no, status, error
		       FROM shopee_sml_cancellations c
		      WHERE c.shop_id = s.shop_id
		        AND c.order_sn = s.order_sn
		      ORDER BY c.updated_at DESC
		      LIMIT 1
		   ) c ON TRUE
		  WHERE s.shop_id = $1 AND s.order_sn = $2
		  LIMIT 1`,
		shopID, strings.TrimSpace(orderSN),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	row, err := scanShopeeSnapshot(rows)
	if err != nil {
		return nil, err
	}
	return &row, rows.Err()
}

func (r *ShopeeRealtimeRepo) InsertPushEvent(ctx context.Context, in ShopeePushEventInput) (bool, error) {
	dedupeKey := strings.TrimSpace(in.DedupeKey)
	if dedupeKey == "" {
		sum := sha256.Sum256(in.RawPayload)
		dedupeKey = hex.EncodeToString(sum[:])
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO shopee_push_events
		  (shop_id, order_sn, push_code, push_name, event_status, event_update_time,
		   event_timestamp, dedupe_key, raw_payload, headers, processing_status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, 'null')::jsonb, '{}'::jsonb),
		         COALESCE(NULLIF($10, 'null')::jsonb, '{}'::jsonb), 'queued')
		 ON CONFLICT (dedupe_key) DO NOTHING`,
		in.ShopID, strings.TrimSpace(in.OrderSN), in.PushCode, strings.TrimSpace(in.PushName),
		strings.ToUpper(strings.TrimSpace(in.EventStatus)), nullableTime(in.UpdateTime), nullableTime(in.Timestamp),
		dedupeKey, string(in.RawPayload), string(in.Headers),
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *ShopeeRealtimeRepo) EnqueueReconcileJob(ctx context.Context, shopID int64, orderSN, reason string) error {
	if shopID <= 0 || strings.TrimSpace(orderSN) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO shopee_reconcile_jobs (shop_id, order_sn, reason)
		 VALUES ($1, $2, $3)`,
		shopID, strings.TrimSpace(orderSN), strings.TrimSpace(reason),
	)
	return err
}

func (r *ShopeeRealtimeRepo) RecoverStaleReconcileJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = 5 * time.Minute
	}
	seconds := int64(olderThan.Seconds())
	if seconds <= 0 {
		seconds = int64((5 * time.Minute).Seconds())
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE shopee_reconcile_jobs
		    SET status = 'queued',
		        next_run_at = NOW(),
		        updated_at = NOW()
		  WHERE status = 'running'
		    AND updated_at < NOW() - ($1 * INTERVAL '1 second')`,
		seconds,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (r *ShopeeRealtimeRepo) LeaseReconcileJobs(ctx context.Context, limit int) ([]models.ShopeeReconcileJob, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx,
		`WITH picked AS (
		   SELECT id
		     FROM shopee_reconcile_jobs
		    WHERE status = 'queued'
		      AND next_run_at <= NOW()
		    ORDER BY next_run_at ASC, created_at ASC
		    LIMIT $1
		    FOR UPDATE SKIP LOCKED
		 )
		 UPDATE shopee_reconcile_jobs j
		    SET status = 'running',
		        attempts = attempts + 1,
		        updated_at = NOW()
		   FROM picked
		  WHERE j.id = picked.id
		  RETURNING j.id::text, j.shop_id, j.order_sn, j.reason, j.status,
		            j.attempts, j.created_at, j.updated_at`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []models.ShopeeReconcileJob{}
	for rows.Next() {
		var job models.ShopeeReconcileJob
		if err := rows.Scan(&job.ID, &job.ShopID, &job.OrderSN, &job.Reason, &job.Status, &job.Attempts, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *ShopeeRealtimeRepo) MarkReconcileJobDone(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE shopee_reconcile_jobs
		    SET status = 'done',
		        last_error = '',
		        updated_at = NOW()
		  WHERE id = $1::uuid`,
		strings.TrimSpace(id),
	)
	return err
}

func (r *ShopeeRealtimeRepo) MarkReconcileJobFailed(ctx context.Context, id string, errMsg string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE shopee_reconcile_jobs
		    SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'queued' END,
		        next_run_at = CASE
		          WHEN attempts >= 5 THEN NOW() + INTERVAL '1 hour'
		          ELSE NOW() + ((attempts * attempts * 30)::text || ' seconds')::interval
		        END,
		        last_error = $2,
		        updated_at = NOW()
		  WHERE id = $1::uuid`,
		strings.TrimSpace(id), strings.TrimSpace(errMsg),
	)
	return err
}

func (r *ShopeeRealtimeRepo) MarkPushEventsForOrder(ctx context.Context, shopID int64, orderSN, status, errMsg string) error {
	if shopID <= 0 || strings.TrimSpace(orderSN) == "" {
		return nil
	}
	if status == "" {
		status = "processed"
	}
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE shopee_push_events
		    SET processing_status = $3,
		        error = $4,
		        processed_at = CASE WHEN $3 IN ('processed','failed') THEN NOW() ELSE processed_at END
		  WHERE shop_id = $1
		    AND order_sn = $2
		    AND processing_status IN ('pending','queued')`,
		shopID, strings.TrimSpace(orderSN), strings.TrimSpace(status), strings.TrimSpace(errMsg),
	)
	return err
}

func (r *ShopeeRealtimeRepo) LinkSnapshotBill(ctx context.Context, shopID int64, orderSN, billID, smlDocNo, erpStatus string) error {
	if shopID <= 0 || strings.TrimSpace(orderSN) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE shopee_order_snapshots
		    SET bill_id = COALESCE(NULLIF($3, '')::uuid, bill_id),
		        sml_doc_no = COALESCE(NULLIF($4, ''), sml_doc_no),
		        erp_status = COALESCE(NULLIF($5, ''), erp_status),
		        last_error = CASE
		          WHEN NULLIF($5, '') IN ('pending','pending_erp','sent') THEN ''
		          ELSE last_error
		        END,
		        updated_at = NOW()
		  WHERE shop_id = $1
		    AND order_sn = $2`,
		shopID, strings.TrimSpace(orderSN), strings.TrimSpace(billID), strings.TrimSpace(smlDocNo), strings.TrimSpace(erpStatus),
	)
	return err
}

type ShopeeSnapshotRef struct {
	ShopID  int64
	OrderSN string
}

func (r *ShopeeRealtimeRepo) UpdateSnapshotForBillSendResult(ctx context.Context, billID, erpStatus, smlDocNo, errMsg string) ([]ShopeeSnapshotRef, error) {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return nil, nil
	}
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	rows, err := r.db.QueryContext(ctx,
		`UPDATE shopee_order_snapshots
		    SET erp_status = COALESCE(NULLIF($2, ''), erp_status),
		        sml_doc_no = COALESCE(NULLIF($3, ''), sml_doc_no),
		        last_error = $4,
		        updated_at = NOW()
		  WHERE bill_id = $1::uuid
		  RETURNING shop_id, order_sn`,
		billID, strings.TrimSpace(erpStatus), strings.TrimSpace(smlDocNo), strings.TrimSpace(errMsg),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShopeeSnapshotRef{}
	for rows.Next() {
		var ref ShopeeSnapshotRef
		if err := rows.Scan(&ref.ShopID, &ref.OrderSN); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (r *ShopeeRealtimeRepo) HasSnapshotForBill(ctx context.Context, billID string) (bool, error) {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1
		     FROM shopee_order_snapshots
		    WHERE bill_id = $1::uuid
		)`,
		billID,
	).Scan(&exists)
	return exists, err
}

func (r *ShopeeRealtimeRepo) ArchiveBillAndUnlinkSnapshotForRecreate(ctx context.Context, billID, userID, reason string) ([]ShopeeSnapshotRef, error) {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return nil, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "shopee realtime recreate with current route"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx,
		`UPDATE shopee_order_snapshots
		    SET bill_id = NULL,
		        sml_doc_no = '',
		        erp_status = 'pending',
		        last_error = '',
		        updated_at = NOW()
		  WHERE bill_id = $1::uuid
		    AND COALESCE(sml_doc_no, '') = ''
		  RETURNING shop_id, order_sn`,
		billID,
	)
	if err != nil {
		return nil, err
	}
	refs := []ShopeeSnapshotRef{}
	for rows.Next() {
		var ref ShopeeSnapshotRef
		if err := rows.Scan(&ref.ShopID, &ref.OrderSN); err != nil {
			_ = rows.Close()
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		if err := tx.Rollback(); err != nil {
			return nil, err
		}
		rollback = false
		return refs, nil
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE bills
		    SET archived_at = COALESCE(archived_at, NOW()),
		        archived_by = NULLIF($2, '')::uuid,
		        archive_reason = $3
		  WHERE id = $1::uuid
		    AND archived_at IS NULL
		    AND COALESCE(sml_doc_no, '') = ''`,
		billID, strings.TrimSpace(userID), reason,
	)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("bill is not eligible for recreate")
	}
	for _, ref := range refs {
		key := fmt.Sprintf("create_document:%d:%s", ref.ShopID, strings.TrimSpace(ref.OrderSN))
		_, err := tx.ExecContext(ctx,
			`UPDATE shopee_action_outbox
			    SET status = 'pending',
			        bill_id = NULL,
			        sml_doc_no = '',
			        response = '{}'::jsonb,
			        error = '',
			        updated_at = NOW()
			  WHERE idempotency_key = $1
			    AND action = 'create_document'
			    AND status IN ('done','blocked','failed')`,
			key,
		)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rollback = false
	return refs, nil
}

func (r *ShopeeRealtimeRepo) StartAction(ctx context.Context, shopID int64, orderSN, action, userID string, request json.RawMessage) (*models.ShopeeActionOutbox, string, error) {
	orderSN = strings.TrimSpace(orderSN)
	action = strings.TrimSpace(action)
	if shopID <= 0 || orderSN == "" || action == "" {
		return nil, "", fmt.Errorf("shop_id, order_sn, and action are required")
	}
	key := fmt.Sprintf("%s:%d:%s", action, shopID, orderSN)
	req := string(request)
	if strings.TrimSpace(req) == "" {
		req = "{}"
	}
	var out models.ShopeeActionOutbox
	var billID sql.NullString
	var createdBy sql.NullString
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO shopee_action_outbox
		  (shop_id, order_sn, action, idempotency_key, status, request, created_by)
		 VALUES ($1, $2, $3, $4, 'running', COALESCE(NULLIF($5, '')::jsonb, '{}'::jsonb), NULLIF($6, '')::uuid)
		 ON CONFLICT (idempotency_key) DO NOTHING
		 RETURNING id::text, shop_id, order_sn, action, idempotency_key, status,
		           bill_id::text, sml_doc_no, error, created_by::text, created_at, updated_at`,
		shopID, orderSN, action, key, req, strings.TrimSpace(userID),
	).Scan(&out.ID, &out.ShopID, &out.OrderSN, &out.Action, &out.IdempotencyKey, &out.Status, &billID, &out.SMLDocNo, &out.Error, &createdBy, &out.CreatedAt, &out.UpdatedAt)
	if err == nil {
		if billID.Valid {
			out.BillID = &billID.String
		}
		if createdBy.Valid {
			out.CreatedBy = &createdBy.String
		}
		return &out, "started", nil
	}
	if err != sql.ErrNoRows {
		return nil, "", err
	}

	err = r.db.QueryRowContext(ctx,
		`UPDATE shopee_action_outbox
		    SET status = 'running',
		        request = COALESCE(NULLIF($2, '')::jsonb, request),
		        error = '',
		        bill_id = CASE
		          WHEN status = 'done' AND action = 'create_document' THEN NULL
		          ELSE bill_id
		        END,
		        sml_doc_no = CASE
		          WHEN status = 'done' AND action = 'create_document' THEN ''
		          ELSE sml_doc_no
		        END,
		        response = CASE
		          WHEN status = 'done' AND action = 'create_document' THEN '{}'::jsonb
		          ELSE response
		        END,
		        created_by = COALESCE(NULLIF($3, '')::uuid, created_by),
		        updated_at = NOW()
		  WHERE idempotency_key = $1
		    AND (
		      status IN ('pending','failed','blocked')
		      OR (status = 'running' AND updated_at < NOW() - INTERVAL '5 minutes')
		      OR (
		        status = 'done'
		        AND action = 'create_document'
		        AND EXISTS (
		          SELECT 1
		            FROM bills b
		           WHERE b.id = shopee_action_outbox.bill_id
		             AND b.archived_at IS NOT NULL
		             AND COALESCE(b.sml_doc_no, '') = ''
		        )
		      )
		    )
		  RETURNING id::text, shop_id, order_sn, action, idempotency_key, status,
		            bill_id::text, sml_doc_no, error, created_by::text, created_at, updated_at`,
		key, req, strings.TrimSpace(userID),
	).Scan(&out.ID, &out.ShopID, &out.OrderSN, &out.Action, &out.IdempotencyKey, &out.Status, &billID, &out.SMLDocNo, &out.Error, &createdBy, &out.CreatedAt, &out.UpdatedAt)
	if err == nil {
		if billID.Valid {
			out.BillID = &billID.String
		}
		if createdBy.Valid {
			out.CreatedBy = &createdBy.String
		}
		return &out, "started", nil
	}
	if err != sql.ErrNoRows {
		return nil, "", err
	}

	err = r.db.QueryRowContext(ctx,
		`SELECT id::text, shop_id, order_sn, action, idempotency_key, status,
		        bill_id::text, sml_doc_no, error, created_by::text, created_at, updated_at
		   FROM shopee_action_outbox
		  WHERE idempotency_key = $1`,
		key,
	).Scan(&out.ID, &out.ShopID, &out.OrderSN, &out.Action, &out.IdempotencyKey, &out.Status, &billID, &out.SMLDocNo, &out.Error, &createdBy, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, "", err
	}
	if billID.Valid {
		out.BillID = &billID.String
	}
	if createdBy.Valid {
		out.CreatedBy = &createdBy.String
	}
	return &out, out.Status, nil
}

func (r *ShopeeRealtimeRepo) CompleteAction(ctx context.Context, idempotencyKey, status, billID, smlDocNo string, response json.RawMessage, errMsg string) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil
	}
	resp := string(response)
	if strings.TrimSpace(resp) == "" {
		resp = "{}"
	}
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE shopee_action_outbox
		    SET status = $2,
		        bill_id = COALESCE(NULLIF($3, '')::uuid, bill_id),
		        sml_doc_no = COALESCE(NULLIF($4, ''), sml_doc_no),
		        response = COALESCE(NULLIF($5, '')::jsonb, response),
		        error = $6,
		        updated_at = NOW()
		  WHERE idempotency_key = $1`,
		strings.TrimSpace(idempotencyKey), strings.TrimSpace(status), strings.TrimSpace(billID),
		strings.TrimSpace(smlDocNo), resp, strings.TrimSpace(errMsg),
	)
	return err
}

func (r *ShopeeRealtimeRepo) RecordAction(ctx context.Context, shopID int64, orderSN, action, userID, status string, request, response json.RawMessage, errMsg string) error {
	orderSN = strings.TrimSpace(orderSN)
	action = strings.TrimSpace(action)
	if shopID <= 0 || orderSN == "" || action == "" {
		return nil
	}
	if strings.TrimSpace(status) == "" {
		status = "done"
	}
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	req := string(request)
	if strings.TrimSpace(req) == "" {
		req = "{}"
	}
	resp := string(response)
	if strings.TrimSpace(resp) == "" {
		resp = "{}"
	}
	key := fmt.Sprintf("%s:%d:%s:%d", action, shopID, orderSN, time.Now().UnixNano())
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO shopee_action_outbox
		  (shop_id, order_sn, action, idempotency_key, status, request, response, error, created_by)
		 VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF($6, '')::jsonb, '{}'::jsonb),
		         COALESCE(NULLIF($7, '')::jsonb, '{}'::jsonb), $8, NULLIF($9, '')::uuid)`,
		shopID, orderSN, action, key, strings.TrimSpace(status), req, resp, strings.TrimSpace(errMsg), strings.TrimSpace(userID),
	)
	return err
}

type ShopeeSMLCancellationInput struct {
	ShopID         int64
	OrderSN        string
	BillID         string
	SaleSMLDocNo   string
	CancelSMLDocNo string
	Status         string
	Error          string
	Response       json.RawMessage
	CreatedBy      string
}

func (r *ShopeeRealtimeRepo) LatestSMLCancellation(ctx context.Context, shopID int64, orderSN, saleSMLDocNo string) (*models.ShopeeSMLCancellation, error) {
	orderSN = strings.TrimSpace(orderSN)
	saleSMLDocNo = strings.TrimSpace(saleSMLDocNo)
	if shopID <= 0 || orderSN == "" {
		return nil, nil
	}
	args := []any{shopID, orderSN}
	whereSale := ""
	if saleSMLDocNo != "" {
		args = append(args, saleSMLDocNo)
		whereSale = fmt.Sprintf(" AND sale_sml_doc_no = $%d", len(args))
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id::text, shop_id, order_sn, bill_id::text, sale_sml_doc_no,
		        cancel_sml_doc_no, status, error, response, created_by::text,
		        created_at, updated_at, completed_at
		   FROM shopee_sml_cancellations
		  WHERE shop_id = $1
		    AND order_sn = $2`+whereSale+`
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	row, err := scanShopeeSMLCancellation(rows)
	if err != nil {
		return nil, err
	}
	return &row, rows.Err()
}

func (r *ShopeeRealtimeRepo) RecordSMLCancellationPreview(ctx context.Context, in ShopeeSMLCancellationInput) (*models.ShopeeSMLCancellation, error) {
	in = normalizeShopeeSMLCancellationInput(in)
	if in.ShopID <= 0 || in.OrderSN == "" || in.SaleSMLDocNo == "" {
		return nil, fmt.Errorf("shop_id, order_sn, and sale_sml_doc_no are required")
	}
	resp := jsonForDB(in.Response)
	var out models.ShopeeSMLCancellation
	var billID, createdBy sql.NullString
	var completedAt sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO shopee_sml_cancellations
		  (shop_id, order_sn, bill_id, sale_sml_doc_no, cancel_sml_doc_no,
		   status, error, response, created_by)
		 VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5,
		         'previewed', $6, COALESCE(NULLIF($7, '')::jsonb, '{}'::jsonb),
		         NULLIF($8, '')::uuid)
		 RETURNING id::text, shop_id, order_sn, bill_id::text, sale_sml_doc_no,
		           cancel_sml_doc_no, status, error, response, created_by::text,
		           created_at, updated_at, completed_at`,
		in.ShopID, in.OrderSN, in.BillID, in.SaleSMLDocNo, in.CancelSMLDocNo,
		truncateDBText(in.Error, 800), resp, in.CreatedBy,
	).Scan(
		&out.ID, &out.ShopID, &out.OrderSN, &billID, &out.SaleSMLDocNo,
		&out.CancelSMLDocNo, &out.Status, &out.Error, &out.Response, &createdBy,
		&out.CreatedAt, &out.UpdatedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	attachShopeeSMLCancellationNulls(&out, billID, createdBy, completedAt)
	return &out, nil
}

func (r *ShopeeRealtimeRepo) StartSMLCancellationCreate(ctx context.Context, in ShopeeSMLCancellationInput) (*models.ShopeeSMLCancellation, string, error) {
	in = normalizeShopeeSMLCancellationInput(in)
	if in.ShopID <= 0 || in.OrderSN == "" || in.SaleSMLDocNo == "" {
		return nil, "", fmt.Errorf("shop_id, order_sn, and sale_sml_doc_no are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	lockKey := fmt.Sprintf("shopee_sml_cancel:%d:%s:%s", in.ShopID, in.OrderSN, in.SaleSMLDocNo)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return nil, "", err
	}
	if existing, err := latestSMLCancellationTx(ctx, tx, in.ShopID, in.OrderSN, in.SaleSMLDocNo, "created", "already_exists"); err != nil {
		return nil, "", err
	} else if existing != nil {
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		rollback = false
		return existing, "done", nil
	}
	if running, err := latestRunningSMLCancellationTx(ctx, tx, in.ShopID, in.OrderSN, in.SaleSMLDocNo); err != nil {
		return nil, "", err
	} else if running != nil {
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		rollback = false
		return running, "running", nil
	}

	resp := jsonForDB(in.Response)
	var out models.ShopeeSMLCancellation
	var billID, createdBy sql.NullString
	var completedAt sql.NullTime
	err = tx.QueryRowContext(ctx,
		`INSERT INTO shopee_sml_cancellations
		  (shop_id, order_sn, bill_id, sale_sml_doc_no, cancel_sml_doc_no,
		   status, error, response, created_by)
		 VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5,
		         'creating', '', COALESCE(NULLIF($6, '')::jsonb, '{}'::jsonb),
		         NULLIF($7, '')::uuid)
		 RETURNING id::text, shop_id, order_sn, bill_id::text, sale_sml_doc_no,
		           cancel_sml_doc_no, status, error, response, created_by::text,
		           created_at, updated_at, completed_at`,
		in.ShopID, in.OrderSN, in.BillID, in.SaleSMLDocNo, in.CancelSMLDocNo,
		resp, in.CreatedBy,
	).Scan(
		&out.ID, &out.ShopID, &out.OrderSN, &billID, &out.SaleSMLDocNo,
		&out.CancelSMLDocNo, &out.Status, &out.Error, &out.Response, &createdBy,
		&out.CreatedAt, &out.UpdatedAt, &completedAt,
	)
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	rollback = false
	attachShopeeSMLCancellationNulls(&out, billID, createdBy, completedAt)
	return &out, "started", nil
}

func (r *ShopeeRealtimeRepo) CompleteSMLCancellation(ctx context.Context, id, status, cancelSMLDocNo string, response json.RawMessage, errMsg string) (*models.ShopeeSMLCancellation, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	resp := jsonForDB(response)
	var out models.ShopeeSMLCancellation
	var billID, createdBy sql.NullString
	var completedAt sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`UPDATE shopee_sml_cancellations
		    SET status = $2,
		        cancel_sml_doc_no = COALESCE(NULLIF($3, ''), cancel_sml_doc_no),
		        response = COALESCE(NULLIF($4, '')::jsonb, response),
		        error = $5,
		        updated_at = NOW(),
		        completed_at = CASE WHEN $2 IN ('created','already_exists','failed','blocked') THEN NOW() ELSE completed_at END
		  WHERE id = $1::uuid
		  RETURNING id::text, shop_id, order_sn, bill_id::text, sale_sml_doc_no,
		            cancel_sml_doc_no, status, error, response, created_by::text,
		            created_at, updated_at, completed_at`,
		id, strings.TrimSpace(status), strings.TrimSpace(cancelSMLDocNo), resp, truncateDBText(errMsg, 800),
	).Scan(
		&out.ID, &out.ShopID, &out.OrderSN, &billID, &out.SaleSMLDocNo,
		&out.CancelSMLDocNo, &out.Status, &out.Error, &out.Response, &createdBy,
		&out.CreatedAt, &out.UpdatedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	attachShopeeSMLCancellationNulls(&out, billID, createdBy, completedAt)
	return &out, nil
}

func (r *ShopeeRealtimeRepo) MergeSnapshotShippingMetadata(ctx context.Context, shopID int64, orderSN string, tracking *shopeeapi.TrackingNumberResponse, info *shopeeapi.TrackingInfoResponse) (*models.ShopeeOrderSnapshot, error) {
	orderSN = strings.TrimSpace(orderSN)
	if shopID <= 0 || orderSN == "" {
		return nil, fmt.Errorf("shop_id and order_sn are required")
	}
	patch := map[string]interface{}{}
	trackingNumber := ""
	logisticsStatus := ""
	if tracking != nil {
		patch["shipping_tracking_number_response"] = tracking.Response
		trackingNumber = strings.TrimSpace(tracking.Response.TrackingNumber)
	}
	if info != nil {
		patch["shipping_tracking_info_response"] = info.Response
		logisticsStatus = strings.TrimSpace(info.Response.LogisticsStatus)
	}
	if len(patch) == 0 {
		return r.FindSnapshot(ctx, shopID, orderSN)
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE shopee_order_snapshots
		    SET tracking_number = COALESCE(NULLIF($3, ''), tracking_number),
		        logistics_status = COALESCE(NULLIF($4, ''), logistics_status),
		        raw_detail = COALESCE(raw_detail, '{}'::jsonb) || COALESCE(NULLIF($5, '')::jsonb, '{}'::jsonb),
		        last_update_source = 'shipping',
		        last_synced_at = NOW(),
		        updated_at = NOW()
		  WHERE shop_id = $1
		    AND order_sn = $2`,
		shopID, orderSN, trackingNumber, logisticsStatus, string(raw),
	)
	if err != nil {
		return nil, err
	}
	return r.FindSnapshot(ctx, shopID, orderSN)
}

func (r *ShopeeRealtimeRepo) RecentPushEvents(ctx context.Context, limit int) ([]models.ShopeeRealtimeDiagnosticEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT e.id::text,
		        e.shop_id,
		        COALESCE(c.shop_name, ''),
		        e.order_sn,
		        e.push_code,
		        e.push_name,
		        CASE
		          WHEN e.push_code IN (1, 2, 12) THEN 'shop_auth'
		          WHEN e.push_code = 0 OR e.push_name IN ('verification_or_unknown', 'unknown') OR e.order_sn = '' THEN 'console_verify'
		          ELSE 'shopee_push'
		        END AS source,
		        e.event_status,
		        e.processing_status,
		        COALESCE(j.status, CASE WHEN e.order_sn = '' THEN 'not_applicable' ELSE e.processing_status END) AS reconcile_status,
		        COALESCE(NULLIF(j.last_error, ''), e.error) AS reconcile_error,
		        e.error,
		        (e.push_code = 0 OR e.push_name IN ('verification_or_unknown', 'unknown') OR (e.order_sn = '' AND e.push_code NOT IN (1, 2, 12))) AS is_verification_event,
		        e.received_at,
		        e.processed_at
		   FROM shopee_push_events e
		   LEFT JOIN shopee_api_connections c ON c.shop_id = e.shop_id
		   LEFT JOIN LATERAL (
		     SELECT status, last_error
		       FROM shopee_reconcile_jobs j
		      WHERE j.shop_id = e.shop_id
		        AND j.order_sn = e.order_sn
		        AND j.reason = ('push:' || e.push_code::text)
		      ORDER BY j.created_at DESC
		      LIMIT 1
		   ) j ON true
		  ORDER BY e.received_at DESC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ShopeeRealtimeDiagnosticEvent{}
	for rows.Next() {
		var row models.ShopeeRealtimeDiagnosticEvent
		var processed sql.NullTime
		if err := rows.Scan(
			&row.ID, &row.ShopID, &row.ShopLabel, &row.OrderSN, &row.PushCode, &row.PushName,
			&row.Source, &row.EventStatus, &row.ProcessingStatus, &row.ReconcileStatus,
			&row.ReconcileError, &row.Error, &row.IsVerificationEvent, &row.ReceivedAt, &processed,
		); err != nil {
			return nil, err
		}
		if processed.Valid {
			row.ProcessedAt = &processed.Time
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *ShopeeRealtimeRepo) OrderTimeline(ctx context.Context, shopID int64, orderSN string) ([]models.ShopeeOrderTimelineEvent, error) {
	orderSN = strings.TrimSpace(orderSN)
	if shopID <= 0 || orderSN == "" {
		return nil, fmt.Errorf("shop_id and order_sn are required")
	}
	snap, err := r.FindSnapshot(ctx, shopID, orderSN)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT source, kind, title, detail, status, created_at
		   FROM (
		     SELECT 'Shopee' AS source,
		            'snapshot' AS kind,
		            'สถานะล่าสุดจาก Shopee' AS title,
		            COALESCE(CONCAT_WS(' · ', NULLIF(order_status, ''), NULLIF(logistics_status, ''), NULLIF(tracking_number, '')), '') AS detail,
		            COALESCE(order_status, '') AS status,
		            last_synced_at AS created_at
		       FROM shopee_order_snapshots
		      WHERE shop_id = $1 AND order_sn = $2
		     UNION ALL
		     SELECT CASE
		              WHEN push_code IN (1, 2, 12) THEN 'Shopee'
		              WHEN push_code = 0 OR push_name IN ('verification_or_unknown', 'unknown') OR order_sn = '' THEN 'Shopee Console'
		              ELSE 'Push'
		            END AS source,
		            'push' AS kind,
		            COALESCE(NULLIF(push_name, ''), 'Shopee push') AS title,
		            COALESCE(NULLIF(event_status, ''), '') AS detail,
		            COALESCE(processing_status, '') AS status,
		            received_at AS created_at
		       FROM shopee_push_events
		      WHERE shop_id = $1 AND order_sn = $2
		     UNION ALL
		     SELECT 'Nexflow' AS source,
		            'reconcile' AS kind,
		            'ตรวจข้อมูลจาก Shopee' AS title,
		            COALESCE(CONCAT_WS(' · ', NULLIF(reason, ''), NULLIF(last_error, '')), '') AS detail,
		            COALESCE(status, '') AS status,
		            updated_at AS created_at
		       FROM shopee_reconcile_jobs
		      WHERE shop_id = $1 AND order_sn = $2
		     UNION ALL
		     SELECT 'Nexflow' AS source,
		            action AS kind,
		            action AS title,
		            COALESCE(NULLIF(error, ''), '') AS detail,
		            COALESCE(status, '') AS status,
		            updated_at AS created_at
		       FROM shopee_action_outbox
		      WHERE shop_id = $1 AND order_sn = $2
		     UNION ALL
		     SELECT 'Nexflow' AS source,
		            'cancel_sml_document' AS kind,
		            'cancel_sml_document' AS title,
		            COALESCE(CONCAT_WS(' · ', NULLIF(sale_sml_doc_no, ''), NULLIF(cancel_sml_doc_no, ''), NULLIF(error, '')), '') AS detail,
		            COALESCE(status, '') AS status,
		            updated_at AS created_at
		       FROM shopee_sml_cancellations
		      WHERE shop_id = $1 AND order_sn = $2
		   ) x
		  WHERE created_at IS NOT NULL
		  ORDER BY created_at DESC
		  LIMIT 80`,
		shopID, orderSN,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ShopeeOrderTimelineEvent{}
	idx := 0
	for rows.Next() {
		var ev models.ShopeeOrderTimelineEvent
		if err := rows.Scan(&ev.Source, &ev.Kind, &ev.Title, &ev.Detail, &ev.Status, &ev.CreatedAt); err != nil {
			return nil, err
		}
		ev.ID = fmt.Sprintf("%s:%d", ev.Kind, idx)
		ev.Title = shopeeTimelineTitle(ev.Kind, ev.Title, ev.Status)
		ev.Source = shopeeTimelineSource(ev.Source, ev.Kind)
		out = append(out, ev)
		idx++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, tracking := range snap.ShippingTracking {
		if tracking.UpdateTime <= 0 {
			continue
		}
		out = append(out, models.ShopeeOrderTimelineEvent{
			ID:        fmt.Sprintf("tracking:%d", i),
			Source:    "Seller Center",
			Kind:      "tracking",
			Title:     strings.TrimSpace(firstNonEmpty(tracking.Description, tracking.LogisticsStatus, "Shopee logistics update")),
			Detail:    strings.TrimSpace(tracking.LogisticsStatus),
			Status:    strings.TrimSpace(tracking.LogisticsStatus),
			CreatedAt: time.Unix(tracking.UpdateTime, 0),
		})
	}
	sortShopeeTimelineEvents(out)
	return out, nil
}

type shopeeStatusEvidence struct {
	Status     string
	Source     string
	Confidence string
	OccurredAt *time.Time
}

func (r *ShopeeRealtimeRepo) OrderLifecycleTimeline(ctx context.Context, snap *models.ShopeeOrderSnapshot) ([]models.ShopeeOrderStatusTimelineStep, []models.ShopeeOrderERPMilestone, error) {
	if snap == nil || snap.ShopID <= 0 || strings.TrimSpace(snap.OrderSN) == "" {
		return nil, nil, fmt.Errorf("snapshot is required")
	}
	statusEvidence, err := r.orderStatusEvidence(ctx, snap.ShopID, snap.OrderSN)
	if err != nil {
		return nil, nil, err
	}
	statusTimeline := buildShopeeStatusTimeline(snap, statusEvidence)
	milestones, err := r.orderERPMilestones(ctx, snap)
	if err != nil {
		return nil, nil, err
	}
	return statusTimeline, milestones, nil
}

func (r *ShopeeRealtimeRepo) orderStatusEvidence(ctx context.Context, shopID int64, orderSN string) (map[string]shopeeStatusEvidence, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_status,
		        event_update_time,
		        event_timestamp,
		        received_at
		   FROM shopee_push_events
		  WHERE shop_id = $1
		    AND order_sn = $2
		    AND COALESCE(event_status, '') <> ''
		  ORDER BY COALESCE(event_update_time, event_timestamp, received_at) ASC, received_at ASC
		  LIMIT 80`,
		shopID, strings.TrimSpace(orderSN),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]shopeeStatusEvidence{}
	for rows.Next() {
		var rawStatus string
		var updateAt, eventAt sql.NullTime
		var receivedAt time.Time
		if err := rows.Scan(&rawStatus, &updateAt, &eventAt, &receivedAt); err != nil {
			return nil, err
		}
		status := models.NormalizeShopeeOrderStatus(rawStatus)
		group := shopeeLifecycleGroup(status)
		if group == "" {
			continue
		}
		when := receivedAt
		confidence := "inferred"
		if eventAt.Valid {
			when = eventAt.Time
			confidence = "confirmed"
		}
		if updateAt.Valid {
			when = updateAt.Time
			confidence = "confirmed"
		}
		if existing, ok := out[group]; ok && existing.OccurredAt != nil && !when.Before(*existing.OccurredAt) {
			continue
		}
		t := when
		out[group] = shopeeStatusEvidence{
			Status:     status,
			Source:     "push",
			Confidence: confidence,
			OccurredAt: &t,
		}
	}
	return out, rows.Err()
}

func (r *ShopeeRealtimeRepo) orderERPMilestones(ctx context.Context, snap *models.ShopeeOrderSnapshot) ([]models.ShopeeOrderERPMilestone, error) {
	var createdAt, sentAt sql.NullTime
	var billStatus, billDocNo string
	if snap.BillID != nil && strings.TrimSpace(*snap.BillID) != "" {
		err := r.db.QueryRowContext(ctx,
			`SELECT created_at, sent_at, COALESCE(status, ''), COALESCE(sml_doc_no, '')
			   FROM bills
			  WHERE id = $1::uuid`,
			strings.TrimSpace(*snap.BillID),
		).Scan(&createdAt, &sentAt, &billStatus, &billDocNo)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}

	var actionCreatedAt sql.NullTime
	_ = r.db.QueryRowContext(ctx,
		`SELECT updated_at
		   FROM shopee_action_outbox
		  WHERE shop_id = $1
		    AND order_sn = $2
		    AND action = 'create_document'
		    AND status = 'done'
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		snap.ShopID, strings.TrimSpace(snap.OrderSN),
	).Scan(&actionCreatedAt)

	documentState := "upcoming"
	documentDetail := "ยังไม่สร้างเอกสารใน Nexflow"
	documentConfidence := "missing"
	var documentAt *time.Time
	if snap.BillID != nil && strings.TrimSpace(*snap.BillID) != "" {
		documentState = "done"
		documentDetail = "สร้างเอกสารใน Nexflow แล้ว"
		documentConfidence = "confirmed"
		if actionCreatedAt.Valid {
			t := actionCreatedAt.Time
			documentAt = &t
		} else if createdAt.Valid {
			t := createdAt.Time
			documentAt = &t
		}
	}

	smlDocNo := strings.TrimSpace(firstNonEmpty(snap.SMLDocNo, billDocNo))
	smlState := "upcoming"
	smlDetail := "ยังไม่ส่ง SML"
	smlConfidence := "missing"
	var smlAt *time.Time
	if strings.EqualFold(snap.ERPStatus, "failed") || strings.EqualFold(billStatus, "failed") {
		smlState = "failed"
		smlDetail = "ส่ง SML ไม่สำเร็จ"
		smlConfidence = "confirmed"
	} else if smlDocNo != "" || strings.EqualFold(snap.ERPStatus, "sent") || strings.EqualFold(billStatus, "sent") {
		smlState = "done"
		smlDetail = strings.TrimSpace("ส่ง SML แล้ว " + smlDocNo)
		smlConfidence = "confirmed"
		if sentAt.Valid {
			t := sentAt.Time
			smlAt = &t
		}
	}

	cancelState := "upcoming"
	cancelDetail := "ยังไม่ต้องสร้างเอกสารยกเลิก SML"
	cancelConfidence := "missing"
	var cancelAt *time.Time
	cancelRow, err := r.LatestSMLCancellation(ctx, snap.ShopID, snap.OrderSN, smlDocNo)
	if err != nil {
		return nil, err
	}
	if cancelRow != nil {
		cancelConfidence = "confirmed"
		switch cancelRow.Status {
		case "created", "already_exists":
			cancelState = "done"
			cancelDetail = strings.TrimSpace("สร้างเอกสารยกเลิก SML แล้ว " + cancelRow.CancelSMLDocNo)
			if cancelRow.CompletedAt != nil {
				t := *cancelRow.CompletedAt
				cancelAt = &t
			} else {
				t := cancelRow.UpdatedAt
				cancelAt = &t
			}
		case "failed", "blocked":
			cancelState = "failed"
			cancelDetail = firstNonEmpty(cancelRow.Error, "สร้างเอกสารยกเลิก SML ไม่สำเร็จ")
			t := cancelRow.UpdatedAt
			cancelAt = &t
		case "creating":
			cancelState = "current"
			cancelDetail = "กำลังสร้างเอกสารยกเลิก SML"
			t := cancelRow.UpdatedAt
			cancelAt = &t
		default:
			cancelState = "current"
			cancelDetail = "เปิด preview เอกสารยกเลิก SML แล้ว"
			t := cancelRow.UpdatedAt
			cancelAt = &t
		}
	} else if shopeeCancelledAfterSML(snap, smlDocNo) {
		cancelState = "current"
		cancelDetail = "ต้องสร้างเอกสารยกเลิก SML"
		cancelConfidence = "confirmed"
	}

	return []models.ShopeeOrderERPMilestone{
		{
			Key:        "document",
			Label:      "สร้างเอกสาร",
			Detail:     documentDetail,
			State:      documentState,
			Source:     "nexflow",
			Confidence: documentConfidence,
			OccurredAt: documentAt,
		},
		{
			Key:        "sml",
			Label:      "ส่ง SML",
			Detail:     smlDetail,
			State:      smlState,
			Source:     "nexflow",
			Confidence: smlConfidence,
			OccurredAt: smlAt,
		},
		{
			Key:        "sml_cancel",
			Label:      "เอกสารยกเลิก SML",
			Detail:     cancelDetail,
			State:      cancelState,
			Source:     "nexflow",
			Confidence: cancelConfidence,
			OccurredAt: cancelAt,
		},
	}, nil
}

func buildShopeeStatusTimeline(snap *models.ShopeeOrderSnapshot, evidence map[string]shopeeStatusEvidence) []models.ShopeeOrderStatusTimelineStep {
	currentGroup := shopeeLifecycleGroup(models.NormalizeShopeeOrderStatus(snap.OrderStatus))
	if evidence == nil {
		evidence = map[string]shopeeStatusEvidence{}
	}
	if currentGroup != "" {
		if existing := evidence[currentGroup]; existing.OccurredAt == nil {
			source := normalizeLifecycleSource(snap.LastUpdateSource)
			confidence := "inferred"
			var occurred *time.Time
			if snap.LastOrderUpdateAt != nil {
				t := *snap.LastOrderUpdateAt
				occurred = &t
			} else if !snap.LastSyncedAt.IsZero() {
				t := snap.LastSyncedAt
				occurred = &t
			} else {
				confidence = "missing"
			}
			evidence[currentGroup] = shopeeStatusEvidence{
				Status:     models.NormalizeShopeeOrderStatus(snap.OrderStatus),
				Source:     source,
				Confidence: confidence,
				OccurredAt: occurred,
			}
		}
	}

	normalOrder := []string{"unpaid", "to_ship", "shipping", "completed"}
	index := map[string]int{"unpaid": 0, "to_ship": 1, "shipping": 2, "completed": 3}
	steps := make([]models.ShopeeOrderStatusTimelineStep, 0, 5)
	currentIndex, hasCurrentIndex := index[currentGroup]
	cancelled := currentGroup == "cancelled"
	for _, key := range normalOrder {
		ev := evidence[key]
		state := "upcoming"
		if cancelled {
			if ev.OccurredAt != nil {
				state = "done"
			} else {
				state = "skipped"
			}
		} else if hasCurrentIndex {
			if index[key] < currentIndex {
				state = "done"
			} else if index[key] == currentIndex {
				state = "current"
			}
		}
		steps = append(steps, shopeeTimelineStepFromEvidence(key, ev, state, key == currentGroup))
	}
	cancelEv := evidence["cancelled"]
	cancelState := "upcoming"
	if cancelled {
		cancelState = "current"
	}
	step := shopeeTimelineStepFromEvidence("cancelled", cancelEv, cancelState, cancelled)
	step.Terminal = true
	steps = append(steps, step)
	return steps
}

func shopeeTimelineStepFromEvidence(key string, ev shopeeStatusEvidence, state string, current bool) models.ShopeeOrderStatusTimelineStep {
	label, status := shopeeLifecycleLabel(key)
	if ev.Status != "" {
		status = ev.Status
	}
	confidence := strings.TrimSpace(ev.Confidence)
	if confidence == "" {
		confidence = "missing"
	}
	source := strings.TrimSpace(ev.Source)
	if source == "" {
		source = "snapshot"
	}
	detail := shopeeLifecycleDetail(key, state, confidence, source)
	return models.ShopeeOrderStatusTimelineStep{
		Key:        key,
		Status:     status,
		Label:      label,
		Detail:     detail,
		State:      state,
		Source:     source,
		Confidence: confidence,
		OccurredAt: ev.OccurredAt,
		Current:    current,
	}
}

func shopeeLifecycleGroup(status string) string {
	switch models.NormalizeShopeeOrderStatus(status) {
	case "UNPAID":
		return "unpaid"
	case "READY_TO_SHIP":
		return "to_ship"
	case "PROCESSED", "SHIPPED":
		return "shipping"
	case "COMPLETED":
		return "completed"
	case "CANCELLED", "IN_CANCEL":
		return "cancelled"
	default:
		return ""
	}
}

func shopeeCancelledAfterSML(snap *models.ShopeeOrderSnapshot, smlDocNo string) bool {
	if snap == nil {
		return false
	}
	switch models.NormalizeShopeeOrderStatus(snap.OrderStatus) {
	case "CANCELLED", "IN_CANCEL":
	default:
		return false
	}
	return strings.TrimSpace(firstNonEmpty(smlDocNo, snap.SMLDocNo)) != ""
}

func shopeeLifecycleLabel(key string) (string, string) {
	switch key {
	case "unpaid":
		return "ยังไม่ชำระ", "UNPAID"
	case "to_ship":
		return "ที่ต้องจัดส่ง", "READY_TO_SHIP"
	case "shipping":
		return "กำลังจัดส่ง", "SHIPPED"
	case "completed":
		return "สำเร็จ", "COMPLETED"
	case "cancelled":
		return "ยกเลิก", "CANCELLED"
	default:
		return key, strings.ToUpper(key)
	}
}

func shopeeLifecycleDetail(key, state, confidence, source string) string {
	if state == "upcoming" {
		if key == "cancelled" {
			return "ไม่มีสถานะยกเลิก"
		}
		return "ยังไม่ถึงสถานะนี้"
	}
	if state == "skipped" {
		return "ไม่มีเวลาจาก Shopee สำหรับสถานะนี้"
	}
	if confidence == "confirmed" && source == "push" {
		return "ยืนยันจาก Shopee Push"
	}
	if source == "sync" {
		return "อัปเดตจาก Shopee Sync"
	}
	if source == "push" {
		return "รับข้อมูลจาก Push แต่ไม่มีเวลาอัปเดตจาก Shopee"
	}
	if source == "shipping" {
		return "อัปเดตจากการตรวจสถานะจัดส่ง"
	}
	if confidence == "missing" {
		return "ยังไม่มีเวลาจาก Shopee"
	}
	return "อัปเดตจาก snapshot ล่าสุด"
}

func normalizeLifecycleSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "push":
		return "push"
	case "sync":
		return "sync"
	case "shipping":
		return "shipping"
	default:
		return "snapshot"
	}
}

func (r *ShopeeRealtimeRepo) MarkConnectionSync(ctx context.Context, shopID int64, status, msg string) {
	if shopID <= 0 {
		return
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, _ = r.db.ExecContext(ctx,
		`UPDATE shopee_api_connections
		    SET last_sync_at = NOW(),
		        last_sync_status = $2,
		        last_sync_error = $3,
		        updated_at = NOW()
		  WHERE shop_id = $1`,
		shopID, status, msg,
	)
}

func (r *ShopeeRealtimeRepo) findShopeeBill(ctx context.Context, shopID int64, orderSN string) (id, status, smlDocNo string, err error) {
	shop := fmt.Sprint(shopID)
	err = r.db.QueryRowContext(ctx,
		`SELECT id::text, status, COALESCE(sml_doc_no, '')
		   FROM bills
		  WHERE source = 'shopee'
		    AND archived_at IS NULL
		    AND (raw_data->>'order_id' = $1 OR raw_data->>'shopee_order_id' = $1 OR sml_order_id = $1)
		    AND (
		      raw_data->>'shopee_shop_id' = $2
		      OR COALESCE(raw_data->>'shopee_shop_id', '') = ''
		    )
		  ORDER BY created_at DESC
		  LIMIT 1`,
		strings.TrimSpace(orderSN), shop,
	).Scan(&id, &status, &smlDocNo)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return id, status, smlDocNo, err
}

func deriveShopeeERPStatus(orderStatus, billStatus, smlDocNo string) string {
	status := strings.ToUpper(strings.TrimSpace(orderStatus))
	switch status {
	case "CANCELLED", "IN_CANCEL":
		return "cancelled"
	case "UNPAID":
		return "blocked"
	}
	switch strings.TrimSpace(billStatus) {
	case "sent":
		return "sent"
	case "failed":
		return "failed"
	case "needs_review":
		return "needs_review"
	case "pending", "confirmed":
		if strings.TrimSpace(smlDocNo) != "" {
			return "pending_erp"
		}
		return "pending_erp"
	default:
		return "pending"
	}
}

func firstPackageFields(packages []shopeeapi.OrderPackage) (number, logistics, tracking, carrier string) {
	for _, p := range packages {
		if number == "" {
			number = strings.TrimSpace(p.PackageNumber)
		}
		if logistics == "" {
			logistics = strings.TrimSpace(p.LogisticsStatus)
		}
		if tracking == "" {
			tracking = strings.TrimSpace(p.TrackingNumber)
		}
		if carrier == "" {
			carrier = strings.TrimSpace(p.ShippingCarrier)
		}
		if number != "" || logistics != "" || tracking != "" || carrier != "" {
			return
		}
	}
	return
}

func shopeeSnapshotWhere(f models.ShopeeOrderSnapshotFilter) (string, []interface{}) {
	where := []string{}
	args := []interface{}{}
	if f.ShopID > 0 {
		args = append(args, f.ShopID)
		where = append(where, fmt.Sprintf("s.shop_id = $%d", len(args)))
	}
	if strings.TrimSpace(f.Status) != "" && strings.TrimSpace(f.Status) != "all" {
		args = append(args, models.NormalizeShopeeOrderStatus(f.Status))
		where = append(where, fmt.Sprintf("s.order_status = $%d", len(args)))
	}
	if groupWhere := shopeeSnapshotStatusGroupWhere(f.StatusGroup); groupWhere != "" {
		where = append(where, groupWhere)
	}
	if strings.TrimSpace(f.ERPStatus) != "" && strings.TrimSpace(f.ERPStatus) != "all" {
		args = append(args, strings.TrimSpace(f.ERPStatus))
		where = append(where, fmt.Sprintf("s.erp_status = $%d", len(args)))
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		where = append(where, fmt.Sprintf(`(
			LOWER(s.order_sn) LIKE $%d OR LOWER(s.buyer_username) LIKE $%d OR
			LOWER(s.tracking_number) LIKE $%d OR LOWER(s.sml_doc_no) LIKE $%d
		)`, len(args), len(args), len(args), len(args)))
	}
	if len(where) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

func shopeeSnapshotStatusGroupWhere(group string) string {
	switch strings.ToLower(strings.TrimSpace(group)) {
	case "", "all":
		return ""
	case "unpaid":
		return "s.order_status = 'UNPAID'"
	case "to_ship":
		return "s.order_status = 'READY_TO_SHIP'"
	case "shipping":
		return "s.order_status IN ('PROCESSED','SHIPPED')"
	case "completed":
		return "s.order_status = 'COMPLETED'"
	case "cancelled":
		return "s.order_status IN ('CANCELLED','IN_CANCEL')"
	default:
		return ""
	}
}

func shopeeTimelineTitle(kind, title, status string) string {
	switch strings.TrimSpace(kind) {
	case "create_document":
		switch strings.TrimSpace(status) {
		case "done":
			return "สร้างเอกสารใน Nexflow แล้ว"
		case "blocked":
			return "สร้างเอกสารถูกบล็อก"
		case "failed":
			return "สร้างเอกสารไม่สำเร็จ"
		default:
			return "สร้างเอกสาร"
		}
	case "reconcile_shipping":
		return "ตรวจสถานะจัดส่งจาก Shopee"
	case "reconcile":
		return "ตรวจข้อมูลจาก Shopee"
	case "ship_order":
		return "คำสั่งจัดส่ง Shopee"
	case "shipping_document_create", "shipping_document_result", "shipping_document_download":
		return "ตรวจใบปะหน้าพัสดุ"
	case "cancel_sml_document":
		switch strings.TrimSpace(status) {
		case "created", "already_exists", "done":
			return "สร้างเอกสารยกเลิก SML แล้ว"
		case "failed":
			return "สร้างเอกสารยกเลิก SML ไม่สำเร็จ"
		case "blocked":
			return "สร้างเอกสารยกเลิก SML ถูกบล็อก"
		case "previewed":
			return "เปิด preview เอกสารยกเลิก SML"
		default:
			return "เอกสารยกเลิก SML"
		}
	default:
		if strings.TrimSpace(title) != "" {
			return strings.TrimSpace(title)
		}
		return "Shopee update"
	}
}

func shopeeTimelineSource(source, kind string) string {
	switch strings.TrimSpace(kind) {
	case "tracking":
		return "Seller Center"
	case "push":
		return "Push"
	case "snapshot":
		return "Sync"
	case "reconcile", "reconcile_shipping", "create_document", "ship_order", "shipping_document_create", "shipping_document_result", "shipping_document_download", "cancel_sml_document":
		return "Nexflow"
	default:
		if strings.TrimSpace(source) != "" {
			return strings.TrimSpace(source)
		}
		return "Nexflow"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sortShopeeTimelineEvents(events []models.ShopeeOrderTimelineEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
}

type txQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type snapshotScanner interface {
	Scan(dest ...interface{}) error
}

func latestSMLCancellationTx(ctx context.Context, tx txQueryer, shopID int64, orderSN, saleSMLDocNo string, statuses ...string) (*models.ShopeeSMLCancellation, error) {
	orderSN = strings.TrimSpace(orderSN)
	saleSMLDocNo = strings.TrimSpace(saleSMLDocNo)
	if shopID <= 0 || orderSN == "" || saleSMLDocNo == "" || len(statuses) == 0 {
		return nil, nil
	}
	args := []any{shopID, orderSN, saleSMLDocNo}
	placeholders := make([]string, 0, len(statuses))
	for _, status := range statuses {
		args = append(args, strings.TrimSpace(status))
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id::text, shop_id, order_sn, bill_id::text, sale_sml_doc_no,
		        cancel_sml_doc_no, status, error, response, created_by::text,
		        created_at, updated_at, completed_at
		   FROM shopee_sml_cancellations
		  WHERE shop_id = $1
		    AND order_sn = $2
		    AND sale_sml_doc_no = $3
		    AND status IN (`+strings.Join(placeholders, ",")+`)
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	row, err := scanShopeeSMLCancellation(rows)
	if err != nil {
		return nil, err
	}
	return &row, rows.Err()
}

func latestRunningSMLCancellationTx(ctx context.Context, tx txQueryer, shopID int64, orderSN, saleSMLDocNo string) (*models.ShopeeSMLCancellation, error) {
	orderSN = strings.TrimSpace(orderSN)
	saleSMLDocNo = strings.TrimSpace(saleSMLDocNo)
	if shopID <= 0 || orderSN == "" || saleSMLDocNo == "" {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id::text, shop_id, order_sn, bill_id::text, sale_sml_doc_no,
		        cancel_sml_doc_no, status, error, response, created_by::text,
		        created_at, updated_at, completed_at
		   FROM shopee_sml_cancellations
		  WHERE shop_id = $1
		    AND order_sn = $2
		    AND sale_sml_doc_no = $3
		    AND status = 'creating'
		    AND updated_at > NOW() - INTERVAL '5 minutes'
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		shopID, orderSN, saleSMLDocNo,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	row, err := scanShopeeSMLCancellation(rows)
	if err != nil {
		return nil, err
	}
	return &row, rows.Err()
}

func scanShopeeSMLCancellation(rows snapshotScanner) (models.ShopeeSMLCancellation, error) {
	var out models.ShopeeSMLCancellation
	var billID, createdBy sql.NullString
	var completedAt sql.NullTime
	if err := rows.Scan(
		&out.ID, &out.ShopID, &out.OrderSN, &billID, &out.SaleSMLDocNo,
		&out.CancelSMLDocNo, &out.Status, &out.Error, &out.Response, &createdBy,
		&out.CreatedAt, &out.UpdatedAt, &completedAt,
	); err != nil {
		return out, err
	}
	attachShopeeSMLCancellationNulls(&out, billID, createdBy, completedAt)
	return out, nil
}

func attachShopeeSMLCancellationNulls(out *models.ShopeeSMLCancellation, billID, createdBy sql.NullString, completedAt sql.NullTime) {
	if out == nil {
		return
	}
	if billID.Valid {
		out.BillID = &billID.String
	}
	if createdBy.Valid {
		out.CreatedBy = &createdBy.String
	}
	if completedAt.Valid {
		out.CompletedAt = &completedAt.Time
	}
}

func normalizeShopeeSMLCancellationInput(in ShopeeSMLCancellationInput) ShopeeSMLCancellationInput {
	in.OrderSN = strings.TrimSpace(in.OrderSN)
	in.BillID = strings.TrimSpace(in.BillID)
	in.SaleSMLDocNo = strings.TrimSpace(in.SaleSMLDocNo)
	in.CancelSMLDocNo = strings.TrimSpace(in.CancelSMLDocNo)
	in.Status = strings.TrimSpace(in.Status)
	in.Error = strings.TrimSpace(in.Error)
	in.CreatedBy = strings.TrimSpace(in.CreatedBy)
	return in
}

func jsonForDB(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return "{}"
	}
	return string(raw)
}

func truncateDBText(v string, limit int) string {
	v = strings.TrimSpace(v)
	if limit > 0 && len(v) > limit {
		return v[:limit]
	}
	return v
}

func scanShopeeSnapshot(rows snapshotScanner) (models.ShopeeOrderSnapshot, error) {
	var out models.ShopeeOrderSnapshot
	var connID, billID sql.NullString
	var lastOrderUpdate sql.NullTime
	if err := rows.Scan(
		&out.ID, &connID, &out.ShopID, &out.ShopLabel, &out.OrderSN, &out.OrderStatus,
		&out.ERPStatus, &billID, &out.SMLDocNo, &out.SMLCancelDocNo, &out.SMLCancelStatus, &out.SMLCancelError,
		&out.DocumentRoute, &out.BillSourceFlow, &out.BuyerUsername, &out.TotalAmount,
		&out.Currency, &out.ItemCount, &out.PackageNumber, &out.LogisticsStatus,
		&out.TrackingNumber, &out.ShippingCarrier, &out.PaymentMethod, &out.PaymentBreakdownStatus, &out.RawDetail,
		&lastOrderUpdate, &out.LastUpdateSource, &out.LastSyncedAt, &out.LastError, &out.CreatedAt, &out.UpdatedAt,
		&out.ShipActionStatus,
	); err != nil {
		return out, err
	}
	if connID.Valid {
		out.ConnectionID = &connID.String
	}
	if billID.Valid {
		out.BillID = &billID.String
	}
	if lastOrderUpdate.Valid {
		out.LastOrderUpdateAt = &lastOrderUpdate.Time
	}
	decorateShopeeSnapshotShippingMetadata(&out)
	return out, nil
}

func decorateShopeeSnapshotShippingMetadata(out *models.ShopeeOrderSnapshot) {
	if out == nil || len(out.RawDetail) == 0 {
		return
	}
	var raw struct {
		CheckoutCarrier              string `json:"checkout_shipping_carrier"`
		ShippingTrackingInfoResponse struct {
			TrackingInfo []models.ShopeeShippingTrackingEvent `json:"tracking_info"`
		} `json:"shipping_tracking_info_response"`
	}
	if err := json.Unmarshal(out.RawDetail, &raw); err != nil {
		return
	}
	out.CheckoutCarrier = strings.TrimSpace(raw.CheckoutCarrier)
	if len(raw.ShippingTrackingInfoResponse.TrackingInfo) > 0 {
		out.ShippingTracking = raw.ShippingTrackingInfoResponse.TrackingInfo
	}
}

func (r *ShopeeRealtimeRepo) documentRouteForBill(ctx context.Context, billID string) (string, error) {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return "", nil
	}
	var route string
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(document_route, '') FROM bills WHERE id = $1::uuid`, billID).Scan(&route)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return route, err
}

func nullableTime(v time.Time) interface{} {
	if v.IsZero() {
		return nil
	}
	return v
}

func normalizeShopeeSnapshotSource(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "push", "sync", "shipping":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "unknown"
	}
}
