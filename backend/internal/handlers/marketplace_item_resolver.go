package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/marketplace"
	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type marketplaceCatalogLookup func(code string) *models.CatalogItem

type marketplaceCatalogReadThrough interface {
	RefreshOneContext(ctx context.Context, itemCode string) (*models.CatalogItem, bool, error)
}

const marketplaceCatalogReadThroughLimit = 50

type matchResolution struct {
	alias   *models.MarketplaceItemAlias
	learned *models.Mapping
}

type marketplaceResolutionBatch struct {
	catalog     map[string]*models.CatalogItem
	resolutions map[string]matchResolution
}

func marketplaceResolutionKey(sourceSKU, rawName string) string {
	return normalizeMarketplaceSKU(sourceSKU) + "\x00" + rawName
}

func (b *marketplaceResolutionBatch) catalogLookup(code string) *models.CatalogItem {
	if b == nil {
		return nil
	}
	return b.catalog[normalizeMarketplaceSKU(code)]
}

func (b *marketplaceResolutionBatch) resolution(sourceSKU, rawName string) matchResolution {
	if b == nil {
		return matchResolution{}
	}
	return b.resolutions[marketplaceResolutionKey(sourceSKU, rawName)]
}

// prepareMarketplaceResolution preloads an import batch and performs bounded
// exact SML read-throughs for SKUs not yet present in the local catalog.
func prepareMarketplaceResolution(
	ctx context.Context,
	source string,
	orders []ShopeeOrder,
	selected map[string]bool,
	catalogRepo *repository.SMLCatalogRepo,
	catalogSvc marketplaceCatalogReadThrough,
	aliasRepo *repository.MarketplaceAliasRepo,
	mappingRepo *repository.MappingRepo,
	logger *zap.Logger,
) (*marketplaceResolutionBatch, error) {
	if catalogRepo == nil {
		return nil, fmt.Errorf("catalog repository is not configured")
	}

	type itemRef struct{ sku, rawName string }
	refs := make([]itemRef, 0)
	codesSet := map[string]struct{}{}
	codes := make([]string, 0)
	rawNamesSet := map[string]struct{}{}
	rawNames := make([]string, 0)
	normalizedKeysSet := map[string]struct{}{}
	normalizedKeys := make([]string, 0)
	for _, order := range orders {
		if selected != nil && !selected[order.OrderID] {
			continue
		}
		for _, item := range order.Items {
			rawName := shopeeItemRawName(item.ProductName, item.OptionName, item.RawName)
			sku := normalizeMarketplaceSKU(item.SKU)
			refs = append(refs, itemRef{sku: sku, rawName: rawName})
			if sku != "" {
				if _, exists := codesSet[sku]; !exists {
					codesSet[sku] = struct{}{}
					codes = append(codes, sku)
				}
			} else {
				if _, exists := rawNamesSet[rawName]; !exists {
					rawNamesSet[rawName] = struct{}{}
					rawNames = append(rawNames, rawName)
				}
				if key := marketplace.NormalizeKey(rawName, ""); key != "" {
					if _, exists := normalizedKeysSet[key]; !exists {
						normalizedKeysSet[key] = struct{}{}
						normalizedKeys = append(normalizedKeys, key)
					}
				}
			}
		}
	}

	catalogItems, err := catalogRepo.GetActiveMany(codes)
	if err != nil {
		return nil, fmt.Errorf("preload catalog: %w", err)
	}

	missing := make([]string, 0)
	for _, code := range codes {
		if catalogItems[code] == nil {
			missing = append(missing, code)
		}
	}
	if len(missing) > marketplaceCatalogReadThroughLimit {
		missing = missing[:marketplaceCatalogReadThroughLimit]
	}
	if catalogSvc != nil && len(missing) > 0 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		semaphore := make(chan struct{}, 3)
		failed := 0
		for _, code := range missing {
			code := code
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-semaphore }()
				lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				item, notFound, lookupErr := catalogSvc.RefreshOneContext(lookupCtx, code)
				mu.Lock()
				defer mu.Unlock()
				if lookupErr != nil {
					failed++
					return
				}
				if !notFound && item != nil && item.IsActive && item.ItemCode == code {
					catalogItems[code] = item
				}
			}()
		}
		wg.Wait()
		if failed > 0 && logger != nil {
			logger.Warn("marketplace catalog read-through incomplete",
				zap.String("source", source), zap.Int("failed", failed), zap.Int("attempted", len(missing)))
		}
	}

	aliasByKey := map[string]*models.MarketplaceItemAlias{}
	if aliasRepo != nil {
		aliasByKey, err = aliasRepo.FindMany(source, codes, normalizedKeys)
		if err != nil {
			return nil, fmt.Errorf("preload aliases: %w", err)
		}
	}
	mappingByName := map[string]*models.Mapping{}
	if mappingRepo != nil {
		mappingByName, err = mappingRepo.FindByRawNames(rawNames)
		if err != nil {
			return nil, fmt.Errorf("preload mappings: %w", err)
		}
	}

	resolutions := make(map[string]matchResolution, len(refs))
	for _, ref := range refs {
		var resolved matchResolution
		if ref.sku != "" {
			resolved.alias = aliasByKey["sku\x00"+ref.sku]
		} else {
			resolved.alias = aliasByKey["name\x00"+marketplace.NormalizeKey(ref.rawName, "")]
			resolved.learned = mappingByName[marketplace.NormalizeKey(ref.rawName, "")]
		}
		resolutions[marketplaceResolutionKey(ref.sku, ref.rawName)] = resolved
	}
	return &marketplaceResolutionBatch{catalog: catalogItems, resolutions: resolutions}, nil
}

func recordMarketplaceResolutionUsage(aliasRepo *repository.MarketplaceAliasRepo, mappingRepo *repository.MappingRepo, resolved matchResolution, exactSKU bool) {
	if exactSKU {
		return
	}
	if resolved.alias != nil && aliasRepo != nil {
		_ = aliasRepo.IncrementUsage(resolved.alias.ID)
		return
	}
	if resolved.learned != nil && mappingRepo != nil {
		_ = mappingRepo.IncrementUsage(resolved.learned.ID)
	}
}

func blockIfCatalogNotReady(c *gin.Context, repo *repository.SMLCatalogRepo) bool {
	if repo == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "catalog_not_ready", "message": "ยังไม่มีข้อมูลสินค้า SML กรุณารีเฟรชสินค้า SML ก่อนสร้างเอกสาร"})
		return true
	}
	count, err := repo.CountActive()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog_unavailable", "message": "ตรวจสอบสินค้า SML ไม่สำเร็จ กรุณาลองใหม่"})
		return true
	}
	if count == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "catalog_not_ready", "message": "ยังไม่มีข้อมูลสินค้า SML กรุณารีเฟรชสินค้า SML ก่อนสร้างเอกสาร"})
		return true
	}
	return false
}

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
	_ []models.CatalogMatch,
	lookup marketplaceCatalogLookup,
	_ float64,
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
	case alias != nil && alias.IsActive:
		bi.ItemCode = &alias.ItemCode
		bi.UnitCode = &alias.UnitCode
		bi.Mapped = true
		return bi, true
	case sourceSKU == "" && learned != nil:
		bi.ItemCode = &learned.ItemCode
		bi.UnitCode = &learned.UnitCode
		bi.MappingID = &learned.ID
		bi.Mapped = true
		return bi, true
	default:
		bi.Mapped = false
		return bi, false
	}
}
