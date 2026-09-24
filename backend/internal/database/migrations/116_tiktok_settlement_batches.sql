-- Operator-selected TikTok Statements may be posted as one SML RC only after
-- the operator verifies the actual bank amount.  This is intentionally not an
-- automatic payout matcher: TikTok Thailand's withdrawal API does not prove
-- Statement membership.

CREATE TABLE IF NOT EXISTS tiktok_settlement_batches (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id TEXT NOT NULL REFERENCES tiktok_shop_connections(shop_id) ON DELETE RESTRICT,
  shop_label TEXT NOT NULL DEFAULT '',
  currency TEXT NOT NULL DEFAULT 'THB',
  statement_count INT NOT NULL CHECK (statement_count > 0),
  order_count INT NOT NULL CHECK (order_count > 0),
  settlement_amount NUMERIC(16,2) NOT NULL,
  invoice_amount NUMERIC(16,2) NOT NULL,
  fee_amount NUMERIC(16,2) NOT NULL DEFAULT 0,
  bank_amount NUMERIC(16,2) NOT NULL,
  bank_reference TEXT NOT NULL DEFAULT '',
  selection_digest TEXT NOT NULL,
  config_version INT NOT NULL CHECK (config_version > 0),
  route_config_version BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'sending' CHECK (status IN ('sending','sent','failed','unknown_result')),
  rc_doc_no TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL DEFAULT '',
  error_msg TEXT NOT NULL DEFAULT '',
  anomaly_reason TEXT NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  created_by UUID REFERENCES users(id),
  created_by_email TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tiktok_settlement_batches_shop_status_idx
  ON tiktok_settlement_batches(shop_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS tiktok_settlement_batch_members (
  batch_id UUID NOT NULL REFERENCES tiktok_settlement_batches(id) ON DELETE RESTRICT,
  run_id UUID NOT NULL REFERENCES tiktok_settlement_runs(id) ON DELETE RESTRICT,
  statement_id TEXT NOT NULL,
  PRIMARY KEY (batch_id, run_id)
);
CREATE INDEX IF NOT EXISTS tiktok_settlement_batch_members_statement_idx
  ON tiktok_settlement_batch_members(statement_id);
