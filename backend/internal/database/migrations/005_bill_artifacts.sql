-- 005_bill_artifacts.sql — store the original source artifact for each bill
-- (PDF binaries, email HTML bodies, envelope JSON, etc.) so users can prove
-- where each bill came from. Files live on disk under ARTIFACTS_DIR; DB
-- only stores metadata + path + integrity hash.

CREATE TABLE IF NOT EXISTS bill_artifacts (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_id       UUID NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,            -- email_pdf, email_html, email_envelope, xlsx, image, audio, chat_history
  filename      TEXT NOT NULL,
  content_type  TEXT,
  size_bytes    BIGINT NOT NULL,
  sha256        TEXT,
  storage_path  TEXT NOT NULL,            -- relative path under ARTIFACTS_DIR
  source_meta   JSONB,                    -- channel-specific: subject/from/message_id/etc.
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bill_artifacts_bill_id ON bill_artifacts(bill_id);
CREATE INDEX IF NOT EXISTS idx_bill_artifacts_kind    ON bill_artifacts(kind);
