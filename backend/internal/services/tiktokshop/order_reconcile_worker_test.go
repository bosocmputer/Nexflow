package tiktokshop

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

type dueRunStoreFake struct {
	run   TikTokOrderReconcileRun
	err   error
	calls int
}

func (f *dueRunStoreFake) ClaimDueRun(context.Context, time.Time) (TikTokOrderReconcileRun, error) {
	f.calls++
	return f.run, f.err
}

type scheduledReconcilerFake struct {
	run   TikTokOrderReconcileRun
	calls int
	err   error
}

func (f *scheduledReconcilerFake) ExecuteScheduled(_ context.Context, run TikTokOrderReconcileRun) (*TikTokOrderReconcileResult, error) {
	f.calls++
	f.run = run
	return &TikTokOrderReconcileResult{RunID: run.ID, ShopID: run.ShopID}, f.err
}

func TestTikTokOrderReconcileWorkerExecutesOneClaimedRun(t *testing.T) {
	store := &dueRunStoreFake{run: TikTokOrderReconcileRun{ID: "run-1", ShopID: "7494619203789490654"}}
	reconciler := &scheduledReconcilerFake{}
	worker := NewTikTokOrderReconcileWorker(true, store, reconciler, nil)

	worker.runOnce(t.Context(), time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC))
	if store.calls != 1 || reconciler.calls != 1 || reconciler.run.ID != "run-1" {
		t.Fatalf("store calls=%d reconciler calls=%d run=%+v", store.calls, reconciler.calls, reconciler.run)
	}
}

func TestTikTokOrderReconcileWorkerDoesNothingWhenGloballyDisabledOrNoShopDue(t *testing.T) {
	store := &dueRunStoreFake{err: sql.ErrNoRows}
	reconciler := &scheduledReconcilerFake{}
	NewTikTokOrderReconcileWorker(false, store, reconciler, nil).runOnce(t.Context(), time.Now())
	if store.calls != 0 || reconciler.calls != 0 {
		t.Fatalf("disabled worker called store=%d reconciler=%d", store.calls, reconciler.calls)
	}

	NewTikTokOrderReconcileWorker(true, store, reconciler, nil).runOnce(t.Context(), time.Now())
	if store.calls != 1 || reconciler.calls != 0 {
		t.Fatalf("idle worker called store=%d reconciler=%d", store.calls, reconciler.calls)
	}
}
