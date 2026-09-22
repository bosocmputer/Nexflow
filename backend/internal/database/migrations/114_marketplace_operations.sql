-- Marketplace Operations is a read-only combined queue.  These indexes keep
-- its cursor scans bounded without changing any source order, bill, or SML data.

CREATE INDEX IF NOT EXISTS shopee_order_snapshots_marketplace_operations_idx
  ON shopee_order_snapshots ((COALESCE(last_order_update_at, updated_at)) DESC, shop_id, order_sn);

CREATE INDEX IF NOT EXISTS tiktok_shop_order_snapshots_marketplace_operations_idx
  ON tiktok_shop_order_snapshots ((COALESCE(last_order_update_at, updated_at)) DESC, shop_id, order_id);

INSERT INTO user_menu_permissions (
  user_id, menu_key, can_view, can_create, can_update, can_delete
)
SELECT
  u.id,
  'marketplace_operations',
  u.role IN ('admin', 'staff'),
  u.role IN ('admin', 'staff'),
  u.role IN ('admin', 'staff'),
  false
FROM users u
ON CONFLICT (user_id, menu_key) DO NOTHING;
