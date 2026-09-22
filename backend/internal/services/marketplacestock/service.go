package marketplacestock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrInvalidPoolInput      = errors.New("marketplace stock pool input is invalid")
	ErrConfirmationRequired  = errors.New("marketplace stock confirmation is required")
	ErrConfigVersionConflict = errors.New("marketplace stock configuration changed")
)

type Store interface {
	Overview(context.Context) (*Overview, error)
	CreatePool(context.Context, PoolInput, string) (*Pool, error)
	UpdatePool(context.Context, string, PoolUpdate, string) (*Pool, error)
	UpdateSettings(context.Context, SettingsUpdate, string) (*Settings, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("marketplace stock is not configured")
	}
	return s.store.Overview(ctx)
}

func (s *Service) CreatePool(ctx context.Context, input PoolInput, userID string) (*Pool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(userID) == "" {
		return nil, ErrInvalidPoolInput
	}
	if err := validatePoolInput(input); err != nil {
		return nil, err
	}
	return s.store.CreatePool(ctx, input, userID)
}

func (s *Service) UpdatePool(ctx context.Context, poolID string, input PoolUpdate, userID string) (*Pool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(poolID) == "" || strings.TrimSpace(userID) == "" || input.ExpectedConfigVersion < 1 {
		return nil, ErrInvalidPoolInput
	}
	if strings.TrimSpace(input.ConfirmAction) != "UPDATE_MARKETPLACE_STOCK_POOL" {
		return nil, ErrConfirmationRequired
	}
	if err := validatePoolInput(input.PoolInput); err != nil {
		return nil, err
	}
	return s.store.UpdatePool(ctx, poolID, input, userID)
}

func (s *Service) UpdateSettings(ctx context.Context, input SettingsUpdate, userID string) (*Settings, error) {
	if s == nil || s.store == nil || strings.TrimSpace(userID) == "" || input.ExpectedConfigVersion < 1 ||
		strings.TrimSpace(input.WarehouseCode) == "" || strings.TrimSpace(input.LocationCode) == "" ||
		math.IsNaN(input.DefaultBufferPct) || math.IsInf(input.DefaultBufferPct, 0) || input.DefaultBufferPct < 0 || input.DefaultBufferPct > 100 {
		return nil, ErrInvalidPoolInput
	}
	if strings.TrimSpace(input.ConfirmAction) != "UPDATE_MARKETPLACE_STOCK_SETTINGS" {
		return nil, ErrConfirmationRequired
	}
	return s.store.UpdateSettings(ctx, input, userID)
}

func validatePoolInput(input PoolInput) error {
	if strings.TrimSpace(input.SMLItemCode) == "" || strings.TrimSpace(input.SMLUnitCode) == "" || len(input.Members) == 0 ||
		(input.AllocationMode != AllocationModeQuota && input.AllocationMode != AllocationModeShared) {
		return ErrInvalidPoolInput
	}
	if input.BufferPctOverride != nil && (math.IsNaN(*input.BufferPctOverride) || math.IsInf(*input.BufferPctOverride, 0) || *input.BufferPctOverride < 0 || *input.BufferPctOverride > 100) {
		return ErrInvalidPoolInput
	}
	if input.AllocationMode == AllocationModeShared && !input.SharedRiskAcknowledged {
		return ErrSharedRiskNotAcknowledged
	}
	seen := make(map[string]struct{}, len(input.Members))
	total := 0.0
	for _, member := range input.Members {
		if (member.Source != "shopee" && member.Source != "tiktok") || strings.TrimSpace(member.AccountKey) == "" ||
			strings.TrimSpace(member.ExternalProductID) == "" || strings.TrimSpace(member.ExternalSKUID) == "" ||
			math.IsNaN(member.UnitFactor) || math.IsInf(member.UnitFactor, 0) || member.UnitFactor <= 0 ||
			math.IsNaN(member.AllocationPct) || math.IsInf(member.AllocationPct, 0) || member.AllocationPct < 0 || member.AllocationPct > 100 {
			return ErrInvalidPoolInput
		}
		key := fmt.Sprintf("%s|%s|%s|%s", member.Source, member.AccountKey, member.ExternalProductID, member.ExternalSKUID)
		if _, exists := seen[key]; exists {
			return ErrInvalidPoolInput
		}
		seen[key] = struct{}{}
		if member.Enabled {
			total += member.AllocationPct
		}
	}
	if input.AllocationMode == AllocationModeQuota && total > 100+1e-9 {
		return ErrAllocationExceedsOneHundred
	}
	return nil
}
