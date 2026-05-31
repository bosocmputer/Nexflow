-- 044_sml_bulk_jobs.sql
-- DB-backed async bulk SML send jobs with item-level progress and retry.

CREATE TABLE IF NOT EXISTS sml_bulk_jobs (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  client_request_id  TEXT NOT NULL UNIQUE,
  status             TEXT NOT NULL DEFAULT 'queued'
                       CHECK (status IN ('queued','running','completed','completed_with_errors','failed')),
  source             TEXT NOT NULL DEFAULT '',
  bill_type          TEXT NOT NULL CHECK (bill_type IN ('purchase','sale')),
  document_route     TEXT NOT NULL DEFAULT '',
  title              TEXT NOT NULL DEFAULT '',
  request_payload    JSONB NOT NULL DEFAULT '{}'::jsonb,
  filter_snapshot    JSONB NOT NULL DEFAULT '{}'::jsonb,
  total_count        INT NOT NULL DEFAULT 0,
  sent_count         INT NOT NULL DEFAULT 0,
  failed_count       INT NOT NULL DEFAULT 0,
  skipped_count      INT NOT NULL DEFAULT 0,
  created_by         UUID REFERENCES users(id),
  created_by_email   TEXT NOT NULL DEFAULT '',
  last_error         TEXT NOT NULL DEFAULT '',
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  started_at         TIMESTAMPTZ,
  finished_at        TIMESTAMPTZ,
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sml_bulk_job_items (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id           UUID NOT NULL REFERENCES sml_bulk_jobs(id) ON DELETE CASCADE,
  bill_id          UUID NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
  sequence         INT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'queued'
                     CHECK (status IN ('queued','running','sent','failed','skipped')),
  order_no         TEXT NOT NULL DEFAULT '',
  doc_no_attempted TEXT NOT NULL DEFAULT '',
  doc_no           TEXT NOT NULL DEFAULT '',
  error            TEXT NOT NULL DEFAULT '',
  attempts         INT NOT NULL DEFAULT 0,
  started_at       TIMESTAMPTZ,
  finished_at      TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (job_id, bill_id),
  UNIQUE (job_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_sml_bulk_jobs_status_created
  ON sml_bulk_jobs (status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_sml_bulk_job_items_job_sequence
  ON sml_bulk_job_items (job_id, sequence);

CREATE INDEX IF NOT EXISTS idx_sml_bulk_job_items_bill_active
  ON sml_bulk_job_items (bill_id, status)
  WHERE status IN ('queued','running');
