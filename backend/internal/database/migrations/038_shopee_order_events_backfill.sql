-- 038_shopee_order_events_backfill.sql
-- Backfill the two production-supported Shopee order status events from bills
-- already created by the IMAP pipeline. Safe to run repeatedly.

WITH source_bills AS (
  SELECT
    b.id AS bill_id,
    b.source,
    COALESCE(b.raw_data->>'subject', '') AS subject,
    COALESCE(b.raw_data->>'from', '') AS from_addr,
    COALESCE(NULLIF(b.raw_data->>'email_message_id', ''), NULLIF(b.raw_data->>'message_id', ''), 'bill:' || b.id::text) AS message_id,
    CASE
      WHEN COALESCE(b.raw_data->>'email_date', '') <> '' THEN (b.raw_data->>'email_date')::timestamptz
      ELSE NULL
    END AS email_date,
    COALESCE(NULLIF(b.raw_data->>'order_id', ''), NULLIF(b.raw_data->>'shopee_order_id', '')) AS raw_order_id
  FROM bills b
  WHERE b.source IN ('shopee_email', 'shopee_shipped')
),
parsed AS (
  SELECT
    bill_id,
    CASE
      WHEN subject LIKE '%ยืนยันการชำระเงิน%' THEN 'payment_confirmed'
      WHEN subject LIKE '%ถูกจัดส่งแล้ว%' THEN 'shipped'
      ELSE ''
    END AS event_type,
    CASE
      WHEN subject LIKE '%ยืนยันการชำระเงิน%' THEN 'ยืนยันการชำระเงินแล้ว'
      WHEN subject LIKE '%ถูกจัดส่งแล้ว%' THEN 'ถูกจัดส่งแล้ว'
      ELSE ''
    END AS status_label,
    subject,
    from_addr,
    message_id,
    email_date,
    regexp_replace(
      COALESCE(NULLIF(raw_order_id, ''), substring(subject from '#([A-Za-z0-9]+)')),
      '^#',
      ''
    ) AS order_id,
    source
  FROM source_bills
),
deduped AS (
  SELECT DISTINCT ON (message_id, order_id, event_type)
    bill_id,
    order_id,
    event_type,
    status_label,
    subject,
    from_addr,
    message_id,
    email_date,
    source
  FROM parsed
  WHERE event_type <> ''
    AND order_id <> ''
  ORDER BY message_id, order_id, event_type, email_date DESC NULLS LAST, bill_id
)
INSERT INTO shopee_order_events
  (bill_id, order_id, event_type, status_label, subject, from_addr, message_id, email_date, raw_data)
SELECT
  bill_id,
  order_id,
  event_type,
  status_label,
  subject,
  from_addr,
  message_id,
  email_date,
  jsonb_build_object('backfilled', true, 'source', source)
FROM deduped
ON CONFLICT (message_id, order_id, event_type) DO UPDATE
   SET bill_id = COALESCE(shopee_order_events.bill_id, EXCLUDED.bill_id),
       status_label = EXCLUDED.status_label,
       subject = COALESCE(NULLIF(shopee_order_events.subject, ''), EXCLUDED.subject),
       from_addr = COALESCE(NULLIF(shopee_order_events.from_addr, ''), EXCLUDED.from_addr),
       email_date = COALESCE(shopee_order_events.email_date, EXCLUDED.email_date),
       raw_data = shopee_order_events.raw_data || EXCLUDED.raw_data;
