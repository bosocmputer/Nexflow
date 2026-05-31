-- 035_bill_archive.sql — production-safe bill lifecycle.
-- "Archive" is shown to users as "เก็บบิล": hide from daily queues while
-- preserving the bill, SML payload/response, and audit trail for lookup.

ALTER TABLE bills ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
ALTER TABLE bills ADD COLUMN IF NOT EXISTS archived_by UUID REFERENCES users(id);
ALTER TABLE bills ADD COLUMN IF NOT EXISTS archive_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_bills_archived_at ON bills(archived_at);
CREATE INDEX IF NOT EXISTS idx_bills_active_created_at ON bills(created_at DESC) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_bills_archived_created_at ON bills(created_at DESC) WHERE archived_at IS NOT NULL;
