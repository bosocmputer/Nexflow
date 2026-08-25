package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAcquireCatalogGenerationLeaseReturnsFencingToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("INSERT INTO sml_catalog_sync_lease").
		WithArgs("worker-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"fencing_token"}).AddRow(int64(7)))

	fence, err := NewSMLCatalogRepo(db).AcquireCatalogGenerationLease(t.Context(), "worker-1", 2*time.Minute)
	if err != nil {
		t.Fatalf("AcquireCatalogGenerationLease: %v", err)
	}
	if fence != 7 {
		t.Fatalf("fence = %d, want 7", fence)
	}
}

func TestAcquireCatalogGenerationLeaseReportsBusy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("INSERT INTO sml_catalog_sync_lease").
		WithArgs("worker-2", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	_, err = NewSMLCatalogRepo(db).AcquireCatalogGenerationLease(t.Context(), "worker-2", 2*time.Minute)
	if !errors.Is(err, ErrCatalogGenerationLeaseBusy) {
		t.Fatalf("error = %v, want ErrCatalogGenerationLeaseBusy", err)
	}
}

func TestRenewCatalogGenerationLeaseFailsWhenFenceIsLost(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("UPDATE sml_catalog_sync_lease").
		WithArgs("worker-1", int64(7), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = NewSMLCatalogRepo(db).RenewCatalogGenerationLease(t.Context(), "worker-1", 7, 2*time.Minute)
	if !errors.Is(err, ErrCatalogGenerationLeaseLost) {
		t.Fatalf("error = %v, want ErrCatalogGenerationLeaseLost", err)
	}
}

func TestActivateCatalogGenerationCastsAuditJSONParameters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	generationID := "00000000-0000-0000-0000-000000000085"
	startedAt := time.Date(2026, time.August, 25, 4, 50, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT generation, product_count, unit_count, product_hash, unit_hash`).
		WithArgs(generationID).
		WillReturnRows(sqlmock.NewRows([]string{"generation", "product_count", "unit_count", "product_hash", "unit_hash"}).
			AddRow(int64(1), int64(2), int64(3), "product-hash", "unit-hash"))
	mock.ExpectQuery(`SELECT EXISTS\(`).
		WithArgs("worker-demo", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT\s+\(SELECT COUNT\(\*\) FROM sml_catalog_product_staging`).
		WithArgs(generationID).
		WillReturnRows(sqlmock.NewRows([]string{"product_count", "unit_count"}).AddRow(int64(2), int64(3)))
	mock.ExpectQuery(`WITH staged_sets AS`).
		WithArgs(generationID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectExec(`INSERT INTO sml_catalog\(`).
		WithArgs(generationID, startedAt).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE sml_catalog\s+SET is_active=false`).
		WithArgs(startedAt).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id::text FROM sml_catalog_sync_runs`).
		WithArgs(generationID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`UPDATE sml_catalog_sync_runs\s+SET status='superseded'`).
		WithArgs(generationID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE sml_catalog_sync_runs\s+SET status='active'`).
		WithArgs(generationID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO marketplace_conversion_readiness`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, jobType := range []string{"alias_conversion", "bill_snapshots", "reservation_ledger"} {
		mock.ExpectExec(`INSERT INTO marketplace_backfill_jobs`).
			WithArgs("demo", jobType, "unit-generation:"+generationID+":"+jobType).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec(`UPDATE shopee_stock_settings SET enabled=false`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`jsonb_build_object\('generation_id',\$4::text\).*`+
		`jsonb_build_object\('product_count',\$5::bigint,'unit_count',\$6::bigint,`+
		`\s*'product_hash',\$7::text,'unit_hash',\$8::text\)`).
		WithArgs(generationID, "demo", int64(1), "", int64(2), int64(3), "product-hash", "unit-hash").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = NewSMLCatalogRepo(db).WithTenantKey("demo").ActivateCatalogGeneration(
		t.Context(), generationID, "worker-demo", 7, startedAt,
	)
	if err != nil {
		t.Fatalf("ActivateCatalogGeneration: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
