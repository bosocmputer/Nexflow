package shopeestock

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"nexflow/internal/services/sml"
)

const unitRatioTolerance = 0.000001
const maxPreviewExcludedItems = 200

var bangkokTimeZone = time.FixedZone("Asia/Bangkok", 7*60*60)

func TodayBangkok() string {
	return time.Now().In(bangkokTimeZone).Format("2006-01-02")
}

func validateSyncReadiness(settings *Settings, trigger string) error {
	if settings == nil || settings.DryRunRequired || strings.TrimSpace(settings.PausedReason) != "" {
		return ErrDryRunRequired
	}
	if !settings.Enabled && !strings.EqualFold(strings.TrimSpace(trigger), "manual") {
		return ErrDryRunRequired
	}
	return nil
}

func validatePreviewScope(settings *Settings) error {
	if settings == nil || settings.CredentialMode != "gateway" {
		return ErrGatewayOnly
	}
	if settings.ScopeMode != "selected" || len(settings.Locations) != 1 {
		return ErrScopeRequired
	}
	return nil
}

func CalculateTarget(balance, stockPct, unitFactor float64) int64 {
	if balance <= 0 || stockPct <= 0 || unitFactor <= 0 {
		return 0
	}
	return int64(math.Floor(balance * stockPct / 100 / unitFactor))
}

func CalculateTargetExact(balance, pending, stockPct, unitFactor string) (int64, error) {
	values := make([]*big.Rat, 4)
	for index, raw := range []string{balance, pending, stockPct, unitFactor} {
		value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
		if !ok {
			return 0, fmt.Errorf("invalid exact stock quantity %q", raw)
		}
		values[index] = value
	}
	if values[0].Sign() < 0 || values[1].Sign() < 0 || values[2].Sign() <= 0 || values[2].Cmp(big.NewRat(100, 1)) > 0 || values[3].Sign() <= 0 {
		return 0, fmt.Errorf("exact stock quantities are outside safe bounds")
	}
	available := new(big.Rat).Sub(values[0], values[1])
	if available.Sign() <= 0 {
		return 0, nil
	}
	target := new(big.Rat).Mul(available, values[2])
	target.Quo(target, big.NewRat(100, 1))
	target.Quo(target, values[3])
	integer := new(big.Int).Quo(target.Num(), target.Denom())
	if !integer.IsInt64() {
		return 0, fmt.Errorf("exact stock target exceeds int64")
	}
	return integer.Int64(), nil
}

func decimalFromFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func exactSubtractClamp(balance, pending string) string {
	left, ok := new(big.Rat).SetString(strings.TrimSpace(balance))
	if !ok {
		return ""
	}
	right, ok := new(big.Rat).SetString(strings.TrimSpace(pending))
	if !ok {
		return ""
	}
	left.Sub(left, right)
	if left.Sign() <= 0 {
		return "0"
	}
	return left.FloatString(maxExactDecimalPlaces(balance, pending))
}

func maxExactDecimalPlaces(values ...string) int {
	places := 0
	for _, value := range values {
		value = strings.TrimSpace(value)
		if dot := strings.IndexByte(value, '.'); dot >= 0 {
			places = max(places, len(value)-dot-1)
		}
	}
	return places
}

func validateAvailabilityCapabilities(capability *sml.StockCapabilities, expectedFingerprint string) error {
	if capability == nil {
		return fmt.Errorf("SML stock capability is missing")
	}
	supported := false
	for _, mode := range capability.AvailabilityModes {
		if mode == "net_sale_order_v1" {
			supported = true
			break
		}
	}
	if !supported || capability.SchemaVersion != "stock-availability-v1" || capability.DecimalQuantityFormat != "string" || capability.MaxItemCodes < 1 {
		return fmt.Errorf("SML stock net capability is unsupported")
	}
	expectedFingerprint = strings.TrimSpace(expectedFingerprint)
	if expectedFingerprint == "" || capability.SourceSemanticsFingerprint != expectedFingerprint {
		return fmt.Errorf("SML stock source fingerprint is not approved")
	}
	return nil
}

