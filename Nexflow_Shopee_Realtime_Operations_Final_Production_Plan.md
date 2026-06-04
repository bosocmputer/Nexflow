# Nexflow Shopee Realtime Operations Final Production Plan

## Summary
- เพิ่มเมนูใหม่แยกชื่อ **Shopee Realtime** ที่ `/shopee-operations`
- ไม่กระทบ flow เดิม: `/import/shopee`, `/sale-invoices`, `/bills`, `/shopee-settlements`, SML send และ settlement ใช้งานเหมือนเดิม
- ใช้ Shopee Push Mechanism เป็น realtime trigger และใช้ `get_order_detail` เป็น source of truth
- เปิดใช้งานแบบ production-safe: read-only → push sync → ERP action → ship action
- ทุกอย่างอยู่หลัง feature flag `ENABLE_SHOPEE_REALTIME_OPS`

## Verified Production Facts
- Server Nexflow production health OK.
- Connected live shop: `Henna.milkford`, `shop_id=264993963`, token valid.
- Existing production data:
  - 84 bills total
  - Shopee sale 46
  - marketplace bills route เป็น `saleinvoice`
  - existing marketplace bills ทั้งหมดเป็น `sent`
- Read-only Shopee API verified:
  - `get_order_list(update_time)` works.
  - `get_order_detail` works.
  - Last 14 days: `SHIPPED=8`, `COMPLETED=21`, `CANCELLED=2`, `READY_TO_SHIP=0`, `PROCESSED=0`, `UNPAID=0`.
- `get_shipping_parameter` callable, but current sample order was already `LOGISTICS_PICKUP_DONE`, so Shopee correctly rejected shipping parameter because package was no longer ready to ship.
- Not yet verified:
  - Shopee push callback reaches Nexflow.
  - `get_shipping_parameter` succeeds on a real `READY_TO_SHIP` order.
  - `ship_order` succeeds on a controlled order.

## Customer Use Cases
- **ออเดอร์ใหม่เข้า**
  - Shopee sends push.
  - Nexflow stores event, fetches order detail, updates Shopee Realtime dashboard.
  - User sees order under `รอบันทึก ERP` or `ต้องตรวจ`.

- **ต้องตรวจสินค้า**
  - SKU/map ไม่ครบ.
  - Dashboard shows `ตรวจสอบสินค้า`.
  - User fixes mapping in Nexflow.
  - Order becomes ready for ERP.

- **บันทึกเข้า ERP**
  - User clicks `บันทึกเข้า ERP`.
  - Nexflow creates or reuses linked bill and sends through existing `ขายสินค้าและบริการ / SI` route.
  - Dashboard shows SML doc number after success.

- **จัดส่ง Shopee**
  - User clicks `จัดส่ง Shopee`.
  - Nexflow calls `get_shipping_parameter`.
  - If pickup/dropoff/package requirements are ready, Nexflow calls `ship_order`.
  - Order is marked as waiting Shopee confirmation until push/detail confirms status.

- **ออเดอร์ยกเลิก**
  - Push/sync sees `CANCELLED`.
  - Nexflow blocks ERP/ship actions.
  - If ERP was already created, dashboard flags it for manual accounting review.

- **Push หลุดหรือซ้ำ**
  - Scheduled sync with `get_order_list(update_time)` fills gaps.
  - Duplicate/out-of-order push is deduped and reconciled safely.

## Logic → API Map
- OAuth/connect shop:
  - `shop/auth_partner`
  - `auth/token/get`
  - `auth/access_token/get`
- Shop info:
  - `shop/get_shop_info`
  - `shop/get_profile`
- Realtime push:
  - `order_status_push`
  - `order_trackingno_push`
  - `package_info_push`
  - `package_fulfillment_status_push`
  - `open_api_authorization_expiry`
  - `shop_authorization_canceled_push`
- Pull sync:
  - `order/get_order_list`
  - `order/get_order_detail`
- ERP/SML:
  - Existing Nexflow bill + SML route, no Shopee API.
