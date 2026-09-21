package repository

import (
	"fmt"
	"strconv"
	"strings"

	"nexflow/internal/marketplace"
	"nexflow/internal/models"
)

func shopeeCatalogReviewEnabled(filter models.MarketplaceAliasReviewFilter) bool {
	source := strings.ToLower(strings.TrimSpace(filter.Source))
	billType := strings.ToLower(strings.TrimSpace(filter.BillType))
	return (source == "" || source == "shopee") && (billType == "" || billType == "sale")
}

func tiktokCatalogReviewEnabled(filter models.MarketplaceAliasReviewFilter) bool {
	source := strings.ToLower(strings.TrimSpace(filter.Source))
	billType := strings.ToLower(strings.TrimSpace(filter.BillType))
	return (source == "" || source == "tiktok") && (billType == "" || billType == "sale")
}

// tiktokCatalogReviewWhere keeps catalog-only discovery separate from order
// snapshots. If a product/SKU is already present in an order snapshot, that
// snapshot owns the review because it can link the operator to the exact order
// preview. Catalog-only rows deliberately have no bill/order side effect.
func tiktokCatalogReviewWhere(filter models.MarketplaceAliasReviewFilter) (string, []interface{}) {
	conditions := []string{
		"p.is_active=true",
		"sku.is_active=true",
		"c.disabled_at IS NULL",
		`NOT EXISTS (
			SELECT 1 FROM sml_catalog exact_product
			 WHERE exact_product.is_active=true
			   AND exact_product.item_code=btrim(replace(COALESCE(sku.seller_sku,''),chr(65279),''))
		)`,
		`NOT EXISTS (
			SELECT 1 FROM marketplace_item_aliases identity_alias
			 WHERE identity_alias.source='tiktok'
			   AND identity_alias.account_key='shop:'||p.shop_id
			   AND identity_alias.external_item_id=p.product_id
			   AND identity_alias.external_variant_id=sku.sku_id
			   AND identity_alias.is_active=true
		)`,
		"snapshot_item.shop_id IS NULL",
	}
	args := []interface{}{}
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+query+"%")
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(`(
			p.title ILIKE $%d OR sku.seller_sku ILIKE $%d OR sku.variant_name ILIKE $%d
			OR p.product_id ILIKE $%d OR sku.sku_id ILIKE $%d
			OR c.label ILIKE $%d OR c.shop_name ILIKE $%d
		)`, n, n, n, n, n, n, n))
	}
	return strings.Join(conditions, " AND "), args
}

func tiktokCatalogReviewFrom() string {
	return ` FROM tiktok_shop_products p
		JOIN tiktok_shop_product_skus sku USING(shop_id,product_id)
		JOIN tiktok_shop_connections c ON c.shop_id=p.shop_id
		LEFT JOIN (
			SELECT DISTINCT s.shop_id,item->>'product_id' AS product_id,item->>'sku_id' AS sku_id
			  FROM tiktok_shop_order_snapshots s
			 CROSS JOIN LATERAL jsonb_array_elements(s.normalized_items) item
			 WHERE jsonb_typeof(s.normalized_items)='array'
			   AND NOT EXISTS (
				SELECT 1 FROM bills b
				 WHERE b.source='tiktok'
				   AND b.source_account_key='shop:'||s.shop_id
				   AND b.sml_order_id=s.order_id
				   AND COALESCE(b.raw_data->>'flow','')='tiktok_shop_api_reviewed'
				   AND b.archived_at IS NULL
			   )
		) snapshot_item ON snapshot_item.shop_id=p.shop_id
			AND snapshot_item.product_id=p.product_id
			AND snapshot_item.sku_id=sku.sku_id`
}

