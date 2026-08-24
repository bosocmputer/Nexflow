package shopeestock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/sml"
)

var (
	ErrUnavailable         = errors.New("ระบบซิงก์สต๊อก Shopee ยังไม่พร้อมใช้งาน")
	ErrGatewayOnly         = errors.New("ซิงก์สต๊อก v1 รองรับเฉพาะ Central Shopee Gateway")
	ErrScopeRequired       = errors.New("กรุณาเลือก 1 คลังและ 1 พื้นที่เก็บ")
	ErrSelectedLocation    = errors.New("คลังหรือพื้นที่เก็บที่เลือกไม่มีอยู่ใน SML แล้ว")
	ErrSyncInProgress      = errors.New("ร้านนี้มีงานซิงก์สต๊อกกำลังทำงานอยู่")
	ErrBlockedReservations = errors.New("มีคำสั่งซื้อที่ยังพิสูจน์ mapping หรือ conversion ไม่ได้ ระบบหยุดส่ง stock เพื่อป้องกันขายเกิน")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{Message: message} }

type Config struct {
	Enabled                  bool
	GatewayMode              bool
	SetStockEnabled          bool
	ReservationLedgerEnabled bool
	GroupedUIEnabled         bool
	Environment              string
	InstanceID               string
}

func (s *Service) GroupedUIEnabled() bool { return s != nil && s.cfg.GroupedUIEnabled }

func (s *Service) ProductGroups(ctx context.Context, shopID int64, filter ProductGroupFilter) ([]ProductGroup, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, ErrUnavailable
	}
	return s.store.ListProductGroups(ctx, shopID, filter)
}

func (s *Service) ProductGroupVariants(ctx context.Context, shopID int64, filter ProductVariantFilter) ([]ProductRow, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, ErrUnavailable
	}
	return s.store.ListProductGroupVariants(ctx, shopID, filter)
}

type Service struct {
	store  *Store
	sml    *sml.StockSyncClient
	shopee shopeeapi.APIClient
	cfg    Config
	log    *zap.Logger

	locationMu       sync.Mutex
	locationCachedAt time.Time
	locations        []Location
	diagnostics      []LocationDiagnostic
}

func NewService(store *Store, smlClient *sml.StockSyncClient, shopeeClient shopeeapi.APIClient, cfg Config, log *zap.Logger) *Service {
	if strings.TrimSpace(cfg.Environment) == "" {
		cfg.Environment = "live"
	}
	if strings.TrimSpace(cfg.InstanceID) == "" {
		cfg.InstanceID = fmt.Sprintf("nexflow-%d", time.Now().UnixNano())
	}
	return &Service{store: store, sml: smlClient, shopee: shopeeClient, cfg: cfg, log: log}
}

func (s *Service) Available() bool {
	if s == nil || !s.cfg.Enabled || !s.cfg.GatewayMode || s.store == nil || s.sml == nil || !s.sml.IsConfigured() || s.shopee == nil {
		return false
	}
	if configured, ok := s.shopee.(interface{ Configured() bool }); ok {
		return configured.Configured()
	}
	return true
}

