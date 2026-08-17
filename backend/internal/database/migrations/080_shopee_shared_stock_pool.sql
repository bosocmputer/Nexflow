-- 080_shopee_shared_stock_pool.sql
-- Share one SML stock balance across multiple Shopee item/model listings.
-- Existing duplicate mappings remain blocked until an admin explicitly saves
-- a complete 100% allocation and runs a new dry-run.

ALTER TABLE shopee_stock_mappings
  ADD COLUMN IF NOT EXISTS shared_pool_enabled BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS pool_allocation_pct NUMERIC(5,2) NOT NULL DEFAULT 100,
  ADD COLUMN IF NOT EXISTS last_preview_pending_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_pool_base_target BIGINT;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'shopee_stock_mappings_pool_allocation_pct_check'
  ) THEN
    ALTER TABLE shopee_stock_mappings
      ADD CONSTRAINT shopee_stock_mappings_pool_allocation_pct_check
      CHECK (pool_allocation_pct > 0 AND pool_allocation_pct <= 100);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS shopee_stock_mappings_shared_pool_idx
  ON shopee_stock_mappings(shop_id, sml_item_code, pool_allocation_pct)
  WHERE shared_pool_enabled = true AND excluded = false AND sml_item_code <> '';

CREATE INDEX IF NOT EXISTS shopee_order_snapshots_stock_reservation_idx
  ON shopee_order_snapshots(shop_id, erp_status, order_status)
  WHERE sml_doc_no = '' AND erp_status NOT IN ('sent', 'cancelled');

-- The migration runner intentionally replays every file on startup, so guard
-- this one-time safety pause with a durable marker. The target formula now
-- reserves active Shopee orders that have not reached SML yet; require one
-- reviewed dry-run before an already-enabled shop writes with the new formula.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM app_settings WHERE key = 'migration.080_stock_formula_paused'
  ) THEN
    UPDATE shopee_stock_settings
       SET enabled = false,
           dry_run_required = true,
           last_error = 'อัปเดตสูตรสำรองสต๊อกสำหรับออเดอร์ใหม่ กรุณาตรวจ Dry-run ก่อนเปิดซิงก์',
           updated_at = NOW()
     WHERE enabled = true;

    INSERT INTO app_settings(key, value, is_secret)
    VALUES ('migration.080_stock_formula_paused', 'true', false)
    ON CONFLICT (key) DO NOTHING;
  END IF;
END $$;
