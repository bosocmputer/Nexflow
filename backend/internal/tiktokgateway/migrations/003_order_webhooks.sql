CREATE TABLE IF NOT EXISTS webhook_events (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         UUID REFERENCES tenants(id),
  notification_id   TEXT NOT NULL UNIQUE CHECK (BTRIM(notification_id) <> ''),
  notification_type INTEGER NOT NULL CHECK (notification_type = 1),
  shop_id           TEXT NOT NULL CHECK (BTRIM(shop_id) <> ''),
  order_id          TEXT NOT NULL CHECK (BTRIM(order_id) <> ''),
  order_status      TEXT NOT NULL CHECK (BTRIM(order_status) <> ''),
  event_timestamp   TIMESTAMPTZ NOT NULL,
  order_update_at   TIMESTAMPTZ NOT NULL,
  body_sha256       CHAR(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  processing_status TEXT NOT NULL CHECK (processing_status IN ('queued','unknown_shop','delivered','failed')),
  attempts          INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error_code   TEXT NOT NULL DEFAULT '',
  received_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS webhook_events_shop_received_idx
  ON webhook_events(shop_id, received_at DESC);

CREATE INDEX IF NOT EXISTS webhook_events_status_received_idx
  ON webhook_events(processing_status, received_at)
  WHERE processing_status IN ('queued','unknown_shop','failed');

CREATE TABLE IF NOT EXISTS webhook_delivery_outbox (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  webhook_event_id UUID NOT NULL UNIQUE REFERENCES webhook_events(id) ON DELETE RESTRICT,
  tenant_id       UUID NOT NULL REFERENCES tenants(id),
  payload         JSONB NOT NULL CHECK (JSONB_TYPEOF(payload) = 'object'),
  status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','delivered','failed')),
  attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_run_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error_code TEXT NOT NULL DEFAULT '',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS webhook_delivery_outbox_due_idx
  ON webhook_delivery_outbox(status, next_run_at, created_at)
  WHERE status IN ('pending','failed');
