-- Marketplace item aliases map platform-specific SKU/name/variant keys to SML items.
-- They let staff confirm a product once in BillFlow instead of editing 1000+
-- marketplace listings by hand.

CREATE TABLE IF NOT EXISTS marketplace_item_aliases (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source         TEXT NOT NULL CHECK (source IN ('shopee','lazada','tiktok')),
  source_sku     TEXT NOT NULL DEFAULT '',
  raw_name       TEXT NOT NULL DEFAULT '',
  normalized_key TEXT NOT NULL,
  item_code      TEXT NOT NULL,
  unit_code      TEXT NOT NULL DEFAULT '',
  confidence     NUMERIC(6,3) NOT NULL DEFAULT 1.0,
  confirmed_by   UUID REFERENCES users(id),
  usage_count    INT NOT NULL DEFAULT 0,
  last_used_at   TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (source_sku <> '' OR normalized_key <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_item_aliases_source_sku_idx
  ON marketplace_item_aliases (source, source_sku)
  WHERE source_sku <> '';

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_item_aliases_normalized_idx
  ON marketplace_item_aliases (source, normalized_key)
  WHERE source_sku = '' AND normalized_key <> '';

CREATE INDEX IF NOT EXISTS marketplace_item_aliases_normalized_lookup_idx
  ON marketplace_item_aliases (source, normalized_key);

CREATE INDEX IF NOT EXISTS marketplace_item_aliases_usage_idx
  ON marketplace_item_aliases (usage_count DESC, updated_at DESC);
