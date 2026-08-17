package shopeestock

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/sml"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) EnsureSettings(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO shopee_stock_settings (shop_id)
		SELECT shop_id FROM shopee_api_connections
		ON CONFLICT (shop_id) DO NOTHING`)
	return err
}

func (s *Store) ListSettings(ctx context.Context, environment string) ([]Settings, error) {
	if err := s.EnsureSettings(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.shop_id, COALESCE(NULLIF(c.label, ''), NULLIF(c.shop_name, ''), 'Shop ' || c.shop_id::text),
		       c.id::text, c.credential_mode,
		       st.enabled, st.stock_pct::float8, st.interval_seconds, st.scope_mode, st.locations,
		       st.all_scope_warning_acknowledged, st.dry_run_required, st.paused_reason,
		       st.last_catalog_sync_at, st.last_full_catalog_sync_at, st.last_catalog_attempt_at,
		       st.last_preview_at, st.last_sync_at, st.last_success_at,
		       st.last_error, st.updated_at
		  FROM shopee_api_connections c
		  JOIN shopee_stock_settings st ON st.shop_id = c.shop_id
		 WHERE c.environment = $1 AND c.disabled_at IS NULL
		 ORDER BY c.label, c.shop_id`, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := []Settings{}
	for rows.Next() {
		var item Settings
		var locations []byte
		if err := rows.Scan(
			&item.ShopID, &item.ShopName, &item.ConnectionID, &item.CredentialMode,
			&item.Enabled, &item.StockPct, &item.IntervalSeconds, &item.ScopeMode, &locations,
			&item.AllScopeWarningAcknowledged, &item.DryRunRequired, &item.PausedReason,
			&item.LastCatalogSyncAt, &item.LastFullCatalogSyncAt, &item.LastCatalogAttemptAt,
			&item.LastPreviewAt, &item.LastSyncAt, &item.LastSuccessAt,
			&item.LastError, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(locations, &item.Locations); err != nil {
			return nil, fmt.Errorf("decode stock locations for shop %d: %w", item.ShopID, err)
		}
		settings = append(settings, item)
	}
	return settings, rows.Err()
}

func (s *Store) GetSettings(ctx context.Context, shopID int64) (*Settings, error) {
	if err := s.EnsureSettings(ctx); err != nil {
		return nil, err
	}
	var item Settings
	var locations []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT c.shop_id, COALESCE(NULLIF(c.label, ''), NULLIF(c.shop_name, ''), 'Shop ' || c.shop_id::text),
		       c.id::text, c.credential_mode,
		       st.enabled, st.stock_pct::float8, st.interval_seconds, st.scope_mode, st.locations,
		       st.all_scope_warning_acknowledged, st.dry_run_required, st.paused_reason,
		       st.last_catalog_sync_at, st.last_full_catalog_sync_at, st.last_catalog_attempt_at,
		       st.last_preview_at, st.last_sync_at, st.last_success_at,
		       st.last_error, st.updated_at
		  FROM shopee_api_connections c
		  JOIN shopee_stock_settings st ON st.shop_id = c.shop_id
		 WHERE c.shop_id = $1 AND c.disabled_at IS NULL`, shopID).Scan(
		&item.ShopID, &item.ShopName, &item.ConnectionID, &item.CredentialMode,
		&item.Enabled, &item.StockPct, &item.IntervalSeconds, &item.ScopeMode, &locations,
		&item.AllScopeWarningAcknowledged, &item.DryRunRequired, &item.PausedReason,
		&item.LastCatalogSyncAt, &item.LastFullCatalogSyncAt, &item.LastCatalogAttemptAt,
		&item.LastPreviewAt, &item.LastSyncAt, &item.LastSuccessAt,
		&item.LastError, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(locations, &item.Locations); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) UpdateSettings(ctx context.Context, shopID int64, request SettingsUpdate, userID string) (*Settings, error) {
	locations, err := json.Marshal(request.Locations)
	if err != nil {
		return nil, err
	}
	current, err := s.GetSettings(ctx, shopID)
	if err != nil {
		return nil, err
	}
	changedCalculation := current.StockPct != request.StockPct || current.ScopeMode != request.ScopeMode || string(mustJSON(current.Locations)) != string(locations)
	if request.Enabled && (current.DryRunRequired || changedCalculation || strings.TrimSpace(current.PausedReason) != "") {
		return nil, ErrDryRunRequired
	}
	var updatedBy any
	if strings.TrimSpace(userID) != "" {
		updatedBy = userID
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE shopee_stock_settings
		   SET enabled = $2,
		       stock_pct = $3,
		       interval_seconds = $4,
		       scope_mode = $5,
		       locations = $6,
		       all_scope_warning_acknowledged = false,
		       dry_run_required = CASE WHEN $7 THEN true ELSE dry_run_required END,
		       updated_by = $8::uuid,
		       updated_at = NOW()
		 WHERE shop_id = $1`, shopID, request.Enabled, request.StockPct, request.IntervalSeconds,
		request.ScopeMode, locations, changedCalculation, updatedBy)
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, shopID)
}

func (s *Store) SetPaused(ctx context.Context, shopID int64, reason, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shopee_stock_settings
		SET enabled = false, paused_reason = $2, last_error = $3, updated_at = NOW()
		WHERE shop_id = $1`, shopID, reason, message)
	return err
}

func (s *Store) MarkCatalogAttempted(ctx context.Context, shopID int64, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shopee_stock_settings SET last_catalog_attempt_at=NOW(),last_error=$2,updated_at=NOW() WHERE shop_id=$1`, shopID, errorMessage)
	return err
}

