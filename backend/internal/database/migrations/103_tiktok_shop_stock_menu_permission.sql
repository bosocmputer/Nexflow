-- Prepare the TikTok Shop stock workspace for existing admins only.
-- Runtime feature gates keep the route hidden until read-only Catalog UAT passes.

INSERT INTO user_menu_permissions (
  user_id, menu_key, can_view, can_create, can_update, can_delete
)
SELECT id, 'tiktok_shop_stock', role = 'admin', false, role = 'admin', false
FROM users
ON CONFLICT (user_id, menu_key) DO NOTHING;
