package repository

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"nexflow/internal/models"
	"testing"
	"time"
)

func TestStockTimelineUsesPersistedEvidence(t *testing.T) {
	now := time.Date(2026, 9, 7, 2, 42, 48, 0, time.UTC)
	for _, tc := range []struct {
		name, status        string
		processed, verified any
		action              string
	}{
		{"complete", "completed", now, now.Add(time.Second), "sml_stock_job_completed"},
		{"missing verification", "completed", now, nil, ""},
		{"missing process evidence", "completed", nil, now, ""},
		{"failure", "failed", nil, nil, "sml_stock_job_incomplete"},
		{"manual", "manual_reconciliation", now, nil, "sml_stock_job_incomplete"},
		{"running is not success", "running", now, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			mock.ExpectQuery(`SELECT j.id::text`).WithArgs("bill").WillReturnRows(sqlmock.NewRows([]string{"id", "status", "doc_no", "processed", "verified", "updated"}).AddRow("job", tc.status, "INV", tc.processed, tc.verified, now.Add(time.Second)))
			logs, err := NewAuditLogRepo(db).AppendStockTimeline(context.Background(), "bill", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.action == "" {
				if len(logs) != 0 {
					t.Fatal(logs)
				}
			} else {
				if len(logs) != 1 || logs[0].Action != tc.action || !logs[0].CreatedAt.Equal(now.Add(time.Second)) || logs[0].CanRetry {
					t.Fatal(logs)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStockTimelineReadFailureAndCap(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	repo := NewAuditLogRepo(db)
	mock.ExpectQuery(`SELECT j.id::text`).WithArgs("bill").WillReturnError(errors.New("unavailable"))
	if _, err := repo.AppendStockTimeline(context.Background(), "bill", nil); err == nil {
		t.Fatal("expected read error")
	}
	now := time.Now()
	mock.ExpectQuery(`SELECT j.id::text`).WithArgs("bill").WillReturnRows(sqlmock.NewRows([]string{"id", "status", "doc_no", "processed", "verified", "updated"}).AddRow("job", "completed", "INV", now, now, now))
	old := make([]models.AuditLog, 200)
	for i := range old {
		old[i].CreatedAt = now.Add(-time.Hour)
	}
	got, err := repo.AppendStockTimeline(context.Background(), "bill", old)
	if err != nil || len(got) != 200 || got[199].ID != "stock-job:job" {
		t.Fatalf("count=%d err=%v", len(got), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStockTimelineDoesNotDuplicateLegacySuccess(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now()
	mock.ExpectQuery(`SELECT j.id::text`).WithArgs("bill").WillReturnRows(sqlmock.NewRows([]string{"id", "status", "doc_no", "processed", "verified", "updated"}).AddRow("job", "completed", "INV", now, now, now))
	logs := []models.AuditLog{{ID: "original", Action: "sml_stock_recalc_ok", Detail: []byte(`{"doc_no":"INV"}`)}}
	got, err := NewAuditLogRepo(db).AppendStockTimeline(context.Background(), "bill", logs)
	if err != nil || len(got) != 1 || got[0].ID != "original" {
		t.Fatal(got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
