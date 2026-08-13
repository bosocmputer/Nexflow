-- 078_sml_set_products.sql
-- Additive local metadata for SML item_type=3 products. SML remains the source
-- of truth and its schema/data are never modified by this migration.

ALTER TABLE sml_catalog
  ADD COLUMN IF NOT EXISTS item_type INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS set_component_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS set_definition_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS set_document_valid BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS set_stock_valid BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS set_warning_codes JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE IF NOT EXISTS sml_catalog_set_components (
  parent_item_code TEXT NOT NULL,
  line_number INTEGER NOT NULL DEFAULT 0,
  row_order INTEGER NOT NULL DEFAULT 0,
  component_item_code TEXT NOT NULL,
  component_item_name TEXT NOT NULL DEFAULT '',
  component_item_type INTEGER NOT NULL DEFAULT 0,
  unit_code TEXT NOT NULL DEFAULT '',
  qty NUMERIC NOT NULL DEFAULT 0,
  price NUMERIC NOT NULL DEFAULT 0,
  sum_amount NUMERIC NOT NULL DEFAULT 0,
  price_ratio NUMERIC NOT NULL DEFAULT 0,
  unit_factor NUMERIC NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT FALSE,
  unit_valid BOOLEAN NOT NULL DEFAULT FALSE,
  definition_hash TEXT NOT NULL DEFAULT '',
  synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (parent_item_code, line_number, row_order, component_item_code)
);

CREATE INDEX IF NOT EXISTS sml_catalog_set_components_parent_idx
  ON sml_catalog_set_components(parent_item_code, line_number, row_order);
CREATE INDEX IF NOT EXISTS sml_catalog_set_components_component_idx
  ON sml_catalog_set_components(component_item_code);
CREATE INDEX IF NOT EXISTS sml_catalog_set_validity_idx
  ON sml_catalog(item_type, set_document_valid, set_stock_valid)
  WHERE is_active = TRUE AND item_type = 3;

ALTER TABLE shopee_stock_sml_catalog
  ADD COLUMN IF NOT EXISTS item_type INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS set_component_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS set_definition_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS set_document_valid BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS set_stock_valid BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS set_warning_codes JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS set_components JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE shopee_stock_mappings
  ADD COLUMN IF NOT EXISTS set_definition_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS shopee_stock_sml_catalog_set_idx
  ON shopee_stock_sml_catalog(item_type, set_stock_valid)
  WHERE is_active = TRUE AND item_type = 3;
