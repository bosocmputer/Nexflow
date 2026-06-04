# SML Product Image DB Maintenance

## Purpose

Nexflow stores only SML image metadata in `sml_catalog`. Actual image bytes stay in the SML image database and are lazy-loaded through:

- `GET /api/catalog/:code/image`
- `GET /api/catalog/:code/images`
- `GET /api/catalog/:code/images/:roworder`

The SML API matches product images by:

```sql
TRIM(image_id) = product_code
```

Because of that `TRIM(image_id)` expression, every SML image database should have the expression index below before catalog images grow beyond a small test set.

## Image Database Naming

Use the tenant database name and append `_images` in lowercase:

| Product DB | Image DB |
|---|---|
| `SML1_2026` | `sml1_2026_images` |
| `SML2_2026` | `sml2_2026_images` |
| `CUSTOMER_2026` | `customer_2026_images` |

## Apply The Index

Preferred script:

```bash
SML_PGHOST=192.168.2.248 \
SML_PGUSER=postgres \
SML_PGPASSWORD='***' \
SML_TENANT=SML1_2026 \
scripts/apply-sml-image-index.sh
```

This mode passes the password through `PGPASSWORD`, not as a `psql` command argument.

If `psql` is only available inside the Nexflow PostgreSQL container on the server:

```bash
PSQL_BIN='docker exec -i -e PGPASSWORD nexflow-postgres psql' \
SML_PGHOST=192.168.2.248 \
SML_PGUSER=postgres \
SML_PGPASSWORD='***' \
SML_TENANT=SML1_2026 \
scripts/apply-sml-image-index.sh
```

Or provide a full connection string:

```bash
SML_IMAGE_DATABASE_URL='postgresql://postgres:***@192.168.2.248:5432/sml1_2026_images?sslmode=disable' \
scripts/apply-sml-image-index.sh
```

The script runs [scripts/sml-image-index.sql](../scripts/sml-image-index.sql):

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS images_trim_image_id_order_roworder_file_idx
ON public.images (
  (TRIM(image_id)),
  COALESCE(image_order, 0),
  roworder
)
WHERE image_file IS NOT NULL;

ANALYZE public.images;
```

## When To Run

Run this whenever:

- moving SML to a new PostgreSQL server;
- creating a new tenant/database;
- restoring only data without indexes;
- creating a new image database manually;
- changing Nexflow `/settings/instance` to a new `Database (tenant)`.

If the migration uses a full `pg_dump` / `pg_restore` of schema and data, the index should already come along. Still verify it.

## Verify

```sql
SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public'
  AND tablename = 'images'
  AND indexname = 'images_trim_image_id_order_roworder_file_idx';
```

Optional query-plan check:

```sql
SET enable_seqscan = off;
EXPLAIN
SELECT roworder, image_order, guid_code, octet_length(image_file)
FROM public.images
WHERE TRIM(image_id) = 'BF0004'
  AND image_file IS NOT NULL
ORDER BY COALESCE(image_order, 0), roworder
LIMIT 10;
RESET enable_seqscan;
```

Expected: `Index Scan using images_trim_image_id_order_roworder_file_idx`.

## Nexflow Cutover Checklist

1. Go to `/settings/instance`.
2. Change `SML REST URL` and/or `Database (tenant)`.
3. Run `scripts/apply-sml-image-index.sh` for the target image DB.
4. Click `ทดสอบค่าที่กรอกอยู่`.
5. Save and let backend restart.
6. Go to `/settings/catalog`.
7. Sync catalog from SML.
8. Search a product known to have images, for example `BF00002` or `BF0004`.
9. Open image preview and confirm thumbnails/gallery work.

## Production Notes

- Keep `CREATE INDEX CONCURRENTLY`; avoid table write blocking.
- Do not run inside `BEGIN/COMMIT`.
- Do not copy binary images into Nexflow PostgreSQL for v1.
- If the image DB is unavailable, Nexflow should continue syncing products with `image_count=0`.
- If images feel slow later, add a dedicated thumbnail cache instead of returning base64 in catalog/search JSON.
