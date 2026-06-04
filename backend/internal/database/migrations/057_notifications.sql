-- 057_notifications.sql — in-app notification persistence.
-- Additive only. Notifications are user-scoped so each operator can read/clear
-- independently while backend events remain deduped.

CREATE TABLE IF NOT EXISTS notifications (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  recipient_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source       TEXT NOT NULL DEFAULT 'system',
  severity     TEXT NOT NULL DEFAULT 'info'
               CHECK (severity IN ('info','warning','error')),
  title        TEXT NOT NULL,
  body         TEXT NOT NULL DEFAULT '',
  action_url   TEXT NOT NULL DEFAULT '',
  entity_type  TEXT NOT NULL DEFAULT '',
  entity_id    TEXT NOT NULL DEFAULT '',
  dedupe_key   TEXT NOT NULL,
  read_at      TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (recipient_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS notifications_recipient_unread_idx
  ON notifications(recipient_id, created_at DESC)
  WHERE read_at IS NULL;

CREATE INDEX IF NOT EXISTS notifications_recipient_created_idx
  ON notifications(recipient_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notifications_entity_idx
  ON notifications(source, entity_type, entity_id, created_at DESC);
