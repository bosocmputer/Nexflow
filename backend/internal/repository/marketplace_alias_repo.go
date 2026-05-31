package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"nexflow/internal/marketplace"
	"nexflow/internal/models"
)

type MarketplaceAliasRepo struct {
	db *sql.DB
}

func NewMarketplaceAliasRepo(db *sql.DB) *MarketplaceAliasRepo {
	return &MarketplaceAliasRepo{db: db}
}

func (r *MarketplaceAliasRepo) Find(source, sourceSKU, rawName string) (*models.MarketplaceItemAlias, error) {
	sourceSKU = normalizeAliasSKU(sourceSKU)
	normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
	if sourceSKU != "" {
		alias, err := r.findByQuery(`source = $1 AND source_sku = $2`, source, sourceSKU)
		if err != nil || alias != nil {
			return alias, err
		}
	}
	if normalizedKey != "" {
		return r.findByQuery(`source = $1 AND normalized_key = $2`, source, normalizedKey)
	}
	return nil, nil
}

func (r *MarketplaceAliasRepo) findByQuery(where string, args ...interface{}) (*models.MarketplaceItemAlias, error) {
	row := r.db.QueryRow(
		`SELECT id, source, source_sku, raw_name, normalized_key, item_code, unit_code,
		        confidence, confirmed_by, usage_count, last_used_at, created_at, updated_at
		   FROM marketplace_item_aliases
		  WHERE `+where+`
		  LIMIT 1`, args...,
	)
	return scanAlias(row)
}

func scanAlias(row *sql.Row) (*models.MarketplaceItemAlias, error) {
	var a models.MarketplaceItemAlias
	err := row.Scan(
		&a.ID, &a.Source, &a.SourceSKU, &a.RawName, &a.NormalizedKey, &a.ItemCode, &a.UnitCode,
		&a.Confidence, &a.ConfirmedBy, &a.UsageCount, &a.LastUsedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *MarketplaceAliasRepo) Upsert(source, sourceSKU, rawName, itemCode, unitCode, confirmedBy string) (*models.MarketplaceItemAlias, error) {
	sourceSKU = normalizeAliasSKU(sourceSKU)
	normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
	if sourceSKU == "" && normalizedKey == "" {
		return nil, fmt.Errorf("source_sku or normalized_key required")
	}
	var confirmedByArg interface{}
	if confirmedBy != "" {
		confirmedByArg = confirmedBy
	}

	query := `
		INSERT INTO marketplace_item_aliases
		    (source, source_sku, raw_name, normalized_key, item_code, unit_code, confidence, confirmed_by, usage_count, last_used_at)
		VALUES ($1, $2, $3, $4, $5, $6, 1.0, $7, 1, NOW())
		ON CONFLICT %s DO UPDATE
		   SET raw_name = EXCLUDED.raw_name,
		       normalized_key = EXCLUDED.normalized_key,
		       item_code = EXCLUDED.item_code,
		       unit_code = EXCLUDED.unit_code,
		       confidence = 1.0,
		       confirmed_by = COALESCE(EXCLUDED.confirmed_by, marketplace_item_aliases.confirmed_by),
		       usage_count = marketplace_item_aliases.usage_count + 1,
		       last_used_at = NOW(),
		       updated_at = NOW()
		RETURNING id, source, source_sku, raw_name, normalized_key, item_code, unit_code,
		          confidence, confirmed_by, usage_count, last_used_at, created_at, updated_at`
	conflict := "(source, normalized_key) WHERE source_sku = '' AND normalized_key <> ''"
	if sourceSKU != "" {
		conflict = "(source, source_sku) WHERE source_sku <> ''"
	}
	row := r.db.QueryRow(fmt.Sprintf(query, conflict), source, sourceSKU, rawName, normalizedKey, itemCode, unitCode, confirmedByArg)
	return scanAlias(row)
}

func (r *MarketplaceAliasRepo) IncrementUsage(id string) error {
	_, err := r.db.Exec(
		`UPDATE marketplace_item_aliases
		    SET usage_count = usage_count + 1,
		        last_used_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1`, id,
	)
	return err
}

func (r *MarketplaceAliasRepo) ReviewGroups(billType string, limit int) ([]models.MarketplaceAliasReviewGroup, error) {
	result, err := r.ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: billType,
		Page:     1,
		PerPage:  limit,
		Sort:     "impact",
	})
	if err != nil {
		return nil, err
	}
	return result.Groups, nil
}

