// Package marketplacestock contains the platform-neutral stock-pool rules.
// It intentionally has no database or Marketplace API dependency so the
// most safety-critical arithmetic is deterministic and testable.
package marketplacestock

import (
	"errors"
	"math"
)

type AllocationMode string

const (
	AllocationModeQuota  AllocationMode = "quota"
	AllocationModeShared AllocationMode = "shared"
)

var (
	ErrInvalidAvailableStock       = errors.New("marketplace stock available quantity is invalid")
	ErrInvalidBufferPercent        = errors.New("marketplace stock buffer percent is invalid")
	ErrInvalidAllocationMode       = errors.New("marketplace stock allocation mode is invalid")
	ErrInvalidPoolMember           = errors.New("marketplace stock pool member is invalid")
	ErrInvalidUnitFactor           = errors.New("marketplace stock unit factor is invalid")
	ErrAllocationExceedsOneHundred = errors.New("marketplace stock allocation exceeds one hundred percent")
	ErrSharedRiskNotAcknowledged   = errors.New("marketplace stock shared risk is not acknowledged")
)

type PoolMember struct {
	ID                string
	AllocationPercent float64
	UnitFactor        float64
}

type AvailableStock struct {
	SMLUsableQty           float64
	BufferPercent          float64
	Mode                   AllocationMode
	SharedRiskAcknowledged bool
	Members                []PoolMember
}

type AllocationResult struct {
	BufferQty        float64
	DistributableQty float64
	Targets          map[string]int64
}

// AllocateTargets converts SML available quantity into absolute Marketplace
// targets. A target is always rounded down so Nexflow never offers inventory
// which SML does not have. The caller must supply SML usable quantity after
// physical stock, outstanding SML demand, and Nexflow reservation deductions.
func AllocateTargets(input AvailableStock) (AllocationResult, error) {
	if math.IsNaN(input.SMLUsableQty) || math.IsInf(input.SMLUsableQty, 0) || input.SMLUsableQty < 0 {
		return AllocationResult{}, ErrInvalidAvailableStock
	}
	if math.IsNaN(input.BufferPercent) || math.IsInf(input.BufferPercent, 0) || input.BufferPercent < 0 || input.BufferPercent > 100 {
		return AllocationResult{}, ErrInvalidBufferPercent
	}
	if input.Mode != AllocationModeQuota && input.Mode != AllocationModeShared {
		return AllocationResult{}, ErrInvalidAllocationMode
	}
	if len(input.Members) == 0 {
		return AllocationResult{}, ErrInvalidPoolMember
	}

	buffer := math.Ceil(input.SMLUsableQty * input.BufferPercent / 100)
	distributable := math.Max(0, input.SMLUsableQty-buffer)
	result := AllocationResult{
		BufferQty: buffer, DistributableQty: distributable, Targets: make(map[string]int64, len(input.Members)),
	}
	allocationTotal := 0.0
	seen := make(map[string]struct{}, len(input.Members))
	for _, member := range input.Members {
		if member.ID == "" {
			return AllocationResult{}, ErrInvalidPoolMember
		}
		if _, duplicate := seen[member.ID]; duplicate {
			return AllocationResult{}, ErrInvalidPoolMember
		}
		seen[member.ID] = struct{}{}
		if math.IsNaN(member.UnitFactor) || math.IsInf(member.UnitFactor, 0) || member.UnitFactor <= 0 {
			return AllocationResult{}, ErrInvalidUnitFactor
		}
		if math.IsNaN(member.AllocationPercent) || math.IsInf(member.AllocationPercent, 0) || member.AllocationPercent < 0 || member.AllocationPercent > 100 {
			return AllocationResult{}, ErrInvalidPoolMember
		}
		allocationTotal += member.AllocationPercent
	}
	if input.Mode == AllocationModeQuota && allocationTotal > 100+1e-9 {
		return AllocationResult{}, ErrAllocationExceedsOneHundred
	}
	if input.Mode == AllocationModeShared && !input.SharedRiskAcknowledged {
		return AllocationResult{}, ErrSharedRiskNotAcknowledged
	}

	for _, member := range input.Members {
		quantity := distributable
		if input.Mode == AllocationModeQuota {
			quantity = distributable * member.AllocationPercent / 100
		}
		result.Targets[member.ID] = int64(math.Floor(quantity / member.UnitFactor))
	}
	return result, nil
}
