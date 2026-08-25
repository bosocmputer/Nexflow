# Marketplace Product Master

`marketplace_item_aliases` is the canonical Marketplace to SML product mapping used by imports, bill review, and Shopee stock sync.

## Resolution order

1. Active SML item whose code exactly equals marketplace SKU.
2. Scoped marketplace item and variant identity.
3. Scoped source SKU.
4. Scoped verified name when no SKU or stable identity exists.
5. Legacy name mappings are review hints only in active mode. They are retained
   for audit and rollback, but are not listed as active mappings in the normal
   admin workflow.

Shopee scope is `shop:<shop_id>`. An unscoped Shopee Excel import may be corrected on its bill but cannot create a reusable Master.

## Runtime mode

- `PRODUCT_MAPPING_MASTER_MODE=shadow` keeps the legacy verified-name result while logging `shadow_mismatch` counts. Use only during rollout comparison.
- `PRODUCT_MAPPING_MASTER_MODE=active` uses the scoped Master. This is the default when the variable is absent.

Logs contain aggregate resolution counts only. They must not include marketplace SKU, product names, or buyer data.

## Quantity conversion

Each scoped variant owns a versioned conversion policy. `ic_unit_use`, received
through the SML stock-catalog feed, is the authority for `stand_value` and
`divide_value`. Product metadata does not supply a price or unit factor.

```text
SML quantity = Marketplace quantity × quantity multiplier
base demand  = Marketplace quantity × quantity multiplier × stand / divide
```

Calculations use exact rational arithmetic. Ambiguous, inactive, or stale units
remain `needs_review`; the system never silently substitutes a factor of 1.
Marketplace gross, discount, shipping, VAT, and net amounts remain fixed from
the imported Marketplace snapshot. SML receives an effective unit price plus
the exact line amount, so increasing quantity does not increase document value.

## Change safety

- Master writes are admin-only.
- Every change previews open bills, reservations, linked stock mappings, shared
  pools, and duplicate stock conflicts. Commit requires the same mapping
  revision and impact digest or returns `409`.
- The short commit transaction creates a new revision, pauses affected shops,
  and enqueues an idempotent reconciliation job. Bill and reservation updates
  run in batches of 200 with restartable leases.
- Only never-attempted, non-archived bill items may change. Manual overrides are
  preserved by field. Sent, attempted, and archived documents never change.
- The first SML attempt persists the exact wire payload before the network call.
  Retry replays those bytes and never rebuilds a payload for the same `doc_no`.
- Changing or disabling a Master linked to Shopee stock pauses every affected
  shop and requires reconciliation plus a new Dry-run.
- A `managed` Shopee variant cannot be deleted, excluded, or changed directly
  to `blocked`. The admin must choose either durable `zeroing` (write zero and
  confirm by read-back) or `manual_unmanaged` with an explicit acknowledgement.
  While zeroing is active, other mutations are locked. The latest durable
  policy job is recoverable by alias after a page reload so failed/unknown jobs
  still expose the same retry action.
- Unit, multiplier, sales policy, and stock policy live on the canonical scoped
  variant. `shopee_stock_mappings` mirrors the resulting factor for stock runs.

## Synthetic performance verification

The 2026-08-25 pre-release check used an isolated local PostgreSQL 14.18
database with 50,000 SML products, 250,000 units, 50,000 Marketplace aliases,
50,000 Shopee variants, and 100,000 active reservations. Production runs
PostgreSQL 16, so repeat the plans on a restored tenant-sized PostgreSQL 16
dataset before enabling flags.

- The original Product Master parent query materialized all variant rows,
  spilled temporary data, and took 368 ms. Selecting bounded parent keys first
  reduced the authenticated API p95 to 64 ms; search p95 was 164 ms.
- The original Shopee parent query used the same materialization pattern and
  took 274 ms. The bounded-key query reduced authenticated API p95 to 9 ms;
  search p95 was 131 ms.
- Child API p95 was 23 ms for Product Master and 56 ms for Shopee stock. The
  largest measured response was about 15 KB, below the 250 KB budget.
- The normalized 100,000-row reservation aggregate took 255 ms. It remains in
  the asynchronous stock-preview path; do not move it into page rendering.

These are repeatable synthetic measurements, not production SLA evidence.
Record PostgreSQL 16 query plans, tenant data distribution, and authenticated
browser timings again at each rollout gate.

## Rollout

1. Back up the instance database and record alias, legacy mapping, stock mapping,
   bill snapshot, and reservation counts.
2. Deploy migration 085 with all new flags disabled. Migration 085 is additive
   and contains no startup backfill or destructive price cleanup.
3. Enable `MARKETPLACE_UNIT_CATALOG_ENABLED=true` in Demo and run a full catalog
   sync. Demo has no Marketplace shops, so use it only to validate SML product
   metadata, `ic_unit_use`, generation activation, and price-free Catalog UI.
4. On AOY, enable `MARKETPLACE_GROUPED_UI_ENABLED=true` first and verify the
   grouped Product Master with real Shopee, TikTok, and Lazada records. This is
   a read-only UI gate; do not enable conversion or write Shopee stock yet.
5. Wait until `/api/marketplace-aliases/readiness` reports catalog, mapping
   backfill, and reservation ledger ready for the enabled feature set.
6. Run real AOY import previews in `MARKETPLACE_CONVERSION_MODE=shadow` and inspect
   aggregate mismatch/reconciliation counts.
7. Confirm that unmatched real orders appear in the `รอจับคู่` tab. Legacy name
   hints remain in storage and must never appear as an unresolved admin queue by
   themselves.
8. Enable `MARKETPLACE_RESERVATION_LEDGER_ENABLED=true`, reconcile counts, then
   switch conversion to `active`. Startup fails closed if readiness is incomplete.
9. Repeat controlled AOY sales for Shopee, TikTok, and Lazada, then verify SML
   recalculation, stock Dry-run, and one live normal Shopee product write.
   Demo must not be used for Shopee verification because it has no Shopee shop.
10. Keep AOY set stock disabled until its real set-product UAT passes.
11. Roll back with conversion `shadow/off` and paused shops. Migration 085 and its
   ledger remain in place; do not delete reconciliation evidence.
