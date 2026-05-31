-- 006_imap_accounts.sql
-- Multi-account IMAP — DB-driven config replacing the .env IMAP_* singleton.
-- Each row is one mailbox the system polls; coordinator spawns one
-- goroutine per enabled row.
--
-- Password is stored plaintext: deployment is internal LAN with restricted
-- DB access. Move to AES-GCM encrypt-at-rest if security model changes.

CREATE TABLE IF NOT EXISTS imap_accounts (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name            TEXT NOT NULL,
  host            TEXT NOT NULL,
  port            INT  NOT NULL DEFAULT 993,
  username        TEXT NOT NULL,
  password        TEXT NOT NULL,
  mailbox         TEXT NOT NULL DEFAULT 'INBOX',

  filter_from     TEXT NOT NULL DEFAULT '',
  filter_subjects TEXT NOT NULL DEFAULT '',

  channel         TEXT NOT NULL DEFAULT 'general'
                  CHECK (channel IN ('general','shopee','lazada')),
  shopee_domains  TEXT NOT NULL DEFAULT '',

  lookback_days   INT  NOT NULL DEFAULT 30
                  CHECK (lookback_days BETWEEN 1 AND 90),
  poll_interval_seconds INT NOT NULL DEFAULT 300
                  CHECK (poll_interval_seconds >= 300),

  enabled         BOOLEAN NOT NULL DEFAULT TRUE,

  -- Runtime status — written by the poller, never the user.
  last_polled_at      TIMESTAMPTZ,
  last_poll_status    TEXT,
  last_poll_error     TEXT,
  last_poll_messages  INT,
  consecutive_failures INT NOT NULL DEFAULT 0,
  last_admin_alert_at TIMESTAMPTZ,

  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_imap_accounts_enabled
  ON imap_accounts(enabled) WHERE enabled = TRUE;
