package repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestApplyShopeePurchaseDiscountsToBillUsesCoinEffectiveDiscount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	billID := "bill-coin"
	repo := NewBillRepo(db)
	mock.ExpectQuery("SELECT raw_data").
		WithArgs(billID).
		WillReturnRows(sqlmock.NewRows([]string{"raw_data"}).AddRow([]byte(`{"order_id":"#A"}`)))
	mock.ExpectQuery("FROM bill_items").
		WithArgs(billID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "bill_id", "raw_name", "source_sku", "source_item_id", "source_variant_id", "marketplace_alias_id", "source_image_url", "item_code", "qty", "unit_code", "price",
			"gross_amount", "discount_amount", "mapped", "mapping_id", "candidates",
			"source_qty", "sml_qty", "quantity_multiplier_snapshot", "unit_stand_value_snapshot", "unit_divide_value_snapshot", "base_qty_snapshot",
			"mapping_revision_snapshot", "unit_catalog_generation_snapshot", "set_definition_hash_snapshot", "conversion_override_fields", "conversion_issue_code",
		}).
			AddRow("item-1", billID, "สินค้า A", "", "", "", nil, "", "SKU-A", 1.0, "ชิ้น", 100.0, nil, 0.0, true, nil, []byte("[]"), nil, nil, nil, nil, nil, nil, nil, nil, "", []byte("{}"), "").
			AddRow("item-2", billID, "สินค้า B", "", "", "", nil, "", "SKU-B", 1.0, "ชิ้น", 200.0, nil, 0.0, true, nil, []byte("[]"), nil, nil, nil, nil, nil, nil, nil, nil, "", []byte("{}"), ""))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE bills").
		WithArgs(sqlmock.AnyArg(), billID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE bill_items SET discount_amount").
		WithArgs(10.0, "item-1", billID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE bill_items SET discount_amount").
		WithArgs(20.0, "item-2", billID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ok, err := repo.ApplyShopeePurchaseDiscountsToBill(billID, ShopeeDiscountSummary{TotalDiscountAmount: 10}, 20)
	if err != nil {
		t.Fatalf("ApplyShopeePurchaseDiscountsToBill: %v", err)
	}
	if !ok {
		t.Fatal("expected update")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestApplyShopeePurchaseDiscountsToBillSkipsSentBills(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	billID := "sent-bill"
	repo := NewBillRepo(db)
	mock.ExpectQuery("SELECT raw_data").
		WithArgs(billID).
		WillReturnError(sql.ErrNoRows)

	ok, err := repo.ApplyShopeePurchaseDiscountsToBill(billID, ShopeeDiscountSummary{TotalDiscountAmount: 10}, 20)
	if err != nil {
		t.Fatalf("ApplyShopeePurchaseDiscountsToBill: %v", err)
	}
	if ok {
		t.Fatal("expected sent/non-active bill to be skipped")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}
