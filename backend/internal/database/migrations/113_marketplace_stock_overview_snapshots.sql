-- 113_marketplace_stock_overview_snapshots.sql
-- The Marketplace Stock overview reads only previously verified snapshots.
-- These partial indexes keep the per-pool lookups bounded as AOY adds pools,
-- without refreshing SML or Marketplace data on page load.

CREATE INDEX IF NOT EXISTS marketplace_stock_runs_preview_snapshot_idx
  ON marketplace_stock_runs(pool_id, finished_at DESC, created_at DESC)
  WHERE run_type='preview' AND status='success';

CREATE INDEX IF NOT EXISTS marketplace_stock_run_lines_member_snapshot_idx
  ON marketplace_stock_run_lines(member_id, updated_at DESC)
  INCLUDE (run_id, target_qty, actual_qty, status)
  WHERE actual_qty IS NOT NULL AND status IN ('changed','unchanged');
