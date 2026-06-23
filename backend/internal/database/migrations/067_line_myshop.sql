-- 067_line_myshop.sql
-- LINE MyShop / LINE SHOPPING API order ingestion.

ALTER TABLE bills DROP CONSTRAINT IF EXISTS bills_source_check;
ALTER TABLE bills ADD CONSTRAINT bills_source_check
  CHECK (source IN (
    'line',
    'email',
    'lazada',
    'tiktok',
    'shopee',
    'shopee_email',
    'shopee_shipped',
    'manual',
    'line_myshop'
  ));

ALTER TABLE channel_defaults
  DROP CONSTRAINT IF EXISTS channel_defaults_channel_check;

ALTER TABLE channel_defaults
  ADD CONSTRAINT channel_defaults_channel_check
  CHECK (channel IN (
    'line',
    'email',
    'shopee',
    'shopee_realtime',
    'shopee_realtime_cancel',
    'shopee_email',
    'shopee_shipped',
    'lazada',
    'tiktok',
    'manual',
    'shopee_settlement',
    'line_myshop'
  ));

CREATE TABLE IF NOT EXISTS line_myshop_connections (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name           TEXT NOT NULL,
  api_key        TEXT NOT NULL DEFAULT '',
  webhook_secret TEXT NOT NULL DEFAULT '',
  channel_id     BIGINT,
  premium_id     TEXT NOT NULL DEFAULT '',
  random_id      TEXT NOT NULL DEFAULT '',
  enabled        BOOLEAN NOT NULL DEFAULT TRUE,
  last_sync_at   TIMESTAMPTZ,
  last_error     TEXT NOT NULL DEFAULT '',
  created_by     UUID,
  updated_by     UUID,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS line_myshop_connections_enabled_idx
  ON line_myshop_connections(enabled, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS line_myshop_connections_channel_id_unique
  ON line_myshop_connections(channel_id)
  WHERE channel_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS line_myshop_order_snapshots (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  connection_id     UUID NOT NULL REFERENCES line_myshop_connections(id) ON DELETE CASCADE,
  order_no          TEXT NOT NULL,
  order_status      TEXT NOT NULL DEFAULT '',
  payment_status    TEXT NOT NULL DEFAULT '',
  shipment_status   TEXT NOT NULL DEFAULT '',
  payment_method    TEXT NOT NULL DEFAULT '',
  total_amount      NUMERIC(14,2) NOT NULL DEFAULT 0,
  subtotal_price    NUMERIC(14,2) NOT NULL DEFAULT 0,
  shipment_price    NUMERIC(14,2) NOT NULL DEFAULT 0,
  discount_amount   NUMERIC(14,2) NOT NULL DEFAULT 0,
  item_count        INT NOT NULL DEFAULT 0,
  raw_detail        JSONB NOT NULL DEFAULT '{}'::jsonb,
  raw_webhook       JSONB NOT NULL DEFAULT '{}'::jsonb,
  bill_id           UUID REFERENCES bills(id) ON DELETE SET NULL,
  sml_doc_no        TEXT NOT NULL DEFAULT '',
  last_event_name   TEXT NOT NULL DEFAULT '',
  last_event_at     TIMESTAMPTZ,
  last_synced_at    TIMESTAMPTZ,
  last_error        TEXT NOT NULL DEFAULT '',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (connection_id, order_no)
);

CREATE INDEX IF NOT EXISTS line_myshop_order_snapshots_status_idx
  ON line_myshop_order_snapshots(connection_id, order_status, payment_status, shipment_status, updated_at DESC);

CREATE INDEX IF NOT EXISTS line_myshop_order_snapshots_bill_idx
  ON line_myshop_order_snapshots(bill_id)
  WHERE bill_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS line_myshop_webhook_events (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  connection_id     UUID NOT NULL REFERENCES line_myshop_connections(id) ON DELETE CASCADE,
  order_no          TEXT NOT NULL DEFAULT '',
  request_id        TEXT NOT NULL DEFAULT '',
  event_name        TEXT NOT NULL DEFAULT '',
  event_at          TIMESTAMPTZ,
  dedupe_key        TEXT NOT NULL,
  signature_valid   BOOLEAN NOT NULL DEFAULT FALSE,
  raw_payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
  processing_status TEXT NOT NULL DEFAULT 'received',
  error             TEXT NOT NULL DEFAULT '',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processed_at      TIMESTAMPTZ,
  UNIQUE (dedupe_key)
);

CREATE INDEX IF NOT EXISTS line_myshop_webhook_events_status_idx
  ON line_myshop_webhook_events(processing_status, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS bills_line_myshop_order_unique
  ON bills ((raw_data->>'line_myshop_connection_id'), (raw_data->>'line_myshop_order_no'))
  WHERE source = 'line_myshop'
    AND raw_data ? 'line_myshop_connection_id'
    AND raw_data ? 'line_myshop_order_no'
    AND COALESCE(raw_data->>'line_myshop_connection_id', '') <> ''
    AND COALESCE(raw_data->>'line_myshop_order_no', '') <> '';
