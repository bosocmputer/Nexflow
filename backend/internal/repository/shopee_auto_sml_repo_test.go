package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestAutoSMLListSettingsExcludesDisabledConnections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	mock.ExpectExec("INSERT INTO shopee_auto_sml_settings").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("JOIN shopee_api_connections c ON c.shop_id = st.shop_id AND c.disabled_at IS NULL").
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "shop_label", "enabled", "trigger_status", "config_version", "eligible_after", "route_signature", "enabled_by",
			"enabled_at", "paused_reason", "paused_at", "consecutive_system_failures", "last_success_at",
			"last_failure_at", "queued_count", "needs_review_count", "failed_count", "oldest_queued_at", "updated_at",
		}))

	settings, err := repo.ListSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 0 {
		t.Fatalf("settings = %d, want 0", len(settings))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAutoSMLUpdateSettingRejectsStaleConfigVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	expectedVersion := int64(6)
	trigger := models.ShopeeAutoSMLTriggerProcessed
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO shopee_auto_sml_settings").
		WithArgs(int64(264993963)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT enabled,trigger_status,config_version,route_signature,paused_reason").
		WithArgs(int64(264993963)).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "trigger_status", "config_version", "route_signature", "paused_reason"}).
			AddRow(true, models.ShopeeAutoSMLTriggerReadyToShip, int64(7), "route-v1", ""))
	mock.ExpectRollback()

	_, err = repo.UpdateSetting(t.Context(), ShopeeAutoSMLSettingUpdate{
		ShopID: 264993963, Enabled: true, TriggerStatus: &trigger,
		ExpectedConfigVersion: &expectedVersion, RouteSignature: "route-v1",
	})
	if !errors.Is(err, ErrShopeeAutoSMLConfigConflict) {
		t.Fatalf("UpdateSetting error = %v, want config conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAutoSMLUpdateSettingKeepsCurrentTriggerWhenLegacyClientOmitsIt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	shopID := int64(264993963)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO shopee_auto_sml_settings").WithArgs(shopID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT enabled,trigger_status,config_version,route_signature,paused_reason").WithArgs(shopID).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "trigger_status", "config_version", "route_signature", "paused_reason"}).
			AddRow(false, models.ShopeeAutoSMLTriggerProcessed, int64(3), "", ""))
	mock.ExpectExec("UPDATE shopee_auto_sml_settings").
		WithArgs(shopID, models.ShopeeAutoSMLTriggerProcessed, int64(4), "route-v2", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectExec("INSERT INTO shopee_auto_sml_settings").WithArgs(shopID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT st.shop_id").WithArgs(shopID).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "shop_label", "enabled", "trigger_status", "config_version", "eligible_after", "route_signature", "enabled_by",
			"enabled_at", "paused_reason", "paused_at", "consecutive_system_failures", "last_success_at",
			"last_failure_at", "queued_count", "needs_review_count", "failed_count", "oldest_queued_at", "updated_at",
		}).AddRow(
			shopID, "Henna.milkford", true, models.ShopeeAutoSMLTriggerProcessed, int64(4), time.Now(), "route-v2", nil,
			time.Now(), "", nil, 0, nil, nil, 0, 0, 0, nil, time.Now(),
		))

	setting, err := repo.UpdateSetting(t.Context(), ShopeeAutoSMLSettingUpdate{
		ShopID: shopID, Enabled: true, RouteSignature: "route-v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if setting.TriggerStatus != models.ShopeeAutoSMLTriggerProcessed || setting.ConfigVersion != 4 {
		t.Fatalf("setting trigger/version = %s/%d, want PROCESSED/4", setting.TriggerStatus, setting.ConfigVersion)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAutoSMLEnqueueSnapshotsTriggerTransitionAndConfigVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	createdAt := time.Date(2026, 8, 24, 6, 46, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 8, 24, 7, 16, 0, 0, time.UTC)
	processedAt := time.Date(2026, 8, 24, 7, 16, 0, 0, time.UTC)
	mock.ExpectExec("trigger_status_snapshot,trigger_transition_at,trigger_config_version").
		WithArgs(
			int64(264993963), "2608247BT82QQM", createdAt, &updatedAt,
			processedAt, "route-signature", models.ShopeeAutoSMLTriggerProcessed, int64(7),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	inserted, err := repo.Enqueue(
		context.Background(), 264993963, "2608247BT82QQM", createdAt, &updatedAt,
		processedAt, "route-signature", models.ShopeeAutoSMLTriggerProcessed, 7,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("expected job to be inserted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

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

func TestGetOrSetDocumentTimePersistsFirstValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeAutoSMLRepo(db)

	mock.ExpectQuery("UPDATE shopee_auto_sml_jobs.*document_time=CASE").
		WithArgs("job-1", "14:25").
		WillReturnRows(sqlmock.NewRows([]string{"document_time"}).AddRow("14:25"))

	got, err := repo.GetOrSetDocumentTime(t.Context(), "job-1", "14:25")
	if err != nil {
		t.Fatal(err)
	}
	if got != "14:25" {
		t.Fatalf("document time = %q, want 14:25", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
