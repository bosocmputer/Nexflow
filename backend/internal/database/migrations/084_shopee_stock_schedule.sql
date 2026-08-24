-- Structured Shopee stock schedules. interval_seconds stays populated for API
-- compatibility, while calendar-month schedules use next_run_at in Bangkok time.

ALTER TABLE shopee_stock_settings
  DROP CONSTRAINT IF EXISTS shopee_stock_settings_interval_seconds_check;

ALTER TABLE shopee_stock_settings
  ADD CONSTRAINT shopee_stock_settings_interval_seconds_check
  CHECK (interval_seconds BETWEEN 300 AND 31536000);

ALTER TABLE shopee_stock_settings
  ADD COLUMN IF NOT EXISTS schedule_mode TEXT NOT NULL DEFAULT 'interval',
  ADD COLUMN IF NOT EXISTS monthly_interval INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS monthly_day INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS monthly_time TEXT NOT NULL DEFAULT '00:00',
  ADD COLUMN IF NOT EXISTS schedule_risk_acknowledged BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_stock_settings_schedule_mode_check'
  ) THEN
    ALTER TABLE shopee_stock_settings
      ADD CONSTRAINT shopee_stock_settings_schedule_mode_check
      CHECK (schedule_mode IN ('interval','monthly'));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_stock_settings_monthly_interval_check'
  ) THEN
    ALTER TABLE shopee_stock_settings
      ADD CONSTRAINT shopee_stock_settings_monthly_interval_check
      CHECK (monthly_interval BETWEEN 1 AND 12);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_stock_settings_monthly_day_check'
  ) THEN
    ALTER TABLE shopee_stock_settings
      ADD CONSTRAINT shopee_stock_settings_monthly_day_check
      CHECK (monthly_day BETWEEN 1 AND 28);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'shopee_stock_settings_monthly_time_check'
  ) THEN
    ALTER TABLE shopee_stock_settings
      ADD CONSTRAINT shopee_stock_settings_monthly_time_check
      CHECK (monthly_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$');
  END IF;
END $$;

UPDATE shopee_stock_settings
   SET schedule_risk_acknowledged = true
 WHERE schedule_mode = 'interval'
   AND interval_seconds >= 86400
   AND schedule_risk_acknowledged = false;

UPDATE shopee_stock_settings
   SET next_run_at = COALESCE(last_sync_at, NOW()) + make_interval(secs => interval_seconds)
 WHERE next_run_at IS NULL;

CREATE INDEX IF NOT EXISTS shopee_stock_settings_due_idx
  ON shopee_stock_settings(next_run_at, shop_id)
  WHERE enabled = true AND dry_run_required = false AND paused_reason = '';
