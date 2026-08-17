-- 081_shopee_stock_excluded_locations.sql
-- Persist the latest dry-run warehouse/location explanation per Shopee model.
-- This is presentation-only diagnostic data and never participates in target
-- stock calculation or Shopee writes.

ALTER TABLE shopee_stock_mappings
  ADD COLUMN IF NOT EXISTS last_preview_excluded_locations JSONB NOT NULL DEFAULT '[]'::jsonb;
