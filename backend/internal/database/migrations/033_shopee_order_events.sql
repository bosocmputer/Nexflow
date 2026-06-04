-- 033_shopee_order_events.sql
-- Store Shopee order status emails as a separate timeline from Nexflow/SML
-- bill.status. These events are informational for staff and do not affect
-- SML send eligibility.

CREATE TABLE IF NOT EXISTS shopee_order_events (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_id      UUID REFERENCES bills(id) ON DELETE SET NULL,
  order_id     TEXT NOT NULL DEFAULT '',
  event_type   TEXT NOT NULL,
  status_label TEXT NOT NULL,
  subject      TEXT NOT NULL DEFAULT '',
  from_addr    TEXT NOT NULL DEFAULT '',
  message_id   TEXT NOT NULL DEFAULT '',
  email_date   TIMESTAMPTZ,
  raw_data     JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(message_id, order_id, event_type)
);

CREATE INDEX IF NOT EXISTS idx_shopee_order_events_bill_id
  ON shopee_order_events(bill_id, COALESCE(email_date, created_at) DESC);

CREATE INDEX IF NOT EXISTS idx_shopee_order_events_order_id
  ON shopee_order_events(order_id, COALESCE(email_date, created_at) DESC);
