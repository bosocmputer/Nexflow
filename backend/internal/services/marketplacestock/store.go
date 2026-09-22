package marketplacestock

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) Overview(ctx context.Context) (*Overview, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	overview := &Overview{Available: true, Pools: []Pool{}, CheckedAt: time.Now().UTC()}
	err := s.db.QueryRowContext(ctx, `SELECT warehouse_code,location_code,default_buffer_pct::float8,kill_switch_enabled,config_version,updated_at
		FROM marketplace_stock_settings WHERE singleton=true`).Scan(
		&overview.Settings.WarehouseCode, &overview.Settings.LocationCode, &overview.Settings.DefaultBufferPct,
		&overview.Settings.KillSwitch, &overview.Settings.ConfigVersion, &overview.Settings.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		overview.Settings = Settings{DefaultBufferPct: 10}
	} else if err != nil {
		return nil, fmt.Errorf("load marketplace stock settings: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
		shared_risk_acknowledged,status,auto_enabled,kill_switch_enabled,schedule_interval_seconds,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_schedule_at,last_error,updated_at
		FROM marketplace_stock_pools ORDER BY updated_at DESC,id`)
	if err != nil {
		return nil, fmt.Errorf("list marketplace stock pools: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		pool, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		overview.Pools = append(overview.Pools, pool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(overview.Pools) == 0 {
		return overview, nil
	}
	members, err := s.listMembers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range overview.Pools {
		overview.Pools[i].Members = append(overview.Pools[i].Members, members[overview.Pools[i].ID]...)
	}
	return overview, nil
}

func (s *PostgresStore) Candidates(ctx context.Context, source string) ([]Candidate, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id::text,a.source,a.account_key,a.external_item_id,a.external_variant_id,
		COALESCE(NULLIF(a.source_product_name,''),a.raw_name),a.source_variant_name,a.item_code,a.unit_code,
		a.quantity_multiplier::float8
		FROM marketplace_item_aliases a
		LEFT JOIN marketplace_stock_pool_members m ON m.marketplace_alias_id=a.id AND m.enabled=true
		WHERE a.is_active=true AND a.source IN ('shopee','tiktok')
		  AND ($1='' OR a.source=$1)
		  AND a.conversion_status='ready' AND a.sales_enabled=true
		  AND a.item_code<>'' AND a.unit_code<>''
		  AND a.external_item_id<>'' AND a.external_variant_id<>''
		  AND m.id IS NULL
		ORDER BY a.item_code,a.unit_code,a.source,a.source_product_name,a.source_variant_name,a.id
		LIMIT 500`, source)
	if err != nil {
		return nil, fmt.Errorf("list marketplace stock candidates: %w", err)
	}
	defer rows.Close()
	result := []Candidate{}
	for rows.Next() {
		candidate := Candidate{}
		if err := rows.Scan(&candidate.AliasID, &candidate.Source, &candidate.AccountKey, &candidate.ExternalProductID,
			&candidate.ExternalSKUID, &candidate.ProductName, &candidate.VariantName, &candidate.SMLItemCode,
			&candidate.SMLUnitCode, &candidate.UnitFactor); err != nil {
			return nil, err
		}
		candidate.Enabled = true
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (s *PostgresStore) CreatePool(ctx context.Context, input PoolInput, userID string) (*Pool, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	pool, err := insertPool(ctx, tx, input, userID)
	if err != nil {
		return nil, err
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_pool_created", pool.ID, userID, map[string]any{
		"sml_item_code": pool.SMLItemCode, "sml_unit_code": pool.SMLUnitCode, "mode": pool.AllocationMode,
		"member_count": len(pool.Members), "status": pool.Status,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return pool, nil
}

func (s *PostgresStore) UpdatePool(ctx context.Context, poolID string, input PoolUpdate, userID string) (*Pool, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var updated Pool
	err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_pools
		SET sml_item_code=$2,sml_unit_code=$3,allocation_mode=$4,buffer_pct_override=$5,
		shared_risk_acknowledged=$6,status='paused',auto_enabled=false,dry_run_required=true,
		paused_reason='configuration_changed',config_version=config_version+1,last_error='',updated_by=$7,updated_at=NOW()
		WHERE id=$1::uuid AND config_version=$8
		RETURNING id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
		shared_risk_acknowledged,status,auto_enabled,kill_switch_enabled,schedule_interval_seconds,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_schedule_at,last_error,updated_at`,
		poolID, input.SMLItemCode, input.SMLUnitCode, input.AllocationMode, input.BufferPctOverride,
		input.SharedRiskAcknowledged, userID, input.ExpectedConfigVersion,
	).Scan(poolScanner(&updated)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("update marketplace stock pool: %w", err)
	}
	if err := upsertPoolMembers(ctx, tx, updated.ID, input.Members); err != nil {
		return nil, err
	}
	members, err := listPoolMembersTx(ctx, tx, updated.ID)
	if err != nil {
		return nil, err
	}
	updated.Members = members
	if err := insertAudit(ctx, tx, "marketplace_stock_pool_updated", updated.ID, userID, map[string]any{
		"config_version": updated.ConfigVersion, "mode": updated.AllocationMode, "member_count": len(updated.Members),
		"paused_reason": updated.PausedReason,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *PostgresStore) UpdateSettings(ctx context.Context, input SettingsUpdate, userID string) (*Settings, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var result Settings
	err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_settings
		SET warehouse_code=$1,location_code=$2,default_buffer_pct=$3,kill_switch_enabled=$4,
		config_version=config_version+1,updated_by=$5,updated_at=NOW()
		WHERE singleton=true AND config_version=$6
		RETURNING warehouse_code,location_code,default_buffer_pct::float8,kill_switch_enabled,config_version,updated_at`,
		input.WarehouseCode, input.LocationCode, input.DefaultBufferPct, input.KillSwitch, userID, input.ExpectedConfigVersion,
	).Scan(&result.WarehouseCode, &result.LocationCode, &result.DefaultBufferPct, &result.KillSwitch, &result.ConfigVersion, &result.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("update marketplace stock settings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_pools
		SET status='paused',auto_enabled=false,dry_run_required=true,paused_reason='stock_source_changed',
		config_version=config_version+1,updated_at=NOW()
		WHERE status IN ('ready','active')`); err != nil {
		return nil, err
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_settings_updated", "", userID, map[string]any{
		"warehouse_code": result.WarehouseCode, "location_code": result.LocationCode,
		"buffer_pct": result.DefaultBufferPct, "kill_switch_enabled": result.KillSwitch,
		"config_version": result.ConfigVersion,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *PostgresStore) StartPreview(ctx context.Context, poolID string, input PreviewRequest, userID string) (*PreviewPlan, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	plan := &PreviewPlan{RequestedBy: userID}
	err = tx.QueryRowContext(ctx, `SELECT warehouse_code,location_code,default_buffer_pct::float8,kill_switch_enabled,config_version,updated_at
		FROM marketplace_stock_settings WHERE singleton=true FOR SHARE`).Scan(
		&plan.Settings.WarehouseCode, &plan.Settings.LocationCode, &plan.Settings.DefaultBufferPct,
		&plan.Settings.KillSwitch, &plan.Settings.ConfigVersion, &plan.Settings.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("load marketplace stock settings for preview: %w", err)
	}
	if plan.Settings.KillSwitch {
		return nil, ErrGlobalKillSwitch
	}
	if strings.TrimSpace(plan.Settings.WarehouseCode) == "" || strings.TrimSpace(plan.Settings.LocationCode) == "" {
		return nil, ErrInvalidPoolInput
	}
	err = tx.QueryRowContext(ctx, `SELECT id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
		shared_risk_acknowledged,status,auto_enabled,kill_switch_enabled,schedule_interval_seconds,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_schedule_at,last_error,updated_at
		FROM marketplace_stock_pools WHERE id=$1::uuid FOR UPDATE`, poolID).Scan(poolScanner(&plan.Pool)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("load marketplace stock pool for preview: %w", err)
	}
	if plan.Pool.Status == "paused" {
		return nil, ErrPoolPaused
	}
	if plan.Pool.ConfigVersion != input.ExpectedConfigVersion {
		return nil, ErrConfigVersionConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT u.stand_value::float8/u.divide_value::float8
		FROM sml_catalog_sync_runs r
		JOIN sml_catalog_units u ON u.generation_id=r.id AND u.is_active=true
		WHERE r.status='active' AND u.item_code=$1 AND u.unit_code=$2
		LIMIT 1`, plan.Pool.SMLItemCode, plan.Pool.SMLUnitCode).Scan(&plan.UnitBaseFactor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidPoolInput
		}
		return nil, fmt.Errorf("load SML unit conversion for preview: %w", err)
	}
	if plan.UnitBaseFactor <= 0 {
		return nil, ErrInvalidPoolInput
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(demand.base_qty),0)::float8 FROM (
		SELECT r.base_qty FROM marketplace_stock_reservations r
		WHERE r.sml_item_code=$1 AND r.state IN ('active','sending_sml','awaiting_stock_recalc') AND r.base_qty>0
		  AND ((r.warehouse_code=$2 AND r.location_code=$3) OR (r.warehouse_code='' AND r.location_code=''))
		  AND NOT EXISTS(SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
		UNION ALL
		SELECT c.component_base_qty FROM marketplace_stock_reservation_components c
		JOIN marketplace_stock_reservations r ON r.id=c.reservation_id
		WHERE c.component_item_code=$1 AND r.state IN ('active','sending_sml','awaiting_stock_recalc') AND c.component_base_qty>0
		  AND ((c.warehouse_code=$2 AND c.location_code=$3) OR (c.warehouse_code='' AND c.location_code=''))
	) demand`, plan.Pool.SMLItemCode, plan.Settings.WarehouseCode, plan.Settings.LocationCode).Scan(&plan.PendingBaseQty); err != nil {
		return nil, fmt.Errorf("load marketplace reservations for preview: %w", err)
	}
	plan.Pool.Members, err = listPoolMembersTx(ctx, tx, plan.Pool.ID)
	if err != nil {
		return nil, err
	}
	if len(plan.Pool.Members) == 0 {
		return nil, ErrInvalidPoolInput
	}
	var liveRunID string
	_ = tx.QueryRowContext(ctx, `SELECT id::text FROM marketplace_stock_runs WHERE pool_id=$1::uuid AND status IN ('queued','running') LIMIT 1`, plan.Pool.ID).Scan(&liveRunID)
	if liveRunID != "" {
		return nil, ErrPreviewAlreadyRunning
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO marketplace_stock_runs(pool_id,trigger_source,run_type,status,config_version,requested_by,started_at)
		VALUES($1::uuid,'manual','preview','running',$2,$3::uuid,NOW()) RETURNING id::text`, plan.Pool.ID, plan.Pool.ConfigVersion, userID).Scan(&plan.RunID)
	if err != nil {
		return nil, fmt.Errorf("create marketplace stock preview: %w", err)
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_preview_started", plan.Pool.ID, userID, map[string]any{
		"run_id": plan.RunID, "config_version": plan.Pool.ConfigVersion,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *PostgresStore) CompletePreview(ctx context.Context, result PreviewResult) error {
	if s == nil || s.db == nil || strings.TrimSpace(result.RunID) == "" {
		return errors.New("marketplace stock preview result is invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var poolID, userID string
	var configVersion int64
	err = tx.QueryRowContext(ctx, `SELECT pool_id::text,COALESCE(requested_by::text,''),config_version
		FROM marketplace_stock_runs WHERE id=$1::uuid AND status='running' FOR UPDATE`, result.RunID).Scan(&poolID, &userID, &configVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConfigVersionConflict
	}
	if err != nil {
		return err
	}
	summary, err := json.Marshal(map[string]any{
		"sml_available_qty": result.SMLAvailableQty, "reservation_qty": result.ReservationQty, "usable_qty": result.UsableQty,
		"buffer_qty": result.BufferQty, "distributable_qty": result.DistributableQty, "write_ready": false,
	})
	if err != nil {
		return err
	}
	for _, line := range result.Lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_run_lines(run_id,member_id,status,target_qty,error_message)
			VALUES($1::uuid,$2::uuid,$3,$4,$5)`, result.RunID, line.MemberID, line.Status, line.TargetQty, line.Message); err != nil {
			return fmt.Errorf("save marketplace stock preview line: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_runs SET status='success',total_count=$2,changed_count=0,blocked_count=0,error_count=0,
		summary=$3::jsonb,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, result.RunID, len(result.Lines), summary); err != nil {
		return err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_pools SET status=CASE WHEN status='draft' THEN 'ready' ELSE status END,
		dry_run_required=false,last_preview_at=NOW(),last_error='',updated_at=NOW()
		WHERE id=$1::uuid AND config_version=$2`, poolID, configVersion)
	if err != nil {
		return err
	}
	if affected, err := updated.RowsAffected(); err != nil || affected != 1 {
		return ErrConfigVersionConflict
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_preview_completed", poolID, userID, map[string]any{
		"run_id": result.RunID, "line_count": len(result.Lines), "write_ready": false,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) FailPreview(ctx context.Context, runID, message string) error {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var poolID, userID string
	err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_runs SET status='failed',error_count=1,error_message=$2,finished_at=NOW(),updated_at=NOW()
		WHERE id=$1::uuid AND status='running' RETURNING pool_id::text,COALESCE(requested_by::text,'')`, runID, message).Scan(&poolID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_pools SET status='paused',auto_enabled=false,dry_run_required=true,
		paused_reason='preview_failed',last_error=$2,updated_at=NOW() WHERE id=$1::uuid`, poolID, message); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_preview_failed", poolID, userID, map[string]any{"run_id": runID}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) UpdateAuto(ctx context.Context, poolID string, input AutoUpdate, userID string) (*Pool, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var pool Pool
	if input.Enabled {
		err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_pools
			SET auto_enabled=true,status='active',paused_reason='',config_version=config_version+1,updated_by=$2::uuid,updated_at=NOW()
			WHERE id=$1::uuid AND config_version=$3 AND status='ready' AND dry_run_required=false
			AND kill_switch_enabled=false AND last_success_at IS NOT NULL
			RETURNING id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
			shared_risk_acknowledged,status,auto_enabled,kill_switch_enabled,schedule_interval_seconds,dry_run_required,paused_reason,config_version,
			last_preview_at,last_success_at,last_schedule_at,last_error,updated_at`, poolID, userID, input.ExpectedConfigVersion).Scan(poolScanner(&pool)...)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_pools
			SET auto_enabled=false,status='paused',paused_reason='auto_disabled_by_admin',config_version=config_version+1,updated_by=$2::uuid,updated_at=NOW()
			WHERE id=$1::uuid AND config_version=$3
			RETURNING id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
			shared_risk_acknowledged,status,auto_enabled,kill_switch_enabled,schedule_interval_seconds,dry_run_required,paused_reason,config_version,
			last_preview_at,last_success_at,last_schedule_at,last_error,updated_at`, poolID, userID, input.ExpectedConfigVersion).Scan(poolScanner(&pool)...)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("update marketplace stock auto: %w", err)
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_auto_"+map[bool]string{true: "enabled", false: "disabled"}[input.Enabled], poolID, userID, map[string]any{"config_version": pool.ConfigVersion}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &pool, nil
}

func (s *PostgresStore) QueueSync(ctx context.Context, poolID string, input SyncRequest, userID string) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	var dryRunRequired, globalKill, poolKill bool
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT p.status,p.dry_run_required,s.kill_switch_enabled,p.kill_switch_enabled,p.config_version
		FROM marketplace_stock_pools p JOIN marketplace_stock_settings s ON s.singleton=true
		WHERE p.id=$1::uuid FOR UPDATE`, poolID).Scan(&status, &dryRunRequired, &globalKill, &poolKill, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, err
	}
	if globalKill {
		return nil, ErrGlobalKillSwitch
	}
	if poolKill || status == "paused" {
		return nil, ErrPoolPaused
	}
	if version != input.ExpectedConfigVersion {
		return nil, ErrConfigVersionConflict
	}
	if dryRunRequired || (status != "ready" && status != "active") {
		return nil, ErrDryRunRequired
	}
	var run Run
	err = tx.QueryRowContext(ctx, `INSERT INTO marketplace_stock_runs(pool_id,trigger_source,run_type,status,config_version,requested_by)
		VALUES($1::uuid,'manual','sync','queued',$2,$3::uuid)
		ON CONFLICT (pool_id) WHERE status IN ('queued','running') DO UPDATE SET updated_at=marketplace_stock_runs.updated_at
		RETURNING id::text,pool_id::text,trigger_source,run_type,status,config_version,total_count,changed_count,blocked_count,error_count,error_message,plan_expires_at,started_at,finished_at`, poolID, version, userID).Scan(runScanner(&run)...)
	if err != nil {
		return nil, fmt.Errorf("queue marketplace stock sync: %w", err)
	}
	if err := insertAudit(ctx, tx, "marketplace_stock_sync_queued", poolID, userID, map[string]any{"run_id": run.ID, "config_version": version}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *PostgresStore) Run(ctx context.Context, runID string) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	var run Run
	err := s.db.QueryRowContext(ctx, `SELECT id::text,pool_id::text,trigger_source,run_type,status,config_version,total_count,changed_count,blocked_count,error_count,error_message,plan_expires_at,started_at,finished_at FROM marketplace_stock_runs WHERE id=$1::uuid`, runID).Scan(runScanner(&run)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func insertPool(ctx context.Context, tx *sql.Tx, input PoolInput, userID string) (*Pool, error) {
	pool := &Pool{}
	err := tx.QueryRowContext(ctx, `INSERT INTO marketplace_stock_pools
		(sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override,shared_risk_acknowledged,created_by,updated_by)
		VALUES($1,$2,$3,$4,$5,$6,$6)
		RETURNING id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
		shared_risk_acknowledged,status,auto_enabled,kill_switch_enabled,schedule_interval_seconds,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_schedule_at,last_error,updated_at`,
		input.SMLItemCode, input.SMLUnitCode, input.AllocationMode, input.BufferPctOverride, input.SharedRiskAcknowledged, userID,
	).Scan(poolScanner(pool)...)
	if err != nil {
		return nil, fmt.Errorf("create marketplace stock pool: %w", err)
	}
	if err := upsertPoolMembers(ctx, tx, pool.ID, input.Members); err != nil {
		return nil, err
	}
	members, err := listPoolMembersTx(ctx, tx, pool.ID)
	if err != nil {
		return nil, err
	}
	pool.Members = members
	return pool, nil
}

func (s *PostgresStore) listMembers(ctx context.Context) (map[string][]Member, error) {
	rows, err := s.db.QueryContext(ctx, memberSelect+` ORDER BY pool_id,source,product_name,variant_name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Member{}
	for rows.Next() {
		var poolID string
		member, err := scanMember(rows, &poolID)
		if err != nil {
			return nil, err
		}
		out[poolID] = append(out[poolID], member)
	}
	return out, rows.Err()
}

const memberSelect = `SELECT pool_id::text,id::text,source,account_key,external_product_id,external_sku_id,external_warehouse_id,
	COALESCE(marketplace_alias_id::text,''),product_name,variant_name,unit_factor::float8,allocation_pct::float8,
	enabled,last_catalog_seen_at,last_target_qty,last_actual_qty,last_error
	FROM marketplace_stock_pool_members`

func listPoolMembersTx(ctx context.Context, tx *sql.Tx, poolID string) ([]Member, error) {
	rows, err := tx.QueryContext(ctx, memberSelect+` WHERE pool_id=$1::uuid ORDER BY source,product_name,variant_name,id`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var readPoolID string
		member, err := scanMember(rows, &readPoolID)
		if err != nil {
			return nil, err
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

func upsertPoolMembers(ctx context.Context, tx *sql.Tx, poolID string, inputs []MemberInput) error {
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_pool_members SET enabled=false,updated_at=NOW() WHERE pool_id=$1::uuid`, poolID); err != nil {
		return err
	}
	for _, input := range inputs {
		var aliasID any
		if strings.TrimSpace(input.AliasID) != "" {
			aliasID = input.AliasID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_pool_members
			(pool_id,source,account_key,external_product_id,external_sku_id,external_warehouse_id,marketplace_alias_id,product_name,variant_name,unit_factor,allocation_pct,enabled)
			VALUES($1::uuid,$2,$3,$4,$5,$6,$7::uuid,$8,$9,$10,$11,$12)
			ON CONFLICT (source,account_key,external_product_id,external_sku_id) DO UPDATE
			SET pool_id=EXCLUDED.pool_id,external_warehouse_id=EXCLUDED.external_warehouse_id,marketplace_alias_id=EXCLUDED.marketplace_alias_id,product_name=EXCLUDED.product_name,
			variant_name=EXCLUDED.variant_name,unit_factor=EXCLUDED.unit_factor,allocation_pct=EXCLUDED.allocation_pct,
			enabled=EXCLUDED.enabled,last_error='',updated_at=NOW()`,
			poolID, input.Source, input.AccountKey, input.ExternalProductID, input.ExternalSKUID, input.ExternalWarehouseID, aliasID,
			input.ProductName, input.VariantName, input.UnitFactor, input.AllocationPct, input.Enabled,
		)
		if err != nil {
			return fmt.Errorf("save marketplace stock pool member: %w", err)
		}
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanPool(row rowScanner) (Pool, error) {
	pool := Pool{Members: []Member{}}
	err := row.Scan(poolScanner(&pool)...)
	return pool, err
}

func poolScanner(pool *Pool) []any {
	return []any{&pool.ID, &pool.SMLItemCode, &pool.SMLUnitCode, &pool.AllocationMode, &pool.BufferPctOverride,
		&pool.SharedRiskAcknowledged, &pool.Status, &pool.AutoEnabled, &pool.KillSwitch, &pool.ScheduleIntervalSeconds, &pool.DryRunRequired, &pool.PausedReason,
		&pool.ConfigVersion, &pool.LastPreviewAt, &pool.LastSuccessAt, &pool.LastScheduleAt, &pool.LastError, &pool.UpdatedAt}
}

func scanMember(row rowScanner, poolID *string) (Member, error) {
	member := Member{}
	err := row.Scan(poolID, &member.ID, &member.Source, &member.AccountKey, &member.ExternalProductID, &member.ExternalSKUID, &member.ExternalWarehouseID,
		&member.AliasID, &member.ProductName, &member.VariantName, &member.UnitFactor, &member.AllocationPct,
		&member.Enabled, &member.LastCatalogSeenAt, &member.LastTargetQty, &member.LastActualQty, &member.LastError)
	return member, err
}

func runScanner(run *Run) []any {
	return []any{&run.ID, &run.PoolID, &run.TriggerSource, &run.RunType, &run.Status, &run.ConfigVersion,
		&run.TotalCount, &run.ChangedCount, &run.BlockedCount, &run.ErrorCount, &run.ErrorMessage,
		&run.PlanExpiresAt, &run.StartedAt, &run.FinishedAt}
}

func insertAudit(ctx context.Context, tx *sql.Tx, action, targetID, userID string, detail map[string]any) error {
	payload, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(action,target_id,user_id,source,level,detail)
		VALUES($1,NULLIF($2,''),NULLIF($3,'')::uuid,'marketplace_stock','info',$4::jsonb)`, action, targetID, userID, payload)
	return err
}
