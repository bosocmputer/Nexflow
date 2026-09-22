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
		shared_risk_acknowledged,status,auto_enabled,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_error,updated_at
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
		shared_risk_acknowledged,status,auto_enabled,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_error,updated_at`,
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

func insertPool(ctx context.Context, tx *sql.Tx, input PoolInput, userID string) (*Pool, error) {
	pool := &Pool{}
	err := tx.QueryRowContext(ctx, `INSERT INTO marketplace_stock_pools
		(sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override,shared_risk_acknowledged,created_by,updated_by)
		VALUES($1,$2,$3,$4,$5,$6,$6)
		RETURNING id::text,sml_item_code,sml_unit_code,allocation_mode,buffer_pct_override::float8,
		shared_risk_acknowledged,status,auto_enabled,dry_run_required,paused_reason,config_version,
		last_preview_at,last_success_at,last_error,updated_at`,
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

const memberSelect = `SELECT pool_id::text,id::text,source,account_key,external_product_id,external_sku_id,
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
			(pool_id,source,account_key,external_product_id,external_sku_id,marketplace_alias_id,product_name,variant_name,unit_factor,allocation_pct,enabled)
			VALUES($1::uuid,$2,$3,$4,$5,$6::uuid,$7,$8,$9,$10,$11)
			ON CONFLICT (source,account_key,external_product_id,external_sku_id) DO UPDATE
			SET pool_id=EXCLUDED.pool_id,marketplace_alias_id=EXCLUDED.marketplace_alias_id,product_name=EXCLUDED.product_name,
			variant_name=EXCLUDED.variant_name,unit_factor=EXCLUDED.unit_factor,allocation_pct=EXCLUDED.allocation_pct,
			enabled=EXCLUDED.enabled,last_error='',updated_at=NOW()`,
			poolID, input.Source, input.AccountKey, input.ExternalProductID, input.ExternalSKUID, aliasID,
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
		&pool.SharedRiskAcknowledged, &pool.Status, &pool.AutoEnabled, &pool.DryRunRequired, &pool.PausedReason,
		&pool.ConfigVersion, &pool.LastPreviewAt, &pool.LastSuccessAt, &pool.LastError, &pool.UpdatedAt}
}

func scanMember(row rowScanner, poolID *string) (Member, error) {
	member := Member{}
	err := row.Scan(poolID, &member.ID, &member.Source, &member.AccountKey, &member.ExternalProductID, &member.ExternalSKUID,
		&member.AliasID, &member.ProductName, &member.VariantName, &member.UnitFactor, &member.AllocationPct,
		&member.Enabled, &member.LastCatalogSeenAt, &member.LastTargetQty, &member.LastActualQty, &member.LastError)
	return member, err
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
