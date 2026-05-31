-- 014_line_oa_accounts.sql — multi-LINE OA support
--
-- Goal: 1 BillFlow ↔ N LINE OAs (e.g. a chain with 5 stores; one inbox in
-- BillFlow consolidates conversations from all of them).
--
-- Schema changes:
--   1. New table line_oa_accounts: per-OA credentials + name + greeting.
--   2. chat_conversations gains line_oa_id (nullable initially, NULLs are
--      backfilled to a "default" OA seeded from .env LINE_* vars on first
--      boot — see seedDefaultLineOA() in main.go).
--
-- Webhook URL after this migration:
--   /webhook/line/<line_oa_id>  → admin pastes into LINE Developer Console
-- The legacy /webhook/line URL (no OA suffix) still works in this release —
-- it routes to whichever single OA is enabled (back-compat for the existing
-- LINE OA already registered with the old URL). Once admin updates the URL
-- in LINE Console, the legacy fallback can be removed.

CREATE TABLE IF NOT EXISTS line_oa_accounts (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name                  TEXT NOT NULL,                  -- admin label
  channel_secret        TEXT NOT NULL,
  channel_access_token  TEXT NOT NULL,
  bot_user_id           TEXT NOT NULL DEFAULT '',       -- auto-fetched from /v2/bot/info
  admin_user_id         TEXT NOT NULL DEFAULT '',       -- LINE userID for system errors (optional)
  greeting              TEXT NOT NULL DEFAULT '',       -- one-time auto-reply on first contact (optional)
  enabled               BOOLEAN NOT NULL DEFAULT TRUE,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE chat_conversations
  ADD COLUMN IF NOT EXISTS line_oa_id UUID REFERENCES line_oa_accounts(id);

CREATE INDEX IF NOT EXISTS chat_conversations_line_oa_idx
  ON chat_conversations(line_oa_id);
