-- 009_channel_defaults_endpoint.sql — per-channel SML endpoint override
--
-- Lets admins switch which SML API a channel posts to without a code change.
-- Empty string = auto-resolve by (channel, bill_type) — matches the original
-- hardcoded routing in handlers/bills.go:Retry.
--
-- Historical note: this migration used to add an enum CHECK constraint for
-- short endpoint keywords only. Migration 010 immediately relaxed endpoint to
-- free-form URL/path. Keep 009 constraint-free so a fresh/idempotent migration
-- run does not fail when existing rows already contain full paths such as
-- /SMLJavaRESTService/v3/api/purchaseorder.

ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS endpoint TEXT NOT NULL DEFAULT '';
