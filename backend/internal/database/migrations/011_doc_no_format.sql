-- 011_doc_no_format.sql — configurable doc_no per channel + atomic counter
--
-- Background: SML's saleorder UI silently fails to display docs whose doc_no
-- matches the YYYYMMDD-hex pattern (e.g. "BF-SO-20260428-aabb1122"). Letting
-- admins set their own prefix + running format avoids the trap and aligns
-- BillFlow's doc_no with whatever convention each customer's SML expects.
--
-- doc_running_format tokens:
--   YYYY  → 4-digit year     (e.g. 2026)
--   YY    → 2-digit year     (26)
--   MM    → 2-digit month    (04)
--   DD    → 2-digit day      (28)
--   #...  → zero-padded counter (#### = 4 digits, ##### = 5 digits)
--
-- Counter reset cadence is derived from the format:
--   contains DD → daily reset
--   contains MM → monthly reset (default — YYMM####)
--   contains YY → yearly reset
--   else        → never resets

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS doc_prefix         TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS doc_running_format TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS doc_counters (
  prefix        TEXT NOT NULL,
  period        TEXT NOT NULL,  -- YYYYMM, YYYYMMDD, YYYY, or "_" for no-reset
  last_used_seq INT  NOT NULL DEFAULT 0,
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (prefix, period)
);
