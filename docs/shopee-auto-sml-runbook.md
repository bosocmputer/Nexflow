# Shopee Auto SML Trigger Runbook

Auto SML is fail-closed at two levels. `SHOPEE_AUTO_SML_ENABLED=true` starts the
worker for an instance; an admin must then enable each shop from
`/shopee-operations`. Each shop selects one trigger:

- `READY_TO_SHIP` (default and recommended): create the bill when Shopee first
  marks the order ready for the seller to prepare.
- `PROCESSED`: wait until the seller has prepared the shipment in Shopee.

Enabling a shop or changing its trigger records a new `eligible_after` cutoff,
route signature, and configuration version, so existing orders are never
treated as backlog. A job already queued keeps its immutable trigger snapshot
and continues under that rule.

## Enable a tenant

1. Confirm Shopee Gateway/token/realtime, SML readiness, `shopee_realtime / sale`
   route, catalog and LINE recipients are healthy.
2. Set `SHOPEE_AUTO_SML_ENABLED=true` in only the intended instance `.env` and
   rebuild its backend.
3. Select one shop in `/shopee-operations`, choose the trigger, review the
   confirmation dialog and enable `สร้างบิล SML อัตโนมัติ`.
4. Create one new controlled order and move it through the selected trigger.
   Verify the Nexflow bill, SML document number, stock calculation, timeline and
   LINE notification.
5. Monitor `shopee_auto_sml_jobs`, queue lag and application logs for 24 hours.

## Safety behavior

- Only orders whose selected trigger transition is at or after `eligible_after`
  are queued. Push evidence is authoritative; an exact current transition may
  use `get_order_detail.update_time` only when a before/after state change is
  observed during reconciliation.
- Duplicate or out-of-order pushes cannot create a second job. Unknown states or
  missing transition evidence stop at `needs_review` instead of falling back or
  replaying an old order.
- A `READY_TO_SHIP` job may continue at `READY_TO_SHIP`, `PROCESSED`, `SHIPPED`,
  `TO_CONFIRM_RECEIVE`, or `COMPLETED`. A `PROCESSED` job may continue from
  `PROCESSED` onward. `UNPAID`, `IN_CANCEL`, and `CANCELLED` stop without an SML
  sale document.
- Every attempt reconciles Shopee again and reloads the bill before writing SML.
- `doc_time` is the current Asia/Bangkok time immediately before the first SML
  write. The durable job persists and reuses it for uncertain-result retries.
- Manual, bulk and automatic sends share the `erp_send:<shop_id>:<order_sn>`
  lease in `shopee_action_outbox`.
- Mapping, amount, shipping, catalog, set-product or route problems stop at
  `needs_review`; the operator fixes the cause and clicks retry.
- Shopee/SML validation and other deterministic 4xx failures do not retry.
  Timeout, connection failure, 429 and 5xx are the only automatic retry class.
- Transient failures retry after 1, 5 and 15 minutes. Three fully failed jobs in
  a row pause that shop. Jobs for one shop are processed serially.
- A route signature change pauses the shop. Re-enable it only after verifying
  the new destination; a manual retry then captures the newly approved route.
- Auto SML success LINE notifications derive the optional
  `ค่าจัดส่งเข้า SML` section only from the final bill item whose
  `source_sku=__shopee_shipping__`. Shopee estimated/actual fee fields must not
  be used. If the final bill has no shipping item, the section is omitted.

## Disable and rollback

Turn off the shop switch first. For an instance-wide stop, set
`SHOPEE_AUTO_SML_ENABLED=false` and rebuild the backend. Do not roll back
migrations 082–083 or additive migration 090, and do not delete successful job
rows; they are idempotency records. An older binary can run with the extra
columns because `READY_TO_SHIP` is the database default. Disabling does not
reverse documents already created in SML.
