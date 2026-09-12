-- Durable read-only TikTok Shop order state built only from typed safe fields.
-- This table is not a Bill/SML queue and has no automatic worker behavior.

CREATE TABLE IF NOT EXISTS tiktok_shop_order_snapshots (
  id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  gateway_connection_id UUID NOT NULL REFERENCES tiktok_shop_connections(gateway_connection_id),
  shop_id                  TEXT NOT NULL CHECK (BTRIM(shop_id) <> ''),
  order_id                 TEXT NOT NULL CHECK (BTRIM(order_id) <> ''),
  order_status             TEXT NOT NULL CHECK (BTRIM(order_status) <> ''),
  currency                 TEXT NOT NULL CHECK (BTRIM(currency) <> ''),
  payment_total_amount     NUMERIC(24,6) NOT NULL DEFAULT 0,
  product_subtotal_amount  NUMERIC(24,6) NOT NULL DEFAULT 0,
  shipping_fee_amount      NUMERIC(24,6) NOT NULL DEFAULT 0,
  item_insurance_fee_amount NUMERIC(24,6) NOT NULL DEFAULT 0,
  item_count               INTEGER NOT NULL CHECK (item_count > 0),
  sku_count                INTEGER NOT NULL CHECK (sku_count > 0),
  safe_order JSONB NOT NULL CHECK (JSONB_TYPEOF(safe_order) = 'object'),
  safe_price_detail JSONB NOT NULL CHECK (JSONB_TYPEOF(safe_price_detail) = 'object'),
  normalized_items JSONB NOT NULL CHECK (JSONB_TYPEOF(normalized_items) = 'array'),
  detail_request_id TEXT NOT NULL CHECK (BTRIM(detail_request_id) <> ''),
  price_detail_request_id TEXT NOT NULL CHECK (BTRIM(price_detail_request_id) <> ''),
  source_hash CHAR(64) NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
  last_order_update_at     TIMESTAMPTZ,
  last_synced_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, order_id)
);

CREATE INDEX IF NOT EXISTS tiktok_shop_order_snapshots_shop_status_idx
  ON tiktok_shop_order_snapshots(shop_id, order_status, last_synced_at DESC);
