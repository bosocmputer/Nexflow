# Shopee CANCELLED -> SML Cancellation Runbook

This automation is fail-closed behind
`SHOPEE_AUTO_SML_CANCEL_ENABLED=false` by default. The SML destination is not a
second hidden setting: it is the active `shopee_realtime_cancel / sale` route in
Channel Settings. Supported routes are TRANS_FLAG 45 (`void`) and TRANS_FLAG 48
(`credit note / return`).

## Enable one tenant

1. Confirm Shopee Realtime, SML readiness, the original bill and SML document,
   LINE recipients, and the intended cancellation route/document format.
2. Confirm `ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=true` and set
   `SHOPEE_AUTO_SML_CANCEL_ENABLED=true` in only the intended tenant runtime.
3. Recreate that tenant backend and verify the startup log reports the worker as
   enabled. Existing or historical cancelled orders are not backfilled.
4. Create a controlled new Shopee order, wait for its SML sale document, then
   cancel it in Shopee.
5. Verify exactly one row in `shopee_sml_cancellations`, exactly one SML
   cancellation document, the SML stock recalculation, timeline, application
   notification, and LINE success notification.

## Safety behavior

- Shopee push only wakes reconciliation; `get_order_detail` is the source of
  truth before enqueue and again before the SML write.
- Only the exact final status `CANCELLED` is eligible. `IN_CANCEL` waits.
- The queue is unique per shop, order, and original SML sale document.
- The first SML attempt persists `doc_no`, payload, endpoint, and route signature
  before the external write. Timeout, connection failure, 429, and 5xx replay
  that exact attempt after 1, 5, and 15 minutes, up to three attempts.
- A changed route, invalid source bill, non-final Shopee status, or conflicting
  prior attempt blocks the job and sends an operational failure notification.
- Jobs for one shop are serialized; leases recover after worker restart.
- Success queues a separate durable `processstockrequest` job. A backend restart
  or temporary SML error retries only stock recalculation and never resends the
  cancellation document. The Operations badge shows pending/running/failure.
- Success sends the existing cancellation-created LINE notification without
  buyer PII.

## Disable and rollback

Set `SHOPEE_AUTO_SML_CANCEL_ENABLED=false` and recreate only the affected
tenant backend. Do not remove migration 089 or delete attempt rows. Disabling
stops new automatic processing but never reverses documents already created in
SML. Pending or ambiguous attempts must be reconciled before any corrected
document is created.
