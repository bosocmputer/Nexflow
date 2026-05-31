-- 012_channel_defaults_inventory.sql — per-channel WH/Shelf + VAT override
--
-- Replaces .env SHOPEE_SML_WH_CODE / SHOPEE_SML_SHELF_CODE / SHOPEE_SML_VAT_TYPE
-- / SHOPEE_SML_VAT_RATE for SML 248 channels (shopee/shopee_email/shopee_shipped/
-- lazada). bills.go overlays these on top of cfg.ShopeeSML* — sentinel values
-- ('' / -1) mean "use the server default", any other value overrides per channel.
--
-- Why a sentinel rather than NULL: keeps Go scan code simple (no sql.Null types
-- for what is fundamentally a "set or unset" choice already encoded by 0/empty).

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS wh_code    TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS shelf_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS vat_type   INT  NOT NULL DEFAULT -1,
  ADD COLUMN IF NOT EXISTS vat_rate   NUMERIC(6,3) NOT NULL DEFAULT -1;
