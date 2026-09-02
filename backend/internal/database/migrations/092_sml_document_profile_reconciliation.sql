-- Durable SML Document Profile completion state. Core document success stays
-- independent from repairable profile/audit completion.

ALTER TABLE bill_sml_attempts
  ADD COLUMN IF NOT EXISTS core_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS profile_version TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS profile_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS profile_payload_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS profile_required_checks JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS profile_completed_checks JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS profile_reconciliation_required BOOLEAN NOT NULL DEFAULT FALSE;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'bill_sml_attempts_profile_status_check') THEN
    ALTER TABLE bill_sml_attempts ADD CONSTRAINT bill_sml_attempts_profile_status_check
      CHECK (profile_status IN ('','pending','complete','needs_reconciliation','terminal_failure'));
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS sml_document_profile_reconciliation_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_key TEXT NOT NULL,
  sml_attempt_id UUID NOT NULL REFERENCES bill_sml_attempts(id) ON DELETE RESTRICT,
  profile_version TEXT NOT NULL,
  payload_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','running','retry_wait','complete','terminal_failure','manual_reconciliation')),
  required_checks JSONB NOT NULL DEFAULT '[]'::jsonb,
  completed_checks JSONB NOT NULL DEFAULT '[]'::jsonb,
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 10),
  manual_retry_count INTEGER NOT NULL DEFAULT 0 CHECK (manual_retry_count >= 0),
  max_attempts INTEGER NOT NULL DEFAULT 10 CHECK (max_attempts = 10),
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_token BIGINT NOT NULL DEFAULT 0,
  lease_until TIMESTAMPTZ,
  correlation_id TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ,
  UNIQUE (sml_attempt_id, profile_version)
);

ALTER TABLE sml_document_profile_reconciliation_jobs
  ADD COLUMN IF NOT EXISTS manual_retry_count INTEGER NOT NULL DEFAULT 0 CHECK (manual_retry_count >= 0);

CREATE INDEX IF NOT EXISTS sml_document_profile_reconciliation_queue_idx
  ON sml_document_profile_reconciliation_jobs(status, next_attempt_at, lease_until, created_at)
  WHERE status IN ('queued','retry_wait','running');

CREATE INDEX IF NOT EXISTS sml_document_profile_reconciliation_tenant_idx
  ON sml_document_profile_reconciliation_jobs(tenant_key, status, created_at DESC);
