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
	mock.ExpectExec("UPDATE tiktok_shop_order_sync_runs").WillReturnResult(sqlmock.NewResult(0, 0))
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

func TestTikTokOrderReconcileStoreClaimsDueShopWithBoundedOverlapWindow(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	windowEnd := now.Add(-30 * time.Second)
	windowStart := windowEnd.Add(-15 * time.Minute)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE tiktok_shop_order_sync_runs").WithArgs(now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT s.shop_id, c.gateway_connection_id::text").WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "gateway_connection_id", "interval_seconds", "overlap_seconds", "watermark_update_at"}).
			AddRow("7494619203789490654", "11111111-1111-4111-8111-111111111111", 300, 900, nil))
	mock.ExpectQuery("INSERT INTO tiktok_shop_order_sync_runs").
		WithArgs("11111111-1111-4111-8111-111111111111", "7494619203789490654", windowStart, windowEnd, tikTokOrderReconcilePageSize, tikTokOrderReconcileMaxPages).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("22222222-2222-4222-8222-222222222222"))
	mock.ExpectExec("UPDATE tiktok_shop_order_sync_settings").WithArgs("7494619203789490654", now.Add(5*time.Minute)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	run, err := NewTikTokOrderReconcileStore(database).ClaimDueRun(t.Context(), now)
	if err != nil {
		t.Fatalf("ClaimDueRun() error = %v", err)
	}
	if run.ID != "22222222-2222-4222-8222-222222222222" || !run.WindowStart.Equal(windowStart) || !run.WindowEnd.Equal(windowEnd) {
		t.Fatalf("run = %+v", run)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderReconcileStoreUpdatesOneShopWithOptimisticVersion(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	nextRun := time.Date(2026, 9, 12, 12, 5, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM tiktok_shop_connections").WithArgs("7494619203789490654").
		WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	mock.ExpectExec("INSERT INTO tiktok_shop_order_sync_settings").WithArgs("7494619203789490654").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE tiktok_shop_order_sync_settings").
		WithArgs("7494619203789490654", true, 300, 900, int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "enabled", "interval_seconds", "overlap_seconds", "watermark_update_at", "next_run_at",
			"last_success_at", "last_error_code", "last_error_message", "config_version",
		}).AddRow("7494619203789490654", true, 300, 900, nil, nextRun, nil, "", "", 2))
	mock.ExpectCommit()

	setting, err := NewTikTokOrderReconcileStore(database).UpdateSetting(t.Context(), "7494619203789490654", TikTokOrderSyncSettingUpdate{
		Enabled: true, IntervalSeconds: 300, OverlapSeconds: 900, ConfigVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateSetting() error = %v", err)
	}
	if !setting.Enabled || setting.ConfigVersion != 2 || setting.IntervalSeconds != 300 {
		t.Fatalf("setting = %+v", setting)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderReconcileStoreListsActiveShopsWithDisabledDefaults(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	nextRun := time.Date(2026, 9, 12, 12, 5, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT c.shop_id, c.shop_name").WillReturnRows(sqlmock.NewRows([]string{
		"shop_id", "shop_name", "enabled", "interval_seconds", "overlap_seconds", "watermark_update_at", "next_run_at",
		"last_success_at", "last_error_code", "last_error_message", "config_version",
	}).AddRow("7494619203789490654", "AOY", false, 300, 900, nil, nextRun, nil, "", "", 1))

	settings, err := NewTikTokOrderReconcileStore(database).ListSettings(t.Context())
	if err != nil {
		t.Fatalf("ListSettings() error = %v", err)
	}
	if len(settings) != 1 || settings[0].ShopName != "AOY" || settings[0].Enabled || settings[0].ConfigVersion != 1 {
		t.Fatalf("settings = %+v", settings)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
