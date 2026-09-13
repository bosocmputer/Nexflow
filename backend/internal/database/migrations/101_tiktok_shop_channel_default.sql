-- Dedicated SML document route for TikTok Shop Open API orders.
-- Intentionally does not seed or copy the existing TikTok Excel route: each
-- tenant must select its own SML customer, warehouse, VAT, and shipping item.

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
    'tiktok_shop',
    'manual',
    'shopee_settlement',
    'line_myshop'
  ));
