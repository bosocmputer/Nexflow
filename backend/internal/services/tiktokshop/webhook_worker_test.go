package tiktokshop

import (
	"context"
	"errors"
	"testing"
	"time"
)

type webhookJobStoreFake struct {
	job       TikTokWebhookJob
	claimErr  error
	succeeded []string
	failed    []TikTokWebhookJob
}

func (s *webhookJobStoreFake) ClaimDue(context.Context, time.Time) (TikTokWebhookJob, error) {
	return s.job, s.claimErr
}
func (s *webhookJobStoreFake) MarkSucceeded(_ context.Context, id string) error {
	s.succeeded = append(s.succeeded, id)
	return nil
}
func (s *webhookJobStoreFake) MarkFailed(_ context.Context, job TikTokWebhookJob, _, _ string, _ time.Time) error {
	s.failed = append(s.failed, job)
	return nil
}

type webhookSnapshotterFake struct {
	input  TikTokOrderSnapshotRequest
	result *TikTokOrderSnapshotResult
	err    error
	calls  int
}

func (s *webhookSnapshotterFake) Sync(_ context.Context, input TikTokOrderSnapshotRequest) (*TikTokOrderSnapshotResult, error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func TestTikTokWebhookWorkerRefreshesExactOrder(t *testing.T) {
	store := &webhookJobStoreFake{job: TikTokWebhookJob{
		ID: "job-1", GatewayEventID: "event-1", NotificationID: "1", ShopID: "7494619203789490654",
		OrderID: "585684843131602849", Attempts: 1,
	}}
	snapshots := &webhookSnapshotterFake{result: &TikTokOrderSnapshotResult{
		ShopID: "7494619203789490654", SyncedCount: 1,
		Snapshots: []TikTokOrderSnapshotSummary{{OrderID: "585684843131602849"}},
	}}
	worker := NewTikTokWebhookWorker(true, store, snapshots, nil)
	processed, err := worker.ProcessOne(t.Context())
	if err != nil || !processed {
		t.Fatalf("ProcessOne() processed=%v error=%v", processed, err)
	}
	if snapshots.calls != 1 || snapshots.input.ShopID != store.job.ShopID || len(snapshots.input.OrderIDs) != 1 || snapshots.input.OrderIDs[0] != store.job.OrderID {
		t.Fatalf("snapshot input = %+v", snapshots.input)
	}
	if len(store.succeeded) != 1 || len(store.failed) != 0 {
		t.Fatalf("succeeded=%v failed=%v", store.succeeded, store.failed)
	}
}

func TestTikTokWebhookWorkerRetriesSafeSnapshotFailure(t *testing.T) {
	store := &webhookJobStoreFake{job: TikTokWebhookJob{ID: "job-1", ShopID: "1", OrderID: "2", Attempts: 1}}
	snapshots := &webhookSnapshotterFake{err: errors.New("upstream unavailable")}
	worker := NewTikTokWebhookWorker(true, store, snapshots, nil)
	processed, err := worker.ProcessOne(t.Context())
	if err != nil || !processed || len(store.failed) != 1 || len(store.succeeded) != 0 {
		t.Fatalf("processed=%v error=%v failed=%v succeeded=%v", processed, err, store.failed, store.succeeded)
	}
}

func TestTikTokWebhookWorkerStaysOffWhenGateDisabled(t *testing.T) {
	store := &webhookJobStoreFake{claimErr: errors.New("must not claim")}
	snapshots := &webhookSnapshotterFake{}
	processed, err := NewTikTokWebhookWorker(false, store, snapshots, nil).ProcessOne(t.Context())
	if processed || !errors.Is(err, ErrWebhookStoreNotConfigured) || snapshots.calls != 0 {
		t.Fatalf("processed=%v error=%v calls=%d", processed, err, snapshots.calls)
	}
}
