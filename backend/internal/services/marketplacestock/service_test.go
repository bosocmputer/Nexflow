package marketplacestock

import (
	"context"
	"testing"
	"time"

	"nexflow/internal/services/sml"
)

type serviceStoreFake struct{ createCalls int }

func (f *serviceStoreFake) Overview(context.Context) (*Overview, error) { return &Overview{}, nil }
func (f *serviceStoreFake) Candidates(context.Context, string) ([]Candidate, error) {
	return []Candidate{}, nil
}
func (f *serviceStoreFake) CreatePool(_ context.Context, input PoolInput, _ string) (*Pool, error) {
	f.createCalls++
	return &Pool{SMLItemCode: input.SMLItemCode}, nil
}
func (f *serviceStoreFake) UpdatePool(context.Context, string, PoolUpdate, string) (*Pool, error) {
	return &Pool{}, nil
}
func (f *serviceStoreFake) ArchivePool(context.Context, string, PoolArchive, string) error {
	return nil
}
func (f *serviceStoreFake) UpdateSettings(context.Context, SettingsUpdate, string) (*Settings, error) {
	return &Settings{}, nil
}
func (f *serviceStoreFake) StartPreview(context.Context, string, PreviewRequest, string) (*PreviewPlan, error) {
	return &PreviewPlan{}, nil
}
func (f *serviceStoreFake) CompletePreview(context.Context, PreviewResult) error { return nil }
func (f *serviceStoreFake) FailPreview(context.Context, string, string) error    { return nil }
func (f *serviceStoreFake) UpdateAuto(context.Context, string, AutoUpdate, string) (*Pool, error) {
	return &Pool{}, nil
}
func (f *serviceStoreFake) QueueSync(context.Context, string, SyncRequest, string) (*Run, error) {
	return &Run{}, nil
}
func (f *serviceStoreFake) Run(context.Context, string) (*Run, error) { return &Run{}, nil }

type previewStoreFake struct {
	serviceStoreFake
	plan      PreviewPlan
	completed *PreviewResult
	failed    bool
}

func TestQueueSyncRejectsWhenWriteRuntimeGateIsClosed(t *testing.T) {
	_, err := NewService(&serviceStoreFake{}).QueueSync(context.Background(), "pool-1", SyncRequest{
		ExpectedConfigVersion: 1, ConfirmAction: "SYNC_MARKETPLACE_STOCK_POOL",
	}, "user-1")
	if err != ErrWriteDisabled {
		t.Fatalf("QueueSync() error=%v, want %v", err, ErrWriteDisabled)
	}
}

func TestUpdateAutoRequiresDedicatedConfirmation(t *testing.T) {
	_, err := NewService(&serviceStoreFake{}).WithWriteEnabled(true).UpdateAuto(context.Background(), "pool-1", AutoUpdate{Enabled: true, ExpectedConfigVersion: 1}, "user-1")
	if err != ErrConfirmationRequired {
		t.Fatalf("UpdateAuto() error=%v, want %v", err, ErrConfirmationRequired)
	}
}

func (f *previewStoreFake) StartPreview(_ context.Context, _ string, _ PreviewRequest, _ string) (*PreviewPlan, error) {
	return &f.plan, nil
}
func (f *previewStoreFake) CompletePreview(_ context.Context, result PreviewResult) error {
	f.completed = &result
	return nil
}
func (f *previewStoreFake) FailPreview(context.Context, string, string) error {
	f.failed = true
	return nil
}

type balanceClientFake struct {
	response *sml.StockBalanceBatchResponse
	calls    int
}

func (f *balanceClientFake) BalancesBatch(_ context.Context, _ sml.StockBalanceBatchRequest) (*sml.StockBalanceBatchResponse, error) {
	f.calls++
	return f.response, nil
}

