-- 019_line_oa_mark_as_read.sql — opt-in LINE read receipts (Premium-only feature)
--
-- LINE Messaging API supports marking incoming messages as read so the
-- customer sees "อ่านแล้ว" — but the endpoint requires LINE Official Account
-- Plus (paid tier). On the Free OA plan it returns 403, which would spam
-- our error logs every time an admin opens a thread.
--
-- This column is per-OA so admins on Free can leave it OFF and admins who
-- upgraded to OA Plus can flip it ON without affecting other accounts.
-- Default OFF — admin must explicitly enable in /settings/line-oa.

ALTER TABLE line_oa_accounts
  ADD COLUMN IF NOT EXISTS mark_as_read_enabled BOOLEAN NOT NULL DEFAULT FALSE;
