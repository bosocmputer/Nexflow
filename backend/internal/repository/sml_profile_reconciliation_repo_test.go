package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestClaimSMLProfileReconciliationJobReclaimsExpiredWorkerLeaseWithFencingToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	mock.ExpectQuery(`(?s)WITH candidate AS.*status='running' AND lease_until<NOW\(\).*FOR UPDATE SKIP LOCKED.*lease_token=lease_token\+1`).
		WithArgs("worker-1", int64(120)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_key", "sml_attempt_id", "profile_version", "payload_hash", "status", "required_checks", "completed_checks",
			"attempt_count", "manual_retry_count", "max_attempts", "lease_owner", "lease_token", "lease_until", "correlation_id", "bill_id", "doc_no", "payload_bytes", "route_settings",
		}).AddRow("job-1", "aoy", "attempt-1", "sml-document-v1", "hash", "running", []byte(`["core"]`), []byte(`[]`),
			1, 0, 10, "worker-1", 4, now.Add(2*time.Minute), "trace-1", "bill-1", "BF-1", []byte(`{}`), []byte(`{}`)))
	job, err := NewBillRepo(db).ClaimSMLProfileReconciliationJob(context.Background(), "worker-1", 2*time.Minute)
	if err != nil || job == nil || job.LeaseToken != 4 || job.AttemptCount != 1 || job.TenantKey != "aoy" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRetrySMLProfileReconciliationReopensTerminalProfileWithoutResendingCore(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT b.id::text,a.id::text.*FOR UPDATE OF a,j`).
		WithArgs("bill-1").
		WillReturnRows(sqlmock.NewRows([]string{"bill_id", "attempt_id", "profile_version", "profile_status", "job_status", "attempt_count", "manual_retry_count", "max_attempts"}).
			AddRow("bill-1", "attempt-1", "sml-document-v1", "terminal_failure", "manual_reconciliation", 10, 2, 10))
	mock.ExpectQuery(`(?s)UPDATE sml_document_profile_reconciliation_jobs SET.*status='queued'.*RETURNING`).
		WithArgs("attempt-1", "trace-2", true, "sml-document-v1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "attempt_count", "manual_retry_count", "max_attempts", "correlation_id"}).
			AddRow("queued", 0, 3, 10, "trace-2"))
	mock.ExpectExec(`(?s)UPDATE bill_sml_attempts SET.*profile_status='needs_reconciliation'`).
		WithArgs("attempt-1", "sml-document-v1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := NewBillRepo(db).RetrySMLProfileReconciliation(context.Background(), "bill-1", "trace-2")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "queued" || result.AttemptCount != 0 || result.ManualRetryCount != 3 {
		t.Fatalf("result=%+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFailSMLProfileReconciliationJobMovesTenthAttemptToManualReconciliation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE sml_document_profile_reconciliation_jobs SET`)).
		WithArgs("job-1", "worker-1", int64(7), "manual_reconciliation", int64(600), "logs_db_down", "logs database unavailable").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bill_sml_attempts SET`)).
		WithArgs("attempt-1", "terminal_failure", "sml-document-v1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	job := &SMLProfileReconciliationJob{ID: "job-1", SMLAttemptID: "attempt-1", ProfileVersion: "sml-document-v1", LeaseOwner: "worker-1", LeaseToken: 7, AttemptCount: 10, MaxAttempts: 10}
	if err := NewBillRepo(db).FailSMLProfileReconciliationJob(context.Background(), job, "logs_db_down", "logs database unavailable", false); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPauseShopeeAutoSMLRequiresThreeDistinctLatestFailedProfileJobs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec(`(?s)WITH target_shops AS.*ORDER BY job.updated_at DESC,job.id DESC.*LIMIT \$2.*streak.total=\$2 AND streak.failed=\$2.*profile_consecutive_failures`).
		WithArgs("bill-3", 3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	paused, err := NewBillRepo(db).PauseShopeeAutoSMLAfterConsecutiveProfileFailures(context.Background(), "bill-3", 3)
	if err != nil || paused != 1 {
		t.Fatalf("paused=%d err=%v", paused, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
