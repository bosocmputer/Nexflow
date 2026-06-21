-- 066_shopee_order_payment_snapshots.sql — cached Shopee order payment breakdowns.
-- The feature is controlled by ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT. UI and
-- LINE notification code read this local snapshot instead of calling Shopee
-- during page render or LINE delivery.

CREATE TABLE IF NOT EXISTS shopee_order_payment_snapshots (
  id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id                    BIGINT NOT NULL,
  order_sn                   TEXT NOT NULL,
  status                     TEXT NOT NULL DEFAULT 'queued'
                             CHECK (status IN ('queued','running','ready','unavailable','failed')),
  buyer_total_amount         NUMERIC(14,2) NOT NULL DEFAULT 0,
  escrow_amount              NUMERIC(14,2) NOT NULL DEFAULT 0,
  original_price             NUMERIC(14,2) NOT NULL DEFAULT 0,
  seller_discount            NUMERIC(14,2) NOT NULL DEFAULT 0,
  shopee_discount            NUMERIC(14,2) NOT NULL DEFAULT 0,
  commission_fee             NUMERIC(14,2) NOT NULL DEFAULT 0,
  service_fee                NUMERIC(14,2) NOT NULL DEFAULT 0,
  seller_transaction_fee     NUMERIC(14,2) NOT NULL DEFAULT 0,
  final_shipping_fee         NUMERIC(14,2) NOT NULL DEFAULT 0,
  actual_shipping_fee        NUMERIC(14,2) NOT NULL DEFAULT 0,
  escrow_tax                 NUMERIC(14,2) NOT NULL DEFAULT 0,
  withholding_tax            NUMERIC(14,2) NOT NULL DEFAULT 0,
  voucher_from_seller        NUMERIC(14,2) NOT NULL DEFAULT 0,
  voucher_from_shopee        NUMERIC(14,2) NOT NULL DEFAULT 0,
  reverse_shipping_fee       NUMERIC(14,2) NOT NULL DEFAULT 0,
  buyer_paid_shipping_fee    NUMERIC(14,2) NOT NULL DEFAULT 0,
  shopee_shipping_rebate     NUMERIC(14,2) NOT NULL DEFAULT 0,
  seller_shipping_discount   NUMERIC(14,2) NOT NULL DEFAULT 0,
  coin                       NUMERIC(14,2) NOT NULL DEFAULT 0,
  raw_escrow                 JSONB NOT NULL DEFAULT '{}'::jsonb,
  attempts                   INT NOT NULL DEFAULT 0,
  next_run_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  locked_at                  TIMESTAMPTZ,
  last_error                 TEXT NOT NULL DEFAULT '',
  last_request_id            TEXT NOT NULL DEFAULT '',
  last_synced_at             TIMESTAMPTZ,
  created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (shop_id, order_sn)
);

CREATE INDEX IF NOT EXISTS shopee_order_payment_snapshots_queue_idx
  ON shopee_order_payment_snapshots(status, next_run_at, created_at)
  WHERE status IN ('queued','failed');

CREATE INDEX IF NOT EXISTS shopee_order_payment_snapshots_order_idx
  ON shopee_order_payment_snapshots(shop_id, order_sn, updated_at DESC);

ALTER TABLE shopee_action_outbox
  DROP CONSTRAINT IF EXISTS shopee_action_outbox_action_check;

ALTER TABLE shopee_action_outbox
  ADD CONSTRAINT shopee_action_outbox_action_check
  CHECK (
    action IN (
      'create_document',
      'erp_send',
      'ship_order',
      'reconcile_shipping',
      'shipping_document_create',
      'shipping_document_result',
      'shipping_document_download',
      'cancel_sml_document',
      'payment_breakdown_refresh'
    )
  );
