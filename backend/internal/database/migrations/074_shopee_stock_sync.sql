-- 074_shopee_stock_sync.sql
-- SML warehouse stock -> Shopee synchronization. All shops start disabled and
-- require an explicit warehouse scope plus a successful dry-run before writes.

CREATE TABLE IF NOT EXISTS shopee_stock_settings (
  shop_id                       BIGINT PRIMARY KEY,
  enabled                       BOOLEAN NOT NULL DEFAULT false,
  stock_pct                     NUMERIC(5,2) NOT NULL DEFAULT 80
                                CHECK (stock_pct >= 1 AND stock_pct <= 100),
  interval_seconds              INTEGER NOT NULL DEFAULT 300
                                CHECK (interval_seconds BETWEEN 300 AND 86400),
  scope_mode                    TEXT NOT NULL DEFAULT 'unconfigured'
                                CHECK (scope_mode IN ('unconfigured','all','selected')),
  locations                     JSONB NOT NULL DEFAULT '[]'::jsonb,
  all_scope_warning_acknowledged BOOLEAN NOT NULL DEFAULT false,
  dry_run_required              BOOLEAN NOT NULL DEFAULT true,
  paused_reason                 TEXT NOT NULL DEFAULT '',
  last_catalog_sync_at          TIMESTAMPTZ,
  last_full_catalog_sync_at     TIMESTAMPTZ,
  last_catalog_attempt_at       TIMESTAMPTZ,
  last_preview_at               TIMESTAMPTZ,
  last_sync_at                  TIMESTAMPTZ,
  last_success_at               TIMESTAMPTZ,
  last_error                    TEXT NOT NULL DEFAULT '',
  last_summary                  JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_by                    UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  FOREIGN KEY (shop_id) REFERENCES shopee_api_connections(shop_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS shopee_stock_sml_catalog (
  item_code          TEXT PRIMARY KEY,
  item_name          TEXT NOT NULL DEFAULT '',
  standard_unit_code TEXT NOT NULL DEFAULT '',
  units              JSONB NOT NULL DEFAULT '[]'::jsonb,
  barcodes           JSONB NOT NULL DEFAULT '[]'::jsonb,
  source_updated_at  TIMESTAMPTZ,
  synced_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_active          BOOLEAN NOT NULL DEFAULT true
);

CREATE INDEX IF NOT EXISTS shopee_stock_sml_catalog_active_idx
  ON shopee_stock_sml_catalog(is_active, item_code);

CREATE TABLE IF NOT EXISTS shopee_stock_products (
  shop_id             BIGINT NOT NULL,
  item_id              BIGINT NOT NULL,
  model_id             BIGINT NOT NULL DEFAULT 0,
  item_name            TEXT NOT NULL DEFAULT '',
  model_name           TEXT NOT NULL DEFAULT '',
  item_sku             TEXT NOT NULL DEFAULT '',
  model_sku            TEXT NOT NULL DEFAULT '',
  item_status          TEXT NOT NULL DEFAULT '',
  model_status         TEXT NOT NULL DEFAULT '',
  shopee_available     BIGINT NOT NULL DEFAULT 0,
  shopee_reserved      BIGINT NOT NULL DEFAULT 0,
  seller_stock         JSONB NOT NULL DEFAULT '[]'::jsonb,
  product_updated_at   TIMESTAMPTZ,
  last_seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_active            BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (shop_id, item_id, model_id),
  FOREIGN KEY (shop_id) REFERENCES shopee_api_connections(shop_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS shopee_stock_products_shop_active_idx
  ON shopee_stock_products(shop_id, is_active, item_id, model_id);

CREATE TABLE IF NOT EXISTS shopee_stock_mappings (
  shop_id             BIGINT NOT NULL,
  item_id              BIGINT NOT NULL,
  model_id             BIGINT NOT NULL DEFAULT 0,
  sml_item_code        TEXT NOT NULL DEFAULT '',
  sml_unit_code        TEXT NOT NULL DEFAULT '',
  unit_factor          NUMERIC NOT NULL DEFAULT 0,
  manual_unit_factor   NUMERIC,
  match_source         TEXT NOT NULL DEFAULT ''
                       CHECK (match_source IN ('','sku','barcode','manual')),
  excluded             BOOLEAN NOT NULL DEFAULT false,
  warning_codes        JSONB NOT NULL DEFAULT '[]'::jsonb,
  last_preview_balance NUMERIC,
  last_preview_excluded_balance NUMERIC,
  last_preview_min_qty NUMERIC,
  last_preview_max_qty NUMERIC,
  last_preview_target  BIGINT,
  last_success_target  BIGINT,
  updated_by           UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (shop_id, item_id, model_id),
  FOREIGN KEY (shop_id, item_id, model_id)
    REFERENCES shopee_stock_products(shop_id, item_id, model_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS shopee_stock_mappings_sml_item_idx
  ON shopee_stock_mappings(sml_item_code)
  WHERE excluded = false AND sml_item_code <> '';

CREATE TABLE IF NOT EXISTS shopee_stock_runs (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id          BIGINT NOT NULL,
  run_type         TEXT NOT NULL CHECK (run_type IN ('catalog','preview','sync')),
  trigger_source   TEXT NOT NULL DEFAULT 'manual'
                   CHECK (trigger_source IN ('manual','scheduler')),
  status           TEXT NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running','success','warning','failed','paused')),
  as_of_date       DATE,
  total_count      INTEGER NOT NULL DEFAULT 0,
  changed_count    INTEGER NOT NULL DEFAULT 0,
  skipped_count    INTEGER NOT NULL DEFAULT 0,
  blocked_count    INTEGER NOT NULL DEFAULT 0,
  error_count      INTEGER NOT NULL DEFAULT 0,
  summary          JSONB NOT NULL DEFAULT '{}'::jsonb,
  error_message    TEXT NOT NULL DEFAULT '',
  started_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at      TIMESTAMPTZ,
  FOREIGN KEY (shop_id) REFERENCES shopee_api_connections(shop_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS shopee_stock_runs_shop_started_idx
  ON shopee_stock_runs(shop_id, started_at DESC);

CREATE TABLE IF NOT EXISTS shopee_stock_attempts (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id         UUID NOT NULL REFERENCES shopee_stock_runs(id) ON DELETE CASCADE,
  shop_id        BIGINT NOT NULL,
  item_id        BIGINT NOT NULL,
  model_id       BIGINT NOT NULL DEFAULT 0,
  sml_item_code  TEXT NOT NULL DEFAULT '',
  result         TEXT NOT NULL CHECK (result IN ('changed','blocked','error','unknown_result')),
  previous_stock BIGINT,
  target_stock   BIGINT,
  reason_code    TEXT NOT NULL DEFAULT '',
  message        TEXT NOT NULL DEFAULT '',
  request_id     TEXT NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shopee_stock_attempts_shop_created_idx
  ON shopee_stock_attempts(shop_id, created_at DESC);

CREATE TABLE IF NOT EXISTS shopee_stock_leases (
  shop_id       BIGINT PRIMARY KEY,
  owner_id      TEXT NOT NULL,
  lease_until   TIMESTAMPTZ NOT NULL,
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  FOREIGN KEY (shop_id) REFERENCES shopee_api_connections(shop_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS shopee_stock_catalog_lease (
  singleton     BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
  owner_id      TEXT NOT NULL,
  lease_until   TIMESTAMPTZ NOT NULL,
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Keep this migration safe to rerun after an interrupted rollout where an
-- earlier copy may already have created the tables.
ALTER TABLE shopee_stock_settings
  ADD COLUMN IF NOT EXISTS last_full_catalog_sync_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_catalog_attempt_at TIMESTAMPTZ;

ALTER TABLE shopee_stock_mappings
  ADD COLUMN IF NOT EXISTS manual_unit_factor NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_balance NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_excluded_balance NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_min_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_max_qty NUMERIC;

INSERT INTO shopee_stock_settings (shop_id)
SELECT shop_id FROM shopee_api_connections
ON CONFLICT (shop_id) DO NOTHING;

INSERT INTO user_menu_permissions (
  user_id, menu_key, can_view, can_create, can_update, can_delete
)
SELECT id, 'shopee_stock', role = 'admin', false, role = 'admin', false
FROM users
ON CONFLICT (user_id, menu_key) DO NOTHING;
