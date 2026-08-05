package repository

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"

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
		return r.findByQuery(`a.source = $1 AND a.source_sku = $2`, source, sourceSKU)
	}
	if normalizedKey != "" {
		return r.findByQuery(`a.source = $1 AND a.source_sku = ''
			AND btrim(regexp_replace(replace(a.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = $2`, source, normalizedKey)
	}
	return nil, nil
}

func (r *MarketplaceAliasRepo) findByQuery(where string, args ...interface{}) (*models.MarketplaceItemAlias, error) {
	row := r.db.QueryRow(
		`SELECT a.id, a.source, a.source_sku, a.raw_name, a.normalized_key, a.item_code, a.unit_code,
		        a.confidence, a.confirmed_by, a.usage_count, a.last_used_at, a.created_at, a.updated_at,
		        a.is_active
		   FROM marketplace_item_aliases a
		   JOIN sml_catalog c ON c.item_code = a.item_code AND c.is_active = TRUE
		  WHERE a.is_active = TRUE AND (`+where+`)
		  LIMIT 1`, args...,
	)
	return scanAlias(row)
}

// FindMany preloads aliases for a marketplace batch. SKU aliases remain exact
// and case-sensitive; no-SKU aliases compare BOM/whitespace-normalized names.
func (r *MarketplaceAliasRepo) FindMany(source string, sourceSKUs, normalizedNames []string) (map[string]*models.MarketplaceItemAlias, error) {
	result := make(map[string]*models.MarketplaceItemAlias, len(sourceSKUs)+len(normalizedNames))
	if len(sourceSKUs) == 0 && len(normalizedNames) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(`
		SELECT a.id, a.source, a.source_sku, a.raw_name, a.normalized_key, a.item_code, a.unit_code,
		       a.confidence, a.confirmed_by, a.usage_count, a.last_used_at, a.created_at, a.updated_at,
		       a.is_active
		FROM marketplace_item_aliases a
		JOIN sml_catalog c ON c.item_code = a.item_code AND c.is_active = TRUE
		WHERE a.is_active = TRUE AND a.source = $1
		  AND ((a.source_sku <> '' AND a.source_sku = ANY($2))
		    OR (a.source_sku = '' AND
		        btrim(regexp_replace(replace(a.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = ANY($3)))
	`, source, pq.Array(sourceSKUs), pq.Array(normalizedNames))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		alias, err := scanAlias(rows)
		if err != nil {
			return nil, err
		}
		key := "name\x00" + marketplace.NormalizeKey(alias.RawName, "")
		if alias.SourceSKU != "" {
			key = "sku\x00" + alias.SourceSKU
		}
		result[key] = alias
	}
	return result, rows.Err()
}

type aliasScanner interface {
	Scan(dest ...interface{}) error
}