func (s *Store) MarkCatalogSynced(ctx context.Context, shopID int64, full bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shopee_stock_settings SET last_catalog_sync_at=NOW(),
		last_full_catalog_sync_at=CASE WHEN $2 THEN NOW() ELSE last_full_catalog_sync_at END,
		last_catalog_attempt_at=NOW(),last_error='',updated_at=NOW() WHERE shop_id=$1`, shopID, full)
	return err
}

func (s *Store) ReplaceSMLCatalog(ctx context.Context, items []sml.StockCatalogItem) error {
	return s.storeSMLCatalog(ctx, items, true)
}

func (s *Store) UpsertSMLCatalog(ctx context.Context, items []sml.StockCatalogItem) error {
	return s.storeSMLCatalog(ctx, items, false)
}

func (s *Store) storeSMLCatalog(ctx context.Context, items []sml.StockCatalogItem, replace bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_sml_catalog SET is_active=false`); err != nil {
			return err
		}
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO shopee_stock_sml_catalog
		  (item_code,item_name,standard_unit_code,units,barcodes,source_updated_at,synced_at,is_active,
		   item_type,set_component_count,set_definition_hash,set_document_valid,set_stock_valid,set_warning_codes,set_components)
		VALUES ($1,$2,$3,$4,$5,$6,NOW(),true,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (item_code) DO UPDATE SET
		  item_name=EXCLUDED.item_name, standard_unit_code=EXCLUDED.standard_unit_code,
		  units=EXCLUDED.units, barcodes=EXCLUDED.barcodes,
		  source_updated_at=EXCLUDED.source_updated_at, synced_at=NOW(), is_active=true,
		  item_type=EXCLUDED.item_type,set_component_count=EXCLUDED.set_component_count,
		  set_definition_hash=EXCLUDED.set_definition_hash,set_document_valid=EXCLUDED.set_document_valid,
		  set_stock_valid=EXCLUDED.set_stock_valid,set_warning_codes=EXCLUDED.set_warning_codes,
		  set_components=EXCLUDED.set_components`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		units, _ := json.Marshal(item.Units)
		barcodes, _ := json.Marshal(item.Barcodes)
		definition := item.SetDefinition
		componentCount, definitionHash := 0, ""
		documentValid, stockValid := item.ItemType != 3, item.ItemType != 3
		warnings := []string{}
		components := item.SetComponents
		if definition != nil {
			componentCount = definition.ComponentCount
			definitionHash = definition.Hash
			documentValid = definition.DocumentValid
			stockValid = definition.StockValid
			warnings = append(warnings, definition.WarningCodes...)
			components = definition.Components
		}
		warningsJSON, _ := json.Marshal(warnings)
		componentsJSON, _ := json.Marshal(components)
		if _, err := stmt.ExecContext(ctx, item.ItemCode, item.ItemName, item.StandardUnit, units, barcodes, item.UpdatedAt,
			item.ItemType, componentCount, definitionHash, documentValid, stockValid, warningsJSON, componentsJSON); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings st
		SET enabled=false,dry_run_required=true,paused_reason='set_definition_changed',
		    last_error='ส่วนประกอบสินค้าชุดเปลี่ยน กรุณาตรวจ Dry-run ใหม่',updated_at=NOW()
		WHERE EXISTS (
		  SELECT 1 FROM shopee_stock_mappings m
		  JOIN shopee_stock_sml_catalog c ON c.item_code=m.sml_item_code
		  WHERE m.shop_id=st.shop_id AND m.excluded=false AND c.item_type=3
		    AND m.set_definition_hash IS DISTINCT FROM c.set_definition_hash
		)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RefreshManualMappings(ctx context.Context, items []sml.StockCatalogItem, complete bool) error {
	byItem := make(map[string]map[string]sml.StockCatalogUnit, len(items))
	for _, item := range items {
		units := map[string]sml.StockCatalogUnit{}
		for _, unit := range item.Units {
			units[unit.Code] = unit
		}
		byItem[item.ItemCode] = units
	}
	rows, err := s.db.QueryContext(ctx, `SELECT shop_id,item_id,model_id,sml_item_code,sml_unit_code,manual_unit_factor::float8
		FROM shopee_stock_mappings WHERE match_source='manual' AND excluded=false`)
	if err != nil {
		return err
	}
	type row struct {
		shopID, itemID, modelID int64
		itemCode, unitCode      string
		manualFactor            *float64
	}
	values := []row{}
	for rows.Next() {
		var value row
		if err := rows.Scan(&value.shopID, &value.itemID, &value.modelID, &value.itemCode, &value.unitCode, &value.manualFactor); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, value := range values {
		units, itemIncluded := byItem[value.itemCode]
		if !itemIncluded && !complete {
			continue
		}
		unit, ok := units[value.unitCode]
		warnings := []string{}
		factor := float64(0)
		if !ok {
			warnings = []string{"unit_not_found"}
		} else if value.manualFactor != nil && *value.manualFactor >= 1 {
			factor = *value.manualFactor
		} else {
			warnings = UnitWarnings(unit)
			factor = UnitFactor(unit)
		}
		encoded, _ := json.Marshal(warnings)
		result, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET unit_factor=$4,warning_codes=$5,updated_at=NOW()
			WHERE shop_id=$1 AND item_id=$2 AND model_id=$3 AND (unit_factor IS DISTINCT FROM $4 OR warning_codes IS DISTINCT FROM $5::jsonb)`, value.shopID, value.itemID, value.modelID, factor, encoded)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET enabled=false,dry_run_required=true,updated_at=NOW() WHERE shop_id=$1`, value.shopID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ListSMLCatalog(ctx context.Context) ([]sml.StockCatalogItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_code,item_name,standard_unit_code,units,barcodes,COALESCE(source_updated_at,to_timestamp(0)),
		item_type,set_component_count,set_definition_hash,set_document_valid,set_stock_valid,set_warning_codes,set_components
		FROM shopee_stock_sml_catalog WHERE is_active=true ORDER BY item_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []sml.StockCatalogItem{}
	for rows.Next() {
		var item sml.StockCatalogItem
		var units, barcodes, warningsJSON, componentsJSON []byte
		var componentCount int
		var definitionHash string
		var documentValid, stockValid bool
		if err := rows.Scan(&item.ItemCode, &item.ItemName, &item.StandardUnit, &units, &barcodes, &item.UpdatedAt,
			&item.ItemType, &componentCount, &definitionHash, &documentValid, &stockValid, &warningsJSON, &componentsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(units, &item.Units); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(barcodes, &item.Barcodes); err != nil {
			return nil, err
		}
		if item.ItemType == 3 {
			definition := &sml.StockSetDefinition{ItemCode: item.ItemCode, ComponentCount: componentCount, Hash: definitionHash, DocumentValid: documentValid, StockValid: stockValid}
			_ = json.Unmarshal(warningsJSON, &definition.WarningCodes)
			_ = json.Unmarshal(componentsJSON, &definition.Components)
			item.SetDefinition = definition
			item.SetComponents = append([]sml.StockSetComponent(nil), definition.Components...)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SearchSMLCatalog(ctx context.Context, query string, limit int) ([]CatalogOption, error) {
	if limit < 1 || limit > 20 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT item_code,item_name,standard_unit_code,units,
		item_type,set_component_count,set_definition_hash,set_document_valid,set_stock_valid,set_warning_codes,set_components
		FROM shopee_stock_sml_catalog
		WHERE is_active=true AND (item_code ILIKE $1 OR item_name ILIKE $1)
		ORDER BY CASE
		  WHEN item_code=$2 THEN 0
		  WHEN item_code ILIKE $3 THEN 1
		  WHEN item_code ILIKE $1 THEN 2
		  ELSE 3
		END,item_code
		LIMIT $4`, "%"+query+"%", query, query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CatalogOption{}
	for rows.Next() {
		var item CatalogOption
		var units, warningsJSON, componentsJSON []byte
		if err := rows.Scan(&item.ItemCode, &item.ItemName, &item.StandardUnit, &units,
			&item.ItemType, &item.SetComponentCount, &item.SetDefinitionHash, &item.SetDocumentValid,
			&item.SetStockValid, &warningsJSON, &componentsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(units, &item.Units); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(warningsJSON, &item.SetWarningCodes)
		_ = json.Unmarshal(componentsJSON, &item.SetComponents)
		items = append(items, item)
	}
	return items, rows.Err()
}

type ShopeeProduct struct {
	ItemID         int64
	ModelID        int64
	ItemName       string
	ModelName      string
	ItemSKU        string
	ModelSKU       string
	ItemStatus     string
	ModelStatus    string
	Available      int64
	Reserved       int64
	SellerStock    []shopeeapi.SellerStock
	ProductUpdated *time.Time
}

func (s *Store) ReplaceShopeeProducts(ctx context.Context, shopID int64, products []ShopeeProduct, index *CatalogIndex) error {
	return s.storeShopeeProducts(ctx, shopID, products, index, true)
}

func (s *Store) UpsertShopeeProducts(ctx context.Context, shopID int64, products []ShopeeProduct, index *CatalogIndex) error {
	return s.storeShopeeProducts(ctx, shopID, products, index, false)
}

