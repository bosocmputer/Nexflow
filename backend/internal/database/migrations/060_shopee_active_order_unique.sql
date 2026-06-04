-- 060_shopee_active_order_unique.sql
-- Allow Shopee Realtime route recreation to archive an unsent bill and create
-- a replacement document for the same shop/order. Duplicate protection should
-- apply to active bills only; archived unsent bills are retained for audit.

DROP INDEX IF EXISTS bills_shopee_shop_order_unique;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM (
        SELECT COALESCE(NULLIF(raw_data->>'shopee_shop_id', ''), 'legacy') AS shop_key,
               raw_data->>'order_id' AS order_id,
               COUNT(*) AS row_count
          FROM bills
         WHERE source = 'shopee'
           AND archived_at IS NULL
           AND raw_data ? 'order_id'
         GROUP BY 1, 2
        HAVING COUNT(*) > 1
      ) dupes
  ) THEN
    CREATE UNIQUE INDEX IF NOT EXISTS bills_shopee_shop_order_unique
      ON bills (
        (COALESCE(NULLIF(raw_data->>'shopee_shop_id', ''), 'legacy')),
        (raw_data->>'order_id')
      )
      WHERE source = 'shopee'
        AND archived_at IS NULL
        AND raw_data ? 'order_id';
  ELSE
    RAISE NOTICE 'Skipped active bills_shopee_shop_order_unique because active Shopee duplicate order rows need reconciliation first';
    CREATE INDEX IF NOT EXISTS bills_shopee_shop_order_lookup_idx
      ON bills (
        (COALESCE(NULLIF(raw_data->>'shopee_shop_id', ''), 'legacy')),
        (raw_data->>'order_id')
      )
      WHERE source = 'shopee'
        AND archived_at IS NULL
        AND raw_data ? 'order_id';
  END IF;
END $$;
