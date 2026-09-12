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
			"id", "gateway_event_id", "notification_id", "shop_id", "order_id", "event_order_status", "event_timestamp", "order_update_at", "attempts",
		}).AddRow(
			"33333333-3333-4333-8333-333333333333", "11111111-1111-4111-8111-111111111111", "7327112393057371910",
			"7494619203789490654", "576486316948490001", "UNPAID", now, now, 1,
		))
	job, err := NewTikTokWebhookStore(database).ClaimDue(t.Context(), now)
	if err != nil || job.OrderID != "576486316948490001" || job.Attempts != 1 {
		t.Fatalf("ClaimDue() job=%+v error=%v", job, err)
	}
}
