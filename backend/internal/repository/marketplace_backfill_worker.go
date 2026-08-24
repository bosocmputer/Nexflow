package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"go.uber.org/zap"

	"nexflow/internal/models"
)

const marketplaceBackfillBatchSize = 200

type marketplaceBackfillJob struct {
	ID           string
	JobType      string
	CursorID     string
	LeaseOwner   string
	AttemptCount int
}

type MarketplaceBackfillWorker struct {
	repo               *MarketplaceAliasRepo
	unitCatalogEnabled bool
	reservationEnabled bool
	log                *zap.Logger
}

func NewMarketplaceBackfillWorker(repo *MarketplaceAliasRepo, unitCatalogEnabled, reservationEnabled bool, log *zap.Logger) *MarketplaceBackfillWorker {
	return &MarketplaceBackfillWorker{repo: repo, unitCatalogEnabled: unitCatalogEnabled, reservationEnabled: reservationEnabled, log: log}
}

func (w *MarketplaceBackfillWorker) Start(ctx context.Context) {
	if w == nil || w.repo == nil || !w.unitCatalogEnabled {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		jobsEnsured := false
		for {
			if !jobsEnsured {
				if err := w.ensureJobs(ctx); err != nil {
					if w.log != nil {
						w.log.Warn("ensure marketplace backfill jobs", zap.Error(err))
					}
				} else {
					jobsEnsured = true
					w.tick(ctx)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if jobsEnsured {
					w.tick(ctx)
				}
			}
		}
	}()
}

func (w *MarketplaceBackfillWorker) ensureJobs(ctx context.Context) error {
	tx, err := w.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_conversion_readiness(singleton)
		VALUES(true) ON CONFLICT(singleton) DO NOTHING`); err != nil {
		return err
	}
	for _, jobType := range []string{"alias_conversion", "bill_snapshots"} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_backfill_jobs(tenant_key,job_type,idempotency_key)
			VALUES($1,$2,$3) ON CONFLICT(idempotency_key) DO NOTHING`, w.repo.tenantKey, jobType, "initial-v1:"+jobType); err != nil {
			return err
		}
	}
	if w.reservationEnabled {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_backfill_jobs(tenant_key,job_type,idempotency_key)
			VALUES($1,'reservation_ledger','initial-v1:reservation_ledger') ON CONFLICT(idempotency_key) DO NOTHING`, w.repo.tenantKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (w *MarketplaceBackfillWorker) tick(ctx context.Context) {
	owner := fmt.Sprintf("marketplace-backfill:%d", time.Now().UnixNano())
	job, err := w.claim(ctx, owner, 5*time.Minute)
	if err != nil || job == nil {
		if err != nil && w.log != nil {
			w.log.Warn("claim marketplace backfill", zap.Error(err))
		}
		return
	}
	processed, done, err := w.process(ctx, job)
	if err != nil {
		_ = w.fail(context.Background(), job, err)
		if w.log != nil {
			w.log.Warn("marketplace backfill", zap.String("job_id", job.ID), zap.String("job_type", job.JobType), zap.Error(err))
		}
		return
	}
	if done {
		if err := w.complete(ctx, job); err != nil && w.log != nil {
			w.log.Warn("complete marketplace backfill", zap.Error(err))
		}
		return
	}
	if processed == 0 {
		_ = w.fail(context.Background(), job, errors.New("backfill made no progress"))
	}
}

func (w *MarketplaceBackfillWorker) claim(ctx context.Context, owner string, duration time.Duration) (*marketplaceBackfillJob, error) {
	var job marketplaceBackfillJob
	var cursor sql.NullString
	err := w.repo.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT j.id FROM marketplace_backfill_jobs j
		WHERE ((j.status='queued') OR (j.status='failed' AND j.next_attempt_at<=NOW() AND j.attempt_count<10)
		  OR (j.status='running' AND j.lease_until<NOW()))
		  AND (j.job_type<>'reservation_ledger' OR $3::boolean)
		  AND EXISTS(SELECT 1 FROM marketplace_conversion_readiness r WHERE r.singleton=true AND r.catalog_generation_ready=true)
		  AND (j.job_type='alias_conversion'
		    OR (j.job_type='bill_snapshots' AND NOT EXISTS (
		      SELECT 1 FROM marketplace_backfill_jobs d WHERE d.job_type='alias_conversion' AND d.status<>'completed'))
		    OR (j.job_type='reservation_ledger' AND NOT EXISTS (
		      SELECT 1 FROM marketplace_backfill_jobs d WHERE d.job_type IN ('alias_conversion','bill_snapshots') AND d.status<>'completed')))
		ORDER BY CASE j.job_type WHEN 'alias_conversion' THEN 1 WHEN 'bill_snapshots' THEN 2 ELSE 3 END,j.created_at,j.id
		FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE marketplace_backfill_jobs j SET status='running',lease_owner=$1,
		lease_until=NOW()+($2*INTERVAL '1 second'),heartbeat_at=NOW(),attempt_count=attempt_count+1,
		error_message='',updated_at=NOW()
	FROM candidate WHERE j.id=candidate.id
	RETURNING j.id::text,j.job_type,j.cursor_id::text,j.lease_owner,j.attempt_count`,
		owner, int64(duration/time.Second), w.reservationEnabled).Scan(
		&job.ID, &job.JobType, &cursor, &job.LeaseOwner, &job.AttemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.CursorID = cursor.String
	return &job, nil
}

