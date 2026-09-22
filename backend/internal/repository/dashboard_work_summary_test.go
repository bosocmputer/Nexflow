package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDashboardWorkSummaryUsesOnlyLocalQueueCounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM shopee_order_snapshots s`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.tiktok_shop_order_snapshots").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)FROM tiktok_shop_order_snapshots s.*tiktok_shop_connections`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(`(?s)FROM bills.*bill_type='sale'`).
		WillReturnRows(sqlmock.NewRows([]string{"needs_review", "pending", "failed"}).AddRow(4, 5, 6))
	mock.ExpectQuery(`(?s)FROM sml_bulk_jobs WHERE status IN \('queued','running'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	mock.ExpectQuery(`SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.marketplace_stock_pools").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)FROM marketplace_stock_pools.*archived_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"needs_dry_run", "paused", "auto_enabled"}).AddRow(8, 9, 10))

	summary, err := NewBillRepo(db).DashboardWorkSummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Documents.Shopee != 2 || summary.Documents.TikTok != 3 {
		t.Fatalf("documents=%+v", summary.Documents)
	}
	if summary.SML.NeedsReview != 4 || summary.SML.ReadyToSend != 5 || summary.SML.Failed != 6 || summary.SML.ActiveBulkJobs != 7 {
		t.Fatalf("sml=%+v", summary.SML)
	}
	if summary.Stock.NeedsDryRun != 8 || summary.Stock.Paused != 9 || summary.Stock.AutoEnabled != 10 {
		t.Fatalf("stock=%+v", summary.Stock)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardWorkSummarySkipsOptionalSchemasThatDoNotExist(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM shopee_order_snapshots s`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.tiktok_shop_order_snapshots").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`(?s)FROM bills.*bill_type='sale'`).
		WillReturnRows(sqlmock.NewRows([]string{"needs_review", "pending", "failed"}).AddRow(0, 1, 0))
	mock.ExpectQuery(`(?s)FROM sml_bulk_jobs WHERE status IN \('queued','running'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.marketplace_stock_pools").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	summary, err := NewBillRepo(db).DashboardWorkSummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Documents.TikTok != 0 || summary.Stock != (DashboardStockWork{}) {
		t.Fatalf("optional schemas should be empty: %+v", summary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
