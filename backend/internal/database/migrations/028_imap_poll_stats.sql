-- 028_imap_poll_stats.sql
-- Store the visible poll summary separately from last_poll_messages.
-- last_poll_messages is kept for compatibility and remains "processed".

ALTER TABLE imap_accounts
  ADD COLUMN IF NOT EXISTS last_poll_found INT,
  ADD COLUMN IF NOT EXISTS last_poll_processed INT,
  ADD COLUMN IF NOT EXISTS last_poll_skipped INT;

UPDATE imap_accounts
SET last_poll_processed = COALESCE(last_poll_processed, last_poll_messages)
WHERE last_poll_processed IS NULL
  AND last_poll_messages IS NOT NULL;
