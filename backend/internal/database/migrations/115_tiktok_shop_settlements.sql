-- TikTok Shop Finance Statement -> verified, operator-confirmed SML AR receipt.
-- All new switches start fail-closed; this migration deliberately has no data
-- migration from Shopee routes or finance rows.

ALTER TABLE channel_defaults DROP CONSTRAINT IF EXISTS channel_defaults_channel_check;
ALTER TABLE channel_defaults ADD CONSTRAINT channel_defaults_channel_check
  CHECK (channel IN ('line','email','shopee','shopee_realtime','shopee_realtime_cancel','shopee_email','shopee_shipped','lazada','tiktok','tiktok_shop','tiktok_shop_cancel','manual','shopee_settlement','tiktok_settlement','line_myshop'));

CREATE TABLE IF NOT EXISTS tiktok_shop_settlement_settings (
  shop_id TEXT PRIMARY KEY REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  read_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  sml_send_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  config_version INT NOT NULL DEFAULT 1 CHECK (config_version > 0),
  last_successful_preflight_at TIMESTAMPTZ,
  last_import_at TIMESTAMPTZ,
  updated_by UUID REFERENCES users(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tiktok_settlement_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  connection_id UUID NOT NULL REFERENCES tiktok_shop_connections(gateway_connection_id),
  shop_id TEXT NOT NULL,
  shop_label TEXT NOT NULL DEFAULT '',
  statement_id TEXT NOT NULL,
  payment_id TEXT NOT NULL DEFAULT '',
  payment_status TEXT NOT NULL DEFAULT '',
  currency TEXT NOT NULL DEFAULT 'THB',
  payment_time TIMESTAMPTZ,
  statement_time TIMESTAMPTZ,
  total_settlement_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  invoice_amount_total NUMERIC(16,2) NOT NULL DEFAULT 0,
  fee_amount_total NUMERIC(16,2) NOT NULL DEFAULT 0,
  shipping_amount_total NUMERIC(16,2) NOT NULL DEFAULT 0,
  adjustment_amount_total NUMERIC(16,2) NOT NULL DEFAULT 0,
  refund_amount_total NUMERIC(16,2) NOT NULL DEFAULT 0,
  reserve_amount_total NUMERIC(16,2) NOT NULL DEFAULT 0,
  content_hash TEXT NOT NULL DEFAULT '',
  upstream_request_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'importing' CHECK (status IN ('importing','reconciling','ready','needs_review','sending','sent','failed','unknown_result','superseded')),
  config_version INT NOT NULL DEFAULT 1,
  route_config_version BIGINT NOT NULL DEFAULT 0,
  rc_doc_no TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL DEFAULT '',
  error_msg TEXT NOT NULL DEFAULT '',
  anomaly_reason TEXT NOT NULL DEFAULT '',
  doc_date_override DATE,
  doc_date_override_reason TEXT NOT NULL DEFAULT '',
  created_by UUID REFERENCES users(id),
  created_by_email TEXT NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, statement_id)
);

CREATE INDEX IF NOT EXISTS tiktok_settlement_runs_connection_status_idx
  ON tiktok_settlement_runs(connection_id, status, payment_time DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS tiktok_settlement_runs_statement_idx
  ON tiktok_settlement_runs(statement_id);

CREATE TABLE IF NOT EXISTS tiktok_settlement_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES tiktok_settlement_runs(id) ON DELETE CASCADE,
  order_id TEXT NOT NULL,
  sml_invoice_doc_no TEXT NOT NULL DEFAULT '',
  customer_code TEXT NOT NULL DEFAULT '',
  invoice_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  settlement_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  fee_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  tax_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  shipping_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  adjustment_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  refund_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  reserve_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'THB',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ready','blocked','sending','sent','failed')),
  block_reason TEXT NOT NULL DEFAULT '',
  receipt_doc_no TEXT NOT NULL DEFAULT '',
  snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (run_id, order_id)
);
CREATE INDEX IF NOT EXISTS tiktok_settlement_items_order_idx ON tiktok_settlement_items(order_id);
CREATE INDEX IF NOT EXISTS tiktok_settlement_items_run_idx ON tiktok_settlement_items(run_id, status);

INSERT INTO user_menu_permissions (user_id, menu_key, can_view, can_create, can_update, can_delete)
SELECT u.id, 'tiktok_settlements', u.role IN ('admin','staff'), u.role IN ('admin','staff'), u.role IN ('admin','staff'), false
FROM users u
ON CONFLICT (user_id, menu_key) DO NOTHING;