- Shipping:
  - `logistics/get_shipping_parameter`
  - `logistics/ship_order`
- Settlement:
  - Existing `payment/get_escrow_list`
  - Existing `payment/get_escrow_detail`

## Data Model
- Add `shopee_order_snapshots`:
  - shop, order_sn, Shopee status, ERP status, bill_id, sml_doc_no, package/tracking/logistics fields, raw detail, last synced time.
- Add `shopee_push_events`:
  - raw payload, push code, shop_id, order_sn, event timestamp, dedupe key, processed status.
- Add `shopee_reconcile_jobs`:
  - queue for fetching detail after push or scheduled sync.
- Add optional `shopee_action_outbox`:
  - idempotency for ERP send and ship actions.

## UX / UI
- New nav group or item: `Shopee Realtime`.
- Page layout:
  - Top readiness bar: token, push, SML route, logistics.
  - Metric strip: new, pending ERP, needs review, ERP saved, waiting ship, shipped, cancelled.
  - Work queue table: order, buyer, amount, Shopee status, ERP status, logistics status, action.
  - Problem queue: missing SKU, missing customer/config, token expired, logistics not ready.
  - Admin diagnostics collapsed: push events, reconcile errors, last sync.
- Buttons:
  - `ซิงก์ล่าสุด`
  - `บันทึกเข้า ERP`
  - `จัดส่ง Shopee`
  - `ดูรายละเอียด`
- Disabled buttons must explain why.

## Production Safety
- Webhook:
  - verify request
  - ACK fast
  - store raw event
  - dedupe
  - enqueue reconcile
- Reconcile:
  - fetch detail
  - update snapshot only with newer truth
  - preserve ERP/SML state
  - never create duplicate bills/documents
- ERP action:
  - must be idempotent
  - must reuse linked bill/doc if exists
  - blocked for unpaid/cancelled/missing mapping/SML not ready
- Ship action:
  - never call `ship_order` without explicit confirmation
  - require `get_shipping_parameter` first
  - do not mark shipped until Shopee confirms via push/detail
- Rollback:
  - turn off feature flag
  - old pages and data remain unaffected

## Performance
- Dashboard reads from local DB snapshot only.
- No Shopee API call on every page render.
- Background sync uses bounded concurrency and backoff.
- Indexes on shop_id, order_sn, Shopee status, ERP status, updated_at.
- SSE/event broker may update dashboard after snapshot changes; polling fallback allowed.
- No new heavy frontend libraries.

## Implementation Phases
- Phase 1: Create `shopee-open-api` skill and reference docs.
- Phase 2: Add DB tables, migrations, repositories, and read-only sync service.
- Phase 3: Add new `/shopee-operations` read-only dashboard.
- Phase 4: Add push webhook + reconcile queue.
- Phase 5: Add scheduled sync fallback.
- Phase 6: Add ERP save action using existing SI/SML route.
- Phase 7: Add shipping readiness and guarded `ship_order`.
- Phase 8: Pilot rollout, then enable for staff.

## Test Plan
- Backend:
  - duplicate push
  - out-of-order push
  - invalid webhook signature
  - missed push fallback sync
  - token expired/refresh
  - Shopee rate limit/5xx
  - `get_order_list` 15-day/cursor behavior
  - `get_order_detail` max 50
  - no duplicate bill/SML doc
  - `ship_order` blocked when logistics not ready
- Frontend:
  - feature flag off hides route
  - feature flag on shows new menu only
  - old pages unchanged
  - dashboard filters/status/actions work
  - disabled reasons readable
  - mobile no overflow
- Production pilot:
  - read-only sync and compare Seller Center count
  - verify push callback
  - test ERP on one approved order
  - test shipping only on one approved ready-to-ship order
  - monitor logs and backend health

## Assumptions
- V1 scope is Order → ERP/SML → Ship Shopee.
- Shipping label/AWB printing is not in V1.
- Daily operation should be inside Nexflow, but initial OAuth/push setup still happens in Shopee Console.
- Existing production behavior must remain unchanged.
