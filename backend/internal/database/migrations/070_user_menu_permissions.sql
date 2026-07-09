-- 070_user_menu_permissions.sql
-- Per-user menu visibility matrix. V1 uses can_view to hide/show menus and
-- stores create/update/delete for a later backend action-permission phase.

CREATE TABLE IF NOT EXISTS user_menu_permissions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  menu_key    TEXT NOT NULL,
  can_view    BOOLEAN NOT NULL DEFAULT false,
  can_create  BOOLEAN NOT NULL DEFAULT false,
  can_update  BOOLEAN NOT NULL DEFAULT false,
  can_delete  BOOLEAN NOT NULL DEFAULT false,
  updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, menu_key)
);

CREATE INDEX IF NOT EXISTS user_menu_permissions_user_idx
  ON user_menu_permissions(user_id, menu_key);

WITH defaults(menu_key, admin_view, admin_create, admin_update, admin_delete, staff_view, staff_create, staff_update, staff_delete, viewer_view, viewer_create, viewer_update, viewer_delete) AS (
  VALUES
    ('dashboard', true, false, false, false, true, false, false, false, true, false, false, false),
    ('nextstep_marketplace', true, false, false, false, true, false, false, false, false, false, false, false),
    ('shopee_operations', true, true, true, false, true, true, true, false, false, false, false, false),
    ('sale_invoices', true, true, true, true, true, true, true, false, true, false, false, false),
    ('sales_orders', true, true, true, true, true, true, true, false, true, false, false, false),
    ('purchase_orders', true, true, true, true, true, true, true, false, true, false, false, false),
    ('marketplace_aliases', true, false, true, false, true, false, true, false, false, false, false, false),
    ('bulk_send_jobs', true, true, true, false, true, true, true, false, false, false, false, false),
    ('import_shopee', true, true, false, false, true, true, false, false, false, false, false, false),
    ('import_lazada', true, true, false, false, true, true, false, false, false, false, false, false),
    ('import_tiktok', true, true, false, false, true, true, false, false, false, false, false, false),
    ('shopee_settlements', true, true, true, false, true, true, true, false, false, false, false, false),
    ('mappings', true, true, true, true, true, true, true, false, true, false, false, false),
    ('catalog', true, true, true, true, true, true, true, false, true, false, false, false),
    ('messages', true, true, true, false, true, true, true, false, false, false, false, false),
    ('line_notifications', true, true, true, true, false, false, false, false, false, false, false, false),
    ('line_oa', true, true, true, true, false, false, false, false, false, false, false, false),
    ('line_myshop', true, true, true, true, false, false, false, false, false, false, false, false),
    ('quick_replies', true, true, true, true, false, false, false, false, false, false, false, false),
    ('chat_tags', true, true, true, true, false, false, false, false, false, false, false, false),
    ('setup', true, false, true, false, false, false, false, false, false, false, false, false),
    ('channel_defaults', true, true, true, true, false, false, false, false, false, false, false, false),
    ('email_accounts', true, true, true, true, false, false, false, false, false, false, false, false),
    ('shopee_connections', true, true, true, true, false, false, false, false, false, false, false, false),
    ('instance_settings', true, false, true, false, false, false, false, false, false, false, false, false),
    ('settings_users', true, true, true, true, false, false, false, false, false, false, false, false),
    ('settings_menu_permissions', true, false, true, false, false, false, false, false, false, false, false, false),
    ('logs', true, false, false, false, true, false, false, false, false, false, false, false),
    ('ai_usage', true, false, false, false, false, false, false, false, false, false, false, false),
    ('old_data', true, false, true, true, false, false, false, false, false, false, false, false)
)
INSERT INTO user_menu_permissions (
  user_id,
  menu_key,
  can_view,
  can_create,
  can_update,
  can_delete
)
SELECT
  u.id,
  d.menu_key,
  CASE u.role WHEN 'admin' THEN d.admin_view WHEN 'staff' THEN d.staff_view ELSE d.viewer_view END,
  CASE u.role WHEN 'admin' THEN d.admin_create WHEN 'staff' THEN d.staff_create ELSE d.viewer_create END,
  CASE u.role WHEN 'admin' THEN d.admin_update WHEN 'staff' THEN d.staff_update ELSE d.viewer_update END,
  CASE u.role WHEN 'admin' THEN d.admin_delete WHEN 'staff' THEN d.staff_delete ELSE d.viewer_delete END
FROM users u
CROSS JOIN defaults d
ON CONFLICT (user_id, menu_key) DO NOTHING;
