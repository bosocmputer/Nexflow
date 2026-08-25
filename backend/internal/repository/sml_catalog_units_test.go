package repository

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestListActiveCatalogUnitsUsesOnlyActivatedGeneration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("JOIN sml_catalog_sync_runs").WithArgs("SKU-1").WillReturnRows(
		sqlmock.NewRows([]string{"item_code", "unit_code", "unit_name", "stand_value", "divide_value", "is_default", "unit_order", "generation_id"}).
			AddRow("SKU-1", "ชิ้น", "ชิ้น", "1", "1", true, 0, "gen-1").
			AddRow("SKU-1", "แพ็ค", "แพ็ค 12", "12", "1", false, 1, "gen-1"),
	)

	units, err := NewSMLCatalogRepo(db).ListActiveUnits(t.Context(), "SKU-1")
	if err != nil {
		t.Fatalf("ListActiveUnits: %v", err)
	}
	if len(units) != 2 || units[1].StandValue != "12" || units[1].DivideValue != "1" {
		t.Fatalf("units = %#v", units)
	}
}

func TestCatalogUpsertDoesNotWriteLegacyPrice(t *testing.T) {
	matcher := func(expectedSQL, actualSQL string) error {
		if expectedSQL == "catalog insert without price" {
			if containsSQLWord(actualSQL, "price") {
				return &queryMatchError{message: "catalog upsert must not write legacy price"}
			}
			return nil
		}
		return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	}
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(matcher)))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectExec("catalog insert without price").WithArgs(catalogUpsertArgsWithoutPrice()...).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("DELETE FROM sml_catalog_set_components").WithArgs("SKU-1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = NewSMLCatalogRepo(db).Upsert(modelsCatalogItemForPriceTest())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestCatalogSearchDoesNotReadLegacyProductPrice(t *testing.T) {
	matcher := func(expectedSQL, actualSQL string) error {
		if expectedSQL == "catalog search without product price" {
			if containsSQLWord(actualSQL, "price") {
				return &queryMatchError{message: "catalog search must not read legacy product price"}
			}
			return nil
		}
		return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	}
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(matcher)))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("catalog search without product price").WithArgs("SKU", 20).
		WillReturnRows(sqlmock.NewRows([]string{"item_code"}))

	items, err := NewSMLCatalogRepo(db).SearchActive("SKU", 20)
	if err != nil {
		t.Fatalf("SearchActive: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("SearchActive returned %d rows, want 0", len(items))
	}
}

type queryMatchError struct{ message string }

func (e *queryMatchError) Error() string { return e.message }

func containsSQLWord(sqlText, word string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(sqlText), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && r != '_'
	}) {
		if token == word {
			return true
		}
	}
	return false
}

func catalogUpsertArgsWithoutPrice() []driver.Value {
	return []driver.Value{
		"SKU-1", "สินค้า", "", "ชิ้น", "", "", "", nil,
		0, nil, "", nil, false, 0, 0, "", true, true, sqlmock.AnyArg(), sqlmock.AnyArg(),
	}
}

func modelsCatalogItemForPriceTest() models.CatalogItem {
	return models.CatalogItem{
		ItemCode: "SKU-1", ItemName: "สินค้า", UnitCode: "ชิ้น",
		SetDocumentValid: true, SetStockValid: true,
	}
}
