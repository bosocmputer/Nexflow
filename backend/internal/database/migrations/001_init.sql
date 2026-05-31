-- 001_init.sql — BillFlow initial schema

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Users & Auth
CREATE TABLE IF NOT EXISTS users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email         TEXT UNIQUE NOT NULL,
  name          TEXT NOT NULL,
  role          TEXT NOT NULL CHECK (role IN ('admin','staff','viewer')),
  password_hash TEXT NOT NULL,
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Item Mapping
CREATE TABLE IF NOT EXISTS mappings (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  raw_name             TEXT NOT NULL,
  item_code            TEXT NOT NULL,
  unit_code            TEXT NOT NULL,
  confidence           FLOAT DEFAULT 1.0,
  source               TEXT DEFAULT 'manual' CHECK (source IN ('manual','ai_learned')),
  usage_count          INT DEFAULT 0,
  last_used_at         TIMESTAMPTZ,
  learned_from_bill_id UUID,
  created_by           UUID REFERENCES users(id),
  created_at           TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(raw_name)
);

-- Bills
CREATE TABLE IF NOT EXISTS bills (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_type     TEXT NOT NULL CHECK (bill_type IN ('sale','purchase')),
  source        TEXT NOT NULL CHECK (source IN ('line','email','lazada','shopee','manual')),
  status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','confirmed','sent','failed','skipped')),
  raw_data      JSONB,
  sml_doc_no    TEXT,
  sml_payload   JSONB,
  sml_response  JSONB,
  ai_confidence FLOAT,
  anomalies     JSONB DEFAULT '[]',
  error_msg     TEXT,
  created_by    UUID REFERENCES users(id),
  created_at    TIMESTAMPTZ DEFAULT NOW(),
  sent_at       TIMESTAMPTZ
);

-- Bill Items
CREATE TABLE IF NOT EXISTS bill_items (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_id    UUID REFERENCES bills(id) ON DELETE CASCADE,
  raw_name   TEXT NOT NULL,
  item_code  TEXT,
  qty        NUMERIC NOT NULL,
  unit_code  TEXT,
  price      NUMERIC,
  mapped     BOOLEAN DEFAULT FALSE,
  mapping_id UUID REFERENCES mappings(id)
);

-- F1: Mapping Feedback
CREATE TABLE IF NOT EXISTS mapping_feedback (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_item_id   UUID REFERENCES bill_items(id),
  original_match TEXT,
  corrected_to   TEXT,
  corrected_by   UUID REFERENCES users(id),
  created_at     TIMESTAMPTZ DEFAULT NOW()
);

-- Audit Log
CREATE TABLE IF NOT EXISTS audit_logs (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID REFERENCES users(id),
  action     TEXT NOT NULL,
  target_id  UUID,
  detail     JSONB,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- F2: Item Price History
CREATE TABLE IF NOT EXISTS item_price_history (
  item_code    TEXT PRIMARY KEY,
  avg_price    NUMERIC,
  min_price    NUMERIC,
  max_price    NUMERIC,
  sample_count INT DEFAULT 0,
  last_updated TIMESTAMPTZ DEFAULT NOW()
);

-- F4: Daily AI Insights
CREATE TABLE IF NOT EXISTS daily_insights (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  date       DATE UNIQUE NOT NULL,
  stats_json JSONB,
  insight    TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Platform Column Mapping (Lazada/Shopee — admin config)
CREATE TABLE IF NOT EXISTS platform_column_mappings (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  platform     TEXT NOT NULL CHECK (platform IN ('lazada','shopee')),
  field_name   TEXT NOT NULL,
  column_name  TEXT NOT NULL,
  updated_by   UUID REFERENCES users(id),
  updated_at   TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(platform, field_name)
);

-- LINE Chat Sessions (persistent conversation state per LINE user)
CREATE TABLE IF NOT EXISTS chat_sessions (
  line_user_id   TEXT PRIMARY KEY,
  history        JSONB NOT NULL DEFAULT '[]',
  pending_order  JSONB,
  last_active    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_bills_status ON bills(status);
CREATE INDEX IF NOT EXISTS idx_bills_created_at ON bills(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bills_source ON bills(source);
CREATE INDEX IF NOT EXISTS idx_bill_items_bill_id ON bill_items(bill_id);
CREATE INDEX IF NOT EXISTS idx_mappings_raw_name ON mappings(raw_name);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_last_active ON chat_sessions(last_active);
