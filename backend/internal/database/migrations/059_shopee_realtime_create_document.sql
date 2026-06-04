-- 059_shopee_realtime_create_document.sql
-- Split Shopee Realtime document routing from the existing Shopee import route
-- and make realtime actions create Nexflow documents before users send SML.

ALTER TABLE channel_defaults
  DROP CONSTRAINT IF EXISTS channel_defaults_channel_check;

ALTER TABLE channel_defaults
  ADD CONSTRAINT channel_defaults_channel_check
  CHECK (channel IN (
    'line',
    'email',
    'shopee',
    'shopee_realtime',
    'shopee_email',
    'shopee_shipped',
    'lazada',
    'tiktok',
    'manual',
    'shopee_settlement'
  ));

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
  'shopee_realtime',
  bill_type,
  party_code,
  party_name,
  party_phone,
  party_address,
  party_tax_id,
  updated_by,
  NOW(),
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
FROM channel_defaults
WHERE channel = 'shopee'
  AND bill_type = 'sale'
ON CONFLICT (channel, bill_type) DO NOTHING;

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
      'shipping_document_download'
    )
  );
