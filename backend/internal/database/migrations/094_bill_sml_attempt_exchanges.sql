-- Bounded, per-request evidence for immutable SML sends. This table is
-- intentionally independent from bill_sml_attempts state so diagnostics can
-- fail without changing a business result or causing a document resend.

CREATE TABLE IF NOT EXISTS bill_sml_attempt_exchanges (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sml_attempt_id UUID NOT NULL REFERENCES bill_sml_attempts(id) ON DELETE RESTRICT,
  exchange_sequence INTEGER NOT NULL CHECK (exchange_sequence > 0),
  trace_id TEXT NOT NULL DEFAULT '',
  route TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'started'
    CHECK (status IN ('started','succeeded','failed','unknown')),
  request_method TEXT NOT NULL,
  request_path TEXT NOT NULL,
  request_content_type TEXT NOT NULL DEFAULT '',
  correlation_id TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  duration_ms BIGINT CHECK (duration_ms IS NULL OR duration_ms >= 0),
  http_status INTEGER CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
  response_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  response_json JSONB,
  response_hash TEXT NOT NULL DEFAULT '',
  response_size BIGINT NOT NULL DEFAULT 0 CHECK (response_size >= 0),
  response_truncated BOOLEAN NOT NULL DEFAULT FALSE,
  error_code TEXT NOT NULL DEFAULT '',
  error_class TEXT NOT NULL DEFAULT '',
  safe_error_summary TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (sml_attempt_id, exchange_sequence),
  CHECK (request_path LIKE '/%' AND POSITION('://' IN request_path) = 0 AND POSITION('?' IN request_path) = 0)
);

CREATE INDEX IF NOT EXISTS bill_sml_attempt_exchanges_attempt_created_idx
  ON bill_sml_attempt_exchanges(sml_attempt_id, created_at);
