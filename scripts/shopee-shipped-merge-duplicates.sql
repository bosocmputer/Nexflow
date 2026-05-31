-- Merge duplicate Shopee shipped/payment lifecycle bills into one bill per order.
--
-- Dry-run:
--   psql "$DATABASE_URL" -v apply=false -f scripts/shopee-shipped-merge-duplicates.sql
--
-- Apply:
--   psql "$DATABASE_URL" -v apply=true -f scripts/shopee-shipped-merge-duplicates.sql
--
-- Rules:
-- - canonical bill = earliest active shopee_shipped purchase bill per order_id
-- - merge only when no bill in the duplicate group has been sent/exported to SML
-- - merge only when all bill item signatures in the duplicate group are identical
-- - move events/artifacts/print events to the canonical bill, then archive duplicates

\set ON_ERROR_STOP on
\if :{?apply}
\else
\set apply false
\endif

BEGIN;

CREATE TEMP TABLE shopee_duplicate_merge_plan ON COMMIT DROP AS
WITH active_bills AS (
  SELECT
    b.id,
    UPPER(TRIM(LEADING '#' FROM COALESCE(NULLIF(b.raw_data->>'order_id', ''), NULLIF(b.sml_order_id, ''), ''))) AS order_id,
    b.created_at,
    b.status,
    COALESCE(b.sml_doc_no, '') AS sml_doc_no,
    b.sent_at,
    COALESCE((
      SELECT jsonb_agg(
               jsonb_build_object(
                 'raw_name', TRIM(bi.raw_name),
                 'source_sku', COALESCE(bi.source_sku, ''),
                 'item_code', COALESCE(bi.item_code, ''),
                 'unit_code', COALESCE(bi.unit_code, ''),
                 'qty', bi.qty::text,
                 'price', COALESCE(bi.price, 0)::text
               )
               ORDER BY TRIM(bi.raw_name), COALESCE(bi.source_sku, ''), COALESCE(bi.item_code, ''),
                        COALESCE(bi.unit_code, ''), bi.qty, COALESCE(bi.price, 0)
             )
        FROM bill_items bi
       WHERE bi.bill_id = b.id
    ), '[]'::jsonb) AS item_signature,
    (SELECT COUNT(*) FROM shopee_order_events soe WHERE soe.bill_id = b.id) AS event_count,
    (SELECT COUNT(*) FROM bill_artifacts ba WHERE ba.bill_id = b.id) AS artifact_count,
    (SELECT COUNT(*) FROM email_print_events epe WHERE epe.bill_id = b.id) AS print_event_count
  FROM bills b
  WHERE b.archived_at IS NULL
    AND b.source = 'shopee_shipped'
    AND b.bill_type = 'purchase'
), duplicate_orders AS (
  SELECT order_id
  FROM active_bills
  WHERE order_id <> ''
  GROUP BY order_id
  HAVING COUNT(*) > 1
), ranked AS (
  SELECT
    ab.*,
    FIRST_VALUE(ab.id) OVER (PARTITION BY ab.order_id ORDER BY ab.created_at ASC, ab.id ASC) AS canonical_id,
    ROW_NUMBER() OVER (PARTITION BY ab.order_id ORDER BY ab.created_at ASC, ab.id ASC) AS rn
  FROM active_bills ab
  JOIN duplicate_orders d ON d.order_id = ab.order_id
), group_stats AS (
  SELECT
    order_id,
    BOOL_OR(status = 'sent' OR sent_at IS NOT NULL OR TRIM(sml_doc_no) <> '') AS has_sent_or_sml_doc,
    COUNT(DISTINCT item_signature::text) AS item_signature_count,
    COUNT(*) AS bill_count
  FROM ranked
  GROUP BY order_id
)
SELECT
  r.order_id,
  r.canonical_id,
  r.id AS duplicate_id,
  gs.bill_count,
  CASE
    WHEN gs.has_sent_or_sml_doc THEN 'skipped_sent'
    WHEN gs.item_signature_count <> 1 THEN 'skipped_item_mismatch'
    ELSE 'mergeable'
  END AS merge_status,
  r.status AS duplicate_status,
  r.created_at AS duplicate_created_at,
  r.event_count,
  r.artifact_count,
  r.print_event_count
FROM ranked r
JOIN group_stats gs ON gs.order_id = r.order_id
WHERE r.rn > 1;

