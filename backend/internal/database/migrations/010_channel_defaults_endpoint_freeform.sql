-- 010_channel_defaults_endpoint_freeform.sql — let admins type any URL
--
-- The previous migration locked endpoint to a 4-value enum. Admins want to
-- type the SML URL freely (e.g. when SML upgrades from /v3 to /v4, or when
-- pointing at a different SML host). The backend now detects which client
-- to use by matching keywords in the URL ("saleorder", "saleinvoice",
-- "purchaseorder", "sale_reserve") — see handlers/bills.go:resolveEndpoint.

DO $$
BEGIN
  IF EXISTS (
    SELECT FROM information_schema.constraint_column_usage
    WHERE constraint_name = 'channel_defaults_endpoint_check'
  ) THEN
    ALTER TABLE channel_defaults DROP CONSTRAINT channel_defaults_endpoint_check;
  END IF;
END $$;
