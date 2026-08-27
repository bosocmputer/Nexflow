package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestCreateWithItemsAndAuditRollsBackWhenAnyItemFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBillRepo(db)
	bill := &models.Bill{BillType: "sale", Source: "tiktok", Status: "pending", SMLOrderID: "TT-1"}
	gross := 100.0
	price := 100.0
	items := []models.BillItem{
		{RawName: "สินค้า A", Qty: 1, Price: &price, GrossAmount: &gross, Mapped: true},
		{RawName: "สินค้า B", Qty: 1, Price: &price, GrossAmount: &gross, Mapped: true},
	}

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO bills").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("bill-1", time.Now()))
	mock.ExpectQuery("INSERT INTO bill_items").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("item-1"))
	mock.ExpectQuery("INSERT INTO bill_items").
		WillReturnError(errors.New("item insert failed"))
	mock.ExpectRollback()

	err = repo.CreateWithItemsAndAudit(bill, items, models.AuditEntry{Action: "bill_created", Source: "tiktok"})
	if err == nil {
		t.Fatal("expected item insert failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestUpdateBillItemFieldsRecalculatesGrossAndClearsAmountReview(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewBillRepo(db)
	qty := 3.0
	price := 33.333333
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status, archived_at, current_sml_attempt_id::text.*FROM bills WHERE id=\$1 FOR UPDATE`).
		WithArgs("bill-1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "archived_at", "current_sml_attempt_id"}).AddRow("pending", nil, nil))
	mock.ExpectExec(`(?s)UPDATE bill_items SET qty=\$1, price=\$2, conversion_override_fields=.*WHERE id=\$4 AND bill_id=\$5`).
		WithArgs(qty, price, sqlmock.AnyArg(), "item-1", "bill-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)SET gross_amount=ROUND.*sml_qty=CASE`).
		WithArgs("item-1", "bill-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE bills SET mutation_revision=mutation_revision\+1.*amount_reviewed_at=CASE`).
		WithArgs("bill-1", false, true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.UpdateBillItemFields("bill-1", "item-1", nil, nil, &qty, &price, nil); err != nil {
		t.Fatalf("UpdateBillItemFields: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestUpdateBillItemFieldsRejectsMutationAfterSMLAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	qty := 2.0
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status, archived_at, current_sml_attempt_id::text.*FROM bills WHERE id=\$1 FOR UPDATE`).
		WithArgs("bill-1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "archived_at", "current_sml_attempt_id"}).AddRow("failed", nil, "attempt-1"))
	mock.ExpectRollback()

	err = NewBillRepo(db).UpdateBillItemFields("bill-1", "item-1", nil, nil, &qty, nil, nil)
	if err != ErrBillMutationConflict {
		t.Fatalf("error = %v, want ErrBillMutationConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyMarketplaceMasterRefreshesExactReservationInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stand, divide, generation := "1", "1", "00000000-0000-0000-0000-000000000099"
	alias := &models.MarketplaceItemAlias{
		ID: "00000000-0000-0000-0000-000000000010", ItemCode: "AH-0009", UnitCode: "แท่ง",
		QuantityMultiplier: 1, MappingRevision: 1, UnitStandValue: &stand, UnitDivideValue: &divide,
		UnitCatalogGeneration: &generation, ConversionStatus: "ready", SalesEnabled: true,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status, archived_at, current_sml_attempt_id::text.*FROM bills WHERE id=\$1 FOR UPDATE`).
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{"status", "archived_at", "current_sml_attempt_id"}).AddRow("needs_review", nil, nil))
	mock.ExpectQuery(`SELECT set_definition_hash FROM sml_catalog`).
		WithArgs("AH-0009").
		WillReturnRows(sqlmock.NewRows([]string{"set_definition_hash"}).AddRow(""))
	mock.ExpectExec(`(?s)UPDATE bill_items SET marketplace_alias_id=.*WHERE bill_id=\$1::uuid AND id=\$2::uuid`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*FROM marketplace_stock_reservations r.*bi.id=ANY\(\$2::uuid\[\]\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*marketplace_stock_reservation_components.*bi.id=ANY\(\$2::uuid\[\]\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)UPDATE marketplace_stock_reservations r SET.*bi.id=ANY\(\$12::uuid\[\]\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)DELETE FROM marketplace_stock_reservation_components.*bi.id=ANY\(\$2::uuid\[\]\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)INSERT INTO marketplace_stock_demand_versions.*SELECT DISTINCT demand.*bi.id=ANY\(\$2::uuid\[\]\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE bills b SET mutation_revision=mutation_revision\+1`).
		WithArgs("00000000-0000-0000-0000-000000000001", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = NewBillRepo(db).ApplyMarketplaceMasterToBillItem(
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		alias,
		true,
	)
	if err != nil {
		t.Fatalf("ApplyMarketplaceMasterToBillItem: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
