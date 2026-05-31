-- 016_chat_conversation_status.sql — Phase 4.2 conversation lifecycle status
--
-- Adds status to chat_conversations so admins can mark threads as "resolved"
-- (✓ ปิดเรื่อง) and keep the inbox uncluttered, with auto-revive when the
-- customer messages again. 'archived' is sticky for spam/blocked threads.

ALTER TABLE chat_conversations
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open','resolved','archived'));

CREATE INDEX IF NOT EXISTS chat_conversations_status_idx
  ON chat_conversations(status, last_message_at DESC);
