-- 077_unified_marketplace_product_master.sql
-- One marketplace -> SML product master shared by imports, bills and Shopee stock.
-- The migration is additive and safe to rerun. Historical sales documents are
-- linked only when the match is unambiguous; their product/unit values are not changed.

ALTER TABLE marketplace_item_aliases
  ADD COLUMN IF NOT EXISTS account_key TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS external_item_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS external_variant_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS match_method TEXT NOT NULL DEFAULT 'manual_name',
  ADD COLUMN IF NOT EXISTS scope_confirmed BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE marketplace_item_aliases
  DROP CONSTRAINT IF EXISTS marketplace_item_aliases_match_method_check;

ALTER TABLE marketplace_item_aliases
  ADD CONSTRAINT marketplace_item_aliases_match_method_check
  CHECK (match_method IN ('exact_sku','manual_identity','manual_sku','manual_name','legacy'));

ALTER TABLE bills
  ADD COLUMN IF NOT EXISTS source_account_key TEXT NOT NULL DEFAULT 'default';

ALTER TABLE bill_items
  ADD COLUMN IF NOT EXISTS source_item_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS source_variant_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS marketplace_alias_id UUID REFERENCES marketplace_item_aliases(id) ON DELETE SET NULL;

ALTER TABLE shopee_stock_mappings
  ADD COLUMN IF NOT EXISTS marketplace_alias_id UUID REFERENCES marketplace_item_aliases(id) ON DELETE SET NULL;

UPDATE bills
SET source_account_key = CASE
  WHEN source = 'shopee' AND COALESCE(raw_data->>'shopee_shop_id', '') <> ''
    THEN 'shop:' || (raw_data->>'shopee_shop_id')
  ELSE 'default'
END
WHERE source_account_key = '' OR source_account_key = 'default';

-- Historical Shopee payload shapes varied. When this tenant has exactly one
-- active shop, that shop is the only safe scope for remaining unscoped bills.
WITH only_shop AS (
  SELECT CASE WHEN COUNT(*) = 1 THEN MIN(shop_id)::text ELSE '' END AS shop_id
  FROM shopee_api_connections
  WHERE disabled_at IS NULL
)
UPDATE bills b
SET source_account_key = 'shop:' || only_shop.shop_id
FROM only_shop
WHERE b.source = 'shopee'
  AND b.source_account_key = 'default'
  AND only_shop.shop_id <> '';

-- Existing Shopee aliases can be scoped safely only when this tenant has one shop.
WITH only_shop AS (
  SELECT CASE WHEN COUNT(*) = 1 THEN MIN(shop_id)::text ELSE '' END AS shop_id
  FROM shopee_api_connections
  WHERE disabled_at IS NULL
)
UPDATE marketplace_item_aliases a
SET account_key = 'shop:' || only_shop.shop_id,
    scope_confirmed = TRUE,
    match_method = CASE WHEN a.source_sku <> '' THEN 'manual_sku' ELSE 'manual_name' END
FROM only_shop
WHERE a.source = 'shopee'
  AND a.account_key = 'default'
  AND only_shop.shop_id <> '';

-- Shopee aliases that still have no proven shop scope remain available for
-- review, but the active resolver must not reuse them across shops.
UPDATE marketplace_item_aliases
SET scope_confirmed = FALSE,
    match_method = 'legacy'
WHERE source = 'shopee'
  AND account_key = 'default';

UPDATE marketplace_item_aliases
SET match_method = CASE WHEN source_sku <> '' THEN 'manual_sku' ELSE 'manual_name' END
WHERE match_method = 'manual_name' AND source_sku <> '';

-- The old uniqueness was global per platform. Replace it with account-scoped
-- identities so two Shopee shops may reuse the same seller SKU safely.
DROP INDEX IF EXISTS marketplace_item_aliases_source_sku_idx;
DROP INDEX IF EXISTS marketplace_item_aliases_normalized_idx;

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_alias_identity_uidx
  ON marketplace_item_aliases(source, account_key, external_item_id, external_variant_id)
  WHERE external_item_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_alias_scoped_sku_uidx
  ON marketplace_item_aliases(source, account_key, source_sku)
  WHERE source_sku <> '';

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_alias_scoped_name_uidx
  ON marketplace_item_aliases(source, account_key, normalized_key)
  WHERE source_sku = '' AND external_item_id = '' AND normalized_key <> '';

CREATE INDEX IF NOT EXISTS marketplace_alias_lookup_idx
  ON marketplace_item_aliases(source, account_key, is_active, updated_at DESC);

CREATE INDEX IF NOT EXISTS marketplace_alias_external_lookup_idx
  ON marketplace_item_aliases(source, account_key, external_item_id, external_variant_id)
  WHERE is_active = TRUE AND external_item_id <> '';

CREATE INDEX IF NOT EXISTS bills_source_account_open_idx
  ON bills(source, source_account_key, status)
  WHERE archived_at IS NULL AND status IN ('pending','needs_review');

CREATE INDEX IF NOT EXISTS bill_items_marketplace_identity_idx
  ON bill_items(source_item_id, source_variant_id)
  WHERE source_item_id <> '';

CREATE INDEX IF NOT EXISTS bill_items_marketplace_alias_idx
  ON bill_items(marketplace_alias_id)
  WHERE marketplace_alias_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS shopee_stock_mappings_alias_idx
  ON shopee_stock_mappings(marketplace_alias_id)
  WHERE marketplace_alias_id IS NOT NULL;

