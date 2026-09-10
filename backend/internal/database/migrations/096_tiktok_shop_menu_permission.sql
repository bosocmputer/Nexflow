-- Make the TikTok Shop connection screen visible to existing admins only.

INSERT INTO user_menu_permissions (
  user_id, menu_key, can_view, can_create, can_update, can_delete
)
SELECT id, 'tiktok_shop_connections', role = 'admin', role = 'admin', false, false
FROM users
ON CONFLICT (user_id, menu_key) DO NOTHING;
