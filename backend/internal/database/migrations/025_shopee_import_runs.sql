CREATE TABLE IF NOT EXISTS import_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source TEXT NOT NULL,
  filename TEXT NOT NULL DEFAULT '',
  file_sha256 TEXT NOT NULL DEFAULT '',
  period_start DATE,
  period_end DATE,
  total_orders INT NOT NULL DEFAULT 0,
  new_orders INT NOT NULL DEFAULT 0,
  duplicate_orders INT NOT NULL DEFAULT 0,
  skipped_orders INT NOT NULL DEFAULT 0,
  warning_count INT NOT NULL DEFAULT 0,
  created_count INT NOT NULL DEFAULT 0,
  failed_count INT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'preview'
    CHECK (status IN ('preview','confirmed','failed')),
  detail JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  confirmed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS import_runs_source_created_idx
  ON import_runs(source, created_at DESC);

CREATE INDEX IF NOT EXISTS import_runs_file_sha_idx
  ON import_runs(file_sha256)
  WHERE file_sha256 <> '';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_indexes
    WHERE schemaname = current_schema()
      AND indexname = 'bills_shopee_order_id_unique'
  ) AND NOT EXISTS (
    SELECT 1
    FROM bills
    WHERE source = 'shopee'
      AND raw_data ? 'order_id'
    GROUP BY raw_data->>'order_id'
    HAVING COUNT(*) > 1
  ) THEN
    CREATE UNIQUE INDEX bills_shopee_order_id_unique
      ON bills ((raw_data->>'order_id'))
      WHERE source = 'shopee'
        AND raw_data ? 'order_id';
  END IF;
END $$;
