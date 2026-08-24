package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBeginCatalogReconciliationPausesStockBeforeMetadataMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO marketplace_conversion_readiness`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE shopee_stock_settings.*enabled=false.*catalog_sync_in_progress.*config_version=config_version\+1`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO audit_logs`).WithArgs("").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewSMLCatalogRepo(db).BeginCatalogReconciliation(context.Background()); err != nil {
		t.Fatalf("BeginCatalogReconciliation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
