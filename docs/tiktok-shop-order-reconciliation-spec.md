# Spec: TikTok Shop bounded order reconciliation

## Objective

Continuously refresh the existing PII-minimized TikTok Shop order snapshots for
one tenant and multiple authorized shops. Every run searches a closed,
bounded update-time window, follows TikTok's opaque page token only within that
run, then re-reads Order Detail `202507` and Price Detail `202407` before
upserting snapshots.

This phase remains read-only toward TikTok Shop and SML. It must not create a
Nexflow Bill, SML document, notification, webhook side effect, fulfillment
action, or stock write.

## API contract

- `POST /api/tiktok-shop-api/orders/reconcile` runs one explicit window for an
  Admin or Staff user. The window is `[update_time_ge, update_time_lt)` and may
  not exceed 24 hours.
- `GET /api/tiktok-shop-api/order-sync-settings` lists per-shop polling state.
- `PUT /api/tiktok-shop-api/order-sync-settings/:shop_id` changes one shop's
  enabled state, interval, overlap, and optimistic config version. Admin only.
- The background worker starts only when
  `TIKTOK_SHOP_ORDER_SYNC_ENABLED=true`; each shop still defaults disabled.

## Reconciliation rules

1. Search with `sort_field=update_time`, `sort_order=ASC`, page size 20, and a
   maximum of 10 pages (200 orders) per run.
2. Treat `next_page_token` as opaque and valid only for the active run. Never
   persist it as a resumable cursor.
3. Reject repeated tokens, duplicate order IDs across pages, missing TikTok
   request IDs, non-numeric order IDs, or records outside the requested window.
4. Snapshot each successful page through the existing typed Detail + Price
   Detail service. Partial pages may already be safely upserted if a later page
   fails, but the run is failed and its watermark is not advanced.
5. Advance the per-shop watermark only after the final page succeeds. Scheduled
   runs replay an overlap before that watermark so late updates are idempotently
   recovered.
6. A first scheduled run starts only from `now - overlap`; it never performs an
   unbounded historical backfill.
7. One shop may have at most one running reconciliation. Run history and safe
   error codes are durable; raw payloads, tokens, credentials, buyer data, and
   recipient data are never stored in the run tables or logs.

## Safe defaults

- Global worker disabled.
- Every shop disabled.
- Interval 300 seconds.
- Overlap 900 seconds.
- 30-second end-of-window safety lag.
- Failed scheduled runs retry after 60 seconds without advancing watermark.

## Testing and rollout

- Focused: `cd backend && go test ./internal/services/tiktokshop ./internal/handlers ./internal/database`
- Full: `cd backend && go test ./...`
- Race: `cd backend && go test -race ./internal/services/tiktokshop ./internal/handlers`
- Static: `cd backend && go vet ./...`
- Runtime guard: `bash scripts/check_sales_only_runtime.sh`
- AOY canary: first run one explicit recent window manually, prove run/snapshot
  counts and unchanged Bill/SML counts, then enable only AOY's connected shop.

## Success criteria

1. A successful multi-page run writes page snapshots idempotently and advances
   its watermark exactly to the exclusive window end.
2. Any page/search/detail/price/persistence failure leaves the watermark
   unchanged and records a bounded safe failure.
3. Scheduler restart or replay cannot duplicate run ownership or snapshot rows.
4. Demo, Lanboon, and Ploy stay disabled; AOY is enabled only after the manual
   canary passes.
5. No Bill, SML attempt, notification, webhook, stock, or fulfillment table is
   touched by this phase.
