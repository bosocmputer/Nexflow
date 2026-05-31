-- 017_chat_crm.sql — Phase 4 CRM lite for chat: phone + notes + tags
--
-- Three independent features bundled because they share the inbox UI:
--   4.7 phone — auto-detect Thai phone in incoming text, save against the
--               conversation row so CreateBillPanel can prefill it
--   4.8 notes — admin-only annotations on a conversation; never sent to LINE
--   4.9 tags  — global label set, many-to-many with conversations, used for
--               inbox filtering (VIP / spam / ขายส่ง / etc.)

-- 4.7
ALTER TABLE chat_conversations
  ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';

-- 4.8
CREATE TABLE IF NOT EXISTS chat_notes (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  line_user_id TEXT NOT NULL REFERENCES chat_conversations(line_user_id) ON DELETE CASCADE,
  body         TEXT NOT NULL,
  created_by   UUID REFERENCES users(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS chat_notes_user_idx
  ON chat_notes(line_user_id, created_at DESC);

-- 4.9 — tag list (admin-curated)
CREATE TABLE IF NOT EXISTS chat_tags (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  label      TEXT NOT NULL UNIQUE,
  color      TEXT NOT NULL DEFAULT 'gray',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4.9 — m2m
CREATE TABLE IF NOT EXISTS chat_conversation_tags (
  line_user_id TEXT NOT NULL REFERENCES chat_conversations(line_user_id) ON DELETE CASCADE,
  tag_id       UUID NOT NULL REFERENCES chat_tags(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (line_user_id, tag_id)
);
CREATE INDEX IF NOT EXISTS chat_conversation_tags_tag_idx
  ON chat_conversation_tags(tag_id);