func (r *MarketplaceAliasRepo) ReviewGroupsPaged(filter models.MarketplaceAliasReviewFilter) (models.MarketplaceAliasReviewResult, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}
	sortKey := strings.TrimSpace(filter.Sort)
	if sortKey == "" {
		sortKey = "impact"
	}

	conditions := []string{
		"b.source IN ('shopee','lazada','tiktok')",
		"b.status IN ('pending', 'needs_review')",
		"(bi.mapped IS DISTINCT FROM TRUE OR COALESCE(bi.item_code, '') = '')",
	}
	args := []interface{}{}
	if filter.BillType != "" {
		args = append(args, filter.BillType)
		conditions = append(conditions, fmt.Sprintf("b.bill_type = $%d", len(args)))
	}
	if filter.Source != "" {
		args = append(args, filter.Source)
		conditions = append(conditions, fmt.Sprintf("b.source = $%d", len(args)))
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		args = append(args, "%"+q+"%")
		conditions = append(conditions, fmt.Sprintf("(bi.raw_name ILIKE $%d OR COALESCE(bi.source_sku, '') ILIKE $%d)", len(args), len(args)))
	}

	rows, err := r.db.Query(
		fmt.Sprintf(`SELECT b.id, b.source, b.bill_type, bi.id, bi.raw_name,
		        COALESCE(bi.source_sku, ''), COALESCE(bi.candidates, '[]')
		   FROM bill_items bi
		   JOIN bills b ON b.id = bi.bill_id
		  WHERE %s
		  ORDER BY b.created_at DESC`, strings.Join(conditions, " AND ")),
		args...,
	)
	if err != nil {
		return models.MarketplaceAliasReviewResult{}, err
	}
	defer rows.Close()

	type groupAgg struct {
		models.MarketplaceAliasReviewGroup
		bills map[string]bool
	}
	groups := map[string]*groupAgg{}
	for rows.Next() {
		var billID, source, bt, itemID, rawName, sourceSKU string
		var candidatesRaw []byte
		if err := rows.Scan(&billID, &source, &bt, &itemID, &rawName, &sourceSKU, &candidatesRaw); err != nil {
			return models.MarketplaceAliasReviewResult{}, err
		}
		normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
		groupKey := source + "|name|" + normalizedKey
		if sourceSKU != "" {
			groupKey = source + "|sku|" + sourceSKU
		}
		g := groups[groupKey]
		if g == nil {
			var candidates []models.CatalogMatch
			_ = json.Unmarshal(candidatesRaw, &candidates)
			g = &groupAgg{
				MarketplaceAliasReviewGroup: models.MarketplaceAliasReviewGroup{
					GroupKey:      groupKey,
					Source:        source,
					BillType:      bt,
					SourceSKU:     sourceSKU,
					RawName:       rawName,
					NormalizedKey: normalizedKey,
					ItemCount:     0,
					Candidates:    candidates,
				},
				bills: map[string]bool{},
			}
			if len(candidates) > 0 {
				c := candidates[0]
				g.SuggestedMatch = &c
			}
			groups[groupKey] = g
		}
		g.ItemCount++
		g.bills[billID] = true
		_ = itemID
	}
	if err := rows.Err(); err != nil {
		return models.MarketplaceAliasReviewResult{}, err
	}

	out := make([]models.MarketplaceAliasReviewGroup, 0, len(groups))
	for _, g := range groups {
		g.BillCount = len(g.bills)
		out = append(out, g.MarketplaceAliasReviewGroup)
	}
	sortMarketplaceReviewGroups(out, sortKey)
	total := len(out)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return models.MarketplaceAliasReviewResult{
		Groups:  out[start:end],
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}, nil
}

