package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestTikTokAutoSMLEnqueueRequiresEnabledPostCutoffExactTrigger(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewTikTokAutoSMLRepo(db)
	transition := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	mock.ExpectExec("INSERT INTO tiktok_shop_auto_sml_jobs").
		WithArgs("7494619203789490654", "586030483469993439", models.TikTokAutoSMLTriggerAwaitingCollection, transition, string64("a"), string64("c"), string64("b")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	inserted, err := repo.Enqueue(context.Background(), TikTokAutoSMLEnqueueInput{
		ShopID: "7494619203789490654", OrderID: "586030483469993439",
		OrderStatus: models.TikTokAutoSMLTriggerAwaitingCollection, TriggerTransitionAt: transition,
		SourceHash: string64("a"), BillFingerprint: string64("c"), RouteSignature: string64("b"),
	})
	if err != nil || !inserted {
		t.Fatalf("inserted=%v err=%v", inserted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokAutoSMLUpdateSettingRejectsStaleVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewTikTokAutoSMLRepo(db)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO tiktok_shop_auto_sml_settings").WithArgs("7494619203789490654").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT config_version").WithArgs("7494619203789490654").WillReturnRows(sqlmock.NewRows([]string{"config_version"}).AddRow(int64(4)))
	mock.ExpectRollback()

	_, err = repo.UpdateSetting(t.Context(), TikTokAutoSMLSettingUpdate{
		ShopID: "7494619203789490654", AutoBillEnabled: true, ExpectedConfigVersion: 3,
		RouteSignature: string64("b"), UserID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	})
	if !errors.Is(err, ErrTikTokAutoSMLConfigConflict) {
		t.Fatalf("err=%v, want config conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokAutoSMLListSettingsIsReadOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewTikTokAutoSMLRepo(db)

	// Opening an Operations page must not create a default setting row. New rows
	// are created only by the explicit update path or the connection migration.
	mock.ExpectQuery("SELECT c.shop_id").WillReturnRows(sqlmock.NewRows([]string{"shop_id"}))
	settings, err := repo.ListSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 0 {
		t.Fatalf("settings=%+v, want empty", settings)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokAutoSMLRetryRefreshesReviewedEvidenceWithoutCreatingAnotherJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewTikTokAutoSMLRepo(db)
	mock.ExpectExec("UPDATE tiktok_shop_auto_sml_jobs j").
		WithArgs("7494619203789490654", "586030483469993439", string64("c"), string64("b")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.RetryJob(t.Context(), "7494619203789490654", "586030483469993439", string64("c"), string64("b")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokAutoSMLMarkBillCreatedStopsBeforeManualSML(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewTikTokAutoSMLRepo(db)
	jobID := "65b124d5-570d-48d4-9741-d22f1f46f1ef"
	billID := "80834efe-8bc3-4109-b270-a139d418f747"
	mock.ExpectExec("UPDATE tiktok_shop_auto_sml_jobs SET status='bill_created'").
		WithArgs(jobID, billID, string64("d")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkBillCreated(t.Context(), jobID, billID, string64("d")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func string64(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
