-- AI usage accounting for OpenRouter requests.
-- Stores token/cost estimates by model + feature so admins can inspect spend
-- inside Nexflow instead of checking provider logs manually.

CREATE TABLE IF NOT EXISTS ai_usage_logs (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider            TEXT NOT NULL DEFAULT 'openrouter',
  model               TEXT NOT NULL,
  feature             TEXT NOT NULL,
  operation           TEXT NOT NULL DEFAULT '',
  bill_id             UUID REFERENCES bills(id) ON DELETE SET NULL,
  input_tokens        INT NOT NULL DEFAULT 0,
  output_tokens       INT NOT NULL DEFAULT 0,
  total_tokens        INT NOT NULL DEFAULT 0,
  estimated_cost_usd  NUMERIC(14,8) NOT NULL DEFAULT 0,
  duration_ms         INT,
  status              TEXT NOT NULL DEFAULT 'success'
                       CHECK (status IN ('success','error')),
  error               TEXT NOT NULL DEFAULT '',
  metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS ai_usage_logs_created_idx
  ON ai_usage_logs (created_at DESC);

CREATE INDEX IF NOT EXISTS ai_usage_logs_model_idx
  ON ai_usage_logs (model, created_at DESC);

CREATE INDEX IF NOT EXISTS ai_usage_logs_feature_idx
  ON ai_usage_logs (feature, created_at DESC);

CREATE INDEX IF NOT EXISTS ai_usage_logs_bill_idx
  ON ai_usage_logs (bill_id)
  WHERE bill_id IS NOT NULL;
