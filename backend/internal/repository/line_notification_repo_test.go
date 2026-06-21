package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestLineNotificationRepoEnqueueCreatesOneDeliveryPerEnabledRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO line_notification_deliveries").
		WithArgs(
			"shopee_realtime", "info", "มีออเดอร์ Shopee ใหม่", "body",
			"/shopee-operations?order=ORDER1", "shopee_order", "264993963:ORDER1",
			"shopee:new_order:264993963:ORDER1", "message", "", "{}", 0,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("delivery-1"))

	repo := NewLineNotificationRepo(db)
	n, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		Source:      "shopee_realtime",
		Severity:    "info",
		Title:       "มีออเดอร์ Shopee ใหม่",
		Body:        "body",
		ActionURL:   "/shopee-operations?order=ORDER1",
		EntityType:  "shopee_order",
		EntityID:    "264993963:ORDER1",
		DedupeKey:   "shopee:new_order:264993963:ORDER1",
		MessageText: "message",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if n != 1 {
		t.Fatalf("inserted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestLineNotificationRepoEnqueueDuplicateReturnsZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO line_notification_deliveries").
		WithArgs("shopee_realtime", "info", "ซ้ำ", "", "", "", "", "dedupe", "message", "", "{}", 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := NewLineNotificationRepo(db)
	n, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		Title:       "ซ้ำ",
		DedupeKey:   "dedupe",
		MessageText: "message",
	})
	if err != nil {
		t.Fatalf("Enqueue duplicate: %v", err)
	}
	if n != 0 {
		t.Fatalf("inserted = %d, want 0", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestLineNotificationRepoEnqueueStoresStructuredFlexPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	payload := json.RawMessage(`{"type":"bubble"}`)
	mock.ExpectQuery("INSERT INTO line_notification_deliveries").
		WithArgs(
			"shopee_settlement", "warning", "Shopee settlement พร้อมตรวจยอด", "body",
			"/shopee-settlements", "shopee_settlement", "run-1", "shopee:settlement:run-1",
			"fallback", "alt", string(payload), 2,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("delivery-1"))

	repo := NewLineNotificationRepo(db)
	n, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		Source:         "shopee_settlement",
		Severity:       "warning",
		Title:          "Shopee settlement พร้อมตรวจยอด",
		Body:           "body",
		ActionURL:      "/shopee-settlements",
		EntityType:     "shopee_settlement",
		EntityID:       "run-1",
		DedupeKey:      "shopee:settlement:run-1",
		MessageText:    "fallback",
		AltText:        "alt",
		FlexPayload:    payload,
		PayloadVersion: 2,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if n != 1 {
		t.Fatalf("inserted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestLineNotificationRepoEnqueueRejectsIncompleteMessage(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewLineNotificationRepo(db)
	if _, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		Title:     "มีออเดอร์ Shopee ใหม่",
		DedupeKey: "dedupe",
	}); err == nil {
		t.Fatal("expected incomplete message error")
	}
}
