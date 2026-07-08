package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestNotificationRepoCreateForRolesReturnsInsertedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("INSERT INTO notifications").
		WithArgs(
			"shopee_realtime", "info", "มีออเดอร์ Shopee ใหม่", "body",
			"/shopee-operations", "shopee_order", "264993963:ORDER1", "shopee:new:264993963:ORDER1",
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "recipient_id", "source", "severity", "title", "body", "action_url",
			"entity_type", "entity_id", "dedupe_key", "read_at", "resolved_at", "resolved_reason", "created_at", "updated_at",
		}).AddRow(
			"notif-1", "user-1", "shopee_realtime", "info", "มีออเดอร์ Shopee ใหม่", "body",
			"/shopee-operations", "shopee_order", "264993963:ORDER1", "shopee:new:264993963:ORDER1",
			nil, nil, "", now, now,
		))

	repo := NewNotificationRepo(db)
	rows, err := repo.CreateForRoles(context.Background(), []string{"admin", "staff", "admin"}, models.NotificationInput{
		Source:     "shopee_realtime",
		Severity:   "info",
		Title:      "มีออเดอร์ Shopee ใหม่",
		Body:       "body",
		ActionURL:  "/shopee-operations",
		EntityType: "shopee_order",
		EntityID:   "264993963:ORDER1",
		DedupeKey:  "shopee:new:264993963:ORDER1",
	})
	if err != nil {
		t.Fatalf("CreateForRoles: %v", err)
	}
	if len(rows) != 1 || rows[0].RecipientID != "user-1" || rows[0].ReadAt != nil {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoCreateForRolesDuplicateReturnsNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO notifications").
		WithArgs("system", "warning", "ซ้ำ", "", "", "", "", "dedupe", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "recipient_id", "source", "severity", "title", "body", "action_url",
			"entity_type", "entity_id", "dedupe_key", "read_at", "resolved_at", "resolved_reason", "created_at", "updated_at",
		}))

	repo := NewNotificationRepo(db)
	rows, err := repo.CreateForRoles(context.Background(), []string{"staff"}, models.NotificationInput{
		Severity:  "warning",
		Title:     "ซ้ำ",
		DedupeKey: "dedupe",
	})
	if err != nil {
		t.Fatalf("CreateForRoles duplicate: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("duplicate returned rows: %#v", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoUnreadCountExcludesResolved(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	repo := NewNotificationRepo(db)
	n, err := repo.UnreadCount(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoUnreadCountsBySource(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT source, COUNT").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"source", "count"}).
			AddRow("shopee_realtime", 2).
			AddRow("nextstep_marketplace", 3))

	repo := NewNotificationRepo(db)
	counts, err := repo.UnreadCountsBySource(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("UnreadCountsBySource: %v", err)
	}
	if counts["shopee_realtime"] != 2 || counts["nextstep_marketplace"] != 3 {
		t.Fatalf("counts = %#v, want source split", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoResolveShopeeShopIssues(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("UPDATE notifications").
		WithArgs("264993963", "shop sync recovered", "shopee:sync_error:264993963:%", "shopee:token_error:264993963:%", "shopee:reconcile_error:264993963:%").
		WillReturnResult(sqlmock.NewResult(0, 3))

	repo := NewNotificationRepo(db)
	changed, err := repo.ResolveShopeeShopIssues(context.Background(), 264993963, "shop sync recovered")
	if err != nil {
		t.Fatalf("ResolveShopeeShopIssues: %v", err)
	}
	if changed != 3 {
		t.Fatalf("changed = %d, want 3", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoMarkReadIsUserScoped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("UPDATE notifications").
		WithArgs("user-1", "notif-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewNotificationRepo(db)
	changed, err := repo.MarkRead(context.Background(), "user-1", "notif-1")
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoMarkReadByEntityIsScoped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`UPDATE notifications\s+SET read_at = COALESCE\(read_at, NOW\(\)\),\s+updated_at = NOW\(\)\s+WHERE recipient_id = \$1\s+AND source = \$2\s+AND entity_type = \$3\s+AND entity_id = \$4\s+AND read_at IS NULL\s+AND resolved_at IS NULL`).
		WithArgs("user-1", "shopee_realtime", "shopee_order", "264993963:ORDER1").
		WillReturnResult(sqlmock.NewResult(0, 2))

	repo := NewNotificationRepo(db)
	changed, err := repo.MarkReadByEntity(context.Background(), "user-1", "shopee_realtime", "shopee_order", "264993963:ORDER1")
	if err != nil {
		t.Fatalf("MarkReadByEntity: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestNotificationRepoMarkReadByEntityNoopIsOK(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("UPDATE notifications").
		WithArgs("user-1", "shopee_realtime", "shopee_order", "264993963:ORDER1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewNotificationRepo(db)
	changed, err := repo.MarkReadByEntity(context.Background(), "user-1", "shopee_realtime", "shopee_order", "264993963:ORDER1")
	if err != nil {
		t.Fatalf("MarkReadByEntity no-op: %v", err)
	}
	if changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}
