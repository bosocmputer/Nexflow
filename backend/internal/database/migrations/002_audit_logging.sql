-- 002_audit_logging.sql — add structured logging columns to audit_logs

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS level TEXT NOT NULL DEFAULT 'info';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS duration_ms INT;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS trace_id TEXT;

CREATE INDEX IF NOT EXISTS idx_audit_logs_source     ON audit_logs(source);
CREATE INDEX IF NOT EXISTS idx_audit_logs_level      ON audit_logs(level);
CREATE INDEX IF NOT EXISTS idx_audit_logs_trace_id   ON audit_logs(trace_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
