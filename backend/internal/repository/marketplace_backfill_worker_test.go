package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMarketplaceBackfillEnsureJobsIsDurableAndOrdered(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO marketplace_conversion_readiness`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO marketplace_backfill_jobs`).
		WithArgs("", "alias_conversion", "initial-v1:alias_conversion").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO marketplace_backfill_jobs`).
		WithArgs("", "bill_snapshots", "initial-v1:bill_snapshots").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO marketplace_backfill_jobs`).WithArgs("").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	worker := NewMarketplaceBackfillWorker(NewMarketplaceAliasRepo(db), true, true, nil)
	if err := worker.ensureJobs(t.Context()); err != nil {
		t.Fatalf("ensureJobs: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceBackfillClaimUsesSkipLockedAndDependencies(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`(?s)WITH candidate AS.*job_type='bill_snapshots'.*job_type='alias_conversion'.*FOR UPDATE SKIP LOCKED.*UPDATE marketplace_backfill_jobs`).
		WithArgs("backfill-1", int64(300), true).
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_type", "cursor_id", "lease_owner", "attempt_count"}).
			AddRow("job-1", "alias_conversion", nil, "backfill-1", 1))

	worker := NewMarketplaceBackfillWorker(NewMarketplaceAliasRepo(db), true, true, nil)
	job, err := worker.claim(t.Context(), "backfill-1", 300_000_000_000)
	if err != nil || job == nil || job.JobType != "alias_conversion" {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
