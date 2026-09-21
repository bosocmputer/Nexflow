package tiktokshop

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/events"
)

type tikTokNotificationRepoFake struct {
	calls    int
	roles    []string
	input    models.NotificationInput
	created  []models.Notification
	err      error
	unread   map[string]int
	bySource map[string]map[string]int
}

func (f *tikTokNotificationRepoFake) CreateForRoles(_ context.Context, roles []string, input models.NotificationInput) ([]models.Notification, error) {
	f.calls++
	f.roles = append([]string(nil), roles...)
	f.input = input
	return f.created, f.err
}

func (f *tikTokNotificationRepoFake) UnreadCount(_ context.Context, recipientID string) (int, error) {
	return f.unread[recipientID], nil
}

func (f *tikTokNotificationRepoFake) UnreadCountsBySource(_ context.Context, recipientID string) (map[string]int, error) {
	return f.bySource[recipientID], nil
}

type tikTokEventPublisherFake struct {
	events []events.Event
}

func (f *tikTokEventPublisherFake) Publish(event events.Event) {
	f.events = append(f.events, event)
}

func TestTikTokNewOrderInAppObserverCreatesRoleScopedNotificationAndPublishesSSE(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(time.Minute)
	repo := &tikTokNotificationRepoFake{
		created: []models.Notification{
			{ID: "notification-1", RecipientID: "admin-1"},
			{ID: "notification-2", RecipientID: "staff-1"},
		},
		unread: map[string]int{"admin-1": 3, "staff-1": 1},
		bySource: map[string]map[string]int{
			"admin-1": {"tiktok_shop": 1, "shopee_realtime": 2},
			"staff-1": {"tiktok_shop": 1},
		},
	}
	publisher := &tikTokEventPublisherFake{}
	observer := NewTikTokNewOrderInAppObserver(
		true,
		cutoff,
		&tikTokLineShopLabelFake{label: "henna_milkford"},
		repo,
		publisher,
		zap.NewNop(),
	)

	err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{
		OrderID: "586030483469993439", OrderStatus: OrderStatusAwaitingShipment,
		ItemCount: 2, SKUCount: 1, OrderCreatedAt: &createdAt, ObservationSource: "polling",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 {
		t.Fatalf("repo calls=%d", repo.calls)
	}
	if len(repo.roles) != 2 || repo.roles[0] != "admin" || repo.roles[1] != "staff" {
		t.Fatalf("roles=%v", repo.roles)
	}
	wantEntityID := "7494619203789490654:586030483469993439"
	if repo.input.Source != "tiktok_shop" || repo.input.Severity != "warning" ||
		repo.input.EntityType != "tiktok_shop_order" || repo.input.EntityID != wantEntityID ||
		repo.input.DedupeKey != "tiktok_shop:new_order:"+wantEntityID ||
		repo.input.ActionURL != "/tiktok-shop-operations?order_id=586030483469993439" {
		t.Fatalf("input=%+v", repo.input)
	}
	if repo.input.Title != "มีออเดอร์ TikTok Shop ใหม่" || repo.input.Body != "henna_milkford · ออเดอร์ 586030483469993439 · 2 รายการ" {
		t.Fatalf("title=%q body=%q", repo.input.Title, repo.input.Body)
	}
	if len(publisher.events) != 4 {
		t.Fatalf("events=%d, want 4", len(publisher.events))
	}
	if publisher.events[0].Type != events.TypeNotificationCreated || publisher.events[0].TargetUserID != "admin-1" {
		t.Fatalf("first event=%+v", publisher.events[0])
	}
	if publisher.events[1].Type != events.TypeNotificationUnreadChanged || publisher.events[1].Payload["total"] != 3 {
		t.Fatalf("second event=%+v", publisher.events[1])
	}
}

func TestTikTokNewOrderInAppObserverDedupeReplayPublishesNoSSE(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(time.Minute)
	repo := &tikTokNotificationRepoFake{created: []models.Notification{}}
	publisher := &tikTokEventPublisherFake{}
	observer := NewTikTokNewOrderInAppObserver(true, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, repo, publisher, zap.NewNop())

	if err := observer.ObserveTikTokOrderSnapshot(t.Context(), "shop-1", TikTokOrderSnapshotRecord{
		OrderID: "order-1", OrderStatus: OrderStatusAwaitingShipment, OrderCreatedAt: &createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || len(publisher.events) != 0 {
		t.Fatalf("repo calls=%d events=%d", repo.calls, len(publisher.events))
	}
}

func TestTikTokNewOrderInAppObserverFailsClosedForIneligibleOrders(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		enabled bool
		created *time.Time
		status  OrderStatus
	}{
		{name: "feature disabled", created: ptrTime(cutoff.Add(time.Minute)), status: OrderStatusAwaitingShipment},
		{name: "historical order", enabled: true, created: ptrTime(cutoff.Add(-time.Second)), status: OrderStatusAwaitingShipment},
		{name: "missing create time", enabled: true, status: OrderStatusAwaitingShipment},
		{name: "unpaid", enabled: true, created: ptrTime(cutoff.Add(time.Minute)), status: OrderStatusUnpaid},
		{name: "cancelled", enabled: true, created: ptrTime(cutoff.Add(time.Minute)), status: OrderStatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tikTokNotificationRepoFake{}
			observer := NewTikTokNewOrderInAppObserver(tt.enabled, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, repo, &tikTokEventPublisherFake{}, zap.NewNop())
			err := observer.ObserveTikTokOrderSnapshot(t.Context(), "shop-1", TikTokOrderSnapshotRecord{
				OrderID: "order-1", OrderStatus: tt.status, OrderCreatedAt: tt.created,
			})
			if err != nil || repo.calls != 0 {
				t.Fatalf("err=%v repo calls=%d", err, repo.calls)
			}
		})
	}
}

func TestTikTokNewOrderInAppObserverPropagatesCreateFailureForRetry(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(time.Minute)
	wantErr := errors.New("notifications unavailable")
	observer := NewTikTokNewOrderInAppObserver(true, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, &tikTokNotificationRepoFake{err: wantErr}, &tikTokEventPublisherFake{}, zap.NewNop())

	err := observer.ObserveTikTokOrderSnapshot(t.Context(), "shop-1", TikTokOrderSnapshotRecord{
		OrderID: "order-1", OrderStatus: OrderStatusAwaitingShipment, OrderCreatedAt: &createdAt,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v, want %v", err, wantErr)
	}
}