func (s *Store) storeShopeeProducts(ctx context.Context, shopID int64, products []ShopeeProduct, index *CatalogIndex, replace bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_products SET is_active=false WHERE shop_id=$1`, shopID); err != nil {
			return err
		}
	}
	productStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO shopee_stock_products
		 (shop_id,item_id,model_id,item_name,model_name,item_sku,model_sku,item_status,model_status,
		  shopee_available,shopee_reserved,seller_stock,product_updated_at,last_seen_at,is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),true)
		ON CONFLICT (shop_id,item_id,model_id) DO UPDATE SET
		 item_name=EXCLUDED.item_name, model_name=EXCLUDED.model_name, item_sku=EXCLUDED.item_sku,
		 model_sku=EXCLUDED.model_sku, item_status=EXCLUDED.item_status, model_status=EXCLUDED.model_status,
		 shopee_available=EXCLUDED.shopee_available, shopee_reserved=EXCLUDED.shopee_reserved,
		 seller_stock=EXCLUDED.seller_stock, product_updated_at=EXCLUDED.product_updated_at,
		 last_seen_at=NOW(), is_active=true`)
	if err != nil {
		return err
	}
	defer productStmt.Close()
	mappingStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO shopee_stock_mappings
		 (shop_id,item_id,model_id,sml_item_code,sml_unit_code,unit_factor,match_source,warning_codes,marketplace_alias_id,set_definition_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,$10)
		ON CONFLICT (shop_id,item_id,model_id) DO UPDATE SET
		 sml_item_code=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.sml_item_code ELSE EXCLUDED.sml_item_code END,
		 sml_unit_code=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.sml_unit_code ELSE EXCLUDED.sml_unit_code END,
		 unit_factor=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.unit_factor ELSE EXCLUDED.unit_factor END,
		 match_source=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.match_source ELSE EXCLUDED.match_source END,
		 warning_codes=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.warning_codes ELSE EXCLUDED.warning_codes END,
		 marketplace_alias_id=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.marketplace_alias_id ELSE EXCLUDED.marketplace_alias_id END,
		 set_definition_hash=CASE WHEN shopee_stock_mappings.match_source='manual' THEN shopee_stock_mappings.set_definition_hash ELSE EXCLUDED.set_definition_hash END,
		 updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer mappingStmt.Close()
	for _, product := range products {
		stockJSON, _ := json.Marshal(product.SellerStock)
		if _, err := productStmt.ExecContext(ctx, shopID, product.ItemID, product.ModelID, product.ItemName, product.ModelName,
			product.ItemSKU, product.ModelSKU, product.ItemStatus, product.ModelStatus, product.Available, product.Reserved,
			stockJSON, product.ProductUpdated); err != nil {
			return err
		}
		match, ok := index.Resolve(PreferredSKU(product.ModelSKU, product.ItemSKU))
		if !ok {
			match.Warnings = []string{"sku_not_found"}
		}
		warnings, _ := json.Marshal(match.Warnings)
		aliasID := ""
		if ok && match.Source == "sku" && match.ItemCode != "" && match.DocumentValid {
			aliasID, err = upsertShopeeStockMasterTx(ctx, tx, shopID, product.ItemID, product.ModelID,
				product.ItemName, product.ModelName, product.ItemSKU, product.ModelSKU, match.ItemCode, match.UnitCode,
				"exact_sku", "", "", nil)
			if err != nil {
				return err
			}
		}
		if _, err := mappingStmt.ExecContext(ctx, shopID, product.ItemID, product.ModelID, match.ItemCode, match.UnitCode, match.Factor, match.Source, warnings, aliasID, match.SetDefinitionHash); err != nil {
			return err
		}
	}
	if err := markDuplicateMappings(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings st
		SET enabled=false,dry_run_required=true,updated_at=NOW()
		WHERE st.shop_id=$1 AND EXISTS (
		  SELECT 1 FROM shopee_stock_mappings m
		  JOIN shopee_stock_sml_catalog c ON c.item_code=m.sml_item_code AND c.is_active=true
		  WHERE m.shop_id=st.shop_id AND m.excluded=false AND c.item_type=3
		    AND m.last_preview_target IS NULL
		)`, shopID); err != nil {
		return err
	}
	return tx.Commit()
}

func markDuplicateMappings(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE shopee_stock_mappings m
		   SET warning_codes=m.warning_codes-'duplicate_sml_item',updated_at=NOW()
		  FROM shopee_stock_products p
		 WHERE p.shop_id=m.shop_id AND p.item_id=m.item_id AND p.model_id=m.model_id AND p.is_active=true
		   AND m.warning_codes ? 'duplicate_sml_item'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		WITH active_groups AS (
		  SELECT m.shop_id,m.sml_item_code,COUNT(*) AS member_count
		    FROM shopee_stock_mappings m
		    JOIN shopee_stock_products p USING (shop_id,item_id,model_id)
		   WHERE p.is_active=true AND m.excluded=false AND m.sml_item_code<>''
		   GROUP BY m.shop_id,m.sml_item_code
		)
		UPDATE shopee_stock_mappings m
		   SET shared_pool_enabled=false,pool_allocation_pct=100,updated_at=NOW()
		  FROM shopee_stock_products p,active_groups g
		 WHERE p.shop_id=m.shop_id AND p.item_id=m.item_id AND p.model_id=m.model_id
		   AND p.is_active=true AND g.shop_id=m.shop_id AND g.sml_item_code=m.sml_item_code
		   AND g.member_count=1 AND (m.shared_pool_enabled=true OR m.pool_allocation_pct<>100)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		WITH duplicated AS (
		  SELECT m.shop_id,m.sml_item_code,
		         BOOL_AND(m.shared_pool_enabled) AS all_pool_enabled,
		         SUM(m.pool_allocation_pct)::float8 AS allocation_total
		    FROM shopee_stock_mappings m
		  JOIN shopee_stock_products p USING (shop_id,item_id,model_id)
		  WHERE p.is_active=true AND m.excluded=false AND m.sml_item_code<>''
		  GROUP BY m.shop_id,m.sml_item_code HAVING COUNT(*) > 1
		)
		UPDATE shopee_stock_mappings m
		   SET warning_codes=m.warning_codes||'["duplicate_sml_item"]'::jsonb,updated_at=NOW()
		  FROM shopee_stock_products p, duplicated d
		 WHERE p.shop_id=m.shop_id AND p.item_id=m.item_id AND p.model_id=m.model_id
		   AND p.is_active=true AND d.shop_id=m.shop_id AND d.sml_item_code=m.sml_item_code
		   AND NOT (d.all_pool_enabled AND ABS(d.allocation_total-100) <= 0.001)`)
	return err
}

