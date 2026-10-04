package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func lineRecipientRows(eventKeys string, enabled bool) *sqlmock.Rows {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id", "line_oa_id", "line_oa_name", "name", "destination_type", "destination_id", "enabled", "event_keys",
		"last_test_at", "last_test_status", "last_test_error", "last_sent_at", "last_error", "created_at", "updated_at",
	}).AddRow("recipient-1", "oa-1", "Nexflow OA", "คลัง", "user", "U123", enabled, eventKeys, nil, "", "", nil, "", now, now)
}

func TestLineNotificationRepoCreateRecipientUsesOperationalDefaultSubscriptions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO line_notification_recipients").
		WithArgs("oa-1", "คลัง", "user", "U123", true).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("recipient-1"))
	mock.ExpectExec("INSERT INTO line_notification_recipient_subscriptions").
		WithArgs("recipient-1", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, int64(len(models.AllLineNotificationEventKeys()))))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT .*event_keys.*FROM line_notification_recipients r").
		WithArgs("recipient-1").
		WillReturnRows(lineRecipientRows(`{"shopee.order.new","tiktok.order.new"}`, true))

	repo := NewLineNotificationRepo(db)
	row, err := repo.CreateRecipient(context.Background(), models.LineNotificationRecipientUpsert{
		LineOAID: "oa-1", Name: "คลัง", DestinationType: "user", DestinationID: "U123",
	})
	if err != nil {
		t.Fatalf("CreateRecipient: %v", err)
	}
	if len(row.EventKeys) != 2 || row.EventKeys[0] != models.LineNotificationEventShopeeOrderNew {
		t.Fatalf("event keys = %#v", row.EventKeys)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLineNotificationRepoUpdateRecipientSuppressesDisabledQueuedEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	currentKeys := `{"shopee.order.new","tiktok.order.new"}`
	mock.ExpectQuery("SELECT .*event_keys.*FROM line_notification_recipients r").WithArgs("recipient-1").WillReturnRows(lineRecipientRows(currentKeys, true))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE line_notification_recipients").WithArgs("oa-1", "คลัง", "user", "U123", true, "recipient-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO line_notification_recipient_subscriptions").WithArgs("recipient-1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE line_notification_deliveries").WithArgs("recipient-1", true, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT .*event_keys.*FROM line_notification_recipients r").WithArgs("recipient-1").WillReturnRows(lineRecipientRows(`{"shopee.order.new"}`, true))

	keys := []string{models.LineNotificationEventShopeeOrderNew}
	repo := NewLineNotificationRepo(db)
	row, err := repo.UpdateRecipient(context.Background(), "recipient-1", models.LineNotificationRecipientUpsert{
		LineOAID: "oa-1", Name: "คลัง", DestinationType: "user", DestinationID: "U123", EventKeys: &keys,
	})
	if err != nil {
		t.Fatalf("UpdateRecipient: %v", err)
	}
	if len(row.EventKeys) != 1 || row.EventKeys[0] != models.LineNotificationEventShopeeOrderNew {
		t.Fatalf("event keys = %#v", row.EventKeys)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLineNotificationRepoEnqueueCreatesOneDeliveryPerEnabledRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("(?s)INSERT INTO line_notification_deliveries.*JOIN line_notification_recipient_subscriptions sub.*sub.enabled = TRUE").
		WithArgs(
			"shopee.order.new", "shopee_realtime", "info", "มีออเดอร์ Shopee ใหม่", "body",
			"/shopee-operations?order=ORDER1", "shopee_order", "264993963:ORDER1",
			"shopee:new_order:264993963:ORDER1", "message", "", "{}", 0,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("delivery-1"))

	repo := NewLineNotificationRepo(db)
	n, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		EventKey:    models.LineNotificationEventShopeeOrderNew,
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

func TestLineNotificationRepoLeaseRechecksRecipientSubscription(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("(?s)WITH picked AS .*JOIN line_notification_recipient_subscriptions sub.*sub.enabled = TRUE").
		WithArgs(3, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewLineNotificationRepo(db)
	jobs, err := repo.LeaseDeliveries(context.Background(), 10, 3)
	if err != nil {
		t.Fatalf("LeaseDeliveries: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %#v", jobs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLineNotificationRepoEnqueueDuplicateReturnsZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("INSERT INTO line_notification_deliveries").
		WithArgs("shopee.order.new", "shopee_realtime", "info", "ซ้ำ", "", "", "", "", "dedupe", "message", "", "{}", 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := NewLineNotificationRepo(db)
	n, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		EventKey:    models.LineNotificationEventShopeeOrderNew,
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
			"shopee.settlement.ready", "shopee_settlement", "warning", "Shopee settlement พร้อมตรวจยอด", "body",
			"/shopee-settlements", "shopee_settlement", "run-1", "shopee:settlement:run-1",
			"fallback", "alt", string(payload), 2,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("delivery-1"))

	repo := NewLineNotificationRepo(db)
	n, err := repo.Enqueue(context.Background(), models.LineNotificationMessageInput{
		EventKey:       models.LineNotificationEventShopeeSettlementReady,
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

func TestValidateLineNotificationDestination(t *testing.T) {
	tests := []struct {
		name            string
		destinationType string
		destinationID   string
		wantErr         bool
	}{
		{name: "user id", destinationType: "user", destinationID: "U1234567890"},
		{name: "group id", destinationType: "group", destinationID: "C1234567890"},
		{name: "room id", destinationType: "room", destinationID: "R1234567890"},
		{name: "default user", destinationType: "", destinationID: "U1234567890"},
		{name: "user with group prefix", destinationType: "user", destinationID: "C1234567890", wantErr: true},
		{name: "group with user prefix", destinationType: "group", destinationID: "U1234567890", wantErr: true},
		{name: "room empty", destinationType: "room", destinationID: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLineNotificationDestination(tt.destinationType, tt.destinationID)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidLineNotificationDestination) {
					t.Fatalf("err = %v, want ErrInvalidLineNotificationDestination", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateLineNotificationDestination: %v", err)
			}
		})
	}
}
