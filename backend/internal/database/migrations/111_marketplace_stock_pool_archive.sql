-- Keep prior Marketplace stock-run and audit evidence while allowing an
-- administrator to remove an obsolete pool from active configuration.
ALTER TABLE marketplace_stock_pools
  ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS archived_by UUID REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE marketplace_stock_pools
  DROP CONSTRAINT IF EXISTS marketplace_stock_pools_sml_item_code_sml_unit_code_key;

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_stock_pools_active_sml_item_unit_idx
  ON marketplace_stock_pools(sml_item_code, sml_unit_code)
  WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS marketplace_stock_pools_active_updated_idx
  ON marketplace_stock_pools(updated_at DESC)
  WHERE archived_at IS NULL;
