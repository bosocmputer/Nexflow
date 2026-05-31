-- 013_chat_inbox.sql — replace AI chatbot with human chat inbox
--
-- Drops legacy chat_sessions (history/pending_order JSONB; consumed only by the
-- AI conversational sales bot which is being removed). Adds three new tables:
--
--   chat_conversations  — one row per LINE user we've ever talked to
--   chat_messages       — each inbound/outbound/system event (text + media+ system)
--   chat_media          — binary attachments (parallel to bill_artifacts; we keep
--                         the two split because chat media has no bill_id and
--                         retrofitting bill_artifacts.bill_id to NULLABLE would
--                         touch existing prod rows)
--
-- See plan: replace LINE OA AI chatbot with human chat inbox

DROP TABLE IF EXISTS chat_sessions;

CREATE TABLE IF NOT EXISTS chat_conversations (
  line_user_id          TEXT PRIMARY KEY,
  display_name          TEXT NOT NULL DEFAULT '',
  picture_url           TEXT NOT NULL DEFAULT '',
  last_message_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_inbound_at       TIMESTAMPTZ,
  last_admin_reply_at   TIMESTAMPTZ,
  unread_admin_count    INT NOT NULL DEFAULT 0,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS chat_conversations_last_message_idx
  ON chat_conversations(last_message_at DESC);

CREATE INDEX IF NOT EXISTS chat_conversations_unread_idx
  ON chat_conversations(unread_admin_count)
  WHERE unread_admin_count > 0;

CREATE TABLE IF NOT EXISTS chat_messages (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  line_user_id      TEXT NOT NULL REFERENCES chat_conversations(line_user_id) ON DELETE CASCADE,
  direction         TEXT NOT NULL CHECK (direction IN ('incoming','outgoing','system')),
  kind              TEXT NOT NULL CHECK (kind IN ('text','image','file','audio','system')),
  text_content      TEXT NOT NULL DEFAULT '',
  line_message_id   TEXT NOT NULL DEFAULT '',  -- LINE inbound message ID (for content download)
  line_event_ts     BIGINT,                    -- LINE event timestamp ms (tie-break under retry)
  sender_admin_id   UUID REFERENCES users(id),
  delivery_status   TEXT NOT NULL DEFAULT 'sent'
                    CHECK (delivery_status IN ('sent','failed','pending')),
  delivery_error    TEXT NOT NULL DEFAULT '',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS chat_messages_thread_idx
  ON chat_messages(line_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS chat_messages_recent_idx
  ON chat_messages(created_at DESC);

CREATE TABLE IF NOT EXISTS chat_media (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id        UUID NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
  filename          TEXT NOT NULL,
  content_type      TEXT NOT NULL,
  size_bytes        BIGINT NOT NULL,
  sha256            TEXT NOT NULL,
  storage_path      TEXT NOT NULL,             -- relative path under cfg.ArtifactsDir
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS chat_media_message_idx
  ON chat_media(message_id);
