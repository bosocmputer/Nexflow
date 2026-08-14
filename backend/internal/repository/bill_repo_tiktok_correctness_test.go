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
	mock.ExpectExec("UPDATE bill_items SET qty=\\$1, price=\\$2 WHERE id=\\$3").
		WithArgs(qty, price, "item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SET gross_amount=ROUND").
		WithArgs("item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE bills SET amount_reviewed_at=NULL").
		WithArgs("item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.UpdateBillItemFields("item-1", nil, nil, &qty, &price, nil); err != nil {
		t.Fatalf("UpdateBillItemFields: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}
