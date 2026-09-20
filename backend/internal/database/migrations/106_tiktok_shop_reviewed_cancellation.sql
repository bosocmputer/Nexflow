-- 106_tiktok_shop_reviewed_cancellation.sql
-- Durable, reviewed TikTok Shop cancellation attempts. The runtime feature
-- gate remains disabled by default and this migration does not backfill work.

CREATE TABLE IF NOT EXISTS tiktok_shop_sml_cancellations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id TEXT NOT NULL,
  order_id TEXT NOT NULL,
  bill_id UUID NOT NULL REFERENCES bills(id) ON DELETE RESTRICT,
  sml_attempt_id UUID NOT NULL REFERENCES bill_sml_attempts(id) ON DELETE RESTRICT,
  sale_sml_doc_no TEXT NOT NULL,
  cancel_sml_doc_no TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'previewed'
    CHECK (status IN ('previewed','creating','created','already_exists','failed','unknown','blocked')),
  source_hash TEXT NOT NULL,
  review_digest TEXT NOT NULL,
  route_endpoint TEXT NOT NULL,
  route_config_version BIGINT NOT NULL,
  route_signature TEXT NOT NULL,
  request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  response JSONB NOT NULL DEFAULT '{}'::jsonb,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  stock_recalc_status TEXT NOT NULL DEFAULT 'not_required'
    CHECK (stock_recalc_status IN ('not_required','pending','running','succeeded','failed','manual_reconciliation')),
  stock_recalc_error TEXT NOT NULL DEFAULT '',
  stock_recalc_attempts INTEGER NOT NULL DEFAULT 0 CHECK (stock_recalc_attempts >= 0),
  stock_recalc_next_run_at TIMESTAMPTZ,
  stock_recalc_lease_until TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, order_id, sml_attempt_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS tiktok_shop_sml_cancellations_success_doc_idx
  ON tiktok_shop_sml_cancellations(cancel_sml_doc_no)
  WHERE status IN ('created','already_exists') AND cancel_sml_doc_no <> '';

CREATE INDEX IF NOT EXISTS tiktok_shop_sml_cancellations_order_idx
  ON tiktok_shop_sml_cancellations(shop_id, order_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS tiktok_shop_sml_cancellations_stock_queue_idx
  ON tiktok_shop_sml_cancellations(stock_recalc_next_run_at, created_at)
  WHERE stock_recalc_status IN ('pending','failed');
