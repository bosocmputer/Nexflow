-- 085_marketplace_units_conversion.sql
-- Additive foundation for generation-based SML units, marketplace quantity
-- conversion, immutable SML attempts, durable reservations and async stock
-- previews. This migration intentionally contains no data backfill; workers do
-- bounded reconciliation after deploy while all feature flags remain disabled.

CREATE TABLE IF NOT EXISTS sml_catalog_sync_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  generation BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
  source_watermark TEXT NOT NULL DEFAULT '',
  source_cursor TEXT NOT NULL DEFAULT '',
  product_count BIGINT NOT NULL DEFAULT 0,
  unit_count BIGINT NOT NULL DEFAULT 0,
  barcode_count BIGINT NOT NULL DEFAULT 0,
  set_component_count BIGINT NOT NULL DEFAULT 0,
  product_hash TEXT NOT NULL DEFAULT '',
  unit_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'staging'
    CHECK (status IN ('staging','validating','active','failed','superseded')),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_fence BIGINT NOT NULL DEFAULT 0,
  lease_until TIMESTAMPTZ,
  sync_started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  activated_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS sml_catalog_sync_runs_active_uidx
  ON sml_catalog_sync_runs((status)) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS sml_catalog_sync_runs_status_idx
  ON sml_catalog_sync_runs(status, created_at DESC);

CREATE TABLE IF NOT EXISTS sml_catalog_units (
  generation_id UUID NOT NULL REFERENCES sml_catalog_sync_runs(id) ON DELETE RESTRICT,
  item_code TEXT NOT NULL,
  unit_code TEXT NOT NULL,
  unit_name TEXT NOT NULL DEFAULT '',
  stand_value NUMERIC NOT NULL CHECK (stand_value > 0),
  divide_value NUMERIC NOT NULL CHECK (divide_value > 0),
  is_default BOOLEAN NOT NULL DEFAULT false,
  unit_order INTEGER NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT true,
  source_updated_at TIMESTAMPTZ,
  synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (generation_id, item_code, unit_code)
);

CREATE INDEX IF NOT EXISTS sml_catalog_units_item_idx
  ON sml_catalog_units(item_code, is_active, unit_order, unit_code);

CREATE TABLE IF NOT EXISTS sml_catalog_barcodes (
  generation_id UUID NOT NULL REFERENCES sml_catalog_sync_runs(id) ON DELETE RESTRICT,
  item_code TEXT NOT NULL,
  unit_code TEXT NOT NULL DEFAULT '',
  barcode TEXT NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT true,
  synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (generation_id, item_code, barcode)
);

CREATE INDEX IF NOT EXISTS sml_catalog_barcodes_lookup_idx
  ON sml_catalog_barcodes(barcode, is_active);

CREATE TABLE IF NOT EXISTS sml_catalog_product_staging (
  generation_id UUID NOT NULL REFERENCES sml_catalog_sync_runs(id) ON DELETE CASCADE,
  item_code TEXT NOT NULL,
  item_name TEXT NOT NULL DEFAULT '',
  item_name2 TEXT NOT NULL DEFAULT '',
  unit_code TEXT NOT NULL DEFAULT '',
  group_code TEXT NOT NULL DEFAULT '',
  item_type INTEGER NOT NULL DEFAULT 0,
  source_updated_at TIMESTAMPTZ,
  payload_hash TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (generation_id, item_code)
);

CREATE TABLE IF NOT EXISTS sml_catalog_unit_staging (
  generation_id UUID NOT NULL REFERENCES sml_catalog_sync_runs(id) ON DELETE CASCADE,
  item_code TEXT NOT NULL,
  unit_code TEXT NOT NULL,
  unit_name TEXT NOT NULL DEFAULT '',
  stand_value NUMERIC NOT NULL,
  divide_value NUMERIC NOT NULL,
  is_default BOOLEAN NOT NULL DEFAULT false,
  unit_order INTEGER NOT NULL DEFAULT 0,
  source_updated_at TIMESTAMPTZ,
  PRIMARY KEY (generation_id, item_code, unit_code)
);

