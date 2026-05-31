-- 050_shopee_settlements.sql — Shopee payout settlement -> SML AR receipt

CREATE TABLE IF NOT EXISTS shopee_settlement_defaults (
  id                  INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  doc_format_code     TEXT NOT NULL DEFAULT '',
  passbook_code       TEXT NOT NULL DEFAULT '',
  passbook_name       TEXT NOT NULL DEFAULT '',
  bank_code           TEXT NOT NULL DEFAULT '',
  bank_branch         TEXT NOT NULL DEFAULT '',
  expense_code        TEXT NOT NULL DEFAULT '',
  expense_name        TEXT NOT NULL DEFAULT '',
  updated_by          UUID REFERENCES users(id),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO shopee_settlement_defaults (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS shopee_settlement_runs (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  connection_id          UUID REFERENCES shopee_api_connections(id),
  shop_id                BIGINT NOT NULL,
  shop_label             TEXT NOT NULL DEFAULT '',
  release_time_from      TIMESTAMPTZ NOT NULL,
  release_time_to        TIMESTAMPTZ NOT NULL,
  status                 TEXT NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending','running','ready','sending','sent','failed','partial')),
  total_count            INT NOT NULL DEFAULT 0,
  ready_count            INT NOT NULL DEFAULT 0,
  blocked_count          INT NOT NULL DEFAULT 0,
  sent_count             INT NOT NULL DEFAULT 0,
  rc_doc_no              TEXT NOT NULL DEFAULT '',
  error_msg              TEXT NOT NULL DEFAULT '',
  selected_doc_format_code TEXT NOT NULL DEFAULT '',
  selected_passbook_code TEXT NOT NULL DEFAULT '',
  selected_passbook_name TEXT NOT NULL DEFAULT '',
  selected_bank_code     TEXT NOT NULL DEFAULT '',
  selected_bank_branch   TEXT NOT NULL DEFAULT '',
  selected_expense_code  TEXT NOT NULL DEFAULT '',
  selected_expense_name  TEXT NOT NULL DEFAULT '',
  created_by             UUID REFERENCES users(id),
  created_by_email       TEXT NOT NULL DEFAULT '',
  started_at             TIMESTAMPTZ,
  finished_at            TIMESTAMPTZ,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shopee_settlement_runs_status_idx
  ON shopee_settlement_runs(status, created_at DESC);

CREATE INDEX IF NOT EXISTS shopee_settlement_runs_shop_created_idx
  ON shopee_settlement_runs(shop_id, created_at DESC);

CREATE TABLE IF NOT EXISTS shopee_settlement_items (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id                UUID NOT NULL REFERENCES shopee_settlement_runs(id) ON DELETE CASCADE,
  shop_id               BIGINT NOT NULL,
  order_sn              TEXT NOT NULL,
  escrow_release_time   TIMESTAMPTZ,
  payout_amount         NUMERIC(14,2) NOT NULL DEFAULT 0,
  escrow_amount         NUMERIC(14,2) NOT NULL DEFAULT 0,
  buyer_total_amount    NUMERIC(14,2) NOT NULL DEFAULT 0,
  invoice_doc_no        TEXT NOT NULL DEFAULT '',
  invoice_doc_date      DATE,
  cust_code             TEXT NOT NULL DEFAULT '',
  invoice_amount        NUMERIC(14,2) NOT NULL DEFAULT 0,
  difference_amount     NUMERIC(14,2) NOT NULL DEFAULT 0,
  status                TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','ready','blocked','sending','sent','skipped','failed')),
  block_reason          TEXT NOT NULL DEFAULT '',
  receipt_doc_no        TEXT NOT NULL DEFAULT '',
  existing_receipt_doc_no TEXT NOT NULL DEFAULT '',
  raw_escrow            JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shopee_settlement_items_run_idx
  ON shopee_settlement_items(run_id, order_sn);

CREATE INDEX IF NOT EXISTS shopee_settlement_items_order_idx
  ON shopee_settlement_items(shop_id, order_sn);

CREATE UNIQUE INDEX IF NOT EXISTS shopee_settlement_items_sent_once_idx
  ON shopee_settlement_items(shop_id, order_sn)
  WHERE status = 'sent';
