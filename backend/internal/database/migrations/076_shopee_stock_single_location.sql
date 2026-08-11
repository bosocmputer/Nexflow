-- 076_shopee_stock_single_location.sql
-- Each Shopee shop must calculate stock from exactly one SML warehouse/location pair.

UPDATE shopee_stock_settings
   SET enabled = false,
       scope_mode = 'unconfigured',
       locations = '[]'::jsonb,
       all_scope_warning_acknowledged = false,
       dry_run_required = true,
       paused_reason = 'single_location_required',
       last_error = 'กรุณาเลือก 1 คลังและ 1 พื้นที่เก็บใหม่ก่อนเปิดซิงก์',
       updated_at = NOW()
 WHERE scope_mode = 'selected'
   AND jsonb_array_length(locations) <> 1;

UPDATE shopee_stock_settings
   SET locations = '[]'::jsonb,
       updated_at = NOW()
 WHERE scope_mode = 'unconfigured'
   AND jsonb_array_length(locations) <> 0;

ALTER TABLE shopee_stock_settings
  DROP CONSTRAINT IF EXISTS shopee_stock_settings_single_location_check;

ALTER TABLE shopee_stock_settings
  ADD CONSTRAINT shopee_stock_settings_single_location_check
  CHECK (
    (scope_mode = 'unconfigured' AND jsonb_array_length(locations) = 0)
    OR
    (scope_mode = 'selected' AND jsonb_array_length(locations) = 1)
  );
