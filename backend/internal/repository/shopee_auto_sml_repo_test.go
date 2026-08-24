package repository

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestAutoSMLRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: time.Minute},
		{attempt: 2, want: 5 * time.Minute},
		{attempt: 3, want: 15 * time.Minute},
		{attempt: 8, want: 15 * time.Minute},
	}
	for _, tt := range tests {
		if got := autoSMLRetryDelay(tt.attempt); got != tt.want {
			t.Fatalf("attempt %d delay = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}

func TestTrimAutoSMLError(t *testing.T) {
	if got := trimAutoSMLError("  route_changed  ", 100); got != "route_changed" {
		t.Fatalf("trimmed = %q", got)
	}
	if got := trimAutoSMLError("abcdef", 3); got != "abc" {
		t.Fatalf("truncated = %q", got)
	}
}

func TestMarkTransientFailureSchedulesRetryWithoutOpeningCircuit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT shop_id,attempts FROM shopee_auto_sml_jobs WHERE id=$1::uuid FOR UPDATE")).
		WithArgs("job-1").
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "attempts"}).AddRow(int64(9), 1))
	mock.ExpectExec("UPDATE shopee_auto_sml_jobs").
		WithArgs("job-1", models.ShopeeAutoSMLRetryWait, int64(60), "timeout", "SML timeout").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	paused, terminal, err := repo.MarkTransientFailure(t.Context(), "job-1", "timeout", "SML timeout", 3)
	if err != nil {
		t.Fatal(err)
	}
	if paused || terminal {
		t.Fatalf("paused=%v terminal=%v, want retry only", paused, terminal)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkTransientFailurePausesShopAfterThreeTerminalJobs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT shop_id,attempts FROM shopee_auto_sml_jobs WHERE id=$1::uuid FOR UPDATE")).
		WithArgs("job-3").
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "attempts"}).AddRow(int64(9), 3))
	mock.ExpectExec("UPDATE shopee_auto_sml_jobs").
		WithArgs("job-3", models.ShopeeAutoSMLFailed, int64(900), "sml_5xx", "SML unavailable").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE shopee_auto_sml_settings").
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"consecutive_system_failures"}).AddRow(3))
	mock.ExpectExec("UPDATE shopee_auto_sml_settings").
		WithArgs(int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	paused, terminal, err := repo.MarkTransientFailure(t.Context(), "job-3", "sml_5xx", "SML unavailable", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !paused || !terminal {
		t.Fatalf("paused=%v terminal=%v, want both true", paused, terminal)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkContentionRetryDoesNotConsumeAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	mock.ExpectExec("UPDATE shopee_auto_sml_jobs.*attempts=GREATEST\\(attempts-1,0\\)").
		WithArgs("job-busy", "manual send in progress").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.MarkContentionRetry(t.Context(), "job-busy", "manual send in progress"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
