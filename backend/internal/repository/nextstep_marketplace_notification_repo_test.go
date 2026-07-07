package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNextStepMarketplaceNotificationRepoBaselineCompleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT EXISTS").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	repo := NewNextStepMarketplaceNotificationRepo(db)
	ok, err := repo.BaselineCompleted(context.Background())
	if err != nil {
		t.Fatalf("BaselineCompleted: %v", err)
	}
	if !ok {
		t.Fatal("BaselineCompleted = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNextStepMarketplaceNotificationRepoUpsertSeenInserted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO nextstep_marketplace_notification_seen").
		WithArgs("MQT26070001", "2026-07-07", "pending").
		WillReturnRows(sqlmock.NewRows([]string{"doc_no"}).AddRow("MQT26070001"))

	repo := NewNextStepMarketplaceNotificationRepo(db)
	inserted, err := repo.UpsertSeen(context.Background(), NextStepMarketplaceSeenInput{
		DocNo:   " MQT26070001 ",
		DocDate: "2026-07-07",
		Status:  "pending",
	})
	if err != nil {
		t.Fatalf("UpsertSeen: %v", err)
	}
	if !inserted {
		t.Fatal("inserted = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNextStepMarketplaceNotificationRepoUpsertSeenExistingUpdates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO nextstep_marketplace_notification_seen").
		WithArgs("MQT26070001", "2026-07-07", "packing").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("UPDATE nextstep_marketplace_notification_seen").
		WithArgs("MQT26070001", "2026-07-07", "packing").
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewNextStepMarketplaceNotificationRepo(db)
	inserted, err := repo.UpsertSeen(context.Background(), NextStepMarketplaceSeenInput{
		DocNo:   "MQT26070001",
		DocDate: "2026-07-07",
		Status:  "packing",
	})
	if err != nil {
		t.Fatalf("UpsertSeen existing: %v", err)
	}
	if inserted {
		t.Fatal("inserted = true, want false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNextStepMarketplaceNotificationRepoMarkNotified(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("UPDATE nextstep_marketplace_notification_seen").
		WithArgs("MQT26070001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewNextStepMarketplaceNotificationRepo(db)
	if err := repo.MarkNotified(context.Background(), " MQT26070001 "); err != nil {
		t.Fatalf("MarkNotified: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}
