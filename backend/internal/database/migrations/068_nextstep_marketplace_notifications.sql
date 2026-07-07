-- 068_nextstep_marketplace_notifications.sql
-- Persistent checkpoint for NextStep Marketplace in-app notifications.
-- Keeps polling idempotent across backend restarts while leaving SML read-only.

CREATE TABLE IF NOT EXISTS nextstep_marketplace_notification_seen (
  doc_no        TEXT PRIMARY KEY,
  doc_date      DATE,
  status        TEXT NOT NULL DEFAULT '',
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  notified_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS nextstep_marketplace_notification_seen_date_idx
  ON nextstep_marketplace_notification_seen(doc_date DESC, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS nextstep_marketplace_notification_seen_unnotified_idx
  ON nextstep_marketplace_notification_seen(last_seen_at DESC)
  WHERE notified_at IS NULL;

CREATE TABLE IF NOT EXISTS nextstep_marketplace_notification_state (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
