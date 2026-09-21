ALTER TABLE tiktok_shop_product_skus
    ADD COLUMN IF NOT EXISTS variant_name TEXT NOT NULL DEFAULT '';
