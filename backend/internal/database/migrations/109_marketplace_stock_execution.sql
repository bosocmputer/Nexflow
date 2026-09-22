-- 109_marketplace_stock_execution.sql
-- Durable execution controls are intentionally additive. Existing pools stay
-- drafts and external writes remain controlled by the runtime feature gate.

ALTER TABLE marketplace_stock_pools
  ADD COLUMN IF NOT EXISTS kill_switch_enabled BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS schedule_interval_seconds INTEGER NOT NULL DEFAULT 300
    CHECK (schedule_interval_seconds >= 300 AND schedule_interval_seconds <= 86400),
  ADD COLUMN IF NOT EXISTS last_schedule_at TIMESTAMPTZ;

ALTER TABLE marketplace_stock_pool_members
  ADD COLUMN IF NOT EXISTS external_warehouse_id TEXT NOT NULL DEFAULT '';

ALTER TABLE marketplace_stock_runs
  ADD COLUMN IF NOT EXISTS idempotency_key CHAR(64) NOT NULL DEFAULT '';

ALTER TABLE marketplace_stock_run_lines
  ADD COLUMN IF NOT EXISTS idempotency_key CHAR(64) NOT NULL DEFAULT '';

-- A target under the same immutable pool configuration is never written more
-- than once. A later configuration version deliberately creates a new key.
CREATE UNIQUE INDEX IF NOT EXISTS marketplace_stock_run_lines_member_idempotency_idx
  ON marketplace_stock_run_lines(member_id, idempotency_key)
  WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS marketplace_stock_pools_schedule_idx
  ON marketplace_stock_pools(auto_enabled, status, last_schedule_at)
  WHERE auto_enabled=true AND status='active';
