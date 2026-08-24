-- 083_shopee_auto_sml_document_time.sql
-- Persist the Bangkok document time immediately before the first SML write.
-- Retrying the same durable job must reuse this value so an uncertain write
-- cannot become a different payload solely because wall-clock time advanced.

ALTER TABLE shopee_auto_sml_jobs
  ADD COLUMN IF NOT EXISTS document_time VARCHAR(5) NOT NULL DEFAULT '';
