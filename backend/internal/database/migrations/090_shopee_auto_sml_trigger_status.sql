-- 090_shopee_auto_sml_trigger_status.sql
-- Adds a versioned per-shop trigger and immutable trigger evidence to queued
-- Auto SML jobs. READY_TO_SHIP preserves the behavior of existing tenants and
-- this additive migration does not enqueue or backfill historical orders.

ALTER TABLE shopee_auto_sml_settings
  ADD COLUMN IF NOT EXISTS trigger_status TEXT NOT NULL DEFAULT 'READY_TO_SHIP';

ALTER TABLE shopee_auto_sml_settings
  ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE shopee_auto_sml_jobs
  ADD COLUMN IF NOT EXISTS trigger_status_snapshot TEXT NOT NULL DEFAULT 'READY_TO_SHIP';

ALTER TABLE shopee_auto_sml_jobs
  ADD COLUMN IF NOT EXISTS trigger_transition_at TIMESTAMPTZ;

ALTER TABLE shopee_auto_sml_jobs
  ADD COLUMN IF NOT EXISTS trigger_config_version BIGINT NOT NULL DEFAULT 1;

-- A pre-090 job could only exist after the READY_TO_SHIP cutoff check had
-- already passed. Preserve that durable evidence across a binary rollback by
-- snapshotting the best timestamp the old job stored. This update is bounded
-- to jobs that may still be processed or retried. The partial index keeps
-- startup migration replay from scanning the whole history after backfill.
CREATE INDEX IF NOT EXISTS shopee_auto_sml_jobs_missing_trigger_evidence_idx
  ON shopee_auto_sml_jobs(status)
  WHERE trigger_transition_at IS NULL
    AND status IN ('queued','running','retry_wait','needs_review','failed');

UPDATE shopee_auto_sml_jobs
   SET trigger_transition_at = COALESCE(order_update_time, created_at)
 WHERE trigger_transition_at IS NULL
   AND status IN ('queued','running','retry_wait','needs_review','failed');

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_auto_sml_settings_trigger_status_check'
       AND conrelid = 'shopee_auto_sml_settings'::regclass
  ) THEN
    ALTER TABLE shopee_auto_sml_settings
      ADD CONSTRAINT shopee_auto_sml_settings_trigger_status_check
      CHECK (trigger_status IN ('READY_TO_SHIP','PROCESSED'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_auto_sml_settings_config_version_check'
       AND conrelid = 'shopee_auto_sml_settings'::regclass
  ) THEN
    ALTER TABLE shopee_auto_sml_settings
      ADD CONSTRAINT shopee_auto_sml_settings_config_version_check
      CHECK (config_version > 0);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_auto_sml_jobs_trigger_status_check'
       AND conrelid = 'shopee_auto_sml_jobs'::regclass
  ) THEN
    ALTER TABLE shopee_auto_sml_jobs
      ADD CONSTRAINT shopee_auto_sml_jobs_trigger_status_check
      CHECK (trigger_status_snapshot IN ('READY_TO_SHIP','PROCESSED'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_auto_sml_jobs_trigger_config_version_check'
       AND conrelid = 'shopee_auto_sml_jobs'::regclass
  ) THEN
    ALTER TABLE shopee_auto_sml_jobs
      ADD CONSTRAINT shopee_auto_sml_jobs_trigger_config_version_check
      CHECK (trigger_config_version > 0);
  END IF;
END $$;
