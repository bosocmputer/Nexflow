package shopeestock

import (
	"context"
	"errors"
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
	mock.ExpectExec(`(?s)UPDATE shopee_stock_settings.*schedule_mode = \$5.*next_run_at = \$10.*config_version = config_version \+ 1`).
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

func TestUpdateMappingRejectsManagedExclusionUnderAliasLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT COALESCE\(m.marketplace_alias_id::text,''\).*shopee_stock_products`).
		WithArgs(int64(42), int64(1001), int64(2001)).
		WillReturnRows(sqlmock.NewRows([]string{"marketplace_alias_id"}).AddRow("00000000-0000-0000-0000-000000000001"))
	mock.ExpectQuery(`SELECT stock_policy FROM marketplace_item_aliases.*FOR UPDATE`).
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{"stock_policy"}).AddRow("managed"))
	mock.ExpectRollback()

	_, err = NewStore(db).UpdateMapping(context.Background(), 42, 1001, 2001, MappingUpdate{Excluded: true}, "")
	if !errors.Is(err, ErrUnsafeManagedExclusion) {
		t.Fatalf("managed mapping exclusion must fail before changing local state: %v", err)
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

func TestListProductGroupsUsesShopeeItemKeyset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)WITH matched_variants AS.*FROM shopee_stock_products matched_product.*JOIN shopee_stock_mappings matched_mapping.*matched_product.item_id>\$3.*matched_items AS.*GROUP BY.*item_id.*LIMIT \$4.*variant_facts AS.*source_totals AS.*unit_totals AS.*aggregated AS.*MAX\(last_seen_at\).*SELECT aggregated.item_id.*sml_unit_totals.*target_count`).
		WithArgs(int64(42), "%milk%", int64(1000), 51).
		WillReturnRows(sqlmock.NewRows([]string{
			"item_id", "item_name", "item_sku", "variant_count", "ready_count", "fix_count", "excluded_count", "updated_at",
			"summary_count", "sml_usable_total", "sml_base_unit_code", "sml_base_unit_name", "sml_total_status",
			"sml_unit_totals", "shopee_stock_total", "target_stock_total", "target_count", "changed_count",
		}).AddRow(
			int64(1100), "Milk product", "MILK-1", 3, 3, 0, 0, time.Now(),
			3, 48.0, "PCS", "ชิ้น", "ready", []byte(`[{"unit_code":"PCS","unit_name":"ชิ้น","quantity":48,"source_count":3}]`), int64(42), int64(48), 3, 2,
		))

	groups, more, err := NewStore(db).ListProductGroups(context.Background(), 42, ProductGroupFilter{Query: "milk", AfterItemID: 1000, Limit: 50})
	if err != nil || more || len(groups) != 1 {
		t.Fatalf("groups=%#v more=%v err=%v", groups, more, err)
	}
	group := groups[0]
	if group.SummaryCount != 3 || group.SMLUsableTotal == nil || *group.SMLUsableTotal != 48 || group.SMLBaseUnitName != "ชิ้น" || group.SMLTotalStatus != "ready" {
		t.Fatalf("unexpected SML summary: %#v", group)
	}
	if len(group.SMLUnitTotals) != 1 || group.SMLUnitTotals[0].Quantity != 48 || group.SMLUnitTotals[0].SourceCount != 3 || group.SMLUnitTotals[0].UnitName != "ชิ้น" {
		t.Fatalf("unexpected SML unit totals: %#v", group.SMLUnitTotals)
	}
	if group.ShopeeStockTotal != 42 || group.TargetStockTotal == nil || *group.TargetStockTotal != 48 || group.TargetCount != 3 || group.ChangedCount != 2 {
		t.Fatalf("unexpected Shopee summary: %#v", group)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListProductGroupVariantsUsesModelKeysetWithoutOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)FROM shopee_stock_products p.*p.shop_id=\$1.*p.item_id=\$2.*p.model_id>\$3.*ORDER BY p.model_id.*LIMIT \$4`).
		WithArgs(int64(42), int64(1000), int64(2000), 101).
		WillReturnRows(sqlmock.NewRows([]string{"shop_id"}))

	variants, more, err := NewStore(db).ListProductGroupVariants(context.Background(), 42, ProductVariantFilter{ItemID: 1000, AfterModelID: 2000, Limit: 100})
	if err != nil || more || len(variants) != 0 {
		t.Fatalf("variants=%#v more=%v err=%v", variants, more, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPendingShopeeReservationsAggregatesTenantDemandBySMLItem(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM marketplace_stock_reservations r.*blocked_mapping.*manual_reconciliation`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`(?s)FROM shopee_stock_mappings m.*JOIN marketplace_stock_reservations r ON r.sml_item_code=m.sml_item_code.*GROUP BY m.item_id,m.model_id`).
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

