package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

var (
	ErrCatalogGenerationLeaseBusy = errors.New("catalog generation sync is already running")
	ErrCatalogGenerationLeaseLost = errors.New("catalog generation lease was lost")
	ErrCatalogGenerationInvalid   = errors.New("catalog generation validation failed")
)

type CatalogGenerationPage struct {
	Products      []CatalogGenerationProduct
	Units         []CatalogGenerationUnit
	Barcodes      []CatalogGenerationBarcode
	SetComponents []CatalogGenerationSetComponent
	Cursor        string
	ProductCount  int64
	UnitCount     int64
	BarcodeCount  int64
	SetCount      int64
	ProductHash   string
	UnitHash      string
}

func leaseInterval(duration time.Duration) string {
	return fmt.Sprintf("%.3f seconds", duration.Seconds())
}

func (r *SMLCatalogRepo) AcquireCatalogGenerationLease(ctx context.Context, owner string, duration time.Duration) (int64, error) {
	var fence int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sml_catalog_sync_lease(singleton, owner_id, fencing_token, lease_until, heartbeat_at, updated_at)
		VALUES (true, $1, 1, NOW() + $2::interval, NOW(), NOW())
		ON CONFLICT (singleton) DO UPDATE SET
		  owner_id = EXCLUDED.owner_id,
		  fencing_token = sml_catalog_sync_lease.fencing_token + 1,
		  lease_until = EXCLUDED.lease_until,
		  heartbeat_at = NOW(),
		  updated_at = NOW()
		WHERE sml_catalog_sync_lease.lease_until <= NOW()
		   OR sml_catalog_sync_lease.owner_id = EXCLUDED.owner_id
		RETURNING fencing_token
	`, owner, leaseInterval(duration)).Scan(&fence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCatalogGenerationLeaseBusy
	}
	if err != nil {
		return 0, fmt.Errorf("acquire catalog generation lease: %w", err)
	}
	return fence, nil
}

func (r *SMLCatalogRepo) RenewCatalogGenerationLease(ctx context.Context, owner string, fence int64, duration time.Duration) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE sml_catalog_sync_lease
		SET lease_until = NOW() + $3::interval, heartbeat_at = NOW(), updated_at = NOW()
		WHERE singleton = true AND owner_id = $1 AND fencing_token = $2 AND lease_until > NOW()
	`, owner, fence, leaseInterval(duration))
	if err != nil {
		return fmt.Errorf("renew catalog generation lease: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("renew catalog generation lease rows: %w", err)
	}
	if updated != 1 {
		return ErrCatalogGenerationLeaseLost
	}
	return nil
}

func (r *SMLCatalogRepo) ReleaseCatalogGenerationLease(ctx context.Context, owner string, fence int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE sml_catalog_sync_lease
		SET lease_until = NOW(), heartbeat_at = NOW(), updated_at = NOW()
		WHERE singleton = true AND owner_id = $1 AND fencing_token = $2
	`, owner, fence)
	return err
}

func (r *SMLCatalogRepo) BeginCatalogGeneration(ctx context.Context, owner string, fence int64, startedAt time.Time) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sml_catalog_sync_runs(
		  source_watermark, status, lease_owner, lease_fence, lease_until, sync_started_at
		)
		SELECT $1, 'staging', $2, $3, lease_until, $4
		FROM sml_catalog_sync_lease
		WHERE singleton = true AND owner_id = $2 AND fencing_token = $3 AND lease_until > NOW()
		RETURNING id::text
	`, startedAt.UTC().Format(time.RFC3339Nano), owner, fence, startedAt.UTC()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCatalogGenerationLeaseLost
	}
	if err != nil {
		return "", fmt.Errorf("begin catalog generation: %w", err)
	}
	return id, nil
}

