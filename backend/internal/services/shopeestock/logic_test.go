package shopeestock

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/sml"
)

func TestSingleWarehouseFallbackOnlyAcceptsShopeeWhitelistCapability(t *testing.T) {
	if !isShopeeSingleWarehouseFallback(&shopeeapi.GatewayError{Code: "warehouse.error_not_in_whitelist"}) {
		t.Fatal("gateway whitelist error should use default seller stock")
	}
	if !isShopeeSingleWarehouseFallback(&shopeeapi.BusinessError{Code: "warehouse.error_not_in_whitelist"}) {
		t.Fatal("direct whitelist error should use default seller stock")
	}
	if isShopeeSingleWarehouseFallback(&shopeeapi.GatewayError{Code: "permission_denied"}) {
		t.Fatal("unrelated permission error must not be ignored")
	}
	if isShopeeSingleWarehouseFallback(errors.New("warehouse.error_not_in_whitelist")) {
		t.Fatal("untyped error must not be silently ignored")
	}
}

func TestTodayBangkokReturnsCalendarDate(t *testing.T) {
	if _, err := time.Parse("2006-01-02", TodayBangkok()); err != nil {
		t.Fatalf("TodayBangkok returned invalid date: %v", err)
	}
}

func TestCalculateTarget(t *testing.T) {
	if got := CalculateTarget(100, 80, 6); got != 13 {
		t.Fatalf("target = %d, want 13", got)
	}
	if got := CalculateTarget(-5, 80, 1); got != 0 {
		t.Fatalf("negative balance must clamp to zero, got %d", got)
	}
}

func TestPreviewExcludedLocationsPreservesSMLWarehouseDetails(t *testing.T) {
	got := previewExcludedLocations([]sml.StockBalanceLocation{{
		WarehouseCode: "W2", WarehouseName: "คลังสำรอง",
		LocationCode: "S2", LocationName: "ชั้นสอง", UnitCode: "ชิ้น", BalanceQty: -3,
	}})
	if len(got) != 1 || got[0].WarehouseCode != "W2" || got[0].WarehouseName != "คลังสำรอง" || got[0].LocationCode != "S2" || got[0].LocationName != "ชั้นสอง" || got[0].UnitCode != "ชิ้น" || got[0].BalanceQty != -3 {
		t.Fatalf("excluded locations = %#v", got)
	}
}

