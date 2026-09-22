package marketplacestock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"nexflow/internal/services/sml"
)

var (
	ErrInvalidPoolInput      = errors.New("marketplace stock pool input is invalid")
	ErrConfirmationRequired  = errors.New("marketplace stock confirmation is required")
	ErrConfigVersionConflict = errors.New("marketplace stock configuration changed")
	ErrPoolPaused            = errors.New("marketplace stock pool is paused")
	ErrGlobalKillSwitch      = errors.New("marketplace stock kill switch is enabled")
	ErrPreviewAlreadyRunning = errors.New("marketplace stock preview is already running")
	ErrWriteDisabled         = errors.New("marketplace stock writes are disabled")
	ErrDryRunRequired        = errors.New("marketplace stock needs a fresh dry-run")
)

type Store interface {
	Overview(context.Context) (*Overview, error)
	Candidates(context.Context, string) ([]Candidate, error)
	CreatePool(context.Context, PoolInput, string) (*Pool, error)
	UpdatePool(context.Context, string, PoolUpdate, string) (*Pool, error)
	UpdateSettings(context.Context, SettingsUpdate, string) (*Settings, error)
	StartPreview(context.Context, string, PreviewRequest, string) (*PreviewPlan, error)
	CompletePreview(context.Context, PreviewResult) error
	FailPreview(context.Context, string, string) error
	UpdateAuto(context.Context, string, AutoUpdate, string) (*Pool, error)
	QueueSync(context.Context, string, SyncRequest, string) (*Run, error)
	Run(context.Context, string) (*Run, error)
}

type balanceClient interface {
	BalancesBatch(context.Context, sml.StockBalanceBatchRequest) (*sml.StockBalanceBatchResponse, error)
}

type Service struct {
	store        Store
	now          func() time.Time
	sml          balanceClient
	writeEnabled bool
}

// WithWriteEnabled is a runtime hard gate. It is deliberately independent
// from UI/role controls so a deploy cannot accidentally begin external writes.
func (s *Service) WithWriteEnabled(enabled bool) *Service {
	if s != nil {
		s.writeEnabled = enabled
	}
	return s
}

