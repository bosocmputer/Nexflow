package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"nexflow/internal/models"
)

func TestEnrichSMLResolutionMarksSentAttemptFailuresResolved(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	logs := []models.AuditLog{{
		ID: "log-1", Action: "sml_failed",
		Detail: []byte(`{"attempt_id":"00000000-0000-0000-0000-000000000001","exchange_id":"exchange-1"}`),
	}}
	mock.ExpectQuery(`(?s)SELECT a.id::text,a.state.*FROM bill_sml_attempts`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "state", "core_status", "bill_status", "is_current"}).
			AddRow("00000000-0000-0000-0000-000000000001", "sent", "created", "sent", true))

	if err := NewAuditLogRepo(db).EnrichSMLResolution(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	if logs[0].AttemptID == "" || logs[0].ExchangeID != "exchange-1" || logs[0].ResolutionStatus != "resolved" || logs[0].CanRetry {
		t.Fatalf("log = %#v", logs[0])
	}
}

func TestEnrichSMLResolutionFailsClosedWhenAttemptCannotBeRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	logs := []models.AuditLog{{ID: "log-1", Action: "sml_failed", Detail: []byte(`{"attempt_id":"00000000-0000-0000-0000-000000000001"}`)}}
	mock.ExpectQuery(`(?s)SELECT a.id::text,a.state.*FROM bill_sml_attempts`).WillReturnError(context.DeadlineExceeded)

	if err := NewAuditLogRepo(db).EnrichSMLResolution(context.Background(), logs); err == nil {
		t.Fatal("expected enrichment error")
	}
	if logs[0].CanRetry {
		t.Fatal("resolution lookup failure must not enable retry")
	}
}
