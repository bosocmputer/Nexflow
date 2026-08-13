package shopeestock

import (
	"math"
	"sort"
	"strings"
	"time"

	"nexflow/internal/services/sml"
)

const unitRatioTolerance = 0.000001

var bangkokTimeZone = time.FixedZone("Asia/Bangkok", 7*60*60)

func TodayBangkok() string {
	return time.Now().In(bangkokTimeZone).Format("2006-01-02")
}

func CalculateTarget(balance, stockPct, unitFactor float64) int64 {
	if balance <= 0 || stockPct <= 0 || unitFactor <= 0 {
		return 0
	}
	return int64(math.Floor(balance * stockPct / 100 / unitFactor))
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
