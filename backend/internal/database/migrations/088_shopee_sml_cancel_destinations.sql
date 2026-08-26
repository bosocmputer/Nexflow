-- 088_shopee_sml_cancel_destinations.sql
-- Normalize only the legacy generic credit-note route. User-selected /void or
-- /cancel routes are left untouched on every replay.

UPDATE channel_defaults
   SET endpoint = '/api/v1/ic/sale-invoices/:doc_no/cancel',
       updated_at = NOW()
 WHERE channel = 'shopee_realtime_cancel'
   AND bill_type = 'sale'
   AND LOWER(TRIM(COALESCE(endpoint, ''))) IN ('creditnote', 'credit_note');
