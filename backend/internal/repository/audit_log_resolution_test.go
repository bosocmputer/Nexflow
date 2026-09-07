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
	mock.ExpectQuery(`(?s)SELECT a.id::text,a.bill_id::text.*FROM bill_sml_attempts`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "bill_id", "doc_no", "route", "state", "core_status", "bill_status", "is_current"}).
			AddRow("00000000-0000-0000-0000-000000000001", "00000000-0000-4000-8000-000000000010", "BF-INV26090002", "SaleInvoice", "sent", "created", "sent", true))

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
	mock.ExpectQuery(`(?s)SELECT a.id::text,a.bill_id::text.*FROM bill_sml_attempts`).WillReturnError(context.DeadlineExceeded)

	if err := NewAuditLogRepo(db).EnrichSMLResolution(context.Background(), logs); err == nil {
		t.Fatal("expected enrichment error")
	}
	if logs[0].CanRetry {
		t.Fatal("resolution lookup failure must not enable retry")
	}
}

func TestEnrichSMLResolutionLinksLegacyEventOnlyWithUniqueDocumentRouteEvidence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	billID := "00000000-0000-4000-8000-000000000010"
	logs := []models.AuditLog{{
		ID: "legacy", Action: "sml_failed", TargetID: &billID,
		Detail: []byte(`{"doc_no_attempted":"BF-INV26090002","route":"SaleInvoice"}`),
	}}
	mock.ExpectQuery(`(?s)SELECT a.id::text,a.bill_id::text.*FROM bill_sml_attempts`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "bill_id", "doc_no", "route", "state", "core_status", "bill_status", "is_current"}).
			AddRow("00000000-0000-4000-8000-000000000001", billID, "BF-INV26090002", "SaleInvoice", "sent", "created", "sent", true))

	if err := NewAuditLogRepo(db).EnrichSMLResolution(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	if logs[0].AttemptID != "00000000-0000-4000-8000-000000000001" || logs[0].ResolutionStatus != "resolved" || logs[0].CanRetry {
		t.Fatalf("log=%#v", logs[0])
	}
}

func TestEnrichSMLResolutionDoesNotLinkAmbiguousLegacyDocument(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	billID := "00000000-0000-4000-8000-000000000010"
	logs := []models.AuditLog{{
		ID: "legacy", Action: "sml_failed", TargetID: &billID,
		Detail: []byte(`{"doc_no_attempted":"BF-INV26090002","route":"SaleInvoice"}`),
	}}
	rows := sqlmock.NewRows([]string{"id", "bill_id", "doc_no", "route", "state", "core_status", "bill_status", "is_current"}).
		AddRow("00000000-0000-4000-8000-000000000001", billID, "BF-INV26090002", "SaleInvoice", "failed_exact_retry", "", "failed", false).
		AddRow("00000000-0000-4000-8000-000000000002", billID, "BF-INV26090002", "SaleInvoice", "sent", "created", "sent", true)
	mock.ExpectQuery(`(?s)SELECT a.id::text,a.bill_id::text.*FROM bill_sml_attempts`).WillReturnRows(rows)

	if err := NewAuditLogRepo(db).EnrichSMLResolution(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	if logs[0].AttemptID != "" || logs[0].ResolutionStatus != "" || logs[0].CanRetry {
		t.Fatalf("ambiguous log must remain standalone: %#v", logs[0])
	}
}
