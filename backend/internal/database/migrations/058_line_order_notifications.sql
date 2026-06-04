-- 058_line_order_notifications.sql — LINE Push recipients and outbox for
-- Shopee Realtime new-order alerts.
--
-- Additive only. This does not enable LINE chat/inbox and does not change
-- Shopee import, SML send, ERP action, or shipping behavior.

CREATE TABLE IF NOT EXISTS line_notification_recipients (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  line_oa_id        UUID NOT NULL REFERENCES line_oa_accounts(id) ON DELETE CASCADE,
  name              TEXT NOT NULL,
  destination_type  TEXT NOT NULL DEFAULT 'user'
                    CHECK (destination_type IN ('user','group','room')),
  destination_id    TEXT NOT NULL,
  enabled           BOOLEAN NOT NULL DEFAULT TRUE,
  last_test_at      TIMESTAMPTZ,
  last_test_status  TEXT NOT NULL DEFAULT '',
  last_test_error   TEXT NOT NULL DEFAULT '',
  last_sent_at      TIMESTAMPTZ,
  last_error        TEXT NOT NULL DEFAULT '',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (line_oa_id, destination_id)
);

CREATE INDEX IF NOT EXISTS line_notification_recipients_oa_idx
  ON line_notification_recipients(line_oa_id, enabled);

CREATE TABLE IF NOT EXISTS line_notification_deliveries (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  recipient_id UUID NOT NULL REFERENCES line_notification_recipients(id) ON DELETE CASCADE,
  line_oa_id   UUID NOT NULL REFERENCES line_oa_accounts(id) ON DELETE CASCADE,
  source       TEXT NOT NULL DEFAULT 'shopee_realtime',
  severity     TEXT NOT NULL DEFAULT 'info'
               CHECK (severity IN ('info','warning','error')),
  title        TEXT NOT NULL,
  body         TEXT NOT NULL DEFAULT '',
  action_url   TEXT NOT NULL DEFAULT '',
  entity_type  TEXT NOT NULL DEFAULT '',
  entity_id    TEXT NOT NULL DEFAULT '',
  dedupe_key   TEXT NOT NULL,
  message_text TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'queued'
               CHECK (status IN ('queued','sending','sent','failed')),
  attempts     INT NOT NULL DEFAULT 0,
  last_error   TEXT NOT NULL DEFAULT '',
  next_run_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  sent_at      TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (recipient_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS line_notification_deliveries_queue_idx
  ON line_notification_deliveries(status, next_run_at, created_at);

CREATE INDEX IF NOT EXISTS line_notification_deliveries_entity_idx
  ON line_notification_deliveries(source, entity_type, entity_id, created_at DESC);