func (s *Service) WithSML(client balanceClient) *Service {
	if s != nil {
		s.sml = client
	}
	return s
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

func (s *Service) Candidates(ctx context.Context, source string) ([]Candidate, error) {
	if s == nil || s.store == nil || (source != "" && source != "shopee" && source != "tiktok") {
		return nil, ErrInvalidPoolInput
	}
	return s.store.Candidates(ctx, source)
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

func (s *Service) UpdateAuto(ctx context.Context, poolID string, input AutoUpdate, userID string) (*Pool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(poolID) == "" || strings.TrimSpace(userID) == "" || input.ExpectedConfigVersion < 1 {
		return nil, ErrInvalidPoolInput
	}
	want := "DISABLE_MARKETPLACE_STOCK_AUTO"
	if input.Enabled {
		want = "ENABLE_MARKETPLACE_STOCK_AUTO"
	}
	if strings.TrimSpace(input.ConfirmAction) != want {
		return nil, ErrConfirmationRequired
	}
	if input.Enabled && !s.writeEnabled {
		return nil, ErrWriteDisabled
	}
	return s.store.UpdateAuto(ctx, poolID, input, userID)
}

// QueueSync only creates a durable job. It never calls a Marketplace API in a
// request handler. The worker claims the job, re-reads SML and Marketplace,
// then records a read-back for every changed SKU.
func (s *Service) QueueSync(ctx context.Context, poolID string, input SyncRequest, userID string) (*Run, error) {
	if s == nil || s.store == nil || strings.TrimSpace(poolID) == "" || strings.TrimSpace(userID) == "" || input.ExpectedConfigVersion < 1 {
		return nil, ErrInvalidPoolInput
	}
	if strings.TrimSpace(input.ConfirmAction) != "SYNC_MARKETPLACE_STOCK_POOL" {
		return nil, ErrConfirmationRequired
	}
	if !s.writeEnabled {
		return nil, ErrWriteDisabled
	}
	return s.store.QueueSync(ctx, poolID, input, userID)
}

func (s *Service) Run(ctx context.Context, runID string) (*Run, error) {
	if s == nil || s.store == nil || strings.TrimSpace(runID) == "" {
		return nil, ErrInvalidPoolInput
	}
	return s.store.Run(ctx, runID)
}

// PreviewPool creates an immutable, read-only SML plan. It never calls a
// Marketplace API. A later worker must re-preflight and re-read both sides
// before a write is allowed.
func (s *Service) PreviewPool(ctx context.Context, poolID string, input PreviewRequest, userID string) (*PreviewResult, error) {
	if s == nil || s.store == nil || s.sml == nil || strings.TrimSpace(poolID) == "" || strings.TrimSpace(userID) == "" || input.ExpectedConfigVersion < 1 {
		return nil, ErrInvalidPoolInput
	}
	if strings.TrimSpace(input.ConfirmAction) != "PREVIEW_MARKETPLACE_STOCK_POOL" {
		return nil, ErrConfirmationRequired
	}
	plan, err := s.store.StartPreview(ctx, poolID, input, userID)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*PreviewResult, error) {
		_ = s.store.FailPreview(context.Background(), plan.RunID, userSafePreviewError(cause))
		return nil, cause
	}
	response, err := s.sml.BalancesBatch(ctx, sml.StockBalanceBatchRequest{
		AsOfDate:         s.now().In(time.FixedZone("Asia/Bangkok", 7*60*60)).Format("2006-01-02"),
		AvailabilityMode: "net_sale_order_v1",
		Scopes: []sml.StockBalanceScopeRequest{{
			ScopeID:   "marketplace-pool:" + plan.Pool.ID,
			ItemCodes: []string{plan.Pool.SMLItemCode}, ScopeMode: "selected",
			Locations: []sml.StockLocationPair{{Warehouse: plan.Settings.WarehouseCode, Location: plan.Settings.LocationCode}},
		}},
	})
	if err != nil || len(response.Scopes) != 1 || len(response.Scopes[0].Items) != 1 {
		if err == nil {
			err = errors.New("SML stock response ไม่ครบสำหรับสินค้าในกลุ่ม")
		}
		return fail(err)
	}
	item := response.Scopes[0].Items[0]
	if item.ItemCode != plan.Pool.SMLItemCode || item.AvailabilityStatus != "ready" || math.IsNaN(item.AvailableBalanceQty) || math.IsInf(item.AvailableBalanceQty, 0) {
		return fail(errors.New("SML ยังยืนยันยอดพร้อมใช้ของสินค้าในกลุ่มไม่ได้"))
	}
	reservationQty := plan.PendingBaseQty / plan.UnitBaseFactor
	usable := math.Max(0, item.AvailableBalanceQty-reservationQty)
	bufferPct := plan.Settings.DefaultBufferPct
	if plan.Pool.BufferPctOverride != nil {
		bufferPct = *plan.Pool.BufferPctOverride
	}
	allocation, err := AllocateTargets(AvailableStock{SMLUsableQty: usable, BufferPercent: bufferPct, Mode: plan.Pool.AllocationMode,
		SharedRiskAcknowledged: plan.Pool.SharedRiskAcknowledged, Members: toAllocationMembers(plan.Pool.Members)})
	if err != nil {
		return fail(err)
	}
	result := &PreviewResult{RunID: plan.RunID, Status: "success", SMLAvailableQty: item.AvailableBalanceQty,
		ReservationQty: reservationQty, UsableQty: usable, BufferQty: allocation.BufferQty, DistributableQty: allocation.DistributableQty,
		ExpiresAt: s.now().UTC().Add(60 * time.Second), Lines: make([]PreviewLine, 0, len(plan.Pool.Members))}
	for _, member := range plan.Pool.Members {
		if !member.Enabled {
			continue
		}
		result.Lines = append(result.Lines, PreviewLine{MemberID: member.ID, TargetQty: allocation.Targets[member.ID], Status: "planned",
			Message: "เป็นแผนจาก SML เท่านั้น ยังไม่อ่านหรือเขียนยอด Marketplace"})
	}
	if err := s.store.CompletePreview(ctx, *result); err != nil {
		return nil, err
	}
	return result, nil
}

func toAllocationMembers(members []Member) []PoolMember {
	result := make([]PoolMember, 0, len(members))
	for _, member := range members {
		if member.Enabled {
			result = append(result, PoolMember{ID: member.ID, AllocationPercent: member.AllocationPct, UnitFactor: member.UnitFactor})
		}
	}
	return result
}

func userSafePreviewError(err error) string {
	if err == nil {
		return ""
	}
	return "ยังสร้างแผนสต๊อกจาก SML ไม่สำเร็จ กรุณาตรวจการเชื่อมต่อและลองใหม่"
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