func TestCreatePoolRejectsSharedPoolWithoutAcknowledgementBeforeStore(t *testing.T) {
	store := &serviceStoreFake{}
	_, err := NewService(store).CreatePool(context.Background(), PoolInput{
		SMLItemCode: "AH-0001", SMLUnitCode: "ชิ้น", AllocationMode: AllocationModeShared,
		Members: []MemberInput{{Source: "tiktok", AccountKey: "shop", ExternalProductID: "product", ExternalSKUID: "sku", UnitFactor: 1, Enabled: true}},
	}, "user-1")
	if err != ErrSharedRiskNotAcknowledged || store.createCalls != 0 {
		t.Fatalf("CreatePool() err=%v calls=%d, want acknowledgement error without store call", err, store.createCalls)
	}
}

func TestUpdatePoolRequiresExplicitConfirmation(t *testing.T) {
	_, err := NewService(&serviceStoreFake{}).UpdatePool(context.Background(), "pool-1", PoolUpdate{
		PoolInput: PoolInput{SMLItemCode: "AH-0001", SMLUnitCode: "ชิ้น", AllocationMode: AllocationModeQuota,
			Members: []MemberInput{{Source: "shopee", AccountKey: "shop", ExternalProductID: "product", ExternalSKUID: "sku", UnitFactor: 1, AllocationPct: 100, Enabled: true}}},
		ExpectedConfigVersion: 1,
	}, "user-1")
	if err != ErrConfirmationRequired {
		t.Fatalf("UpdatePool() error = %v, want %v", err, ErrConfirmationRequired)
	}
}

func TestArchivePoolRequiresExplicitConfirmation(t *testing.T) {
	err := NewService(&serviceStoreFake{}).ArchivePool(context.Background(), "pool-1", PoolArchive{ExpectedConfigVersion: 1}, "user-1")
	if err != ErrConfirmationRequired {
		t.Fatalf("ArchivePool() error = %v, want %v", err, ErrConfirmationRequired)
	}
}

func TestPreviewPoolUsesNetSMLStockReservationAndBufferWithoutMarketplaceWrite(t *testing.T) {
	store := &previewStoreFake{plan: PreviewPlan{
		RunID: "run-1", Settings: Settings{WarehouseCode: "AB-1", LocationCode: "001", DefaultBufferPct: 10}, UnitBaseFactor: 2,
		PendingBaseQty: 4,
		Pool: Pool{ID: "pool-1", SMLItemCode: "AH-1", AllocationMode: AllocationModeQuota, ConfigVersion: 7,
			Members: []Member{{ID: "member-a", UnitFactor: 1, AllocationPct: 100, Enabled: true}}},
	}}
	balance := &balanceClientFake{response: &sml.StockBalanceBatchResponse{Scopes: []sml.StockBalanceScopeResult{{
		Items: []sml.StockBalanceItem{{ItemCode: "AH-1", AvailableBalanceQty: 10, AvailabilityStatus: "ready"}},
	}}}}
	result, err := NewService(store).WithSML(balance).PreviewPool(context.Background(), "pool-1", PreviewRequest{
		ExpectedConfigVersion: 7, ConfirmAction: "PREVIEW_MARKETPLACE_STOCK_POOL",
	}, "user-1")
	if err != nil {
		t.Fatalf("PreviewPool() error = %v", err)
	}
	if result.ReservationQty != 2 || result.UsableQty != 8 || result.BufferQty != 1 || result.DistributableQty != 7 {
		t.Fatalf("preview arithmetic = %#v, want reservation=2 usable=8 buffer=1 distributable=7", result)
	}
	if len(result.Lines) != 1 || result.Lines[0].TargetQty != 7 || result.Lines[0].Status != "planned" {
		t.Fatalf("preview lines = %#v", result.Lines)
	}
	if balance.calls != 1 || store.completed == nil || store.failed {
		t.Fatalf("balance calls=%d completed=%v failed=%v", balance.calls, store.completed != nil, store.failed)
	}
	if result.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("preview plan should expire in the future: %s", result.ExpiresAt)
	}
}
