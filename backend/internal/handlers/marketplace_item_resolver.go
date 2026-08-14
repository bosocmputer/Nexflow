package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	learned *models.Mapping // legacy review hint only; never auto-applied
}

type marketplaceResolutionBatch struct {
	catalog      map[string]*models.CatalogItem
	resolutions  map[string]matchResolution
	exactMasters map[string]*models.MarketplaceItemAlias
	aliasUsage   map[string]int
	mode         string
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

func catalogItemDocumentReady(item *models.CatalogItem) bool {
	// Callers preload only active rows through GetActive/GetActiveMany. Keeping
	// this helper focused on set validity also makes pure resolver tests explicit.
	return item != nil && (item.ItemType != 3 || item.SetDocumentValid)
}

func (b *marketplaceResolutionBatch) resolution(sourceSKU, rawName string) matchResolution {
	return b.resolutionScoped("default", "", "", sourceSKU, rawName)
}

func (b *marketplaceResolutionBatch) resolutionScoped(accountKey, externalItemID, externalVariantID, sourceSKU, rawName string) matchResolution {
	if b == nil {
		return matchResolution{}
	}
	if accountKey == "" {
		accountKey = "default"
	}
	if externalItemID != "" {
		if resolved, ok := b.resolutions["identity\x00"+accountKey+"\x00"+externalItemID+"\x00"+externalVariantID]; ok && resolved.alias != nil {
			return resolved
		}
	}
	if externalItemID == "" && externalVariantID != "" {
		if resolved, ok := b.resolutions["variant\x00"+accountKey+"\x00"+externalVariantID]; ok && resolved.alias != nil {
			return resolved
		}
	}
	if sku := normalizeMarketplaceSKU(sourceSKU); sku != "" {
		if resolved, ok := b.resolutions["sku\x00"+accountKey+"\x00"+sku]; ok && resolved.alias != nil {
			return resolved
		}
	}
	if resolved, ok := b.resolutions["name\x00"+accountKey+"\x00"+marketplace.NormalizeKey(rawName, "")]; ok {
		return resolved
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

	type itemRef struct{ accountKey, itemID, variantID, sku, rawName string }
	refs := make([]itemRef, 0)
	accountKeysSet := map[string]struct{}{}
	accountKeys := make([]string, 0)
	externalItemIDsSet := map[string]struct{}{}
	externalItemIDs := make([]string, 0)
	externalVariantIDsSet := map[string]struct{}{}
	externalVariantIDs := make([]string, 0)
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
		accountKey := marketplaceSourceAccountKey(source, order.ShopeeShopID)
		if _, exists := accountKeysSet[accountKey]; !exists {
			accountKeysSet[accountKey] = struct{}{}
			accountKeys = append(accountKeys, accountKey)
		}
		for _, item := range order.Items {
			rawName := shopeeItemRawName(item.ProductName, item.OptionName, item.RawName)
			sku := normalizeMarketplaceSKU(item.SKU)
			itemID := strings.TrimSpace(item.SourceItemID)
			variantID := strings.TrimSpace(item.SourceVariantID)
			refs = append(refs, itemRef{accountKey: accountKey, itemID: itemID, variantID: variantID, sku: sku, rawName: rawName})
			if itemID != "" {
				if _, exists := externalItemIDsSet[itemID]; !exists {
					externalItemIDsSet[itemID] = struct{}{}
					externalItemIDs = append(externalItemIDs, itemID)
				}
				if _, exists := externalVariantIDsSet[variantID]; !exists {
					externalVariantIDsSet[variantID] = struct{}{}
					externalVariantIDs = append(externalVariantIDs, variantID)
				}
			}
			if variantID != "" {
				if _, exists := externalVariantIDsSet[variantID]; !exists {
					externalVariantIDsSet[variantID] = struct{}{}
					externalVariantIDs = append(externalVariantIDs, variantID)
				}
			}
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
		aliasSKUs := append([]string(nil), codes...)
		// Compatibility for AOY's eight historical TikTok aliases: older imports
		// stored TikTok SKU ID in source_sku. New imports keep it as variant ID,
		// but may still resolve those verified rows until an admin resaves them.
		if source == "tiktok" {
			aliasSKUs = append(aliasSKUs, externalVariantIDs...)
		}
		aliasByKey, err = aliasRepo.FindManyScoped(source, accountKeys, externalItemIDs, externalVariantIDs, aliasSKUs, normalizedKeys)
		if err != nil {
			return nil, fmt.Errorf("preload aliases: %w", err)
		}
		aliasTargetSet := map[string]struct{}{}
		aliasTargets := make([]string, 0)
		for _, alias := range aliasByKey {
			code := normalizeMarketplaceSKU(alias.ItemCode)
			if code == "" {
				continue
			}
			if _, ok := aliasTargetSet[code]; !ok {
				aliasTargetSet[code] = struct{}{}
				aliasTargets = append(aliasTargets, code)
			}
		}
		loadedTargets, loadErr := catalogRepo.GetActiveMany(aliasTargets)
		if loadErr != nil {
			return nil, fmt.Errorf("preload alias targets: %w", loadErr)
		}
		for code, item := range loadedTargets {
			catalogItems[code] = item
		}
	}
	mappingByName := map[string]*models.Mapping{}
	if mappingRepo != nil {
		mappingByName, err = mappingRepo.FindByRawNames(rawNames)
		if err != nil {
			return nil, fmt.Errorf("preload mappings: %w", err)
		}
	}

	resolutions := make(map[string]matchResolution, len(refs)*3)
	exactSKUMatches, identityMatches, skuMatches, nameMatches, legacyHints, shadowMismatches, unmatched := 0, 0, 0, 0, 0, 0, 0
	for _, ref := range refs {
		var resolved matchResolution
		exactSKU := ref.sku != "" && catalogItemDocumentReady(catalogItems[ref.sku])
		if exactSKU {
			exactSKUMatches++
		}
		if ref.itemID != "" {
			resolved.alias = aliasByKey["identity\x00"+ref.accountKey+"\x00"+ref.itemID+"\x00"+ref.variantID]
			if resolved.alias != nil {
				identityMatches++
			}
		}
		if resolved.alias == nil && ref.itemID == "" && ref.variantID != "" {
			resolved.alias = aliasByKey["variant\x00"+ref.accountKey+"\x00"+ref.variantID]
			if resolved.alias == nil && source == "tiktok" {
				resolved.alias = aliasByKey["sku\x00"+ref.accountKey+"\x00"+ref.variantID]
			}
			if resolved.alias != nil {
				identityMatches++
			}
		}
		if resolved.alias == nil && ref.sku != "" {
			resolved.alias = aliasByKey["sku\x00"+ref.accountKey+"\x00"+ref.sku]
			if resolved.alias != nil {
				skuMatches++
			}
		}
		if resolved.alias == nil && ref.sku == "" && ref.itemID == "" {
			resolved.alias = aliasByKey["name\x00"+ref.accountKey+"\x00"+marketplace.NormalizeKey(ref.rawName, "")]
			if resolved.alias != nil {
				nameMatches++
			}
		}
		if resolved.alias != nil && !catalogItemDocumentReady(catalogItems[normalizeMarketplaceSKU(resolved.alias.ItemCode)]) {
			resolved.alias = nil
		}
		if ref.sku == "" {
			resolved.learned = mappingByName[marketplace.NormalizeKey(ref.rawName, "")]
			if resolved.learned != nil {
				legacyHints++
				if resolved.alias == nil || resolved.alias.ItemCode != resolved.learned.ItemCode || resolved.alias.UnitCode != resolved.learned.UnitCode {
					shadowMismatches++
				}
			}
		}
		if !exactSKU && resolved.alias == nil {
			unmatched++
		}
		if ref.itemID != "" {
			resolutions["identity\x00"+ref.accountKey+"\x00"+ref.itemID+"\x00"+ref.variantID] = resolved
		}
		if ref.itemID == "" && ref.variantID != "" {
			resolutions["variant\x00"+ref.accountKey+"\x00"+ref.variantID] = resolved
		}
		if ref.sku != "" {
			resolutions["sku\x00"+ref.accountKey+"\x00"+ref.sku] = resolved
		}
		resolutions["name\x00"+ref.accountKey+"\x00"+marketplace.NormalizeKey(ref.rawName, "")] = resolved
		resolutions[marketplaceResolutionKey(ref.sku, ref.rawName)] = resolved
	}
	if logger != nil {
		logger.Info("marketplace_mapping_resolution_batch",
			zap.String("source", source), zap.Int("items", len(refs)), zap.Int("exact_sku", exactSKUMatches), zap.Int("identity", identityMatches),
			zap.Int("scoped_sku", skuMatches), zap.Int("scoped_name", nameMatches),
			zap.Int("legacy_review_hint", legacyHints), zap.Int("shadow_mismatch", shadowMismatches), zap.Int("unmatched", unmatched))
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PRODUCT_MAPPING_MASTER_MODE")))
	if mode != "shadow" {
		mode = "active"
	}
	return &marketplaceResolutionBatch{catalog: catalogItems, resolutions: resolutions, exactMasters: map[string]*models.MarketplaceItemAlias{}, aliasUsage: map[string]int{}, mode: mode}, nil
}

func recordMarketplaceResolutionUsage(batch *marketplaceResolutionBatch, resolved matchResolution, exactSKU bool) {
	if exactSKU {
		return
	}
	if batch != nil && resolved.alias != nil {
		if batch.aliasUsage == nil {
			batch.aliasUsage = map[string]int{}
		}
		batch.aliasUsage[resolved.alias.ID]++
	}
}

func flushMarketplaceResolutionUsage(batch *marketplaceResolutionBatch, aliasRepo *repository.MarketplaceAliasRepo, logger *zap.Logger) {
	if batch == nil || aliasRepo == nil || len(batch.aliasUsage) == 0 {
		return
	}
	if err := aliasRepo.IncrementUsageCounts(batch.aliasUsage); err != nil && logger != nil {
		logger.Warn("marketplace mapping usage update failed", zap.Error(err), zap.Int("mappings", len(batch.aliasUsage)))
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
		if cat := lookup(sourceSKU); catalogItemDocumentReady(cat) {
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
	case alias != nil && alias.IsActive && lookup != nil && catalogItemDocumentReady(lookup(alias.ItemCode)):
		bi.ItemCode = &alias.ItemCode
		bi.UnitCode = &alias.UnitCode
		bi.Mapped = true
		return bi, true
	case sourceSKU == "" && learned != nil:
		// Legacy raw-name mappings are surfaced in the review queue only. They
		// are intentionally not trusted for automatic document creation.
		bi.Mapped = false
		return bi, false
	default:
		bi.Mapped = false
		return bi, false
	}
}

func marketplaceBillItemFromResolution(
	source, accountKey string,
	item ShopeeExcelItem,
	defaultUnit string,
	resolved matchResolution,
	batch *marketplaceResolutionBatch,
	aliasRepo *repository.MarketplaceAliasRepo,
	confirmedBy string,
) (models.BillItem, bool, error) {
	rawName := shopeeItemRawName(item.ProductName, item.OptionName, item.RawName)
	price := item.Price
	exactSKU := normalizeMarketplaceSKU(item.SKU) != "" && batch.catalogLookup(item.SKU) != nil
	bi, mapped := marketplaceBillItemFromMatch(rawName, item.SKU, item.Qty, &price, defaultUnit, resolved.alias, resolved.learned, nil, batch.catalogLookup, 1)
	if item.GrossAmount > 0 {
		gross := item.GrossAmount
		bi.GrossAmount = &gross
	}
	bi.DiscountAmount = item.DiscountAmount
	if batch.mode == "shadow" && !mapped && normalizeMarketplaceSKU(item.SKU) == "" && resolved.learned != nil {
		bi.ItemCode = &resolved.learned.ItemCode
		bi.UnitCode = &resolved.learned.UnitCode
		bi.Mapped = true
		mapped = true
	}
	bi.SourceItemID = strings.TrimSpace(item.SourceItemID)
	bi.SourceVariantID = strings.TrimSpace(item.SourceVariantID)
	if resolved.alias != nil {
		id := resolved.alias.ID
		bi.MarketplaceAliasID = &id
	}
	if !exactSKU || aliasRepo == nil {
		return bi, mapped, nil
	}
	// Shopee Excel without a selected shop may resolve exact SKU for this bill,
	// but cannot create a reusable cross-shop master safely.
	if source == "shopee" && accountKey == "default" && bi.SourceItemID == "" {
		return bi, mapped, nil
	}
	cat := batch.catalogLookup(item.SKU)
	if cat == nil {
		return bi, mapped, nil
	}
	identity := models.MarketplaceAliasIdentity{
		Source: source, AccountKey: accountKey, ExternalItemID: bi.SourceItemID,
		ExternalVariantID: bi.SourceVariantID, SourceSKU: item.SKU, RawName: rawName,
	}
	masterKey := "sku\x00" + accountKey + "\x00" + normalizeMarketplaceSKU(item.SKU)
	if bi.SourceItemID != "" {
		masterKey = "identity\x00" + accountKey + "\x00" + bi.SourceItemID + "\x00" + bi.SourceVariantID
	}
	if batch.exactMasters == nil {
		batch.exactMasters = map[string]*models.MarketplaceItemAlias{}
	}
	if existing := batch.exactMasters[masterKey]; existing != nil {
		bi.MarketplaceAliasID = &existing.ID
		batch.aliasUsage[existing.ID]++
		return bi, mapped, nil
	}
	mutation := repository.MarketplaceAliasMutation{
		Identity: identity,
		BillType: "sale", ItemCode: cat.ItemCode, UnitCode: cat.UnitCode,
		MatchMethod: "exact_sku", ScopeConfirmed: true, ConfirmedBy: confirmedBy,
	}
	if resolved.alias != nil {
		version := resolved.alias.UpdatedAt
		mutation.ID = resolved.alias.ID
		mutation.Version = &version
	}
	result, err := aliasRepo.SaveAndApply(mutation)
	if err != nil {
		return bi, false, fmt.Errorf("save exact SKU master: %w", err)
	}
	if result != nil && result.Alias != nil {
		bi.MarketplaceAliasID = &result.Alias.ID
		batch.exactMasters[masterKey] = result.Alias
		batch.aliasUsage[result.Alias.ID]++
	}
	return bi, mapped, nil
}

func marketplaceSourceAccountKey(source, shopID string) string {
	if source == "shopee" && strings.TrimSpace(shopID) != "" {
		return "shop:" + strings.TrimSpace(shopID)
	}
	return "default"
}
