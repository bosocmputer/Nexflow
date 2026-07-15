CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tenants (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug            TEXT NOT NULL UNIQUE,
  public_base_url TEXT NOT NULL,
  backend_url     TEXT NOT NULL,
  enabled         BOOLEAN NOT NULL DEFAULT TRUE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shop_connections (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id              UUID NOT NULL REFERENCES tenants(id),
  shop_id                BIGINT NOT NULL UNIQUE,
  merchant_id            BIGINT,
  shop_name              TEXT NOT NULL DEFAULT '',
  environment            TEXT NOT NULL CHECK (environment IN ('sandbox', 'live')),
  access_token_cipher    BYTEA NOT NULL,
  access_token_nonce     BYTEA NOT NULL,
  refresh_token_cipher   BYTEA NOT NULL,
  refresh_token_nonce    BYTEA NOT NULL,
  encryption_key_version INT NOT NULL DEFAULT 1,
  access_expires_at      TIMESTAMPTZ NOT NULL,
  refresh_expires_at     TIMESTAMPTZ NOT NULL,
  last_refreshed_at      TIMESTAMPTZ,
  last_api_at            TIMESTAMPTZ,
  last_error_code        TEXT NOT NULL DEFAULT '',
  disabled_at            TIMESTAMPTZ,
  connected_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shop_connections_tenant_updated_idx
  ON shop_connections(tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS oauth_states (
  state_hash   TEXT PRIMARY KEY,
  tenant_id    UUID NOT NULL REFERENCES tenants(id),
  user_id      TEXT NOT NULL,
  return_url   TEXT NOT NULL,
  nonce        TEXT NOT NULL,
  environment  TEXT NOT NULL CHECK (environment IN ('sandbox', 'live')),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS oauth_states_tenant_nonce_idx
  ON oauth_states(tenant_id, nonce);

CREATE INDEX IF NOT EXISTS oauth_states_expires_idx
  ON oauth_states(expires_at);

CREATE TABLE IF NOT EXISTS push_events (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          UUID REFERENCES tenants(id),
  shop_id            BIGINT NOT NULL DEFAULT 0,
  order_sn           TEXT NOT NULL DEFAULT '',
  push_code          INT NOT NULL DEFAULT 0,
  event_status       TEXT NOT NULL DEFAULT '',
  dedupe_key         TEXT NOT NULL UNIQUE,
  raw_payload        JSONB NOT NULL DEFAULT '{}'::jsonb,
  processing_status  TEXT NOT NULL DEFAULT 'pending'
                     CHECK (processing_status IN ('pending', 'queued', 'delivered', 'unknown_shop', 'failed')),
  attempts           INT NOT NULL DEFAULT 0,
  last_error_code    TEXT NOT NULL DEFAULT '',
  received_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at       TIMESTAMPTZ,
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS push_events_queue_idx
  ON push_events(processing_status, received_at)
  WHERE processing_status IN ('pending', 'queued', 'failed');

CREATE INDEX IF NOT EXISTS push_events_shop_idx
  ON push_events(shop_id, received_at DESC);

CREATE TABLE IF NOT EXISTS api_request_logs (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     UUID REFERENCES tenants(id),
  nonce         TEXT NOT NULL,
  direction     TEXT NOT NULL CHECK (direction IN ('tenant_to_gateway', 'gateway_to_tenant')),
  operation     TEXT NOT NULL,
  status_code   INT NOT NULL DEFAULT 0,
  duration_ms   INT NOT NULL DEFAULT 0,
  error_code    TEXT NOT NULL DEFAULT '',
  request_id    TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, nonce, direction)
);

CREATE INDEX IF NOT EXISTS api_request_logs_created_idx
  ON api_request_logs(created_at DESC);

CREATE TABLE IF NOT EXISTS delivery_outbox (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL REFERENCES tenants(id),
  event_type      TEXT NOT NULL CHECK (event_type IN ('connection_upsert', 'push_event')),
  dedupe_key      TEXT NOT NULL UNIQUE,
  payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
  status          TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'running', 'delivered', 'failed')),
  attempts        INT NOT NULL DEFAULT 0,
  next_run_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error_code TEXT NOT NULL DEFAULT '',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS delivery_outbox_queue_idx
  ON delivery_outbox(status, next_run_at, created_at)
  WHERE status IN ('pending', 'failed');
