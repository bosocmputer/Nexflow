package database

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWithMigrationLockUsesOneDedicatedConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("SELECT pg_advisory_lock").WithArgs(migrationAdvisoryLockKey).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT 1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock").WithArgs(migrationAdvisoryLockKey).WillReturnResult(sqlmock.NewResult(0, 1))

	err = withMigrationLock(db, func(conn *sql.Conn) error {
		_, execErr := conn.ExecContext(t.Context(), "SELECT 1")
		return execErr
	})
	if err != nil {
		t.Fatalf("withMigrationLock: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestWithMigrationLockUnlocksWhenMigrationFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("SELECT pg_advisory_lock").WithArgs(migrationAdvisoryLockKey).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock").WithArgs(migrationAdvisoryLockKey).WillReturnResult(sqlmock.NewResult(0, 1))

	want := errors.New("migration failed")
	err = withMigrationLock(db, func(_ *sql.Conn) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("withMigrationLock error = %v, want %v", err, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestMigration085IsAdditiveAndSchemaOnly(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/085_marketplace_units_conversion.sql")
	if err != nil {
		t.Fatalf("read migration 085: %v", err)
	}
	sqlText := string(data)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS sml_catalog_sync_runs",
		"CREATE TABLE IF NOT EXISTS sml_catalog_sync_lease",
		"CREATE TABLE IF NOT EXISTS sml_catalog_units",
		"CREATE TABLE IF NOT EXISTS marketplace_mapping_jobs",
		"CREATE TABLE IF NOT EXISTS marketplace_backfill_jobs",
		"CREATE TABLE IF NOT EXISTS marketplace_stock_reservations",
		"CREATE TABLE IF NOT EXISTS marketplace_stock_reservation_components",
		"CREATE TABLE IF NOT EXISTS marketplace_stock_demand_versions",
		"CREATE TABLE IF NOT EXISTS bill_sml_attempts",
		"CONSTRAINT marketplace_stock_reservations_identity_check CHECK",
		"CONSTRAINT marketplace_stock_reservations_source_qty_check CHECK (source_qty > 0)",
		"CONSTRAINT marketplace_stock_reservations_multiplier_check CHECK (quantity_multiplier BETWEEN 1 AND 1000000)",
		"CONSTRAINT marketplace_stock_reservations_unit_ratio_check CHECK",
		"CONSTRAINT marketplace_stock_reservations_base_qty_check CHECK (base_qty IS NULL OR base_qty > 0)",
		"CONSTRAINT marketplace_stock_reservations_demand_revision_check CHECK (demand_revision >= 1)",
		"CONSTRAINT marketplace_stock_reservations_pending_proof_check CHECK",
		"component_base_qty NUMERIC NOT NULL CHECK (component_base_qty > 0)",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("migration 085 missing %q", required)
		}
	}

	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(trimmed, "UPDATE ") || strings.HasPrefix(trimmed, "DELETE ") {
			t.Errorf("migration 085 must not perform startup backfill: %q", line)
		}
	}

	if count := strings.Count(sqlText, "external_request_started_at TIMESTAMPTZ"); count != 1 {
		t.Fatalf("migration 085 declares bill_sml_attempts.external_request_started_at %d times, want exactly once", count)
	}
}

func TestMigration087IsAdditiveAndSchemaOnly(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/087_shopee_stock_outstanding_sales_orders.sql")
	if err != nil {
		t.Fatalf("read migration 087: %v", err)
	}
	sqlText := string(data)
	for _, required := range []string{
		"last_preview_sml_physical_qty NUMERIC",
		"sml_outstanding_so_qty NUMERIC",
		"CREATE TABLE IF NOT EXISTS marketplace_stock_representation_evidence",
		"reservation_id UUID NOT NULL REFERENCES marketplace_stock_reservations(id) ON DELETE RESTRICT",
		"expected_base_qty NUMERIC NOT NULL CHECK (expected_base_qty > 0)",
		"UNIQUE (reservation_id, sml_attempt_id, warehouse_code, location_code, item_code, evidence_kind)",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("migration 087 missing %q", required)
		}
	}
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(trimmed, "UPDATE ") || strings.HasPrefix(trimmed, "DELETE ") {
			t.Errorf("migration 087 must not perform startup backfill: %q", line)
		}
	}
}

func TestCheckMarketplaceActivationFailsClosedWhenReadinessIsIncomplete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT catalog_generation_ready").
		WillReturnRows(sqlmock.NewRows([]string{"catalog_generation_ready", "mapping_backfill_ready", "reservation_ledger_ready"}).
			AddRow(true, false, true))

	err = CheckMarketplaceActivation(t.Context(), db, "active", true, true)
	if err == nil || !strings.Contains(err.Error(), "mapping backfill") {
		t.Fatalf("CheckMarketplaceActivation error = %v, want mapping backfill readiness error", err)
	}
}

func TestCheckMarketplaceActivationAllowsReadyTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT catalog_generation_ready").
		WillReturnRows(sqlmock.NewRows([]string{"catalog_generation_ready", "mapping_backfill_ready", "reservation_ledger_ready"}).
			AddRow(true, true, true))

	if err := CheckMarketplaceActivation(t.Context(), db, "active", true, true); err != nil {
		t.Fatalf("CheckMarketplaceActivation: %v", err)
	}
}

func TestCheckMarketplaceActivationRequiresFeatureDependencies(t *testing.T) {
	if err := CheckMarketplaceActivation(t.Context(), nil, "active", false, true); err == nil {
		t.Fatal("expected active mode without unit catalog to fail")
	}
	if err := CheckMarketplaceActivation(t.Context(), nil, "active", true, false); err == nil {
		t.Fatal("expected active mode without reservation ledger to fail")
	}
	if err := CheckMarketplaceActivation(t.Context(), nil, "shadow", false, false); err != nil {
		t.Fatalf("shadow mode should not require readiness: %v", err)
	}
}
