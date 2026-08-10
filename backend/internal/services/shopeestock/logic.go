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
	ItemCode string
	UnitCode string
	Factor   float64
	Source   string
	Warnings []string
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
				ItemCode: item.ItemCode, UnitCode: barcode.UnitCode, Factor: UnitFactor(unit), Source: "barcode", Warnings: UnitWarnings(unit),
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
			return CatalogMatch{ItemCode: item.ItemCode, UnitCode: item.StandardUnit, Source: "sku", Warnings: []string{"unit_factor_missing"}}, true
		}
		return CatalogMatch{ItemCode: item.ItemCode, UnitCode: unit.Code, Factor: UnitFactor(unit), Source: "sku", Warnings: UnitWarnings(unit)}, true
	}
	if matches := i.byBarcode[key]; len(matches) == 1 {
		return matches[0], true
	}
	return CatalogMatch{}, false
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

func normalizeExactKey(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
}
