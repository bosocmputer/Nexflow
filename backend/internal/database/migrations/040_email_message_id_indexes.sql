-- 040_email_message_id_indexes.sql
-- Speed up IMAP duplicate pre-checks against old read+unread messages.

CREATE INDEX IF NOT EXISTS idx_bills_raw_email_message_id
  ON bills ((raw_data->>'email_message_id'))
  WHERE raw_data ? 'email_message_id';

CREATE INDEX IF NOT EXISTS idx_bills_raw_message_id
  ON bills ((raw_data->>'message_id'))
  WHERE raw_data ? 'message_id';
