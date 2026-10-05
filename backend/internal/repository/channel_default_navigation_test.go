package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestHasConfiguredSalesOrderRoute(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT EXISTS.*channel_defaults.*saleorder.*sale-orders`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	got, err := NewChannelDefaultRepo(db).HasConfiguredSalesOrderRoute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("configured = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHasConfiguredSalesOrderRouteReturnsFalseForInvoiceOnlyTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT EXISTS.*channel_defaults.*saleorder.*sale-orders`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	got, err := NewChannelDefaultRepo(db).HasConfiguredSalesOrderRoute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("configured = true, want false")
	}
}
