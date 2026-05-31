-- 007_channel_defaults.sql — per-channel customer/supplier defaults from SML
--
-- Replaces the unused channel_customer_defaults table (renamed to _v1 for safety)
-- and the .env vars SHOPEE_SML_CUST_CODE / SHIPPED_SML_CUST_CODE. Composite PK
-- (channel, bill_type) so Lazada can configure separate cust/supplier rows.
--
-- party_code stores AR.code (sale → customer master) or V.code (purchase →
-- supplier master). For SML 248 channels (shopee*, lazada) the value is sent
-- as cust_code in saleinvoice/purchaseorder. For SML 213 channels (line/email)
-- party_name is used as the static contact_name override on sale_reserve.

-- 003 re-runs on every boot (CREATE TABLE IF NOT EXISTS) and would clash
-- with channel_customer_defaults_v1 if we kept the rename pattern. Since the
-- old table was never read by any code, just drop both forms — channel_defaults
-- is the new source of truth.
DROP TABLE IF EXISTS channel_customer_defaults;
DROP TABLE IF EXISTS channel_customer_defaults_v1;

CREATE TABLE IF NOT EXISTS channel_defaults (
  channel        TEXT NOT NULL
                 CHECK (channel IN ('line','email','shopee','shopee_email',
                                     'shopee_shipped','lazada','manual')),
  bill_type      TEXT NOT NULL CHECK (bill_type IN ('sale','purchase')),
  party_code     TEXT NOT NULL,
  party_name     TEXT NOT NULL,
  party_phone    TEXT NOT NULL DEFAULT '',
  party_address  TEXT NOT NULL DEFAULT '',
  party_tax_id   TEXT NOT NULL DEFAULT '',
  updated_by     UUID,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (channel, bill_type)
);
