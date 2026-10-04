-- 120_line_notification_recipient_preferences.sql — recipient-level LINE
-- notification subscriptions and stable event identity.
--
-- Existing recipients keep their previous behaviour by subscribing to every
-- currently supported event. New recipients receive the application default
-- preset when they are created. Disabled queued work is retained as
-- `suppressed` for auditability and is never leased by the worker.

ALTER TABLE line_notification_deliveries
  ADD COLUMN IF NOT EXISTS event_key TEXT NOT NULL DEFAULT 'legacy.unclassified';

UPDATE line_notification_deliveries
   SET event_key = CASE
     WHEN dedupe_key LIKE 'shopee:new_order:%' THEN 'shopee.order.new'
     WHEN dedupe_key LIKE 'tiktok_shop:new_order:%' THEN 'tiktok.order.new'
     WHEN dedupe_key LIKE 'nextstep:new_order:%' THEN 'nextstep.order.new'
     WHEN dedupe_key LIKE 'shopee:cancelled:%'
       OR dedupe_key LIKE 'shopee:cancelled_after_sml:%' THEN 'shopee.order.cancelled'
     WHEN dedupe_key LIKE 'tiktok_shop:cancelled:%' THEN 'tiktok.order.cancelled'
     WHEN dedupe_key LIKE 'shopee:auto_sml:success:%' THEN 'shopee.sml.auto_success'
     WHEN dedupe_key LIKE 'tiktok_shop:auto_sml:success:%' THEN 'tiktok.sml.auto_success'
     WHEN dedupe_key LIKE 'shopee:auto_sml:review:%' THEN 'shopee.sml.needs_review'
     WHEN dedupe_key LIKE 'tiktok_shop:auto_sml:review:%' THEN 'tiktok.sml.needs_review'
     WHEN dedupe_key LIKE 'shopee:auto_sml:failure:%' THEN 'shopee.sml.failed'
     WHEN dedupe_key LIKE 'tiktok_shop:auto_sml:failure:%' THEN 'tiktok.sml.failed'
     WHEN dedupe_key LIKE 'shopee:sml_cancel_created:%' THEN 'shopee.sml.cancellation_created'
     WHEN dedupe_key LIKE 'shopee:settlement:%' THEN 'shopee.settlement.ready'
     ELSE event_key
   END
 WHERE event_key = 'legacy.unclassified';

ALTER TABLE line_notification_deliveries
  DROP CONSTRAINT IF EXISTS line_notification_deliveries_status_check;

ALTER TABLE line_notification_deliveries
  ADD CONSTRAINT line_notification_deliveries_status_check
  CHECK (status IN ('queued','sending','sent','failed','suppressed'));

CREATE TABLE IF NOT EXISTS line_notification_recipient_subscriptions (
  recipient_id UUID NOT NULL REFERENCES line_notification_recipients(id) ON DELETE CASCADE,
  event_key    TEXT NOT NULL,
  enabled      BOOLEAN NOT NULL DEFAULT TRUE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (recipient_id, event_key)
);

CREATE INDEX IF NOT EXISTS line_notification_recipient_subscriptions_event_idx
  ON line_notification_recipient_subscriptions(event_key, recipient_id)
  WHERE enabled = TRUE;

INSERT INTO line_notification_recipient_subscriptions (recipient_id, event_key, enabled)
SELECT r.id, event_key, TRUE
  FROM line_notification_recipients r
 CROSS JOIN (VALUES
   ('shopee.order.new'),
   ('tiktok.order.new'),
   ('nextstep.order.new'),
   ('shopee.order.cancelled'),
   ('tiktok.order.cancelled'),
   ('shopee.sml.auto_success'),
   ('tiktok.sml.auto_success'),
   ('shopee.sml.needs_review'),
   ('tiktok.sml.needs_review'),
   ('shopee.sml.failed'),
   ('tiktok.sml.failed'),
   ('shopee.sml.cancellation_created'),
   ('shopee.settlement.ready')
 ) AS events(event_key)
ON CONFLICT (recipient_id, event_key) DO NOTHING;

UPDATE line_notification_deliveries
   SET status = 'suppressed',
       last_error = 'ไม่สามารถจัดประเภทการแจ้งเตือนเดิมได้ ระบบระงับคิวเพื่อความปลอดภัย',
       updated_at = NOW()
 WHERE event_key = 'legacy.unclassified'
   AND status IN ('queued','failed');

CREATE INDEX IF NOT EXISTS line_notification_deliveries_event_status_idx
  ON line_notification_deliveries(event_key, status, next_run_at, created_at);
