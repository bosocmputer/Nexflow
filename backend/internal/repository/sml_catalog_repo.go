package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"

	"nexflow/internal/models"
	"nexflow/internal/services/itemcode"
)

// SMLCatalogRepo handles DB operations for sml_catalog
type SMLCatalogRepo struct {
	db *sql.DB
}

func NewSMLCatalogRepo(db *sql.DB) *SMLCatalogRepo {
	return &SMLCatalogRepo{db: db}
}

// DB returns the underlying database connection (for cross-table ops in handlers)
func (r *SMLCatalogRepo) DB() *sql.DB {
	return r.db
}

// UpdateItemMapping sets item_code + unit_code + mapped=true for a bill_item
func (r *SMLCatalogRepo) UpdateItemMapping(billItemID, billID, itemCode, unitCode string) error {
	_, err := r.db.Exec(`
		UPDATE bill_items
		SET item_code = $1, unit_code = $2, mapped = TRUE
		WHERE id = $3 AND bill_id = $4
	`, itemCode, unitCode, billItemID, billID)
	return err
}

// CountUnmappedItems returns number of bill_items for a bill with mapped=false
func (r *SMLCatalogRepo) CountUnmappedItems(billID string) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM bill_items WHERE bill_id = $1 AND mapped = FALSE`, billID,
	).Scan(&n)
	return n, err
}

// Upsert inserts or updates a catalog item (no embedding)
func (r *SMLCatalogRepo) Upsert(item models.CatalogItem) error {
	return r.UpsertAt(item, time.Now().UTC())
}

// UpsertAt stores one product and its set components atomically. last_seen_at
// uses the full-sync start timestamp so a failed page never deactivates rows.
func (r *SMLCatalogRepo) UpsertAt(item models.CatalogItem, runStartedAt time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	warnings, _ := json.Marshal(item.SetWarningCodes)
	_, err = tx.Exec(`
		INSERT INTO sml_catalog
		  (item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		   group_code, balance_qty, image_count, primary_image_roworder,
		   primary_image_guid, primary_image_bytes, image_synced_at, synced_at,
		   item_type, set_component_count, set_definition_hash,
		   set_document_valid, set_stock_valid, set_warning_codes, last_seen_at)
		VALUES (
		  $1,$2,$3,$4,$5,$6,$7,$8,
		  CASE WHEN $13::boolean THEN $9::int ELSE 0 END,
		  CASE WHEN $13::boolean THEN $10::int ELSE NULL::int END,
		  CASE WHEN $13::boolean THEN $11::text ELSE '' END,
		  CASE WHEN $13::boolean THEN $12::bigint ELSE NULL::bigint END,
		  CASE WHEN $13::boolean THEN NOW() ELSE NULL END,
		  NOW(),$14,$15,$16,$17,$18,$19,$20
		)
		ON CONFLICT (item_code) DO UPDATE SET
		  item_name   = EXCLUDED.item_name,
		  item_name2  = EXCLUDED.item_name2,
		  unit_code   = EXCLUDED.unit_code,
		  wh_code     = EXCLUDED.wh_code,
		  shelf_code  = EXCLUDED.shelf_code,
		  group_code  = EXCLUDED.group_code,
		  balance_qty = EXCLUDED.balance_qty,
		  item_type = EXCLUDED.item_type,
		  set_component_count = EXCLUDED.set_component_count,
		  set_definition_hash = EXCLUDED.set_definition_hash,
		  set_document_valid = EXCLUDED.set_document_valid,
		  set_stock_valid = EXCLUDED.set_stock_valid,
		  set_warning_codes = EXCLUDED.set_warning_codes,
		  is_active = TRUE,
		  missing_at = NULL,
		  last_seen_at = EXCLUDED.last_seen_at,
		  image_count = CASE
		    WHEN $13::boolean THEN EXCLUDED.image_count
		    ELSE sml_catalog.image_count
		  END,
		  primary_image_roworder = CASE
		    WHEN $13::boolean THEN EXCLUDED.primary_image_roworder
		    ELSE sml_catalog.primary_image_roworder
		  END,
		  primary_image_guid = CASE
		    WHEN $13::boolean THEN EXCLUDED.primary_image_guid
		    ELSE sml_catalog.primary_image_guid
		  END,
		  primary_image_bytes = CASE
		    WHEN $13::boolean THEN EXCLUDED.primary_image_bytes
		    ELSE sml_catalog.primary_image_bytes
		  END,
		  image_synced_at = CASE
		    WHEN $13::boolean AND (
		      sml_catalog.image_count IS DISTINCT FROM EXCLUDED.image_count OR
		      sml_catalog.primary_image_roworder IS DISTINCT FROM EXCLUDED.primary_image_roworder OR
		      sml_catalog.primary_image_guid IS DISTINCT FROM EXCLUDED.primary_image_guid OR
		      sml_catalog.primary_image_bytes IS DISTINCT FROM EXCLUDED.primary_image_bytes
		    ) THEN NOW()
		    ELSE sml_catalog.image_synced_at
		  END,
		  synced_at   = NOW(),
		  embedding_status = 'disabled',
		  embedded_at = NULL,
		  embedding_model = NULL,
		  embedding = NULL
	`,
		item.ItemCode, item.ItemName, item.ItemName2,
		item.UnitCode, item.WHCode, item.ShelfCode,
		item.GroupCode, item.BalanceQty,
		item.ImageCount, item.PrimaryImageRoworder, item.PrimaryImageGuid,
		item.PrimaryImageBytes, item.ImageMetadataSynced,
		item.ItemType, item.SetComponentCount, item.SetDefinitionHash,
		item.SetDocumentValid, item.SetStockValid, warnings, runStartedAt,
	)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sml_catalog_set_components WHERE parent_item_code=$1`, item.ItemCode); err != nil {
		return err
	}
	for _, component := range item.SetComponents {
		if _, err := tx.Exec(`INSERT INTO sml_catalog_set_components (
			parent_item_code,line_number,row_order,component_item_code,component_item_name,
			component_item_type,unit_code,qty,price,sum_amount,price_ratio,unit_factor,
			is_active,unit_valid,definition_hash,synced_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NOW())`,
			item.ItemCode, component.LineNumber, component.RowOrder, component.ItemCode, component.ItemName,
			component.ItemType, component.UnitCode, component.Qty, component.Price, component.SumAmount,
			component.PriceRatio, component.UnitFactor, component.Active, component.UnitValid, item.SetDefinitionHash,
		); err != nil {
			return err
		}
	}
	if item.ItemType == 3 && !item.SetDocumentValid {
		if _, err := tx.Exec(`UPDATE bills b
			SET status='needs_review', error_msg='สินค้าชุดใน SML ไม่พร้อมใช้งาน กรุณาแก้ Master และอัปเดตรายการสินค้า'
			FROM bill_items bi
			WHERE bi.bill_id=b.id AND bi.item_code=$1
			  AND b.archived_at IS NULL AND b.status IN ('pending','needs_review')`, item.ItemCode); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListActiveUnits returns only units belonging to the fully activated catalog
// generation. A partially staged generation is never visible to callers.
func (r *SMLCatalogRepo) ListActiveUnits(ctx context.Context, itemCode string) ([]models.CatalogUnit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.item_code, u.unit_code, u.unit_name,
		       u.stand_value::text, u.divide_value::text,
		       u.is_default, u.unit_order, u.generation_id::text
		FROM sml_catalog_units u
		JOIN sml_catalog_sync_runs run ON run.id = u.generation_id AND run.status = 'active'
		WHERE u.item_code = $1 AND u.is_active = true
		ORDER BY u.is_default DESC, u.unit_order, u.unit_code
	`, itemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	units := make([]models.CatalogUnit, 0)
	for rows.Next() {
		var unit models.CatalogUnit
		if err := rows.Scan(&unit.ItemCode, &unit.UnitCode, &unit.UnitName,
			&unit.StandValue, &unit.DivideValue, &unit.IsDefault, &unit.UnitOrder, &unit.GenerationID); err != nil {
			return nil, err
		}
		units = append(units, unit)
	}
	return units, rows.Err()
}

func (r *SMLCatalogRepo) FinalizeSuccessfulSync(runStartedAt time.Time) error {
	_, err := r.db.Exec(`UPDATE sml_catalog
		SET is_active=false, missing_at=COALESCE(missing_at,NOW()), synced_at=NOW()
		WHERE is_active=true AND (last_seen_at IS NULL OR last_seen_at < $1)`, runStartedAt)
	return err
}

// SetEmbedding saves a computed embedding for one item
func (r *SMLCatalogRepo) SetEmbedding(itemCode string, embedding []float64, model string) error {
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}
	now := time.Now()
	_, err = r.db.Exec(`
		UPDATE sml_catalog
		SET embedding = $1, embedding_status = 'done', embedded_at = $2, embedding_model = $3
		WHERE item_code = $4
	`, embJSON, now, model, itemCode)
	return err
}

// SetEmbeddingError marks an item as embedding error
func (r *SMLCatalogRepo) SetEmbeddingError(itemCode string) error {
	_, err := r.db.Exec(`
		UPDATE sml_catalog SET embedding_status = 'error' WHERE item_code = $1
	`, itemCode)
	return err
}

// GetEmbedding retrieves the stored embedding for one item
func (r *SMLCatalogRepo) GetEmbedding(itemCode string) ([]float64, error) {
	var embJSON []byte
	err := r.db.QueryRow(`SELECT embedding FROM sml_catalog WHERE item_code = $1`, itemCode).Scan(&embJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if embJSON == nil {
		return nil, nil
	}
	var emb []float64
	if err := json.Unmarshal(embJSON, &emb); err != nil {
		return nil, fmt.Errorf("unmarshal embedding: %w", err)
	}
	return emb, nil
}

// EmbeddedItem is used for in-memory catalog search index building
type EmbeddedItem struct {
	ItemCode             string
	ItemName             string
	ItemName2            string
	UnitCode             string
	WHCode               string
	ShelfCode            string
	Price                *float64
	ImageCount           int
	PrimaryImageRoworder *int
	PrimaryImageGuid     string
	PrimaryImageBytes    *int64
	Embedding            []float64
}

// LoadAllEmbeddings returns all items with embedding_status='done'
// Used to build the in-memory search index
func (r *SMLCatalogRepo) LoadAllEmbeddings() ([]EmbeddedItem, error) {
	rows, err := r.db.Query(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		       COALESCE(price, 0), image_count, primary_image_roworder,
		       primary_image_guid, primary_image_bytes, embedding
		FROM sml_catalog
		WHERE embedding_status = 'done' AND embedding IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []EmbeddedItem
	for rows.Next() {
		var it EmbeddedItem
		var embJSON []byte
		var primaryRoworder sql.NullInt64
		var primaryBytes sql.NullInt64
		if err := rows.Scan(
			&it.ItemCode, &it.ItemName, &it.ItemName2,
			&it.UnitCode, &it.WHCode, &it.ShelfCode,
			&it.Price, &it.ImageCount, &primaryRoworder,
			&it.PrimaryImageGuid, &primaryBytes, &embJSON,
		); err != nil {
			continue
		}
		applyEmbeddedImageScan(&it, primaryRoworder, primaryBytes)
		if embJSON != nil {
			_ = json.Unmarshal(embJSON, &it.Embedding)
		}
		if len(it.Embedding) > 0 {
			items = append(items, it)
		}
	}
	return items, rows.Err()
}

// List returns paginated catalog items (no embedding data).
// q filters by item_code or item_name (case-insensitive prefix/substring match).
func (r *SMLCatalogRepo) List(page, perPage int, statusFilter, q string) ([]models.CatalogItem, int, error) {
	offset := (page - 1) * perPage

	// Build WHERE clauses
	conditions := []string{"is_active = TRUE"}
	countArgs := []interface{}{}
	if statusFilter != "" {
		conditions = append(conditions, fmt.Sprintf("embedding_status = $%d", len(countArgs)+1))
		countArgs = append(countArgs, statusFilter)
	}
	if q != "" {
		like := "%" + q + "%"
		conditions = append(conditions, fmt.Sprintf("(item_code ILIKE $%d OR item_name ILIKE $%d OR item_name2 ILIKE $%d)", len(countArgs)+1, len(countArgs)+2, len(countArgs)+3))
		countArgs = append(countArgs, like, like, like)
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAnd(conditions)
	}

	var total int
	countQ := "SELECT COUNT(*) FROM sml_catalog " + where
	if err := r.db.QueryRow(countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// For the main query, append LIMIT/OFFSET args
	listArgs := append(countArgs, perPage, offset)
	n := len(listArgs)
	query := fmt.Sprintf(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		       price, group_code, balance_qty, embedding_status, embedded_at,
		       is_active,
		       image_count, primary_image_roworder, primary_image_guid,
		       primary_image_bytes, image_synced_at, synced_at, created_at,
		       item_type, set_component_count, set_definition_hash,
		       set_document_valid, set_stock_valid, set_warning_codes
		FROM sml_catalog
		%s
		ORDER BY item_code
		LIMIT $%d OFFSET $%d
	`, where, n-1, n)

	rows, err := r.db.Query(query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []models.CatalogItem
	for rows.Next() {
		var it models.CatalogItem
		var primaryRoworder sql.NullInt64
		var primaryBytes sql.NullInt64
		var imageSyncedAt sql.NullTime
		var setWarnings []byte
		if err := rows.Scan(
			&it.ItemCode, &it.ItemName, &it.ItemName2,
			&it.UnitCode, &it.WHCode, &it.ShelfCode,
			&it.Price, &it.GroupCode, &it.BalanceQty,
			&it.EmbeddingStatus, &it.EmbeddedAt, &it.IsActive,
			&it.ImageCount, &primaryRoworder, &it.PrimaryImageGuid,
			&primaryBytes, &imageSyncedAt,
			&it.SyncedAt, &it.CreatedAt,
			&it.ItemType, &it.SetComponentCount, &it.SetDefinitionHash,
			&it.SetDocumentValid, &it.SetStockValid, &setWarnings,
		); err != nil {
			continue
		}
		_ = json.Unmarshal(setWarnings, &it.SetWarningCodes)
		applyItemCodeMetadata(&it)
		applyCatalogImageScan(&it, primaryRoworder, primaryBytes, imageSyncedAt)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if err := r.attachSetComponents(items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func joinAnd(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " AND "
		}
		result += p
	}
	return result
}

// Stats returns count by embedding_status
func (r *SMLCatalogRepo) Stats() (total, done, pending, errCount int, err error) {
	err = r.db.QueryRow(`
		SELECT
		  COUNT(*),
		  COUNT(*) FILTER (WHERE embedding_status = 'done'),
		  COUNT(*) FILTER (WHERE embedding_status = 'pending'),
		  COUNT(*) FILTER (WHERE embedding_status = 'error')
		FROM sml_catalog
		WHERE is_active = TRUE
	`).Scan(&total, &done, &pending, &errCount)
	return
}

func (r *SMLCatalogRepo) CountHiddenItemCodes() (int, error) {
	rows, err := r.db.Query(`SELECT item_code FROM sml_catalog`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return 0, err
		}
		if itemcode.Inspect(code).HasHiddenChars {
			n++
		}
	}
	return n, rows.Err()
}

func (r *SMLCatalogRepo) ListHiddenItemCodes(limit int) ([]models.CatalogItem, int, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := r.db.Query(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		       price, group_code, balance_qty, embedding_status, embedded_at,
		       is_active,
		       image_count, primary_image_roworder, primary_image_guid,
		       primary_image_bytes, image_synced_at, synced_at, created_at
		FROM sml_catalog
		WHERE is_active = TRUE
		ORDER BY item_code
	`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]models.CatalogItem, 0, limit)
	total := 0
	for rows.Next() {
		var it models.CatalogItem
		var primaryRoworder sql.NullInt64
		var primaryBytes sql.NullInt64
		var imageSyncedAt sql.NullTime
		if err := rows.Scan(
			&it.ItemCode, &it.ItemName, &it.ItemName2,
			&it.UnitCode, &it.WHCode, &it.ShelfCode,
			&it.Price, &it.GroupCode, &it.BalanceQty,
			&it.EmbeddingStatus, &it.EmbeddedAt, &it.IsActive,
			&it.ImageCount, &primaryRoworder, &it.PrimaryImageGuid,
			&primaryBytes, &imageSyncedAt,
			&it.SyncedAt, &it.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		applyItemCodeMetadata(&it)
		if !it.HasHiddenChars {
			continue
		}
		total++
		if len(items) < limit {
			applyCatalogImageScan(&it, primaryRoworder, primaryBytes, imageSyncedAt)
			items = append(items, it)
		}
	}
	return items, total, rows.Err()
}

// Delete removes a single catalog row by item_code. SML 248 is not touched —
// callers are expected to have already deleted the master in SML (or to be
// pruning a zombie left over after an SML-side delete). Returns sql.ErrNoRows
// when the code wasn't in the catalog.
func (r *SMLCatalogRepo) Delete(itemCode string) error {
	res, err := r.db.Exec(`DELETE FROM sml_catalog WHERE item_code = $1`, itemCode)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetOne returns a single catalog item
func (r *SMLCatalogRepo) GetOne(itemCode string) (*models.CatalogItem, error) {
	var it models.CatalogItem
	var primaryRoworder sql.NullInt64
	var primaryBytes sql.NullInt64
	var imageSyncedAt sql.NullTime
	var setWarnings []byte
	err := r.db.QueryRow(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		       price, group_code, balance_qty, embedding_status, embedded_at,
		       is_active,
		       image_count, primary_image_roworder, primary_image_guid,
		       primary_image_bytes, image_synced_at, synced_at, created_at,
		       item_type,set_component_count,set_definition_hash,
		       set_document_valid,set_stock_valid,set_warning_codes
		FROM sml_catalog WHERE item_code = $1
	`, itemCode).Scan(
		&it.ItemCode, &it.ItemName, &it.ItemName2,
		&it.UnitCode, &it.WHCode, &it.ShelfCode,
		&it.Price, &it.GroupCode, &it.BalanceQty,
		&it.EmbeddingStatus, &it.EmbeddedAt, &it.IsActive,
		&it.ImageCount, &primaryRoworder, &it.PrimaryImageGuid,
		&primaryBytes, &imageSyncedAt,
		&it.SyncedAt, &it.CreatedAt,
		&it.ItemType, &it.SetComponentCount, &it.SetDefinitionHash,
		&it.SetDocumentValid, &it.SetStockValid, &setWarnings,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err == nil {
		_ = json.Unmarshal(setWarnings, &it.SetWarningCodes)
		applyItemCodeMetadata(&it)
		applyCatalogImageScan(&it, primaryRoworder, primaryBytes, imageSyncedAt)
		items := []models.CatalogItem{it}
		if attachErr := r.attachSetComponents(items); attachErr != nil {
			return nil, attachErr
		}
		it = items[0]
	}
	return &it, err
}

func (r *SMLCatalogRepo) attachSetComponents(items []models.CatalogItem) error {
	indexes := make(map[string]int)
	codes := make([]string, 0)
	for i := range items {
		if items[i].ItemType != 3 {
			continue
		}
		indexes[items[i].ItemCode] = i
		codes = append(codes, items[i].ItemCode)
	}
	if len(codes) == 0 {
		return nil
	}
	rows, err := r.db.Query(`SELECT parent_item_code,line_number,row_order,
		component_item_code,component_item_name,component_item_type,unit_code,
		qty::float8,price::float8,sum_amount::float8,price_ratio::float8,
		unit_factor::float8,is_active,unit_valid
		FROM sml_catalog_set_components
		WHERE parent_item_code = ANY($1)
		ORDER BY parent_item_code,line_number,row_order,component_item_code`, pq.Array(codes))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var parentCode string
		var component models.CatalogSetComponent
		if err := rows.Scan(&parentCode, &component.LineNumber, &component.RowOrder,
			&component.ItemCode, &component.ItemName, &component.ItemType, &component.UnitCode,
			&component.Qty, &component.Price, &component.SumAmount, &component.PriceRatio,
			&component.UnitFactor, &component.Active, &component.UnitValid); err != nil {
			return err
		}
		if index, ok := indexes[parentCode]; ok {
			items[index].SetComponents = append(items[index].SetComponents, component)
		}
	}
	return rows.Err()
}

// CountPending returns number of items pending embedding
func (r *SMLCatalogRepo) CountPending() (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM sml_catalog WHERE embedding_status = 'pending'`).Scan(&n)
	return n, err
}

// GetPendingBatch returns a batch of pending items for embedding
func (r *SMLCatalogRepo) GetPendingBatch(limit int) ([]models.CatalogItem, error) {
	rows, err := r.db.Query(`
		SELECT item_code, item_name, item_name2
		FROM sml_catalog
		WHERE embedding_status = 'pending'
		ORDER BY item_code
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.CatalogItem
	for rows.Next() {
		var it models.CatalogItem
		_ = rows.Scan(&it.ItemCode, &it.ItemName, &it.ItemName2)
		items = append(items, it)
	}
	return items, rows.Err()
}

// ListAllNames returns all item codes + names (for Levenshtein fallback)
func (r *SMLCatalogRepo) ListAllNames() ([]models.CatalogItem, error) {
	rows, err := r.db.Query(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		       COALESCE(price, 0), image_count, primary_image_roworder,
		       primary_image_guid, primary_image_bytes, image_synced_at
		FROM sml_catalog
		WHERE is_active = TRUE
		ORDER BY item_code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.CatalogItem
	for rows.Next() {
		var it models.CatalogItem
		var price float64
		var primaryRoworder sql.NullInt64
		var primaryBytes sql.NullInt64
		var imageSyncedAt sql.NullTime
		_ = rows.Scan(&it.ItemCode, &it.ItemName, &it.ItemName2,
			&it.UnitCode, &it.WHCode, &it.ShelfCode, &price,
			&it.ImageCount, &primaryRoworder, &it.PrimaryImageGuid,
			&primaryBytes, &imageSyncedAt)
		it.Price = &price
		applyItemCodeMetadata(&it)
		applyCatalogImageScan(&it, primaryRoworder, primaryBytes, imageSyncedAt)
		items = append(items, it)
	}
	return items, rows.Err()
}

// GetActive returns an exact case-sensitive item code only when it is active.
func (r *SMLCatalogRepo) GetActive(itemCode string) (*models.CatalogItem, error) {
	item, err := r.GetOne(itemCode)
	if err != nil || item == nil || !item.IsActive {
		return nil, err
	}
	return item, nil
}

// GetActiveMany loads exact, case-sensitive product codes with one query.
func (r *SMLCatalogRepo) GetActiveMany(itemCodes []string) (map[string]*models.CatalogItem, error) {
	result := make(map[string]*models.CatalogItem, len(itemCodes))
	if len(itemCodes) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code, price,
		       item_type,set_component_count,set_definition_hash,set_document_valid,set_stock_valid,set_warning_codes
		FROM sml_catalog
		WHERE is_active = TRUE AND item_code = ANY($1)
	`, pq.Array(itemCodes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item models.CatalogItem
		var setWarnings []byte
		if err := rows.Scan(
			&item.ItemCode, &item.ItemName, &item.ItemName2,
			&item.UnitCode, &item.WHCode, &item.ShelfCode, &item.Price,
			&item.ItemType, &item.SetComponentCount, &item.SetDefinitionHash,
			&item.SetDocumentValid, &item.SetStockValid, &setWarnings,
		); err != nil {
			return nil, err
		}
		item.IsActive = true
		_ = json.Unmarshal(setWarnings, &item.SetWarningCodes)
		result[item.ItemCode] = &item
	}
	return result, rows.Err()
}

// CountActive is used to block document creation when the local SML catalog
// has never been synchronized.
func (r *SMLCatalogRepo) CountActive() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM sml_catalog WHERE is_active = TRUE`).Scan(&count)
	return count, err
}

// SearchActive is deterministic and database-only. Item codes remain
// case-sensitive: ordering favours exact code, then prefix, then contains,
// before matching product names.
func (r *SMLCatalogRepo) SearchActive(query string, limit int) ([]models.CatalogMatch, error) {
	rows, err := r.db.Query(`
		SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,
		       COALESCE(price, 0), image_count, primary_image_roworder,
		       primary_image_guid, primary_image_bytes,
		       item_type,set_component_count,set_definition_hash,set_document_valid,set_warning_codes,
		       CASE
		         WHEN item_code = $1 THEN 'exact_code'
		         WHEN item_code LIKE $1 || '%' THEN 'code_prefix'
		         WHEN item_code LIKE '%' || $1 || '%' THEN 'code_contains'
		         ELSE 'product_name'
		       END AS match_type
		FROM sml_catalog
		WHERE is_active = TRUE
		  AND (item_code LIKE '%' || $1 || '%'
		       OR item_name ILIKE '%' || $1 || '%'
		       OR item_name2 ILIKE '%' || $1 || '%')
		ORDER BY CASE
		           WHEN item_code = $1 THEN 0
		           WHEN item_code LIKE $1 || '%' THEN 1
		           WHEN item_code LIKE '%' || $1 || '%' THEN 2
		           ELSE 3
		         END,
		         item_code
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]models.CatalogMatch, 0, limit)
	for rows.Next() {
		var match models.CatalogMatch
		var primaryRoworder sql.NullInt64
		var primaryBytes sql.NullInt64
		var setWarnings []byte
		if err := rows.Scan(
			&match.ItemCode, &match.ItemName, &match.ItemName2,
			&match.UnitCode, &match.WHCode, &match.ShelfCode,
			&match.Price, &match.ImageCount, &primaryRoworder,
			&match.PrimaryImageGuid, &primaryBytes,
			&match.ItemType, &match.SetComponentCount, &match.SetDefinitionHash,
			&match.SetDocumentValid, &setWarnings, &match.MatchType,
		); err != nil {
			return nil, err
		}
		if primaryRoworder.Valid {
			roworder := int(primaryRoworder.Int64)
			match.PrimaryImageRoworder = &roworder
		}
		if primaryBytes.Valid {
			bytes := primaryBytes.Int64
			match.PrimaryImageBytes = &bytes
		}
		match.Method = "database"
		_ = json.Unmarshal(setWarnings, &match.SetWarningCodes)
		switch match.MatchType {
		case "exact_code":
			match.Score = 1
		case "code_prefix":
			match.Score = .95
		case "code_contains":
			match.Score = .9
		default:
			match.Score = .8
		}
		results = append(results, match)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := r.attachSetComponentsToMatches(results); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *SMLCatalogRepo) attachSetComponentsToMatches(matches []models.CatalogMatch) error {
	indexes := make(map[string]int)
	codes := make([]string, 0)
	for i := range matches {
		if matches[i].ItemType != 3 {
			continue
		}
		indexes[matches[i].ItemCode] = i
		codes = append(codes, matches[i].ItemCode)
	}
	if len(codes) == 0 {
		return nil
	}
	rows, err := r.db.Query(`SELECT parent_item_code,line_number,row_order,
		component_item_code,component_item_name,component_item_type,unit_code,
		qty::float8,price::float8,sum_amount::float8,price_ratio::float8,
		unit_factor::float8,is_active,unit_valid
		FROM sml_catalog_set_components
		WHERE parent_item_code = ANY($1)
		ORDER BY parent_item_code,line_number,row_order,component_item_code`, pq.Array(codes))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var parentCode string
		var component models.CatalogSetComponent
		if err := rows.Scan(&parentCode, &component.LineNumber, &component.RowOrder,
			&component.ItemCode, &component.ItemName, &component.ItemType, &component.UnitCode,
			&component.Qty, &component.Price, &component.SumAmount, &component.PriceRatio,
			&component.UnitFactor, &component.Active, &component.UnitValid); err != nil {
			return err
		}
		if index, ok := indexes[parentCode]; ok {
			matches[index].SetComponents = append(matches[index].SetComponents, component)
		}
	}
	return rows.Err()
}

func applyItemCodeMetadata(it *models.CatalogItem) {
	meta := itemcode.Inspect(it.ItemCode)
	it.HasHiddenChars = meta.HasHiddenChars
	it.CleanItemCode = meta.CleanItemCode
	it.HiddenCharKinds = meta.Kinds
}

func applyCatalogImageScan(it *models.CatalogItem, primaryRoworder, primaryBytes sql.NullInt64, imageSyncedAt sql.NullTime) {
	if primaryRoworder.Valid {
		roworder := int(primaryRoworder.Int64)
		it.PrimaryImageRoworder = &roworder
	}
	if primaryBytes.Valid {
		bytes := primaryBytes.Int64
		it.PrimaryImageBytes = &bytes
	}
	if imageSyncedAt.Valid {
		syncedAt := imageSyncedAt.Time
		it.ImageSyncedAt = &syncedAt
	}
}

func applyEmbeddedImageScan(it *EmbeddedItem, primaryRoworder, primaryBytes sql.NullInt64) {
	if primaryRoworder.Valid {
		roworder := int(primaryRoworder.Int64)
		it.PrimaryImageRoworder = &roworder
	}
	if primaryBytes.Valid {
		bytes := primaryBytes.Int64
		it.PrimaryImageBytes = &bytes
	}
}
