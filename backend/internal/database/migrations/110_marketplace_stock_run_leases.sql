-- A sync execution and its immutable SML planning run may coexist for the
-- same pool. Each run type remains single-flight per pool.
DROP INDEX IF EXISTS marketplace_stock_runs_one_live_pool_idx;
CREATE UNIQUE INDEX marketplace_stock_runs_one_live_pool_type_idx
  ON marketplace_stock_runs(pool_id, run_type)
  WHERE status IN ('queued','running');
