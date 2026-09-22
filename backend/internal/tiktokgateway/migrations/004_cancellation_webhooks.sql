-- Keep cancellation status notifications separate from order-status webhooks.
-- Type 11 contains cancellation evidence, while the tenant worker always
-- refreshes the exact order before any local workflow can act on it.

ALTER TABLE webhook_events
  ADD COLUMN IF NOT EXISTS cancellation_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cancellation_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cancellations_role TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cancellation_created_at TIMESTAMPTZ;

ALTER TABLE webhook_events
  DROP CONSTRAINT IF EXISTS webhook_events_notification_type_check,
  ADD CONSTRAINT webhook_events_notification_type_check
    CHECK (notification_type IN (1, 11));

ALTER TABLE webhook_events
  DROP CONSTRAINT IF EXISTS webhook_events_order_status_check,
  ADD CONSTRAINT webhook_events_order_status_check
    CHECK (
      (notification_type = 1 AND BTRIM(order_status) <> '')
      OR (notification_type = 11 AND BTRIM(order_status) = '')
    );

ALTER TABLE webhook_events
  DROP CONSTRAINT IF EXISTS webhook_events_cancellation_event_check,
  ADD CONSTRAINT webhook_events_cancellation_event_check
    CHECK (
      (notification_type = 1
        AND cancellation_status = ''
        AND cancellation_id = ''
        AND cancellations_role = ''
        AND cancellation_created_at IS NULL)
      OR (notification_type = 11
        AND cancellation_status IN (
          'CANCELLATION_REQUEST_PENDING',
          'CANCELLATION_REQUEST_SUCCESS',
          'CANCELLATION_REQUEST_CANCELLED',
          'CANCELLATION_REQUEST_COMPLETE'
        )
        AND BTRIM(cancellation_id) <> ''
        AND cancellations_role IN ('BUYER', 'SELLER', 'SYSTEM')
        AND cancellation_created_at IS NOT NULL)
    );
