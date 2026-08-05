package repository

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMarketplaceAliasIncrementUsageDoesNotChangeVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`(?s)UPDATE marketplace_item_aliases\s+SET usage_count = usage_count \+ 1,\s+last_used_at = NOW\(\)\s+WHERE id = \$1`).
		WithArgs("alias-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewMarketplaceAliasRepo(db).IncrementUsage("alias-1"); err != nil {
		t.Fatalf("IncrementUsage: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasApplyMatchesOnlyExactSourceSKU(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT bi\.id.*NOT EXISTS.*c\.item_code = btrim.*\$5 <> ''.*source_sku.*= \$5`).
		WithArgs("shopee", "sale", "TARGET", "PCS", "SKU-A", "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("item-a"))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bill_items
		    SET item_code = $1, unit_code = $2, mapped = TRUE
		  WHERE id IN ($3)`)).
		WithArgs("TARGET", "PCS", "item-a").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE bills b\s+SET status = 'pending'.*WHERE b\.source = \$1.*AND b\.bill_type = \$2`).
		WithArgs("shopee", "sale").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, ready, err := NewMarketplaceAliasRepo(db).ApplyToOpenItems(
		"shopee", "sale", "SKU-A", "", "ชื่อเดียวกัน", "TARGET", "PCS",
	)
	if err != nil {
		t.Fatalf("ApplyToOpenItems: %v", err)
	}
	if applied != 1 || ready != 1 {
		t.Fatalf("applied=%d ready=%d, want 1/1", applied, ready)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasNoSKUUpsertUsesContiguousPostgresParameters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)UPDATE marketplace_item_aliases.*SET raw_name = \$2.*item_code = \$4.*unit_code = \$5.*COALESCE\(\$6, confirmed_by\).*source = \$1.*= \$3`).
		WithArgs("shopee", "ชื่อ สินค้า", "ชื่อ สินค้า", "SKU-1", "PCS", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "source_sku", "raw_name", "normalized_key", "item_code", "unit_code",
			"confidence", "confirmed_by", "usage_count", "last_used_at", "created_at", "updated_at", "is_active",
		}))
	mock.ExpectQuery(`(?s)INSERT INTO marketplace_item_aliases.*VALUES \(\$1, \$2, \$3, \$4, \$5, \$6, 1\.0, \$7`).
		WithArgs("shopee", "", "ชื่อ สินค้า", "ชื่อ สินค้า", "SKU-1", "PCS", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "source_sku", "raw_name", "normalized_key", "item_code", "unit_code",
			"confidence", "confirmed_by", "usage_count", "last_used_at", "created_at", "updated_at", "is_active",
		}).AddRow("alias-1", "shopee", "", "ชื่อ สินค้า", "ชื่อ สินค้า", "SKU-1", "PCS", 1.0, "user-1", 1, now, now, now, true))

	alias, err := NewMarketplaceAliasRepo(db).Upsert("shopee", "", "  ชื่อ   สินค้า  ", "SKU-1", "PCS", "user-1")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if alias == nil || alias.ID != "alias-1" {
		t.Fatalf("alias = %#v", alias)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
