ALTER TABLE sml_catalog
  ADD COLUMN IF NOT EXISTS image_count INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS primary_image_roworder INT,
  ADD COLUMN IF NOT EXISTS primary_image_guid TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS primary_image_bytes BIGINT,
  ADD COLUMN IF NOT EXISTS image_synced_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sml_catalog_has_images
  ON sml_catalog (item_code)
  WHERE image_count > 0;