func scanAlias(row aliasScanner) (*models.MarketplaceItemAlias, error) {
	var a models.MarketplaceItemAlias
	err := row.Scan(
		&a.ID, &a.Source, &a.SourceSKU, &a.RawName, &a.NormalizedKey, &a.ItemCode, &a.UnitCode,
		&a.Confidence, &a.ConfirmedBy, &a.UsageCount, &a.LastUsedAt, &a.CreatedAt, &a.UpdatedAt,
		&a.IsActive,
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
	rawName = marketplace.NormalizeKey(rawName, "")
	normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
	if sourceSKU == "" && normalizedKey == "" {
		return nil, fmt.Errorf("source_sku or normalized_key required")
	}
	var confirmedByArg interface{}
	if confirmedBy != "" {
		confirmedByArg = confirmedBy
	}
	if sourceSKU == "" {
		row := r.db.QueryRow(`
			UPDATE marketplace_item_aliases
			SET raw_name = $2, item_code = $4, unit_code = $5, confidence = 1.0,
			    confirmed_by = COALESCE($6, confirmed_by), usage_count = usage_count + 1,
			    last_used_at = NOW(), is_active = TRUE, updated_at = NOW()
			WHERE id = (
			  SELECT id FROM marketplace_item_aliases
			  WHERE source = $1 AND source_sku = ''
			    AND btrim(regexp_replace(replace(raw_name, chr(65279), ''), '\s+', ' ', 'g')) = $3
			  ORDER BY updated_at DESC LIMIT 1
			)
			RETURNING id, source, source_sku, raw_name, normalized_key, item_code, unit_code,
			          confidence, confirmed_by, usage_count, last_used_at, created_at, updated_at,
			          is_active`, source, rawName, normalizedKey, itemCode, unitCode, confirmedByArg)
		if alias, err := scanAlias(row); err != nil {
			return nil, err
		} else if alias != nil {
			return alias, nil
		}
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
		       is_active = TRUE,
		       updated_at = NOW()
		RETURNING id, source, source_sku, raw_name, normalized_key, item_code, unit_code,
		          confidence, confirmed_by, usage_count, last_used_at, created_at, updated_at,
		          is_active`
	conflict := "(source, normalized_key) WHERE source_sku = '' AND normalized_key <> ''"
	if sourceSKU != "" {
		conflict = "(source, source_sku) WHERE source_sku <> ''"
	}
	row := r.db.QueryRow(fmt.Sprintf(query, conflict), source, sourceSKU, rawName, normalizedKey, itemCode, unitCode, confirmedByArg)
	return scanAlias(row)
}

func (r *MarketplaceAliasRepo) List(source, query string, page, perPage int) ([]models.MarketplaceItemAlias, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	conditions := []string{"a.is_active = TRUE"}
	args := []interface{}{}
	if source != "" {
		args = append(args, source)
		conditions = append(conditions, fmt.Sprintf("a.source = $%d", len(args)))
	}
	if strings.TrimSpace(query) != "" {
		args = append(args, "%"+strings.TrimSpace(query)+"%")
		conditions = append(conditions, fmt.Sprintf("(a.source_sku ILIKE $%d OR a.raw_name ILIKE $%d OR a.item_code ILIKE $%d OR c.item_name ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM marketplace_item_aliases a LEFT JOIN sml_catalog c ON c.item_code = a.item_code WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.Query(fmt.Sprintf(`
		SELECT a.id, a.source, a.source_sku, a.raw_name, a.normalized_key,
		       a.item_code, a.unit_code, a.confidence, a.confirmed_by,
		       a.usage_count, a.last_used_at, a.created_at, a.updated_at,
		       a.is_active, COALESCE(c.item_name, ''), COALESCE(u.email, ''),
		       COALESCE(c.is_active, FALSE),
		       (SELECT COUNT(*)
		          FROM bill_items bi JOIN bills b ON b.id = bi.bill_id
		         WHERE b.source = a.source
		           AND b.bill_type = 'sale'
		           AND b.status IN ('pending', 'needs_review')
		           AND b.archived_at IS NULL
		           AND NOT EXISTS (
		             SELECT 1 FROM sml_catalog exact_product
		              WHERE exact_product.is_active = TRUE
		                AND exact_product.item_code = btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), ''))
		           )
		           AND ((a.source_sku <> '' AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = a.source_sku)
		             OR (a.source_sku = ''
		                 AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = ''
		                 AND btrim(regexp_replace(replace(bi.raw_name, chr(65279), ''), '\s+', ' ', 'g')) =
		                     btrim(regexp_replace(replace(a.raw_name, chr(65279), ''), '\s+', ' ', 'g')))))
		FROM marketplace_item_aliases a
		LEFT JOIN sml_catalog c ON c.item_code = a.item_code
		LEFT JOIN users u ON u.id = a.confirmed_by
		WHERE %s
		ORDER BY a.updated_at DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	aliases := make([]models.MarketplaceItemAlias, 0, perPage)
	for rows.Next() {
		var alias models.MarketplaceItemAlias
		if err := rows.Scan(
			&alias.ID, &alias.Source, &alias.SourceSKU, &alias.RawName,
			&alias.NormalizedKey, &alias.ItemCode, &alias.UnitCode,
			&alias.Confidence, &alias.ConfirmedBy, &alias.UsageCount,
			&alias.LastUsedAt, &alias.CreatedAt, &alias.UpdatedAt,
			&alias.IsActive, &alias.ItemName, &alias.ConfirmedName, &alias.ProductActive,
			&alias.OpenItemCount,
		); err != nil {
			return nil, 0, err
		}
		aliases = append(aliases, alias)
	}
	return aliases, total, rows.Err()
}

