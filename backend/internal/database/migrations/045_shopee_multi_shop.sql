-- 045_shopee_multi_shop.sql
-- Shopee Open API multi-shop readiness. Keep SML routing shared for v1,
-- but preserve shop identity on connections and imported bills.

ALTER TABLE shopee_api_connections
  ADD COLUMN IF NOT EXISTS merchant_id BIGINT,
  ADD COLUMN IF NOT EXISTS label TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_error_code TEXT NOT NULL DEFAULT '';

UPDATE shopee_api_connections
   SET label = COALESCE(NULLIF(label, ''), NULLIF(shop_name, ''), 'Shop ' || shop_id::text)
 WHERE label = '';

CREATE INDEX IF NOT EXISTS shopee_api_connections_active_idx
  ON shopee_api_connections(environment, disabled_at, updated_at DESC);

-- Legacy index keyed only by order_id blocks different Shopee shops from having
-- the same order number. Replace it with shop-aware uniqueness while preserving
-- a "legacy" bucket for old Excel imports that did not know shop_id yet.
DROP INDEX IF EXISTS bills_shopee_order_id_unique;

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
        AND raw_data ? 'order_id';
  ELSE
    RAISE NOTICE 'Skipped bills_shopee_shop_order_unique because existing Shopee duplicate order rows need reconciliation first';
    CREATE INDEX IF NOT EXISTS bills_shopee_shop_order_lookup_idx
      ON bills (
        (COALESCE(NULLIF(raw_data->>'shopee_shop_id', ''), 'legacy')),
        (raw_data->>'order_id')
      )
      WHERE source = 'shopee'
        AND raw_data ? 'order_id';
  END IF;
END $$;
