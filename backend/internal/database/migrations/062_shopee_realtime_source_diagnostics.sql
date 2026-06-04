-- Track whether the latest Shopee snapshot update came from live push,
-- scheduled/manual sync, or a shipping refresh. Additive and idempotent.

ALTER TABLE shopee_order_snapshots
  ADD COLUMN IF NOT EXISTS last_update_source TEXT NOT NULL DEFAULT 'unknown';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_constraint
     WHERE conname = 'shopee_order_snapshots_last_update_source_check'
  ) THEN
    ALTER TABLE shopee_order_snapshots
      ADD CONSTRAINT shopee_order_snapshots_last_update_source_check
      CHECK (last_update_source IN ('unknown','sync','push','shipping'));
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS shopee_order_snapshots_update_source_idx
  ON shopee_order_snapshots(last_update_source, updated_at DESC);
