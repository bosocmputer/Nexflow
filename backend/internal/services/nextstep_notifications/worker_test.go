package nextstepnotifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
	"nexflow/internal/services/sml"
)

func TestPollOnceSilentBaselineDoesNotNotify(t *testing.T) {
	w := testWorker([]sml.NextStepMarketplaceOrder{{
		DocNo:       "MQT26070001",
		DocDate:     "2026-07-07",
		Status:      "pending",
		TotalAmount: 1200,
	}})
	w.seenRepo.(*fakeSeenRepo).baselineCompleted = false
	line := &fakeLineNotifier{}
	w.WithLineNotifier(line)

	result, err := w.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if !result.Baseline || result.Notifications != 0 {
		t.Fatalf("result = %+v, want silent baseline", result)
	}
	if got := len(w.notifyRepo.(*fakeNotificationRepo).inputs); got != 0 {
		t.Fatalf("notifications created = %d, want 0", got)
	}
	if got := len(line.calls); got != 0 {
		t.Fatalf("line calls = %d, want 0", got)
	}
	if !w.seenRepo.(*fakeSeenRepo).markBaseline {
		t.Fatal("baseline was not marked complete")
	}
}

func TestPollOnceNewActionableOrderCreatesNotification(t *testing.T) {
	w := testWorker([]sml.NextStepMarketplaceOrder{{
		DocNo:       "MQT26070002",
		DocDate:     "2026-07-07",
		DocTime:     "12:34",
		Status:      "payment",
		TotalAmount: 1500,
	}})
	w.seenRepo.(*fakeSeenRepo).baselineCompleted = true

	result, err := w.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if result.Notifications != 1 || result.Inserted != 1 {
		t.Fatalf("result = %+v, want one notification", result)
	}
	notify := w.notifyRepo.(*fakeNotificationRepo)
	if len(notify.inputs) != 1 {
		t.Fatalf("inputs = %d, want 1", len(notify.inputs))
	}
	if notify.inputs[0].Source != "nextstep_marketplace" || notify.inputs[0].EntityID != "MQT26070002" {
		t.Fatalf("notification input = %+v", notify.inputs[0])
	}
	if notify.inputs[0].Title != "มีออเดอร์ NextStep Marketplace ใหม่" {
		t.Fatalf("notification title = %q", notify.inputs[0].Title)
	}
	seen := w.seenRepo.(*fakeSeenRepo)
	if len(seen.markNotified) != 1 || seen.markNotified[0] != "MQT26070002" {
		t.Fatalf("markNotified = %+v", seen.markNotified)
	}
	if got := len(w.broker.(*fakePublisher).events); got != 2 {
		t.Fatalf("published events = %d, want 2", got)
	}
	for _, ev := range w.broker.(*fakePublisher).events {
		if _, ok := ev.Payload["unread_by_source"]; !ok {
			t.Fatalf("event payload missing unread_by_source: %#v", ev.Payload)
		}
	}
}

func TestPollOnceNewActionableOrderEnqueuesLineNotification(t *testing.T) {
	w := testWorker([]sml.NextStepMarketplaceOrder{{
		DocNo:       "MQT26070006",
		DocDate:     "2026-07-07",
		DocTime:     "12:34",
		Status:      "pending",
		TotalAmount: 900,
	}})
	w.seenRepo.(*fakeSeenRepo).baselineCompleted = true
	line := &fakeLineNotifier{}
	w.WithLineNotifier(line)

	result, err := w.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if result.Notifications != 1 {
		t.Fatalf("result = %+v, want one notification", result)
	}
	if len(line.calls) != 1 {
		t.Fatalf("line calls = %d, want 1", len(line.calls))
	}
	if line.calls[0].order.DocNo != "MQT26070006" || line.calls[0].dedupeKey != "nextstep:new_order:MQT26070006" {
		t.Fatalf("line call = %+v", line.calls[0])
	}
}

func TestPollOnceLineNotificationFailureDoesNotFailPoll(t *testing.T) {
	w := testWorker([]sml.NextStepMarketplaceOrder{{
		DocNo:       "MQT26070007",
		DocDate:     "2026-07-07",
		Status:      "payment",
		TotalAmount: 900,
	}})
	w.seenRepo.(*fakeSeenRepo).baselineCompleted = true
	line := &fakeLineNotifier{err: errors.New("line queue down")}
	w.WithLineNotifier(line)

	result, err := w.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if result.Notifications != 1 {
		t.Fatalf("result = %+v, want in-app notification preserved", result)
	}
	seen := w.seenRepo.(*fakeSeenRepo)
	if len(seen.markNotified) != 1 || seen.markNotified[0] != "MQT26070007" {
		t.Fatalf("markNotified = %+v", seen.markNotified)
	}
	if len(line.calls) != 1 {
		t.Fatalf("line calls = %d, want attempted once", len(line.calls))
	}
}

