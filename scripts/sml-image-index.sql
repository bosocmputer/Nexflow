-- SML product image lookup index.
--
-- Run this against the SML image database for the active tenant, for example:
--   SML1_2026        -> sml1_2026_images
--   customer_2026    -> customer_2026_images
--
-- This index supports sml-api-bybos image metadata and image stream queries:
--   WHERE TRIM(image_id) = <product_code>
--   ORDER BY COALESCE(image_order, 0), roworder
--
-- Keep CONCURRENTLY: this database can be live while admins sync/view catalog.
-- Do not wrap this file in BEGIN/COMMIT because PostgreSQL rejects
-- CREATE INDEX CONCURRENTLY inside a transaction block.

CREATE INDEX CONCURRENTLY IF NOT EXISTS images_trim_image_id_order_roworder_file_idx
ON public.images (
  (TRIM(image_id)),
  COALESCE(image_order, 0),
  roworder
)
WHERE image_file IS NOT NULL;

ANALYZE public.images;
