-- 023_processed_email_keys.sql — durable email dedup tombstones
--
-- bills.raw_data is still the primary source for active bill dedup, but UAT
-- often needs a clean bills/logs queue while old emails remain in the mailbox
-- lookback window. This table records email/order keys that have already been
-- processed so clearing test bills does not cause old emails to be imported
-- again.

CREATE TABLE IF NOT EXISTS processed_email_keys (
  source      TEXT NOT NULL,
  message_id  TEXT NOT NULL,
  order_id    TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (source, message_id, order_id)
);

CREATE INDEX IF NOT EXISTS idx_processed_email_keys_message
  ON processed_email_keys(message_id);