func sortMarketplaceReviewGroups(groups []models.MarketplaceAliasReviewGroup, sortKey string) {
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		switch sortKey {
		case "source":
			if a.Source != b.Source {
				return a.Source < b.Source
			}
		case "name":
			if a.RawName != b.RawName {
				return a.RawName < b.RawName
			}
		case "score":
			return aliasSuggestedScore(a) < aliasSuggestedScore(b)
		default:
			if a.BillCount != b.BillCount {
				return a.BillCount > b.BillCount
			}
			if a.ItemCount != b.ItemCount {
				return a.ItemCount > b.ItemCount
			}
		}
		if a.ItemCount != b.ItemCount {
			return a.ItemCount > b.ItemCount
		}
		return a.GroupKey < b.GroupKey
	})
}

func aliasSuggestedScore(g models.MarketplaceAliasReviewGroup) float64 {
	if g.SuggestedMatch == nil {
		return -1
	}
	return g.SuggestedMatch.Score
}

func (r *MarketplaceAliasRepo) ApplyToOpenItems(source, billType, sourceSKU, normalizedKey, rawName, itemCode, unitCode string) (int, int, error) {
	rows, err := r.db.Query(
		`SELECT bi.id, bi.raw_name, COALESCE(bi.source_sku, '')
		   FROM bill_items bi
		   JOIN bills b ON b.id = bi.bill_id
		  WHERE b.source = $1
		    AND b.bill_type = $2
		    AND b.status IN ('pending', 'needs_review')
		    AND (bi.mapped IS DISTINCT FROM TRUE OR COALESCE(bi.item_code, '') <> $3 OR COALESCE(bi.unit_code, '') <> $4)`,
		source, billType, itemCode, unitCode,
	)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, rowRaw, rowSKU string
		if err := rows.Scan(&id, &rowRaw, &rowSKU); err != nil {
			return 0, 0, err
		}
		if sourceSKU != "" && rowSKU == sourceSKU {
			ids = append(ids, id)
			continue
		}
		if sourceSKU == "" && marketplace.NormalizeKey(rowRaw, rowSKU) == normalizedKey {
			ids = append(ids, id)
			continue
		}
		if rawName != "" && rowRaw == rawName {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(ids) == 0 {
		return 0, 0, nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	placeholders := make([]string, len(ids))
	args := []interface{}{itemCode, unitCode}
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, id)
	}
	res, err := tx.Exec(
		`UPDATE bill_items
		    SET item_code = $1, unit_code = $2, mapped = TRUE
		  WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...,
	)
	if err != nil {
		return 0, 0, err
	}
	applied, _ := res.RowsAffected()

	readyRes, err := tx.Exec(
		`UPDATE bills b
		    SET status = 'pending',
		        error_msg = NULL
		  WHERE b.source = $1
		    AND b.bill_type = $2
		    AND b.status = 'needs_review'
		    AND NOT EXISTS (
		      SELECT 1 FROM bill_items bi
		       WHERE bi.bill_id = b.id
		         AND (COALESCE(bi.item_code, '') = '' OR bi.mapped IS DISTINCT FROM TRUE)
		    )`,
		source, billType,
	)
	if err != nil {
		return 0, 0, err
	}
	ready, _ := readyRes.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return int(applied), int(ready), nil
}

func normalizeAliasSKU(s string) string {
	s = strings.ReplaceAll(s, "\ufeff", "")
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "nan") || strings.EqualFold(s, "null") || s == "-" {
		return ""
	}
	return s
}
