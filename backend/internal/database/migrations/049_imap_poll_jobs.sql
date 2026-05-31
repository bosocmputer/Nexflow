-- 049_imap_poll_jobs.sql
-- Background progress records for manual IMAP pulls.

CREATE TABLE IF NOT EXISTS imap_poll_jobs (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id     UUID NOT NULL REFERENCES imap_accounts(id) ON DELETE CASCADE,
  status         TEXT NOT NULL DEFAULT 'queued'
                   CHECK (status IN ('queued','running','completed','completed_with_errors','failed')),
  total_count    INT NOT NULL DEFAULT 0,
  scanned_count  INT NOT NULL DEFAULT 0,
  created_count  INT NOT NULL DEFAULT 0,
  skipped_count  INT NOT NULL DEFAULT 0,
  failed_count   INT NOT NULL DEFAULT 0,
  backlog_count  INT NOT NULL DEFAULT 0,
  reason_counts  JSONB NOT NULL DEFAULT '{}'::jsonb,
  latest_details JSONB NOT NULL DEFAULT '[]'::jsonb,
  last_error     TEXT NOT NULL DEFAULT '',
  created_by     UUID REFERENCES users(id),
  created_by_email TEXT NOT NULL DEFAULT '',
  started_at     TIMESTAMPTZ,
  finished_at    TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_imap_poll_jobs_account_status
  ON imap_poll_jobs (account_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_imap_poll_jobs_status_updated
  ON imap_poll_jobs (status, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_imap_poll_jobs_one_active_per_account
  ON imap_poll_jobs (account_id)
  WHERE status IN ('queued','running');
