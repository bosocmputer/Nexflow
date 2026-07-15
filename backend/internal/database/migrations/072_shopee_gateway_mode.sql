-- 072_shopee_gateway_mode.sql
-- Tenant metadata and replay protection for the central Shopee gateway.
-- Existing direct credentials remain untouched for instant rollback.

ALTER TABLE shopee_api_connections
  ADD COLUMN IF NOT EXISTS credential_mode TEXT NOT NULL DEFAULT 'direct',
  ADD COLUMN IF NOT EXISTS gateway_connection_id UUID,
  ADD COLUMN IF NOT EXISTS gateway_token_state TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS gateway_access_expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS gateway_refresh_expires_at TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'shopee_api_connections_credential_mode_check'
  ) THEN
    ALTER TABLE shopee_api_connections
      ADD CONSTRAINT shopee_api_connections_credential_mode_check
      CHECK (credential_mode IN ('direct', 'gateway'));
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS shopee_api_connections_gateway_id_idx
  ON shopee_api_connections(gateway_connection_id)
  WHERE gateway_connection_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS shopee_gateway_request_nonces (
  tenant_slug TEXT NOT NULL,
  nonce       TEXT NOT NULL,
  expires_at  TIMESTAMPTZ NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tenant_slug, nonce)
);

CREATE INDEX IF NOT EXISTS shopee_gateway_request_nonces_exp_idx
  ON shopee_gateway_request_nonces(expires_at);