CREATE TABLE IF NOT EXISTS sml_catalog_barcode_staging (
  generation_id UUID NOT NULL REFERENCES sml_catalog_sync_runs(id) ON DELETE CASCADE,
  item_code TEXT NOT NULL,
  unit_code TEXT NOT NULL DEFAULT '',
  barcode TEXT NOT NULL,
  PRIMARY KEY (generation_id, item_code, barcode)
);

CREATE TABLE IF NOT EXISTS sml_catalog_set_component_staging (
  generation_id UUID NOT NULL REFERENCES sml_catalog_sync_runs(id) ON DELETE CASCADE,
  parent_item_code TEXT NOT NULL,
  line_number INTEGER NOT NULL DEFAULT 0,
  row_order INTEGER NOT NULL DEFAULT 0,
  component_item_code TEXT NOT NULL,
  component_item_name TEXT NOT NULL DEFAULT '',
  unit_code TEXT NOT NULL DEFAULT '',
  qty NUMERIC NOT NULL DEFAULT 0,
  unit_factor NUMERIC NOT NULL DEFAULT 0,
  definition_hash TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (generation_id, parent_item_code, line_number, row_order, component_item_code)
);

ALTER TABLE sml_catalog
  ADD COLUMN IF NOT EXISTS catalog_generation_id UUID REFERENCES sml_catalog_sync_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS metadata_updated_at TIMESTAMPTZ;