func (s *Store) RefreshDuplicateMappings(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := markDuplicateMappings(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetSharedPool(ctx context.Context, shopID int64, smlItemCode string) (*SharedPool, error) {
	smlItemCode = strings.TrimSpace(smlItemCode)
	if shopID <= 0 || smlItemCode == "" {
		return nil, sql.ErrNoRows
	}
	var stockPct float64
	if err := s.db.QueryRowContext(ctx, `SELECT stock_pct::float8 FROM shopee_stock_settings WHERE shop_id=$1`, shopID).Scan(&stockPct); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.item_id,p.model_id,p.item_name,p.model_name,p.item_sku,p.model_sku,
		       p.shopee_available,p.shopee_reserved,m.sml_unit_code,COALESCE(c.units,'[]'::jsonb),
		       m.unit_factor::float8,m.shared_pool_enabled,m.pool_allocation_pct::float8,
		       m.last_preview_balance::float8,m.last_preview_pending_qty::float8,
		       m.last_preview_target,m.last_preview_pool_base_target,m.updated_at,
		       COALESCE(c.item_name,'')
		  FROM shopee_stock_mappings m
		  JOIN shopee_stock_products p USING(shop_id,item_id,model_id)
		  LEFT JOIN shopee_stock_sml_catalog c ON c.item_code=m.sml_item_code AND c.is_active=true
		 WHERE m.shop_id=$1 AND m.sml_item_code=$2 AND m.excluded=false AND p.is_active=true
		 ORDER BY p.item_name,p.model_name,p.item_id,p.model_id`, shopID, smlItemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pool := &SharedPool{ShopID: shopID, SMLItemCode: smlItemCode, StockPct: stockPct, Members: []SharedPoolMember{}}
	allEnabled := true
	for rows.Next() {
		var member SharedPoolMember
		var unitsJSON []byte
		if err := rows.Scan(&member.ItemID, &member.ModelID, &member.ItemName, &member.ModelName, &member.ItemSKU, &member.ModelSKU,
			&member.ShopeeAvailable, &member.ShopeeReserved, &member.SMLUnitCode, &unitsJSON,
			&member.UnitFactor, &member.SharedPoolEnabled, &member.PoolAllocationPct,
			&member.LastPreviewBalance, &member.LastPreviewPendingQty, &member.LastPreviewTarget,
			&member.LastPreviewPoolBaseTarget, &member.UpdatedAt, &pool.SMLItemName); err != nil {
			return nil, err
		}
		var units []sml.StockCatalogUnit
		if err := json.Unmarshal(unitsJSON, &units); err != nil {
			return nil, fmt.Errorf("decode SML units for shared pool %s: %w", smlItemCode, err)
		}
		product := ProductRow{SMLUnitCode: member.SMLUnitCode}
		populateProductUnitNames(&product, units)
		member.SMLUnitName = product.SMLUnitName
		member.SMLBaseUnitCode = product.SMLBaseUnitCode
		member.SMLBaseUnitName = product.SMLBaseUnitName
		pool.AllocationTotal += member.PoolAllocationPct
		allEnabled = allEnabled && member.SharedPoolEnabled
		pool.Members = append(pool.Members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pool.Members) < 2 {
		return nil, sql.ErrNoRows
	}
	pool.Configured = allEnabled && mathAbs(pool.AllocationTotal-100) <= 0.001
	return pool, nil
}

func (s *Store) UpdateSharedPool(ctx context.Context, shopID int64, request SharedPoolUpdate, userID string) (*SharedPool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT m.item_id,m.model_id,m.updated_at
		  FROM shopee_stock_mappings m
		  JOIN shopee_stock_products p USING(shop_id,item_id,model_id)
		 WHERE m.shop_id=$1 AND m.sml_item_code=$2 AND m.excluded=false AND p.is_active=true
		 ORDER BY m.item_id,m.model_id
		 FOR UPDATE OF m`, shopID, request.SMLItemCode)
	if err != nil {
		return nil, err
	}
	type currentMember struct {
		itemID, modelID int64
		updatedAt       time.Time
	}
	current := []currentMember{}
	for rows.Next() {
		var member currentMember
		if err := rows.Scan(&member.itemID, &member.modelID, &member.updatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		current = append(current, member)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(current) < 2 || len(current) != len(request.Members) {
		return nil, invalid("รายการในกลุ่มเปลี่ยนแล้ว กรุณารีเฟรชและตั้งค่าสต๊อกร่วมกันใหม่")
	}
	requested := make(map[string]SharedPoolMemberUpdate, len(request.Members))
	for _, member := range request.Members {
		requested[stockProductKey(member.ItemID, member.ModelID)] = member
	}
	for _, member := range current {
		requestMember, ok := requested[stockProductKey(member.itemID, member.modelID)]
		if !ok {
			return nil, invalid("กรุณากำหนดสัดส่วนให้ครบทุกรายการ Shopee ในกลุ่ม")
		}
		if requestMember.UpdatedAt.IsZero() || !requestMember.UpdatedAt.Equal(member.updatedAt) {
			return nil, ErrMappingConflict
		}
		result, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings
			SET shared_pool_enabled=true,pool_allocation_pct=$4,updated_by=NULLIF($5,'')::uuid,updated_at=NOW()
			WHERE shop_id=$1 AND item_id=$2 AND model_id=$3 AND updated_at=$6`,
			shopID, member.itemID, member.modelID, requestMember.AllocationPct, userID, member.updatedAt)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return nil, ErrMappingConflict
		}
	}
	if err := markDuplicateMappings(ctx, tx); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings
		SET enabled=false,dry_run_required=true,updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
		return nil, err
	}
	detail := map[string]any{"shop_id": shopID, "sml_item_code": request.SMLItemCode, "member_count": len(request.Members), "allocations": request.Members}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(action,user_id,source,level,detail)
		VALUES('shopee_shared_stock_pool_updated',NULLIF($1,'')::uuid,'shopee_stock','info',$2)`, userID, mustJSON(detail)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetSharedPool(ctx, shopID, request.SMLItemCode)
}

func (s *Store) PendingShopeeReservations(ctx context.Context, shopID int64) (map[string]float64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT (item->>'item_id')::bigint AS item_id,
		       COALESCE(NULLIF(item->>'model_id',''),'0')::bigint AS model_id,
		       SUM((item->>'model_quantity_purchased')::numeric)::float8 AS pending_qty
		  FROM shopee_order_snapshots snapshot
		 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(snapshot.raw_detail->'item_list','[]'::jsonb)) item
		 WHERE snapshot.shop_id=$1
		   AND snapshot.sml_doc_no=''
		   AND snapshot.erp_status NOT IN ('sent','cancelled')
		   AND snapshot.order_status NOT IN ('CANCELLED','IN_CANCEL','UNPAID')
		   AND COALESCE(item->>'item_id','') ~ '^[0-9]+$'
		   AND COALESCE(item->>'model_id','0') ~ '^[0-9]+$'
		   AND COALESCE(item->>'model_quantity_purchased','') ~ '^[0-9]+([.][0-9]+)?$'
		 GROUP BY item_id,model_id`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reservations := map[string]float64{}
	for rows.Next() {
		var itemID, modelID int64
		var qty float64
		if err := rows.Scan(&itemID, &modelID, &qty); err != nil {
			return nil, err
		}
		reservations[stockProductKey(itemID, modelID)] = qty
	}
	return reservations, rows.Err()
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func (s *Store) EnabledSMLItemsOtherShops(ctx context.Context, shopID int64) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT consumed.item_code
		FROM shopee_stock_mappings m
		JOIN shopee_stock_products p USING(shop_id,item_id,model_id)
		JOIN shopee_stock_settings st USING(shop_id)
		LEFT JOIN shopee_stock_sml_catalog c ON c.item_code=m.sml_item_code AND c.is_active=true
		CROSS JOIN LATERAL (
		  SELECT m.sml_item_code AS item_code WHERE COALESCE(c.item_type,0)<>3
		  UNION ALL
		  SELECT component->>'item_code' FROM jsonb_array_elements(COALESCE(c.set_components,'[]'::jsonb)) component
		  WHERE c.item_type=3
		) consumed
		WHERE m.shop_id<>$1 AND st.enabled=true AND p.is_active=true AND m.excluded=false
		  AND COALESCE(consumed.item_code,'')<>''`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[string]struct{}{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		items[code] = struct{}{}
	}
	return items, rows.Err()
}

func (s *Store) ListProducts(ctx context.Context, shopID int64) ([]ProductRow, error) {
	products, _, _, err := s.listProducts(ctx, shopID, ProductFilter{Page: 1, Size: 100000})
	return products, err
}

func (s *Store) ListProductsPage(ctx context.Context, shopID int64, filter ProductFilter) ([]ProductRow, int, ProductCounts, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Size < 1 || filter.Size > 100 {
		filter.Size = 50
	}
	return s.listProducts(ctx, shopID, filter)
}

func (s *Store) countProductsByStatus(ctx context.Context, shopID int64, query string) (ProductCounts, error) {
	query = strings.TrimSpace(query)
	queryFilter := ""
	args := []any{shopID}
	if query != "" {
		queryFilter = " AND (p.item_name ILIKE $2 OR p.model_name ILIKE $2 OR p.item_sku ILIKE $2 OR p.model_sku ILIKE $2 OR m.sml_item_code ILIKE $2)"
		args = append(args, "%"+query+"%")
	}
	var counts ProductCounts
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (
		           WHERE m.excluded=false AND m.sml_item_code<>'' AND jsonb_array_length(m.warning_codes)=0
		       )::int AS ready_count,
		       COUNT(*) FILTER (
		           WHERE m.excluded=false AND (m.sml_item_code='' OR jsonb_array_length(m.warning_codes)>0)
		       )::int AS fix_count,
		       COUNT(*) FILTER (WHERE m.excluded=true)::int AS excluded_count
		  FROM shopee_stock_products p
		  JOIN shopee_stock_mappings m USING(shop_id,item_id,model_id)
		 WHERE p.shop_id=$1 AND p.is_active=true`+queryFilter, args...).Scan(&counts.Ready, &counts.Fix, &counts.Excluded)
	return counts, err
}

func (c ProductCounts) totalForStatus(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready":
		return c.Ready
	case "fix":
		return c.Fix
	case "excluded":
		return c.Excluded
	default:
		return c.Ready + c.Fix + c.Excluded
	}
}

func (s *Store) listProducts(ctx context.Context, shopID int64, filter ProductFilter) ([]ProductRow, int, ProductCounts, error) {
	status := strings.ToLower(strings.TrimSpace(filter.Status))
	query := strings.TrimSpace(filter.Query)
	whereStatus := ""
	switch status {
	case "ready":
		whereStatus = " AND m.excluded=false AND m.sml_item_code<>'' AND jsonb_array_length(m.warning_codes)=0"
	case "fix":
		whereStatus = " AND m.excluded=false AND (m.sml_item_code='' OR jsonb_array_length(m.warning_codes)>0)"
	case "excluded":
		whereStatus = " AND m.excluded=true"
	}
	queryFilter := ""
	args := []any{shopID}
	if query != "" {
		queryFilter = " AND (p.item_name ILIKE $2 OR p.model_name ILIKE $2 OR p.item_sku ILIKE $2 OR p.model_sku ILIKE $2 OR m.sml_item_code ILIKE $2)"
		args = append(args, "%"+query+"%")
	}
	counts, err := s.countProductsByStatus(ctx, shopID, query)
	if err != nil {
		return nil, 0, ProductCounts{}, err
	}
	total := counts.totalForStatus(status)
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	args = append(args, filter.Size, (filter.Page-1)*filter.Size)
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.shop_id,p.item_id,p.model_id,p.item_name,p.model_name,p.item_sku,p.model_sku,
		       p.shopee_available,p.shopee_reserved,m.sml_item_code,COALESCE(c.item_name,''),m.sml_unit_code,
		       COALESCE(c.units,'[]'::jsonb),
		       m.unit_factor::float8,m.manual_unit_factor::float8,m.match_source,m.shared_pool_enabled,m.pool_allocation_pct::float8,
		       COALESCE(a.id::text,''),a.updated_at,m.excluded,m.warning_codes,
		       m.last_preview_balance::float8,m.last_preview_excluded_balance::float8,
		       COALESCE(m.last_preview_excluded_locations,'[]'::jsonb),
		       m.last_preview_min_qty::float8,m.last_preview_max_qty::float8,
		       m.last_preview_target,m.last_preview_pending_qty::float8,m.last_preview_pool_base_target,m.last_success_target,m.updated_at,
		       COALESCE(c.item_type,0),COALESCE(c.set_component_count,0),COALESCE(c.set_definition_hash,''),COALESCE(m.set_definition_hash,''),
		       COALESCE(c.set_document_valid,true),COALESCE(c.set_stock_valid,true),COALESCE(c.set_components,'[]'::jsonb)
		  FROM shopee_stock_products p
		  JOIN shopee_stock_mappings m USING (shop_id,item_id,model_id)
		  LEFT JOIN shopee_stock_sml_catalog c ON c.item_code=m.sml_item_code AND c.is_active=true
		  LEFT JOIN LATERAL (
		    SELECT master.id,master.updated_at
		      FROM marketplace_item_aliases master
		     WHERE master.is_active=true AND master.source='shopee'
		       AND ((master.id=m.marketplace_alias_id)
		         OR (master.account_key='shop:'||p.shop_id::text
		           AND master.external_item_id=p.item_id::text
		           AND master.external_variant_id=p.model_id::text))
		     ORDER BY (master.id=m.marketplace_alias_id) DESC
		     LIMIT 1
		  ) a ON true
		 WHERE p.shop_id=$1 AND p.is_active=true`+whereStatus+queryFilter+`
		 ORDER BY (jsonb_array_length(m.warning_codes)>0) DESC,p.item_name,p.model_name,p.item_id,p.model_id
		 LIMIT $`+strconv.Itoa(limitArg)+` OFFSET $`+strconv.Itoa(offsetArg), args...)
	if err != nil {
		return nil, 0, ProductCounts{}, err
	}
	defer rows.Close()
	products := []ProductRow{}
	for rows.Next() {
		var item ProductRow
		var warnings, unitsJSON, componentsJSON, excludedLocationsJSON []byte
		if err := rows.Scan(&item.ShopID, &item.ItemID, &item.ModelID, &item.ItemName, &item.ModelName, &item.ItemSKU, &item.ModelSKU,
			&item.ShopeeAvailable, &item.ShopeeReserved, &item.SMLItemCode, &item.SMLItemName, &item.SMLUnitCode, &unitsJSON, &item.UnitFactor, &item.ManualUnitFactor,
			&item.MatchSource, &item.SharedPoolEnabled, &item.PoolAllocationPct, &item.MarketplaceAliasID, &item.MarketplaceAliasUpdatedAt, &item.Excluded, &warnings,
			&item.LastPreviewBalance, &item.LastPreviewExcludedBalance, &excludedLocationsJSON, &item.LastPreviewMinQty, &item.LastPreviewMaxQty,
			&item.LastPreviewTarget, &item.LastPreviewPendingQty, &item.LastPreviewPoolBaseTarget, &item.LastSuccessTarget, &item.UpdatedAt,
			&item.SMLItemType, &item.SetComponentCount, &item.SetDefinitionHash, &item.MappingSetDefinitionHash,
			&item.SetDocumentValid, &item.SetStockValid, &componentsJSON); err != nil {
			return nil, 0, ProductCounts{}, err
		}
		_ = json.Unmarshal(warnings, &item.WarningCodes)
		_ = json.Unmarshal(excludedLocationsJSON, &item.LastPreviewExcludedLocations)
		var units []sml.StockCatalogUnit
		if err := json.Unmarshal(unitsJSON, &units); err != nil {
			return nil, 0, ProductCounts{}, fmt.Errorf("decode SML units for %s: %w", item.SMLItemCode, err)
		}
		_ = json.Unmarshal(componentsJSON, &item.SetComponents)
		populateProductUnitNames(&item, units)
		products = append(products, item)
	}
	return products, total, counts, rows.Err()
}

func (s *Store) UpdateMapping(ctx context.Context, shopID, itemID, modelID int64, request MappingUpdate, userID string) (*ProductRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var productName, modelName, itemSKU, modelSKU, previousSMLItemCode string
	if err := tx.QueryRowContext(ctx, `SELECT p.item_name,p.model_name,p.item_sku,p.model_sku,m.sml_item_code
		FROM shopee_stock_products p JOIN shopee_stock_mappings m USING(shop_id,item_id,model_id)
		WHERE p.shop_id=$1 AND p.item_id=$2 AND p.model_id=$3 AND p.is_active=true FOR UPDATE OF m`, shopID, itemID, modelID).
		Scan(&productName, &modelName, &itemSKU, &modelSKU, &previousSMLItemCode); err != nil {
		return nil, err
	}
	if request.Excluded && strings.TrimSpace(request.SMLItemCode) == "" {
		result, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings
			SET excluded=true,shared_pool_enabled=false,pool_allocation_pct=100,updated_by=NULLIF($5,'')::uuid,updated_at=NOW()
			WHERE shop_id=$1 AND item_id=$2 AND model_id=$3 AND updated_at=$4`, shopID, itemID, modelID, request.UpdatedAt, userID)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return nil, ErrMappingConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET enabled=false,dry_run_required=true,updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
			return nil, err
		}
		if err := insertStockMappingAuditTx(ctx, tx, userID, shopID, itemID, modelID, "", "", true); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		// The mapping transaction is already committed. Duplicate warnings can be
		// rebuilt by the next catalog refresh; do not report a successful save as
		// failed because this derived-state refresh was temporarily unavailable.
		_ = s.RefreshDuplicateMappings(ctx)
		return s.findProduct(ctx, shopID, itemID, modelID)
	}
	var item sml.StockCatalogItem
	var unitsJSON, barcodesJSON, setWarningsJSON, setComponentsJSON []byte
	var componentCount int
	var definitionHash string
	var documentValid, stockValid bool
	err = tx.QueryRowContext(ctx, `SELECT item_code,item_name,standard_unit_code,units,barcodes,COALESCE(source_updated_at,to_timestamp(0)),
		item_type,set_component_count,set_definition_hash,set_document_valid,set_stock_valid,set_warning_codes,set_components
		FROM shopee_stock_sml_catalog WHERE item_code=$1 AND is_active=true`, request.SMLItemCode).Scan(
		&item.ItemCode, &item.ItemName, &item.StandardUnit, &unitsJSON, &barcodesJSON, &item.UpdatedAt,
		&item.ItemType, &componentCount, &definitionHash, &documentValid, &stockValid, &setWarningsJSON, &setComponentsJSON)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(unitsJSON, &item.Units)
	_ = json.Unmarshal(barcodesJSON, &item.Barcodes)
	if item.ItemType == 3 {
		definition := &sml.StockSetDefinition{ItemCode: item.ItemCode, ComponentCount: componentCount, Hash: definitionHash, DocumentValid: documentValid, StockValid: stockValid}
		_ = json.Unmarshal(setWarningsJSON, &definition.WarningCodes)
		_ = json.Unmarshal(setComponentsJSON, &definition.Components)
		item.SetDefinition = definition
		item.SetComponents = append([]sml.StockSetComponent(nil), definition.Components...)
		if !definition.StockValid {
			return nil, invalid("สินค้าชุดนี้ยังไม่พร้อมซิงก์สต๊อก กรุณาแก้ส่วนประกอบใน SML แล้วอัปเดตรายการ")
		}
		if !definition.DocumentValid {
			return nil, invalid("สินค้าชุดนี้ยังไม่พร้อมใช้ใน Product Master กรุณาแก้ส่วนประกอบใน SML แล้วอัปเดตรายการ")
		}
	}
	var unit sml.StockCatalogUnit
	found := false
	for _, candidate := range item.Units {
		if candidate.Code == request.SMLUnitCode {
			unit, found = candidate, true
			break
		}
	}
	if !found {
		return nil, ErrInvalidUnit
	}
	exactSKU := strings.TrimSpace(modelSKU)
	if exactSKU == "" {
		exactSKU = strings.TrimSpace(itemSKU)
	}
	masterMatchMethod := "manual_identity"
	if exactSKU != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM shopee_stock_sml_catalog WHERE item_code=$1 AND is_active=true)`, exactSKU).Scan(&exists); err != nil {
			return nil, err
		}
		if exists && exactSKU != item.ItemCode {
			return nil, invalid("SKU ตรงกับสินค้า SML " + exactSKU + " อยู่แล้ว จึงไม่สามารถจับคู่ข้ามรหัสได้")
		}
		if exists {
			masterMatchMethod = "exact_sku"
		}
	}
	factor := UnitFactor(unit)
	warningsList := UnitWarnings(unit)
	if item.ItemType == 3 {
		for _, warning := range stockSetWarnings(item) {
			warningsList = appendUnique(warningsList, warning)
		}
	}
	if request.ManualUnitFactor != nil {
		if *request.ManualUnitFactor < 1 {
			return nil, ErrInvalidManualFactor
		}
		factor = *request.ManualUnitFactor
		warningsList = nil
	}
	warnings, _ := json.Marshal(warningsList)
	aliasID, err := upsertShopeeStockMasterTx(ctx, tx, shopID, itemID, modelID, productName, modelName, itemSKU, modelSKU,
		item.ItemCode, item.StandardUnit, masterMatchMethod, userID, request.MarketplaceAliasID, request.MarketplaceAliasUpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := applyShopeeStockMasterToOpenBillsTx(ctx, tx, shopID, itemID, modelID, aliasID, item.ItemCode, item.StandardUnit); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE shopee_stock_mappings
		   SET sml_item_code=$5,sml_unit_code=$6,unit_factor=$7,manual_unit_factor=$8,match_source='manual',excluded=$9,
		       warning_codes=$10,marketplace_alias_id=$12,set_definition_hash=$13,
		       shared_pool_enabled=false,pool_allocation_pct=100,
		       updated_by=NULLIF($11,'')::uuid,updated_at=NOW()
		 WHERE shop_id=$1 AND item_id=$2 AND model_id=$3 AND updated_at=$4`,
		shopID, itemID, modelID, request.UpdatedAt, item.ItemCode, unit.Code, factor, request.ManualUnitFactor, request.Excluded, warnings, userID, aliasID, definitionHash)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil, ErrMappingConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET enabled=false,dry_run_required=true,updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
		return nil, err
	}
	if err := insertStockMappingAuditTx(ctx, tx, userID, shopID, itemID, modelID, aliasID, item.ItemCode, request.Excluded); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = s.RefreshDuplicateMappings(ctx)
	return s.findProduct(ctx, shopID, itemID, modelID)
}

func insertStockMappingAuditTx(ctx context.Context, tx *sql.Tx, userID string, shopID, itemID, modelID int64, aliasID, itemCode string, excluded bool) error {
	detail, err := json.Marshal(map[string]interface{}{
		"shop_id": shopID, "item_id": itemID, "model_id": modelID,
		"marketplace_alias_id": aliasID, "item_code": itemCode, "excluded": excluded,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(action,user_id,source,level,detail)
		VALUES('shopee_stock_mapping_updated',NULLIF($1,'')::uuid,'shopee','info',$2)`, userID, detail)
	return err
}

func (s *Store) findProduct(ctx context.Context, shopID, itemID, modelID int64) (*ProductRow, error) {
	products, err := s.ListProducts(ctx, shopID)
	if err != nil {
		return nil, err
	}
	for index := range products {
		if products[index].ItemID == itemID && products[index].ModelID == modelID {
			return &products[index], nil
		}
	}
	return nil, sql.ErrNoRows
}

func upsertShopeeStockMasterTx(
	ctx context.Context,
	tx *sql.Tx,
	shopID, itemID, modelID int64,
	itemName, modelName, itemSKU, modelSKU, smlItemCode, standardUnit, matchMethod, userID, expectedAliasID string,
	expectedAliasUpdatedAt *time.Time,
) (string, error) {
	sourceSKU := strings.TrimSpace(modelSKU)
	if sourceSKU == "" {
		sourceSKU = strings.TrimSpace(itemSKU)
	}
	rawName := strings.TrimSpace(strings.Join(nonEmptyStrings(itemName, modelName), " / "))
	normalized := strings.Join(strings.Fields(strings.ReplaceAll(rawName, "\ufeff", "")), " ")
	accountKey := "shop:" + strconv.FormatInt(shopID, 10)
	externalItemID := strconv.FormatInt(itemID, 10)
	externalVariantID := strconv.FormatInt(modelID, 10)

	if expectedAliasID != "" {
		if expectedAliasUpdatedAt == nil {
			return "", ErrMappingConflict
		}
		var currentUpdatedAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT updated_at FROM marketplace_item_aliases
			WHERE id=$1 AND source='shopee' AND account_key=$2 AND external_item_id=$3
			  AND external_variant_id=$4 AND is_active=true FOR UPDATE`,
			expectedAliasID, accountKey, externalItemID, externalVariantID).Scan(&currentUpdatedAt); err != nil {
			if err == sql.ErrNoRows {
				return "", ErrMappingConflict
			}
			return "", err
		}
		if !currentUpdatedAt.Equal(*expectedAliasUpdatedAt) {
			return "", ErrMappingConflict
		}
		return updateShopeeStockMasterTx(ctx, tx, expectedAliasID, sourceSKU, rawName, normalized, smlItemCode, standardUnit, matchMethod, userID)
	}

	var existingID string
	err := tx.QueryRowContext(ctx, `SELECT id::text FROM marketplace_item_aliases
		WHERE source='shopee' AND account_key=$1 AND external_item_id=$2
		  AND external_variant_id=$3 AND is_active=true FOR UPDATE`, accountKey, externalItemID, externalVariantID).Scan(&existingID)
	if err == nil {
		return updateShopeeStockMasterTx(ctx, tx, existingID, sourceSKU, rawName, normalized, smlItemCode, standardUnit, matchMethod, userID)
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	// A historical SKU/name Master may predate stable Shopee IDs. Promote it
	// only when it already points to the same SML item; otherwise require admin
	// review instead of silently changing an established mapping.
	candidateWhere := `source='shopee' AND account_key=$1 AND external_item_id='' AND source_sku='' AND normalized_key=$2`
	candidateArgs := []interface{}{accountKey, normalized, smlItemCode}
	if sourceSKU != "" {
		candidateWhere = `source='shopee' AND account_key=$1 AND external_item_id='' AND source_sku=$2`
		candidateArgs = []interface{}{accountKey, sourceSKU, smlItemCode}
	}
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM marketplace_item_aliases WHERE `+candidateWhere+`
		AND item_code=$3 AND is_active=true FOR UPDATE`, candidateArgs...).Scan(&existingID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE marketplace_item_aliases
			SET external_item_id=$2,external_variant_id=$3,updated_at=NOW()
			WHERE id=$1`, existingID, externalItemID, externalVariantID)
		if err != nil {
			return "", marketplaceMasterConflict(err)
		}
		return updateShopeeStockMasterTx(ctx, tx, existingID, sourceSKU, rawName, normalized, smlItemCode, standardUnit, matchMethod, userID)
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	err = tx.QueryRowContext(ctx, `INSERT INTO marketplace_item_aliases(
		source,account_key,external_item_id,external_variant_id,source_sku,raw_name,normalized_key,
		item_code,unit_code,confidence,confirmed_by,usage_count,last_used_at,match_method,scope_confirmed,is_active)
		VALUES('shopee',$1,$2,$3,$4,$5,$6,$7,$8,1.0,NULLIF($10,'')::uuid,0,NOW(),$9,true,true)
		ON CONFLICT DO NOTHING RETURNING id::text`, accountKey, externalItemID, externalVariantID, sourceSKU, rawName, normalized, smlItemCode, standardUnit, matchMethod, userID).Scan(&existingID)
	if err == sql.ErrNoRows {
		return "", invalid("SKU หรือรหัสตัวเลือกนี้มี Product Master อื่นในร้านแล้ว กรุณาตรวจรายการจับคู่สินค้า Marketplace")
	}
	if err != nil {
		return "", marketplaceMasterConflict(err)
	}
	return existingID, nil
}

func updateShopeeStockMasterTx(ctx context.Context, tx *sql.Tx, aliasID, sourceSKU, rawName, normalized, itemCode, unitCode, matchMethod, userID string) (string, error) {
	_, err := tx.ExecContext(ctx, `UPDATE marketplace_item_aliases SET
		source_sku=$2,raw_name=$3,normalized_key=$4,item_code=$5,unit_code=$6,confidence=1.0,
		confirmed_by=COALESCE(NULLIF($7,'')::uuid,confirmed_by),match_method=$8,
		scope_confirmed=true,is_active=true,
		updated_at=CASE WHEN source_sku IS DISTINCT FROM $2 OR raw_name IS DISTINCT FROM $3
			OR normalized_key IS DISTINCT FROM $4 OR item_code IS DISTINCT FROM $5
			OR unit_code IS DISTINCT FROM $6 OR match_method IS DISTINCT FROM $8
			THEN NOW() ELSE updated_at END
		WHERE id=$1`, aliasID, sourceSKU, rawName, normalized, itemCode, unitCode, userID, matchMethod)
	if err != nil {
		return "", marketplaceMasterConflict(err)
	}
	return aliasID, nil
}

func marketplaceMasterConflict(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return invalid("SKU หรือรหัสตัวเลือกนี้มี Product Master อื่นในร้านแล้ว กรุณาตรวจรายการจับคู่สินค้า Marketplace")
	}
	return err
}

func applyShopeeStockMasterToOpenBillsTx(ctx context.Context, tx *sql.Tx, shopID, itemID, modelID int64, aliasID, itemCode, unitCode string) error {
	accountKey := "shop:" + strconv.FormatInt(shopID, 10)
	_, err := tx.ExecContext(ctx, `UPDATE bill_items bi
		SET item_code=$1,unit_code=$2,mapped=true,marketplace_alias_id=$3
		FROM bills b
		WHERE bi.bill_id=b.id AND b.source='shopee' AND b.source_account_key=$4
		  AND b.bill_type='sale' AND b.status IN ('pending','needs_review') AND b.archived_at IS NULL
		  AND bi.source_item_id=$5 AND bi.source_variant_id=$6
		  AND NOT EXISTS (SELECT 1 FROM sml_catalog c WHERE c.is_active=true
		    AND c.item_code=btrim(replace(COALESCE(bi.source_sku,''),chr(65279),'')))`,
		itemCode, unitCode, aliasID, accountKey, strconv.FormatInt(itemID, 10), strconv.FormatInt(modelID, 10))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE bills b SET status='pending',error_msg=NULL
		WHERE b.source='shopee' AND b.source_account_key=$1 AND b.bill_type='sale'
		  AND b.status='needs_review' AND b.archived_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM bill_items bi WHERE bi.bill_id=b.id
		    AND (COALESCE(bi.item_code,'')='' OR bi.mapped IS DISTINCT FROM true))`, accountKey)
	return err
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && (len(out) == 0 || out[len(out)-1] != value) {
			out = append(out, value)
		}
	}
	return out
}

