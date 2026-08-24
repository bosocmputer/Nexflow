package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"nexflow/internal/models"
)

func TestBuildMarketplaceReservationUsesSnapshottedBaseDemand(t *testing.T) {
	aliasID := "alias-1"
	itemCode := "SML-A"
	unit := "PACK"
	sourceQty := 2.0
	multiplier := int64(1)
	revision := int64(7)
	stand, divide, base := "12", "1", "24"
	item := models.BillItem{
		ID: "line-db-1", SourceLineID: "line-1", SourceItemID: "100", SourceVariantID: "200",
		MarketplaceAliasID: &aliasID, ItemCode: &itemCode, UnitCode: &unit, Qty: 2, SourceQty: &sourceQty,
		QuantityMultiplierSnapshot: &multiplier, MappingRevisionSnapshot: &revision,
		UnitStandValueSnapshot: &stand, UnitDivideValueSnapshot: &divide, BaseQtySnapshot: &base,
	}

	reservation, include := buildMarketplaceReservation("shopee", "shop:99", "ORDER-1", item)
	if !include {
		t.Fatal("ready marketplace line was skipped")
	}
	if reservation.State != "active" || reservation.BaseQty != "24" || reservation.QuantityMultiplier != 1 || reservation.MappingRevision == nil || *reservation.MappingRevision != 7 {
		t.Fatalf("reservation = %#v", reservation)
	}
}

func TestBuildMarketplaceReservationFailsClosedWhenConversionIsUnproved(t *testing.T) {
	itemCode := "SML-A"
	item := models.BillItem{ID: "line-db-1", SourceLineID: "line-1", SourceSKU: "SKU-A", ItemCode: &itemCode, Qty: 2}
	reservation, include := buildMarketplaceReservation("lazada", "default", "ORDER-1", item)
	if !include {
		t.Fatal("unproved line must be retained as a blocked reservation")
	}
	if reservation.State != "blocked_mapping" || reservation.StateReason == "" {
		t.Fatalf("reservation = %#v", reservation)
	}
}

func TestBuildMarketplaceReservationSkipsShipping(t *testing.T) {
	item := models.BillItem{SourceSKU: models.ShopeeShippingSourceSKU, Qty: 1}
	if _, include := buildMarketplaceReservation("shopee", "shop:99", "ORDER-1", item); include {
		t.Fatal("shipping line must not reserve product stock")
	}
}

func TestInsertMarketplaceReservationsAggregatesDuplicateSetComponents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	aliasID := "alias-1"
	itemCode := "SET-A"
	unit := "PACK"
	sourceQty := 2.0
	multiplier := int64(1)
	revision := int64(7)
	stand, divide, base := "1", "1", "2"
	bill := &models.Bill{ID: "bill-1", Source: "shopee", SourceAccountKey: "shop:99", SMLOrderID: "ORDER-1"}
	items := []models.BillItem{{
		ID: "line-db-1", SourceLineID: "line-1", SourceItemID: "100", SourceVariantID: "200",
		MarketplaceAliasID: &aliasID, ItemCode: &itemCode, UnitCode: &unit, Qty: 2, SourceQty: &sourceQty,
		QuantityMultiplierSnapshot: &multiplier, MappingRevisionSnapshot: &revision,
		UnitStandValueSnapshot: &stand, UnitDivideValueSnapshot: &divide, BaseQtySnapshot: &base,
		SetDefinitionHashSnapshot: "set-hash",
	}}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO marketplace_stock_reservations.*RETURNING id::text,state`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "state"}).AddRow("reservation-1", "active"))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_reservation_components.*SUM\(\$2::numeric \* qty \* unit_factor\).*GROUP BY component_item_code`).
		WithArgs("reservation-1", "2", "set-hash", "SET-A").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*marketplace_stock_reservation_components`).
		WithArgs("reservation-1").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := insertMarketplaceReservationsTx(tx, "", bill, items); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimStockRecalcJobUsesSkipLockedLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)WITH candidate AS.*FOR UPDATE SKIP LOCKED.*UPDATE marketplace_stock_recalc_jobs.*RETURNING`).
		WithArgs("worker-1", 300).
		WillReturnRows(sqlmock.NewRows([]string{"id", "bill_id", "sml_attempt_id", "attempt_count", "lease_owner", "lease_until", "processstock_succeeded_at"}).
			AddRow("job-1", "bill-1", "attempt-1", 1, "worker-1", now.Add(5*time.Minute), nil))

	job, err := NewBillRepo(db).ClaimStockRecalcJob(context.Background(), "worker-1", 5*time.Minute)
	if err != nil || job == nil || job.ID != "job-1" || job.AttemptCount != 1 {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteStockRecalcReleasesReservationOnlyAfterVerification(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE marketplace_stock_recalc_jobs.*balance_verified_at=NOW.*lease_owner=\$2.*RETURNING bill_id`).
		WithArgs("job-1", "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"bill_id"}).AddRow("bill-1"))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*marketplace_stock_reservation_components`).
		WithArgs("bill-1").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*marketplace_stock_reservations r.*NOT EXISTS`).
		WithArgs("bill-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE marketplace_stock_reservations.*state='incorporated_in_sml'.*awaiting_stock_recalc`).
		WithArgs("bill-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewBillRepo(db).CompleteStockRecalcJob(context.Background(), "job-1", "worker-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileMarketplaceReservationCancelledKeepsUncertainSMLDemand(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*marketplace_stock_reservations r.*state IN \('active','blocked_mapping'\)`).
		WithArgs("shopee", "shop:99", "ORDER-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*marketplace_stock_reservation_components c.*state IN \('active','blocked_mapping'\)`).
		WithArgs("shopee", "shop:99", "ORDER-1").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)UPDATE marketplace_stock_reservations.*state=CASE.*active.*blocked_mapping.*released_cancelled.*sending_sml.*awaiting_stock_recalc.*manual_reconciliation`).
		WithArgs("shopee", "shop:99", "ORDER-1").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	if err := NewBillRepo(db).ReconcileMarketplaceReservationCancelled(context.Background(), "shopee", "shop:99", "ORDER-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFailStockRecalcJobMovesTerminalReservationsInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE marketplace_stock_recalc_jobs.*RETURNING status,bill_id::text`).
		WithArgs("job-1", "worker-1", "balance unavailable").
		WillReturnRows(sqlmock.NewRows([]string{"status", "bill_id"}).AddRow("manual_reconciliation", "bill-1"))
	mock.ExpectExec(`(?s)UPDATE marketplace_stock_reservations.*state='manual_reconciliation'.*bill_id=\$1`).
		WithArgs("bill-1").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := NewBillRepo(db).FailStockRecalcJob(context.Background(), "job-1", "worker-1", "balance unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
