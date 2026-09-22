package marketplacestock

import (
	"context"
	"testing"
)

type serviceStoreFake struct{ createCalls int }

func (f *serviceStoreFake) Overview(context.Context) (*Overview, error) { return &Overview{}, nil }
func (f *serviceStoreFake) CreatePool(_ context.Context, input PoolInput, _ string) (*Pool, error) {
	f.createCalls++
	return &Pool{SMLItemCode: input.SMLItemCode}, nil
}
func (f *serviceStoreFake) UpdatePool(context.Context, string, PoolUpdate, string) (*Pool, error) {
	return &Pool{}, nil
}
func (f *serviceStoreFake) UpdateSettings(context.Context, SettingsUpdate, string) (*Settings, error) {
	return &Settings{}, nil
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
