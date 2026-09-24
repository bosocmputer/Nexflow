-- Split TikTok Shop automation into two safe, explicit phases.
-- Existing `enabled` continues to mean automatic Nexflow Bill creation.
-- SML sending starts disabled for every shop so rollout cannot create a new
-- external accounting write without an explicit admin confirmation.

ALTER TABLE tiktok_shop_auto_sml_settings
  ADD COLUMN IF NOT EXISTS sml_send_enabled BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'tiktok_shop_auto_sml_jobs_status_check'
       AND conrelid = 'tiktok_shop_auto_sml_jobs'::regclass
  ) THEN
    ALTER TABLE tiktok_shop_auto_sml_jobs
      DROP CONSTRAINT tiktok_shop_auto_sml_jobs_status_check;
  END IF;
END $$;

ALTER TABLE tiktok_shop_auto_sml_jobs
  ADD CONSTRAINT tiktok_shop_auto_sml_jobs_status_check
  CHECK (status IN ('queued','running','retry_wait','bill_created','needs_review','succeeded','failed','cancelled'));
