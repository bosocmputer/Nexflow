package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
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

const aliasSelectColumns = `a.id, a.source, a.account_key, a.external_item_id, a.external_variant_id,
	a.source_sku, a.raw_name, a.normalized_key, a.item_code, a.unit_code,
	a.confidence, a.confirmed_by, a.usage_count, a.last_used_at, a.created_at, a.updated_at,
	a.is_active, a.match_method, a.scope_confirmed,
	a.external_parent_id, a.parent_key, a.parent_key_kind, a.source_product_name, a.source_variant_name,
	a.mapping_revision, a.metadata_updated_at, a.quantity_multiplier,
	a.unit_stand_value::text, a.unit_divide_value::text, a.unit_catalog_generation::text,
	a.conversion_status, a.sales_enabled, a.stock_policy`

type MarketplaceAliasMutation struct {
	ID             string
	Identity       models.MarketplaceAliasIdentity
	BillType       string
	ItemCode       string
	UnitCode       string
	MatchMethod    string
	ScopeConfirmed bool
	ConfirmedBy    string
	Version        *time.Time
}

type MarketplaceAliasSaveResult struct {
	Alias        *models.MarketplaceItemAlias
	AppliedItems int
	ReadyBills   int
	Impact       models.MarketplaceAliasImpact
}

func (r *MarketplaceAliasRepo) Find(source, sourceSKU, rawName string) (*models.MarketplaceItemAlias, error) {
	return r.FindScoped(models.MarketplaceAliasIdentity{Source: source, AccountKey: "default", SourceSKU: sourceSKU, RawName: rawName})
}

func (r *MarketplaceAliasRepo) FindScoped(identity models.MarketplaceAliasIdentity) (*models.MarketplaceItemAlias, error) {
	identity = normalizeAliasIdentity(identity)
	if identity.ExternalItemID != "" {
		return r.findByQuery(`a.source = $1 AND a.account_key = $2 AND a.external_item_id = $3 AND a.external_variant_id = $4`,
			identity.Source, identity.AccountKey, identity.ExternalItemID, identity.ExternalVariantID)
	}
	if identity.Source == "tiktok" && identity.ExternalVariantID != "" {
		return r.findByQuery(`a.source = $1 AND a.account_key = $2 AND a.external_item_id = '' AND a.external_variant_id = $3`,
			identity.Source, identity.AccountKey, identity.ExternalVariantID)
	}
	source := identity.Source
	sourceSKU := identity.SourceSKU
	rawName := identity.RawName
	sourceSKU = normalizeAliasSKU(sourceSKU)
	normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
	if sourceSKU != "" {
		return r.findByQuery(`a.source = $1 AND a.account_key = $2 AND a.source_sku = $3`, source, identity.AccountKey, sourceSKU)
	}
	if normalizedKey != "" {
		return r.findByQuery(`a.source = $1 AND a.account_key = $2 AND a.source_sku = '' AND a.external_item_id = ''
			AND a.normalized_key = $3`, source, identity.AccountKey, normalizedKey)
	}
	return nil, nil
}

func (r *MarketplaceAliasRepo) findByQuery(where string, args ...interface{}) (*models.MarketplaceItemAlias, error) {
	row := r.db.QueryRow(
		`SELECT `+aliasSelectColumns+`
		   FROM marketplace_item_aliases a
		   JOIN sml_catalog c ON c.item_code = a.item_code AND c.is_active = TRUE
		  WHERE a.is_active = TRUE AND a.scope_confirmed = TRUE AND (`+where+`)
		  LIMIT 1`, args...,
	)
	return scanAlias(row)
}

func (r *MarketplaceAliasRepo) GetByID(id string) (*models.MarketplaceItemAlias, error) {
	row := r.db.QueryRow(`SELECT `+aliasSelectColumns+`
		FROM marketplace_item_aliases a
		WHERE a.is_active=true AND a.id=$1`, id)
	return scanAlias(row)
}

// FindMany preloads aliases for a marketplace batch. SKU aliases remain exact
// and case-sensitive; no-SKU aliases compare BOM/whitespace-normalized names.
func (r *MarketplaceAliasRepo) FindMany(source string, sourceSKUs, normalizedNames []string) (map[string]*models.MarketplaceItemAlias, error) {
	aliases, err := r.FindManyScoped(source, []string{"default"}, nil, nil, sourceSKUs, normalizedNames)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*models.MarketplaceItemAlias, len(aliases))
	for _, alias := range aliases {
		key := "name\x00" + marketplace.NormalizeKey(alias.RawName, "")
		if alias.SourceSKU != "" {
			key = "sku\x00" + alias.SourceSKU
		}
		result[key] = alias
	}
	return result, nil
}

