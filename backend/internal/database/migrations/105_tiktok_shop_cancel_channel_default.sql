-- Dedicated cancellation route for TikTok Shop Open API orders. This route is
-- intentionally not seeded: each tenant must explicitly select a verified SML
-- cancellation document format before cancellation automation can be enabled.

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
    'tiktok_shop_cancel',
    'manual',
    'shopee_settlement',
    'tiktok_settlement',
    'line_myshop'
  ));
