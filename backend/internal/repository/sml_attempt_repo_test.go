package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCreateSMLAttemptPersistsImmutablePayloadBeforeExternalCall(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"doc_no":"BF-SO26080001","name":"\u0e44\u0e17\u0e22"}`)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, archived_at, current_sml_attempt_id::text
		   FROM bills WHERE id=$1 FOR UPDATE`)).
		WithArgs("bill-1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "archived_at", "current_sml_attempt_id"}).AddRow("pending", nil, nil))
	mock.ExpectQuery(`(?s)INSERT INTO bill_sml_attempts.*payload_bytes.*RETURNING`).
		WithArgs("tenant-a", "bill-1", "BF-SO26080001", "SaleOrder", payload, json.RawMessage(payload), "payload-hash",
			json.RawMessage(`{"url_override":"/sale-orders"}`), json.RawMessage(`{"item-1":2}`), nil,
			json.RawMessage(`{}`), "lease-1", 300, nil).
		WillReturnRows(sqlmock.NewRows(smlAttemptColumns()).AddRow(
			"attempt-1", "tenant-a", "bill-1", "BF-SO26080001", "sending", "SaleOrder", payload, payload,
			"payload-hash", []byte(`{"url_override":"/sale-orders"}`), []byte(`{"item-1":2}`), nil, []byte(`{}`),
			"lease-1", now.Add(5*time.Minute), now, nil, nil, "", "", nil, now, now,
		))
	mock.ExpectExec(`(?s)UPDATE bills.*current_sml_attempt_id=\$1.*sml_attempt_state='sending'.*sml_payload=\$4`).
		WithArgs("attempt-1", "BF-SO26080001", "bill-1", json.RawMessage(payload)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	attempt, err := NewBillRepo(db).CreateSMLAttempt(context.Background(), SMLAttemptCreate{
		TenantKey: "tenant-a", BillID: "bill-1", DocNo: "BF-SO26080001", Route: "SaleOrder",
		PayloadBytes: payload, PayloadJSON: payload, PayloadHash: "payload-hash",
		RouteSettings: json.RawMessage(`{"url_override":"/sale-orders"}`), MappingRevisions: json.RawMessage(`{"item-1":2}`),
		SetDefinitionHashes: json.RawMessage(`{}`), LeaseOwner: "lease-1", LeaseDuration: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempt == nil || attempt.ID != "attempt-1" || string(attempt.PayloadBytes) != string(payload) {
		t.Fatalf("attempt = %#v", attempt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimExistingSMLAttemptRejectsLiveLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, archived_at, current_sml_attempt_id::text
		   FROM bills WHERE id=$1 FOR UPDATE`)).
		WithArgs("bill-1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "archived_at", "current_sml_attempt_id"}).AddRow("failed", nil, "attempt-1"))
	mock.ExpectQuery(`(?s)SELECT.*FROM bill_sml_attempts.*WHERE id=\$1 FOR UPDATE`).
		WithArgs("attempt-1").
		WillReturnRows(sqlmock.NewRows(smlAttemptColumns()).AddRow(
			"attempt-1", "tenant-a", "bill-1", "BF-SO26080001", "sending", "SaleOrder", []byte(`{}`), []byte(`{}`),
			"hash", []byte(`{}`), []byte(`{}`), nil, []byte(`{}`), "other-worker", now.Add(time.Minute), now, nil, nil, "", "", nil, now, now,
		))
	mock.ExpectRollback()

	_, err = NewBillRepo(db).ClaimExistingSMLAttempt(context.Background(), "bill-1", "lease-2", 5*time.Minute)
	if err != ErrSMLAttemptBusy {
		t.Fatalf("error = %v, want ErrSMLAttemptBusy", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFinishSMLAttemptUsesLeaseOwnerAsFencingToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	response := []byte(`{"status":"success","data":{"doc_no":"BF-SO26080001"}}`)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE bill_sml_attempts.*lease_owner=\$6.*RETURNING bill_id::text, doc_no`).
		WithArgs("attempt-1", "sent", response, sqlmock.AnyArg(), "", "lease-1").
		WillReturnRows(sqlmock.NewRows([]string{"bill_id", "doc_no"}).AddRow("bill-1", "BF-SO26080001"))
	mock.ExpectExec(`(?s)UPDATE bills.*status=\$2.*sml_attempt_state=\$3.*current_sml_attempt_id=\$6`).
		WithArgs("bill-1", "sent", "sent", "BF-SO26080001", json.RawMessage(response), "attempt-1", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewBillRepo(db).FinishSMLAttempt(context.Background(), "attempt-1", "lease-1", "sent", "sent", response, ""); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFinishSMLAttemptRejectsWorkerThatLostLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE bill_sml_attempts.*lease_owner=\$6.*RETURNING bill_id::text, doc_no`).
		WithArgs("attempt-1", "unknown", []byte(nil), sqlmock.AnyArg(), "timeout", "old-lease").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err = NewBillRepo(db).FinishSMLAttempt(context.Background(), "attempt-1", "old-lease", "unknown", "failed", nil, "timeout")
	if err != ErrSMLAttemptLeaseLost {
		t.Fatalf("error = %v, want ErrSMLAttemptLeaseLost", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func smlAttemptColumns() []string {
	return []string{
		"id", "tenant_key", "bill_id", "doc_no", "state", "route", "payload_bytes", "payload_json", "payload_hash",
		"route_settings", "mapping_revisions", "unit_catalog_generation", "set_definition_hashes", "lease_owner", "lease_until",
		"external_request_started_at", "external_request_finished_at", "response_bytes", "response_hash", "error_message", "created_by", "created_at", "updated_at",
	}
}