// FindManyScoped preloads all possible stable identities for one import batch.
// Results are keyed by identityKey, scoped SKU and scoped normalized name.
func (r *MarketplaceAliasRepo) FindManyScoped(source string, accountKeys, externalItemIDs, externalVariantIDs, sourceSKUs, normalizedNames []string) (map[string]*models.MarketplaceItemAlias, error) {
	result := make(map[string]*models.MarketplaceItemAlias, len(sourceSKUs)+len(normalizedNames))
	if len(accountKeys) == 0 || (len(externalItemIDs) == 0 && len(externalVariantIDs) == 0 && len(sourceSKUs) == 0 && len(normalizedNames) == 0) {
		return result, nil
	}
	rows, err := r.db.Query(`
		SELECT `+aliasSelectColumns+`
		FROM marketplace_item_aliases a
		JOIN sml_catalog c ON c.item_code = a.item_code AND c.is_active = TRUE
		WHERE a.is_active = TRUE AND a.scope_confirmed = TRUE AND a.source = $1
		  AND a.account_key = ANY($2)
		  AND ((a.external_item_id <> '' AND a.external_item_id = ANY($3) AND a.external_variant_id = ANY($4))
		    OR (a.source = 'tiktok' AND a.external_item_id = '' AND a.external_variant_id <> '' AND a.external_variant_id = ANY($4))
		    OR (a.source_sku <> '' AND a.source_sku = ANY($5))
		    OR (a.source_sku = '' AND a.external_item_id = '' AND a.normalized_key = ANY($6)))
	`, source, pq.Array(accountKeys), pq.Array(externalItemIDs), pq.Array(externalVariantIDs), pq.Array(sourceSKUs), pq.Array(normalizedNames))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		alias, err := scanAlias(rows)
		if err != nil {
			return nil, err
		}
		if alias.ExternalItemID != "" {
			result[aliasIdentityKey(alias.AccountKey, alias.ExternalItemID, alias.ExternalVariantID)] = alias
		}
		if alias.Source == "tiktok" && alias.ExternalItemID == "" && alias.ExternalVariantID != "" {
			result[aliasVariantKey(alias.AccountKey, alias.ExternalVariantID)] = alias
		}
		if alias.SourceSKU != "" {
			result[aliasSKUKey(alias.AccountKey, alias.SourceSKU)] = alias
		}
		if alias.SourceSKU == "" && alias.ExternalItemID == "" {
			result[aliasNameKey(alias.AccountKey, alias.NormalizedKey)] = alias
		}
	}
	return result, rows.Err()
}

type aliasScanner interface {
	Scan(dest ...interface{}) error
}