func (r *SMLCatalogRepo) StageCatalogGenerationPage(ctx context.Context, generationID, owner string, fence int64, page CatalogGenerationPage) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := copyGenerationProducts(ctx, tx, generationID, page.Products); err != nil {
		return err
	}
	if err := copyGenerationUnits(ctx, tx, "sml_catalog_unit_staging", generationID, page.Units); err != nil {
		return err
	}
	if err := copyGenerationUnits(ctx, tx, "sml_catalog_units", generationID, page.Units); err != nil {
		return err
	}
	if err := copyGenerationBarcodes(ctx, tx, "sml_catalog_barcode_staging", generationID, page.Barcodes); err != nil {
		return err
	}
	if err := copyGenerationBarcodes(ctx, tx, "sml_catalog_barcodes", generationID, page.Barcodes); err != nil {
		return err
	}
	if err := copyGenerationSetComponents(ctx, tx, generationID, page.SetComponents); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE sml_catalog_sync_runs run SET
		  source_cursor=$4, product_count=$5, unit_count=$6, barcode_count=$7,
		  set_component_count=$8, product_hash=$9, unit_hash=$10,
		  lease_until=lease.lease_until
		FROM sml_catalog_sync_lease lease
		WHERE run.id=$1::uuid AND run.status='staging'
		  AND lease.singleton=true AND lease.owner_id=$2 AND lease.fencing_token=$3
		  AND lease.lease_until > NOW()
	`, generationID, owner, fence, page.Cursor, page.ProductCount, page.UnitCount,
		page.BarcodeCount, page.SetCount, page.ProductHash, page.UnitHash)
	if err != nil {
		return fmt.Errorf("update catalog generation progress: %w", err)
	}
	updated, _ := result.RowsAffected()
	if updated != 1 {
		return ErrCatalogGenerationLeaseLost
	}
	return tx.Commit()
}

func (r *SMLCatalogRepo) ActivateCatalogGeneration(ctx context.Context, generationID, owner string, fence int64, productSyncStartedAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var generation, productCount, unitCount int64
	var productHash, unitHash string
	err = tx.QueryRowContext(ctx, `
		SELECT generation, product_count, unit_count, product_hash, unit_hash
		FROM sml_catalog_sync_runs
		WHERE id=$1::uuid AND status='staging'
		FOR UPDATE
	`, generationID).Scan(&generation, &productCount, &unitCount, &productHash, &unitHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCatalogGenerationInvalid
	}
	if err != nil {
		return err
	}
	if productCount < 1 || unitCount < productCount || productHash == "" || unitHash == "" {
		return ErrCatalogGenerationInvalid
	}

	var leaseValid bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM sml_catalog_sync_lease
		  WHERE singleton=true AND owner_id=$1 AND fencing_token=$2 AND lease_until > NOW()
		)
	`, owner, fence).Scan(&leaseValid)
	if err != nil {
		return err
	}
	if !leaseValid {
		return ErrCatalogGenerationLeaseLost
	}

	var stagedProducts, stagedUnits int64
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM sml_catalog_product_staging WHERE generation_id=$1::uuid),
		(SELECT COUNT(*) FROM sml_catalog_unit_staging WHERE generation_id=$1::uuid)
	`, generationID).Scan(&stagedProducts, &stagedUnits); err != nil {
		return err
	}
	if stagedProducts != productCount || stagedUnits != unitCount {
		return ErrCatalogGenerationInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sml_catalog
		SET is_active=false, missing_at=COALESCE(missing_at,NOW()), synced_at=NOW()
		WHERE is_active=true AND (last_seen_at IS NULL OR last_seen_at < $1)`, productSyncStartedAt.UTC()); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sml_catalog_sync_runs
		SET status='superseded', finished_at=COALESCE(finished_at,NOW())
		WHERE status='active' AND id <> $1::uuid`, generationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sml_catalog_sync_runs
		SET status='active', activated_at=NOW(), finished_at=NOW(), lease_until=NULL
		WHERE id=$1::uuid`, generationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_conversion_readiness(
		singleton, catalog_generation_ready, mapping_backfill_ready, reservation_ledger_ready, updated_at
	) VALUES (true,true,false,false,NOW())
	ON CONFLICT (singleton) DO UPDATE SET catalog_generation_ready=true, updated_at=NOW()`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_mapping_jobs(
		tenant_key,idempotency_key,target_revision,job_type,status,dependency_snapshot
	) VALUES ('',$1,$2,'unit_stale_reconcile','queued',jsonb_build_object('catalog_generation_id',$3))
	ON CONFLICT (idempotency_key) DO NOTHING`,
		"unit-generation:"+generationID, generation, generationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SMLCatalogRepo) FailCatalogGeneration(ctx context.Context, generationID, message string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sml_catalog_sync_runs
		SET status='failed', error_message=$2, finished_at=NOW(), lease_until=NULL
		WHERE id=$1::uuid AND status IN ('staging','validating')`, generationID, message)
	return err
}

func copyGenerationProducts(ctx context.Context, tx *sql.Tx, generationID string, products []CatalogGenerationProduct) error {
	rows := make([][]any, 0, len(products))
	for _, item := range products {
		var updated any
		if !item.SourceUpdatedAt.IsZero() {
			updated = item.SourceUpdatedAt.UTC()
		}
		rows = append(rows, []any{generationID, item.ItemCode, item.ItemName, "", item.StandardUnit, "", item.ItemType, updated, ""})
	}
	return copyRows(ctx, tx, "sml_catalog_product_staging",
		[]string{"generation_id", "item_code", "item_name", "item_name2", "unit_code", "group_code", "item_type", "source_updated_at", "payload_hash"}, rows)
}

func copyGenerationUnits(ctx context.Context, tx *sql.Tx, table, generationID string, units []CatalogGenerationUnit) error {
	rows := make([][]any, 0, len(units))
	for _, unit := range units {
		if table == "sml_catalog_units" {
			rows = append(rows, []any{generationID, unit.ItemCode, unit.UnitCode, unit.UnitName,
				unit.StandValue, unit.DivideValue, unit.IsDefault, unit.UnitOrder, true})
		} else {
			rows = append(rows, []any{generationID, unit.ItemCode, unit.UnitCode, unit.UnitName,
				unit.StandValue, unit.DivideValue, unit.IsDefault, unit.UnitOrder, nil})
		}
	}
	if table == "sml_catalog_units" {
		return copyRows(ctx, tx, table,
			[]string{"generation_id", "item_code", "unit_code", "unit_name", "stand_value", "divide_value", "is_default", "unit_order", "is_active"}, rows)
	}
	return copyRows(ctx, tx, table,
		[]string{"generation_id", "item_code", "unit_code", "unit_name", "stand_value", "divide_value", "is_default", "unit_order", "source_updated_at"}, rows)
}

func copyGenerationBarcodes(ctx context.Context, tx *sql.Tx, table, generationID string, barcodes []CatalogGenerationBarcode) error {
	rows := make([][]any, 0, len(barcodes))
	for _, barcode := range barcodes {
		if table == "sml_catalog_barcodes" {
			rows = append(rows, []any{generationID, barcode.ItemCode, barcode.UnitCode, barcode.Barcode, true})
		} else {
			rows = append(rows, []any{generationID, barcode.ItemCode, barcode.UnitCode, barcode.Barcode})
		}
	}
	if table == "sml_catalog_barcodes" {
		return copyRows(ctx, tx, table, []string{"generation_id", "item_code", "unit_code", "barcode", "is_active"}, rows)
	}
	return copyRows(ctx, tx, table, []string{"generation_id", "item_code", "unit_code", "barcode"}, rows)
}

func copyGenerationSetComponents(ctx context.Context, tx *sql.Tx, generationID string, components []CatalogGenerationSetComponent) error {
	rows := make([][]any, 0, len(components))
	for _, component := range components {
		rows = append(rows, []any{generationID, component.ParentItemCode, component.LineNumber,
			component.RowOrder, component.ItemCode, component.ItemName, component.UnitCode,
			component.Qty, component.UnitFactor, component.DefinitionHash})
	}
	return copyRows(ctx, tx, "sml_catalog_set_component_staging",
		[]string{"generation_id", "parent_item_code", "line_number", "row_order", "component_item_code",
			"component_item_name", "unit_code", "qty", "unit_factor", "definition_hash"}, rows)
}

func copyRows(ctx context.Context, tx *sql.Tx, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, pq.CopyIn(table, columns...))
	if err != nil {
		return fmt.Errorf("prepare COPY %s: %w", table, err)
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row...); err != nil {
			return fmt.Errorf("COPY %s row: %w", table, err)
		}
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("flush COPY %s: %w", table, err)
	}
	return stmt.Close()
}
