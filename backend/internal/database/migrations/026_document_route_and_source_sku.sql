-- 026_document_route_and_source_sku.sql
-- Keep the source platform SKU separate from the SML item_code, and stamp
-- which SML document route a bill belongs to so saleorder/saleinvoice queues
-- can be shown as separate work menus.

ALTER TABLE bills
  ADD COLUMN IF NOT EXISTS document_route TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS bills_document_route_idx
  ON bills (document_route);

ALTER TABLE bill_items
  ADD COLUMN IF NOT EXISTS source_sku TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS bill_items_source_sku_idx
  ON bill_items (source_sku)
  WHERE source_sku <> '';