SELECT
  merge_status,
  COUNT(DISTINCT order_id) AS duplicate_order_groups,
  COUNT(*) AS duplicate_bills,
  COALESCE(SUM(event_count), 0) AS events_to_move,
  COALESCE(SUM(artifact_count), 0) AS artifacts_to_move,
  COALESCE(SUM(print_event_count), 0) AS print_events_to_move
FROM shopee_duplicate_merge_plan
GROUP BY merge_status
ORDER BY merge_status;

SELECT
  order_id,
  canonical_id,
  duplicate_id,
  merge_status,
  duplicate_status,
  duplicate_created_at,
  event_count,
  artifact_count,
  print_event_count
FROM shopee_duplicate_merge_plan
ORDER BY merge_status, order_id, duplicate_created_at
LIMIT 30;

\if :apply

CREATE TEMP TABLE moved_shopee_events ON COMMIT DROP AS
WITH moved AS (
  UPDATE shopee_order_events soe
     SET bill_id = p.canonical_id
    FROM shopee_duplicate_merge_plan p
   WHERE p.merge_status = 'mergeable'
     AND soe.bill_id = p.duplicate_id
  RETURNING p.order_id, p.canonical_id, p.duplicate_id, soe.id
)
SELECT * FROM moved;

CREATE TEMP TABLE moved_bill_artifacts ON COMMIT DROP AS
WITH moved AS (
  UPDATE bill_artifacts ba
     SET bill_id = p.canonical_id
    FROM shopee_duplicate_merge_plan p
   WHERE p.merge_status = 'mergeable'
     AND ba.bill_id = p.duplicate_id
  RETURNING p.order_id, p.canonical_id, p.duplicate_id, ba.id
)
SELECT * FROM moved;

CREATE TEMP TABLE moved_email_print_events ON COMMIT DROP AS
WITH moved AS (
  UPDATE email_print_events epe
     SET bill_id = p.canonical_id
    FROM shopee_duplicate_merge_plan p
   WHERE p.merge_status = 'mergeable'
     AND epe.bill_id = p.duplicate_id
  RETURNING p.order_id, p.canonical_id, p.duplicate_id, epe.id
)
SELECT * FROM moved;

CREATE TEMP TABLE archived_duplicate_bills ON COMMIT DROP AS
WITH archived AS (
  UPDATE bills b
     SET archived_at = NOW(),
         archived_by = NULL,
         archive_reason = 'merged_duplicate_shopee_order:' || p.canonical_id::text
    FROM shopee_duplicate_merge_plan p
   WHERE p.merge_status = 'mergeable'
     AND b.id = p.duplicate_id
     AND b.archived_at IS NULL
  RETURNING p.order_id, p.canonical_id, p.duplicate_id, b.id
)
SELECT * FROM archived;

INSERT INTO audit_logs (action, target_id, source, level, detail)
SELECT
  'shopee_duplicate_merged',
  p.duplicate_id,
  'system',
  'info',
  jsonb_build_object(
    'order_id', p.order_id,
    'canonical_bill_id', p.canonical_id,
    'duplicate_bill_id', p.duplicate_id,
    'events_moved', (SELECT COUNT(*) FROM moved_shopee_events m WHERE m.duplicate_id = p.duplicate_id),
    'artifacts_moved', (SELECT COUNT(*) FROM moved_bill_artifacts m WHERE m.duplicate_id = p.duplicate_id),
    'print_events_moved', (SELECT COUNT(*) FROM moved_email_print_events m WHERE m.duplicate_id = p.duplicate_id),
    'archive_reason', 'merged_duplicate_shopee_order:' || p.canonical_id::text
  )
FROM shopee_duplicate_merge_plan p
WHERE p.merge_status = 'mergeable';

SELECT 'applied' AS result,
       (SELECT COUNT(*) FROM archived_duplicate_bills) AS archived_duplicate_bills,
       (SELECT COUNT(*) FROM moved_shopee_events) AS moved_events,
       (SELECT COUNT(*) FROM moved_bill_artifacts) AS moved_artifacts,
       (SELECT COUNT(*) FROM moved_email_print_events) AS moved_print_events;

COMMIT;

\else

SELECT 'dry_run_only' AS result,
       (SELECT COUNT(*) FROM shopee_duplicate_merge_plan WHERE merge_status = 'mergeable') AS mergeable_duplicate_bills,
       (SELECT COUNT(*) FROM shopee_duplicate_merge_plan WHERE merge_status <> 'mergeable') AS skipped_duplicate_bills;

ROLLBACK;

\endif
