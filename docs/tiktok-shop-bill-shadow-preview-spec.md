# TikTok Shop Bill Shadow Preview

Status: implementation specification  
Scope: AOY controlled UAT, reusable for multiple TikTok Shop connections  
Safety mode: reviewed local Bill capability behind an AOY-off feature flag; SML remains disabled

## Outcome

Nexflow can inspect one trusted local TikTok Shop order snapshot and explain
whether it is ready to become a sale Bill. The preview exposes the proposed
document amount, marketplace-to-SML item mapping, quantity conversion, and
blocking reasons without creating or modifying any Bill, SML document,
Product Master alias, reservation, notification, stock, fulfilment, or TikTok
Shop resource.

## API contract

`GET /api/tiktok-shop-api/orders/:shop_id/:order_id/bill-shadow-preview`

- Roles: `admin`, `staff`.
- `shop_id` and `order_id` must be numeric TikTok identifiers.
- Source: the current row in `tiktok_shop_order_snapshots` only. Rendering a
  preview must never call TikTok Shop or the Central Gateway.
- A missing snapshot returns `404 snapshot_not_found`.
- An internal source-integrity failure returns a generic safe `500` response.
- The response excludes buyer data, raw snapshot JSON, line IDs, upstream
  request IDs, source hashes, tokens, signatures, and application secrets.
- `can_create_bill` is `true` only when every blocker is clear and the tenant
  explicitly enables `TIKTOK_SHOP_REVIEWED_BILL_ENABLED`. Deployments default
  this flag to `false`.

### Reviewed Product Master mapping

Admins can resolve a blocker through two explicit endpoints without enabling
Bill creation:

- `POST /api/tiktok-shop-api/orders/:shop_id/:order_id/bill-shadow-mapping/impact-preview`
- `POST /api/tiktok-shop-api/orders/:shop_id/:order_id/bill-shadow-mapping/confirm`

Both endpoints accept only the selected `product_id`, `sku_id`, SML item/unit,
and integer Marketplace quantity multiplier. The server derives
`source=tiktok` and `account_key=shop:<shop_id>` from the authenticated route
and verifies that the exact product/SKU exists in the current local snapshot;
the client cannot provide or override account scope. Staff may see the blocker
but cannot call these admin-only mutation routes.

Confirmation requires the current mapping revision and the SHA-256 impact
digest returned by the preview call. A revision/digest mismatch returns a
conflict and requires a fresh review. The impact response must expose affected
open Bills/reservations and whether the selected SML item affects Shopee stock.
If Shopee is affected, the UI warns that confirmation will pause automatic
stock sync for those shops until a new dry-run succeeds. Mapping confirmation
still must not create a Bill, reservation, SML attempt, notification,
fulfillment, TikTok stock write, or order-status change.

The response includes:

- shop and order identity, current TikTok status, currency, and snapshot time;
- `shadow_mode: true`, tenant-gated `can_create_bill`, and
  `ready_for_reviewed_bill`;
- a lowercase SHA-256 `review_digest` binding the source hash, snapshot time,
  status, amounts, route, item identity, mapping revision, Catalog generation,
  and exact unit-conversion evidence;
- proposed product, shipping, document, buyer-payment, and excluded
  buyer/platform-only charge totals;
- one grouped product/SKU row with Marketplace quantity, line amount, exact
  shop-scoped Product Master mapping, and calculated SML quantity;
- non-secret route readiness (`doc_format_code` and semantic route only);
- stable blocker codes and Thai operator-facing explanations;
- an existing active TikTok Bill reference, when one already exists for the
  same Order ID, to prevent duplicate creation across API and Excel entry
  points.

## Multi-store identity

TikTok Shop API mapping identity is:

`source=tiktok + account_key=shop:<shop_id> + product_id + sku_id`

The shadow preview never treats an alias under `account_key=default` as ready
for an API order. A matching unscoped alias may be reported as a legacy
candidate requiring review, but it must not cross shops automatically. TikTok
Excel remains under `account_key=default` until an explicit import-to-shop
linking workflow is designed.