func (s *Store) SetDryRunRequired(ctx context.Context, shopID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shopee_stock_settings SET enabled=false,dry_run_required=true,updated_at=NOW() WHERE shop_id=$1`, shopID)
	return err
}

func (s *Store) CreateRun(ctx context.Context, shopID int64, runType, trigger, asOfDate string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `INSERT INTO shopee_stock_runs(shop_id,run_type,trigger_source,as_of_date)
		VALUES($1,$2,$3,NULLIF($4,'')::date) RETURNING id::text`, shopID, runType, trigger, asOfDate).Scan(&id)
	return id, err
}

func (s *Store) FinishRun(ctx context.Context, runID, status string, result *PreviewResult, errorCount int, errorMessage string) error {
	if result == nil {
		result = &PreviewResult{}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE shopee_stock_runs SET status=$2,total_count=$3,changed_count=$4,
		skipped_count=$5,blocked_count=$6,error_count=$7,error_message=$8,finished_at=NOW() WHERE id=$1::uuid`,
		runID, status, result.TotalCount, result.ChangedCount, result.SkippedCount, result.BlockedCount, errorCount, errorMessage)
	return err
}

func (s *Store) SavePreview(ctx context.Context, shopID int64, result *PreviewResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, line := range result.Lines {
		excludedLocationsJSON, err := json.Marshal(line.ExcludedLocations)
		if err != nil {
			return fmt.Errorf("encode excluded stock locations for %d/%d: %w", line.ItemID, line.ModelID, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET
			last_preview_balance=$4,last_preview_excluded_balance=$5,last_preview_min_qty=$6,
			last_preview_max_qty=$7,last_preview_target=$8,last_preview_pending_qty=$12,
			last_preview_pool_base_target=NULLIF($13,0),
			last_preview_excluded_locations=$14::jsonb,
			set_definition_hash=CASE WHEN $9=3 AND $10=false THEN $11 ELSE set_definition_hash END,
			updated_at=updated_at
			WHERE shop_id=$1 AND item_id=$2 AND model_id=$3`, shopID, line.ItemID, line.ModelID,
			line.ScopeBalance, line.ExcludedBalance, line.MinQty, line.MaxQty, line.TargetStock,
			line.ItemType, line.Blocked, line.SetDefinitionHash, line.PendingNexflowQty, line.PoolBaseTarget, excludedLocationsJSON); err != nil {
			return err
		}
		if !line.Changed && !line.Blocked {
			continue
		}
		kind := "changed"
		reason := ""
		message := ""
		if line.Blocked {
			kind = "blocked"
			reason = firstWarning(line.WarningCodes)
			message = strings.Join(line.WarningCodes, ",")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO shopee_stock_attempts
			(run_id,shop_id,item_id,model_id,sml_item_code,result,previous_stock,target_stock,reason_code,message)
			VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, result.RunID, shopID, line.ItemID, line.ModelID, line.SMLItemCode, kind, line.CurrentStock, line.TargetStock, reason, message); err != nil {
			return err
		}
	}
	if result.CircuitBreaker == "" {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET dry_run_required=false,paused_reason='',last_preview_at=NOW(),last_error='',updated_at=NOW() WHERE shop_id=$1`, shopID); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET enabled=false,dry_run_required=true,paused_reason=$2,last_preview_at=NOW(),updated_at=NOW() WHERE shop_id=$1`, shopID, result.CircuitBreaker); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveSyncAttempt(ctx context.Context, runID string, shopID int64, line PreviewLine, result, reason, message, requestID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO shopee_stock_attempts
		(run_id,shop_id,item_id,model_id,sml_item_code,result,previous_stock,target_stock,reason_code,message,request_id)
		VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, runID, shopID, line.ItemID, line.ModelID, line.SMLItemCode, result, line.CurrentStock, line.TargetStock, reason, message, requestID)
	return err
}

func (s *Store) MarkSyncSuccess(ctx context.Context, shopID int64, lines []PreviewLine) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, line := range lines {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET last_success_target=$4 WHERE shop_id=$1 AND item_id=$2 AND model_id=$3`, shopID, line.ItemID, line.ModelID, line.TargetStock); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_products SET shopee_available=$4 WHERE shop_id=$1 AND item_id=$2 AND model_id=$3`, shopID, line.ItemID, line.ModelID, line.TargetStock); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET last_sync_at=NOW(),last_success_at=NOW(),last_error='',updated_at=NOW() WHERE shop_id=$1`, shopID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordSyncChecked(ctx context.Context, shopID int64, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shopee_stock_settings SET last_sync_at=NOW(),last_error=$2,updated_at=NOW() WHERE shop_id=$1`, shopID, errorMessage)
	return err
}

func (s *Store) ListRuns(ctx context.Context, shopID int64, limit int) ([]Run, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,shop_id,run_type,trigger_source,status,COALESCE(as_of_date::text,''),
		total_count,changed_count,skipped_count,blocked_count,error_count,error_message,started_at,finished_at
		FROM shopee_stock_runs WHERE ($1=0 OR shop_id=$1) ORDER BY started_at DESC LIMIT $2`, shopID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.ShopID, &run.RunType, &run.TriggerSource, &run.Status, &run.AsOfDate, &run.TotalCount, &run.ChangedCount, &run.SkippedCount, &run.BlockedCount, &run.ErrorCount, &run.ErrorMessage, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) AcquireLease(ctx context.Context, shopID int64, owner string, duration time.Duration) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO shopee_stock_leases(shop_id,owner_id,lease_until)
		VALUES($1,$2,NOW()+make_interval(secs=>$3))
		ON CONFLICT(shop_id) DO UPDATE SET owner_id=EXCLUDED.owner_id,lease_until=EXCLUDED.lease_until,updated_at=NOW()
		WHERE shopee_stock_leases.lease_until < NOW()`, shopID, owner, int(duration.Seconds()))
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (s *Store) ReleaseLease(ctx context.Context, shopID int64, owner string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM shopee_stock_leases WHERE shop_id=$1 AND owner_id=$2`, shopID, owner)
	return err
}