func (w *MarketplaceBackfillWorker) process(ctx context.Context, job *marketplaceBackfillJob) (int, bool, error) {
	switch job.JobType {
	case "alias_conversion":
		return w.processAliasBatch(ctx, job)
	case "bill_snapshots":
		return w.processBillBatch(ctx, job)
	case "reservation_ledger":
		return w.processReservationBatch(ctx, job)
	default:
		return 0, false, fmt.Errorf("unsupported marketplace backfill job %q", job.JobType)
	}
}

func (w *MarketplaceBackfillWorker) processAliasBatch(ctx context.Context, job *marketplaceBackfillJob) (int, bool, error) {
	tx, err := w.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT a.id::text FROM marketplace_item_aliases a
		JOIN LATERAL (SELECT id FROM sml_catalog_sync_runs WHERE status='active'
		  ORDER BY activated_at DESC NULLS LAST,created_at DESC LIMIT 1) g ON true
		WHERE a.is_active=true AND a.unit_catalog_generation IS DISTINCT FROM g.id
		ORDER BY a.id FOR UPDATE OF a LIMIT $1`, marketplaceBackfillBatchSize)
	if err != nil {
		return 0, false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, false, err
	}
	if len(ids) == 0 {
		return 0, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `WITH active_generation AS (
		SELECT id FROM sml_catalog_sync_runs WHERE status='active'
		ORDER BY activated_at DESC NULLS LAST,created_at DESC LIMIT 1
	), evidence AS (
		SELECT a.id,u.stand_value,u.divide_value,g.id AS generation_id,
		       c.is_active AS product_active,c.set_document_valid,
		       CASE WHEN g.id IS NOT NULL AND u.stand_value>0 AND u.divide_value>0
		         AND COALESCE(c.is_active,false) AND COALESCE(c.set_document_valid,true) AND a.scope_confirmed
		         AND NOT EXISTS(SELECT 1 FROM shopee_stock_mappings m WHERE m.marketplace_alias_id=a.id AND m.manual_unit_factor IS NOT NULL)
		         THEN 'ready' ELSE 'needs_review' END AS next_status
		FROM marketplace_item_aliases a
		LEFT JOIN active_generation g ON true
		LEFT JOIN sml_catalog_units u ON u.generation_id=g.id AND u.item_code=a.item_code AND u.unit_code=a.unit_code AND u.is_active=true
		LEFT JOIN sml_catalog c ON c.item_code=a.item_code
		WHERE a.id=ANY($1::uuid[])
	)
	UPDATE marketplace_item_aliases a SET
		unit_stand_value=e.stand_value,unit_divide_value=e.divide_value,unit_catalog_generation=e.generation_id,
		conversion_status=e.next_status,
		quantity_multiplier=GREATEST(COALESCE(a.quantity_multiplier,1),1),
		mapping_revision=GREATEST(a.mapping_revision,1)+CASE WHEN
		  a.unit_stand_value IS DISTINCT FROM e.stand_value OR a.unit_divide_value IS DISTINCT FROM e.divide_value
		  OR a.conversion_status IS DISTINCT FROM e.next_status THEN 1 ELSE 0 END,
		updated_at=NOW()
	FROM evidence e WHERE a.id=e.id`, pq.Array(ids)); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings m SET
		sml_item_code=a.item_code,sml_unit_code=a.unit_code,
		unit_factor=CASE WHEN a.conversion_status='ready' THEN a.quantity_multiplier*a.unit_stand_value/a.unit_divide_value ELSE m.unit_factor END,
		shared_pool_enabled=CASE WHEN a.stock_policy='managed' THEN m.shared_pool_enabled ELSE false END,
		warning_codes=(m.warning_codes-'conversion_policy_missing'-'conversion_needs_review'-'conversion_stale'-'conversion_blocked'-
		  'stock_policy_blocked'-'stock_policy_manual_unmanaged'-'stock_policy_zeroing'-'stock_policy_disabled_zero') ||
		  CASE WHEN a.conversion_status='ready' THEN '[]'::jsonb ELSE jsonb_build_array('conversion_'||a.conversion_status) END ||
		  CASE WHEN a.stock_policy='managed' THEN '[]'::jsonb ELSE jsonb_build_array('stock_policy_'||a.stock_policy) END,
		updated_at=NOW()
	FROM marketplace_item_aliases a WHERE m.marketplace_alias_id=a.id AND a.id=ANY($1::uuid[])`, pq.Array(ids)); err != nil {
		return 0, false, err
	}
	if err := w.advanceTx(ctx, tx, job, ids[len(ids)-1], len(ids)); err != nil {
		return 0, false, err
	}
	return len(ids), false, tx.Commit()
}

