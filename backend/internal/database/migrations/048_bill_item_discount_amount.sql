-- 048_bill_item_discount_amount.sql
-- Per-line discount for Shopee purchase email orders. Defaults to zero so
-- existing channels keep their current behavior.

ALTER TABLE bill_items
  ADD COLUMN IF NOT EXISTS discount_amount NUMERIC(14,2) NOT NULL DEFAULT 0;
