-- 037_data_lifecycle.sql — production data lifecycle and list-query indexes.
-- Keeps hot operational queues fast while preserving summarized old activity.

CREATE TABLE IF NOT EXISTS audit_log_daily_summaries (
  day          DATE NOT NULL,
  source       TEXT NOT NULL DEFAULT '',
  action       TEXT NOT NULL DEFAULT '',
  level        TEXT NOT NULL DEFAULT '',
  count        BIGINT NOT NULL DEFAULT 0,
  error_count  BIGINT NOT NULL DEFAULT 0,
  warn_count   BIGINT NOT NULL DEFAULT 0,
  first_seen_at TIMESTAMPTZ,
  last_seen_at  TIMESTAMPTZ,
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (day, source, action, level)
);

CREATE TABLE IF NOT EXISTS ai_usage_daily_summaries (
  day                 DATE NOT NULL,
  provider            TEXT NOT NULL DEFAULT '',
  model               TEXT NOT NULL DEFAULT '',
  feature             TEXT NOT NULL DEFAULT '',
  operation           TEXT NOT NULL DEFAULT '',
  status              TEXT NOT NULL DEFAULT '',
  requests            BIGINT NOT NULL DEFAULT 0,
  input_tokens        BIGINT NOT NULL DEFAULT 0,
  output_tokens       BIGINT NOT NULL DEFAULT 0,
  total_tokens        BIGINT NOT NULL DEFAULT 0,
  estimated_cost_usd  NUMERIC(14,8) NOT NULL DEFAULT 0,
  avg_duration_ms     NUMERIC(14,2),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (day, provider, model, feature, operation, status)
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_id_desc
  ON audit_logs (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_level_created_id_desc
  ON audit_logs (level, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_action_created_id_desc
  ON audit_logs (action, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_source_created_id_desc
  ON audit_logs (source, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_bills_active_route_status_created_id
  ON bills (document_route, status, created_at DESC, id DESC)
  WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_active_source_type_created_id
  ON bills (source, bill_type, created_at DESC, id DESC)
  WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_archived_route_status_created_id
  ON bills (document_route, status, created_at DESC, id DESC)
  WHERE archived_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_bills_created_id_desc
  ON bills (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_bill_items_bill_id
  ON bill_items (bill_id);

CREATE INDEX IF NOT EXISTS idx_ai_usage_logs_created_id_desc
  ON ai_usage_logs (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_log_daily_summaries_day
  ON audit_log_daily_summaries (day DESC);

CREATE INDEX IF NOT EXISTS idx_ai_usage_daily_summaries_day
  ON ai_usage_daily_summaries (day DESC);
