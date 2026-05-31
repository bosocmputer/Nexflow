-- 027_bill_item_source_image.sql
-- Best-effort source product image from email/marketplace evidence.
ALTER TABLE bill_items
  ADD COLUMN IF NOT EXISTS source_image_url TEXT NOT NULL DEFAULT '';
