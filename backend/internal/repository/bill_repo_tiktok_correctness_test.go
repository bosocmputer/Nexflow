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