func (r *MarketplaceAliasRepo) countTikTokCatalogReviewGroups(filter models.MarketplaceAliasReviewFilter) (int, error) {
	if !tiktokCatalogReviewEnabled(filter) {
		return 0, nil
	}
	where, args := tiktokCatalogReviewWhere(filter)
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*)`+tiktokCatalogReviewFrom()+`
		WHERE `+where, args...).Scan(&total)
	return total, err
}

func (r *MarketplaceAliasRepo) listTikTokCatalogReviewGroups(filter models.MarketplaceAliasReviewFilter, limit, offset int) ([]models.MarketplaceAliasReviewGroup, error) {
	if !tiktokCatalogReviewEnabled(filter) || limit <= 0 {
		return []models.MarketplaceAliasReviewGroup{}, nil
	}
	where, args := tiktokCatalogReviewWhere(filter)
	args = append(args, limit, offset)
	limitParam, offsetParam := len(args)-1, len(args)
	orderBy := "p.shop_id,p.product_id,sku.sku_id"
	if strings.TrimSpace(filter.Sort) == "name" {
		orderBy = "p.title,sku.seller_sku,p.shop_id,p.product_id,sku.sku_id"
	}
	rows, err := r.db.Query(fmt.Sprintf(`SELECT p.shop_id,
		COALESCE(NULLIF(c.label,''),NULLIF(c.shop_name,''),'TikTok Shop '||p.shop_id),
		p.product_id,sku.sku_id,p.title,sku.seller_sku,sku.variant_name
		%s
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, tiktokCatalogReviewFrom(), where, orderBy, limitParam, offsetParam), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]models.MarketplaceAliasReviewGroup, 0, limit)
	for rows.Next() {
		var shopID, accountName, productID, skuID, title, sellerSKU, variantName string
		if err := rows.Scan(&shopID, &accountName, &productID, &skuID, &title, &sellerSKU, &variantName); err != nil {
			return nil, err
		}
		sourceSKU := normalizeAliasSKU(sellerSKU)
		productName := strings.TrimSpace(title)
		variantName = strings.TrimSpace(variantName)
		rawName := productName
		if variantName != "" {
			rawName = strings.TrimSpace(productName + " / " + variantName)
		}
		if rawName == "" {
			rawName = firstNonEmptyRepository(sourceSKU, "สินค้า TikTok Shop "+productID+"/"+skuID)
		}
		accountKey := "shop:" + shopID
		groups = append(groups, models.MarketplaceAliasReviewGroup{
			GroupKey: "tiktok|" + accountKey + "|identity|" + productID + "|" + skuID,
			Source:   "tiktok", AccountKey: accountKey, AccountName: accountName,
			ExternalItemID: productID, ExternalVariantID: skuID, BillType: "sale",
			SourceSKU: sourceSKU, SourceProductName: productName, SourceVariantName: variantName,
			RawName: rawName, NormalizedKey: marketplace.NormalizeKey(rawName, sourceSKU),
			CatalogProduct: true, DiscoverySource: "tiktok_product_catalog", InputChannels: []string{"tiktok_shop"},
		})
	}
	return groups, rows.Err()
}

func shopeeCatalogReviewWhere(filter models.MarketplaceAliasReviewFilter) (string, []interface{}) {
	conditions := []string{
		"p.is_active=true",
		"sc.disabled_at IS NULL",
		`NOT EXISTS (
			SELECT 1 FROM sml_catalog exact_product
			 WHERE exact_product.is_active=true
			   AND exact_product.item_code=btrim(replace(
				COALESCE(NULLIF(btrim(p.model_sku),''),p.item_sku,''),chr(65279),''
			   ))
		)`,
		`NOT EXISTS (
			SELECT 1 FROM marketplace_item_aliases linked_alias
			 WHERE linked_alias.id=m.marketplace_alias_id
			   AND linked_alias.source='shopee'
			   AND linked_alias.is_active=true
		)`,
		`NOT EXISTS (
			SELECT 1 FROM marketplace_item_aliases identity_alias
			 WHERE identity_alias.source='shopee'
			   AND identity_alias.account_key='shop:'||p.shop_id::text
			   AND identity_alias.external_item_id=p.item_id::text
			   AND identity_alias.external_variant_id=p.model_id::text
			   AND identity_alias.is_active=true
		)`,
		`NOT EXISTS (
			SELECT 1
			  FROM bill_items bi
			  JOIN bills b ON b.id=bi.bill_id
			 WHERE b.source='shopee'
			   AND b.source_account_key='shop:'||p.shop_id::text
			   AND b.bill_type='sale'
			   AND b.status IN ('pending','needs_review')
			   AND b.archived_at IS NULL
			   AND (bi.mapped IS DISTINCT FROM true OR COALESCE(bi.item_code,'')='')
			   AND bi.source_item_id=p.item_id::text
			   AND bi.source_variant_id=p.model_id::text
			   AND NOT EXISTS (
				SELECT 1 FROM sml_catalog exact_product
				 WHERE exact_product.is_active=true
				   AND exact_product.item_code=btrim(replace(COALESCE(bi.source_sku,''),chr(65279),''))
			   )
		)`,
	}
	args := []interface{}{}
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+query+"%")
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(`(p.item_name ILIKE $%d OR p.model_name ILIKE $%d
			OR p.item_sku ILIKE $%d OR p.model_sku ILIKE $%d OR p.item_id::text ILIKE $%d
			OR p.model_id::text ILIKE $%d OR sc.label ILIKE $%d OR sc.shop_name ILIKE $%d)`, n, n, n, n, n, n, n, n))
	}
	return strings.Join(conditions, " AND "), args
}

