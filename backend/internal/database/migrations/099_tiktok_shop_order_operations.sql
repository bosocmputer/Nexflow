-- Expose the PII-minimized TikTok order snapshot queue to operators. This is
-- read-only presentation state; it does not create Bills or SML documents.

INSERT INTO user_menu_permissions (
  user_id, menu_key, can_view, can_create, can_update, can_delete
)
SELECT id, 'tiktok_shop_operations', role IN ('admin', 'staff'), false, false, false
FROM users
ON CONFLICT (user_id, menu_key) DO NOTHING;

CREATE INDEX IF NOT EXISTS tiktok_shop_order_snapshots_list_idx
  ON tiktok_shop_order_snapshots(last_synced_at DESC, order_id DESC);