var ErrMarketplaceAliasConflict = fmt.Errorf("marketplace alias changed")

func (r *MarketplaceAliasRepo) Update(id, itemCode, unitCode, confirmedBy string, version time.Time) (*models.MarketplaceItemAlias, error) {
	var confirmedByArg interface{}
	if confirmedBy != "" {
		confirmedByArg = confirmedBy
	}
	row := r.db.QueryRow(`
		UPDATE marketplace_item_aliases
		SET item_code = $1, unit_code = $2, confirmed_by = COALESCE($3, confirmed_by),
		    confidence = 1.0, updated_at = NOW()
		WHERE id = $4 AND is_active = TRUE AND updated_at = $5
		RETURNING id, source, source_sku, raw_name, normalized_key, item_code, unit_code,
		          confidence, confirmed_by, usage_count, last_used_at, created_at, updated_at,
		          is_active`, itemCode, unitCode, confirmedByArg, id, version)
	alias, err := scanAlias(row)
	if err == sql.ErrNoRows {
		return nil, ErrMarketplaceAliasConflict
	}
	return alias, err
}

func (r *MarketplaceAliasRepo) Deactivate(id string, version time.Time) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE marketplace_item_aliases
		SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1 AND is_active = TRUE AND updated_at = $2`, id, version)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *MarketplaceAliasRepo) IncrementUsage(id string) error {
	_, err := r.db.Exec(
		`UPDATE marketplace_item_aliases
		    SET usage_count = usage_count + 1,
		        last_used_at = NOW()
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
		        COALESCE(bi.source_sku, '')
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
		if err := rows.Scan(&billID, &source, &bt, &itemID, &rawName, &sourceSKU); err != nil {
			return models.MarketplaceAliasReviewResult{}, err
		}
		normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
		groupKey := source + "|name|" + normalizedKey
		if sourceSKU != "" {
			groupKey = source + "|sku|" + sourceSKU
		}
		g := groups[groupKey]
		if g == nil {
			g = &groupAgg{
				MarketplaceAliasReviewGroup: models.MarketplaceAliasReviewGroup{
					GroupKey:      groupKey,
					Source:        source,
					BillType:      bt,
					SourceSKU:     sourceSKU,
					RawName:       rawName,
					NormalizedKey: normalizedKey,
					ItemCount:     0,
				},
				bills: map[string]bool{},
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

func (r *MarketplaceAliasRepo) ApplyToOpenItems(source, billType, sourceSKU, normalizedKey, _ string, itemCode, unitCode string) (int, int, error) {
	rows, err := r.db.Query(
		`SELECT bi.id
		   FROM bill_items bi
		   JOIN bills b ON b.id = bi.bill_id
		  WHERE b.source = $1
		    AND b.bill_type = $2
		    AND b.status IN ('pending', 'needs_review')
		    AND b.archived_at IS NULL
		    AND NOT EXISTS (
		      SELECT 1 FROM sml_catalog c
		       WHERE c.is_active = TRUE
		         AND c.item_code = btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), ''))
		    )
		    AND (bi.mapped IS DISTINCT FROM TRUE OR COALESCE(bi.item_code, '') <> $3 OR COALESCE(bi.unit_code, '') <> $4)
		    AND (
		      ($5 <> '' AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = $5)
		      OR
		      ($5 = ''
		       AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = ''
		       AND btrim(regexp_replace(replace(bi.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = $6)
		    )`,
		source, billType, itemCode, unitCode, sourceSKU, normalizedKey,
	)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		ids = append(ids, id)
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
