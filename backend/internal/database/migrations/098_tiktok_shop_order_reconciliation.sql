-- Bounded, read-only TikTok Shop order reconciliation state. No rows are
-- enabled or backfilled by this migration.

CREATE TABLE IF NOT EXISTS tiktok_shop_order_sync_settings (
  shop_id                TEXT PRIMARY KEY REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  enabled                BOOLEAN NOT NULL DEFAULT FALSE,
  interval_seconds       INTEGER NOT NULL DEFAULT 300 CHECK (interval_seconds BETWEEN 60 AND 86400),
  overlap_seconds        INTEGER NOT NULL DEFAULT 900 CHECK (overlap_seconds BETWEEN 60 AND 86400),
  watermark_update_at    TIMESTAMPTZ,
  next_run_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_success_at        TIMESTAMPTZ,
  last_error_code        TEXT NOT NULL DEFAULT '',
  last_error_message     TEXT NOT NULL DEFAULT '' CHECK (OCTET_LENGTH(last_error_message) <= 512),
  config_version         BIGINT NOT NULL DEFAULT 1 CHECK (config_version >= 1),
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tiktok_shop_order_sync_runs (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  gateway_connection_id  UUID NOT NULL REFERENCES tiktok_shop_connections(gateway_connection_id) ON DELETE RESTRICT,
  shop_id                TEXT NOT NULL REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  trigger_source         TEXT NOT NULL CHECK (trigger_source IN ('manual','schedule')),
  status                 TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','succeeded','failed')),
  window_start           TIMESTAMPTZ NOT NULL,
  window_end             TIMESTAMPTZ NOT NULL,
  page_size              INTEGER NOT NULL CHECK (page_size BETWEEN 1 AND 100),
  max_pages              INTEGER NOT NULL CHECK (max_pages BETWEEN 1 AND 100),
  page_count             INTEGER NOT NULL DEFAULT 0 CHECK (page_count >= 0 AND page_count <= max_pages),
  discovered_count       INTEGER NOT NULL DEFAULT 0 CHECK (discovered_count >= 0),
  snapshotted_count      INTEGER NOT NULL DEFAULT 0 CHECK (snapshotted_count >= 0 AND snapshotted_count <= discovered_count),
  last_page_token_hash   CHAR(64) CHECK (last_page_token_hash ~ '^[0-9a-f]{64}$'),
  search_request_ids     JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (JSONB_TYPEOF(search_request_ids) = 'array'),
  error_code             TEXT NOT NULL DEFAULT '',
  error_message          TEXT NOT NULL DEFAULT '' CHECK (OCTET_LENGTH(error_message) <= 512),
  lease_until            TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '30 minutes'),
  started_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at            TIMESTAMPTZ,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (window_start < window_end),
  CHECK (window_end - window_start <= INTERVAL '24 hours')
);

ALTER TABLE tiktok_shop_order_sync_runs
  ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '30 minutes');

CREATE UNIQUE INDEX IF NOT EXISTS tiktok_shop_order_sync_runs_one_running_idx
  ON tiktok_shop_order_sync_runs(shop_id)
  WHERE status = 'running';

CREATE INDEX IF NOT EXISTS tiktok_shop_order_sync_settings_due_idx
  ON tiktok_shop_order_sync_settings(next_run_at, shop_id)
  WHERE enabled = TRUE;

CREATE INDEX IF NOT EXISTS tiktok_shop_order_sync_runs_shop_created_idx
  ON tiktok_shop_order_sync_runs(shop_id, created_at DESC);

CREATE INDEX IF NOT EXISTS tiktok_shop_order_sync_runs_lease_idx
  ON tiktok_shop_order_sync_runs(lease_until)
  WHERE status = 'running';
