-- 032_imap_poll_progress.sql
-- Track IMAP backlog progress so large inboxes are processed in small,
-- resumable batches instead of re-reading the same lookback window forever.

ALTER TABLE imap_accounts
  ADD COLUMN IF NOT EXISTS last_seen_uid BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_poll_limited BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS last_poll_backlog INT;
