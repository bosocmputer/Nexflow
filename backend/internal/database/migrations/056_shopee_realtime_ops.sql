-- 056_shopee_realtime_ops.sql — additive Shopee realtime operations state.
-- The feature is behind ENABLE_SHOPEE_REALTIME_OPS and does not alter the
-- existing Shopee import, saleinvoice, settlement, or SML send flows.

CREATE TABLE IF NOT EXISTS shopee_order_snapshots (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  connection_id           UUID REFERENCES shopee_api_connections(id),
  shop_id                 BIGINT NOT NULL,
  shop_label              TEXT NOT NULL DEFAULT '',
  order_sn                TEXT NOT NULL,
  order_status            TEXT NOT NULL DEFAULT '',
  erp_status              TEXT NOT NULL DEFAULT 'pending'
                          CHECK (erp_status IN (
                            'blocked','pending','pending_erp','needs_review',
                            'sent','failed','cancelled','waiting_shopee'
                          )),
  bill_id                 UUID REFERENCES bills(id),
  sml_doc_no              TEXT NOT NULL DEFAULT '',
  buyer_username          TEXT NOT NULL DEFAULT '',
  total_amount            NUMERIC(14,2) NOT NULL DEFAULT 0,
  currency                TEXT NOT NULL DEFAULT '',
  item_count              INT NOT NULL DEFAULT 0,
  package_number          TEXT NOT NULL DEFAULT '',
  logistics_status        TEXT NOT NULL DEFAULT '',
  tracking_number         TEXT NOT NULL DEFAULT '',
  shipping_carrier        TEXT NOT NULL DEFAULT '',
  payment_method          TEXT NOT NULL DEFAULT '',
  raw_detail              JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_order_update_at    TIMESTAMPTZ,
  last_synced_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error              TEXT NOT NULL DEFAULT '',
  created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, order_sn)
);

CREATE INDEX IF NOT EXISTS shopee_order_snapshots_shop_status_idx
  ON shopee_order_snapshots(shop_id, order_status, updated_at DESC);

CREATE INDEX IF NOT EXISTS shopee_order_snapshots_erp_status_idx
  ON shopee_order_snapshots(erp_status, updated_at DESC);

CREATE INDEX IF NOT EXISTS shopee_order_snapshots_bill_idx
  ON shopee_order_snapshots(bill_id)
  WHERE bill_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS shopee_push_events (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id            BIGINT NOT NULL,
  order_sn           TEXT NOT NULL DEFAULT '',
  push_code          INT NOT NULL DEFAULT 0,
  push_name          TEXT NOT NULL DEFAULT '',
  event_status       TEXT NOT NULL DEFAULT '',
  event_update_time  TIMESTAMPTZ,
  event_timestamp    TIMESTAMPTZ,
  dedupe_key         TEXT NOT NULL UNIQUE,
  raw_payload        JSONB NOT NULL DEFAULT '{}'::jsonb,
  headers            JSONB NOT NULL DEFAULT '{}'::jsonb,
  processing_status  TEXT NOT NULL DEFAULT 'pending'
                     CHECK (processing_status IN ('pending','queued','processed','duplicate','failed')),
  error              TEXT NOT NULL DEFAULT '',
  received_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processed_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS shopee_push_events_shop_received_idx
  ON shopee_push_events(shop_id, received_at DESC);

CREATE INDEX IF NOT EXISTS shopee_push_events_order_idx
  ON shopee_push_events(shop_id, order_sn, received_at DESC);

CREATE TABLE IF NOT EXISTS shopee_reconcile_jobs (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id        BIGINT NOT NULL,
  order_sn       TEXT NOT NULL,
  reason         TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued','running','done','failed')),
  attempts       INT NOT NULL DEFAULT 0,
  next_run_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error     TEXT NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shopee_reconcile_jobs_queue_idx
  ON shopee_reconcile_jobs(status, next_run_at, created_at);

CREATE INDEX IF NOT EXISTS shopee_reconcile_jobs_order_idx
  ON shopee_reconcile_jobs(shop_id, order_sn, created_at DESC);

CREATE TABLE IF NOT EXISTS shopee_action_outbox (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id        BIGINT NOT NULL,
  order_sn       TEXT NOT NULL,
  action         TEXT NOT NULL CHECK (action IN ('erp_send','ship_order')),
  idempotency_key TEXT NOT NULL UNIQUE,
  status         TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','running','done','blocked','failed')),
  bill_id        UUID REFERENCES bills(id),
  sml_doc_no     TEXT NOT NULL DEFAULT '',
  request        JSONB NOT NULL DEFAULT '{}'::jsonb,
  response       JSONB NOT NULL DEFAULT '{}'::jsonb,
  error          TEXT NOT NULL DEFAULT '',
  created_by     UUID REFERENCES users(id),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS shopee_action_outbox_order_idx
  ON shopee_action_outbox(shop_id, order_sn, action, created_at DESC);
