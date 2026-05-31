-- 041_shopee_open_api.sql
-- Shopee Open API direct connection state. Orders imported through this path
-- still create bills with source='shopee' and raw_data.flow='shopee_api' so
-- they reuse the existing Shopee review/SML retry pipeline.

CREATE TABLE IF NOT EXISTS shopee_api_connections (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id             BIGINT NOT NULL UNIQUE,
  shop_name           TEXT NOT NULL DEFAULT '',
  access_token        TEXT NOT NULL,
  refresh_token       TEXT NOT NULL,
  access_expires_at   TIMESTAMPTZ NOT NULL,
  refresh_expires_at  TIMESTAMPTZ NOT NULL,
  environment         TEXT NOT NULL DEFAULT 'sandbox'
                      CHECK (environment IN ('sandbox','live')),
  connected_by        UUID REFERENCES users(id),
  connected_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_refreshed_at   TIMESTAMPTZ,
  last_sync_at        TIMESTAMPTZ,
  last_sync_status    TEXT NOT NULL DEFAULT ''
                      CHECK (last_sync_status IN ('','ok','error')),
  last_sync_error     TEXT NOT NULL DEFAULT '',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shopee_api_connections_updated_idx
  ON shopee_api_connections(updated_at DESC);

CREATE TABLE IF NOT EXISTS shopee_api_oauth_states (
  state_hash   TEXT PRIMARY KEY,
  user_id      UUID REFERENCES users(id),
  environment  TEXT NOT NULL DEFAULT 'sandbox'
               CHECK (environment IN ('sandbox','live')),
  redirect_url TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS shopee_api_oauth_states_exp_idx
  ON shopee_api_oauth_states(expires_at);
