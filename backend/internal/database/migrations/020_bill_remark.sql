-- Migration 020: add remark column to bills table
-- Stores supplier/buyer remark that admin enters before sending to SML.
-- Passed to SML purchaseorder payload as "remark" field.
ALTER TABLE bills ADD COLUMN IF NOT EXISTS remark TEXT NOT NULL DEFAULT '';
