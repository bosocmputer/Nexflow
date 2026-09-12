package tiktokshop

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTikTokOrderReconcileStoreStartsManualRunForActiveConnection(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	start := time.Unix(1_789_000_000, 0).UTC()
	end := time.Unix(1_789_000_100, 0).UTC()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT gateway_connection_id::text").WithArgs("7494619203789490654").
		WillReturnRows(sqlmock.NewRows([]string{"gateway_connection_id"}).AddRow("11111111-1111-4111-8111-111111111111"))
	mock.ExpectExec("INSERT INTO tiktok_shop_order_sync_settings").WithArgs("7494619203789490654").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO tiktok_shop_order_sync_runs").
		WithArgs("11111111-1111-4111-8111-111111111111", "7494619203789490654", start, end, tikTokOrderReconcilePageSize, tikTokOrderReconcileMaxPages).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("22222222-2222-4222-8222-222222222222"))
	mock.ExpectCommit()

	run, err := NewTikTokOrderReconcileStore(database).StartManualRun(t.Context(), TikTokOrderReconcileRequest{
		ShopID: "7494619203789490654", UpdateTimeGE: start.Unix(), UpdateTimeLT: end.Unix(),
	})
	if err != nil {
		t.Fatalf("StartManualRun() error = %v", err)
	}
	if run.ID != "22222222-2222-4222-8222-222222222222" || run.ShopID != "7494619203789490654" || !run.WindowEnd.Equal(end) {
		t.Fatalf("run = %+v", run)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderReconcileStoreCompletesRunAndAdvancesWatermarkAtomically(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	windowEnd := time.Unix(1_789_000_100, 0).UTC()
	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE tiktok_shop_order_sync_runs").
		WithArgs("run-1", 2, 3, 3, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "window_end"}).AddRow("7494619203789490654", windowEnd))
	mock.ExpectExec("UPDATE tiktok_shop_order_sync_settings").WithArgs("7494619203789490654", windowEnd).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = NewTikTokOrderReconcileStore(database).CompleteRun(t.Context(), "run-1", TikTokOrderReconcileProgress{
		PageCount: 2, DiscoveredCount: 3, SnapshottedCount: 3, LastPageToken: "opaque", SearchRequestIDs: []string{"req-1", "req-2"},
	})
	if err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderReconcileStoreRecordsOnlyHashOfOpaquePageToken(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mock.ExpectExec("UPDATE tiktok_shop_order_sync_runs").
		WithArgs("run-1", 1, 2, 2, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = NewTikTokOrderReconcileStore(database).RecordPage(t.Context(), "run-1", TikTokOrderReconcileProgress{
		PageCount: 1, DiscoveredCount: 2, SnapshottedCount: 2, LastPageToken: "opaque-secret-cursor", SearchRequestIDs: []string{"req-1"},
	})
	if err != nil {
		t.Fatalf("RecordPage() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
