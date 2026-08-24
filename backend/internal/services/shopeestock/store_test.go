package shopeestock

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func stockSettingsRows(interval int, nextRun time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"shop_id", "shop_name", "connection_id", "credential_mode", "enabled", "stock_pct", "interval_seconds",
		"schedule_mode", "monthly_interval", "monthly_day", "monthly_time", "schedule_risk_acknowledged", "next_run_at", "scope_mode", "locations",
		"all_scope_warning_acknowledged", "dry_run_required", "paused_reason", "last_catalog_sync_at", "last_full_catalog_sync_at",
		"last_catalog_attempt_at", "last_preview_at", "last_sync_at", "last_success_at", "last_error", "updated_at",
	}).AddRow(
		int64(42), "Test shop", "connection-id", "gateway", true, 80.0, interval,
		"interval", 1, 1, "00:00", false, nextRun, "selected", []byte(`[{"warehouse":"W1","location":"S1"}]`),
		false, false, "", nil, nil, nil, nil, nil, nil, "", time.Now(),
	)
}

func TestUpdateSettingsPersistsStructuredScheduleAndNextRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	nextRun := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	mock.ExpectExec(`INSERT INTO shopee_stock_settings`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`(?s)SELECT c.shop_id.*st.schedule_mode.*WHERE c.shop_id = \$1`).WithArgs(int64(42)).WillReturnRows(stockSettingsRows(300, nextRun))
	mock.ExpectExec(`(?s)UPDATE shopee_stock_settings.*schedule_mode = \$5.*next_run_at = \$10`).
		WithArgs(int64(42), true, 80.0, 600, "interval", 1, 1, "00:00", false, nextRun, "selected", sqlmock.AnyArg(), false, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO shopee_stock_settings`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`(?s)SELECT c.shop_id.*st.schedule_mode.*WHERE c.shop_id = \$1`).WithArgs(int64(42)).WillReturnRows(stockSettingsRows(600, nextRun))

	result, err := NewStore(db).UpdateSettings(context.Background(), 42, SettingsUpdate{
		Enabled: true, StockPct: 80, IntervalSeconds: 600, ScheduleMode: "interval",
		MonthlyInterval: 1, MonthlyDay: 1, MonthlyTime: "00:00", NextRunAt: &nextRun,
		ScopeMode: "selected", Locations: []LocationPair{{Warehouse: "W1", Location: "S1"}},
	}, "")
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if result.IntervalSeconds != 600 || result.NextRunAt == nil || !result.NextRunAt.Equal(nextRun) {
		t.Fatalf("result=%+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}

func TestEnabledDueShopsUsesPersistedNextRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)COALESCE\(st.next_run_at.*ORDER BY st.next_run_at NULLS FIRST.*LIMIT 20`).
		WillReturnRows(sqlmock.NewRows([]string{"shop_id"}).AddRow(int64(42)).AddRow(int64(43)))

	got, err := NewStore(db).EnabledDueShops(context.Background())
	if err != nil {
		t.Fatalf("EnabledDueShops: %v", err)
	}
	if len(got) != 2 || got[0] != 42 || got[1] != 43 {
		t.Fatalf("shops=%v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}

func TestCountProductsByStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FILTER.*FROM shopee_stock_products`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"ready_count", "fix_count", "excluded_count"}).AddRow(7, 45, 2))

	counts, err := NewStore(db).countProductsByStatus(context.Background(), 42, "")
	if err != nil {
		t.Fatalf("countProductsByStatus: %v", err)
	}
	if counts.Ready != 7 || counts.Fix != 45 || counts.Excluded != 2 {
		t.Fatalf("counts=%+v", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}

func TestCountProductsByStatusUsesSearchAcrossAllTabs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FILTER.*ILIKE \$2`).
		WithArgs(int64(42), "%MIRUKU%").
		WillReturnRows(sqlmock.NewRows([]string{"ready_count", "fix_count", "excluded_count"}).AddRow(0, 8, 0))

	counts, err := NewStore(db).countProductsByStatus(context.Background(), 42, " MIRUKU ")
	if err != nil {
		t.Fatalf("countProductsByStatus: %v", err)
	}
	if counts.Fix != 8 {
		t.Fatalf("counts=%+v", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}

func TestProductCountsTotalForStatus(t *testing.T) {
	counts := ProductCounts{Ready: 7, Fix: 45, Excluded: 2}
	tests := map[string]int{
		"ready":    7,
		"fix":      45,
		"excluded": 2,
		"":         54,
		"unknown":  54,
	}
	for status, want := range tests {
		if got := counts.totalForStatus(status); got != want {
			t.Fatalf("status=%q got=%d want=%d", status, got, want)
		}
	}
}

func TestPendingShopeeReservationsAggregatesByItemAndModel(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM marketplace_stock_reservations r.*blocked_mapping.*NOT EXISTS`).
		WithArgs("shop:264993963", int64(264993963)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`(?s)FROM marketplace_stock_reservations r.*JOIN LATERAL.*marketplace_alias_id.*GROUP BY mapped.item_id,mapped.model_id`).
		WithArgs(int64(264993963), "shop:264993963").
		WillReturnRows(sqlmock.NewRows([]string{"item_id", "model_id", "pending_qty", "pending_base_qty"}).
			AddRow(int64(1001), int64(2001), 3.0, 36.0).
			AddRow(int64(1002), int64(2002), 7.0, 7.0))

	reservations, err := NewStore(db).PendingShopeeReservationLedger(context.Background(), 264993963)
	if err != nil {
		t.Fatalf("PendingShopeeReservations: %v", err)
	}
	if reservations[stockProductKey(1001, 2001)].SourceQty != 3 || reservations[stockProductKey(1001, 2001)].BaseQty != 36 ||
		reservations[stockProductKey(1002, 2002)].BaseQty != 7 {
		t.Fatalf("reservations=%v", reservations)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}

func TestSavePreviewPersistsExcludedWarehouseLocations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE shopee_stock_mappings SET.*last_preview_excluded_locations=\$14::jsonb`).
		WithArgs(
			int64(42), int64(1001), int64(2001),
			7.0, -2.0, 0.0, 0.0, int64(5),
			0, false, "", 0.0, int64(0), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE shopee_stock_settings SET dry_run_required=false`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result := &PreviewResult{Lines: []PreviewLine{{
		ItemID: 1001, ModelID: 2001, ScopeBalance: 7, ExcludedBalance: -2, TargetStock: 5,
		ExcludedLocations: []ExcludedStockLocation{{
			ItemCode: "AH-0006", WarehouseCode: "AB-2", WarehouseName: "คลังสำรอง",
			LocationCode: "002", LocationName: "หลังร้าน", UnitCode: "กล่อง", BalanceQty: -2,
		}},
	}}}
	if err := NewStore(db).SavePreview(context.Background(), 42, result); err != nil {
		t.Fatalf("SavePreview: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}
