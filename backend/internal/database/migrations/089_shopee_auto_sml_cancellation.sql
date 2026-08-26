-- 089_shopee_auto_sml_cancellation.sql
-- Durable Shopee CANCELLED -> SML cancellation queue. All existing rows remain
-- manual history; the new automation is separately gated by runtime config.

ALTER TABLE shopee_sml_cancellations
  ADD COLUMN IF NOT EXISTS trigger_source TEXT NOT NULL DEFAULT 'manual',
  ADD COLUMN IF NOT EXISTS request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS route_endpoint TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS route_signature TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS error_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS stock_recalc_status TEXT NOT NULL DEFAULT 'not_required',
  ADD COLUMN IF NOT EXISTS stock_recalc_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS stock_recalc_attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS stock_recalc_next_run_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS stock_recalc_lease_until TIMESTAMPTZ;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_sml_cancellations_status_check'
       AND pg_get_constraintdef(oid) NOT LIKE '%superseded%'
  ) THEN
    ALTER TABLE shopee_sml_cancellations
      DROP CONSTRAINT shopee_sml_cancellations_status_check;
    ALTER TABLE shopee_sml_cancellations
      ADD CONSTRAINT shopee_sml_cancellations_status_check
      CHECK (status IN ('pending','previewed','creating','created','already_exists','failed','blocked','superseded'));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_sml_cancellations_status_check'
  ) THEN
    ALTER TABLE shopee_sml_cancellations
      ADD CONSTRAINT shopee_sml_cancellations_status_check
      CHECK (status IN ('pending','previewed','creating','created','already_exists','failed','blocked','superseded'));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_sml_cancellations_trigger_source_check'
  ) THEN
    ALTER TABLE shopee_sml_cancellations
      ADD CONSTRAINT shopee_sml_cancellations_trigger_source_check
      CHECK (trigger_source IN ('manual','auto'));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_sml_cancellations_attempts_check'
  ) THEN
    ALTER TABLE shopee_sml_cancellations
      ADD CONSTRAINT shopee_sml_cancellations_attempts_check
      CHECK (attempts >= 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_sml_cancellations_stock_recalc_status_check'
  ) THEN
    ALTER TABLE shopee_sml_cancellations
      ADD CONSTRAINT shopee_sml_cancellations_stock_recalc_status_check
      CHECK (stock_recalc_status IN ('not_required','pending','running','succeeded','failed','manual_reconciliation'));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_sml_cancellations_stock_recalc_attempts_check'
  ) THEN
    ALTER TABLE shopee_sml_cancellations
      ADD CONSTRAINT shopee_sml_cancellations_stock_recalc_attempts_check
      CHECK (stock_recalc_attempts >= 0);
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS shopee_sml_cancellations_auto_unique_idx
  ON shopee_sml_cancellations(shop_id, order_sn, sale_sml_doc_no)
  WHERE trigger_source = 'auto';

CREATE INDEX IF NOT EXISTS shopee_sml_cancellations_auto_queue_idx
  ON shopee_sml_cancellations(next_run_at, created_at)
  WHERE trigger_source = 'auto' AND status = 'pending';

CREATE INDEX IF NOT EXISTS shopee_sml_cancellations_auto_lease_idx
  ON shopee_sml_cancellations(lease_until)
  WHERE trigger_source = 'auto' AND status = 'creating';

CREATE INDEX IF NOT EXISTS shopee_sml_cancellations_stock_recalc_queue_idx
  ON shopee_sml_cancellations(stock_recalc_next_run_at, created_at)
  WHERE stock_recalc_status IN ('pending','failed');

CREATE UNIQUE INDEX IF NOT EXISTS shopee_sml_cancellations_stock_recalc_owner_idx
  ON shopee_sml_cancellations(shop_id, order_sn, sale_sml_doc_no)
  WHERE stock_recalc_status IN ('pending','running','succeeded','failed','manual_reconciliation');
