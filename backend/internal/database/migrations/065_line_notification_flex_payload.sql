-- 065_line_notification_flex_payload.sql — structured LINE Flex outbox payloads.
--
-- Additive only. Existing text-only deliveries keep working through
-- message_text fallback; workers must not query Shopee/SML while pushing LINE.

ALTER TABLE line_notification_deliveries
  ADD COLUMN IF NOT EXISTS alt_text TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS flex_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS payload_version INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS line_notification_deliveries_payload_version_idx
  ON line_notification_deliveries(source, payload_version, created_at DESC)
  WHERE payload_version > 0;
