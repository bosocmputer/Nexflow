package tiktokshop

import (
	"context"
	"errors"
	"testing"
	"time"
)

type reconcileGatewayFake struct {
	responses map[string]*GatewayOrderSearchResponse
	inputs    []GatewayOrderSearchRequest
	err       error
}

func (f *reconcileGatewayFake) SearchOrders(_ context.Context, input GatewayOrderSearchRequest) (*GatewayOrderSearchResponse, error) {
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return nil, f.err
	}
	return f.responses[input.Search.PageToken], nil
}

type reconcileSnapshotterFake struct {
	inputs []TikTokOrderSnapshotRequest
	errAt  int
}

func (f *reconcileSnapshotterFake) Sync(_ context.Context, input TikTokOrderSnapshotRequest) (*TikTokOrderSnapshotResult, error) {
	f.inputs = append(f.inputs, input)
	if f.errAt > 0 && len(f.inputs) == f.errAt {
		return nil, errors.New("snapshot failed")
	}
	return &TikTokOrderSnapshotResult{ShopID: input.ShopID, SyncedCount: len(input.OrderIDs)}, nil
}

type reconcileStoreFake struct {
	run       TikTokOrderReconcileRun
	started   int
	progress  []TikTokOrderReconcileProgress
	completed int
	failed    int
	failCode  string
}

func (f *reconcileStoreFake) StartManualRun(_ context.Context, input TikTokOrderReconcileRequest) (TikTokOrderReconcileRun, error) {
	f.started++
	f.run.ShopID = input.ShopID
	f.run.WindowStart = time.Unix(input.UpdateTimeGE, 0).UTC()
	f.run.WindowEnd = time.Unix(input.UpdateTimeLT, 0).UTC()
	return f.run, nil
}

func (f *reconcileStoreFake) RecordPage(_ context.Context, _ string, progress TikTokOrderReconcileProgress) error {
	f.progress = append(f.progress, progress)
	return nil
}

func (f *reconcileStoreFake) CompleteRun(_ context.Context, _ string, _ TikTokOrderReconcileProgress) error {
	f.completed++
	return nil
}

func (f *reconcileStoreFake) FailRun(_ context.Context, _, code, _ string) error {
	f.failed++
	f.failCode = code
	return nil
}