func TestPendingReservationBaseDemandExpandsSetsOnceAcrossChannels(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)SELECT demand.item_code,SUM\(demand.base_qty\)::text.*marketplace_stock_reservations r.*warehouse_code.*UNION ALL.*marketplace_stock_reservation_components c.*warehouse_code`).
		WithArgs("W1", "S1").
		WillReturnRows(sqlmock.NewRows([]string{"item_code", "base_qty"}).
			AddRow("NORMAL-1", "8.000").
			AddRow("COMPONENT-1", "24"))

	demand, err := NewStore(db).PendingReservationBaseDemand(context.Background(), "W1", "S1")
	if err != nil {
		t.Fatalf("PendingReservationBaseDemand: %v", err)
	}
	if demand["NORMAL-1"].Value != 8 || demand["NORMAL-1"].Exact != "8.000" || demand["COMPONENT-1"].Value != 24 {
		t.Fatalf("demand=%v", demand)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSavePreviewPersistsExcludedWarehouseLocations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	snapshot := time.Date(2026, 8, 26, 9, 10, 11, 0, time.FixedZone("Asia/Bangkok", 7*60*60))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM shopee_stock_run_lines WHERE run_id=\$1::uuid`).
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)INSERT INTO shopee_stock_run_lines.*detail.*VALUES`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM shopee_stock_attempts.*result IN \('changed','blocked'\)`).
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)UPDATE shopee_stock_mappings SET.*last_preview_excluded_locations=\$14::jsonb.*last_preview_sml_physical_qty=\$15::numeric.*last_preview_availability_reason=\$22`).
		WithArgs(
			int64(42), int64(1001), int64(2001),
			7.0, -2.0, 0.0, 0.0, int64(5),
			0, false, "", 24.0, int64(0), sqlmock.AnyArg(),
			"31", "8", "23", "7", "net_sale_order_v1", &snapshot, "sha256:approved", "",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE shopee_stock_settings SET dry_run_required=false`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result := &PreviewResult{RunID: "00000000-0000-0000-0000-000000000001", Lines: []PreviewLine{{
		ItemID: 1001, ModelID: 2001, ScopeBalance: 7, ExcludedBalance: -2, TargetStock: 5,
		PendingNexflowQty: 2, PendingBaseQty: 24,
		SMLPhysicalQtyExact: "31", SMLOutstandingSOQtyExact: "8", SMLUsableQtyExact: "23", CalculationUsableQtyExact: "7",
		AvailabilityVersion: "net_sale_order_v1", SourceSnapshotAt: &snapshot, SourceFingerprint: "sha256:approved",
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

func TestQueuePreviewRunSnapshotsConfigCatalogAndDemandRevisions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)INSERT INTO shopee_stock_runs.*config_version.*catalog_generation_id.*demand_revision_snapshot.*SELECT.*st.config_version.*jsonb_object_agg.*RETURNING id::text`).
		WithArgs(int64(42), "2026-08-24").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-1"))

	runID, err := NewStore(db).QueuePreviewRun(context.Background(), 42, "2026-08-24")
	if err != nil || runID != "run-1" {
		t.Fatalf("runID=%q err=%v", runID, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateSyncRunSnapshotsRevisionsAndFencingToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)INSERT INTO shopee_stock_runs.*run_type.*config_version.*catalog_generation_id.*demand_revision_snapshot.*lease_fencing_token.*SELECT.*'sync'.*st.config_version.*RETURNING id::text`).
		WithArgs(int64(42), "scheduler", "2026-08-24", "sync-1", int64(9), 120).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-sync-1"))

	runID, err := NewStore(db).CreateSyncRun(context.Background(), 42, "scheduler", "2026-08-24", "sync-1", 9, 2*time.Minute)
	if err != nil || runID != "run-sync-1" {
		t.Fatalf("runID=%q err=%v", runID, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRenewAndValidateLiveWriteFailsClosedWhenSnapshotChanged(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)WITH renewed AS.*UPDATE shopee_stock_leases.*fencing_token=\$4.*UPDATE shopee_stock_runs.*\(st.enabled=true OR r.trigger_source='manual'\).*config_version=st.config_version.*demand_revision_snapshot.*RETURNING true`).
		WithArgs("run-sync-1", int64(42), "sync-1", int64(9), 120).
		WillReturnRows(sqlmock.NewRows([]string{"valid"}))

	valid, err := NewStore(db).RenewAndValidateLiveWrite(context.Background(), "run-sync-1", 42, "sync-1", 9, 2*time.Minute)
	if err != nil || valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareReadyMappingsPromotesNormalItemsAndCreatesEqualPools(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)WITH candidates AS.*stock_policy='blocked'.*conversion_status='ready'.*UPDATE marketplace_item_aliases.*stock_policy='managed'.*RETURNING`).
		WithArgs(int64(42), "00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{"managed_count"}).AddRow(37))
	mock.ExpectQuery(`(?s)WITH candidate_groups AS.*COUNT\(\*\) BETWEEN 2 AND 50.*BOOL_AND\(NOT m.shared_pool_enabled\).*UPDATE shopee_stock_mappings.*shared_pool_enabled=true.*pool_allocation_pct.*RETURNING`).
		WithArgs(int64(42), "00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{"pool_count", "member_count"}).AddRow(2, 6))
	mock.ExpectExec(`UPDATE shopee_stock_mappings m.*warning_codes=m.warning_codes-'duplicate_sml_item'`).WillReturnResult(sqlmock.NewResult(0, 6))
	mock.ExpectExec(`(?s)WITH active_groups AS.*UPDATE shopee_stock_mappings`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)WITH duplicated AS.*UPDATE shopee_stock_mappings`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE shopee_stock_settings.*enabled=false.*dry_run_required=true.*config_version=config_version\+1`).
		WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_logs.*shopee_stock_ready_mappings_prepared`).
		WithArgs("00000000-0000-0000-0000-000000000001", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := NewStore(db).PrepareReadyMappings(context.Background(), 42, "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if result.ManagedCount != 37 || result.SharedPoolCount != 2 || result.SharedPoolMemberCount != 6 {
		t.Fatalf("result=%+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimQueuedPreviewUsesSkipLockedAndRenewableLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)WITH candidate AS.*FOR UPDATE SKIP LOCKED.*UPDATE shopee_stock_runs.*lease_owner=\$1.*lease_until=NOW\(\)\+make_interval.*RETURNING`).
		WithArgs("worker-1", 300).
		WillReturnRows(sqlmock.NewRows([]string{"id", "shop_id", "as_of_date", "config_version", "catalog_generation_id", "demand_revision_snapshot", "attempt_count", "lease_owner", "lease_until"}).
			AddRow("run-1", int64(42), "2026-08-24", int64(7), nil, []byte(`{"W/S/A":3}`), 1, "worker-1", now.Add(5*time.Minute)))

	job, err := NewStore(db).ClaimQueuedPreview(context.Background(), "worker-1", 5*time.Minute)
	if err != nil || job == nil || job.ID != "run-1" || job.ConfigVersion != 7 {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSavePreviewLeasedRejectsStaleSnapshotBeforePersistingLines(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT r.lease_owner=\$2.*r.config_version=st.config_version.*demand_revision_snapshot.*FOR SHARE OF r,st`).
		WithArgs("00000000-0000-0000-0000-000000000001", "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(false))
	mock.ExpectRollback()

	err = NewStore(db).SavePreviewLeased(context.Background(), 42, &PreviewResult{RunID: "00000000-0000-0000-0000-000000000001"}, "worker-1")
	if !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("err=%v want ErrPreviewStale", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
