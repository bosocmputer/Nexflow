-- 039_imap_poll_summary.sql
-- Store structured latest-poll counts so /settings/email can explain
-- duplicate-only polls without treating them as errors.

ALTER TABLE imap_accounts
  ADD COLUMN IF NOT EXISTS last_poll_summary JSONB NOT NULL DEFAULT '{}'::jsonb;
