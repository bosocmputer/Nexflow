-- 018_chat_reply_token.sql — Hybrid Reply + Push API support
--
-- LINE replyToken from inbound webhook events is single-use and free (Reply
-- API doesn't count against the 200/month Free OA quota). Admin replies in
-- BillFlow today always go through Push API → quota burns fast.
--
-- This migration stores the latest replyToken per conversation so admin
-- replies can try Reply first, fallback to Push if expired/consumed.
--
-- Per-conversation (not per-message) because admin always replies to the
-- most recent inbound — newer tokens always overwrite older ones, and
-- atomic UPDATE...WHERE...!='' RETURNING handles concurrent admin races.
--
-- chat_messages.delivery_method records which path each outgoing message
-- went through, for the dashboard "X reply (free) / Y push (counted)" stat.

ALTER TABLE chat_conversations
  ADD COLUMN IF NOT EXISTS last_reply_token TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_reply_token_at TIMESTAMPTZ;

ALTER TABLE chat_messages
  ADD COLUMN IF NOT EXISTS delivery_method TEXT NOT NULL DEFAULT 'push'
    CHECK (delivery_method IN ('reply','push'));
