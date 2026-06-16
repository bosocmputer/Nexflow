-- 030_tiktok_import.sql — TikTok Excel/CSV import channel

ALTER TABLE bills DROP CONSTRAINT IF EXISTS bills_source_check;
ALTER TABLE bills ADD CONSTRAINT bills_source_check
  CHECK (source IN ('line','email','lazada','tiktok','shopee','shopee_email','shopee_shipped','manual'));

ALTER TABLE channel_defaults DROP CONSTRAINT IF EXISTS channel_defaults_channel_check;
ALTER TABLE channel_defaults ADD CONSTRAINT channel_defaults_channel_check
  CHECK (channel IN ('line','email','shopee','shopee_realtime','shopee_realtime_cancel','shopee_email','shopee_shipped','lazada','tiktok','manual','shopee_settlement'));

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_indexes
     WHERE schemaname = current_schema()
       AND indexname = 'bills_tiktok_order_id_unique'
  ) THEN
    CREATE UNIQUE INDEX bills_tiktok_order_id_unique
      ON bills ((raw_data->>'order_id'))
      WHERE source = 'tiktok'
        AND raw_data ? 'order_id'
        AND COALESCE(raw_data->>'order_id', '') <> '';
  END IF;
END $$;
