package tiktokshop

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTikTokWebhookStoreIngestsOnceForActiveShop(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT gateway_connection_id::text").WithArgs("7494619203789490654").
		WillReturnRows(sqlmock.NewRows([]string{"gateway_connection_id"}).AddRow("22222222-2222-4222-8222-222222222222"))
	mock.ExpectExec("INSERT INTO tiktok_shop_webhook_events").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	inserted, err := NewTikTokWebhookStore(database).Ingest(t.Context(), GatewayWebhookDelivery{
		GatewayEventID: "11111111-1111-4111-8111-111111111111", NotificationID: "7327112393057371910",
		ShopID: "7494619203789490654", OrderID: "576486316948490001", OrderStatus: "UNPAID",
		Timestamp: "2026-09-12T10:00:00Z", OrderUpdateAt: "2026-09-12T10:00:00Z",
	})
	if err != nil || !inserted {
		t.Fatalf("Ingest() inserted=%v error=%v", inserted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokWebhookStoreClaimsExactOrderJob(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, time.September, 12, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("WITH recovered AS").WithArgs(now, maxTikTokWebhookAttempts).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "gateway_event_id", "notification_id", "notification_type", "shop_id", "order_id", "event_order_status",
			"cancellation_status", "cancellation_id", "cancellation_role", "cancellation_created_at", "event_timestamp", "order_update_at", "attempts",
		}).AddRow(
			"33333333-3333-4333-8333-333333333333", "11111111-1111-4111-8111-111111111111", "7327112393057371910",
			1, "7494619203789490654", "576486316948490001", "UNPAID", "", "", "", nil, now, now, 1,
		))
	job, err := NewTikTokWebhookStore(database).ClaimDue(t.Context(), now)
	if err != nil || job.OrderID != "576486316948490001" || job.Attempts != 1 {
		t.Fatalf("ClaimDue() job=%+v error=%v", job, err)
	}
}

func TestPrepareWebhookDeliveryAcceptsTypedCancellation(t *testing.T) {
	job, err := prepareWebhookDelivery(GatewayWebhookDelivery{
		GatewayEventID: "11111111-1111-4111-8111-111111111111", NotificationID: "7327112393057371910", NotificationType: 11,
		ShopID: "7494619203789490654", OrderID: "576486316948490001", CancellationStatus: "CANCELLATION_REQUEST_SUCCESS",
		CancellationID: "987654321", CancellationRole: "BUYER", CancellationCreatedAt: "2026-09-12T10:00:00Z",
		Timestamp: "2026-09-12T10:00:00Z", OrderUpdateAt: "2026-09-12T10:00:00Z",
	})
	if err != nil || job.NotificationType != 11 || job.CancellationStatus != "CANCELLATION_REQUEST_SUCCESS" || job.OrderStatus != "" {
		t.Fatalf("prepareWebhookDelivery() job=%+v error=%v", job, err)
	}
}