func scanAlias(row aliasScanner) (*models.MarketplaceItemAlias, error) {
	var a models.MarketplaceItemAlias
	err := row.Scan(
		&a.ID, &a.Source, &a.AccountKey, &a.ExternalItemID, &a.ExternalVariantID,
		&a.SourceSKU, &a.RawName, &a.NormalizedKey, &a.ItemCode, &a.UnitCode,
		&a.Confidence, &a.ConfirmedBy, &a.UsageCount, &a.LastUsedAt, &a.CreatedAt, &a.UpdatedAt,
		&a.IsActive, &a.MatchMethod, &a.ScopeConfirmed,
		&a.ExternalParentID, &a.ParentKey, &a.ParentKeyKind, &a.SourceProductName, &a.SourceVariantName,
		&a.MappingRevision, &a.MetadataUpdatedAt, &a.QuantityMultiplier,
		&a.UnitStandValue, &a.UnitDivideValue, &a.UnitCatalogGeneration,
		&a.ConversionStatus, &a.SalesEnabled, &a.StockPolicy,
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
	method := "manual_name"
	if normalizeAliasSKU(sourceSKU) != "" {
		method = "manual_sku"
	}
	return r.UpsertScoped(models.MarketplaceAliasIdentity{
		Source: source, AccountKey: "default", SourceSKU: sourceSKU, RawName: rawName,
	}, itemCode, unitCode, method, true, confirmedBy)
}

func (r *MarketplaceAliasRepo) UpsertScoped(identity models.MarketplaceAliasIdentity, itemCode, unitCode, matchMethod string, scopeConfirmed bool, confirmedBy string) (*models.MarketplaceItemAlias, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	alias, err := upsertAliasTx(tx, identity, itemCode, unitCode, matchMethod, scopeConfirmed, confirmedBy)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return alias, nil
}

func upsertAliasTx(tx *sql.Tx, identity models.MarketplaceAliasIdentity, itemCode, unitCode, matchMethod string, scopeConfirmed bool, confirmedBy string) (*models.MarketplaceItemAlias, error) {
	identity = normalizeAliasIdentity(identity)
	if identity.ExternalItemID == "" && identity.ExternalVariantID == "" && identity.SourceSKU == "" && identity.NormalizedKey == "" {
		return nil, fmt.Errorf("stable identity, source_sku or normalized_key required")
	}
	if matchMethod == "" {
		matchMethod = "manual_name"
	}
	// Historical AOY TikTok aliases stored TikTok SKU ID in source_sku because
	// the old import did not distinguish Seller SKU from variant identity.
	// Promote that exact legacy row in place the first time the variant is
	// confirmed, preserving usage/audit history and avoiding a duplicate master.
	if identity.Source == "tiktok" && identity.ExternalItemID == "" && identity.ExternalVariantID != "" && identity.SourceSKU == "" {
		_, err := tx.Exec(`UPDATE marketplace_item_aliases legacy
			SET external_variant_id=$3, source_sku='', match_method='manual_identity', updated_at=NOW()
			WHERE legacy.id=(SELECT id FROM marketplace_item_aliases
				WHERE source=$1 AND account_key=$2 AND external_item_id='' AND external_variant_id=''
				  AND source_sku=$3 AND is_active=true ORDER BY updated_at DESC LIMIT 1)
			  AND NOT EXISTS (SELECT 1 FROM marketplace_item_aliases current
				WHERE current.source=$1 AND current.account_key=$2 AND current.external_item_id=''
				  AND current.external_variant_id=$3 AND current.is_active=true)`,
			identity.Source, identity.AccountKey, identity.ExternalVariantID)
		if err != nil {
			return nil, err
		}
	}
	var confirmedByArg interface{}
	if confirmedBy != "" {
		confirmedByArg = confirmedBy
	}
	_, err := tx.Exec(`
		INSERT INTO marketplace_item_aliases (
			source, account_key, external_item_id, external_variant_id,
			source_sku, raw_name, normalized_key, item_code, unit_code,
			confidence, confirmed_by, usage_count, last_used_at, match_method, scope_confirmed, is_active
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,1.0,$10,0,NOW(),$11,$12,TRUE)
		ON CONFLICT DO NOTHING`,
		identity.Source, identity.AccountKey, identity.ExternalItemID, identity.ExternalVariantID,
		identity.SourceSKU, identity.RawName, identity.NormalizedKey, itemCode, unitCode,
		confirmedByArg, matchMethod, scopeConfirmed,
	)
	if err != nil {
		return nil, err
	}
	where, args := aliasIdentityWhere(identity)
	args = append(args, identity.SourceSKU, identity.RawName, identity.NormalizedKey, itemCode, unitCode, confirmedByArg, matchMethod, scopeConfirmed)
	base := len(args) - 8
	row := tx.QueryRow(`UPDATE marketplace_item_aliases a
		SET source_sku=$`+fmt.Sprint(base+1)+`, raw_name=$`+fmt.Sprint(base+2)+`, normalized_key=$`+fmt.Sprint(base+3)+`,
		    item_code=$`+fmt.Sprint(base+4)+`, unit_code=$`+fmt.Sprint(base+5)+`, confidence=1.0,
		    confirmed_by=COALESCE($`+fmt.Sprint(base+6)+`,a.confirmed_by), match_method=$`+fmt.Sprint(base+7)+`,
		    scope_confirmed=$`+fmt.Sprint(base+8)+`, is_active=TRUE, updated_at=NOW()
		WHERE `+where+`
		RETURNING `+strings.ReplaceAll(aliasSelectColumns, "a.", ""), args...)
	alias, err := scanAlias(row)
	if err == nil && alias == nil {
		return nil, ErrMarketplaceAliasConflict
	}
	return alias, err
}

func aliasIdentityWhere(identity models.MarketplaceAliasIdentity) (string, []interface{}) {
	if identity.ExternalItemID != "" {
		return "a.source=$1 AND a.account_key=$2 AND a.external_item_id=$3 AND a.external_variant_id=$4",
			[]interface{}{identity.Source, identity.AccountKey, identity.ExternalItemID, identity.ExternalVariantID}
	}
	if identity.Source == "tiktok" && identity.ExternalVariantID != "" {
		return "a.source=$1 AND a.account_key=$2 AND a.external_item_id='' AND a.external_variant_id=$3",
			[]interface{}{identity.Source, identity.AccountKey, identity.ExternalVariantID}
	}
	if identity.SourceSKU != "" {
		return "a.source=$1 AND a.account_key=$2 AND a.source_sku=$3",
			[]interface{}{identity.Source, identity.AccountKey, identity.SourceSKU}
	}
	return "a.source=$1 AND a.account_key=$2 AND a.source_sku='' AND a.external_item_id='' AND a.normalized_key=$3",
		[]interface{}{identity.Source, identity.AccountKey, identity.NormalizedKey}
}

func (r *MarketplaceAliasRepo) List(source, query string, usableOnly bool, page, perPage int) ([]models.MarketplaceItemAlias, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	conditions := []string{"a.is_active = TRUE"}
	args := []interface{}{}
	if usableOnly {
		conditions = append(conditions, "a.scope_confirmed = TRUE")
	}
	if source != "" {
		args = append(args, source)
		conditions = append(conditions, fmt.Sprintf("a.source = $%d", len(args)))
	}
	if strings.TrimSpace(query) != "" {
		args = append(args, "%"+strings.TrimSpace(query)+"%")
		conditions = append(conditions, fmt.Sprintf("(a.source_sku ILIKE $%d OR a.raw_name ILIKE $%d OR a.item_code ILIKE $%d OR c.item_name ILIKE $%d OR a.external_item_id ILIKE $%d OR a.external_variant_id ILIKE $%d)", len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM marketplace_item_aliases a LEFT JOIN sml_catalog c ON c.item_code = a.item_code WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.Query(fmt.Sprintf(`
		SELECT a.id, a.source, a.account_key, a.external_item_id, a.external_variant_id,
		       a.source_sku, a.raw_name, a.normalized_key,
		       a.item_code, a.unit_code, a.confidence, a.confirmed_by,
		       a.usage_count, a.last_used_at, a.created_at, a.updated_at,
		       a.is_active, a.match_method, a.scope_confirmed,
		       a.external_parent_id, a.parent_key, a.parent_key_kind, a.source_product_name, a.source_variant_name,
		       a.mapping_revision, a.metadata_updated_at, a.quantity_multiplier,
		       a.unit_stand_value::text, a.unit_divide_value::text, a.unit_catalog_generation::text,
		       a.conversion_status, a.sales_enabled, a.stock_policy,
		       COALESCE(NULLIF(sc.label,''),NULLIF(sc.shop_name,''),''),
		       COALESCE(c.item_name, ''), COALESCE(u.email, ''),
		       COALESCE(c.is_active, FALSE),
		       (SELECT COUNT(*)
		          FROM bill_items bi JOIN bills b ON b.id = bi.bill_id
		         WHERE b.source = a.source
		           AND b.source_account_key = a.account_key
		           AND b.bill_type = 'sale'
		           AND b.status IN ('pending', 'needs_review')
		           AND b.archived_at IS NULL
		           AND NOT EXISTS (
		             SELECT 1 FROM sml_catalog exact_product
		              WHERE exact_product.is_active = TRUE
		                AND exact_product.item_code = btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), ''))
		           )
		           AND ((a.external_item_id <> '' AND bi.source_item_id = a.external_item_id AND bi.source_variant_id = a.external_variant_id)
		             OR (a.source = 'tiktok' AND a.external_item_id = '' AND a.external_variant_id <> ''
		                 AND COALESCE(bi.source_item_id, '') = '' AND bi.source_variant_id = a.external_variant_id)
		             OR (a.external_item_id = '' AND a.source_sku <> '' AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = a.source_sku)
			             OR (a.external_item_id = '' AND a.source_sku = ''
			                 AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = ''
			                 AND btrim(regexp_replace(replace(bi.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = a.normalized_key))),
		       (SELECT COUNT(*) FROM shopee_stock_mappings sm WHERE sm.marketplace_alias_id = a.id)
		FROM marketplace_item_aliases a
		LEFT JOIN sml_catalog c ON c.item_code = a.item_code
		LEFT JOIN users u ON u.id = a.confirmed_by
		LEFT JOIN shopee_api_connections sc ON a.source='shopee' AND a.account_key='shop:'||sc.shop_id::text
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
			&alias.ID, &alias.Source, &alias.AccountKey, &alias.ExternalItemID, &alias.ExternalVariantID,
			&alias.SourceSKU, &alias.RawName,
			&alias.NormalizedKey, &alias.ItemCode, &alias.UnitCode,
			&alias.Confidence, &alias.ConfirmedBy, &alias.UsageCount,
			&alias.LastUsedAt, &alias.CreatedAt, &alias.UpdatedAt,
			&alias.IsActive, &alias.MatchMethod, &alias.ScopeConfirmed,
			&alias.ExternalParentID, &alias.ParentKey, &alias.ParentKeyKind, &alias.SourceProductName, &alias.SourceVariantName,
			&alias.MappingRevision, &alias.MetadataUpdatedAt, &alias.QuantityMultiplier,
			&alias.UnitStandValue, &alias.UnitDivideValue, &alias.UnitCatalogGeneration,
			&alias.ConversionStatus, &alias.SalesEnabled, &alias.StockPolicy,
			&alias.AccountName,
			&alias.ItemName, &alias.ConfirmedName, &alias.ProductActive,
			&alias.OpenItemCount, &alias.StockMappingCount,
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
		UPDATE marketplace_item_aliases a
		SET item_code = $1, unit_code = $2, confirmed_by = COALESCE($3, confirmed_by),
		    confidence = 1.0, updated_at = NOW()
		WHERE id = $4 AND is_active = TRUE AND updated_at = $5
		RETURNING `+strings.ReplaceAll(aliasSelectColumns, "a.", ""), itemCode, unitCode, confirmedByArg, id, version)
	alias, err := scanAlias(row)
	if err == sql.ErrNoRows || (err == nil && alias == nil) {
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

// IncrementUsageCounts records one import batch with a single round trip.
func (r *MarketplaceAliasRepo) IncrementUsageCounts(counts map[string]int) error {
	if len(counts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(counts))
	values := make([]int64, 0, len(counts))
	for id, count := range counts {
		if id == "" || count <= 0 {
			continue
		}
		ids = append(ids, id)
		values = append(values, int64(count))
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(`UPDATE marketplace_item_aliases a
		SET usage_count=a.usage_count+batch.count,last_used_at=NOW()
		FROM (SELECT * FROM unnest($1::uuid[],$2::bigint[]) AS u(id,count)) batch
		WHERE a.id=batch.id`, pq.Array(ids), pq.Array(values))
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
		"b.archived_at IS NULL",
		"(bi.mapped IS DISTINCT FROM TRUE OR COALESCE(bi.item_code, '') = '')",
		`NOT EXISTS (
			SELECT 1 FROM sml_catalog exact_product
			 WHERE exact_product.is_active = TRUE
			   AND exact_product.item_code = btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), ''))
		)`,
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
		fmt.Sprintf(`SELECT b.id, b.source, b.source_account_key,
		        COALESCE(NULLIF(sc.label,''),NULLIF(sc.shop_name,''),''), b.bill_type, bi.id, bi.raw_name,
		        COALESCE(bi.source_sku, ''), COALESCE(bi.source_item_id, ''), COALESCE(bi.source_variant_id, '')
		   FROM bill_items bi
		   JOIN bills b ON b.id = bi.bill_id
		   LEFT JOIN shopee_api_connections sc ON b.source='shopee' AND b.source_account_key='shop:'||sc.shop_id::text
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
		var billID, source, accountKey, accountName, bt, itemID, rawName, sourceSKU, externalItemID, externalVariantID string
		if err := rows.Scan(&billID, &source, &accountKey, &accountName, &bt, &itemID, &rawName, &sourceSKU, &externalItemID, &externalVariantID); err != nil {
			return models.MarketplaceAliasReviewResult{}, err
		}
		normalizedKey := marketplace.NormalizeKey(rawName, sourceSKU)
		groupKey := source + "|" + accountKey + "|name|" + normalizedKey
		if externalItemID != "" {
			groupKey = source + "|" + accountKey + "|identity|" + externalItemID + "|" + externalVariantID
		} else if source == "tiktok" && externalVariantID != "" {
			groupKey = source + "|" + accountKey + "|variant|" + externalVariantID
		} else if sourceSKU != "" {
			groupKey = source + "|" + accountKey + "|sku|" + sourceSKU
		}
		g := groups[groupKey]
		if g == nil {
			g = &groupAgg{
				MarketplaceAliasReviewGroup: models.MarketplaceAliasReviewGroup{
					GroupKey:          groupKey,
					Source:            source,
					AccountKey:        accountKey,
					AccountName:       accountName,
					ExternalItemID:    externalItemID,
					ExternalVariantID: externalVariantID,
					BillType:          bt,
					SourceSKU:         sourceSKU,
					RawName:           rawName,
					NormalizedKey:     normalizedKey,
					ItemCount:         0,
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

// PreviewImpact reports the operational effect of a master change without
// mutating documents. It is intentionally bounded to open bills and linked
// stock mappings; sent/archived documents never participate.
func (r *MarketplaceAliasRepo) PreviewImpact(identity models.MarketplaceAliasIdentity, aliasID, nextItemCode string) (models.MarketplaceAliasImpact, error) {
	identity = normalizeAliasIdentity(identity)
	var impact models.MarketplaceAliasImpact
	where, args := billItemIdentityWhere(identity, 1)
	err := r.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT b.id)
		FROM bill_items bi JOIN bills b ON b.id=bi.bill_id
		WHERE b.status IN ('pending','needs_review') AND b.archived_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM sml_catalog c WHERE c.is_active=true
		    AND c.item_code=btrim(replace(COALESCE(bi.source_sku,''),chr(65279),'')))
		  AND (`+where+`)`, args...).Scan(&impact.OpenItems, &impact.OpenBills)
	if err != nil {
		return impact, err
	}
	if aliasID != "" {
		var currentItemCode string
		if err := r.db.QueryRow(`SELECT item_code FROM marketplace_item_aliases WHERE id=$1 AND is_active=true`, aliasID).Scan(&currentItemCode); err != nil {
			return impact, err
		}
		if err := r.db.QueryRow(`SELECT COUNT(*) FROM shopee_stock_mappings WHERE marketplace_alias_id=$1`, aliasID).Scan(&impact.StockMappings); err != nil {
			return impact, err
		}
		impact.DryRunRequired = impact.StockMappings > 0 && currentItemCode != nextItemCode
	}
	if identity.Source == "shopee" && strings.HasPrefix(identity.AccountKey, "shop:") && nextItemCode != "" {
		shopID := strings.TrimPrefix(identity.AccountKey, "shop:")
		if err := r.db.QueryRow(`SELECT COUNT(*) FROM shopee_stock_mappings m
			WHERE m.shop_id=$1::bigint AND m.excluded=false AND m.sml_item_code=$2
			  AND NOT (m.item_id::text=$3 AND m.model_id::text=$4)`, shopID, nextItemCode, identity.ExternalItemID, identity.ExternalVariantID).Scan(&impact.StockConflicts); err != nil {
			return impact, err
		}
	}
	return impact, nil
}

// SaveAndApply persists the master, applies it only to open documents, gates
// linked stock mappings and writes the audit record in one database transaction.
func (r *MarketplaceAliasRepo) SaveAndApply(mutation MarketplaceAliasMutation) (*MarketplaceAliasSaveResult, error) {
	mutation.Identity = normalizeAliasIdentity(mutation.Identity)
	if mutation.BillType == "" {
		mutation.BillType = "sale"
	}
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var alias *models.MarketplaceItemAlias
	if mutation.ID == "" {
		alias, err = upsertAliasTx(tx, mutation.Identity, mutation.ItemCode, mutation.UnitCode, mutation.MatchMethod, mutation.ScopeConfirmed, mutation.ConfirmedBy)
	} else {
		if mutation.Version == nil {
			return nil, ErrMarketplaceAliasConflict
		}
		var confirmedByArg interface{}
		if mutation.ConfirmedBy != "" {
			confirmedByArg = mutation.ConfirmedBy
		}
		var lockedID string
		if err = tx.QueryRow(`SELECT id::text FROM marketplace_item_aliases WHERE id=$1 AND is_active=true FOR UPDATE`, mutation.ID).Scan(&lockedID); err != nil {
			return nil, ErrMarketplaceAliasConflict
		}
		row := tx.QueryRow(`UPDATE marketplace_item_aliases a
			SET source=$1,account_key=$2,external_item_id=$3,external_variant_id=$4,
			    source_sku=$5,raw_name=$6,normalized_key=$7,item_code=$8,unit_code=$9,
			    confirmed_by=COALESCE($10,confirmed_by),confidence=1.0,match_method=$11,
			    scope_confirmed=$12,updated_at=NOW()
			WHERE id=$13 AND is_active=true AND updated_at=$14
			RETURNING `+strings.ReplaceAll(aliasSelectColumns, "a.", ""),
			mutation.Identity.Source, mutation.Identity.AccountKey, mutation.Identity.ExternalItemID, mutation.Identity.ExternalVariantID,
			mutation.Identity.SourceSKU, mutation.Identity.RawName, mutation.Identity.NormalizedKey,
			mutation.ItemCode, mutation.UnitCode, confirmedByArg, mutation.MatchMethod, mutation.ScopeConfirmed, mutation.ID, *mutation.Version)
		alias, err = scanAlias(row)
		if err == nil && alias == nil {
			err = ErrMarketplaceAliasConflict
		}
	}
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrMarketplaceAliasConflict
		}
		if errorsIsNoRows(err) {
			return nil, ErrMarketplaceAliasConflict
		}
		return nil, err
	}

	impact, err := previewImpactTx(tx, identityFromAlias(alias), alias.ID, mutation.ItemCode)
	if err != nil {
		return nil, err
	}
	applied, ready, err := applyAliasToOpenItemsTx(tx, alias, mutation.BillType)
	if err != nil {
		return nil, err
	}
	stockChanged, err := gateLinkedStockTx(tx, alias.ID, alias.ItemCode)
	if err != nil {
		return nil, err
	}
	impact.DryRunRequired = stockChanged > 0

	if err := insertAliasAuditTx(tx, mutation.ConfirmedBy, alias, applied, ready, impact, mutation.ID == ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MarketplaceAliasSaveResult{Alias: alias, AppliedItems: applied, ReadyBills: ready, Impact: impact}, nil
}

func (r *MarketplaceAliasRepo) DeactivateWithImpact(id string, version time.Time, userID string) (models.MarketplaceAliasImpact, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return models.MarketplaceAliasImpact{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRow(`SELECT `+aliasSelectColumns+` FROM marketplace_item_aliases a WHERE a.id=$1 AND a.is_active=true FOR UPDATE`, id)
	alias, err := scanAlias(row)
	if err != nil || alias == nil || !alias.UpdatedAt.Equal(version) {
		return models.MarketplaceAliasImpact{}, ErrMarketplaceAliasConflict
	}
	impact, err := previewImpactTx(tx, identityFromAlias(alias), alias.ID, alias.ItemCode)
	if err != nil {
		return impact, err
	}
	res, err := tx.Exec(`UPDATE marketplace_item_aliases SET is_active=false,updated_at=NOW() WHERE id=$1 AND updated_at=$2`, id, version)
	if err != nil {
		return impact, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return impact, ErrMarketplaceAliasConflict
	}
	stockChanged, err := pauseLinkedStockTx(tx, id, "master_inactive")
	if err != nil {
		return impact, err
	}
	impact.StockMappings = stockChanged
	impact.DryRunRequired = stockChanged > 0
	if err := insertAliasAuditTx(tx, userID, alias, 0, 0, impact, false); err != nil {
		return impact, err
	}
	if err := tx.Commit(); err != nil {
		return impact, err
	}
	return impact, nil
}

func applyAliasToOpenItemsTx(tx *sql.Tx, alias *models.MarketplaceItemAlias, billType string) (int, int, error) {
	identity := identityFromAlias(alias)
	where, args := billItemIdentityWhere(identity, 5)
	baseArgs := []interface{}{alias.ItemCode, alias.UnitCode, alias.ID, billType}
	baseArgs = append(baseArgs, args...)
	res, err := tx.Exec(`UPDATE bill_items bi
		SET item_code=$1,unit_code=$2,mapped=true,marketplace_alias_id=$3
		FROM bills b
		WHERE bi.bill_id=b.id AND b.bill_type=$4
		  AND b.status IN ('pending','needs_review') AND b.archived_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM sml_catalog c WHERE c.is_active=true
		    AND c.item_code=btrim(replace(COALESCE(bi.source_sku,''),chr(65279),'')))
		  AND (`+where+`)
		  AND (COALESCE(bi.item_code,'') IS DISTINCT FROM $1 OR COALESCE(bi.unit_code,'') IS DISTINCT FROM $2
		       OR bi.mapped IS DISTINCT FROM true OR bi.marketplace_alias_id IS DISTINCT FROM $3)`, baseArgs...)
	if err != nil {
		return 0, 0, err
	}
	applied, _ := res.RowsAffected()
	readyRes, err := tx.Exec(`UPDATE bills b SET status='pending',error_msg=NULL
		WHERE b.source=$1 AND b.source_account_key=$2 AND b.bill_type=$3 AND b.status='needs_review'
		  AND b.archived_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM bill_items bi WHERE bi.bill_id=b.id
		    AND (COALESCE(bi.item_code,'')='' OR bi.mapped IS DISTINCT FROM true))`, alias.Source, alias.AccountKey, billType)
	if err != nil {
		return 0, 0, err
	}
	ready, _ := readyRes.RowsAffected()
	return int(applied), int(ready), nil
}

func billItemIdentityWhere(identity models.MarketplaceAliasIdentity, start int) (string, []interface{}) {
	args := []interface{}{identity.Source, identity.AccountKey}
	sourceParam, accountParam := start, start+1
	if identity.ExternalItemID != "" {
		args = append(args, identity.ExternalItemID, identity.ExternalVariantID)
		return fmt.Sprintf("b.source=$%d AND b.source_account_key=$%d AND bi.source_item_id=$%d AND bi.source_variant_id=$%d", sourceParam, accountParam, start+2, start+3), args
	}
	if identity.Source == "tiktok" && identity.ExternalVariantID != "" {
		args = append(args, identity.ExternalVariantID)
		return fmt.Sprintf("b.source=$%d AND b.source_account_key=$%d AND bi.source_item_id='' AND bi.source_variant_id=$%d", sourceParam, accountParam, start+2), args
	}
	if identity.SourceSKU != "" {
		args = append(args, identity.SourceSKU)
		return fmt.Sprintf("b.source=$%d AND b.source_account_key=$%d AND btrim(replace(COALESCE(bi.source_sku,''),chr(65279),''))=$%d", sourceParam, accountParam, start+2), args
	}
	args = append(args, identity.NormalizedKey)
	return fmt.Sprintf("b.source=$%d AND b.source_account_key=$%d AND COALESCE(bi.source_sku,'')='' AND btrim(regexp_replace(replace(bi.raw_name,chr(65279),''),'\\s+',' ','g'))=$%d", sourceParam, accountParam, start+2), args
}

func previewImpactTx(tx *sql.Tx, identity models.MarketplaceAliasIdentity, aliasID, nextItemCode string) (models.MarketplaceAliasImpact, error) {
	var impact models.MarketplaceAliasImpact
	where, args := billItemIdentityWhere(identity, 1)
	if err := tx.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT b.id) FROM bill_items bi JOIN bills b ON b.id=bi.bill_id
		WHERE b.status IN ('pending','needs_review') AND b.archived_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM sml_catalog c WHERE c.is_active=true
		    AND c.item_code=btrim(replace(COALESCE(bi.source_sku,''),chr(65279),'')))
		  AND (`+where+`)`, args...).Scan(&impact.OpenItems, &impact.OpenBills); err != nil {
		return impact, err
	}
	if aliasID != "" {
		var currentItemCode string
		if err := tx.QueryRow(`SELECT item_code FROM marketplace_item_aliases WHERE id=$1 AND is_active=true`, aliasID).Scan(&currentItemCode); err != nil {
			return impact, err
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM shopee_stock_mappings WHERE marketplace_alias_id=$1`, aliasID).Scan(&impact.StockMappings); err != nil {
			return impact, err
		}
		impact.DryRunRequired = impact.StockMappings > 0 && currentItemCode != nextItemCode
	}
	if identity.Source == "shopee" && strings.HasPrefix(identity.AccountKey, "shop:") && nextItemCode != "" {
		if err := tx.QueryRow(`SELECT COUNT(*) FROM shopee_stock_mappings m WHERE m.shop_id=$1::bigint
			AND m.excluded=false AND m.sml_item_code=$2 AND NOT (m.item_id::text=$3 AND m.model_id::text=$4)`,
			strings.TrimPrefix(identity.AccountKey, "shop:"), nextItemCode, identity.ExternalItemID, identity.ExternalVariantID).Scan(&impact.StockConflicts); err != nil {
			return impact, err
		}
	}
	return impact, nil
}

func gateLinkedStockTx(tx *sql.Tx, aliasID, nextItemCode string) (int, error) {
	res, err := tx.Exec(`UPDATE shopee_stock_mappings
		SET sml_item_code=$2,
		    sml_unit_code=CASE WHEN sml_item_code=$2 THEN sml_unit_code ELSE '' END,
		    unit_factor=CASE WHEN sml_item_code=$2 THEN unit_factor ELSE 0 END,
		    manual_unit_factor=CASE WHEN sml_item_code=$2 THEN manual_unit_factor ELSE NULL END,
		    warning_codes=CASE WHEN sml_item_code=$2 THEN warning_codes ELSE '["master_target_changed"]'::jsonb END,
		    updated_at=NOW()
		WHERE marketplace_alias_id=$1
		  AND sml_item_code IS DISTINCT FROM $2`, aliasID, nextItemCode)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, err = tx.Exec(`UPDATE shopee_stock_settings s SET enabled=false,dry_run_required=true,updated_at=NOW()
			WHERE EXISTS (SELECT 1 FROM shopee_stock_mappings m WHERE m.shop_id=s.shop_id AND m.marketplace_alias_id=$1)`, aliasID)
	}
	return int(n), err
}

func pauseLinkedStockTx(tx *sql.Tx, aliasID, warning string) (int, error) {
	res, err := tx.Exec(`UPDATE shopee_stock_mappings SET warning_codes=jsonb_build_array($2::text),updated_at=NOW() WHERE marketplace_alias_id=$1`, aliasID, warning)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, err = tx.Exec(`UPDATE shopee_stock_settings s SET enabled=false,dry_run_required=true,updated_at=NOW()
			WHERE EXISTS (SELECT 1 FROM shopee_stock_mappings m WHERE m.shop_id=s.shop_id AND m.marketplace_alias_id=$1)`, aliasID)
	}
	return int(n), err
}

func insertAliasAuditTx(tx *sql.Tx, userID string, alias *models.MarketplaceItemAlias, applied, ready int, impact models.MarketplaceAliasImpact, created bool) error {
	detail, err := json.Marshal(map[string]interface{}{
		"alias_id": alias.ID, "account_key": alias.AccountKey, "match_method": alias.MatchMethod,
		"item_code": alias.ItemCode, "applied_items": applied, "ready_bills": ready,
		"stock_mappings": impact.StockMappings, "stock_conflicts": impact.StockConflicts,
	})
	if err != nil {
		return err
	}
	action := "marketplace_alias_updated"
	if created {
		action = "marketplace_alias_confirmed"
	}
	_, err = tx.Exec(`INSERT INTO audit_logs(action,user_id,source,level,detail)
		VALUES($1,NULLIF($2,'')::uuid,$3,'info',$4)`, action, userID, alias.Source, detail)
	return err
}

func identityFromAlias(alias *models.MarketplaceItemAlias) models.MarketplaceAliasIdentity {
	if alias == nil {
		return models.MarketplaceAliasIdentity{}
	}
	return models.MarketplaceAliasIdentity{Source: alias.Source, AccountKey: alias.AccountKey,
		ExternalItemID: alias.ExternalItemID, ExternalVariantID: alias.ExternalVariantID,
		SourceSKU: alias.SourceSKU, RawName: alias.RawName, NormalizedKey: alias.NormalizedKey}
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }

func normalizeAliasIdentity(identity models.MarketplaceAliasIdentity) models.MarketplaceAliasIdentity {
	identity.Source = strings.ToLower(strings.TrimSpace(identity.Source))
	identity.AccountKey = strings.TrimSpace(identity.AccountKey)
	if identity.AccountKey == "" {
		identity.AccountKey = "default"
	}
	identity.ExternalItemID = strings.TrimSpace(identity.ExternalItemID)
	identity.ExternalVariantID = strings.TrimSpace(identity.ExternalVariantID)
	identity.SourceSKU = normalizeAliasSKU(identity.SourceSKU)
	identity.RawName = marketplace.NormalizeKey(identity.RawName, "")
	identity.NormalizedKey = marketplace.NormalizeKey(identity.RawName, identity.SourceSKU)
	return identity
}

func aliasIdentityKey(accountKey, itemID, variantID string) string {
	return "identity\x00" + accountKey + "\x00" + itemID + "\x00" + variantID
}

func aliasVariantKey(accountKey, variantID string) string {
	return "variant\x00" + accountKey + "\x00" + variantID
}

func aliasSKUKey(accountKey, sku string) string   { return "sku\x00" + accountKey + "\x00" + sku }
func aliasNameKey(accountKey, name string) string { return "name\x00" + accountKey + "\x00" + name }

func normalizeAliasSKU(s string) string {
	s = strings.ReplaceAll(s, "\ufeff", "")
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "nan") || strings.EqualFold(s, "null") || s == "-" {
		return ""
	}
	return s
}
