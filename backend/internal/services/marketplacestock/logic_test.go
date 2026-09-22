package marketplacestock

import "testing"

func TestAllocateTargetsSubtractsBufferAndFloorsPerMember(t *testing.T) {
	result, err := AllocateTargets(AvailableStock{
		SMLUsableQty:  10,
		BufferPercent: 10,
		Mode:          AllocationModeQuota,
		Members: []PoolMember{
			{ID: "shopee", AllocationPercent: 60, UnitFactor: 1},
			{ID: "tiktok", AllocationPercent: 40, UnitFactor: 2},
		},
	})
	if err != nil {
		t.Fatalf("AllocateTargets() error = %v", err)
	}
	if result.BufferQty != 1 || result.DistributableQty != 9 {
		t.Fatalf("buffer/distributable = %v/%v, want 1/9", result.BufferQty, result.DistributableQty)
	}
	if got := result.Targets["shopee"]; got != 5 {
		t.Fatalf("Shopee target = %v, want 5", got)
	}
	if got := result.Targets["tiktok"]; got != 1 {
		t.Fatalf("TikTok target = %v, want 1", got)
	}
}

func TestAllocateTargetsRejectsQuotaAboveOneHundredPercent(t *testing.T) {
	_, err := AllocateTargets(AvailableStock{
		SMLUsableQty:  10,
		BufferPercent: 10,
		Mode:          AllocationModeQuota,
		Members: []PoolMember{
			{ID: "shopee", AllocationPercent: 60, UnitFactor: 1},
			{ID: "tiktok", AllocationPercent: 50, UnitFactor: 1},
		},
	})
	if err != ErrAllocationExceedsOneHundred {
		t.Fatalf("AllocateTargets() error = %v, want %v", err, ErrAllocationExceedsOneHundred)
	}
}

func TestAllocateTargetsRequiresSharedRiskAcknowledgement(t *testing.T) {
	_, err := AllocateTargets(AvailableStock{
		SMLUsableQty:  10,
		BufferPercent: 10,
		Mode:          AllocationModeShared,
		Members:       []PoolMember{{ID: "shopee", UnitFactor: 1}},
	})
	if err != ErrSharedRiskNotAcknowledged {
		t.Fatalf("AllocateTargets() error = %v, want %v", err, ErrSharedRiskNotAcknowledged)
	}
}

func TestAllocateTargetsSharedUsesSameDistributableQuantity(t *testing.T) {
	result, err := AllocateTargets(AvailableStock{
		SMLUsableQty:           10,
		BufferPercent:          10,
		Mode:                   AllocationModeShared,
		SharedRiskAcknowledged: true,
		Members:                []PoolMember{{ID: "shopee", UnitFactor: 1}, {ID: "tiktok", UnitFactor: 2}},
	})
	if err != nil {
		t.Fatalf("AllocateTargets() error = %v", err)
	}
	if result.Targets["shopee"] != 9 || result.Targets["tiktok"] != 4 {
		t.Fatalf("shared targets = %#v, want shopee=9 tiktok=4", result.Targets)
	}
}

func TestAllocateTargetsRejectsInvalidMemberConversion(t *testing.T) {
	_, err := AllocateTargets(AvailableStock{
		SMLUsableQty:  10,
		BufferPercent: 10,
		Mode:          AllocationModeQuota,
		Members:       []PoolMember{{ID: "tiktok", AllocationPercent: 100}},
	})
	if err != ErrInvalidUnitFactor {
		t.Fatalf("AllocateTargets() error = %v, want %v", err, ErrInvalidUnitFactor)
	}
}
