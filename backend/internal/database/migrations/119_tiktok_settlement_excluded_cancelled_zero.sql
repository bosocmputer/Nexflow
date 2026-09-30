-- A cancelled TikTok order can remain in the Finance Statement with a zero
-- settlement amount. Keep it as evidence, but exclude it from RC creation
-- rather than blocking a valid Statement for an invoice that cannot exist.

ALTER TABLE tiktok_settlement_items
  DROP CONSTRAINT IF EXISTS tiktok_settlement_items_status_check,
  ADD CONSTRAINT tiktok_settlement_items_status_check
  CHECK (status IN ('pending','ready','excluded','blocked','sending','sent','failed'));
