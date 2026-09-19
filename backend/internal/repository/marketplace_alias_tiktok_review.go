package repository

import (
	"fmt"
	"strings"

	"nexflow/internal/marketplace"
	"nexflow/internal/models"
)

func tiktokOrderSnapshotReviewEnabled(filter models.MarketplaceAliasReviewFilter) bool {
	source := strings.ToLower(strings.TrimSpace(filter.Source))
	billType := strings.ToLower(strings.TrimSpace(filter.BillType))
	return (source == "" || source == "tiktok") && (billType == "" || billType == "sale")
}

func tiktokOrderSnapshotReviewQuery(filter models.MarketplaceAliasReviewFilter) (string, []interface{}) {
	conditions := []string{
		"c.disabled_at IS NULL",
		"jsonb_typeof(s.normalized_items)='array'",
		"COALESCE(item->>'product_id','') <> ''",
		"COALESCE(item->>'sku_id','') <> ''",
		`NOT EXISTS (
			SELECT 1
			  FROM bills b
			 WHERE b.source='tiktok'
			   AND b.source_account_key='shop:'||s.shop_id
			   AND b.sml_order_id=s.order_id
			   AND COALESCE(b.raw_data->>'flow','')='tiktok_shop_api_reviewed'
			   AND b.archived_at IS NULL
		)`,
		`NOT EXISTS (
			SELECT 1
			  FROM marketplace_item_aliases a
			 WHERE a.source='tiktok'
			   AND a.account_key='shop:'||s.shop_id
			   AND a.external_item_id=item->>'product_id'
			   AND a.external_variant_id=item->>'sku_id'
			   AND a.is_active=true
		)`,
	}
	args := []interface{}{}
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+query+"%")
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(item->>'product_name','') ILIKE $%d
			OR COALESCE(item->>'sku_name','') ILIKE $%d
			OR COALESCE(item->>'seller_sku','') ILIKE $%d
			OR item->>'product_id' ILIKE $%d
			OR item->>'sku_id' ILIKE $%d
			OR COALESCE(NULLIF(c.label,''),NULLIF(c.shop_name,''),'') ILIKE $%d
		)`, n, n, n, n, n, n))
	}
	query := ` FROM tiktok_shop_order_snapshots s
		JOIN tiktok_shop_connections c ON c.shop_id=s.shop_id
		CROSS JOIN LATERAL jsonb_array_elements(s.normalized_items) item
		WHERE ` + strings.Join(conditions, " AND ")
	return query, args
}

func (r *MarketplaceAliasRepo) countTikTokOrderSnapshotReviewGroups(filter models.MarketplaceAliasReviewFilter) (int, error) {
	if !tiktokOrderSnapshotReviewEnabled(filter) {
		return 0, nil
	}
	from, args := tiktokOrderSnapshotReviewQuery(filter)
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM (
		SELECT s.shop_id,item->>'product_id',item->>'sku_id'`+from+`
		GROUP BY s.shop_id,item->>'product_id',item->>'sku_id'
	) review_groups`, args...).Scan(&total)
	return total, err
}

func (r *MarketplaceAliasRepo) listTikTokOrderSnapshotReviewGroups(filter models.MarketplaceAliasReviewFilter, limit, offset int) ([]models.MarketplaceAliasReviewGroup, error) {
	if !tiktokOrderSnapshotReviewEnabled(filter) || limit <= 0 {
		return []models.MarketplaceAliasReviewGroup{}, nil
	}
	from, args := tiktokOrderSnapshotReviewQuery(filter)
	args = append(args, limit, offset)
	limitParam, offsetParam := len(args)-1, len(args)
	orderBy := "order_count DESC,MAX(s.last_synced_at) DESC,s.shop_id,external_item_id,external_variant_id"
	switch strings.TrimSpace(filter.Sort) {
	case "source":
		orderBy = "s.shop_id,external_item_id,external_variant_id"
	case "name":
		orderBy = "product_name,sku_name,s.shop_id,external_item_id,external_variant_id"
	}
	rows, err := r.db.Query(fmt.Sprintf(`SELECT s.shop_id,
		COALESCE(NULLIF(MAX(c.label),''),NULLIF(MAX(c.shop_name),''),'TikTok Shop '||s.shop_id),
		item->>'product_id' AS external_item_id,
		item->>'sku_id' AS external_variant_id,
		(COALESCE((ARRAY_AGG(item->>'product_name' ORDER BY s.last_synced_at DESC,s.order_id DESC))[1],'')) AS product_name,
		(COALESCE((ARRAY_AGG(item->>'sku_name' ORDER BY s.last_synced_at DESC,s.order_id DESC))[1],'')) AS sku_name,
		(COALESCE((ARRAY_AGG(item->>'seller_sku' ORDER BY s.last_synced_at DESC,s.order_id DESC))[1],'')) AS source_sku,
		COUNT(DISTINCT s.order_id)::integer AS order_count,
		(ARRAY_AGG(s.order_id ORDER BY s.last_synced_at DESC,s.order_id DESC))[1] AS source_order_id
		%s
		GROUP BY s.shop_id,item->>'product_id',item->>'sku_id'
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, from, orderBy, limitParam, offsetParam), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]models.MarketplaceAliasReviewGroup, 0, limit)
	for rows.Next() {
		var shopID, accountName, externalItemID, externalVariantID, productName, skuName, sourceSKU, sourceOrderID string
		var orderCount int
		if err := rows.Scan(&shopID, &accountName, &externalItemID, &externalVariantID, &productName, &skuName, &sourceSKU, &orderCount, &sourceOrderID); err != nil {
			return nil, err
		}
		nameParts := make([]string, 0, 2)
		if value := strings.TrimSpace(productName); value != "" {
			nameParts = append(nameParts, value)
		}
		if value := strings.TrimSpace(skuName); value != "" {
			nameParts = append(nameParts, value)
		}
		rawName := strings.Join(nameParts, " · ")
		if rawName == "" {
			rawName = firstNonEmptyRepository(sourceSKU, "สินค้า TikTok Shop "+externalItemID+"/"+externalVariantID)
		}
		accountKey := "shop:" + shopID
		groups = append(groups, models.MarketplaceAliasReviewGroup{
			GroupKey: "tiktok|" + accountKey + "|identity|" + externalItemID + "|" + externalVariantID,
			Source:   "tiktok", AccountKey: accountKey, AccountName: accountName,
			ExternalItemID: externalItemID, ExternalVariantID: externalVariantID, BillType: "sale",
			SourceSKU: sourceSKU, RawName: rawName, NormalizedKey: marketplace.NormalizeKey(rawName, sourceSKU),
			CatalogProduct: true, DiscoverySource: "tiktok_order_snapshot", SourceReferenceID: sourceOrderID,
			OrderCount: orderCount, InputChannels: []string{"tiktok_shop"},
		})
	}
	return groups, rows.Err()
}