func (r *MarketplaceAliasRepo) countShopeeCatalogReviewGroups(filter models.MarketplaceAliasReviewFilter) (int, error) {
	if !shopeeCatalogReviewEnabled(filter) {
		return 0, nil
	}
	where, args := shopeeCatalogReviewWhere(filter)
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*)
		FROM shopee_stock_products p
		JOIN shopee_stock_mappings m USING(shop_id,item_id,model_id)
		JOIN shopee_api_connections sc ON sc.shop_id=p.shop_id
		WHERE `+where, args...).Scan(&total)
	return total, err
}

func (r *MarketplaceAliasRepo) listShopeeCatalogReviewGroups(filter models.MarketplaceAliasReviewFilter, limit, offset int) ([]models.MarketplaceAliasReviewGroup, error) {
	if !shopeeCatalogReviewEnabled(filter) || limit <= 0 {
		return []models.MarketplaceAliasReviewGroup{}, nil
	}
	where, args := shopeeCatalogReviewWhere(filter)
	args = append(args, limit, offset)
	limitParam, offsetParam := len(args)-1, len(args)
	orderBy := "'shopee|shop:'||p.shop_id::text||'|identity|'||p.item_id::text||'|'||p.model_id::text"
	if strings.TrimSpace(filter.Sort) == "name" {
		orderBy = "p.item_name,p.model_name,p.item_id,p.model_id"
	}
	rows, err := r.db.Query(fmt.Sprintf(`SELECT p.shop_id,
		COALESCE(NULLIF(sc.label,''),NULLIF(sc.shop_name,''),'Shop '||p.shop_id::text),
		p.item_id,p.model_id,p.item_name,p.model_name,p.item_sku,p.model_sku
		FROM shopee_stock_products p
		JOIN shopee_stock_mappings m USING(shop_id,item_id,model_id)
		JOIN shopee_api_connections sc ON sc.shop_id=p.shop_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, where, orderBy, limitParam, offsetParam), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]models.MarketplaceAliasReviewGroup, 0, limit)
	for rows.Next() {
		var shopID, itemID, modelID int64
		var accountName, itemName, modelName, itemSKU, modelSKU string
		if err := rows.Scan(&shopID, &accountName, &itemID, &modelID, &itemName, &modelName, &itemSKU, &modelSKU); err != nil {
			return nil, err
		}
		sourceSKU := normalizeAliasSKU(firstNonEmptyRepository(modelSKU, itemSKU))
		nameParts := make([]string, 0, 2)
		if value := strings.TrimSpace(itemName); value != "" {
			nameParts = append(nameParts, value)
		}
		if value := strings.TrimSpace(modelName); value != "" {
			nameParts = append(nameParts, value)
		}
		rawName := strings.Join(nameParts, " · ")
		if rawName == "" {
			rawName = firstNonEmptyRepository(sourceSKU, fmt.Sprintf("สินค้า Shopee %d/%d", itemID, modelID))
		}
		accountKey := "shop:" + strconv.FormatInt(shopID, 10)
		externalItemID, externalVariantID := strconv.FormatInt(itemID, 10), strconv.FormatInt(modelID, 10)
		groups = append(groups, models.MarketplaceAliasReviewGroup{
			GroupKey: "shopee|" + accountKey + "|identity|" + externalItemID + "|" + externalVariantID,
			Source:   "shopee", AccountKey: accountKey, AccountName: accountName,
			ExternalItemID: externalItemID, ExternalVariantID: externalVariantID, BillType: "sale",
			SourceSKU: sourceSKU, RawName: rawName, NormalizedKey: marketplace.NormalizeKey(rawName, sourceSKU),
			CatalogProduct: true, DiscoverySource: "product_catalog", InputChannels: []string{"shopee"},
		})
	}
	return groups, rows.Err()
}
