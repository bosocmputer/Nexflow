-- Tenant-side metadata for central TikTok Shop Gateway connections.
-- Sensitive authorization material remains exclusively in the central gateway.

CREATE TABLE IF NOT EXISTS tiktok_shop_connections (
  gateway_connection_id UUID PRIMARY KEY,
  shop_id               TEXT NOT NULL UNIQUE CHECK (BTRIM(shop_id) <> ''),
  shop_name             TEXT NOT NULL DEFAULT '',
  label                 TEXT NOT NULL DEFAULT '',
  shop_region           TEXT NOT NULL DEFAULT '',
  seller_type           TEXT NOT NULL DEFAULT '',
  shop_code             TEXT NOT NULL DEFAULT '',
  granted_scopes        JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (JSONB_TYPEOF(granted_scopes) = 'array'),
  access_expires_at     TIMESTAMPTZ NOT NULL,
  refresh_expires_at    TIMESTAMPTZ NOT NULL,
  disabled_at           TIMESTAMPTZ,
  connected_at          TIMESTAMPTZ NOT NULL,
  gateway_updated_at    TIMESTAMPTZ NOT NULL,
  last_gateway_sync_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tiktok_shop_connections_active_idx
  ON tiktok_shop_connections(updated_at DESC)
  WHERE disabled_at IS NULL;
