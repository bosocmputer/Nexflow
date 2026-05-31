-- 021_instance_settings.sql — DB-backed per-deploy instance config.
-- BillFlow is still deployed as one isolated instance per customer, but these
-- settings let admins/support change customer-specific integration values from
-- the UI instead of editing scattered .env keys by hand. Runtime services still
-- use boot-time config, so SML/AI changes apply after backend restart.

CREATE TABLE IF NOT EXISTS app_settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  is_secret  BOOLEAN NOT NULL DEFAULT FALSE,
  updated_by UUID REFERENCES users(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS app_settings_updated_at_idx ON app_settings(updated_at DESC);
