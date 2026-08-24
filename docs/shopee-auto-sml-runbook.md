# Shopee READY_TO_SHIP Auto SML Runbook

Auto SML is fail-closed at two levels. `SHOPEE_AUTO_SML_ENABLED=true` starts the
worker for an instance; an admin must then enable each shop from
`/shopee-operations`. Enabling a shop records the current time and SML route
signature, so existing orders are never treated as backlog.

## Enable a tenant

1. Confirm Shopee Gateway/token/realtime, SML readiness, `shopee_realtime / sale`
   route, catalog and LINE recipients are healthy.
2. Set `SHOPEE_AUTO_SML_ENABLED=true` in only the intended instance `.env` and
   rebuild its backend.
3. Select one shop in `/shopee-operations`, review the confirmation dialog and
   enable `สร้างบิล SML อัตโนมัติ`.
4. Create one new controlled `READY_TO_SHIP` order. Verify the Nexflow bill,
   SML document number, stock calculation, timeline and LINE notification.
5. Monitor `shopee_auto_sml_jobs`, queue lag and application logs for 24 hours.

## Safety behavior

- Only orders with Shopee `create_time` at or after `eligible_after` are queued.
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

## Disable and rollback

Turn off the shop switch first. For an instance-wide stop, set
`SHOPEE_AUTO_SML_ENABLED=false` and rebuild the backend. Do not roll back
migrations 082–083 and do not delete successful job rows; they are idempotency
records. Disabling does not reverse documents already created in SML.
