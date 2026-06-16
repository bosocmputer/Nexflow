-- 064_shopee_cancel_after_sml.sql
-- Track Shopee orders cancelled after a sale invoice was sent to SML.
-- Create-CN is controlled by ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS and still
-- requires SML readiness before writing any cancellation document.

ALTER TABLE channel_defaults
  DROP CONSTRAINT IF EXISTS channel_defaults_channel_check;

ALTER TABLE channel_defaults
  ADD CONSTRAINT channel_defaults_channel_check
  CHECK (channel IN (
    'line',
    'email',
    'shopee',
    'shopee_realtime',
    'shopee_realtime_cancel',
    'shopee_email',
    'shopee_shipped',
    'lazada',
    'tiktok',
    'manual',
    'shopee_settlement'
  ));

WITH source_route AS (
  SELECT *
    FROM channel_defaults
   WHERE channel = 'shopee_realtime'
     AND bill_type = 'sale'
   LIMIT 1
), fallback_route AS (
  SELECT *
    FROM channel_defaults
   WHERE channel = 'shopee'
     AND bill_type = 'sale'
   LIMIT 1
), picked AS (
  SELECT * FROM source_route
  UNION ALL
  SELECT * FROM fallback_route
   WHERE NOT EXISTS (SELECT 1 FROM source_route)
)
INSERT INTO channel_defaults (
  channel,
  bill_type,
  party_code,
  party_name,
  party_phone,
  party_address,
  party_tax_id,
  updated_by,
  updated_at,
  doc_format_code,
  endpoint,
  doc_prefix,
  doc_running_format,
  wh_code,
  shelf_code,
  vat_type,
  vat_rate,
  branch_code,
  sale_code,
  unit_code,
  doc_time,
  shipping_item_enabled,
  shipping_item_code,
  shipping_item_unit_code,
  passbook_code,
  passbook_name,
  bank_code,
  bank_branch,
  expense_code,
  expense_name,
  inquiry_type,
  remark_2
)
SELECT
  'shopee_realtime_cancel',
  'sale',
  party_code,
  party_name,
  party_phone,
  party_address,
  party_tax_id,
  updated_by,
  NOW(),
  'CN',
  'creditnote',
  'CN',
  'YYMM####',
  wh_code,
  shelf_code,
  vat_type,
  vat_rate,
  branch_code,
  sale_code,
  unit_code,
  doc_time,
  false,
  '',
  '',
  '',
  '',
  '',
  '',
  '',
  '',
  inquiry_type,
  remark_2
FROM picked
ON CONFLICT (channel, bill_type) DO NOTHING;

CREATE TABLE IF NOT EXISTS shopee_sml_cancellations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id BIGINT NOT NULL,
  order_sn TEXT NOT NULL,
  bill_id UUID REFERENCES bills(id) ON DELETE SET NULL,
  sale_sml_doc_no TEXT NOT NULL DEFAULT '',
  cancel_sml_doc_no TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','previewed','creating','created','already_exists','failed','blocked')),
  error TEXT NOT NULL DEFAULT '',
  response JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS shopee_sml_cancellations_order_idx
  ON shopee_sml_cancellations(shop_id, order_sn, updated_at DESC);

CREATE INDEX IF NOT EXISTS shopee_sml_cancellations_bill_idx
  ON shopee_sml_cancellations(bill_id, updated_at DESC)
  WHERE bill_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS shopee_sml_cancellations_success_unique_idx
  ON shopee_sml_cancellations(shop_id, order_sn, sale_sml_doc_no)
  WHERE status IN ('created','already_exists');

ALTER TABLE shopee_action_outbox
  DROP CONSTRAINT IF EXISTS shopee_action_outbox_action_check;

ALTER TABLE shopee_action_outbox
  ADD CONSTRAINT shopee_action_outbox_action_check
  CHECK (
    action IN (
      'create_document',
      'erp_send',
      'ship_order',
      'reconcile_shipping',
      'shipping_document_create',
      'shipping_document_result',
      'shipping_document_download',
      'cancel_sml_document'
    )
  );
