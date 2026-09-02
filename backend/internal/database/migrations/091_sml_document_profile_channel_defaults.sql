-- 091_sml_document_profile_channel_defaults.sql
-- Additive configuration authority for SML Document Profile V1. Existing
-- remark_2 sentinel values (tax/notax/re/empty) remain byte-for-byte intact.

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS remark VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE channel_defaults
  ALTER COLUMN remark_2 TYPE VARCHAR(255);

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_constraint
     WHERE conname = 'channel_defaults_config_version_check'
       AND conrelid = 'channel_defaults'::regclass
  ) THEN
    ALTER TABLE channel_defaults
      ADD CONSTRAINT channel_defaults_config_version_check
      CHECK (config_version > 0);
  END IF;
END $$;