ALTER TABLE marketplace_item_aliases
  ADD COLUMN IF NOT EXISTS external_parent_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS parent_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS parent_key_kind TEXT NOT NULL DEFAULT 'derived',
  ADD COLUMN IF NOT EXISTS source_product_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS source_variant_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS mapping_revision BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS metadata_updated_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS quantity_multiplier BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS unit_stand_value NUMERIC,
  ADD COLUMN IF NOT EXISTS unit_divide_value NUMERIC,
  ADD COLUMN IF NOT EXISTS unit_catalog_generation UUID REFERENCES sml_catalog_sync_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS conversion_status TEXT NOT NULL DEFAULT 'needs_review',
  ADD COLUMN IF NOT EXISTS sales_enabled BOOLEAN NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS stock_policy TEXT NOT NULL DEFAULT 'blocked';

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'marketplace_alias_quantity_multiplier_check') THEN
    ALTER TABLE marketplace_item_aliases ADD CONSTRAINT marketplace_alias_quantity_multiplier_check
      CHECK (quantity_multiplier BETWEEN 1 AND 1000000);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'marketplace_alias_unit_ratio_check') THEN
    ALTER TABLE marketplace_item_aliases ADD CONSTRAINT marketplace_alias_unit_ratio_check
      CHECK ((unit_stand_value IS NULL AND unit_divide_value IS NULL)
        OR (unit_stand_value > 0 AND unit_divide_value > 0));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'marketplace_alias_conversion_status_check') THEN
    ALTER TABLE marketplace_item_aliases ADD CONSTRAINT marketplace_alias_conversion_status_check
      CHECK (conversion_status IN ('ready','needs_review','stale','blocked'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'marketplace_alias_stock_policy_check') THEN
    ALTER TABLE marketplace_item_aliases ADD CONSTRAINT marketplace_alias_stock_policy_check
      CHECK (stock_policy IN ('managed','zeroing','disabled_zero','manual_unmanaged','blocked'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'marketplace_alias_parent_key_kind_check') THEN
    ALTER TABLE marketplace_item_aliases ADD CONSTRAINT marketplace_alias_parent_key_kind_check
      CHECK (parent_key_kind IN ('external','derived'));
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS marketplace_alias_parent_page_idx
  ON marketplace_item_aliases(source, account_key, parent_key, external_variant_id, id)
  WHERE is_active = true;
CREATE INDEX IF NOT EXISTS marketplace_alias_conversion_idx
  ON marketplace_item_aliases(source, account_key, conversion_status, stock_policy, id)
  WHERE is_active = true;

ALTER TABLE bill_items
  ADD COLUMN IF NOT EXISTS source_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS sml_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS quantity_multiplier_snapshot BIGINT,
  ADD COLUMN IF NOT EXISTS unit_stand_value_snapshot NUMERIC,
  ADD COLUMN IF NOT EXISTS unit_divide_value_snapshot NUMERIC,
  ADD COLUMN IF NOT EXISTS base_qty_snapshot NUMERIC,
  ADD COLUMN IF NOT EXISTS mapping_revision_snapshot BIGINT,
  ADD COLUMN IF NOT EXISTS unit_catalog_generation_snapshot UUID REFERENCES sml_catalog_sync_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS set_definition_hash_snapshot TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS conversion_override_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS conversion_issue_code TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS bill_items_conversion_reconcile_idx
  ON bill_items(marketplace_alias_id, mapping_revision_snapshot, id)
  WHERE marketplace_alias_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS bill_sml_attempts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_key TEXT NOT NULL DEFAULT '',
  bill_id UUID NOT NULL REFERENCES bills(id) ON DELETE RESTRICT,
  doc_no TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'unattempted'
    CHECK (state IN ('unattempted','sending','unknown','sent','failed_exact_retry','stale_requires_reconciliation')),
  route TEXT NOT NULL,
  payload_bytes BYTEA NOT NULL,
  payload_json JSONB,
  payload_hash TEXT NOT NULL,
  route_settings JSONB NOT NULL DEFAULT '{}'::jsonb,
  mapping_revisions JSONB NOT NULL DEFAULT '{}'::jsonb,
  unit_catalog_generation UUID REFERENCES sml_catalog_sync_runs(id) ON DELETE SET NULL,
  set_definition_hashes JSONB NOT NULL DEFAULT '{}'::jsonb,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ,
  external_request_started_at TIMESTAMPTZ,
  external_request_finished_at TIMESTAMPTZ,
  response_bytes BYTEA,
  response_hash TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (bill_id, doc_no)
);

CREATE INDEX IF NOT EXISTS bill_sml_attempts_state_idx
  ON bill_sml_attempts(state, lease_until, created_at);

CREATE OR REPLACE FUNCTION prevent_bill_sml_attempt_payload_change()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.payload_bytes IS DISTINCT FROM OLD.payload_bytes
     OR NEW.payload_hash IS DISTINCT FROM OLD.payload_hash
     OR NEW.doc_no IS DISTINCT FROM OLD.doc_no
     OR NEW.route IS DISTINCT FROM OLD.route THEN
    RAISE EXCEPTION 'immutable SML attempt payload cannot be changed';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS bill_sml_attempt_payload_immutable ON bill_sml_attempts;
CREATE TRIGGER bill_sml_attempt_payload_immutable
BEFORE UPDATE ON bill_sml_attempts
FOR EACH ROW EXECUTE FUNCTION prevent_bill_sml_attempt_payload_change();

ALTER TABLE bills
  ADD COLUMN IF NOT EXISTS current_sml_attempt_id UUID REFERENCES bill_sml_attempts(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS sml_attempt_state TEXT NOT NULL DEFAULT 'unattempted';

CREATE TABLE IF NOT EXISTS marketplace_mapping_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_key TEXT NOT NULL DEFAULT '',
  alias_id UUID REFERENCES marketplace_item_aliases(id) ON DELETE SET NULL,
  target_revision BIGINT NOT NULL,
  job_type TEXT NOT NULL DEFAULT 'mapping_reconcile'
    CHECK (job_type IN ('mapping_reconcile','legacy_backfill','unit_stale_reconcile')),
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','running','completed','failed','cancelled')),
  impact_digest TEXT NOT NULL DEFAULT '',
  dependency_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  cursor_created_at TIMESTAMPTZ,
  cursor_id UUID,
  processed_count BIGINT NOT NULL DEFAULT 0,
  skipped_count BIGINT NOT NULL DEFAULT 0,
  failed_count BIGINT NOT NULL DEFAULT 0,
  result_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ,
  requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (alias_id, target_revision, job_type)
);

CREATE INDEX IF NOT EXISTS marketplace_mapping_jobs_claim_idx
  ON marketplace_mapping_jobs(status, lease_until, created_at);

CREATE TABLE IF NOT EXISTS marketplace_stock_reservations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_key TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL,
  account_key TEXT NOT NULL,
  order_id TEXT NOT NULL,
  source_line_id TEXT NOT NULL,
  external_item_id TEXT NOT NULL DEFAULT '',
  external_variant_id TEXT NOT NULL DEFAULT '',
  bill_id UUID REFERENCES bills(id) ON DELETE SET NULL,
  marketplace_alias_id UUID REFERENCES marketplace_item_aliases(id) ON DELETE SET NULL,
  mapping_revision BIGINT,
  source_qty NUMERIC NOT NULL,
  quantity_multiplier BIGINT NOT NULL,
  unit_code TEXT NOT NULL DEFAULT '',
  unit_stand_value NUMERIC,
  unit_divide_value NUMERIC,
  base_qty NUMERIC,
  sml_item_code TEXT NOT NULL DEFAULT '',
  warehouse_code TEXT NOT NULL DEFAULT '',
  location_code TEXT NOT NULL DEFAULT '',
  set_definition_hash TEXT NOT NULL DEFAULT '',
  source_event_version TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL
    CHECK (state IN ('active','blocked_mapping','sending_sml','awaiting_stock_recalc','incorporated_in_sml','released_cancelled','manual_reconciliation')),
  state_reason TEXT NOT NULL DEFAULT '',
  demand_revision BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  incorporated_at TIMESTAMPTZ,
  released_at TIMESTAMPTZ,
  UNIQUE (source, account_key, order_id, source_line_id, external_item_id, external_variant_id)
);

