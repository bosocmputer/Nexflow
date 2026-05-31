package handlers

import (
	"strings"

	"nexflow/internal/marketplace"
	"nexflow/internal/models"
)

type marketplaceCatalogLookup func(code string) *models.CatalogItem

func normalizeMarketplaceSKU(sku string) string {
	sku = strings.ReplaceAll(sku, "\ufeff", "")
	sku = strings.TrimSpace(sku)
	if strings.EqualFold(sku, "nan") || strings.EqualFold(sku, "null") || sku == "-" {
		return ""
	}
	return sku
}

func marketplaceBillItemFromMatch(
	rawName string,
	sourceSKU string,
	qty float64,
	price *float64,
	defaultUnit string,
	alias *models.MarketplaceItemAlias,
	learned *models.Mapping,
	matches []models.CatalogMatch,
	lookup marketplaceCatalogLookup,
	highConfThreshold float64,
) (models.BillItem, bool) {
	sourceSKU = normalizeMarketplaceSKU(sourceSKU)
	bi := models.BillItem{
		RawName:   rawName,
		SourceSKU: sourceSKU,
		Qty:       qty,
		Price:     price,
	}

	if sourceSKU != "" && lookup != nil {
		if cat := lookup(sourceSKU); cat != nil {
			code := cat.ItemCode
			unit := cat.UnitCode
			if unit == "" {
				unit = defaultUnit
			}
			bi.ItemCode = &code
			bi.UnitCode = &unit
			bi.Mapped = true
			return bi, true
		}
	}

	switch {
	case alias != nil:
		bi.ItemCode = &alias.ItemCode
		bi.UnitCode = &alias.UnitCode
		bi.Mapped = true
		return bi, true
	case learned != nil:
		bi.ItemCode = &learned.ItemCode
		bi.UnitCode = &learned.UnitCode
		bi.MappingID = &learned.ID
		bi.Mapped = true
		return bi, true
	case len(matches) > 0 && matches[0].Score >= highConfThreshold && safeMarketplaceCandidate(rawName, matches):
		bi.ItemCode = &matches[0].ItemCode
		unit := matches[0].UnitCode
		if unit == "" {
			unit = defaultUnit
		}
		bi.UnitCode = &unit
		bi.Mapped = true
		return bi, true
	default:
		if len(matches) > 0 {
			bi.ItemCode = &matches[0].ItemCode
			unit := matches[0].UnitCode
			if unit == "" {
				unit = defaultUnit
			}
			bi.UnitCode = &unit
		}
		bi.Mapped = false
		return bi, false
	}
}

func safeMarketplaceCandidate(rawName string, matches []models.CatalogMatch) bool {
	if len(matches) == 0 {
		return false
	}
	if marketplace.VariantConflict(rawName, matches[0].ItemName) {
		return false
	}
	if len(matches) > 1 && matches[0].Score-matches[1].Score < 0.05 {
		return false
	}
	return true
}