func TestTikTokOrderReconcilerPaginatesAndAdvancesOnlyAfterFinalPage(t *testing.T) {
	gateway := &reconcileGatewayFake{responses: map[string]*GatewayOrderSearchResponse{
		"": {
			UpstreamRequestID: "search-1", NextPageToken: "opaque-next", TotalCount: 3,
			Orders: []Order{
				{ID: "1001", Status: OrderStatusAwaitingShipment, UpdateTime: 1_789_000_010},
				{ID: "1002", Status: OrderStatusCompleted, UpdateTime: 1_789_000_020},
			},
		},
		"opaque-next": {
			UpstreamRequestID: "search-2", TotalCount: 3,
			Orders: []Order{{ID: "1003", Status: OrderStatusCancelled, UpdateTime: 1_789_000_030}},
		},
	}}
	snapshots := &reconcileSnapshotterFake{}
	store := &reconcileStoreFake{run: TikTokOrderReconcileRun{ID: "run-1"}}
	service := NewTikTokOrderReconciler(gateway, snapshots, store)

	result, err := service.Reconcile(t.Context(), TikTokOrderReconcileRequest{
		ShopID: "7494619203789490654", UpdateTimeGE: 1_789_000_000, UpdateTimeLT: 1_789_000_100,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.PageCount != 2 || result.DiscoveredCount != 3 || result.SnapshottedCount != 3 {
		t.Fatalf("result = %+v", result)
	}
	if len(snapshots.inputs) != 2 || len(snapshots.inputs[0].OrderIDs) != 2 || snapshots.inputs[1].OrderIDs[0] != "1003" {
		t.Fatalf("snapshot inputs = %+v", snapshots.inputs)
	}
	if len(gateway.inputs) != 2 || gateway.inputs[0].Search.SortField != OrderSortFieldUpdateTime ||
		gateway.inputs[0].Search.SortOrder != OrderSortAscending || gateway.inputs[0].Search.PageSize != 20 ||
		gateway.inputs[1].Search.PageToken != "opaque-next" {
		t.Fatalf("search inputs = %+v", gateway.inputs)
	}
	if store.completed != 1 || store.failed != 0 || len(store.progress) != 2 {
		t.Fatalf("store completed=%d failed=%d progress=%+v", store.completed, store.failed, store.progress)
	}
}

func TestTikTokOrderReconcilerDoesNotAdvanceWatermarkAfterLaterPageFails(t *testing.T) {
	gateway := &reconcileGatewayFake{responses: map[string]*GatewayOrderSearchResponse{
		"": {UpstreamRequestID: "search-1", NextPageToken: "next", TotalCount: 2,
			Orders: []Order{{ID: "1001", Status: OrderStatusCompleted, UpdateTime: 1_789_000_010}}},
		"next": {UpstreamRequestID: "search-2", TotalCount: 2,
			Orders: []Order{{ID: "1002", Status: OrderStatusCompleted, UpdateTime: 1_789_000_020}}},
	}}
	snapshots := &reconcileSnapshotterFake{errAt: 2}
	store := &reconcileStoreFake{run: TikTokOrderReconcileRun{ID: "run-2"}}
	service := NewTikTokOrderReconciler(gateway, snapshots, store)

	_, err := service.Reconcile(t.Context(), TikTokOrderReconcileRequest{
		ShopID: "7494619203789490654", UpdateTimeGE: 1_789_000_000, UpdateTimeLT: 1_789_000_100,
	})
	if !errors.Is(err, ErrOrderReconcileFailed) {
		t.Fatalf("error = %v, want ErrOrderReconcileFailed", err)
	}
	if store.completed != 0 || store.failed != 1 || store.failCode != "snapshot_failed" || len(store.progress) != 1 {
		t.Fatalf("store completed=%d failed=%d code=%q progress=%+v", store.completed, store.failed, store.failCode, store.progress)
	}
}

func TestTikTokOrderReconcilerRejectsRepeatedPageToken(t *testing.T) {
	gateway := &reconcileGatewayFake{responses: map[string]*GatewayOrderSearchResponse{
		"": {UpstreamRequestID: "search-1", NextPageToken: "repeat", TotalCount: 1,
			Orders: []Order{{ID: "1001", Status: OrderStatusCompleted, UpdateTime: 1_789_000_010}}},
		"repeat": {UpstreamRequestID: "search-2", NextPageToken: "repeat", TotalCount: 1, Orders: []Order{}},
	}}
	store := &reconcileStoreFake{run: TikTokOrderReconcileRun{ID: "run-3"}}
	service := NewTikTokOrderReconciler(gateway, &reconcileSnapshotterFake{}, store)

	_, err := service.Reconcile(t.Context(), TikTokOrderReconcileRequest{
		ShopID: "7494619203789490654", UpdateTimeGE: 1_789_000_000, UpdateTimeLT: 1_789_000_100,
	})
	if !errors.Is(err, ErrOrderReconcileFailed) || store.failCode != "page_token_repeated" || store.completed != 0 {
		t.Fatalf("error=%v code=%q completed=%d", err, store.failCode, store.completed)
	}
}

func TestTikTokOrderReconcilerRejectsInvalidWindowBeforeCreatingRun(t *testing.T) {
	store := &reconcileStoreFake{run: TikTokOrderReconcileRun{ID: "run-4"}}
	service := NewTikTokOrderReconciler(&reconcileGatewayFake{}, &reconcileSnapshotterFake{}, store)

	_, err := service.Reconcile(t.Context(), TikTokOrderReconcileRequest{
		ShopID: "7494619203789490654", UpdateTimeGE: 1_789_000_000, UpdateTimeLT: 1_789_000_000 + int64((25 * time.Hour).Seconds()),
	})
	if !errors.Is(err, ErrInvalidOrderReconcileInput) || store.started != 0 {
		t.Fatalf("error=%v started=%d", err, store.started)
	}
}
