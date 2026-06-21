-- Add Shopee shipping reconcile, tracking, and label discovery action audit types.
-- Idempotent: keeps existing rows and only broadens the action CHECK constraint.

ALTER TABLE shopee_action_outbox
  DROP CONSTRAINT IF EXISTS shopee_action_outbox_action_check;

ALTER TABLE shopee_action_outbox
  ADD CONSTRAINT shopee_action_outbox_action_check
  CHECK (
    action IN (
      'create_document',
      'erp_send',
      'ship_order',
      'reconcile_shipping',
      'shipping_document_create',
      'shipping_document_result',
      'shipping_document_download',
      'cancel_sml_document',
      'payment_breakdown_refresh'
    )
  );