CREATE INDEX IF NOT EXISTS marketplace_stock_reservations_demand_idx
  ON marketplace_stock_reservations(warehouse_code, location_code, sml_item_code, state)
  WHERE state IN ('active','sending_sml','awaiting_stock_recalc');
CREATE INDEX IF NOT EXISTS marketplace_stock_reservations_alias_idx
  ON marketplace_stock_reservations(marketplace_alias_id, mapping_revision, state);

CREATE TABLE IF NOT EXISTS marketplace_stock_reservation_components (
  reservation_id UUID NOT NULL REFERENCES marketplace_stock_reservations(id) ON DELETE CASCADE,
  component_item_code TEXT NOT NULL,
  warehouse_code TEXT NOT NULL DEFAULT '',
  location_code TEXT NOT NULL DEFAULT '',
  component_base_qty NUMERIC NOT NULL CHECK (component_base_qty >= 0),
  set_definition_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (reservation_id, component_item_code, warehouse_code, location_code)
);

CREATE INDEX IF NOT EXISTS marketplace_stock_reservation_components_demand_idx
  ON marketplace_stock_reservation_components(warehouse_code, location_code, component_item_code);

CREATE TABLE IF NOT EXISTS marketplace_stock_demand_versions (
  warehouse_code TEXT NOT NULL,
  location_code TEXT NOT NULL,
  item_code TEXT NOT NULL,
  revision BIGINT NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (warehouse_code, location_code, item_code)
);

CREATE TABLE IF NOT EXISTS marketplace_stock_recalc_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_key TEXT NOT NULL DEFAULT '',
  bill_id UUID REFERENCES bills(id) ON DELETE SET NULL,
  sml_attempt_id UUID REFERENCES bill_sml_attempts(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','running','completed','failed','manual_reconciliation')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ,
  processstock_succeeded_at TIMESTAMPTZ,
  balance_verified_at TIMESTAMPTZ,
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (sml_attempt_id)
);

