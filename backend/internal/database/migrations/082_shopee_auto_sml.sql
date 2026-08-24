-- 082_shopee_auto_sml.sql
-- Durable, per-shop Shopee READY_TO_SHIP -> SML automation. The global
-- environment flag and every shop setting default to disabled, so applying the
-- migration never sends an existing order or changes an existing bill.

CREATE TABLE IF NOT EXISTS shopee_auto_sml_settings (
  shop_id                     BIGINT PRIMARY KEY,
  enabled                     BOOLEAN NOT NULL DEFAULT false,
  eligible_after              TIMESTAMPTZ,
  route_signature             TEXT NOT NULL DEFAULT '',
  enabled_by                  UUID REFERENCES users(id) ON DELETE SET NULL,
  enabled_at                  TIMESTAMPTZ,
  paused_reason               TEXT NOT NULL DEFAULT '',
  paused_at                   TIMESTAMPTZ,
  consecutive_system_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_system_failures >= 0),
  last_success_at             TIMESTAMPTZ,
  last_failure_at             TIMESTAMPTZ,
  created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  FOREIGN KEY (shop_id) REFERENCES shopee_api_connections(shop_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS shopee_auto_sml_jobs (
  id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id                    BIGINT NOT NULL,
  order_sn                   TEXT NOT NULL,
  bill_id                    UUID REFERENCES bills(id) ON DELETE SET NULL,
  sml_doc_no                 TEXT NOT NULL DEFAULT '',
  status                     TEXT NOT NULL DEFAULT 'queued'
                             CHECK (status IN (
                               'queued','running','retry_wait','needs_review',
                               'succeeded','failed','cancelled'
                             )),
  attempts                   INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_run_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_until                TIMESTAMPTZ,
  order_create_time          TIMESTAMPTZ NOT NULL,
  order_update_time          TIMESTAMPTZ,
  bill_fingerprint           TEXT NOT NULL DEFAULT '',
  route_signature            TEXT NOT NULL DEFAULT '',
  last_error_code            TEXT NOT NULL DEFAULT '',
  last_error_message         TEXT NOT NULL DEFAULT '',
  started_at                 TIMESTAMPTZ,
  completed_at               TIMESTAMPTZ,
  created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, order_sn),
  FOREIGN KEY (shop_id) REFERENCES shopee_api_connections(shop_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS shopee_auto_sml_jobs_queue_idx
  ON shopee_auto_sml_jobs(next_run_at, created_at)
  WHERE status IN ('queued','retry_wait');

CREATE INDEX IF NOT EXISTS shopee_auto_sml_jobs_shop_history_idx
  ON shopee_auto_sml_jobs(shop_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS shopee_auto_sml_jobs_bill_idx
  ON shopee_auto_sml_jobs(bill_id)
  WHERE bill_id IS NOT NULL;

INSERT INTO shopee_auto_sml_settings (shop_id)
SELECT shop_id FROM shopee_api_connections
ON CONFLICT (shop_id) DO NOTHING;
