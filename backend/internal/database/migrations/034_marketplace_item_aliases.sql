-- Marketplace item aliases map platform-specific SKU/name/variant keys to SML items.
-- They let staff confirm a product once in Nexflow instead of editing 1000+
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

-- Do not recreate the original platform-wide unique indexes here. Migration
-- 077 replaces them with account-scoped identities and intentionally drops
-- their legacy names. Because every migration is replayed on startup, trying
-- to recreate these indexes before 077 would prevent a valid multi-account
-- dataset from restarting when two accounts reuse the same SKU or name.

CREATE INDEX IF NOT EXISTS marketplace_item_aliases_normalized_lookup_idx
  ON marketplace_item_aliases (source, normalized_key);

CREATE INDEX IF NOT EXISTS marketplace_item_aliases_usage_idx
  ON marketplace_item_aliases (usage_count DESC, updated_at DESC);