func (w *MarketplaceBackfillWorker) processBillBatch(ctx context.Context, job *marketplaceBackfillJob) (int, bool, error) {
	tx, err := w.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT bi.id::text,bi.bill_id::text FROM bill_items bi
		JOIN bills b ON b.id=bi.bill_id JOIN marketplace_item_aliases a ON a.id=bi.marketplace_alias_id
		WHERE b.archived_at IS NULL AND b.current_sml_attempt_id IS NULL AND b.status IN ('pending','needs_review','failed')
		  AND (bi.mapping_revision_snapshot IS DISTINCT FROM a.mapping_revision
		    OR bi.unit_catalog_generation_snapshot IS DISTINCT FROM a.unit_catalog_generation)
		ORDER BY bi.id FOR UPDATE OF bi LIMIT $1`, marketplaceBackfillBatchSize)
	if err != nil {
		return 0, false, err
	}
	ids, billIDs := []string{}, []string{}
	seenBills := map[string]struct{}{}
	for rows.Next() {
		var id, billID string
		if err := rows.Scan(&id, &billID); err != nil {
			rows.Close()
			return 0, false, err
		}
		ids = append(ids, id)
		if _, seen := seenBills[billID]; !seen {
			seenBills[billID] = struct{}{}
			billIDs = append(billIDs, billID)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, false, err
	}
	if len(ids) == 0 {
		return 0, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bill_items bi SET
		item_code=CASE WHEN bi.conversion_override_fields ? 'item_code' THEN bi.item_code ELSE a.item_code END,
		unit_code=CASE WHEN bi.conversion_override_fields ? 'unit_code' THEN bi.unit_code ELSE a.unit_code END,
		source_qty=COALESCE(bi.source_qty,bi.qty),gross_amount=COALESCE(bi.gross_amount,ROUND(bi.qty*COALESCE(bi.price,0),2)),
		sml_qty=CASE WHEN a.unit_stand_value IS NOT NULL AND a.unit_divide_value>0 THEN bi.qty*a.quantity_multiplier ELSE NULL END,
		quantity_multiplier_snapshot=a.quantity_multiplier,unit_stand_value_snapshot=a.unit_stand_value,
		unit_divide_value_snapshot=a.unit_divide_value,
		base_qty_snapshot=CASE WHEN a.unit_stand_value IS NOT NULL AND a.unit_divide_value>0
		  THEN bi.qty*a.quantity_multiplier*a.unit_stand_value/a.unit_divide_value ELSE NULL END,
		mapping_revision_snapshot=a.mapping_revision,unit_catalog_generation_snapshot=a.unit_catalog_generation,
		set_definition_hash_snapshot=COALESCE(c.set_definition_hash,''),
		conversion_issue_code=CASE WHEN bi.conversion_override_fields ?| ARRAY['item_code','unit_code'] THEN 'manual_conversion_review_required'
		  WHEN a.conversion_status<>'ready' THEN 'conversion_'||a.conversion_status WHEN NOT a.sales_enabled THEN 'sales_disabled' ELSE '' END,
		mapped=NOT (bi.conversion_override_fields ?| ARRAY['item_code','unit_code']) AND a.conversion_status='ready' AND a.sales_enabled
	FROM marketplace_item_aliases a LEFT JOIN sml_catalog c ON c.item_code=a.item_code
	WHERE bi.id=ANY($1::uuid[]) AND bi.marketplace_alias_id=a.id`, pq.Array(ids)); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bills b SET mutation_revision=mutation_revision+1,
		status=CASE WHEN EXISTS(SELECT 1 FROM bill_items bi WHERE bi.bill_id=b.id AND bi.mapped IS DISTINCT FROM true)
		  THEN 'needs_review' WHEN b.status='needs_review' THEN 'pending' ELSE b.status END
		WHERE b.id=ANY($1::uuid[]) AND b.current_sml_attempt_id IS NULL AND b.archived_at IS NULL`, pq.Array(billIDs)); err != nil {
		return 0, false, err
	}
	if err := w.advanceTx(ctx, tx, job, ids[len(ids)-1], len(ids)); err != nil {
		return 0, false, err
	}
	return len(ids), false, tx.Commit()
}

func (w *MarketplaceBackfillWorker) processReservationBatch(ctx context.Context, job *marketplaceBackfillJob) (int, bool, error) {
	tx, err := w.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT bi.id::text FROM bill_items bi JOIN bills b ON b.id=bi.bill_id
		WHERE b.source IN ('shopee','lazada','tiktok') AND b.archived_at IS NULL AND b.current_sml_attempt_id IS NULL
		  AND b.status IN ('pending','needs_review','failed')
		  AND COALESCE(bi.source_sku,'') NOT IN ($1,$2,$3)
		  AND COALESCE(NULLIF(b.sml_order_id,''),b.raw_data->>'order_id','')<>''
		  AND UPPER(COALESCE(b.raw_data->>'order_status',b.raw_data->>'status',b.raw_data->>'payment_status',''))<>'UNPAID'
		  AND NOT EXISTS (
		    SELECT 1 FROM marketplace_stock_reservations r
		    WHERE r.source=b.source AND r.account_key=COALESCE(NULLIF(b.source_account_key,''),'default')
		      AND r.order_id=COALESCE(NULLIF(b.sml_order_id,''),b.raw_data->>'order_id')
		      AND r.source_line_id=COALESCE(NULLIF(bi.source_line_id,''),bi.id::text)
		      AND r.external_item_id=COALESCE(bi.source_item_id,'') AND r.external_variant_id=COALESCE(bi.source_variant_id,'')
		      AND r.mapping_revision IS NOT DISTINCT FROM bi.mapping_revision_snapshot
		      AND r.base_qty IS NOT DISTINCT FROM bi.base_qty_snapshot
		      AND r.sml_item_code=COALESCE(bi.item_code,'')
		      AND r.set_definition_hash=COALESCE(bi.set_definition_hash_snapshot,'')
		  )
		ORDER BY bi.id LIMIT $4`, models.ShopeeShippingSourceSKU, models.LazadaShippingSourceSKU, models.TikTokShippingSourceSKU, marketplaceBackfillBatchSize)
	if err != nil {
		return 0, false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, false, err
	}
	if len(ids) == 0 {
		return 0, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_reservations
		(tenant_key,source,account_key,order_id,source_line_id,external_item_id,external_variant_id,bill_id,marketplace_alias_id,
		 mapping_revision,source_qty,quantity_multiplier,unit_code,unit_stand_value,unit_divide_value,base_qty,sml_item_code,
		 set_definition_hash,source_event_version,state,state_reason)
	SELECT $2,b.source,COALESCE(NULLIF(b.source_account_key,''),'default'),COALESCE(NULLIF(b.sml_order_id,''),b.raw_data->>'order_id'),
		COALESCE(NULLIF(bi.source_line_id,''),bi.id::text),COALESCE(bi.source_item_id,''),COALESCE(bi.source_variant_id,''),b.id,
		bi.marketplace_alias_id,bi.mapping_revision_snapshot,COALESCE(bi.source_qty,bi.qty),COALESCE(bi.quantity_multiplier_snapshot,1),
		COALESCE(bi.unit_code,''),bi.unit_stand_value_snapshot,bi.unit_divide_value_snapshot,bi.base_qty_snapshot,
		COALESCE(bi.item_code,''),COALESCE(bi.set_definition_hash_snapshot,''),
		COALESCE(b.raw_data->>'event_version',b.raw_data->>'amount_source_fingerprint',b.raw_data->>'import_run_id',''),
		CASE WHEN bi.marketplace_alias_id IS NOT NULL AND bi.mapping_revision_snapshot IS NOT NULL
		  AND bi.quantity_multiplier_snapshot>=1 AND bi.unit_stand_value_snapshot>0 AND bi.unit_divide_value_snapshot>0
		  AND bi.base_qty_snapshot>0 AND COALESCE(bi.item_code,'')<>'' AND COALESCE(bi.conversion_issue_code,'')=''
		  THEN 'active' ELSE 'blocked_mapping' END,
		CASE WHEN bi.marketplace_alias_id IS NOT NULL AND bi.mapping_revision_snapshot IS NOT NULL
		  AND bi.quantity_multiplier_snapshot>=1 AND bi.unit_stand_value_snapshot>0 AND bi.unit_divide_value_snapshot>0
		  AND bi.base_qty_snapshot>0 AND COALESCE(bi.item_code,'')<>'' AND COALESCE(bi.conversion_issue_code,'')=''
		  THEN '' ELSE COALESCE(NULLIF(bi.conversion_issue_code,''),'conversion_snapshot_missing') END
	FROM bill_items bi JOIN bills b ON b.id=bi.bill_id WHERE bi.id=ANY($1::uuid[])
	ON CONFLICT (source,account_key,order_id,source_line_id,external_item_id,external_variant_id) DO UPDATE SET
	  bill_id=EXCLUDED.bill_id,marketplace_alias_id=EXCLUDED.marketplace_alias_id,mapping_revision=EXCLUDED.mapping_revision,
	  source_qty=EXCLUDED.source_qty,quantity_multiplier=EXCLUDED.quantity_multiplier,unit_code=EXCLUDED.unit_code,
	  unit_stand_value=EXCLUDED.unit_stand_value,unit_divide_value=EXCLUDED.unit_divide_value,base_qty=EXCLUDED.base_qty,
	  sml_item_code=EXCLUDED.sml_item_code,set_definition_hash=EXCLUDED.set_definition_hash,
	  source_event_version=EXCLUDED.source_event_version,state=EXCLUDED.state,state_reason=EXCLUDED.state_reason,
	  demand_revision=marketplace_stock_reservations.demand_revision+1,updated_at=NOW()
	WHERE marketplace_stock_reservations.state IN ('active','blocked_mapping')`, pq.Array(ids), w.repo.tenantKey); err != nil {
		return 0, false, err
	}
	if err := w.advanceTx(ctx, tx, job, ids[len(ids)-1], len(ids)); err != nil {
		return 0, false, err
	}
	return len(ids), false, tx.Commit()
}

func (w *MarketplaceBackfillWorker) advanceTx(ctx context.Context, tx *sql.Tx, job *marketplaceBackfillJob, cursor string, processed int) error {
	result, err := tx.ExecContext(ctx, `UPDATE marketplace_backfill_jobs SET cursor_id=$3::uuid,
		processed_count=processed_count+$4,heartbeat_at=NOW(),lease_until=NOW()+INTERVAL '5 minutes',updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND status='running'`, job.ID, job.LeaseOwner, cursor, processed)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrMarketplaceImpactChanged
	}
	return nil
}

func (w *MarketplaceBackfillWorker) complete(ctx context.Context, job *marketplaceBackfillJob) error {
	tx, err := w.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE marketplace_backfill_jobs SET status='completed',lease_owner='',lease_until=NULL,
		heartbeat_at=NOW(),finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid AND lease_owner=$2 AND status='running'`, job.ID, job.LeaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrMarketplaceImpactChanged
	}
	if job.JobType == "reservation_ledger" {
		// Invalidate every old dependency pool before rebuilding component demand.
		// This preserves the monotonic demand revision fence on both sides of a
		// catalog or mapping change, including a set becoming a normal item.
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
			SELECT demand.warehouse_code,demand.location_code,demand.item_code,1 FROM (
			  SELECT r.warehouse_code,r.location_code,r.sml_item_code AS item_code FROM marketplace_stock_reservations r
			  WHERE r.state IN ('active','blocked_mapping') AND NOT EXISTS(
			    SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
			  UNION SELECT c.warehouse_code,c.location_code,c.component_item_code
			  FROM marketplace_stock_reservation_components c JOIN marketplace_stock_reservations r ON r.id=c.reservation_id
			  WHERE r.state IN ('active','blocked_mapping')
			) demand WHERE demand.item_code<>'' GROUP BY demand.warehouse_code,demand.location_code,demand.item_code
			ON CONFLICT (warehouse_code,location_code,item_code) DO UPDATE
			SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM marketplace_stock_reservation_components c
			USING marketplace_stock_reservations r WHERE c.reservation_id=r.id AND r.state IN ('active','blocked_mapping')`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_reservation_components
			(reservation_id,component_item_code,warehouse_code,location_code,component_base_qty,set_definition_hash)
			SELECT r.id,c.component_item_code,r.warehouse_code,r.location_code,SUM(r.base_qty*c.qty*c.unit_factor),r.set_definition_hash
			FROM marketplace_stock_reservations r JOIN sml_catalog_set_components c
			  ON c.parent_item_code=r.sml_item_code AND c.definition_hash=r.set_definition_hash AND c.is_active=true AND c.unit_valid=true
			WHERE r.state='active' AND r.set_definition_hash<>''
			GROUP BY r.id,c.component_item_code,r.warehouse_code,r.location_code,r.set_definition_hash
			ON CONFLICT (reservation_id,component_item_code,warehouse_code,location_code) DO UPDATE
			SET component_base_qty=EXCLUDED.component_base_qty,set_definition_hash=EXCLUDED.set_definition_hash`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
			SELECT demand.warehouse_code,demand.location_code,demand.item_code,1 FROM (
			  SELECT r.warehouse_code,r.location_code,r.sml_item_code AS item_code FROM marketplace_stock_reservations r
			  WHERE r.state='active' AND NOT EXISTS(SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
			  UNION SELECT c.warehouse_code,c.location_code,c.component_item_code FROM marketplace_stock_reservation_components c
			  JOIN marketplace_stock_reservations r ON r.id=c.reservation_id WHERE r.state='active'
			) demand WHERE demand.item_code<>'' GROUP BY demand.warehouse_code,demand.location_code,demand.item_code
			ON CONFLICT (warehouse_code,location_code,item_code) DO UPDATE
			SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_conversion_readiness SET
		mapping_backfill_ready=NOT EXISTS(SELECT 1 FROM marketplace_backfill_jobs WHERE job_type IN ('alias_conversion','bill_snapshots') AND status<>'completed'),
		reservation_ledger_ready=NOT EXISTS(SELECT 1 FROM marketplace_backfill_jobs WHERE job_type='reservation_ledger' AND status<>'completed')
		  AND EXISTS(SELECT 1 FROM marketplace_backfill_jobs WHERE job_type='reservation_ledger'),
		reconciliation_summary=jsonb_build_object(
		  'aliases',(SELECT COUNT(*) FROM marketplace_item_aliases WHERE is_active=true),
		  'ready_aliases',(SELECT COUNT(*) FROM marketplace_item_aliases WHERE is_active=true AND conversion_status='ready'),
		  'reservations',(SELECT COUNT(*) FROM marketplace_stock_reservations),
		  'blocked_reservations',(SELECT COUNT(*) FROM marketplace_stock_reservations WHERE state='blocked_mapping')),
		updated_at=NOW() WHERE singleton=true`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs
		(action,source,level,tenant_key,job_id,before_state,after_state,detail)
		VALUES('marketplace_backfill_completed','marketplace','info',$1,$2::uuid,
		  jsonb_build_object('status','running'),jsonb_build_object('status','completed'),
		  jsonb_build_object('job_type',$3))`, w.repo.tenantKey, job.ID, job.JobType); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *MarketplaceBackfillWorker) fail(ctx context.Context, job *marketplaceBackfillJob, cause error) error {
	message := "marketplace backfill failed"
	if cause != nil {
		message = cause.Error()
	}
	delaySeconds := int64(15 * (1 << min(job.AttemptCount, 8)))
	_, err := w.repo.db.ExecContext(ctx, `UPDATE marketplace_backfill_jobs SET status='failed',failed_count=failed_count+1,
		error_message=$3,next_attempt_at=NOW()+($4*INTERVAL '1 second'),lease_owner='',lease_until=NULL,updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND status='running'`, job.ID, job.LeaseOwner, message, delaySeconds)
	return err
}

func (r *MarketplaceAliasRepo) ConversionReadiness(ctx context.Context) (*models.MarketplaceConversionReadiness, error) {
	var result models.MarketplaceConversionReadiness
	var summary []byte
	err := r.db.QueryRowContext(ctx, `SELECT catalog_generation_ready,mapping_backfill_ready,reservation_ledger_ready,
		reconciliation_summary,updated_at FROM marketplace_conversion_readiness WHERE singleton=true`).Scan(
		&result.CatalogGenerationReady, &result.MappingBackfillReady, &result.ReservationLedgerReady, &summary, &result.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		result.ReconciliationSummary = map[string]any{}
		result.Jobs = []models.MarketplaceBackfillJobStatus{}
		return &result, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(summary, &result.ReconciliationSummary)
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,job_type,status,processed_count,failed_count,attempt_count,
		error_message,updated_at,finished_at FROM marketplace_backfill_jobs ORDER BY created_at,job_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result.Jobs = []models.MarketplaceBackfillJobStatus{}
	for rows.Next() {
		var job models.MarketplaceBackfillJobStatus
		if err := rows.Scan(&job.ID, &job.JobType, &job.Status, &job.ProcessedCount, &job.FailedCount,
			&job.AttemptCount, &job.ErrorMessage, &job.UpdatedAt, &job.FinishedAt); err != nil {
			return nil, err
		}
		result.Jobs = append(result.Jobs, job)
	}
	return &result, rows.Err()
}
