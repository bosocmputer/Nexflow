package repository

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMappingUpdateRejectsStaleVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	version := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)UPDATE mappings.*WHERE id = \$4 AND updated_at = \$5`).
		WithArgs("NEW-CODE", "PCS", "user-1", "mapping-1", version).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "raw_name", "item_code", "unit_code", "confidence", "source",
			"usage_count", "last_used_at", "learned_from_bill_id", "created_by", "created_at", "updated_at",
		}))

	_, err = NewMappingRepo(db).UpdateByID("mapping-1", "NEW-CODE", "PCS", "user-1", version)
	if !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("UpdateByID error = %v, want ErrMappingConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMappingApplyOnlyTouchesOpenSalesItemsWithoutSKU(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bill_items bi
		SET item_code = $1, unit_code = $2, mapped = TRUE
		FROM bills b
		WHERE b.id = bi.bill_id
		  AND b.source IN ('shopee','lazada','tiktok')
		  AND b.bill_type = 'sale'
		  AND b.status IN ('pending','needs_review')
		  AND b.archived_at IS NULL
		  AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = ''
		  AND btrim(regexp_replace(replace(bi.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = $3`)).
		WithArgs("TARGET", "PCS", "ชื่อ เดิม").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)UPDATE bills b.*b\.status = 'needs_review'.*NOT EXISTS`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, ready, err := NewMappingRepo(db).ApplyToOpenNoSKUItems("  ชื่อ   เดิม ", "TARGET", "PCS")
	if err != nil {
		t.Fatalf("ApplyToOpenNoSKUItems: %v", err)
	}
	if applied != 2 || ready != 1 {
		t.Fatalf("applied=%d ready=%d, want 2/1", applied, ready)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