func (s *Service) Overview(ctx context.Context, shopID int64, filter ProductFilter) (*Overview, error) {
	overview := &Overview{
		Available: s.Available(), GatewayMode: s.cfg.GatewayMode, Settings: []Settings{}, Locations: []Location{},
		Diagnostics: []LocationDiagnostic{}, Products: []ProductRow{}, Runs: []Run{}, CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if !s.cfg.Enabled {
		overview.AvailabilityCode = "shopee_disabled"
		overview.AvailabilityText = "ฟีเจอร์ Shopee Open API ยังไม่เปิดสำหรับร้านนี้"
	} else if !s.cfg.GatewayMode {
		overview.AvailabilityCode = "gateway_required"
		overview.AvailabilityText = "ซิงก์สต๊อกรองรับเฉพาะ Central Shopee Gateway"
	} else if s.sml == nil || !s.sml.IsConfigured() {
		overview.AvailabilityCode = "sml_unavailable"
		overview.AvailabilityText = "ยังไม่ได้ตั้งค่าการเชื่อมต่อ SML"
	}
	settings, err := s.store.ListSettings(ctx, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	overview.Settings = settings
	if shopID == 0 && len(settings) > 0 {
		shopID = settings[0].ShopID
	}
	if shopID > 0 {
		overview.Products, overview.ProductsTotal, overview.ProductCounts, err = s.store.ListProductsPage(ctx, shopID, filter)
		if err != nil {
			return nil, err
		}
		overview.ProductsPage, overview.ProductsSize = filter.Page, filter.Size
		if overview.ProductsPage < 1 {
			overview.ProductsPage = 1
		}
		if overview.ProductsSize < 1 || overview.ProductsSize > 100 {
			overview.ProductsSize = 50
		}
		overview.Runs, err = s.store.ListRuns(ctx, shopID, 30)
		if err != nil {
			return nil, err
		}
	}
	if s.Available() {
		locations, diagnostics, locationErr := s.cachedLocations(ctx, false)
		if locationErr != nil {
			overview.AvailabilityText = "ตรวจคลัง SML ไม่สำเร็จ: " + userSafeError(locationErr)
		} else {
			overview.Locations, overview.Diagnostics = locations, diagnostics
		}
	}
	return overview, nil
}

func (s *Service) UpdateSettings(ctx context.Context, shopID int64, request SettingsUpdate, userID string) (*Settings, error) {
	if request.StockPct < 1 || request.StockPct > 100 {
		return nil, invalid("เปอร์เซ็นต์สต๊อกต้องอยู่ระหว่าง 1-100")
	}
	settings, err := s.store.GetSettings(ctx, shopID)
	if err != nil {
		return nil, err
	}
	request = preserveScheduleForLegacyRequest(request, settings)
	request, err = normalizeSchedule(request)
	if err != nil {
		return nil, err
	}
	request, err = normalizeSelectedScope(request)
	if err != nil {
		return nil, ErrScopeRequired
	}
	if request.Enabled && !s.Available() {
		return nil, ErrUnavailable
	}
	if request.Enabled && settings.CredentialMode != "gateway" {
		return nil, ErrGatewayOnly
	}
	if scheduleChanged(settings, request) || (request.Enabled && !settings.Enabled) || settings.NextRunAt == nil {
		next := nextScheduledRun(settingsFromUpdate(request), time.Now())
		request.NextRunAt = &next
	} else {
		request.NextRunAt = settings.NextRunAt
	}
	return s.store.UpdateSettings(ctx, shopID, request, userID)
}

func (s *Service) SyncCatalog(ctx context.Context, shopID int64) (*Run, error) {
	return s.syncCatalog(ctx, shopID, "manual", true)
}

func (s *Service) SyncCatalogScheduled(ctx context.Context, shopID int64, full bool) (*Run, error) {
	return s.syncCatalog(ctx, shopID, "scheduler", full)
}

func (s *Service) syncCatalog(ctx context.Context, shopID int64, trigger string, full bool) (*Run, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	settings, err := s.store.GetSettings(ctx, shopID)
	if err != nil {
		return nil, err
	}
	if settings.CredentialMode != "gateway" {
		return nil, ErrGatewayOnly
	}
	owner := s.leaseOwner("catalog", shopID)
	catalogLease, err := s.store.AcquireCatalogLease(ctx, owner, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	if !catalogLease {
		return nil, ErrSyncInProgress
	}
	defer func() { _ = s.store.ReleaseCatalogLease(context.Background(), owner) }()
	lease, err := s.store.AcquireLease(ctx, shopID, owner, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	if !lease {
		return nil, ErrSyncInProgress
	}
	defer func() { _ = s.store.ReleaseLease(context.Background(), shopID, owner) }()
	_ = s.store.MarkCatalogAttempted(ctx, shopID, "")
	runID, err := s.store.CreateRun(ctx, shopID, "catalog", trigger, "")
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*Run, error) {
		_ = s.store.FinishRun(context.Background(), runID, "failed", nil, 1, userSafeError(cause))
		_ = s.store.MarkCatalogAttempted(context.Background(), shopID, userSafeError(cause))
		return nil, cause
	}

	locationID, multipleWarehouses, err := s.sellerLocationID(ctx, shopID)
	if err != nil {
		return fail(err)
	}
	if multipleWarehouses {
		_ = s.store.SetPaused(context.Background(), shopID, "multiple_shopee_warehouses", "Shopee มีหลาย seller warehouse locations ซึ่ง v1 ยังไม่รองรับ")
		return fail(errors.New("Shopee มีหลาย seller warehouse locations กรุณาหยุดซิงก์และตรวจร้าน"))
	}

	var updatedFrom, updatedTo *time.Time
	if !full && settings.LastCatalogSyncAt != nil {
		from := settings.LastCatalogSyncAt.Add(-10 * time.Minute)
		to := time.Now().UTC()
		updatedFrom, updatedTo = &from, &to
	}
	smlCatalog, err := s.fetchSMLCatalog(ctx, updatedFrom, updatedTo)
	if err != nil {
		return fail(err)
	}
	if full && len(smlCatalog) == 0 {
		return fail(errors.New("SML ไม่คืนรายการสินค้าที่ทำสต๊อก"))
	}
	if full {
		if err := s.store.ReplaceSMLCatalog(ctx, smlCatalog); err != nil {
			return fail(err)
		}
	} else if len(smlCatalog) > 0 {
		if err := s.store.UpsertSMLCatalog(ctx, smlCatalog); err != nil {
			return fail(err)
		}
	}
	if err := s.store.RefreshManualMappings(ctx, smlCatalog, full); err != nil {
		return fail(err)
	}

	products, err := s.fetchShopeeProducts(ctx, shopID, locationID, updatedFrom, updatedTo)
	if err != nil {
		return fail(err)
	}
	localCatalog, err := s.store.ListSMLCatalog(ctx)
	if err != nil {
		return fail(err)
	}
	if full {
		if err := s.store.ReplaceShopeeProducts(ctx, shopID, products, NewCatalogIndex(localCatalog)); err != nil {
			return fail(err)
		}
	} else if len(products) > 0 {
		if err := s.store.UpsertShopeeProducts(ctx, shopID, products, NewCatalogIndex(localCatalog)); err != nil {
			return fail(err)
		}
	}
	if err := s.store.MarkCatalogSynced(ctx, shopID, full); err != nil {
		return fail(err)
	}
	result := &PreviewResult{TotalCount: len(products)}
	if err := s.store.FinishRun(ctx, runID, "success", result, 0, ""); err != nil {
		return nil, err
	}
	runs, _ := s.store.ListRuns(ctx, shopID, 1)
	if len(runs) == 0 {
		return nil, sql.ErrNoRows
	}
	return &runs[0], nil
}

func (s *Service) Preview(ctx context.Context, shopID int64, asOfDate string) (*PreviewResult, error) {
	return s.calculate(ctx, shopID, asOfDate, "preview", "manual", true)
}

func (s *Service) RunSync(ctx context.Context, shopID int64, trigger string) (*SyncResult, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	settings, err := s.store.GetSettings(ctx, shopID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled || settings.DryRunRequired || settings.PausedReason != "" {
		return nil, ErrDryRunRequired
	}
	owner := s.leaseOwner("sync", shopID)
	lease, err := s.store.AcquireLease(ctx, shopID, owner, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	if !lease {
		return nil, ErrSyncInProgress
	}
	defer func() { _ = s.store.ReleaseLease(context.Background(), shopID, owner) }()
	preview, err := s.calculate(ctx, shopID, TodayBangkok(), "sync", trigger, false)
	if err != nil {
		_ = s.store.RecordSyncChecked(context.Background(), shopID, userSafeError(err), nextScheduledRunAfter(*settings, time.Now()))
		return nil, err
	}
	result := &SyncResult{RunID: preview.RunID, ShopID: shopID, BlockedCount: preview.BlockedCount}
	if preview.CircuitBreaker != "" {
		reason := preview.CircuitBreaker
		_ = s.store.SetPaused(context.Background(), shopID, reason, "SML stock เปลี่ยนผิดปกติ จึงหยุดทั้งร้านเพื่อความปลอดภัย")
		_ = s.store.FinishRun(ctx, preview.RunID, "paused", preview, 0, reason)
		return result, fmt.Errorf("หยุดซิงก์เพื่อความปลอดภัย: %s", reason)
	}
	changed := make([]PreviewLine, 0)
	for _, line := range preview.Lines {
		if line.Changed && !line.Blocked {
			changed = append(changed, line)
		}
	}
	groups := groupLinesByItem(changed)
	locationID, multipleWarehouses, err := s.sellerLocationID(ctx, shopID)
	if err != nil {
		_ = s.store.RecordSyncChecked(context.Background(), shopID, userSafeError(err), nextScheduledRunAfter(*settings, time.Now()))
		_ = s.store.FinishRun(ctx, preview.RunID, "failed", preview, 1, userSafeError(err))
		return result, err
	}
	if multipleWarehouses {
		_ = s.store.SetPaused(context.Background(), shopID, "multiple_shopee_warehouses", "Shopee มีหลาย seller warehouse locations")
		_ = s.store.FinishRun(ctx, preview.RunID, "paused", preview, 0, "multiple_shopee_warehouses")
		return result, errors.New("Shopee มีหลาย seller warehouse locations ซึ่ง v1 ยังไม่รองรับ")
	}
	type groupResult struct {
		success         []PreviewLine
		errors, unknown int
	}
	sem := make(chan struct{}, 2)
	results := make(chan groupResult, len(groups))
	var wg sync.WaitGroup
	for itemID, lines := range groups {
		itemID, lines := itemID, lines
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- groupResult{errors: len(lines)}
				return
			}
			defer func() { <-sem }()
			results <- s.updateItemStock(ctx, settings, preview.RunID, itemID, locationID, lines)
		}()
	}
	wg.Wait()
	close(results)
	for row := range results {
		if len(row.success) > 0 {
			if err := s.store.MarkSyncSuccess(ctx, shopID, row.success); err != nil {
				row.errors += len(row.success)
			} else {
				result.ChangedCount += len(row.success)
			}
		}
		result.ErrorCount += row.errors
		result.UnknownCount += row.unknown
	}
	status := "success"
	if result.BlockedCount > 0 || result.ErrorCount > 0 || result.UnknownCount > 0 {
		status = "warning"
	}
	// Persist the number of confirmed Shopee updates, not the dry-run candidates.
	preview.ChangedCount = result.ChangedCount
	errorMessage := ""
	if result.ErrorCount > 0 || result.UnknownCount > 0 {
		errorMessage = "มีรายการอัปเดตไม่สำเร็จหรือยังไม่ทราบผล กรุณาตรวจประวัติ"
	}
	_ = s.store.RecordSyncChecked(context.Background(), shopID, errorMessage, nextScheduledRunAfter(*settings, time.Now()))
	if err := s.store.FinishRun(ctx, preview.RunID, status, preview, result.ErrorCount+result.UnknownCount, ""); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) sellerLocationID(ctx context.Context, shopID int64) (shopeeapi.StringID, bool, error) {
	warehouse, err := s.shopee.GetWarehouseDetail(ctx, "", shopID)
	if err != nil {
		if isShopeeSingleWarehouseFallback(err) {
			s.log.Info("shopee stock uses default seller stock",
				zap.Int64("shop_id", shopID),
				zap.String("reason", "multi_warehouse_not_enabled"),
			)
			return "", false, nil
		}
		return "", false, err
	}
	locations := warehouse.Locations()
	if len(locations) > 1 {
		return "", true, nil
	}
	if len(locations) == 1 {
		return locations[0].LocationID, false, nil
	}
	return "", false, nil
}

func isShopeeSingleWarehouseFallback(err error) bool {
	var gatewayErr *shopeeapi.GatewayError
	if errors.As(err, &gatewayErr) && strings.EqualFold(strings.TrimSpace(gatewayErr.Code), "warehouse.error_not_in_whitelist") {
		return true
	}
	var businessErr *shopeeapi.BusinessError
	return errors.As(err, &businessErr) && strings.EqualFold(strings.TrimSpace(businessErr.Code), "warehouse.error_not_in_whitelist")
}

func (s *Service) UpdateMapping(ctx context.Context, shopID, itemID, modelID int64, request MappingUpdate, userID string) (*ProductRow, error) {
	request.SMLItemCode = strings.TrimSpace(request.SMLItemCode)
	request.SMLUnitCode = strings.TrimSpace(request.SMLUnitCode)
	request.MarketplaceAliasID = strings.TrimSpace(request.MarketplaceAliasID)
	if request.UpdatedAt.IsZero() || (!request.Excluded && (request.SMLItemCode == "" || request.SMLUnitCode == "")) {
		return nil, invalid("กรุณาเลือกสินค้า/หน่วย SML และรีเฟรชรายการก่อนแก้ไข")
	}
	if !request.Excluded && request.MarketplaceAliasID != "" && request.MarketplaceAliasUpdatedAt == nil {
		return nil, invalid("ข้อมูล Product Master ไม่ครบ กรุณารีเฟรชรายการแล้วลองใหม่")
	}
	if request.ManualUnitFactor != nil && (math.IsNaN(*request.ManualUnitFactor) || math.IsInf(*request.ManualUnitFactor, 0) || *request.ManualUnitFactor < 1 || *request.ManualUnitFactor > 1_000_000_000) {
		return nil, ErrInvalidManualFactor
	}
	return s.store.UpdateMapping(ctx, shopID, itemID, modelID, request, userID)
}

func (s *Service) GetSharedPool(ctx context.Context, shopID int64, smlItemCode string) (*SharedPool, error) {
	if shopID <= 0 || strings.TrimSpace(smlItemCode) == "" {
		return nil, invalid("กรุณาระบุร้านและรหัสสินค้า SML")
	}
	return s.store.GetSharedPool(ctx, shopID, smlItemCode)
}

func (s *Service) UpdateSharedPool(ctx context.Context, shopID int64, request SharedPoolUpdate, userID string) (*SharedPool, error) {
	if shopID <= 0 {
		return nil, invalid("ร้าน Shopee ไม่ถูกต้อง")
	}
	normalized, err := normalizeSharedPoolUpdate(request)
	if err != nil {
		return nil, err
	}
	return s.store.UpdateSharedPool(ctx, shopID, normalized, userID)
}

func normalizeSharedPoolUpdate(request SharedPoolUpdate) (SharedPoolUpdate, error) {
	request.SMLItemCode = strings.TrimSpace(request.SMLItemCode)
	if request.SMLItemCode == "" {
		return request, invalid("กรุณาระบุรหัสสินค้า SML")
	}
	if len(request.Members) < 2 || len(request.Members) > 50 {
		return request, invalid("สต๊อกร่วมกันต้องมีรายการ Shopee ตั้งแต่ 2 ถึง 50 รายการ")
	}
	total := 0.0
	seen := make(map[string]struct{}, len(request.Members))
	for index := range request.Members {
		member := &request.Members[index]
		key := stockProductKey(member.ItemID, member.ModelID)
		if member.ItemID <= 0 || member.ModelID < 0 || member.UpdatedAt.IsZero() {
			return request, invalid("ข้อมูลรายการ Shopee ไม่ครบ กรุณารีเฟรชแล้วลองใหม่")
		}
		if _, exists := seen[key]; exists {
			return request, invalid("พบรายการ Shopee ซ้ำในกลุ่ม")
		}
		if member.AllocationPct <= 0 || member.AllocationPct > 100 || math.IsNaN(member.AllocationPct) || math.IsInf(member.AllocationPct, 0) {
			return request, invalid("สัดส่วนของแต่ละรายการต้องมากกว่า 0 และไม่เกิน 100%")
		}
		rounded := math.Round(member.AllocationPct*100) / 100
		if math.Abs(member.AllocationPct-rounded) > 0.000001 {
			return request, invalid("สัดส่วนกำหนดได้ไม่เกิน 2 ตำแหน่งทศนิยม")
		}
		member.AllocationPct = rounded
		seen[key] = struct{}{}
		total += member.AllocationPct
	}
	if math.Abs(total-100) > 0.001 {
		return request, invalid("สัดส่วนสต๊อกทุกรายการรวมกันต้องเท่ากับ 100%")
	}
	return request, nil
}

func (s *Service) SearchCatalog(ctx context.Context, query string) ([]CatalogOption, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len([]rune(query)) > 100 {
		return nil, invalid("กรอกคำค้น 2-100 ตัวอักษร")
	}
	return s.store.SearchSMLCatalog(ctx, query, 20)
}

func (s *Service) calculate(ctx context.Context, shopID int64, asOfDate, runType, trigger string, savePreview bool) (*PreviewResult, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	if _, err := time.Parse("2006-01-02", asOfDate); err != nil {
		return nil, invalid("วันที่ต้องเป็น YYYY-MM-DD")
	}
	settings, err := s.store.GetSettings(ctx, shopID)
	if err != nil {
		return nil, err
	}
	if settings.CredentialMode != "gateway" {
		return nil, ErrGatewayOnly
	}
	if settings.ScopeMode != "selected" || len(settings.Locations) != 1 {
		return nil, ErrScopeRequired
	}
	runID, err := s.store.CreateRun(ctx, shopID, runType, trigger, asOfDate)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*PreviewResult, error) {
		_ = s.store.FinishRun(context.Background(), runID, "failed", nil, 1, userSafeError(cause))
		return nil, cause
	}
	locations, _, err := s.cachedLocations(ctx, true)
	if err != nil {
		return fail(err)
	}
	if !selectedLocationsExist(settings.Locations, locations) {
		_ = s.store.SetPaused(context.Background(), shopID, "sml_location_missing", ErrSelectedLocation.Error())
		return fail(ErrSelectedLocation)
	}
	products, err := s.store.ListProducts(ctx, shopID)
	if err != nil {
		return fail(err)
	}
	if len(products) == 0 {
		return fail(errors.New("ยังไม่มีรายการสินค้า Shopee กรุณากดอัปเดตรายการสินค้าก่อน"))
	}
	var pendingReservations map[string]ReservationDemand
	if s.cfg.ReservationLedgerEnabled {
		pendingReservations, err = s.store.PendingShopeeReservationLedger(ctx, shopID)
	} else {
		pendingReservations, err = s.store.PendingShopeeReservationsLegacy(ctx, shopID)
		factorByKey := make(map[string]float64, len(products))
		for _, product := range products {
			factorByKey[stockProductKey(product.ItemID, product.ModelID)] = product.UnitFactor
		}
		for key, demand := range pendingReservations {
			demand.BaseQty = demand.SourceQty * factorByKey[key]
			pendingReservations[key] = demand
		}
	}
	if err != nil {
		return fail(fmt.Errorf("load pending Shopee stock reservations: %w", err))
	}
	sharedPools := validSharedPoolProducts(products)
	otherShopItems, err := s.store.EnabledSMLItemsOtherShops(ctx, shopID)
	if err != nil {
		return fail(err)
	}
	setCodes := make([]string, 0)
	setCodeSeen := map[string]struct{}{}
	for _, product := range products {
		if product.Excluded || product.SMLItemType != 3 || product.SMLItemCode == "" {
			continue
		}
		if _, ok := setCodeSeen[product.SMLItemCode]; !ok {
			setCodeSeen[product.SMLItemCode] = struct{}{}
			setCodes = append(setCodes, product.SMLItemCode)
		}
	}
	sort.Strings(setCodes)
	liveSetDefinitions := map[string]sml.StockSetDefinition{}
	if len(setCodes) > 0 && s.cfg.SetStockEnabled {
		definitions, loadErr := s.loadSetDefinitions(ctx, setCodes)
		if loadErr != nil {
			return fail(loadErr)
		}
		liveSetDefinitions = definitions
	}

	consumedByProduct := make(map[string][]string, len(products))
	componentOwners := map[string]map[string]struct{}{}
	for _, product := range products {
		if product.Excluded || product.SMLItemCode == "" {
			continue
		}
		key := stockProductKey(product.ItemID, product.ModelID)
		ownerKey := key
		if _, pooled := sharedPools[product.SMLItemCode]; pooled {
			ownerKey = "pool:" + product.SMLItemCode
		}
		consumed := []string{product.SMLItemCode}
		if product.SMLItemType == 3 {
			consumed = nil
			if definition, ok := liveSetDefinitions[product.SMLItemCode]; ok {
				for _, component := range definition.Components {
					if code := strings.TrimSpace(component.ItemCode); code != "" {
						consumed = append(consumed, code)
					}
				}
			}
		}
		consumedByProduct[key] = consumed
		for _, code := range consumed {
			if componentOwners[code] == nil {
				componentOwners[code] = map[string]struct{}{}
			}
			componentOwners[code][ownerKey] = struct{}{}
		}
	}
	itemSet := map[string]struct{}{}
	for _, consumed := range consumedByProduct {
		for _, code := range consumed {
			itemSet[code] = struct{}{}
		}
	}
	itemCodes := make([]string, 0, len(itemSet))
	for code := range itemSet {
		itemCodes = append(itemCodes, code)
	}
	sort.Strings(itemCodes)
	balanceMap := map[string]sml.StockBalanceItem{}
	excludedLocations := []ExcludedStockLocation{}
	if len(itemCodes) > 0 {
		request := sml.StockBalanceBatchRequest{AsOfDate: asOfDate, Scopes: []sml.StockBalanceScopeRequest{{
			ScopeID: "shop:" + strconv.FormatInt(shopID, 10), ItemCodes: itemCodes, ScopeMode: settings.ScopeMode,
			IncludeItemExcludedLocations: true,
		}}}
		for _, pair := range settings.Locations {
			request.Scopes[0].Locations = append(request.Scopes[0].Locations, sml.StockLocationPair{Warehouse: pair.Warehouse, Location: pair.Location})
		}
		balances, balanceErr := s.sml.BalancesBatch(ctx, request)
		if balanceErr != nil {
			return fail(balanceErr)
		}
		if len(balances.Scopes) != 1 {
			return fail(errors.New("SML stock response ไม่มี scope ที่ร้องขอ"))
		}
		for _, item := range balances.Scopes[0].Items {
			balanceMap[item.ItemCode] = item
		}
		excludedLocations = previewExcludedLocations("", balances.Scopes[0].ExcludedLocations)
	}
	result := &PreviewResult{
		RunID: runID, ShopID: shopID, AsOfDate: asOfDate,
		ExcludedLocations: excludedLocations, Lines: make([]PreviewLine, 0, len(products)),
	}
	for _, location := range excludedLocations {
		result.ExcludedBalance += location.BalanceQty
	}
	productByKey := make(map[string]ProductRow, len(products))
	lineIndex := make(map[string]int, len(products))
	for _, product := range products {
		if product.Excluded {
			result.SkippedCount++
			continue
		}
		result.TotalCount++
		key := stockProductKey(product.ItemID, product.ModelID)
		productByKey[key] = product
		pendingDemand := pendingReservations[key]
		line := PreviewLine{
			ItemID: product.ItemID, ModelID: product.ModelID, SMLItemCode: product.SMLItemCode,
			UnitFactor: product.UnitFactor, CurrentStock: product.ShopeeAvailable, ReservedStock: product.ShopeeReserved,
			PendingNexflowQty: pendingDemand.SourceQty, PendingBaseQty: pendingDemand.BaseQty, WarningCodes: append([]string(nil), product.WarningCodes...),
			ItemType: product.SMLItemType, SetDefinitionHash: product.SetDefinitionHash,
			SharedPoolEnabled: product.SharedPoolEnabled, PoolAllocationPct: product.PoolAllocationPct,
		}
		if _, pooled := sharedPools[product.SMLItemCode]; pooled {
			line.WarningCodes = removeWarning(line.WarningCodes, "duplicate_sml_item")
		}
		if hasSharedComponentStock(consumedByProduct[key], componentOwners, otherShopItems) {
			line.WarningCodes = appendUnique(line.WarningCodes, "shared_component_stock")
		}
		if product.SMLItemType == 3 {
			if !s.cfg.SetStockEnabled {
				line.WarningCodes = appendUnique(line.WarningCodes, "set_stock_feature_disabled")
			} else if definition, ok := liveSetDefinitions[product.SMLItemCode]; !ok {
				line.WarningCodes = appendUnique(line.WarningCodes, "set_definition_missing")
			} else {
				line.SetDefinitionHash = definition.Hash
				if product.MappingSetDefinitionHash != definition.Hash {
					line.WarningCodes = appendUnique(line.WarningCodes, "set_definition_changed")
					_ = s.store.SetDryRunRequired(context.Background(), shopID)
				}
				_, availableSets, components, setWarnings := CalculateSetTarget(definition, balanceMap, settings.StockPct, product.UnitFactor)
				line.ScopeBalance = float64(availableSets)
				pendingBase := line.PendingBaseQty
				if pendingBase > line.ScopeBalance {
					line.WarningCodes = appendUnique(line.WarningCodes, "pending_orders_exceed_sml_stock")
				}
				line.TargetStock = CalculateTarget(math.Max(line.ScopeBalance-pendingBase, 0), settings.StockPct, line.UnitFactor)
				line.SetComponents = components
				componentLocations := make([][]ExcludedStockLocation, 0, len(components))
				for _, component := range components {
					componentBalance, exists := balanceMap[component.ItemCode]
					if exists {
						componentLocations = append(componentLocations, previewExcludedLocations(component.ItemCode, componentBalance.ExcludedLocations))
					}
				}
				line.ExcludedLocations = mergePreviewExcludedLocations(componentLocations...)
				for _, component := range components {
					if component.Bottleneck && line.BottleneckItemCode == "" {
						line.BottleneckItemCode = component.ItemCode
					}
				}
				for _, warning := range setWarnings {
					line.WarningCodes = appendUnique(line.WarningCodes, warning)
				}
			}
		} else {
			balance, exists := balanceMap[product.SMLItemCode]
			if !exists || product.SMLItemCode == "" {
				line.WarningCodes = appendUnique(line.WarningCodes, "stock_balance_missing")
			}
			line.ScopeBalance = balance.BalanceQty
			line.ExcludedBalance = balance.ExcludedBalanceQty
			line.ExcludedLocations = previewExcludedLocations(product.SMLItemCode, balance.ExcludedLocations)
			line.MinQty = balance.MinQty
			line.MaxQty = balance.MaxQty
			pendingBase := line.PendingBaseQty
			if pendingBase > line.ScopeBalance {
				line.WarningCodes = appendUnique(line.WarningCodes, "pending_orders_exceed_sml_stock")
			}
			line.TargetStock = CalculateTarget(math.Max(line.ScopeBalance-pendingBase, 0), settings.StockPct, line.UnitFactor)
		}
		lineIndex[key] = len(result.Lines)
		result.Lines = append(result.Lines, line)
	}

	for _, members := range sharedPools {
		allocations := make([]SharedPoolAllocation, 0, len(members))
		pendingBase := 0.0
		poolBalance := 0.0
		for _, member := range members {
			key := stockProductKey(member.ItemID, member.ModelID)
			index, ok := lineIndex[key]
			if !ok {
				continue
			}
			line := result.Lines[index]
			poolBalance = line.ScopeBalance
			pendingBase += line.PendingBaseQty
			allocations = append(allocations, SharedPoolAllocation{
				ItemID: member.ItemID, ModelID: member.ModelID, UnitFactor: member.UnitFactor, AllocationPct: member.PoolAllocationPct,
			})
		}
		targets, poolBaseTarget, warnings := CalculateSharedPoolTargets(poolBalance, pendingBase, settings.StockPct, allocations)
		for _, member := range members {
			key := stockProductKey(member.ItemID, member.ModelID)
			index, ok := lineIndex[key]
			if !ok {
				continue
			}
			line := &result.Lines[index]
			line.PoolBaseTarget = poolBaseTarget
			if pendingBase > poolBalance {
				line.WarningCodes = appendUnique(line.WarningCodes, "pending_orders_exceed_sml_stock")
			}
			for _, warning := range warnings {
				line.WarningCodes = appendUnique(line.WarningCodes, warning)
			}
			line.TargetStock = targets[key]
		}
	}

	previousTargets := make([]int64, 0, len(result.Lines))
	nextTargets := make([]int64, 0, len(result.Lines))
	for index := range result.Lines {
		line := &result.Lines[index]
		if line.ReservedStock > int64(line.TargetStock) {
			line.WarningCodes = appendUnique(line.WarningCodes, "reserved_stock_exceeds_target")
		}
		line.Blocked = hasBlockingStockWarnings(line.WarningCodes, savePreview)
		line.Changed = line.TargetStock != line.CurrentStock
		if line.Blocked {
			result.BlockedCount++
		} else if line.Changed {
			result.ChangedCount++
		} else {
			result.SkippedCount++
		}
		previous := int64(0)
		product := productByKey[stockProductKey(line.ItemID, line.ModelID)]
		if product.LastSuccessTarget != nil {
			previous = *product.LastSuccessTarget
		}
		if !line.Blocked {
			previousTargets = append(previousTargets, previous)
			nextTargets = append(nextTargets, line.TargetStock)
		}
	}
	result.CircuitBreaker = ZeroDropCircuit(previousTargets, nextTargets)
	if result.CircuitBreaker != "" {
		_ = s.store.SetPaused(context.Background(), shopID, result.CircuitBreaker, "SML stock เปลี่ยนเป็นศูนย์จำนวนมากผิดปกติ")
	}
	if savePreview {
		if err := s.store.SavePreview(ctx, shopID, result); err != nil {
			return fail(err)
		}
		status := "success"
		if result.BlockedCount > 0 || result.CircuitBreaker != "" {
			status = "warning"
		}
		if err := s.store.FinishRun(ctx, runID, status, result, 0, result.CircuitBreaker); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) loadSetDefinitions(ctx context.Context, itemCodes []string) (map[string]sml.StockSetDefinition, error) {
	definitions := make(map[string]sml.StockSetDefinition, len(itemCodes))
	for start := 0; start < len(itemCodes); start += 500 {
		end := start + 500
		if end > len(itemCodes) {
			end = len(itemCodes)
		}
		result, err := s.sml.ProductSetsBatch(ctx, itemCodes[start:end])
		if err != nil {
			return nil, err
		}
		for _, definition := range result.Definitions {
			definitions[definition.ItemCode] = definition
		}
	}
	return definitions, nil
}

func hasBlockingStockWarnings(warnings []string, allowDefinitionRefresh bool) bool {
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		if allowDefinitionRefresh && warning == "set_definition_changed" {
			continue
		}
		if warning != "" {
			return true
		}
	}
	return false
}

func (s *Service) fetchSMLCatalog(ctx context.Context, updatedFrom, updatedTo *time.Time) ([]sml.StockCatalogItem, error) {
	items := []sml.StockCatalogItem{}
	for page := 1; ; page++ {
		result, err := s.sml.CatalogRange(ctx, page, 500, updatedFrom, updatedTo)
		if err != nil {
			return nil, err
		}
		items = append(items, result.Items...)
		if len(items) >= result.Total || len(result.Items) == 0 {
			return items, nil
		}
		if page > 100 {
			return nil, errors.New("SML stock catalog เกินขีดจำกัด 50,000 สินค้า")
		}
	}
}

func (s *Service) fetchShopeeProducts(ctx context.Context, shopID int64, locationID shopeeapi.StringID, updatedFrom, updatedTo *time.Time) ([]ShopeeProduct, error) {
	entries := []shopeeapi.ItemListEntry{}
	offset := 0
	for {
		request := shopeeapi.ItemListRequest{Offset: offset, PageSize: 100, ItemStatuses: []string{"NORMAL"}}
		if updatedFrom != nil {
			request.UpdateTimeFrom = updatedFrom.Unix()
		}
		if updatedTo != nil {
			request.UpdateTimeTo = updatedTo.Unix()
		}
		result, err := s.shopee.GetItemList(ctx, "", shopID, request)
		if err != nil {
			return nil, err
		}
		entries = append(entries, result.Response.Item...)
		if !result.Response.HasNextPage {
			break
		}
		if result.Response.NextOffset <= offset {
			return nil, errors.New("Shopee item pagination ไม่ขยับ")
		}
		offset = result.Response.NextOffset
	}
	products := []ShopeeProduct{}
	for start := 0; start < len(entries); start += 50 {
		end := start + 50
		if end > len(entries) {
			end = len(entries)
		}
		ids := make([]int64, 0, end-start)
		for _, entry := range entries[start:end] {
			ids = append(ids, entry.ItemID)
		}
		base, err := s.shopee.GetItemBaseInfo(ctx, "", shopID, ids)
		if err != nil {
			return nil, err
		}
		for _, item := range base.Response.ItemList {
			updated := time.Unix(item.UpdateTime, 0).UTC()
			if item.HasModel {
				models, err := s.shopee.GetModelList(ctx, "", shopID, item.ItemID)
				if err != nil {
					return nil, err
				}
				for _, model := range models.Response.Model {
					products = append(products, ShopeeProduct{ItemID: item.ItemID, ModelID: model.ModelID, ItemName: item.ItemName, ModelName: model.ModelName, ItemSKU: item.ItemSKU, ModelSKU: model.ModelSKU, ItemStatus: item.ItemStatus, ModelStatus: model.ModelStatus, Available: shopeeapi.CurrentSellerStock(model.StockInfoV2, locationID), Reserved: model.StockInfoV2.SummaryInfo.TotalReservedStock, SellerStock: model.StockInfoV2.SellerStock, ProductUpdated: &updated})
				}
			} else {
				products = append(products, ShopeeProduct{ItemID: item.ItemID, ModelID: 0, ItemName: item.ItemName, ItemSKU: item.ItemSKU, ItemStatus: item.ItemStatus, Available: shopeeapi.CurrentSellerStock(item.StockInfoV2, locationID), Reserved: item.StockInfoV2.SummaryInfo.TotalReservedStock, SellerStock: item.StockInfoV2.SellerStock, ProductUpdated: &updated})
			}
		}
	}
	return products, nil
}

func (s *Service) updateItemStock(ctx context.Context, settings *Settings, runID string, itemID int64, locationID shopeeapi.StringID, lines []PreviewLine) (out struct {
	success         []PreviewLine
	errors, unknown int
}) {
	if len(lines) > 50 {
		for _, line := range lines {
			_ = s.store.SaveSyncAttempt(context.Background(), runID, settings.ShopID, line, "blocked", "too_many_models", "Shopee item มีมากกว่า 50 models", "")
		}
		out.errors = len(lines)
		return
	}
	request := shopeeapi.UpdateStockRequest{ItemID: itemID, StockList: make([]shopeeapi.ModelStock, 0, len(lines))}
	for _, line := range lines {
		request.StockList = append(request.StockList, shopeeapi.ModelStock{ModelID: line.ModelID, SellerStock: []shopeeapi.SellerStock{{LocationID: locationID, Stock: line.TargetStock}}})
	}
	var response *shopeeapi.UpdateStockResponse
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		response, err = s.shopee.UpdateStock(ctx, "", settings.ShopID, request)
		if err == nil {
			break
		}
		if !isRetryableWrite(err) {
			break
		}
		// update_stock is idempotent for the same absolute target, but a timeout
		// can mean Shopee applied the write. Always read back before retrying.
		if s.readBackMatches(context.Background(), settings.ShopID, itemID, locationID, lines) {
			out.success = lines
			return
		}
		if attempt < 2 {
			delay := time.Duration(1<<attempt)*300*time.Millisecond + time.Duration(rand.IntN(200))*time.Millisecond
			select {
			case <-ctx.Done():
				err = ctx.Err()
				attempt = 2
			case <-time.After(delay):
			}
		}
	}
	if err != nil {
		resultKind, reason := "error", "shopee_update_failed"
		if isUnknownWrite(err) {
			resultKind, reason = "unknown_result", "write_timeout"
			out.unknown = len(lines)
		} else {
			out.errors = len(lines)
		}
		for _, line := range lines {
			_ = s.store.SaveSyncAttempt(context.Background(), runID, settings.ShopID, line, resultKind, reason, userSafeError(err), "")
		}
		return
	}
	failed := map[int64]string{}
	for _, failure := range response.Response.FailureList {
		failed[failure.ModelID] = failure.FailedReason
	}
	for _, line := range lines {
		if reason, ok := failed[line.ModelID]; ok {
			_ = s.store.SaveSyncAttempt(context.Background(), runID, settings.ShopID, line, "error", "shopee_validation", reason, response.RequestID)
			out.errors++
		} else {
			_ = s.store.SaveSyncAttempt(context.Background(), runID, settings.ShopID, line, "changed", "", "", response.RequestID)
			out.success = append(out.success, line)
		}
	}
	return
}

func (s *Service) readBackMatches(ctx context.Context, shopID, itemID int64, locationID shopeeapi.StringID, lines []PreviewLine) bool {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if len(lines) == 1 && lines[0].ModelID == 0 {
		base, err := s.shopee.GetItemBaseInfo(ctx, "", shopID, []int64{itemID})
		if err != nil || len(base.Response.ItemList) != 1 {
			return false
		}
		actual, ok := shopeeapi.SellerStockAtLocation(base.Response.ItemList[0].StockInfoV2, locationID)
		return ok && actual == lines[0].TargetStock
	}
	models, err := s.shopee.GetModelList(ctx, "", shopID, itemID)
	if err != nil {
		return false
	}
	actual := map[int64]int64{}
	for _, model := range models.Response.Model {
		stock, ok := shopeeapi.SellerStockAtLocation(model.StockInfoV2, locationID)
		if !ok {
			return false
		}
		actual[model.ModelID] = stock
	}
	for _, line := range lines {
		if actual[line.ModelID] != line.TargetStock {
			return false
		}
	}
	return true
}

func (s *Service) cachedLocations(ctx context.Context, force bool) ([]Location, []LocationDiagnostic, error) {
	s.locationMu.Lock()
	defer s.locationMu.Unlock()
	if !force && time.Since(s.locationCachedAt) < 5*time.Minute {
		return append([]Location(nil), s.locations...), append([]LocationDiagnostic(nil), s.diagnostics...), nil
	}
	response, err := s.sml.Locations(ctx, TodayBangkok())
	if err != nil {
		return nil, nil, err
	}
	locations := make([]Location, 0, len(response.Locations))
	for _, item := range response.Locations {
		locations = append(locations, Location{WarehouseCode: item.WarehouseCode, WarehouseName: item.WarehouseName, LocationCode: item.LocationCode, LocationName: item.LocationName})
	}
	diagnostics := make([]LocationDiagnostic, 0, len(response.Diagnostics))
	for _, item := range response.Diagnostics {
		diagnostics = append(diagnostics, LocationDiagnostic{Warehouse: item.Warehouse, Location: item.Location, Balance: item.Balance, Code: item.Code})
	}
	s.locations, s.diagnostics, s.locationCachedAt = locations, diagnostics, time.Now()
	return append([]Location(nil), locations...), append([]LocationDiagnostic(nil), diagnostics...), nil
}

func normalizeLocations(values []LocationPair) []LocationPair {
	seen := map[string]LocationPair{}
	for _, value := range values {
		value.Warehouse = strings.TrimSpace(value.Warehouse)
		value.Location = strings.TrimSpace(value.Location)
		if value.Warehouse == "" || value.Location == "" {
			continue
		}
		seen[value.Warehouse+"\x00"+value.Location] = value
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]LocationPair, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func normalizeSelectedScope(request SettingsUpdate) (SettingsUpdate, error) {
	request.ScopeMode = strings.ToLower(strings.TrimSpace(request.ScopeMode))
	request.Locations = normalizeLocations(request.Locations)
	request.AcknowledgeAllScopeWarnings = false
	if request.ScopeMode != "selected" || len(request.Locations) != 1 {
		return request, ErrScopeRequired
	}
	return request, nil
}

func selectedLocationsExist(selected []LocationPair, available []Location) bool {
	set := map[string]struct{}{}
	for _, item := range available {
		set[item.WarehouseCode+"\x00"+item.LocationCode] = struct{}{}
	}
	for _, item := range selected {
		if _, ok := set[item.Warehouse+"\x00"+item.Location]; !ok {
			return false
		}
	}
	return true
}
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
func groupLinesByItem(lines []PreviewLine) map[int64][]PreviewLine {
	out := map[int64][]PreviewLine{}
	for _, line := range lines {
		out[line.ItemID] = append(out[line.ItemID], line)
	}
	return out
}
func isUnknownWrite(err error) bool {
	var gatewayErr *shopeeapi.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr.Code == "shopee_timeout"
	}
	message := strings.ToLower(err.Error())
	return errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(message, "timeout") || strings.Contains(message, "deadline") ||
		strings.Contains(message, "connection reset") || strings.Contains(message, "unexpected eof") || message == "eof"
}

func isRetryableWrite(err error) bool {
	var gatewayErr *shopeeapi.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr.Retryable
	}
	return isUnknownWrite(err)
}
func userSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 300 {
		return message[:300]
	}
	return message
}

func (s *Service) leaseOwner(kind string, shopID int64) string {
	return fmt.Sprintf("%s:%s:%d:%d", s.cfg.InstanceID, kind, shopID, time.Now().UnixNano())
}
