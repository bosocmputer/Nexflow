-- 071_settings_menu_permissions.sql
-- Add the separated menu-permission settings page to existing production users.

INSERT INTO user_menu_permissions (
  user_id,
  menu_key,
  can_view,
  can_create,
  can_update,
  can_delete
)
SELECT
  id,
  'settings_menu_permissions',
  role = 'admin',
  false,
  role = 'admin',
  false
FROM users
ON CONFLICT (user_id, menu_key) DO NOTHING;
