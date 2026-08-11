-- 075_shopee_stock_selected_scope_only.sql
-- Stock synchronization must use explicitly selected warehouse/location pairs.

UPDATE shopee_stock_settings
   SET enabled = false,
       scope_mode = 'unconfigured',
       locations = '[]'::jsonb,
       all_scope_warning_acknowledged = false,
       dry_run_required = true,
       paused_reason = 'warehouse_scope_required',
       last_error = 'กรุณาเลือกคลังและพื้นที่เก็บใหม่ก่อนเปิดซิงก์',
       updated_at = NOW()
 WHERE scope_mode = 'all';

ALTER TABLE shopee_stock_settings
  DROP CONSTRAINT IF EXISTS shopee_stock_settings_scope_mode_check;

ALTER TABLE shopee_stock_settings
  ADD CONSTRAINT shopee_stock_settings_scope_mode_check
  CHECK (scope_mode IN ('unconfigured', 'selected'));
