-- Durable tenant-side receipt and exact-order reconciliation queue for signed
-- TikTok Shop webhooks. This queue updates only typed order snapshots.

CREATE TABLE IF NOT EXISTS tiktok_gateway_request_nonces (
  tenant_slug TEXT NOT NULL,
  nonce       TEXT NOT NULL,
  expires_at  TIMESTAMPTZ NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tenant_slug, nonce)
);

CREATE INDEX IF NOT EXISTS tiktok_gateway_request_nonces_exp_idx
  ON tiktok_gateway_request_nonces(expires_at);

CREATE TABLE IF NOT EXISTS tiktok_shop_webhook_events (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  gateway_event_id      UUID NOT NULL UNIQUE,
  notification_id       TEXT NOT NULL UNIQUE CHECK (BTRIM(notification_id) <> ''),
  gateway_connection_id UUID NOT NULL REFERENCES tiktok_shop_connections(gateway_connection_id) ON DELETE RESTRICT,
  shop_id               TEXT NOT NULL REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  order_id              TEXT NOT NULL CHECK (BTRIM(order_id) <> ''),
  event_order_status    TEXT NOT NULL CHECK (BTRIM(event_order_status) <> ''),
  event_timestamp       TIMESTAMPTZ NOT NULL,
  order_update_at       TIMESTAMPTZ NOT NULL,
  status                TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','running','retry','succeeded','needs_review')),
  attempts              INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 8),
  next_run_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_until           TIMESTAMPTZ,
  last_error_code       TEXT NOT NULL DEFAULT '',
  last_error_message    TEXT NOT NULL DEFAULT '' CHECK (OCTET_LENGTH(last_error_message) <= 512),
  received_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  started_at            TIMESTAMPTZ,
  completed_at          TIMESTAMPTZ,
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tiktok_shop_webhook_events_due_idx
  ON tiktok_shop_webhook_events(status, next_run_at, received_at)
  WHERE status IN ('pending','retry');

CREATE INDEX IF NOT EXISTS tiktok_shop_webhook_events_shop_order_idx
  ON tiktok_shop_webhook_events(shop_id, order_id, received_at DESC);