func TestPollOnceSkipsSuccessAndCancel(t *testing.T) {
	w := testWorker([]sml.NextStepMarketplaceOrder{
		{DocNo: "MQT26070003", DocDate: "2026-07-07", Status: "success"},
		{DocNo: "PREQT26070004", DocDate: "2026-07-07", Status: "cancel"},
	})
	w.seenRepo.(*fakeSeenRepo).baselineCompleted = true
	line := &fakeLineNotifier{}
	w.WithLineNotifier(line)

	result, err := w.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if result.Notifications != 0 || result.SkippedInactive != 2 {
		t.Fatalf("result = %+v, want inactive skips", result)
	}
	if got := len(line.calls); got != 0 {
		t.Fatalf("line calls = %d, want 0", got)
	}
}

func TestPollOnceNotificationFailureAllowsRetry(t *testing.T) {
	w := testWorker([]sml.NextStepMarketplaceOrder{{
		DocNo:   "MQT26070005",
		DocDate: "2026-07-07",
		Status:  "pending",
	}})
	w.seenRepo.(*fakeSeenRepo).baselineCompleted = true
	w.notifyRepo.(*fakeNotificationRepo).err = errors.New("db down")

	_, err := w.PollOnce(context.Background())
	if err == nil {
		t.Fatal("PollOnce error = nil, want notification failure")
	}
	seen := w.seenRepo.(*fakeSeenRepo)
	if len(seen.deleted) != 1 || seen.deleted[0] != "MQT26070005" {
		t.Fatalf("deleted = %+v, want retry cleanup", seen.deleted)
	}
}

func testWorker(orders []sml.NextStepMarketplaceOrder) *Worker {
	w := NewWorker(
		&fakeMarketplaceClient{orders: orders},
		&fakeSeenRepo{baselineCompleted: true},
		&fakeNotificationRepo{},
		&fakePublisher{},
		nil,
	)
	w.now = func() time.Time {
		return time.Date(2026, 7, 7, 12, 0, 0, 0, time.FixedZone("Asia/Bangkok", 7*60*60))
	}
	w.pageCap = 1
	return w
}

type fakeMarketplaceClient struct {
	orders []sml.NextStepMarketplaceOrder
}

func (f *fakeMarketplaceClient) IsConfigured() bool { return true }

func (f *fakeMarketplaceClient) Fetch(context.Context, sml.NextStepMarketplaceRequest) (*sml.NextStepMarketplaceData, error) {
	return &sml.NextStepMarketplaceData{
		Orders: f.orders,
		Meta: sml.NextStepMarketplaceMeta{
			Total: len(f.orders),
		},
	}, nil
}

type fakeSeenRepo struct {
	baselineCompleted bool
	markBaseline      bool
	upserts           []repository.NextStepMarketplaceSeenInput
	insertedByDoc     map[string]bool
	markNotified      []string
	deleted           []string
}

func (f *fakeSeenRepo) BaselineCompleted(context.Context) (bool, error) {
	return f.baselineCompleted, nil
}

func (f *fakeSeenRepo) MarkBaselineCompleted(context.Context) error {
	f.markBaseline = true
	return nil
}

func (f *fakeSeenRepo) UpsertSeen(_ context.Context, in repository.NextStepMarketplaceSeenInput) (bool, error) {
	f.upserts = append(f.upserts, in)
	if f.insertedByDoc == nil {
		return true, nil
	}
	return f.insertedByDoc[in.DocNo], nil
}

func (f *fakeSeenRepo) MarkNotified(_ context.Context, docNo string) error {
	f.markNotified = append(f.markNotified, docNo)
	return nil
}

func (f *fakeSeenRepo) DeleteIfUnnotified(_ context.Context, docNo string) error {
	f.deleted = append(f.deleted, docNo)
	return nil
}

type fakeNotificationRepo struct {
	inputs []models.NotificationInput
	err    error
}

func (f *fakeNotificationRepo) CreateForRoles(_ context.Context, _ []string, in models.NotificationInput) ([]models.Notification, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return []models.Notification{{
		ID:          "notif-1",
		RecipientID: "user-1",
		Source:      in.Source,
		Severity:    in.Severity,
		Title:       in.Title,
		Body:        in.Body,
		ActionURL:   in.ActionURL,
		EntityType:  in.EntityType,
		EntityID:    in.EntityID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}}, nil
}

func (f *fakeNotificationRepo) UnreadCount(context.Context, string) (int, error) {
	return 1, nil
}

func (f *fakeNotificationRepo) UnreadCountsBySource(context.Context, string) (map[string]int, error) {
	return map[string]int{"nextstep_marketplace": 1}, nil
}

type fakePublisher struct {
	events []events.Event
}

func (f *fakePublisher) Publish(ev events.Event) {
	f.events = append(f.events, ev)
}

type fakeLineNotifier struct {
	calls []fakeLineCall
	err   error
}

type fakeLineCall struct {
	order     sml.NextStepMarketplaceOrder
	dedupeKey string
}

func (f *fakeLineNotifier) EnqueueNextStepMarketplaceNewOrder(_ context.Context, order sml.NextStepMarketplaceOrder, dedupeKey string) (int, error) {
	f.calls = append(f.calls, fakeLineCall{order: order, dedupeKey: dedupeKey})
	if f.err != nil {
		return 0, f.err
	}
	return 1, nil
}
