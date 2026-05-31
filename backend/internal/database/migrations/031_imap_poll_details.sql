-- 031_imap_poll_details.sql
-- Keep a small user-readable summary of the latest IMAP poll per account.
-- This powers /settings/email so admins can see which messages were found,
-- processed, or skipped and why.

ALTER TABLE imap_accounts
  ADD COLUMN IF NOT EXISTS last_poll_details JSONB NOT NULL DEFAULT '[]'::jsonb;
