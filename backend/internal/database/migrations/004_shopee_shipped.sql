-- 004_shopee_shipped.sql — extend bills.source CHECK to allow 'shopee_shipped'
-- (Shopee shipping confirmation emails → SML purchaseorder)

ALTER TABLE bills DROP CONSTRAINT IF EXISTS bills_source_check;
ALTER TABLE bills ADD CONSTRAINT bills_source_check
  CHECK (source IN ('line','email','lazada','tiktok','shopee','shopee_email','shopee_shipped','manual'));
