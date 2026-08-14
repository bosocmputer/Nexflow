-- 079_tiktok_sales_invoice_correctness.sql
-- Preserve marketplace gross line totals and make TikTok amount review durable.
-- Existing bills remain compatible: NULL gross_amount means qty * price.

ALTER TABLE bill_items
  ADD COLUMN IF NOT EXISTS gross_amount NUMERIC(14,2);

ALTER TABLE bills
  ADD COLUMN IF NOT EXISTS amount_reviewed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS amount_reviewed_by UUID REFERENCES users(id),
  ADD COLUMN IF NOT EXISTS amount_review_fingerprint TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS import_run_artifacts (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  import_run_id UUID NOT NULL REFERENCES import_runs(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,
  filename      TEXT NOT NULL,
  content_type  TEXT,
  size_bytes    BIGINT NOT NULL,
  sha256        TEXT,
  storage_path  TEXT NOT NULL,
  source_meta   JSONB,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (import_run_id)
);

CREATE INDEX IF NOT EXISTS import_run_artifacts_run_idx
  ON import_run_artifacts(import_run_id);

-- TikTok Seller SKU may be blank while SKU ID remains a stable variant ID.
-- Keep this identity separate from external_item_id so legacy AOY aliases can
-- continue resolving without pretending SKU ID is an SML item code.
CREATE UNIQUE INDEX IF NOT EXISTS marketplace_alias_tiktok_variant_uidx
  ON marketplace_item_aliases(source, account_key, external_variant_id)
  WHERE source = 'tiktok'
    AND external_item_id = ''
    AND external_variant_id <> '';
