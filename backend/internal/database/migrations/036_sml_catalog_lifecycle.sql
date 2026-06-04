-- 036_sml_catalog_lifecycle.sql — soft lifecycle for SML catalog rows
--
-- SML product master currently exposes only a full-list API. Nexflow therefore
-- keeps local rows for audit/history, but marks products that disappear from a
-- full SML sync as inactive instead of hard-deleting them.

ALTER TABLE sml_catalog
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS missing_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Backfill last_seen_at from the previous sync timestamp for existing rows so
-- the first lifecycle-aware sync can safely detect rows that were not seen.
UPDATE sml_catalog
SET last_seen_at = COALESCE(synced_at, created_at, NOW())
WHERE last_seen_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_sml_catalog_is_active ON sml_catalog(is_active);
CREATE INDEX IF NOT EXISTS idx_sml_catalog_missing_at ON sml_catalog(missing_at) WHERE is_active = FALSE;
