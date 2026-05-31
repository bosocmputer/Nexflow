-- 046_shopee_order_search_indexes.sql
-- Speed up exact marketplace order-id searches and Shopee email lifecycle
-- dedupe. The generic free-text path still exists for names/subjects, but
-- order-like searches should avoid scanning large raw_data email bodies.

CREATE INDEX IF NOT EXISTS idx_bills_sml_order_id_norm
  ON bills (UPPER(TRIM(LEADING '#' FROM COALESCE(sml_order_id, ''))))
  WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_raw_order_id_norm
  ON bills (UPPER(TRIM(LEADING '#' FROM COALESCE(raw_data->>'order_id', ''))))
  WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_raw_shopee_order_id_norm
  ON bills (UPPER(TRIM(LEADING '#' FROM COALESCE(raw_data->>'shopee_order_id', ''))))
  WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_shopee_order_events_order_id_upper_bill
  ON shopee_order_events (UPPER(order_id), bill_id);
