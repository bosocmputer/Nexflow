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
