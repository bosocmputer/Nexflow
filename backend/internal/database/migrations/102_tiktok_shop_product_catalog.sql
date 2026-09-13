-- Durable, tenant-local TikTok Shop product and inventory snapshots.
-- Authorization credentials and opaque page tokens remain in the Central Gateway.

CREATE TABLE IF NOT EXISTS tiktok_shop_product_catalog_runs (
  id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shop_id                  TEXT NOT NULL REFERENCES tiktok_shop_connections(shop_id),
  status                   TEXT NOT NULL CHECK (status IN ('running','succeeded','failed')),
  trigger_source           TEXT NOT NULL CHECK (trigger_source IN ('manual','schedule')),
  page_count               INTEGER NOT NULL DEFAULT 0 CHECK (page_count >= 0),
  product_count            INTEGER NOT NULL DEFAULT 0 CHECK (product_count >= 0),
  sku_count                INTEGER NOT NULL DEFAULT 0 CHECK (sku_count >= 0),
  warehouse_count          INTEGER NOT NULL DEFAULT 0 CHECK (warehouse_count >= 0),
  upstream_request_ids     JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (JSONB_TYPEOF(upstream_request_ids) = 'array'),
  error_code               TEXT NOT NULL DEFAULT '',
  lease_until              TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '2 hours'),
  started_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at              TIMESTAMPTZ,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS tiktok_shop_product_catalog_runs_active_idx
  ON tiktok_shop_product_catalog_runs(shop_id)
  WHERE status = 'running';

CREATE INDEX IF NOT EXISTS tiktok_shop_product_catalog_runs_history_idx
  ON tiktok_shop_product_catalog_runs(shop_id, started_at DESC);

CREATE TABLE IF NOT EXISTS tiktok_shop_products (
  shop_id                  TEXT NOT NULL REFERENCES tiktok_shop_connections(shop_id),
  product_id               TEXT NOT NULL CHECK (product_id ~ '^[0-9]{1,64}$'),
  title                    TEXT NOT NULL CHECK (BTRIM(title) <> ''),
  status                   TEXT NOT NULL CHECK (BTRIM(status) <> ''),
  source_create_time       BIGINT NOT NULL DEFAULT 0 CHECK (source_create_time >= 0),
  source_update_time       BIGINT NOT NULL DEFAULT 0 CHECK (source_update_time >= 0),
  catalog_run_id           UUID NOT NULL REFERENCES tiktok_shop_product_catalog_runs(id),
  last_seen_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_active                BOOLEAN NOT NULL DEFAULT TRUE,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (shop_id, product_id)
);

CREATE INDEX IF NOT EXISTS tiktok_shop_products_active_idx
  ON tiktok_shop_products(shop_id, product_id)
  WHERE is_active = TRUE;

CREATE TABLE IF NOT EXISTS tiktok_shop_product_skus (
  shop_id                  TEXT NOT NULL,
  product_id               TEXT NOT NULL,
  sku_id                   TEXT NOT NULL CHECK (sku_id ~ '^[0-9]{1,64}$'),
  seller_sku               TEXT NOT NULL DEFAULT '',
  price                    JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (JSONB_TYPEOF(price) = 'object'),
  total_available_quantity BIGINT NOT NULL DEFAULT 0 CHECK (total_available_quantity >= 0),
  total_committed_quantity BIGINT NOT NULL DEFAULT 0 CHECK (total_committed_quantity >= 0),
  catalog_run_id           UUID NOT NULL REFERENCES tiktok_shop_product_catalog_runs(id),
  last_seen_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_active                BOOLEAN NOT NULL DEFAULT TRUE,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (shop_id, product_id, sku_id),
  FOREIGN KEY (shop_id, product_id) REFERENCES tiktok_shop_products(shop_id, product_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS tiktok_shop_product_skus_identity_idx
  ON tiktok_shop_product_skus(shop_id, sku_id);

CREATE INDEX IF NOT EXISTS tiktok_shop_product_skus_active_idx
  ON tiktok_shop_product_skus(shop_id, product_id, sku_id)
  WHERE is_active = TRUE;

CREATE TABLE IF NOT EXISTS tiktok_shop_product_inventory (
  shop_id                  TEXT NOT NULL,
  product_id               TEXT NOT NULL,
  sku_id                   TEXT NOT NULL,
  warehouse_id             TEXT NOT NULL CHECK (warehouse_id ~ '^[0-9]{1,64}$'),
  available_quantity       BIGINT NOT NULL DEFAULT 0 CHECK (available_quantity >= 0),
  committed_quantity       BIGINT NOT NULL DEFAULT 0 CHECK (committed_quantity >= 0),
  catalog_run_id           UUID NOT NULL REFERENCES tiktok_shop_product_catalog_runs(id),
  last_seen_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_active                BOOLEAN NOT NULL DEFAULT TRUE,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (shop_id, product_id, sku_id, warehouse_id),
  FOREIGN KEY (shop_id, product_id, sku_id)
    REFERENCES tiktok_shop_product_skus(shop_id, product_id, sku_id)
);

CREATE INDEX IF NOT EXISTS tiktok_shop_product_inventory_active_idx
  ON tiktok_shop_product_inventory(shop_id, sku_id, warehouse_id)
  WHERE is_active = TRUE;
