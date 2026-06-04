-- 051_shopee_settlement_channel_defaults.sql
-- Merge Shopee settlement routing defaults into channel_defaults.

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

ALTER TABLE channel_defaults
  DROP CONSTRAINT IF EXISTS channel_defaults_bill_type_check;

ALTER TABLE channel_defaults
  ADD CONSTRAINT channel_defaults_bill_type_check
  CHECK (bill_type IN ('sale', 'purchase', 'ar_receipt'));

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS passbook_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS passbook_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS bank_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS bank_branch TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS expense_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS expense_name TEXT NOT NULL DEFAULT '';

INSERT INTO channel_defaults (
  channel,
  bill_type,
  party_code,
  party_name,
  doc_format_code,
  endpoint,
  doc_prefix,
  doc_running_format,
  passbook_code,
  passbook_name,
  bank_code,
  bank_branch,
  expense_code,
  expense_name,
  updated_by,
  updated_at
)
SELECT
  'shopee_settlement',
  'ar_receipt',
  '',
  '',
  COALESCE(NULLIF(doc_format_code, ''), 'RC'),
  '/api/v1/ar/receipts',
  COALESCE(NULLIF(doc_format_code, ''), 'RC'),
  '@YYMM####',
  passbook_code,
  passbook_name,
  bank_code,
  bank_branch,
  expense_code,
  expense_name,
  updated_by,
  updated_at
FROM shopee_settlement_defaults
WHERE id = 1
ON CONFLICT (channel, bill_type) DO NOTHING;