CREATE INDEX IF NOT EXISTS marketplace_stock_recalc_jobs_claim_idx
  ON marketplace_stock_recalc_jobs(status, next_attempt_at, lease_until);

ALTER TABLE shopee_stock_settings
  ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE shopee_stock_leases
  ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ;

ALTER TABLE shopee_stock_catalog_lease
  ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ;

ALTER TABLE shopee_stock_runs
  DROP CONSTRAINT IF EXISTS shopee_stock_runs_status_check;
ALTER TABLE shopee_stock_runs
  ADD CONSTRAINT shopee_stock_runs_status_check
  CHECK (status IN ('queued','running','success','warning','failed','paused','cancelled'));
ALTER TABLE shopee_stock_runs
  ADD COLUMN IF NOT EXISTS config_version BIGINT,
  ADD COLUMN IF NOT EXISTS catalog_generation_id UUID REFERENCES sml_catalog_sync_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS demand_revision_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS lease_fencing_token BIGINT,
  ADD COLUMN IF NOT EXISTS processed_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS progress_pct NUMERIC(5,2) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS shopee_stock_run_lines (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES shopee_stock_runs(id) ON DELETE CASCADE,
  shop_id BIGINT NOT NULL,
  item_id BIGINT NOT NULL,
  model_id BIGINT NOT NULL DEFAULT 0,
  parent_key TEXT NOT NULL DEFAULT '',
  product_name TEXT NOT NULL DEFAULT '',
  variant_name TEXT NOT NULL DEFAULT '',
  sml_item_code TEXT NOT NULL DEFAULT '',
  sml_unit_code TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL
    CHECK (status IN ('changed','unchanged','skipped','blocked','failed','stale_before_write')),
  previous_stock BIGINT,
  target_stock BIGINT,
  available_base_qty NUMERIC,
  pending_base_qty NUMERIC,
  reason_code TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  line_order BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (run_id, item_id, model_id)
);

CREATE INDEX IF NOT EXISTS shopee_stock_run_lines_page_idx
  ON shopee_stock_run_lines(run_id, status, line_order, id);

CREATE TABLE IF NOT EXISTS shopee_stock_policy_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_key TEXT NOT NULL DEFAULT '',
  shop_id BIGINT NOT NULL REFERENCES shopee_api_connections(shop_id) ON DELETE CASCADE,
  marketplace_alias_id UUID REFERENCES marketplace_item_aliases(id) ON DELETE SET NULL,
  policy_action TEXT NOT NULL CHECK (policy_action IN ('zero_then_disable')),
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','running','completed','failed','unknown')),
  idempotency_key TEXT NOT NULL UNIQUE,
  request_hash TEXT NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ,
  error_message TEXT NOT NULL DEFAULT '',
  requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS shopee_stock_policy_jobs_claim_idx
  ON shopee_stock_policy_jobs(status, next_attempt_at, lease_until);

CREATE TABLE IF NOT EXISTS marketplace_conversion_readiness (
  singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
  catalog_generation_ready BOOLEAN NOT NULL DEFAULT false,
  mapping_backfill_ready BOOLEAN NOT NULL DEFAULT false,
  reservation_ledger_ready BOOLEAN NOT NULL DEFAULT false,
  reconciliation_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE audit_logs
  ADD COLUMN IF NOT EXISTS tenant_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS revision BIGINT,
  ADD COLUMN IF NOT EXISTS job_id UUID,
  ADD COLUMN IF NOT EXISTS before_state JSONB,
  ADD COLUMN IF NOT EXISTS after_state JSONB;

CREATE INDEX IF NOT EXISTS audit_logs_job_idx
  ON audit_logs(job_id, created_at DESC) WHERE job_id IS NOT NULL;
