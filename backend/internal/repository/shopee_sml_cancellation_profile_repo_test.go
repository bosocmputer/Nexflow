package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCompleteSMLCancellationQueuesProfileRepairInSameTransactionAsCoreResult(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	columns := cancellationScanColumnsForTest()
	row := cancellationScanValuesForTest(now)
	row[6] = "created"
	row[13] = []byte(`{"document_profile_version":"sml-document-v1","doc_no":"CN-1"}`)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE shopee_sml_cancellations AS target.*core_status.*profile_reconciliation_required.*stock_recalc_status.*RETURNING`).
		WithArgs("11111111-1111-1111-1111-111111111111", "created", "CN-1", sqlmock.AnyArg(), "",
			"created", "sml-document-v1", "needs_reconciliation", "hash-cn", sqlmock.AnyArg(), sqlmock.AnyArg(), true).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))
	mock.ExpectExec(`(?s)INSERT INTO sml_cancellation_profile_reconciliation_jobs.*ON CONFLICT.*DO NOTHING`).
		WithArgs("aoy", "11111111-1111-1111-1111-111111111111", "sml-document-v1", "creditnote", "hash-cn", sqlmock.AnyArg(), sqlmock.AnyArg(), "trace-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := NewShopeeRealtimeRepo(db).WithTenantKey("aoy").CompleteSMLCancellation(
		context.Background(), "11111111-1111-1111-1111-111111111111", "created", "CN-1",
		json.RawMessage(`{"success":true}`), "", SMLCancellationProfileResult{
			Version: "sml-document-v1", Route: "creditnote", PayloadHash: "hash-cn", CoreStatus: "created",
			ProfileStatus: "needs_reconciliation", RequiredChecks: []string{"core", "erp_log"},
			CompletedChecks: []string{"core"}, ReconciliationRequired: true, CorrelationID: "trace-1",
		})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "created" || got.StockRecalcStatus != "pending" || !got.ProfileNeedsRepair || got.ProfileStatus != "needs_reconciliation" {
		t.Fatalf("cancellation=%+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimSMLCancellationProfileJobUsesTenantAndFencing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	mock.ExpectQuery(`(?s)WITH candidate AS.*tenant_key=\$3.*FOR UPDATE OF job SKIP LOCKED.*lease_token=lease_token\+1`).
		WithArgs("worker-1", int64(120), "aoy").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_key", "cancellation_id", "profile_version", "route_name", "payload_hash", "status",
			"required_checks", "completed_checks", "attempt_count", "manual_retry_count", "max_attempts",
			"lease_owner", "lease_token", "lease_until", "correlation_id", "shop_id", "order_sn", "bill_id",
			"source_doc_no", "cancel_doc_no", "route_endpoint", "request_payload",
		}).AddRow("job-1", "aoy", "cancel-1", "sml-document-v1", "creditnote", "hash", "running",
			[]byte(`["core","erp_log"]`), []byte(`["core"]`), 1, 0, 10, "worker-1", int64(4), now.Add(2*time.Minute),
			"trace-1", int64(1), "ORDER-1", "bill-1", "BF-INV-1", "CN-1",
			"/api/v1/ic/sale-invoices/:doc_no/cancel", []byte(`{"document_profile_version":"sml-document-v1","doc_no":"CN-1"}`)))
	job, err := NewShopeeRealtimeRepo(db).WithTenantKey("aoy").ClaimSMLCancellationProfileReconciliationJob(context.Background(), "worker-1", 2*time.Minute)
	if err != nil || job == nil || job.LeaseToken != 4 || job.Route != "creditnote" || job.TenantKey != "aoy" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSMLCancellationProfileQueueMetricsAreTenantScoped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)FROM sml_cancellation_profile_reconciliation_jobs WHERE tenant_key=\$1`).
		WithArgs("aoy").
		WillReturnRows(sqlmock.NewRows([]string{"depth", "oldest", "p95", "retries", "terminal", "mismatch"}).
			AddRow(int64(2), 61.5, 45.0, int64(3), int64(1), int64(1)))
	got, err := NewShopeeRealtimeRepo(db).WithTenantKey("aoy").SMLCancellationProfileQueueMetrics(context.Background())
	if err != nil || got.QueueDepth != 2 || got.TerminalCount != 1 || got.PayloadMismatchCount != 1 || got.TenantKey != "aoy" {
		t.Fatalf("metrics=%+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFailSMLCancellationProfileJobNeverChangesCoreOrStockState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE sml_cancellation_profile_reconciliation_jobs SET`)).
		WithArgs("job-1", "worker-1", int64(7), "manual_reconciliation", int64(600), "logs_db_down", "logs database unavailable").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE shopee_sml_cancellations SET
		profile_status=$2,profile_reconciliation_required=TRUE,updated_at=NOW()`)).
		WithArgs("cancel-1", "terminal_failure", "sml-document-v1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	job := &SMLCancellationProfileReconciliationJob{
		ID: "job-1", CancellationID: "cancel-1", ProfileVersion: "sml-document-v1",
		LeaseOwner: "worker-1", LeaseToken: 7, AttemptCount: 10, MaxAttempts: 10,
	}
	if err := NewShopeeRealtimeRepo(db).FailSMLCancellationProfileReconciliationJob(context.Background(), job, "logs_db_down", "logs database unavailable", false); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRetrySMLCancellationProfileReopensOnlyProfileJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM sml_cancellation_profile_reconciliation_jobs job.*FOR UPDATE OF job,cancellation`).
		WithArgs("cancel-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "profile_version", "status", "attempt_count", "manual_retry_count", "max_attempts", "lease_until"}).
			AddRow("job-1", "sml-document-v1", "manual_reconciliation", 10, 2, 10, nil))
	mock.ExpectQuery(`(?s)UPDATE sml_cancellation_profile_reconciliation_jobs SET.*status='queued'.*RETURNING`).
		WithArgs("job-1", "trace-2", true).
		WillReturnRows(sqlmock.NewRows([]string{"status", "attempt_count", "manual_retry_count", "max_attempts", "correlation_id"}).
			AddRow("queued", 0, 3, 10, "trace-2"))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE shopee_sml_cancellations SET
		profile_status='needs_reconciliation',profile_reconciliation_required=TRUE,updated_at=NOW()`)).
		WithArgs("cancel-1", "sml-document-v1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	job, err := NewShopeeRealtimeRepo(db).RetrySMLCancellationProfileReconciliation(context.Background(), "cancel-1", "trace-2")
	if err != nil || job == nil || job.Status != "queued" || job.AttemptCount != 0 || job.ManualRetryCount != 3 {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func cancellationScanColumnsForTest() []string {
	return []string{
		"id", "shop_id", "order_sn", "bill_id", "sale_sml_doc_no", "cancel_sml_doc_no", "status", "error",
		"response", "created_by", "created_at", "updated_at", "completed_at", "request_payload", "trigger_source",
		"route_endpoint", "route_signature", "error_code", "attempts", "next_run_at", "lease_until", "stock_recalc_status",
		"stock_recalc_error", "stock_recalc_attempts", "stock_recalc_next_run_at", "stock_recalc_lease_until",
	}
}

func cancellationScanValuesForTest(now time.Time) []driver.Value {
	return []driver.Value{
		"11111111-1111-1111-1111-111111111111", int64(1), "ORDER-1", "22222222-2222-2222-2222-222222222222",
		"BF-INV-1", "CN-1", "created", "", []byte(`{"success":true}`), nil, now, now, now,
		[]byte(`{}`), "auto", "/api/v1/ic/sale-invoices/:doc_no/cancel", "sig", "", 1, now, nil,
		"pending", "", 0, now, nil,
	}
}
