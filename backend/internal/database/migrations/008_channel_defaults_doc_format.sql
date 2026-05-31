-- 008_channel_defaults_doc_format.sql — per-channel doc_format_code
--
-- Replaces .env SHOPEE_SML_DOC_FORMAT and SHIPPED_SML_DOC_FORMAT. Each
-- (channel, bill_type) row carries the SML doc_format_code its bills should
-- post with — saleorder rows typically use "SR" / "RU" / similar, purchaseorder
-- rows use "PO". Empty string means "not applicable" (e.g. SML 213 sale_reserve
-- doesn't take a doc_format_code).

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS doc_format_code TEXT NOT NULL DEFAULT '';