func (s *Store) AcquireCatalogLease(ctx context.Context, owner string, duration time.Duration) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO shopee_stock_catalog_lease(singleton,owner_id,lease_until)
		VALUES(true,$1,NOW()+make_interval(secs=>$2))
		ON CONFLICT(singleton) DO UPDATE SET owner_id=EXCLUDED.owner_id,lease_until=EXCLUDED.lease_until,updated_at=NOW()
		WHERE shopee_stock_catalog_lease.lease_until < NOW()`, owner, int(duration.Seconds()))
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (s *Store) ReleaseCatalogLease(ctx context.Context, owner string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM shopee_stock_catalog_lease WHERE singleton=true AND owner_id=$1`, owner)
	return err
}

func (s *Store) CatalogDueShops(ctx context.Context) ([]CatalogDue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT st.shop_id,
		(st.last_full_catalog_sync_at IS NULL OR st.last_full_catalog_sync_at < NOW()-INTERVAL '24 hours')
		FROM shopee_stock_settings st
		JOIN shopee_api_connections c ON c.shop_id=st.shop_id
		WHERE st.enabled=true AND c.disabled_at IS NULL AND c.credential_mode='gateway'
		AND (st.last_catalog_attempt_at IS NULL OR st.last_catalog_attempt_at < NOW()-INTERVAL '1 hour')
		ORDER BY st.shop_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CatalogDue{}
	for rows.Next() {
		var item CatalogDue
		if err := rows.Scan(&item.ShopID, &item.Full); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) EnabledDueShops(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT st.shop_id FROM shopee_stock_settings st
		JOIN shopee_api_connections c ON c.shop_id=st.shop_id
		WHERE st.enabled=true AND st.dry_run_required=false AND st.paused_reason=''
		AND c.disabled_at IS NULL AND c.credential_mode='gateway'
		AND (st.last_sync_at IS NULL OR st.last_sync_at + make_interval(secs=>st.interval_seconds) <= NOW())
		ORDER BY st.shop_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) DeleteOldAttempts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM shopee_stock_attempts WHERE created_at < NOW()-INTERVAL '90 days'`)
	return err
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }
func firstWarning(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

var (
	ErrDryRunRequired      = errors.New("ต้องตรวจผลกระทบแบบ dry-run ก่อนเปิดซิงก์")
	ErrInvalidUnit         = errors.New("ไม่พบหน่วยนับที่เลือกในสินค้า SML")
	ErrInvalidManualFactor = errors.New("อัตราส่วนที่กำหนดเองต้องไม่น้อยกว่า 1")
	ErrMappingConflict     = errors.New("ข้อมูลการจับคู่ถูกแก้ไขโดยผู้ใช้อื่น กรุณารีเฟรช")
)
