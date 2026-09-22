-- 108_marketplace_stock_control.sql
-- A tenant-scoped, platform-neutral stock-control foundation. This migration
-- deliberately creates drafts only; no legacy Shopee configuration is enabled
-- or migrated into a live pool.

CREATE TABLE IF NOT EXISTS marketplace_stock_settings (
  singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
  warehouse_code TEXT NOT NULL DEFAULT '',
  location_code TEXT NOT NULL DEFAULT '',
  default_buffer_pct NUMERIC(5,2) NOT NULL DEFAULT 10
    CHECK (default_buffer_pct >= 0 AND default_buffer_pct <= 100),
  kill_switch_enabled BOOLEAN NOT NULL DEFAULT false,
  config_version BIGINT NOT NULL DEFAULT 1 CHECK (config_version >= 1),
  updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS marketplace_stock_pools (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sml_item_code TEXT NOT NULL,
  sml_unit_code TEXT NOT NULL,
  allocation_mode TEXT NOT NULL DEFAULT 'quota'
    CHECK (allocation_mode IN ('quota','shared')),
  buffer_pct_override NUMERIC(5,2) CHECK (buffer_pct_override >= 0 AND buffer_pct_override <= 100),
  shared_risk_acknowledged BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft','ready','paused','active')),
  auto_enabled BOOLEAN NOT NULL DEFAULT false,
  dry_run_required BOOLEAN NOT NULL DEFAULT true,
  paused_reason TEXT NOT NULL DEFAULT '',
  config_version BIGINT NOT NULL DEFAULT 1 CHECK (config_version >= 1),
  last_preview_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (sml_item_code, sml_unit_code)
);

CREATE TABLE IF NOT EXISTS marketplace_stock_pool_members (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pool_id UUID NOT NULL REFERENCES marketplace_stock_pools(id) ON DELETE CASCADE,
  source TEXT NOT NULL CHECK (source IN ('shopee','tiktok')),
  account_key TEXT NOT NULL,
  external_product_id TEXT NOT NULL,
  external_sku_id TEXT NOT NULL,
  marketplace_alias_id UUID REFERENCES marketplace_item_aliases(id) ON DELETE RESTRICT,
  product_name TEXT NOT NULL DEFAULT '',
  variant_name TEXT NOT NULL DEFAULT '',
  unit_factor NUMERIC NOT NULL CHECK (unit_factor > 0),
  allocation_pct NUMERIC(5,2) NOT NULL DEFAULT 0 CHECK (allocation_pct >= 0 AND allocation_pct <= 100),
  enabled BOOLEAN NOT NULL DEFAULT true,
  last_catalog_seen_at TIMESTAMPTZ,
  last_target_qty BIGINT,
  last_actual_qty BIGINT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (source, account_key, external_product_id, external_sku_id),
  UNIQUE (pool_id, source, account_key, external_product_id, external_sku_id)
);

CREATE INDEX IF NOT EXISTS marketplace_stock_pool_members_pool_idx
  ON marketplace_stock_pool_members(pool_id, source, enabled, id);

CREATE TABLE IF NOT EXISTS marketplace_stock_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pool_id UUID NOT NULL REFERENCES marketplace_stock_pools(id) ON DELETE RESTRICT,
  trigger_source TEXT NOT NULL CHECK (trigger_source IN ('manual','schedule','event','nightly')),
  run_type TEXT NOT NULL CHECK (run_type IN ('preview','sync','reconcile')),
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','running','success','warning','failed','paused','cancelled')),
  config_version BIGINT NOT NULL,
  demand_revision_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  plan_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  plan_expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '60 seconds'),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_fencing_token BIGINT NOT NULL DEFAULT 0,
  lease_until TIMESTAMPTZ,
  total_count INTEGER NOT NULL DEFAULT 0,
  changed_count INTEGER NOT NULL DEFAULT 0,
  blocked_count INTEGER NOT NULL DEFAULT 0,
  error_count INTEGER NOT NULL DEFAULT 0,
  summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  error_message TEXT NOT NULL DEFAULT '',
  requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS marketplace_stock_runs_one_live_pool_idx
  ON marketplace_stock_runs(pool_id)
  WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS marketplace_stock_runs_due_idx
  ON marketplace_stock_runs(status, created_at)
  WHERE status IN ('queued','running');

CREATE TABLE IF NOT EXISTS marketplace_stock_run_lines (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES marketplace_stock_runs(id) ON DELETE CASCADE,
  member_id UUID NOT NULL REFERENCES marketplace_stock_pool_members(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK (status IN ('changed','unchanged','blocked','failed','stale_before_write')),
  previous_qty BIGINT,
  target_qty BIGINT,
  actual_qty BIGINT,
  request_hash CHAR(64) NOT NULL DEFAULT '',
  response_hash CHAR(64) NOT NULL DEFAULT '',
  upstream_request_id TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (run_id, member_id)
);

CREATE TABLE IF NOT EXISTS marketplace_stock_pool_leases (
  pool_id UUID PRIMARY KEY REFERENCES marketplace_stock_pools(id) ON DELETE CASCADE,
  owner_id TEXT NOT NULL,
  fencing_token BIGINT NOT NULL DEFAULT 0,
  lease_until TIMESTAMPTZ NOT NULL,
  heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Admin changes stock policy. Staff may run non-external previews/manual work
-- when explicitly granted create permission; viewers remain read-only.
INSERT INTO user_menu_permissions (user_id, menu_key, can_view, can_create, can_update, can_delete)
SELECT id, 'marketplace_stock', true, role IN ('admin','staff'), role = 'admin', false
FROM users
ON CONFLICT (user_id, menu_key) DO NOTHING;
