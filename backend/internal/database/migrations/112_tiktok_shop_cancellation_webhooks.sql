-- Durable tenant-side receipt for TikTok Shop type-11 cancellation status
-- notifications. The worker reconciles the exact order only; it never creates
-- an SML cancellation document from a webhook receipt by itself.

ALTER TABLE tiktok_shop_webhook_events
  ADD COLUMN IF NOT EXISTS notification_type INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS cancellation_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cancellation_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cancellation_role TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cancellation_created_at TIMESTAMPTZ;

ALTER TABLE tiktok_shop_webhook_events
  DROP CONSTRAINT IF EXISTS tiktok_shop_webhook_events_event_order_status_check,
  ADD CONSTRAINT tiktok_shop_webhook_events_event_order_status_check
    CHECK (
      (notification_type = 1 AND BTRIM(event_order_status) <> '')
      OR (notification_type = 11 AND BTRIM(event_order_status) = '')
    );

ALTER TABLE tiktok_shop_webhook_events
  DROP CONSTRAINT IF EXISTS tiktok_shop_webhook_events_notification_type_check,
  ADD CONSTRAINT tiktok_shop_webhook_events_notification_type_check
    CHECK (notification_type IN (1, 11));

ALTER TABLE tiktok_shop_webhook_events
  DROP CONSTRAINT IF EXISTS tiktok_shop_webhook_events_cancellation_event_check,
  ADD CONSTRAINT tiktok_shop_webhook_events_cancellation_event_check
    CHECK (
      (notification_type = 1
        AND cancellation_status = ''
        AND cancellation_id = ''
        AND cancellation_role = ''
        AND cancellation_created_at IS NULL)
      OR (notification_type = 11
        AND cancellation_status IN (
          'CANCELLATION_REQUEST_PENDING',
          'CANCELLATION_REQUEST_SUCCESS',
          'CANCELLATION_REQUEST_CANCELLED',
          'CANCELLATION_REQUEST_COMPLETE'
        )
        AND BTRIM(cancellation_id) <> ''
        AND cancellation_role IN ('BUYER', 'SELLER', 'SYSTEM')
        AND cancellation_created_at IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS tiktok_shop_webhook_events_cancellation_idx
  ON tiktok_shop_webhook_events(shop_id, cancellation_status, received_at DESC)
  WHERE notification_type = 11;
