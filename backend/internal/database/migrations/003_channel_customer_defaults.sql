-- 003_channel_customer_defaults.sql — default cust_code/name/phone per input channel
-- Used by Email/LINE/Shopee/Lazada paths to fill in customer info when source data is incomplete.

CREATE TABLE IF NOT EXISTS channel_customer_defaults (
  channel     TEXT PRIMARY KEY
              CHECK (channel IN ('line','email','shopee','lazada')),
  cust_code   TEXT NOT NULL,
  cust_name   TEXT NOT NULL,
  cust_phone  TEXT NOT NULL DEFAULT '',
  updated_by  UUID,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
