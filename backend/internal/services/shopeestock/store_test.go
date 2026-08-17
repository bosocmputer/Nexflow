package shopeestock

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

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

	mock.ExpectQuery(`(?s)FROM shopee_order_snapshots snapshot.*snapshot\.erp_status NOT IN.*GROUP BY item_id,model_id`).
		WithArgs(int64(264993963)).
		WillReturnRows(sqlmock.NewRows([]string{"item_id", "model_id", "pending_qty"}).
			AddRow(int64(1001), int64(2001), 3.0).
			AddRow(int64(1002), int64(2002), 7.0))

	reservations, err := NewStore(db).PendingShopeeReservations(context.Background(), 264993963)
	if err != nil {
		t.Fatalf("PendingShopeeReservations: %v", err)
	}
	if reservations[stockProductKey(1001, 2001)] != 3 || reservations[stockProductKey(1002, 2002)] != 7 {
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