func TestNormalizeSelectedScopeRequiresWarehouseAndLocation(t *testing.T) {
	tests := []struct {
		name      string
		request   SettingsUpdate
		wantError bool
	}{
		{name: "unconfigured", request: SettingsUpdate{ScopeMode: "unconfigured"}, wantError: true},
		{name: "legacy all", request: SettingsUpdate{ScopeMode: "all", Locations: []LocationPair{{Warehouse: "W1", Location: "S1"}}}, wantError: true},
		{name: "selected without location", request: SettingsUpdate{ScopeMode: "selected"}, wantError: true},
		{name: "selected with blank location", request: SettingsUpdate{ScopeMode: "selected", Locations: []LocationPair{{Warehouse: "W1"}}}, wantError: true},
		{name: "selected with multiple locations", request: SettingsUpdate{ScopeMode: "selected", Locations: []LocationPair{{Warehouse: "W1", Location: "S1"}, {Warehouse: "W1", Location: "S2"}}}, wantError: true},
		{name: "selected", request: SettingsUpdate{ScopeMode: " SELECTED ", Locations: []LocationPair{{Warehouse: " W1 ", Location: " S1 "}, {Warehouse: "W1", Location: "S1"}}, AcknowledgeAllScopeWarnings: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeSelectedScope(test.request)
			if test.wantError {
				if !errors.Is(err, ErrScopeRequired) {
					t.Fatalf("error = %v, want ErrScopeRequired", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeSelectedScope: %v", err)
			}
			if got.ScopeMode != "selected" || len(got.Locations) != 1 {
				t.Fatalf("normalized scope = %+v", got)
			}
			if got.Locations[0].Warehouse != "W1" || got.Locations[0].Location != "S1" {
				t.Fatalf("normalized location = %+v", got.Locations[0])
			}
			if got.AcknowledgeAllScopeWarnings {
				t.Fatal("legacy all-scope acknowledgement must be ignored")
			}
		})
	}
}

func TestWriteErrorClassification(t *testing.T) {
	rateLimit := &shopeeapi.GatewayError{Code: "rate_limited", Retryable: true}
	if !isRetryableWrite(rateLimit) || isUnknownWrite(rateLimit) {
		t.Fatal("rate limit must retry after read-back but is not an unknown timeout")
	}
	timeout := &shopeeapi.GatewayError{Code: "shopee_timeout", Retryable: true}
	if !isRetryableWrite(timeout) || !isUnknownWrite(timeout) {
		t.Fatal("gateway timeout must be retryable and retain unknown-result semantics")
	}
	if !isUnknownWrite(context.DeadlineExceeded) {
		t.Fatal("transport deadline must retain unknown-result semantics")
	}
}

func TestCalculateTargetConvertsSmallestUnitToShopeeSellingUnit(t *testing.T) {
	if got := CalculateTarget(375, 80, 6); got != 50 {
		t.Fatalf("target = %d, want floor(375*0.8/6)=50", got)
	}
}

func TestCalculateSharedPoolTargetsSubtractsPendingOrdersBeforeAllocation(t *testing.T) {
	members := []SharedPoolAllocation{
		{ItemID: 10, ModelID: 101, UnitFactor: 1, AllocationPct: 80},
		{ItemID: 20, ModelID: 201, UnitFactor: 1, AllocationPct: 20},
	}

	targets, poolBaseTarget, warnings := CalculateSharedPoolTargets(63, 13, 80, members)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if poolBaseTarget != 40 {
		t.Fatalf("pool base target = %d, want floor((63-13)*0.8)=40", poolBaseTarget)
	}
	if targets[stockProductKey(10, 101)] != 32 || targets[stockProductKey(20, 201)] != 8 {
		t.Fatalf("targets = %v, want 32/8", targets)
	}
}

func TestCalculateSharedPoolTargetsNeverOvercommitsDifferentSellingUnits(t *testing.T) {
	members := []SharedPoolAllocation{
		{ItemID: 10, ModelID: 101, UnitFactor: 1, AllocationPct: 50},
		{ItemID: 20, ModelID: 201, UnitFactor: 6, AllocationPct: 50},
	}

	targets, poolBaseTarget, warnings := CalculateSharedPoolTargets(101, 0, 80, members)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	allocatedBase := targets[stockProductKey(10, 101)] + targets[stockProductKey(20, 201)]*6
	if allocatedBase > poolBaseTarget {
		t.Fatalf("allocated base %d exceeds pool target %d", allocatedBase, poolBaseTarget)
	}
}

func TestCalculateSharedPoolTargetsRejectsInvalidAllocation(t *testing.T) {
	tests := []struct {
		name    string
		members []SharedPoolAllocation
	}{
		{name: "one member", members: []SharedPoolAllocation{{ItemID: 1, UnitFactor: 1, AllocationPct: 100}}},
		{name: "sum below 100", members: []SharedPoolAllocation{{ItemID: 1, UnitFactor: 1, AllocationPct: 60}, {ItemID: 2, UnitFactor: 1, AllocationPct: 30}}},
		{name: "duplicate member", members: []SharedPoolAllocation{{ItemID: 1, ModelID: 2, UnitFactor: 1, AllocationPct: 50}, {ItemID: 1, ModelID: 2, UnitFactor: 1, AllocationPct: 50}}},
		{name: "invalid factor", members: []SharedPoolAllocation{{ItemID: 1, UnitFactor: 0, AllocationPct: 50}, {ItemID: 2, UnitFactor: 1, AllocationPct: 50}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, warnings := CalculateSharedPoolTargets(100, 0, 80, test.members); len(warnings) == 0 {
				t.Fatal("invalid pool must return a warning")
			}
		})
	}
}

func TestNormalizeSharedPoolUpdateRequiresCompleteOneHundredPercent(t *testing.T) {
	now := time.Now().UTC()
	request := SharedPoolUpdate{
		SMLItemCode: " AH-0001 ",
		Members: []SharedPoolMemberUpdate{
			{ItemID: 1, ModelID: 10, AllocationPct: 70, UpdatedAt: now},
			{ItemID: 2, ModelID: 20, AllocationPct: 30, UpdatedAt: now},
		},
	}
	got, err := normalizeSharedPoolUpdate(request)
	if err != nil {
		t.Fatalf("normalizeSharedPoolUpdate: %v", err)
	}
	if got.SMLItemCode != "AH-0001" {
		t.Fatalf("item code = %q", got.SMLItemCode)
	}

	request.Members[1].AllocationPct = 29
	if _, err := normalizeSharedPoolUpdate(request); err == nil {
		t.Fatal("allocation below 100 must fail")
	}
	request.Members[1].AllocationPct = 30
	request.Members[1].ItemID = 1
	request.Members[1].ModelID = 10
	if _, err := normalizeSharedPoolUpdate(request); err == nil {
		t.Fatal("duplicate member must fail")
	}
	request.Members[1].ItemID = 2
	request.Members[1].ModelID = 20
	request.Members[0].AllocationPct = 66.667
	request.Members[1].AllocationPct = 33.333
	if _, err := normalizeSharedPoolUpdate(request); err == nil {
		t.Fatal("allocation with more than two decimal places must fail")
	}
}

func TestValidSharedPoolProductsRequiresEveryActiveMember(t *testing.T) {
	products := []ProductRow{
		{ItemID: 1, ModelID: 10, SMLItemCode: "AH-0001", UnitFactor: 1, SharedPoolEnabled: true, PoolAllocationPct: 70},
		{ItemID: 2, ModelID: 20, SMLItemCode: "AH-0001", UnitFactor: 1, SharedPoolEnabled: true, PoolAllocationPct: 30},
		{ItemID: 3, ModelID: 30, SMLItemCode: "AH-0002", UnitFactor: 1, SharedPoolEnabled: true, PoolAllocationPct: 50},
		{ItemID: 4, ModelID: 40, SMLItemCode: "AH-0002", UnitFactor: 1, SharedPoolEnabled: false, PoolAllocationPct: 50},
	}
	pools := validSharedPoolProducts(products)
	if len(pools["AH-0001"]) != 2 {
		t.Fatalf("valid pool members = %+v", pools["AH-0001"])
	}
	if _, ok := pools["AH-0002"]; ok {
		t.Fatal("partially enabled pool must remain blocked")
	}
}

func TestCalculateSetTargetUsesBottleneckComponentAndParentUnit(t *testing.T) {
	definition := sml.StockSetDefinition{
		ItemCode: "AH-0058", StockValid: true,
		Components: []sml.StockSetComponent{
			{LineNumber: 1, ItemCode: "AH-0001", ItemName: "สีเพ้นท์", ItemType: 0, UnitCode: "กล่อง", Qty: 3, UnitFactor: 1, Active: true, UnitValid: true},
			{LineNumber: 2, ItemCode: "BOX", ItemName: "กล่องชุด", ItemType: 0, UnitCode: "ใบ", Qty: 1, UnitFactor: 1, Active: true, UnitValid: true},
		},
	}
	balances := map[string]sml.StockBalanceItem{
		"AH-0001": {ItemCode: "AH-0001", UnitCode: "แท่ง", BalanceQty: 31},
		"BOX":     {ItemCode: "BOX", UnitCode: "ใบ", BalanceQty: 8},
	}
	target, available, components, warnings := CalculateSetTarget(definition, balances, 80, 2)
	if available != 8 || target != 3 {
		t.Fatalf("available/target = %d/%d, want 8/3", available, target)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(components) != 2 || components[0].PossibleSets != 10 || components[1].PossibleSets != 8 || !components[1].Bottleneck {
		t.Fatalf("components = %+v", components)
	}
	if components[0].ComponentQty != 3 || components[0].UnitCode != "กล่อง" || components[0].BalanceUnitCode != "แท่ง" {
		t.Fatalf("component units = %+v", components[0])
	}
}

func TestCalculateSetTargetBlocksInvalidComponentUnit(t *testing.T) {
	definition := sml.StockSetDefinition{StockValid: true, Components: []sml.StockSetComponent{{
		ItemCode: "A", ItemType: 0, Qty: 2, UnitFactor: 0, Active: true, UnitValid: false,
	}}}
	_, _, _, warnings := CalculateSetTarget(definition, map[string]sml.StockBalanceItem{"A": {ItemCode: "A", BalanceQty: 10}}, 80, 1)
	if !reflect.DeepEqual(warnings, []string{"set_component_unit_invalid"}) {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestCalculateSetTargetConvertsComponentUnitToSmallestUnit(t *testing.T) {
	definition := sml.StockSetDefinition{StockValid: true, Components: []sml.StockSetComponent{{
		ItemCode: "A", ItemType: 0, UnitCode: "แพ็ค", Qty: 2, UnitFactor: 6, Active: true, UnitValid: true,
	}}}
	target, available, components, warnings := CalculateSetTarget(
		definition,
		map[string]sml.StockBalanceItem{"A": {ItemCode: "A", UnitCode: "ชิ้น", BalanceQty: 25}},
		100,
		1,
	)
	if len(warnings) != 0 || available != 2 || target != 2 {
		t.Fatalf("target/available/warnings = %d/%d/%v, want 2/2/[]", target, available, warnings)
	}
	if len(components) != 1 || components[0].RequiredBase != 12 || components[0].BalanceUnitCode != "ชิ้น" {
		t.Fatalf("components = %+v", components)
	}
}

func TestHasSharedComponentStockWithinShopAndAcrossShops(t *testing.T) {
	owners := map[string]map[string]struct{}{
		"A": {"item:1": {}, "item:2": {}},
		"B": {"item:1": {}},
	}
	if !hasSharedComponentStock([]string{"A"}, owners, nil) {
		t.Fatal("component used by two mappings in the same shop must be blocked")
	}
	if hasSharedComponentStock([]string{"B"}, owners, nil) {
		t.Fatal("component with one owner and no other-shop mapping should be allowed")
	}
	if !hasSharedComponentStock([]string{"B"}, owners, map[string]struct{}{"B": {}}) {
		t.Fatal("component used by another enabled shop must be blocked")
	}
}

func TestSetDefinitionChangeBlocksSyncUntilManualPreview(t *testing.T) {
	warnings := []string{"set_definition_changed"}
	if !hasBlockingStockWarnings(warnings, false) {
		t.Fatal("stock sync must block a stale set definition")
	}
	if hasBlockingStockWarnings(warnings, true) {
		t.Fatal("manual dry-run should be allowed to adopt the current definition hash")
	}
}

func TestUnitWarnings(t *testing.T) {
	valid := sml.StockCatalogUnit{StandValue: 6, DivideValue: 1, Ratio: 6}
	if got := UnitWarnings(valid); len(got) != 0 {
		t.Fatalf("valid unit warnings = %v", got)
	}
	broken := sml.StockCatalogUnit{StandValue: 1, DivideValue: 2, Ratio: 1}
	if got := UnitWarnings(broken); !reflect.DeepEqual(got, []string{"unit_factor_below_one", "unit_ratio_mismatch"}) {
		t.Fatalf("warnings = %v", got)
	}
}

func TestCatalogResolveUsesModelSKUExactBeforeBarcode(t *testing.T) {
	index := NewCatalogIndex([]sml.StockCatalogItem{
		{ItemCode: "SKU-1", StandardUnit: "PCS", Units: []sml.StockCatalogUnit{{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1}}},
		{ItemCode: "OTHER", StandardUnit: "BOX", Units: []sml.StockCatalogUnit{{Code: "BOX", StandValue: 6, DivideValue: 1, Ratio: 6}}, Barcodes: []sml.StockCatalogBarcode{{Barcode: "BAR-2", UnitCode: "BOX"}}},
	})
	match, ok := index.Resolve(PreferredSKU("SKU-1", "BAR-2"))
	if !ok || match.ItemCode != "SKU-1" || match.Source != "sku" {
		t.Fatalf("unexpected match: %#v, %v", match, ok)
	}
}

func TestCatalogResolveUsesSmallestUnitPriorityForItemCode(t *testing.T) {
	index := NewCatalogIndex([]sml.StockCatalogItem{{
		ItemCode: "SKU-1", StandardUnit: "BOX",
		Units: []sml.StockCatalogUnit{
			{Code: "BOX", StandValue: 6, DivideValue: 1, Ratio: 6, RowOrder: 2, LineNumber: 2},
			{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1, RowOrder: 1, LineNumber: 1},
		},
	}})
	match, ok := index.Resolve("SKU-1")
	if !ok || match.UnitCode != "PCS" || match.Factor != 1 {
		t.Fatalf("must use smallest unit by row_order: %#v", match)
	}
}

func TestPopulateProductUnitNamesUsesSMLPriorityAndSelectedUnit(t *testing.T) {
	product := ProductRow{SMLUnitCode: "BOX"}
	populateProductUnitNames(&product, []sml.StockCatalogUnit{
		{Code: "BOX", Name: "กล่อง", RowOrder: 2, LineNumber: 2},
		{Code: "PCS", Name: "ชิ้น", RowOrder: 1, LineNumber: 1},
	})
	if product.SMLBaseUnitCode != "PCS" || product.SMLBaseUnitName != "ชิ้น" {
		t.Fatalf("base unit = %s/%s, want PCS/ชิ้น", product.SMLBaseUnitCode, product.SMLBaseUnitName)
	}
	if product.SMLUnitName != "กล่อง" {
		t.Fatalf("selected unit name = %q, want กล่อง", product.SMLUnitName)
	}
}

func TestCatalogResolveDoesNotGuessAmbiguousBarcode(t *testing.T) {
	items := []sml.StockCatalogItem{
		{ItemCode: "A", Units: []sml.StockCatalogUnit{{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1}}, Barcodes: []sml.StockCatalogBarcode{{Barcode: "SAME", UnitCode: "PCS"}}},
		{ItemCode: "B", Units: []sml.StockCatalogUnit{{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1}}, Barcodes: []sml.StockCatalogBarcode{{Barcode: "SAME", UnitCode: "PCS"}}},
	}
	if _, ok := NewCatalogIndex(items).Resolve("SAME"); ok {
		t.Fatal("ambiguous barcode must not auto-map")
	}
}

func TestZeroDropCircuit(t *testing.T) {
	previous := []int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	next := []int64{0, 0, 0, 0, 0, 0, 1, 1, 1, 1}
	if got := ZeroDropCircuit(previous, next); got != "mass_zero_drop" {
		t.Fatalf("circuit = %q", got)
	}
}
