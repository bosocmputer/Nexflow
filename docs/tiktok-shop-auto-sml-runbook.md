# TikTok Shop Bill and SML runbook

TikTok Shop follows the same operating model as Shopee: Nexflow creates a
local Bill for a paid, fulfilment-ready order first; sending that Bill to SML
is a separate choice. Staff may always review and send a pending Bill manually.

1. **สร้าง Bill ใน Nexflow** is a local, idempotent action. It never writes to
   SML.
2. **ส่ง SML อัตโนมัติ** is optional. When it is off, staff open the pending
   Bill in `/sale-invoices` and use the ordinary manual SML send flow.

`TIKTOK_SHOP_AUTO_SML_ENABLED=true` enables the tenant worker that prepares
safe local Bills. `sml_send_enabled=false` remains the default for every shop,
so deployment never turns on automatic SML posting as a side effect. A bounded
AOY-only reconciliation can prepare eligible existing snapshots as local Bills;
it never posts them to SML.

## Trigger and evidence

- A local Bill may be prepared only for paid, fulfilment-ready states:
  `AWAITING_SHIPMENT`, `PARTIALLY_SHIPPING`, `AWAITING_COLLECTION`,
  `IN_TRANSIT`, `DELIVERED`, or `COMPLETED`.
- `UNPAID`, `ON_HOLD`, and `CANCELLED` never create a sale Bill. A cancellation
  follows the separate cancellation/return document flow.
- The normal live queue honors the shop's `eligible_after` cutoff. The bounded
  historical reconciliation is explicitly scoped to local Bill preparation and
  is never an SML-send backfill.
- `(shop_id, order_id)` is unique, so webhook and scheduled reconciliation may
  safely observe the same order without creating duplicate work.
- The job snapshots the trigger time, configuration version, source hash,
  bill-relevant fingerprint, and SML route signature.
- Later fulfilment states may finish the same immutable job only when products,
  amounts, mappings, and route still match.
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
4. Confirm the TikTok sale route points to the intended AOY SML customer,
   warehouse, shelf, VAT, document format, and shipping item. Confirm
   `TIKTOK_SHOP_SML_SEND_ENABLED=true` only before enabling **ส่ง SML อัตโนมัติ**.
5. Rotate the TikTok Gateway internal shared secret before enabling production
   automation if it has appeared in any diagnostic output or transcript.
6. Set `TIKTOK_SHOP_AUTO_SML_ENABLED=true` for AOY only and redeploy.
7. Use one paid, fulfilment-ready TikTok order and verify its local Bill appears
   in `/sale-invoices`.
8. Send that Bill manually and verify the ordinary SML result.
9. Enable **ส่ง SML อัตโนมัติ**
   only after that controlled Bill/SML canary succeeds.

## Canary acceptance

For the single canary order, verify all of the following before wider use:

- exactly one durable job exists for the shop/order;
- exactly one active Nexflow Bill exists with flow
  `tiktok_shop_api_reviewed`;
- product quantities, item codes, units, shipping line, VAT, and total match the
  reviewed TikTok preview;
- the order row shows the local Bill and `รอส่ง SML ด้วยมือ`;
- staff can open the same pending Bill from `/sale-invoices` and send it through
  the ordinary manual SML flow;
- `/logs` shows queue and automatic-Bill milestones without buyer PII;
- if automatic SML was explicitly enabled for a separate canary, exactly one
  SML sale document exists, the Bill stores its number, and the order row shows
  `ส่ง SML แล้ว (อัตโนมัติ)`;
- no retry, duplicate, or severe backend error is present.

Do not enable another shop or bulk rollout until this canary passes.

## Failures and recovery

- Mapping, amount, status, or evidence changes end in `needs_review`; they are
  never guessed or silently rewritten.
- A route change pauses the shop. Re-run diagnostics and explicitly resume;
  resuming writes a new cutoff and does not process old orders.
- System failures use bounded retries at 1, 5, and 15 minutes. Three terminal
  failures pause the shop.
- A `bill_created` job is terminal for automation and leaves its Bill pending;
  it must never retry merely to send SML by itself. Existing SML retry rules
  apply only when the separate automatic-SML switch was enabled.
- LINE sends results only for automatic SML success, needs-review, or terminal
  failure. Intermediate retries do not notify LINE. Dedupe is scoped to the
  TikTok shop, order, outcome, and its SML document/error evidence.
- LINE content must not contain buyer name, recipient address, phone, email, or
  shipment contact data. A LINE delivery failure is logged but never retries an
  SML write or changes the Auto SML job result.
- After correcting an actionable issue, use `ลอง Auto SML ใหม่` on that exact
  order. Retry refreshes the reviewed fingerprint but reuses the same durable
  job and any existing Bill.

## Rollback

1. Disable **ส่ง SML อัตโนมัติ** for the AOY shop in TikTok Shop Operations.
2. Set `TIKTOK_SHOP_AUTO_SML_ENABLED=false` and redeploy if a tenant-wide stop
   is required.
3. Leave succeeded jobs and created SML documents intact for audit. Never
   delete or recreate an SML document as an automated rollback.
4. Continue manual SML sending from already-created Bills while the automation
   issue is fixed.