## Readiness rules

The preview is ready for a future reviewed Bill only when all rules pass:

1. The snapshot belongs to an active TikTok Shop connection.
2. Status is one of `AWAITING_COLLECTION`, `IN_TRANSIT`, `DELIVERED`, or
   `COMPLETED`. Earlier, cancelled, and unknown states remain visible but are
   blocked.
3. The local order, price detail, normalized items, stored totals, and grouped
   line amounts reconcile exactly.
4. Every product/SKU has one active, scope-confirmed, sales-enabled mapping
   whose conversion status is `ready` in the active unit Catalog generation.
5. Marketplace quantity conversion is exact and finite.
6. A positive seller shipping amount has a configured TikTok shipping item and
   unit. Zero shipping does not require a shipping item.
7. The TikTok sale route exists and has a document format and supported sale
   endpoint.
8. No active TikTok Bill already uses the same Order ID. Duplicate detection is
   intentionally broader than the shop scope so an earlier TikTok Excel import
   cannot be recreated through the API path.
9. The saved snapshot has valid immutable source-hash evidence.

## Reviewed Bill create contract

`POST /api/tiktok-shop-api/orders/:shop_id/:order_id/reviewed-bill`

- Role: `admin` only.
- Requires exact `confirm=CREATE_REVIEWED_BILL` and the lowercase SHA-256
  `review_digest` from the latest preview.
- The backend rebuilds the preview from local persisted evidence and compares
  the digest in constant time. Any snapshot, amount, route, Catalog, mapping,
  or conversion change returns a conflict and requires a fresh review.
- One database transaction creates the pending Bill, all Bill items, exact
  mapping/conversion snapshots, Marketplace reservations, and creation audit.
- The existing unique TikTok Order ID guard makes retries idempotent. A retry
  reuses only a Bill owned by the same shop and
  `flow=tiktok_shop_api_reviewed`; an Excel/default-scope or other-flow Bill is
  a conflict rather than an automatic reuse.
- This endpoint does not create an SML attempt/document, LINE or in-app
  notification, TikTok/Shopee fulfillment or stock write, cancellation, or
  return. Sending the reviewed Bill to SML is a later, separately authorized
  phase.
- `TIKTOK_SHOP_SML_SEND_ENABLED` is a separate default-off gate checked at the
  central Bill-to-SML boundary before readiness checks, document-number
  allocation, attempt persistence, or external calls. It therefore covers
  direct retry, bulk send, and future automation paths even while reviewed
  Bill creation is enabled.
- Reviewed API Bills are labelled and filterable as `TikTok Shop API`; they
  must not be presented as legacy `TikTok Excel` imports.

## Amount semantics

- Proposed product total is the trusted TikTok product subtotal and must equal
  the sum of grouped line sale prices.
- Proposed shipping total is the seller document shipping amount.
- Proposed document total is product plus shipping.
- Buyer payment remains visible as reconciliation evidence but is not assumed
  to be the SML document total.
- Item insurance is a buyer/platform-only charge in the current AOY evidence;
  it is shown as excluded and never converted into a synthetic SML item.
- If buyer payment cannot be explained by the supported components, or line
  totals differ from the stored subtotal, the preview fails closed with an
  amount blocker.

Before Product Master confirmation, controlled order `586030483469993439`
showed product/document total `300.00 THB`, buyer payment `307.49 THB`,
excluded item insurance `7.49 THB`, and one missing shop-scoped mapping
blocker. After the operator confirmed the exact SML mapping, the same preview
showed the mapping ready with no blocker while remaining shadow-only.

## Observability and acceptance

Each request logs a bounded structured event with trace ID, entry point, shop
ID, order ID, mapping-ready count, total item count, blocker count, and outcome.
Logs contain no product names, raw payloads, buyer data, or credentials.

Acceptance requires backend unit/handler tests, race tests for changed Go
packages, `go vet`, frontend regression tests, lint/build, desktop and 390 px
browser QA, production health/Gateway checks, and proof that Bill, SML attempt,
notification, and LINE-delivery counts are unchanged after preview use.

