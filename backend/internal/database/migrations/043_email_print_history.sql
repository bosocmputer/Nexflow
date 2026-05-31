-- 043_email_print_history.sql
-- Track print requests for original email artifacts and speed up email metadata search.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS email_print_events (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_id            UUID NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
  artifact_id        UUID REFERENCES bill_artifacts(id) ON DELETE SET NULL,
  email_message_id   TEXT NOT NULL,
  email_group_key    TEXT NOT NULL,
  subject            TEXT NOT NULL DEFAULT '',
  from_addr          TEXT NOT NULL DEFAULT '',
  requested_by       UUID REFERENCES users(id),
  requested_by_email TEXT NOT NULL DEFAULT '',
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_print_events_message_created
  ON email_print_events (email_message_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_email_print_events_bill_created
  ON email_print_events (bill_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_bills_raw_email_subject_trgm
  ON bills USING GIN ((raw_data->>'subject') gin_trgm_ops)
  WHERE raw_data ? 'subject';

CREATE INDEX IF NOT EXISTS idx_bills_raw_email_from_trgm
  ON bills USING GIN ((raw_data->>'from') gin_trgm_ops)
  WHERE raw_data ? 'from';

CREATE INDEX IF NOT EXISTS idx_bills_raw_email_message_id_trgm
  ON bills USING GIN ((raw_data->>'email_message_id') gin_trgm_ops)
  WHERE raw_data ? 'email_message_id';

CREATE INDEX IF NOT EXISTS idx_bills_raw_message_id_trgm
  ON bills USING GIN ((raw_data->>'message_id') gin_trgm_ops)
  WHERE raw_data ? 'message_id';
