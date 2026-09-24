package tiktokshop

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
)

type tikTokLineShopLabelFake struct {
	label string
	err   error
}

func (f *tikTokLineShopLabelFake) ActiveShopLabel(context.Context, string) (string, error) {
	return f.label, f.err
}

type tikTokLineNotifierFake struct {
	calls     int
	input     models.TikTokShopNewOrderNotification
	dedupeKey string
	err       error
}

func (f *tikTokLineNotifierFake) EnqueueTikTokShopNewOrder(_ context.Context, input models.TikTokShopNewOrderNotification, dedupeKey string) (int, error) {
	f.calls++
	f.input = input
	f.dedupeKey = dedupeKey
	return 2, f.err
}

func TestTikTokNewOrderLineObserverQueuesEligibleOrderOncePerStableIdentity(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(time.Minute)
	notifier := &tikTokLineNotifierFake{}
	observer := NewTikTokNewOrderLineObserver(true, cutoff, &tikTokLineShopLabelFake{label: "henna_milkford"}, notifier, zap.NewNop())
	record := TikTokOrderSnapshotRecord{
		OrderID: "586030483469993439", OrderStatus: OrderStatusAwaitingShipment,
		Currency: "THB", PaymentTotal: "307.49", ProductSubtotal: "300.00", ShippingFee: "0.00",
		ItemCount: 2, SKUCount: 1, OrderCreatedAt: &createdAt, ObservationSource: "polling",
		NormalizedItems: []NormalizedTikTokOrderItem{{ProductID: "p1", SKUID: "s1", ProductName: "สินค้า A", SKUName: "แดง", Quantity: 2}},
	}
	if err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", record); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 || notifier.dedupeKey != "tiktok_shop:new_order:7494619203789490654:586030483469993439" {
		t.Fatalf("calls=%d dedupe=%q", notifier.calls, notifier.dedupeKey)
	}
	if notifier.input.ShopName != "henna_milkford" || notifier.input.ObservationSource != "polling" || len(notifier.input.Items) != 1 {
		t.Fatalf("input=%+v", notifier.input)
	}
}

func TestTikTokNewOrderLineObserverFailsClosedBeforeCutoffOrReadyStatus(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		enabled bool
		created *time.Time
		status  OrderStatus
	}{
		{name: "feature disabled", enabled: false, created: ptrTime(cutoff.Add(time.Minute)), status: OrderStatusAwaitingShipment},
		{name: "historical order", enabled: true, created: ptrTime(cutoff.Add(-time.Second)), status: OrderStatusAwaitingShipment},
		{name: "missing create time", enabled: true, status: OrderStatusAwaitingShipment},
		{name: "unpaid waits", enabled: true, created: ptrTime(cutoff.Add(time.Minute)), status: OrderStatusUnpaid},
		{name: "cancelled never alerts as new", enabled: true, created: ptrTime(cutoff.Add(time.Minute)), status: OrderStatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := &tikTokLineNotifierFake{}
			observer := NewTikTokNewOrderLineObserver(tt.enabled, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, notifier, zap.NewNop())
			err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{
				OrderID: "586030483469993439", OrderStatus: tt.status, OrderCreatedAt: tt.created,
			})
			if err != nil || notifier.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, notifier.calls)
			}
		})
	}
}

func TestTikTokNewOrderLineObserverSkipsSettlementBackfill(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(time.Minute)
	notifier := &tikTokLineNotifierFake{}
	observer := NewTikTokNewOrderLineObserver(true, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, notifier, zap.NewNop())
	if err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{
		OrderID: "586030483469993439", OrderStatus: OrderStatusAwaitingShipment, OrderCreatedAt: &createdAt, ObservationSource: "settlement_backfill",
	}); err != nil || notifier.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, notifier.calls)
	}
}

func TestTikTokNewOrderLineObserverPropagatesDurableEnqueueFailureForRetry(t *testing.T) {
	cutoff := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(time.Minute)
	wantErr := errors.New("outbox unavailable")
	observer := NewTikTokNewOrderLineObserver(true, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, &tikTokLineNotifierFake{err: wantErr}, zap.NewNop())
	err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{
		OrderID: "586030483469993439", OrderStatus: OrderStatusAwaitingShipment, OrderCreatedAt: &createdAt,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v, want %v", err, wantErr)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