func validateNetAvailabilityResponse(response *sml.StockBalanceBatchResponse, expectedFingerprint string) (*time.Time, error) {
	if response == nil || response.ModeApplied != "net_sale_order_v1" || response.SchemaVersion != "stock-availability-v1" {
		return nil, fmt.Errorf("SML stock response did not apply approved net availability semantics")
	}
	expectedFingerprint = strings.TrimSpace(expectedFingerprint)
	if expectedFingerprint == "" || response.SourceSemanticsFingerprint != expectedFingerprint {
		return nil, fmt.Errorf("SML stock response fingerprint changed")
	}
	snapshot, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(response.SourceSnapshotAt))
	if err != nil {
		return nil, fmt.Errorf("SML stock source snapshot is invalid: %w", err)
	}
	return &snapshot, nil
}

func validateNetAvailabilityItem(item sml.StockBalanceItem) error {
	if strings.TrimSpace(item.AvailabilityStatus) != "ready" {
		reason := strings.TrimSpace(item.AvailabilityReason)
		if reason == "" {
			reason = "availability_not_ready"
		}
		return fmt.Errorf("%s", reason)
	}
	physical, ok := new(big.Rat).SetString(strings.TrimSpace(item.PhysicalBalanceQtyExact))
	if !ok || physical.Sign() < 0 {
		return fmt.Errorf("invalid physical stock evidence")
	}
	outstanding, ok := new(big.Rat).SetString(strings.TrimSpace(item.OutstandingSalesOrderQtyExact))
	if !ok || outstanding.Sign() < 0 {
		return fmt.Errorf("invalid outstanding sales order evidence")
	}
	available, ok := new(big.Rat).SetString(strings.TrimSpace(item.AvailableBalanceQtyExact))
	if !ok || available.Sign() < 0 {
		return fmt.Errorf("invalid usable stock evidence")
	}
	balance, ok := new(big.Rat).SetString(strings.TrimSpace(item.BalanceQtyExact))
	if !ok || balance.Sign() < 0 {
		return fmt.Errorf("invalid balance stock evidence")
	}
	expected := new(big.Rat).Sub(physical, outstanding)
	if expected.Sign() < 0 {
		expected.SetInt64(0)
	}
	if available.Cmp(expected) != 0 || balance.Cmp(available) != 0 {
		return fmt.Errorf("SML stock exact availability is inconsistent")
	}
	return nil
}

func stockLinesSourceFresh(lines []PreviewLine, now time.Time, maxAge time.Duration) bool {
	if len(lines) == 0 || maxAge <= 0 {
		return false
	}
	for _, line := range lines {
		if line.SourceSnapshotAt == nil || line.SourceSnapshotAt.After(now.Add(time.Minute)) || now.Sub(*line.SourceSnapshotAt) > maxAge {
			return false
		}
	}
	return true
}

func previewExcludedLocations(itemCode string, items []sml.StockBalanceLocation) []ExcludedStockLocation {
	result := make([]ExcludedStockLocation, 0, len(items))
	for _, item := range items {
		result = append(result, ExcludedStockLocation{
			ItemCode:      itemCode,
			WarehouseCode: item.WarehouseCode,
			WarehouseName: item.WarehouseName,
			LocationCode:  item.LocationCode,
			LocationName:  item.LocationName,
			UnitCode:      item.UnitCode,
			BalanceQty:    item.BalanceQty,
		})
	}
	return result
}

func previewItemExcludedLocations(items []sml.StockBalanceItem) []ExcludedStockLocation {
	groups := make([][]ExcludedStockLocation, 0, len(items))
	for _, item := range items {
		locations := previewExcludedLocations(item.ItemCode, item.ExcludedLocations)
		for index := range locations {
			locations[index].ItemName = item.ItemName
			if strings.TrimSpace(locations[index].UnitCode) == "" {
				locations[index].UnitCode = item.UnitCode
			}
		}
		groups = append(groups, locations)
	}
	return mergePreviewExcludedLocations(groups...)
}

func appendPreviewExcludedItemSummary(current []ExcludedStockLocation, total int, next []ExcludedStockLocation) ([]ExcludedStockLocation, int) {
	total += len(next)
	remaining := maxPreviewExcludedItems - len(current)
	if remaining <= 0 {
		return current, total
	}
	if len(next) > remaining {
		next = next[:remaining]
	}
	return append(current, next...), total
}

