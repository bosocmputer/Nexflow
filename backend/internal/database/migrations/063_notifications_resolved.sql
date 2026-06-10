-- 063_notifications_resolved.sql
-- Add a resolved lifecycle so transient Shopee/API issues stop appearing as
-- active work after the shop/token recovers.

ALTER TABLE notifications
  ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ;

ALTER TABLE notifications
  ADD COLUMN IF NOT EXISTS resolved_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS notifications_recipient_active_unread_idx
  ON notifications(recipient_id, created_at DESC)
  WHERE read_at IS NULL AND resolved_at IS NULL;

CREATE INDEX IF NOT EXISTS notifications_recipient_active_created_idx
  ON notifications(recipient_id, created_at DESC)
  WHERE resolved_at IS NULL;

CREATE INDEX IF NOT EXISTS notifications_shop_issue_active_idx
  ON notifications(entity_type, entity_id, created_at DESC)
  WHERE resolved_at IS NULL
    AND entity_type = 'shopee_shop';

UPDATE notifications n
   SET resolved_at = COALESCE(n.resolved_at, NOW()),
       resolved_reason = CASE
         WHEN COALESCE(n.resolved_reason, '') = '' THEN 'shop sync recovered before migration'
         ELSE n.resolved_reason
       END,
       updated_at = NOW()
  FROM shopee_api_connections c
 WHERE n.resolved_at IS NULL
   AND n.entity_type = 'shopee_shop'
   AND n.entity_id = c.shop_id::text
   AND c.last_sync_status = 'ok'
   AND COALESCE(c.last_sync_error, '') = ''
   AND (
     n.dedupe_key LIKE ('shopee:sync_error:' || c.shop_id::text || ':%')
     OR n.dedupe_key LIKE ('shopee:token_error:' || c.shop_id::text || ':%')
   );
