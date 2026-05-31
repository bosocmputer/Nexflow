package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestIMAPPollJobRepoCreateOrGetActiveCreatesJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIMAPPollJobRepo(db)
	accountID := "237024eb-441d-4e4d-845c-d842f75ccfa2"
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT (.+) FROM imap_poll_jobs").
		WithArgs(accountID).
		WillReturnRows(imapPollJobRows())
	mock.ExpectQuery("INSERT INTO imap_poll_jobs").
		WithArgs(accountID, "", "admin@nexflow.local").
		WillReturnRows(imapPollJobRows().AddRow(
			"job-1", accountID, "", "", "queued",
			0, 0, 0, 0, 0, 0, []byte(`{}`), []byte(`[]`), "",
			nil, "admin@nexflow.local", nil, nil, now, now,
		))
	mock.ExpectCommit()

	job, existing, err := repo.CreateOrGetActive(CreateIMAPPollJobInput{
		AccountID:      accountID,
		CreatedByEmail: "admin@nexflow.local",
	})
	if err != nil {
		t.Fatalf("CreateOrGetActive: %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	if job == nil || job.ID != "job-1" {
		t.Fatalf("job = %#v, want job-1", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestIMAPPollJobRepoCreateOrGetActiveReturnsExisting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewIMAPPollJobRepo(db)
	accountID := "237024eb-441d-4e4d-845c-d842f75ccfa2"
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT (.+) FROM imap_poll_jobs").
		WithArgs(accountID).
		WillReturnRows(imapPollJobRows().AddRow(
			"job-existing", accountID, "Shopee Inbox", "pd@example.com", "running",
			10, 4, 2, 1, 1, 6, []byte(`{"accepted":2}`), []byte(`[]`), "",
			nil, "admin@nexflow.local", now, nil, now, now,
		))
	mock.ExpectCommit()

	job, existing, err := repo.CreateOrGetActive(CreateIMAPPollJobInput{
		AccountID:      accountID,
		CreatedByEmail: "admin@nexflow.local",
	})
	if err != nil {
		t.Fatalf("CreateOrGetActive: %v", err)
	}
	if !existing {
		t.Fatal("existing = false, want true")
	}
	if job == nil || job.ID != "job-existing" {
		t.Fatalf("job = %#v, want job-existing", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func imapPollJobRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "account_id", "name", "username", "status",
		"total_count", "scanned_count", "created_count", "skipped_count",
		"failed_count", "backlog_count", "reason_counts", "latest_details",
		"last_error", "created_by", "created_by_email",
		"started_at", "finished_at", "created_at", "updated_at",
	})
}
