-- 022_channel_defaults_document_settings.sql — move document defaults from
-- instance-level config into per-channel SML defaults.

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS branch_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sale_code   TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS unit_code   TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS doc_time    TEXT NOT NULL DEFAULT '';
