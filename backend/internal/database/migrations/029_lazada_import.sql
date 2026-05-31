DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_indexes
    WHERE schemaname = current_schema()
      AND indexname = 'bills_lazada_order_id_unique'
  ) AND NOT EXISTS (
    SELECT 1
    FROM bills
    WHERE source = 'lazada'
      AND raw_data ? 'order_id'
    GROUP BY raw_data->>'order_id'
    HAVING COUNT(*) > 1
  ) THEN
    CREATE UNIQUE INDEX bills_lazada_order_id_unique
      ON bills ((raw_data->>'order_id'))
      WHERE source = 'lazada'
        AND raw_data ? 'order_id';
  END IF;
END $$;
