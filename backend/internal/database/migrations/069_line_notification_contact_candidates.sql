-- 069_line_notification_contact_candidates.sql
-- Captures LINE destinations that have contacted a configured OA so admins can
-- add them as Shopee notification recipients without manually copying IDs.

CREATE TABLE IF NOT EXISTS line_notification_contact_candidates (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  line_oa_id            UUID NOT NULL REFERENCES line_oa_accounts(id) ON DELETE CASCADE,
  destination_type      TEXT NOT NULL
                          CHECK (destination_type IN ('user','group','room')),
  destination_id        TEXT NOT NULL,
  display_name          TEXT NOT NULL DEFAULT '',
  last_message_preview  TEXT NOT NULL DEFAULT '',
  last_webhook_event_id TEXT NOT NULL DEFAULT '',
  last_seen_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  hidden_at             TIMESTAMPTZ,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (line_oa_id, destination_type, destination_id)
);

CREATE INDEX IF NOT EXISTS line_notification_candidates_oa_seen_idx
  ON line_notification_contact_candidates(line_oa_id, last_seen_at DESC)
  WHERE hidden_at IS NULL;

CREATE INDEX IF NOT EXISTS line_notification_candidates_seen_idx
  ON line_notification_contact_candidates(last_seen_at DESC)
  WHERE hidden_at IS NULL;
