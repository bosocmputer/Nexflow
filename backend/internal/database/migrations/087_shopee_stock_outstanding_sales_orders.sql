-- 087_shopee_stock_outstanding_sales_orders.sql
-- Additive evidence for stock availability net of active SML sales orders.
-- This migration is replay-safe and intentionally performs no data backfill.

ALTER TABLE shopee_stock_mappings
  ADD COLUMN IF NOT EXISTS last_preview_sml_physical_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_sml_outstanding_so_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_sml_usable_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_calculation_usable_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS last_preview_availability_version TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_preview_source_snapshot_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_preview_source_fingerprint TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_preview_availability_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE shopee_stock_run_lines
  ADD COLUMN IF NOT EXISTS sml_physical_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS sml_outstanding_so_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS sml_usable_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS calculation_usable_qty NUMERIC,
  ADD COLUMN IF NOT EXISTS availability_version TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS source_snapshot_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS source_fingerprint TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS availability_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS marketplace_stock_representation_evidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reservation_id UUID NOT NULL REFERENCES marketplace_stock_reservations(id) ON DELETE RESTRICT,
  sml_attempt_id UUID NOT NULL REFERENCES bill_sml_attempts(id) ON DELETE RESTRICT,
  doc_no TEXT NOT NULL,
  route TEXT NOT NULL,
  warehouse_code TEXT NOT NULL,
  location_code TEXT NOT NULL,
  item_code TEXT NOT NULL,
  expected_base_qty NUMERIC NOT NULL CHECK (expected_base_qty > 0),
  evidence_group_id TEXT NOT NULL,
  document_scope_expected_base_qty NUMERIC NOT NULL CHECK (document_scope_expected_base_qty > 0),
  actual_base_qty NUMERIC,
  evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('sale_order_demand','stock_movement')),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','verified','mismatch','manual_reconciliation')),
  source_semantics_fingerprint TEXT NOT NULL DEFAULT '',
  evidence_hash TEXT NOT NULL DEFAULT '',
  verified_source_snapshot_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
  last_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (reservation_id, sml_attempt_id, warehouse_code, location_code, item_code, evidence_kind)
);

CREATE INDEX IF NOT EXISTS marketplace_stock_representation_evidence_attempt_idx
  ON marketplace_stock_representation_evidence(sml_attempt_id, status, item_code);

CREATE INDEX IF NOT EXISTS marketplace_stock_representation_evidence_resource_idx
  ON marketplace_stock_representation_evidence(warehouse_code, location_code, item_code, status);
