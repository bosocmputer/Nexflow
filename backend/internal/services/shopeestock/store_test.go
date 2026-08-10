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
