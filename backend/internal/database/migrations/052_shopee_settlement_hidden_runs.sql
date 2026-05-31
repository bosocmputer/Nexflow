-- 052_shopee_settlement_hidden_runs.sql — hide Shopee settlement runs without deleting history

ALTER TABLE shopee_settlement_runs
  ADD COLUMN IF NOT EXISTS hidden_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS hidden_by UUID REFERENCES users(id),
  ADD COLUMN IF NOT EXISTS hidden_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS shopee_settlement_runs_visible_created_idx
  ON shopee_settlement_runs(created_at DESC)
  WHERE hidden_at IS NULL;

CREATE INDEX IF NOT EXISTS shopee_settlement_runs_hidden_created_idx
  ON shopee_settlement_runs(hidden_at DESC, created_at DESC)
  WHERE hidden_at IS NOT NULL;
