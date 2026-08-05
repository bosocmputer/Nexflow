-- 073_sales_only_no_ai.sql -- deterministic sales-only product resolution.
-- Historical AI usage is intentionally retained for audit.

ALTER TABLE sml_catalog
  ALTER COLUMN embedding_status SET DEFAULT 'disabled';

ALTER TABLE sml_catalog
  DROP CONSTRAINT IF EXISTS sml_catalog_embedding_status_check;

ALTER TABLE sml_catalog
  ADD CONSTRAINT sml_catalog_embedding_status_check
  CHECK (embedding_status IN ('disabled', 'pending', 'done', 'error'));

UPDATE sml_catalog
SET embedding_status = 'disabled',
    embedding = NULL,
    embedded_at = NULL,
    embedding_model = NULL
WHERE embedding_status <> 'disabled'
   OR embedding IS NOT NULL
   OR embedded_at IS NOT NULL
   OR embedding_model IS NOT NULL;

DELETE FROM daily_insights;
DELETE FROM app_settings
WHERE key LIKE 'ai.%'
   OR key IN ('automation.auto_confirm_threshold', 'automation.insight_cron');

ALTER TABLE mappings
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE mappings
  DROP CONSTRAINT IF EXISTS mappings_source_check;

UPDATE mappings
SET source = 'verified'
WHERE source = 'ai_learned';

ALTER TABLE mappings
  ADD CONSTRAINT mappings_source_check
  CHECK (source IN ('manual', 'verified'));

ALTER TABLE marketplace_item_aliases
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS sml_catalog_active_item_code_trgm_idx
  ON sml_catalog USING gin (item_code gin_trgm_ops)
  WHERE is_active = TRUE;

CREATE INDEX IF NOT EXISTS sml_catalog_active_item_name_trgm_idx
  ON sml_catalog USING gin (item_name gin_trgm_ops)
  WHERE is_active = TRUE;

CREATE INDEX IF NOT EXISTS sml_catalog_active_item_name2_trgm_idx
  ON sml_catalog USING gin (item_name2 gin_trgm_ops)
  WHERE is_active = TRUE;

CREATE INDEX IF NOT EXISTS marketplace_item_aliases_active_source_idx
  ON marketplace_item_aliases (source, updated_at DESC)
  WHERE is_active = TRUE;
