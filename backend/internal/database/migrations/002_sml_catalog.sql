-- 002_sml_catalog.sql — SML Product Catalog + Smart Matching

-- 1. Add marketplace email/import sources to bills.source.
-- Include later sources here because this migration is intentionally
-- idempotent and can re-run after rows from newer channels already exist.
ALTER TABLE bills DROP CONSTRAINT IF EXISTS bills_source_check;
ALTER TABLE bills ADD CONSTRAINT bills_source_check
  CHECK (source IN ('line','email','lazada','tiktok','shopee','shopee_email','shopee_shipped','manual'));

-- 2. Add needs_review to bills.status
ALTER TABLE bills DROP CONSTRAINT IF EXISTS bills_status_check;
ALTER TABLE bills ADD CONSTRAINT bills_status_check
  CHECK (status IN ('pending','confirmed','sent','failed','skipped','needs_review'));

-- 3. Add sml_order_id to bills (for Shopee email order dedup)
ALTER TABLE bills ADD COLUMN IF NOT EXISTS sml_order_id TEXT;
CREATE INDEX IF NOT EXISTS idx_bills_sml_order_id ON bills(sml_order_id) WHERE sml_order_id IS NOT NULL;

-- 4. Add candidates column to bill_items (top-5 catalog matches)
-- Format: [{"item_code":"CON-01000","item_name":"ปูน...","score":0.92,"unit_code":"ถุง"}]
ALTER TABLE bill_items ADD COLUMN IF NOT EXISTS candidates JSONB DEFAULT '[]';

-- 5. SML Product Catalog
-- Embedding stored as JSONB array of float64 (768 dims from Gemini text-embedding-004)
CREATE TABLE IF NOT EXISTS sml_catalog (
  item_code        TEXT PRIMARY KEY,
  item_name        TEXT NOT NULL,
  item_name2       TEXT NOT NULL DEFAULT '',
  unit_code        TEXT NOT NULL DEFAULT '',
  wh_code          TEXT NOT NULL DEFAULT '',
  shelf_code       TEXT NOT NULL DEFAULT '',
  price            NUMERIC(14,4),
  group_code       TEXT NOT NULL DEFAULT '',
  balance_qty      NUMERIC(14,4),
  -- Embedding fields
  embedding_status TEXT NOT NULL DEFAULT 'pending'
                     CHECK (embedding_status IN ('pending','done','error')),
  embedded_at      TIMESTAMPTZ,
  embedding        JSONB,          -- []float64, 768 dims
  embedding_model  TEXT,
  -- Sync metadata
  synced_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sml_catalog_embedding_status ON sml_catalog(embedding_status);
CREATE INDEX IF NOT EXISTS idx_sml_catalog_item_name ON sml_catalog USING gin(to_tsvector('simple', item_name));