-- Promote existing stock mappings (including AOY AH-0006) into the canonical
-- product master. Stock unit/factor remain in shopee_stock_mappings; the master
-- stores the SML standard unit for sales resolution.
INSERT INTO marketplace_item_aliases (
  source, account_key, external_item_id, external_variant_id,
  source_sku, raw_name, normalized_key, item_code, unit_code,
  confidence, usage_count, match_method, scope_confirmed, is_active
)
SELECT
  'shopee',
  'shop:' || p.shop_id::text,
  p.item_id::text,
  p.model_id::text,
  COALESCE(NULLIF(p.model_sku, ''), NULLIF(p.item_sku, ''), ''),
  btrim(concat_ws(' / ', NULLIF(p.item_name, ''), NULLIF(p.model_name, ''))),
  btrim(regexp_replace(replace(concat_ws(' / ', NULLIF(p.item_name, ''), NULLIF(p.model_name, '')), chr(65279), ''), '\s+', ' ', 'g')),
  m.sml_item_code,
  COALESCE(NULLIF(c.standard_unit_code, ''), m.sml_unit_code),
  1.0,
  0,
  CASE WHEN COALESCE(NULLIF(p.model_sku, ''), NULLIF(p.item_sku, ''), '') = m.sml_item_code
    THEN 'exact_sku' ELSE 'manual_identity' END,
  TRUE,
  TRUE
FROM shopee_stock_mappings m
JOIN shopee_stock_products p USING (shop_id, item_id, model_id)
LEFT JOIN shopee_stock_sml_catalog c ON c.item_code = m.sml_item_code AND c.is_active = TRUE
WHERE m.excluded = FALSE AND m.sml_item_code <> ''
ON CONFLICT DO NOTHING;

-- If a scoped SKU/name Master already existed before stock sync was added,
-- promote that row to stable Shopee identity only when both sides point to
-- the same SML item. Conflicting targets remain unlinked for admin review.
WITH promotion_candidates AS (
  SELECT
    a.id AS alias_id,
    MIN(p.item_id)::text AS item_id,
    MIN(p.model_id)::text AS model_id,
    MIN(btrim(concat_ws(' / ', NULLIF(p.item_name, ''), NULLIF(p.model_name, '')))) AS raw_name
  FROM marketplace_item_aliases a
  JOIN shopee_stock_mappings m
    ON a.source = 'shopee'
   AND a.account_key = 'shop:' || m.shop_id::text
   AND a.external_item_id = ''
   AND a.item_code = m.sml_item_code
  JOIN shopee_stock_products p USING (shop_id, item_id, model_id)
  WHERE m.excluded = FALSE
    AND m.sml_item_code <> ''
    AND (
      (COALESCE(NULLIF(p.model_sku, ''), NULLIF(p.item_sku, ''), '') <> ''
        AND a.source_sku = COALESCE(NULLIF(p.model_sku, ''), NULLIF(p.item_sku, ''), ''))
      OR
      (COALESCE(NULLIF(p.model_sku, ''), NULLIF(p.item_sku, ''), '') = ''
        AND a.source_sku = ''
        AND a.normalized_key = btrim(regexp_replace(replace(concat_ws(' / ', NULLIF(p.item_name, ''), NULLIF(p.model_name, '')), chr(65279), ''), '\s+', ' ', 'g')))
    )
    AND NOT EXISTS (
      SELECT 1 FROM marketplace_item_aliases identity_alias
       WHERE identity_alias.source = 'shopee'
         AND identity_alias.account_key = 'shop:' || m.shop_id::text
         AND identity_alias.external_item_id = p.item_id::text
         AND identity_alias.external_variant_id = p.model_id::text
         AND identity_alias.id <> a.id
    )
  GROUP BY a.id
  HAVING COUNT(*) = 1
)
UPDATE marketplace_item_aliases a
SET external_item_id = candidate.item_id,
    external_variant_id = candidate.model_id,
    raw_name = candidate.raw_name,
    normalized_key = btrim(regexp_replace(replace(candidate.raw_name, chr(65279), ''), '\s+', ' ', 'g')),
    match_method = CASE WHEN a.source_sku = a.item_code THEN 'exact_sku' ELSE 'manual_identity' END,
    scope_confirmed = TRUE,
    updated_at = NOW()
FROM promotion_candidates candidate
WHERE a.id = candidate.alias_id;

UPDATE shopee_stock_mappings m
SET marketplace_alias_id = a.id
FROM marketplace_item_aliases a
WHERE a.source = 'shopee'
  AND a.account_key = 'shop:' || m.shop_id::text
  AND a.external_item_id = m.item_id::text
  AND a.external_variant_id = m.model_id::text
  AND a.is_active = TRUE
  AND m.marketplace_alias_id IS DISTINCT FROM a.id;

-- Link historical open and sent items only when one active master matches.
-- No item_code/unit/status is modified here.
WITH candidate AS (
  SELECT bi.id AS bill_item_id, MIN(a.id::text)::uuid AS alias_id
  FROM bill_items bi
  JOIN bills b ON b.id = bi.bill_id
  JOIN marketplace_item_aliases a
    ON a.source = b.source
   AND a.account_key = b.source_account_key
   AND a.is_active = TRUE
   AND (
     (bi.source_item_id <> '' AND a.external_item_id = bi.source_item_id
       AND a.external_variant_id = bi.source_variant_id)
     OR
     (bi.source_item_id = '' AND bi.source_sku <> '' AND a.source_sku = bi.source_sku)
   )
  GROUP BY bi.id
  HAVING COUNT(*) = 1
)
UPDATE bill_items bi
SET marketplace_alias_id = candidate.alias_id
FROM candidate
WHERE bi.id = candidate.bill_item_id
  AND bi.marketplace_alias_id IS NULL;