func mergePreviewExcludedLocations(groups ...[]ExcludedStockLocation) []ExcludedStockLocation {
	values := map[string]*ExcludedStockLocation{}
	for _, group := range groups {
		for _, item := range group {
			key := strings.Join([]string{item.ItemCode, item.WarehouseCode, item.LocationCode, item.UnitCode}, "\x00")
			current := values[key]
			if current == nil {
				copy := item
				current = &copy
				values[key] = current
				continue
			}
			if strings.TrimSpace(current.ItemName) == "" {
				current.ItemName = item.ItemName
			}
			current.BalanceQty += item.BalanceQty
		}
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if value != nil && math.Abs(value.BalanceQty) > 1e-9 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make([]ExcludedStockLocation, 0, len(keys))
	for _, key := range keys {
		result = append(result, *values[key])
	}
	return result
}

type SharedPoolAllocation struct {
	ItemID          int64
	ModelID         int64
	UnitFactor      float64
	UnitFactorExact string
	AllocationPct   float64
}

func stockProductKey(itemID, modelID int64) string {
	return strconv.FormatInt(itemID, 10) + ":" + strconv.FormatInt(modelID, 10)
}

// CalculateSharedPoolTargets allocates one SML stock balance across multiple
// Shopee models. The calculation stays in the SML base unit until the final
// model conversion, so rounding can only hold stock back and never overcommit.
func CalculateSharedPoolTargets(
	balance, pendingBase, stockPct float64,
	members []SharedPoolAllocation,
) (map[string]int64, int64, []string) {
	targets := make(map[string]int64, len(members))
	if len(members) < 2 || stockPct <= 0 || stockPct > 100 || math.IsNaN(stockPct) || math.IsInf(stockPct, 0) {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	totalPct := 0.0
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		key := stockProductKey(member.ItemID, member.ModelID)
		if _, exists := seen[key]; exists || member.UnitFactor <= 0 || math.IsNaN(member.UnitFactor) || math.IsInf(member.UnitFactor, 0) ||
			member.AllocationPct <= 0 || member.AllocationPct > 100 || math.IsNaN(member.AllocationPct) || math.IsInf(member.AllocationPct, 0) {
			return targets, 0, []string{"shared_pool_invalid"}
		}
		seen[key] = struct{}{}
		totalPct += member.AllocationPct
	}
	if math.Abs(totalPct-100) > 0.001 {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	availableBase := math.Max(balance-math.Max(pendingBase, 0), 0)
	poolBaseTarget := int64(math.Floor(availableBase * stockPct / 100))
	for _, member := range members {
		allocatedBase := float64(poolBaseTarget) * member.AllocationPct / 100
		targets[stockProductKey(member.ItemID, member.ModelID)] = int64(math.Floor(allocatedBase / member.UnitFactor))
	}
	return targets, poolBaseTarget, nil
}

func CalculateSharedPoolTargetsExact(
	balance, pendingBase, stockPct string,
	members []SharedPoolAllocation,
) (map[string]int64, int64, []string) {
	targets := make(map[string]int64, len(members))
	if len(members) < 2 {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	available, ok := new(big.Rat).SetString(strings.TrimSpace(balance))
	if !ok || available.Sign() < 0 {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	pending, ok := new(big.Rat).SetString(strings.TrimSpace(pendingBase))
	if !ok || pending.Sign() < 0 {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	stockPercentage, ok := new(big.Rat).SetString(strings.TrimSpace(stockPct))
	if !ok || stockPercentage.Sign() <= 0 || stockPercentage.Cmp(big.NewRat(100, 1)) > 0 {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	seen := make(map[string]struct{}, len(members))
	allocations := make([]*big.Rat, len(members))
	factors := make([]*big.Rat, len(members))
	totalAllocation := new(big.Rat)
	for index, member := range members {
		key := stockProductKey(member.ItemID, member.ModelID)
		if _, exists := seen[key]; exists {
			return targets, 0, []string{"shared_pool_invalid"}
		}
		seen[key] = struct{}{}
		allocation, allocationOK := new(big.Rat).SetString(decimalFromFloat(member.AllocationPct))
		factorExact := strings.TrimSpace(member.UnitFactorExact)
		if factorExact == "" {
			factorExact = decimalFromFloat(member.UnitFactor)
		}
		factor, factorOK := new(big.Rat).SetString(factorExact)
		if !allocationOK || !factorOK || allocation.Sign() <= 0 || allocation.Cmp(big.NewRat(100, 1)) > 0 || factor.Sign() <= 0 {
			return targets, 0, []string{"shared_pool_invalid"}
		}
		allocations[index], factors[index] = allocation, factor
		totalAllocation.Add(totalAllocation, allocation)
	}
	if totalAllocation.Cmp(big.NewRat(100, 1)) != 0 {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	available.Sub(available, pending)
	if available.Sign() < 0 {
		available.SetInt64(0)
	}
	poolTarget := new(big.Rat).Mul(available, stockPercentage)
	poolTarget.Quo(poolTarget, big.NewRat(100, 1))
	poolInteger := new(big.Int).Quo(poolTarget.Num(), poolTarget.Denom())
	if !poolInteger.IsInt64() {
		return targets, 0, []string{"shared_pool_invalid"}
	}
	poolBaseTarget := poolInteger.Int64()
	for index, member := range members {
		allocated := new(big.Rat).Mul(new(big.Rat).SetInt64(poolBaseTarget), allocations[index])
		allocated.Quo(allocated, big.NewRat(100, 1))
		allocated.Quo(allocated, factors[index])
		quantity := new(big.Int).Quo(allocated.Num(), allocated.Denom())
		if !quantity.IsInt64() {
			return map[string]int64{}, 0, []string{"shared_pool_invalid"}
		}
		targets[stockProductKey(member.ItemID, member.ModelID)] = quantity.Int64()
	}
	return targets, poolBaseTarget, nil
}

func validSharedPoolProducts(products []ProductRow) map[string][]ProductRow {
	groups := map[string][]ProductRow{}
	for _, product := range products {
		if product.Excluded || strings.TrimSpace(product.SMLItemCode) == "" {
			continue
		}
		groups[product.SMLItemCode] = append(groups[product.SMLItemCode], product)
	}
	for itemCode, members := range groups {
		if len(members) < 2 {
			delete(groups, itemCode)
			continue
		}
		total := 0.0
		valid := true
		for _, member := range members {
			valid = valid && member.SharedPoolEnabled && member.PoolAllocationPct > 0 && member.UnitFactor > 0
			total += member.PoolAllocationPct
		}
		if !valid || math.Abs(total-100) > 0.001 {
			delete(groups, itemCode)
		}
	}
	return groups
}

func removeWarning(warnings []string, remove string) []string {
	out := warnings[:0]
	for _, warning := range warnings {
		if warning != remove {
			out = append(out, warning)
		}
	}
	return out
}

func CalculateSetTarget(
	definition sml.StockSetDefinition,
	balances map[string]sml.StockBalanceItem,
	stockPct, parentUnitFactor float64,
) (int64, int64, []SetStockComponentPreview, []string) {
	warnings := append([]string(nil), definition.WarningCodes...)
	if !definition.StockValid || len(definition.Components) == 0 {
		if len(warnings) == 0 {
			warnings = append(warnings, "set_stock_invalid")
		}
		return 0, 0, nil, warnings
	}
	if parentUnitFactor <= 0 {
		warnings = appendUnique(warnings, "unit_factor_missing")
		return 0, 0, nil, warnings
	}
	availableSets := int64(math.MaxInt64)
	components := make([]SetStockComponentPreview, 0, len(definition.Components))
	for _, component := range definition.Components {
		required := component.Qty * component.UnitFactor
		preview := SetStockComponentPreview{
			ItemCode: component.ItemCode, ItemName: component.ItemName,
			ComponentQty: component.Qty, UnitCode: component.UnitCode, RequiredBase: required,
		}
		if component.ItemType == 3 {
			warnings = appendUnique(warnings, "nested_set_not_supported")
		} else if component.ItemType != 0 {
			warnings = appendUnique(warnings, "set_component_not_stock_item")
		}
		if !component.Active {
			warnings = appendUnique(warnings, "set_component_inactive")
		}
		if !component.UnitValid || component.UnitFactor < 1 || required <= 0 || math.IsNaN(required) || math.IsInf(required, 0) {
			warnings = appendUnique(warnings, "set_component_unit_invalid")
		}
		balance, ok := balances[component.ItemCode]
		if !ok {
			warnings = appendUnique(warnings, "stock_balance_missing")
		} else {
			preview.BalanceQty = balance.BalanceQty
			preview.BalanceUnitCode = balance.UnitCode
			if required > 0 {
				preview.PossibleSets = int64(math.Floor(math.Max(balance.BalanceQty, 0) / required))
				if preview.PossibleSets < availableSets {
					availableSets = preview.PossibleSets
				}
			}
		}
		components = append(components, preview)
	}
	if availableSets == math.MaxInt64 {
		availableSets = 0
	}
	for i := range components {
		components[i].Bottleneck = components[i].PossibleSets == availableSets
	}
	target := int64(math.Floor(float64(availableSets) * stockPct / 100 / parentUnitFactor))
	if target < 0 {
		target = 0
	}
	return target, availableSets, components, warnings
}

func UnitWarnings(unit sml.StockCatalogUnit) []string {
	warnings := []string{}
	if unit.StandValue <= 0 || unit.DivideValue <= 0 {
		warnings = append(warnings, "unit_factor_missing")
		return warnings
	}
	factor := unit.StandValue / unit.DivideValue
	if factor < 1 {
		warnings = append(warnings, "unit_factor_below_one")
	}
	if unit.Ratio <= 0 || math.Abs(unit.Ratio-factor) > unitRatioTolerance*math.Max(1, math.Abs(factor)) {
		warnings = append(warnings, "unit_ratio_mismatch")
	}
	return warnings
}

func UnitFactor(unit sml.StockCatalogUnit) float64 {
	if unit.StandValue <= 0 || unit.DivideValue <= 0 {
		return 0
	}
	return unit.StandValue / unit.DivideValue
}

func PreferredSKU(modelSKU, itemSKU string) string {
	if value := normalizeExactKey(modelSKU); value != "" {
		return value
	}
	return normalizeExactKey(itemSKU)
}

type CatalogMatch struct {
	ItemCode          string
	UnitCode          string
	Factor            float64
	Source            string
	ItemType          int
	SetDefinitionHash string
	DocumentValid     bool
	Warnings          []string
}

type CatalogIndex struct {
	byItemCode map[string][]sml.StockCatalogItem
	byBarcode  map[string][]CatalogMatch
}

func NewCatalogIndex(items []sml.StockCatalogItem) *CatalogIndex {
	index := &CatalogIndex{byItemCode: map[string][]sml.StockCatalogItem{}, byBarcode: map[string][]CatalogMatch{}}
	for _, item := range items {
		code := normalizeExactKey(item.ItemCode)
		if code == "" {
			continue
		}
		index.byItemCode[code] = append(index.byItemCode[code], item)
		unitByCode := map[string]sml.StockCatalogUnit{}
		for _, unit := range item.Units {
			unitByCode[normalizeExactKey(unit.Code)] = unit
		}
		for _, barcode := range item.Barcodes {
			key := normalizeExactKey(barcode.Barcode)
			if key == "" {
				continue
			}
			unit := unitByCode[normalizeExactKey(barcode.UnitCode)]
			index.byBarcode[key] = append(index.byBarcode[key], CatalogMatch{
				ItemCode: item.ItemCode, UnitCode: barcode.UnitCode, Factor: UnitFactor(unit), Source: "barcode",
				ItemType: item.ItemType, SetDefinitionHash: stockSetHash(item), DocumentValid: stockDocumentValid(item), Warnings: stockCatalogWarnings(item, unit),
			})
		}
	}
	return index
}

func (i *CatalogIndex) Resolve(sku string) (CatalogMatch, bool) {
	key := normalizeExactKey(sku)
	if key == "" {
		return CatalogMatch{}, false
	}
	if items := i.byItemCode[key]; len(items) == 1 {
		item := items[0]
		unit, ok := chooseSmallestUnit(item)
		if !ok {
			return CatalogMatch{ItemCode: item.ItemCode, UnitCode: item.StandardUnit, Source: "sku", ItemType: item.ItemType, SetDefinitionHash: stockSetHash(item), DocumentValid: stockDocumentValid(item), Warnings: append(stockSetWarnings(item), "unit_factor_missing")}, true
		}
		return CatalogMatch{ItemCode: item.ItemCode, UnitCode: unit.Code, Factor: UnitFactor(unit), Source: "sku", ItemType: item.ItemType, SetDefinitionHash: stockSetHash(item), DocumentValid: stockDocumentValid(item), Warnings: stockCatalogWarnings(item, unit)}, true
	}
	if matches := i.byBarcode[key]; len(matches) == 1 {
		return matches[0], true
	}
	return CatalogMatch{}, false
}

func stockSetHash(item sml.StockCatalogItem) string {
	if item.SetDefinition == nil {
		return ""
	}
	return item.SetDefinition.Hash
}

func stockDocumentValid(item sml.StockCatalogItem) bool {
	return item.ItemType != 3 || (item.SetDefinition != nil && item.SetDefinition.DocumentValid)
}

func stockSetWarnings(item sml.StockCatalogItem) []string {
	if item.ItemType != 3 {
		return nil
	}
	if item.SetDefinition == nil {
		return []string{"set_definition_missing"}
	}
	warnings := append([]string(nil), item.SetDefinition.WarningCodes...)
	if !item.SetDefinition.StockValid && len(warnings) == 0 {
		warnings = append(warnings, "set_stock_invalid")
	}
	return warnings
}

func stockCatalogWarnings(item sml.StockCatalogItem, unit sml.StockCatalogUnit) []string {
	warnings := stockSetWarnings(item)
	for _, warning := range UnitWarnings(unit) {
		warnings = appendUnique(warnings, warning)
	}
	return warnings
}

func chooseSmallestUnit(item sml.StockCatalogItem) (sml.StockCatalogUnit, bool) {
	if len(item.Units) == 0 {
		return sml.StockCatalogUnit{}, false
	}
	units := append([]sml.StockCatalogUnit(nil), item.Units...)
	sort.SliceStable(units, func(a, b int) bool {
		if units[a].RowOrder != units[b].RowOrder {
			return units[a].RowOrder < units[b].RowOrder
		}
		if units[a].LineNumber != units[b].LineNumber {
			return units[a].LineNumber < units[b].LineNumber
		}
		return units[a].Code < units[b].Code
	})
	return units[0], true
}

func populateProductUnitNames(product *ProductRow, units []sml.StockCatalogUnit) {
	if product == nil || len(units) == 0 {
		return
	}
	if baseUnit, ok := chooseSmallestUnit(sml.StockCatalogItem{Units: units}); ok {
		product.SMLBaseUnitCode = baseUnit.Code
		product.SMLBaseUnitName = baseUnit.Name
	}
	for _, unit := range units {
		if unit.Code == product.SMLUnitCode {
			product.SMLUnitName = unit.Name
			return
		}
	}
}

func ZeroDropCircuit(previousTargets, nextTargets []int64) string {
	if len(previousTargets) != len(nextTargets) || len(previousTargets) == 0 {
		return ""
	}
	previousPositive := 0
	droppedToZero := 0
	previousTotal := int64(0)
	nextTotal := int64(0)
	for index := range previousTargets {
		previousTotal += previousTargets[index]
		nextTotal += nextTargets[index]
		if previousTargets[index] > 0 {
			previousPositive++
			if nextTargets[index] == 0 {
				droppedToZero++
			}
		}
	}
	if previousTotal > 0 && nextTotal == 0 {
		return "all_stock_became_zero"
	}
	if previousPositive >= 10 && float64(droppedToZero)/float64(previousPositive) > 0.5 {
		return "mass_zero_drop"
	}
	return ""
}

func hasSharedComponentStock(consumed []string, owners map[string]map[string]struct{}, otherShopItems map[string]struct{}) bool {
	for _, itemCode := range consumed {
		if len(owners[itemCode]) > 1 {
			return true
		}
		if _, duplicated := otherShopItems[itemCode]; duplicated {
			return true
		}
	}
	return false
}

func normalizeExactKey(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
}