## Product Master mapping UAT — 2026-09-13

- AOY deployed commit `946a8fd`; database backup
  `pre-deploy-20260913-025422.sql.gz`.
- Controlled order `586030483469993439` exposes the admin action
  `จับคู่สินค้า SML`, exact product/SKU identity, and the label
  `ผูกเฉพาะร้านนี้`.
- Desktop and 390 px QA opened the mapping dialog and SML Catalog drawer, then
  cancelled without selecting an SML item. Nested dialog/drawer focus, return
  to the refreshed Bill Shadow Preview, and horizontal overflow passed; the
  browser console had no warning or error.
- No impact-preview or confirm request was sent because choosing the correct
  SML item/unit is a business decision. The exact shop/product/SKU therefore
  still has zero active scoped mappings.
- Before and after QA, snapshot/webhook/Bill/SML-attempt/notification/LINE-
  delivery counts remained `9/5/323/27/537/252`; total Product Master aliases
  remained 74 and recent severe logs remained zero.

## Product Master mapping completion — 2026-09-13

- The operator confirmed the exact AOY shop/product/SKU mapping to SML item
  `AH-0002`, unit `กล่อง`, Marketplace quantity multiplier `1`. The persisted
  alias is scope-confirmed, sales-enabled, conversion `ready`, active, and at
  mapping revision 1. Its reconciliation job completed on the first attempt
  with zero processed failures.
- Reopening Bill Shadow Preview for order `586030483469993439` showed
  `ข้อมูลพร้อมสำหรับขั้นตรวจทาน`, mapping `พร้อมใช้`, SML quantity `1`, the
  unchanged `300.00 THB` proposed Bill, and no blocker. The UI still exposes no
  Bill or SML write action.
- Because `AH-0002` participates in the existing Shopee stock model, mapping
  confirmation disabled and paused automatic stock sync for shop `264993963`
  until a fresh preview. The subsequent manual preview run
  `6c1b8635-7049-4d38-869b-a2ebfa5859d3` succeeded for all 44 listings: 8
  changed targets, 36 unchanged, 0 blocked, and 0 errors. This was a dry-run;
  the automatic switch remains off and no Shopee stock write was sent.
- The preview also reported excluded negative balances for `AH-0001` and
  `AH-0003` in warehouse/location `AB-2 / 002`; those quantities were displayed
  as evidence and were not included in the selected `AB-1 / 001` calculation.
- Final snapshot/webhook/Bill/SML-attempt counts remain `9/5/323/27` and aliases
  are now 75. In-app/LINE counts increased to `540/254` solely from a concurrent
  deduplicated Shopee realtime order (`3/2` recipient deliveries), not from the
  TikTok mapping or shadow preview. AOY health returned HTTP 200 and the browser
  console plus recent severe-log scan were clean.

## Reviewed Bill capability deployment — 2026-09-13

- Backend commit `8b620d8` and UI commit `c4f6ea4` were deployed to AOY only.
  The tenant flag remains absent/off, so production preview returns
  `can_create_bill=false`, the dialog remains labelled `Shadow`, and no create
  button or POST path is reachable from the UI.
- Production QA for order `586030483469993439` still shows no blocker,
  `AH-0002 / กล่อง / SML quantity 1`, proposed Bill `300.00 THB`, buyer
  payment `307.49 THB`, and excluded insurance `7.49 THB`. The console had no
  warning/error.
- Counts after deploy and preview are Bills `323`, reviewed TikTok Bills `0`,
  Bills for this Order ID `0`, SML attempts `27`, notifications `540`, and LINE
  deliveries `254`. No migration or external write ran.
- AOY app health, Central TikTok Gateway health/network, edge checks, and SML
  tenant `aoy` readiness returned healthy. The database backup is
  `pre-deploy-20260913-033608.sql.gz`.
- Next activation requires explicit operator authorization to set the AOY flag
  true. The first click must create exactly one pending local Bill, a repeat
  must reuse it, and the SML-attempt/notification/LINE counts must remain
  unchanged before any later SML phase is designed.
