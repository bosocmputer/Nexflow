-- 047_channel_shipping_item_defaults.sql
-- Optional per-channel item used to turn Shopee purchase shipping fees into a
-- normal SML line item. Defaults are disabled so existing shops are untouched.

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS shipping_item_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS shipping_item_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS shipping_item_unit_code TEXT NOT NULL DEFAULT '';
