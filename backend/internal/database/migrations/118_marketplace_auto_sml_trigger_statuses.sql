-- Expand tenant-scoped Auto SML trigger choices without changing existing values.
-- UNPAID remains intentionally excluded because no sale document may be sent
-- before Shopee confirms payment.

ALTER TABLE shopee_auto_sml_settings
  DROP CONSTRAINT IF EXISTS shopee_auto_sml_settings_trigger_status_check,
  ADD CONSTRAINT shopee_auto_sml_settings_trigger_status_check
  CHECK (trigger_status IN ('READY_TO_SHIP','PROCESSED','SHIPPED','COMPLETED'));

ALTER TABLE shopee_auto_sml_jobs
  DROP CONSTRAINT IF EXISTS shopee_auto_sml_jobs_trigger_status_check,
  ADD CONSTRAINT shopee_auto_sml_jobs_trigger_status_check
  CHECK (trigger_status_snapshot IN ('READY_TO_SHIP','PROCESSED','SHIPPED','COMPLETED'));

ALTER TABLE tiktok_shop_auto_sml_settings
  DROP CONSTRAINT IF EXISTS tiktok_shop_auto_sml_settings_trigger_status_check,
  ADD CONSTRAINT tiktok_shop_auto_sml_settings_trigger_status_check
  CHECK (trigger_status IN ('AWAITING_SHIPMENT','AWAITING_COLLECTION','IN_TRANSIT','COMPLETED'));

ALTER TABLE tiktok_shop_auto_sml_jobs
  DROP CONSTRAINT IF EXISTS tiktok_shop_auto_sml_jobs_trigger_status_check,
  ADD CONSTRAINT tiktok_shop_auto_sml_jobs_trigger_status_check
  CHECK (trigger_status_snapshot IN ('AWAITING_SHIPMENT','AWAITING_COLLECTION','IN_TRANSIT','COMPLETED'));
