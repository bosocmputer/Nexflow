-- Tenant-scoped TikTok Shop Auto SML controls and durable work queue.
-- Every shop starts disabled. Enabling records a cutoff so existing orders are
-- never backfilled, and the unique shop/order key prevents duplicate creates.

CREATE TABLE IF NOT EXISTS tiktok_shop_auto_sml_settings (
  shop_id                     TEXT PRIMARY KEY REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  enabled                     BOOLEAN NOT NULL DEFAULT FALSE,
  trigger_status              TEXT NOT NULL DEFAULT 'AWAITING_COLLECTION'
                              CHECK (trigger_status = 'AWAITING_COLLECTION'),
  config_version              BIGINT NOT NULL DEFAULT 1 CHECK (config_version >= 1),
  eligible_after              TIMESTAMPTZ,
  route_signature             CHAR(64) NOT NULL DEFAULT ''
                              CHECK (route_signature = '' OR route_signature ~ '^[0-9a-f]{64}$'),
  enabled_by                  UUID REFERENCES users(id) ON DELETE SET NULL,
  enabled_at                  TIMESTAMPTZ,
  paused_reason               TEXT NOT NULL DEFAULT '',
  paused_at                   TIMESTAMPTZ,
  consecutive_system_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_system_failures >= 0),
  last_success_at             TIMESTAMPTZ,
  last_failure_at             TIMESTAMPTZ,
  created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tiktok_shop_auto_sml_jobs (
  id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id                    TEXT NOT NULL REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  order_id                   TEXT NOT NULL CHECK (BTRIM(order_id) <> ''),
  status                     TEXT NOT NULL DEFAULT 'queued'
                             CHECK (status IN ('queued','running','retry_wait','needs_review','succeeded','failed','cancelled')),
  trigger_status_snapshot    TEXT NOT NULL CHECK (trigger_status_snapshot = 'AWAITING_COLLECTION'),
  trigger_transition_at      TIMESTAMPTZ NOT NULL,
  trigger_config_version     BIGINT NOT NULL CHECK (trigger_config_version >= 1),
  source_hash                CHAR(64) NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
  bill_fingerprint           CHAR(64) NOT NULL CHECK (bill_fingerprint ~ '^[0-9a-f]{64}$'),
  route_signature            CHAR(64) NOT NULL CHECK (route_signature ~ '^[0-9a-f]{64}$'),
  bill_id                    UUID REFERENCES bills(id) ON DELETE RESTRICT,
  sml_doc_no                 TEXT NOT NULL DEFAULT '',
  review_digest              CHAR(64) NOT NULL DEFAULT ''
                             CHECK (review_digest = '' OR review_digest ~ '^[0-9a-f]{64}$'),
  document_time              TEXT NOT NULL DEFAULT '' CHECK (document_time = '' OR document_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
  attempts                   INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 8),
  next_run_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_until                TIMESTAMPTZ,
  last_error_code            TEXT NOT NULL DEFAULT '',
  last_error_message         TEXT NOT NULL DEFAULT '' CHECK (OCTET_LENGTH(last_error_message) <= 800),
  started_at                 TIMESTAMPTZ,
  completed_at               TIMESTAMPTZ,
  created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, order_id)
);

CREATE INDEX IF NOT EXISTS tiktok_shop_auto_sml_jobs_due_idx
  ON tiktok_shop_auto_sml_jobs(next_run_at, created_at)
  WHERE status IN ('queued','retry_wait');

CREATE INDEX IF NOT EXISTS tiktok_shop_auto_sml_jobs_shop_status_idx
  ON tiktok_shop_auto_sml_jobs(shop_id, status, updated_at DESC);

INSERT INTO tiktok_shop_auto_sml_settings (shop_id)
SELECT shop_id FROM tiktok_shop_connections
ON CONFLICT (shop_id) DO NOTHING;
