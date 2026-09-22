# TikTok Shop Auto SML Runbook

TikTok Shop Auto SML is fail-closed at two levels:

1. `TIKTOK_SHOP_AUTO_SML_ENABLED=true` enables the worker for one tenant.
2. Each connected shop must be enabled explicitly from TikTok Shop Operations.

Both levels default to `false`. Applying migration 104 creates only disabled
settings and never creates or backfills an Auto SML job.

## Trigger and evidence

- The only supported trigger is the first observed exact
  `AWAITING_COLLECTION` snapshot.
- Enabling or resuming a shop writes a new `eligible_after` cutoff. A transition
  before that cutoff cannot enter the queue.
- `(shop_id, order_id)` is unique, so webhook and scheduled reconciliation may
  safely observe the same order without creating duplicate work.
- The job snapshots the trigger time, configuration version, source hash,
  bill-relevant fingerprint, and SML route signature.
- Forward states `IN_TRANSIT`, `DELIVERED`, and `COMPLETED` may finish the same
  immutable job only when products, amounts, mappings, and route still match.
- `CANCELLED` cancels an unstarted job. Unknown or backward states stop at
  `needs_review`.

Buyer name, recipient address, phone number, and email are not prerequisites
and are not stored in diagnostics or Auto SML queue records.

## Preflight before enabling AOY

1. Keep the global flag off while deploying migration 104 and the application.
2. Open `/tiktok-shop-operations`, select the AOY shop, and click
   `ตรวจสอบระบบ`.
3. Confirm Open API/Gateway, order sync, webhook, SML route, and the bounded
   mapping sample are green.
4. Confirm `TIKTOK_SHOP_SML_SEND_ENABLED=true` and the TikTok sale route points
   to the intended AOY SML customer, warehouse, shelf, VAT, document format,
   and shipping item.
5. Rotate the TikTok Gateway internal shared secret before enabling production
   automation if it has appeared in any diagnostic output or transcript.
6. Set `TIKTOK_SHOP_AUTO_SML_ENABLED=true` for AOY only and redeploy.
7. Reopen diagnostics, select AOY, and explicitly enable the shop. Record the
   displayed cutoff time.
8. Use one new order that reaches `AWAITING_COLLECTION` after the cutoff.

## Canary acceptance

For the single canary order, verify all of the following before wider use:

- exactly one durable job exists for the shop/order;
- exactly one active Nexflow Bill exists with flow
  `tiktok_shop_api_reviewed`;
- product quantities, item codes, units, shipping line, VAT, and total match the
  reviewed TikTok preview;
- exactly one SML sale document exists and the Nexflow Bill stores its number;
- the order row shows `ส่ง SML แล้ว (AUTO)`;
- `/logs` shows queue, Bill/SML, and success milestones without buyer PII;
- LINE shows a dark `TikTok Shop` card for the Auto SML result, distinct from
  Shopee, with the Order ID, Bill ID, SML document number, and bounded product
  list only;
- no retry, duplicate, or severe backend error is present.

Do not enable another shop or bulk rollout until this canary passes.

## Failures and recovery

- Mapping, amount, status, or evidence changes end in `needs_review`; they are
  never guessed or silently rewritten.
- A route change pauses the shop. Re-run diagnostics and explicitly resume;
  resuming writes a new cutoff and does not process old orders.
- System failures use bounded retries at 1, 5, and 15 minutes. Three terminal
  failures pause the shop.
- LINE sends one durable result per Auto SML outcome: success, needs-review, or
  terminal failure. Intermediate retries do not notify LINE. Dedupe is scoped
  to the TikTok shop, order, outcome, and its SML document/error evidence.
- LINE content must not contain buyer name, recipient address, phone, email, or
  shipment contact data. A LINE delivery failure is logged but never retries an
  SML write or changes the Auto SML job result.
- After correcting an actionable issue, use `ลอง Auto SML ใหม่` on that exact
  order. Retry refreshes the reviewed fingerprint but reuses the same durable
  job and any existing Bill.

## Rollback

1. Disable the AOY shop in TikTok Shop Operations.
2. Set `TIKTOK_SHOP_AUTO_SML_ENABLED=false` and redeploy if a tenant-wide stop
   is required.
3. Leave succeeded jobs and created SML documents intact for audit. Never
   delete or recreate an SML document as an automated rollback.
4. Continue manual reviewed-Bill sending while the automation issue is fixed.
