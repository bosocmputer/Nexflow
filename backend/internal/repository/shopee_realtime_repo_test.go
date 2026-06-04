package repository

import (
	"testing"
	"time"

	"nexflow/internal/models"
)

func TestShopeeSnapshotStatusGroupWhere(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  string
	}{
		{name: "all", group: "all", want: ""},
		{name: "empty", group: "", want: ""},
		{name: "unpaid", group: "unpaid", want: "s.order_status = 'UNPAID'"},
		{name: "to ship", group: "to_ship", want: "s.order_status = 'READY_TO_SHIP'"},
		{name: "shipping", group: "shipping", want: "s.order_status IN ('PROCESSED','SHIPPED')"},
		{name: "completed", group: "completed", want: "s.order_status = 'COMPLETED'"},
		{name: "cancelled", group: "cancelled", want: "s.order_status IN ('CANCELLED','IN_CANCEL')"},
		{name: "unknown", group: "bad", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeSnapshotStatusGroupWhere(tt.group); got != tt.want {
				t.Fatalf("where = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShopeeTimelineTitle(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		title  string
		status string
		want   string
	}{
		{name: "create done", kind: "create_document", status: "done", want: "สร้างเอกสารใน Nexflow แล้ว"},
		{name: "create blocked", kind: "create_document", status: "blocked", want: "สร้างเอกสารถูกบล็อก"},
		{name: "shipping reconcile", kind: "reconcile_shipping", want: "ตรวจสถานะจัดส่งจาก Shopee"},
		{name: "fallback title", kind: "push", title: "order_status_push", want: "order_status_push"},
		{name: "default", kind: "unknown", want: "Shopee update"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeTimelineTitle(tt.kind, tt.title, tt.status); got != tt.want {
				t.Fatalf("title = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShopeeTimelineSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		kind   string
		want   string
	}{
		{name: "tracking", kind: "tracking", want: "Seller Center"},
		{name: "push", kind: "push", want: "Push"},
		{name: "snapshot", kind: "snapshot", want: "Sync"},
		{name: "create document", kind: "create_document", want: "Nexflow"},
		{name: "source fallback", source: "Shopee Console", kind: "unknown", want: "Shopee Console"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeTimelineSource(tt.source, tt.kind); got != tt.want {
				t.Fatalf("source = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildShopeeStatusTimelineUsesSnapshotAsCurrentStatus(t *testing.T) {
	unpaidAt := time.Date(2026, 6, 4, 8, 18, 30, 0, time.UTC)
	readyAt := time.Date(2026, 6, 4, 8, 48, 30, 0, time.UTC)
	syncedAt := time.Date(2026, 6, 4, 10, 6, 29, 0, time.UTC)
	steps := buildShopeeStatusTimeline(&models.ShopeeOrderSnapshot{
		OrderStatus:       "READY_TO_SHIP",
		LastUpdateSource:  "shipping",
		LastOrderUpdateAt: &readyAt,
		LastSyncedAt:      syncedAt,
	}, map[string]shopeeStatusEvidence{
		"unpaid": {
			Status:     "UNPAID",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: &unpaidAt,
		},
		"to_ship": {
			Status:     "READY_TO_SHIP",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: &readyAt,
		},
		"shipping": {
			Status:     "SHIPPED",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: ptrTime(readyAt.Add(30 * time.Minute)),
		},
	})

	if got := stepByKey(steps, "to_ship"); got == nil || !got.Current || got.State != "current" {
		t.Fatalf("to_ship step = %+v, want current state", got)
	}
	if got := stepByKey(steps, "shipping"); got == nil || got.Current || got.State != "upcoming" {
		t.Fatalf("shipping step = %+v, want upcoming despite later push evidence", got)
	}
}

func TestBuildShopeeStatusTimelineDoesNotInventMissingHistory(t *testing.T) {
	completedAt := time.Date(2026, 6, 2, 7, 36, 27, 0, time.UTC)
	steps := buildShopeeStatusTimeline(&models.ShopeeOrderSnapshot{
		OrderStatus:       "COMPLETED",
		LastUpdateSource:  "sync",
		LastOrderUpdateAt: &completedAt,
		LastSyncedAt:      completedAt.Add(2 * time.Hour),
	}, nil)

	if got := stepByKey(steps, "completed"); got == nil || got.State != "current" || got.Confidence != "inferred" || got.Source != "sync" {
		t.Fatalf("completed step = %+v, want inferred sync current", got)
	}
	if got := stepByKey(steps, "unpaid"); got == nil || got.OccurredAt != nil || got.Confidence != "missing" {
		t.Fatalf("unpaid step = %+v, want missing time", got)
	}
}

func TestBuildShopeeStatusTimelineCancelledBranch(t *testing.T) {
	cancelAt := time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)
	steps := buildShopeeStatusTimeline(&models.ShopeeOrderSnapshot{
		OrderStatus:      "CANCELLED",
		LastUpdateSource: "push",
		LastSyncedAt:     cancelAt,
	}, map[string]shopeeStatusEvidence{
		"cancelled": {
			Status:     "CANCELLED",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: &cancelAt,
		},
	})

	if got := stepByKey(steps, "cancelled"); got == nil || !got.Current || !got.Terminal || got.State != "current" {
		t.Fatalf("cancelled step = %+v, want terminal current", got)
	}
	if got := stepByKey(steps, "to_ship"); got == nil || got.State != "skipped" {
		t.Fatalf("to_ship step = %+v, want skipped before cancellation without evidence", got)
	}
}

func stepByKey(steps []models.ShopeeOrderStatusTimelineStep, key string) *models.ShopeeOrderStatusTimelineStep {
	for i := range steps {
		if steps[i].Key == key {
			return &steps[i]
		}
	}
	return nil
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
