package tiktokshop

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
)

type tikTokCancellationBillLookupFake struct {
	docNo string
	err   error
}

func (f *tikTokCancellationBillLookupFake) ExistingBillSMLDocNo(context.Context, string) (string, error) {
	return f.docNo, f.err
}

type tikTokCancellationLineNotifierFake struct {
	calls     int
	input     models.TikTokShopOrderCancellationNotification
	dedupeKey string
	err       error
}

func (f *tikTokCancellationLineNotifierFake) EnqueueTikTokShopOrderCancelled(_ context.Context, input models.TikTokShopOrderCancellationNotification, dedupeKey string) (int, error) {
	f.calls++
	f.input, f.dedupeKey = input, dedupeKey
	return 2, f.err
}

func TestTikTokCancellationLineObserverQueuesOnlyFreshCancelledOrder(t *testing.T) {
	cutoff := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	updated := cutoff.Add(time.Minute)
	notifier := &tikTokCancellationLineNotifierFake{}
	observer := NewTikTokCancellationLineObserver(true, cutoff, &tikTokLineShopLabelFake{label: "henna_milkford"}, &tikTokCancellationBillLookupFake{docNo: "BF-INV26090001"}, notifier, zap.NewNop())
	err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{
		OrderID: "586291320330093597", OrderStatus: OrderStatusCancelled, Currency: "THB", PaymentTotal: "307.49", ItemCount: 1, SKUCount: 1,
		LastOrderUpdateAt: &updated, ObservationSource: "webhook",
	})
	if err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 || notifier.dedupeKey != "tiktok_shop:cancelled:7494619203789490654:586291320330093597" {
		t.Fatalf("calls=%d dedupe=%q", notifier.calls, notifier.dedupeKey)
	}
	if notifier.input.SMLDocNo != "BF-INV26090001" || notifier.input.ShopName != "henna_milkford" {
		t.Fatalf("input=%+v", notifier.input)
	}
}

func TestTikTokCancellationLineObserverFailsClosedForOldOrUnsafeSnapshots(t *testing.T) {
	cutoff := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	old := cutoff.Add(-time.Second)
	fresh := cutoff.Add(time.Minute)
	for _, tc := range []struct {
		name    string
		enabled bool
		status  OrderStatus
		updated *time.Time
		source  string
	}{
		{"disabled", false, OrderStatusCancelled, &fresh, "webhook"},
		{"historical", true, OrderStatusCancelled, &old, "polling"},
		{"no update time", true, OrderStatusCancelled, nil, "polling"},
		{"not cancelled", true, OrderStatusAwaitingShipment, &fresh, "webhook"},
		{"settlement backfill", true, OrderStatusCancelled, &fresh, "settlement_backfill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := &tikTokCancellationLineNotifierFake{}
			observer := NewTikTokCancellationLineObserver(tc.enabled, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, &tikTokCancellationBillLookupFake{}, notifier, zap.NewNop())
			err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{OrderID: "586291320330093597", OrderStatus: tc.status, LastOrderUpdateAt: tc.updated, ObservationSource: tc.source})
			if err != nil || notifier.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, notifier.calls)
			}
		})
	}
}

func TestTikTokCancellationLineObserverPropagatesDurableEnqueueFailure(t *testing.T) {
	cutoff := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	updated := cutoff.Add(time.Minute)
	wantErr := errors.New("outbox unavailable")
	observer := NewTikTokCancellationLineObserver(true, cutoff, &tikTokLineShopLabelFake{label: "AOY"}, &tikTokCancellationBillLookupFake{}, &tikTokCancellationLineNotifierFake{err: wantErr}, zap.NewNop())
	err := observer.ObserveTikTokOrderSnapshot(t.Context(), "7494619203789490654", TikTokOrderSnapshotRecord{OrderID: "586291320330093597", OrderStatus: OrderStatusCancelled, LastOrderUpdateAt: &updated})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v, want %v", err, wantErr)
	}
}
