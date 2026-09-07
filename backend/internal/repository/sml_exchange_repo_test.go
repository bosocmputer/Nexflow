package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBeginSMLAttemptExchangeLocksParentBeforeAllocatingSequence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id::text FROM bill_sml_attempts WHERE id=$1 FOR UPDATE")).
		WithArgs("attempt-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("attempt-1"))
	mock.ExpectQuery(`(?s)INSERT INTO bill_sml_attempt_exchanges.*COALESCE\(MAX\(exchange_sequence\),0\)\+1.*RETURNING`).
		WithArgs("attempt-1", "trace-1", "SaleInvoice", "POST", "/api/v1/ic/sale-invoices", "application/json", "correlation-1").
		WillReturnRows(sqlmock.NewRows(smlExchangeColumns()).AddRow(
			"exchange-1", "attempt-1", 2, "trace-1", "SaleInvoice", "started",
			"POST", "/api/v1/ic/sale-invoices", "application/json", "correlation-1",
			now, nil, nil, nil, []byte(`{}`), nil, "", int64(0), false, "", "", "", now,
		))
	mock.ExpectCommit()

	exchange, err := NewBillRepo(db).BeginSMLAttemptExchange(context.Background(), SMLExchangeStart{
		AttemptID: "attempt-1", TraceID: "trace-1", Route: "SaleInvoice",
		Method: "POST", CanonicalPath: "/api/v1/ic/sale-invoices",
		ContentType: "application/json", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if exchange.ID != "exchange-1" || exchange.Sequence != 2 || exchange.Status != "started" {
		t.Fatalf("exchange = %#v", exchange)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBeginSMLAttemptExchangeRejectsUnsafeRequestPath(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = NewBillRepo(db).BeginSMLAttemptExchange(context.Background(), SMLExchangeStart{
		AttemptID: "attempt-1", Route: "SaleInvoice", Method: "POST",
		CanonicalPath: "https://credential@example.test/api?token=secret",
	})
	if !errors.Is(err, ErrInvalidSMLExchange) {
		t.Fatalf("error = %v, want ErrInvalidSMLExchange", err)
	}
}

func TestFinishSMLAttemptExchangeSanitizesResponseIndependently(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	status := 409
	raw := []byte(`{"status":"failed","access_token":"secret","data":{"doc_no":"BF-INV26090002"}}`)

	mock.ExpectQuery(`(?s)UPDATE bill_sml_attempt_exchanges.*status='started'.*RETURNING`).
		WithArgs(
			"exchange-1", "failed", &status, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(len(raw)), false,
			"document_conflict", "business", "SML rejected the document",
		).
		WillReturnRows(sqlmock.NewRows(smlExchangeColumns()).AddRow(
			"exchange-1", "attempt-1", 1, "trace-1", "SaleInvoice", "failed",
			"POST", "/api/v1/ic/sale-invoices", "application/json", "trace-1",
			time.Now(), time.Now(), int64(8), status, []byte(`{"Content-Type":"application/json"}`),
			[]byte(`{"access_token":"[REDACTED]","data":{"doc_no":"BF-INV26090002"},"status":"failed"}`),
			"hash", int64(len(raw)), false, "document_conflict", "business", "SML rejected the document", time.Now(),
		))

	exchange, err := NewBillRepo(db).FinishSMLAttemptExchange(context.Background(), SMLExchangeFinish{
		ExchangeID: "exchange-1", Status: "failed", HTTPStatus: &status,
		ResponseHeaders: map[string][]string{
			"Content-Type":  {"application/json"},
			"Authorization": {"Bearer secret"},
		},
		ResponseBody: raw, ErrorCode: "document_conflict", ErrorClass: "business",
		SafeErrorSummary: "SML rejected the document",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(exchange.ResponseJSON) == "" || !json.Valid(exchange.ResponseJSON) {
		t.Fatalf("expected sanitized response JSON, got %q", exchange.ResponseJSON)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFinishSMLAttemptExchangeDoesNotOverwriteFinalEvidence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)UPDATE bill_sml_attempt_exchanges.*status='started'.*RETURNING`).
		WillReturnError(sqlmock.ErrCancelled)

	_, err = NewBillRepo(db).FinishSMLAttemptExchange(context.Background(), SMLExchangeFinish{
		ExchangeID: "exchange-1", Status: "succeeded",
	})
	if !errors.Is(err, sqlmock.ErrCancelled) {
		t.Fatalf("error = %v", err)
	}
}

func smlExchangeColumns() []string {
	return []string{
		"id", "sml_attempt_id", "exchange_sequence", "trace_id", "route", "status",
		"request_method", "request_path", "request_content_type", "correlation_id",
		"started_at", "finished_at", "duration_ms", "http_status", "response_headers",
		"response_json", "response_hash", "response_size", "response_truncated", "error_code",
		"error_class", "safe_error_summary", "created_at",
	}
}
