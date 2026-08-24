# Shopee Stock Sync Runbook

## Purpose

Nexflow publishes a configurable percentage of usable SML stock to Shopee. SML
is the stock source of truth and returns balances in the smallest unit. Nexflow
subtracts only normalized Marketplace reservations that have not yet been
proven incorporated in a successful SML stock recalculation.

Target stock:

```text
floor(max(SML scope balance - pending base demand, 0) * stock percentage / 100 / unit factor)
```

When multiple active Shopee listings intentionally use the same SML item,
Nexflow requires an admin-defined shared-stock allocation totaling exactly
100%. Pending orders from Shopee, Lazada, and TikTok that have not reached a
verified SML recalculation are reserved first:

```text
pool = floor(max(SML scope balance - pending order base units, 0) * stock percentage / 100)
listing target = floor(pool * listing allocation percentage / 100 / unit factor)
```

The pool is blocked until every active listing has a valid allocation. This
prevents Nexflow from publishing the full SML balance independently to each
duplicate Shopee listing.

For an SML set product (`item_type=3`), Nexflow reads the current component
definition from `sml-api-bybos`, calculates how many complete sets every
component can form, uses the bottleneck component, then applies the shop
percentage and parent unit factor. Components are never mapped manually.

## Safety Model

- Every shop starts disabled with `stock_pct=80` and a 5-minute interval.
- Admins can select 5, 10, 20, or 30 minutes, 1 hour, or a custom interval up
  to 12 weeks. Calendar schedules support every 1-12 months on days 1-28 at a
  Bangkok time. Daily-or-longer schedules require an explicit risk
  acknowledgement because Shopee can lag behind SML for the entire interval.
- The scheduler stores `next_run_at`, runs at most one missed cycle after
  downtime, and keeps the calendar anchor for monthly schedules. A manual stock
  sync starts the next fixed interval from the time it completes; monthly
  schedules keep their configured day and time.
- At most five shops enter scheduled stock sync concurrently per Nexflow
  instance. Per-shop leases and changed-stock checks still prevent overlapping
  writes and unnecessary Shopee updates.
- Stock writes require Central Shopee Gateway mode.
- Selecting or changing a warehouse scope, percentage, or mapping disables the
  shop and requires a new dry-run.
- Saving or changing a shared-stock allocation disables the shop and requires a
  new dry-run. The allocation must include every active listing using that SML
  item and total exactly 100%.
- Paid/active Shopee orders without an SML document reserve stock before the
  target is calculated, preventing a later sync from restoring stock that an
  incoming order has already consumed.
- `UNPAID` does not reserve. `IN_CANCEL` continues to reserve until Shopee
  reports a completed cancellation. A sent reservation remains reserved in
  `awaiting_stock_recalc` until `processstockrequest` succeeds and a later SML
  balance read is verified; uncertainty pauses outbound stock fail-closed.
- Pending demand is aggregated by physical SML item/component across every
  Marketplace channel and shop in the tenant. An unproved reservation blocks
  outbound stock rather than falling back to zero demand.
- SML errors or malformed responses never become zero-stock updates.
- Minimum and maximum SML quantities are shown for the selected scope as
  reference values only; they never change the target-stock formula.
- A shop pauses if all previous positive stock becomes zero, or more than half
  of at least ten positive SKUs become zero.
- A model is blocked when reserved Shopee stock exceeds its calculated target.
- SKU/unit/mapping errors block only the affected model; valid models continue.
- Set products fail closed unless `SHOPEE_SET_STOCK_ENABLED=true`. Nested sets,
  inactive components, invalid units, changed definition hashes, and components
  shared by multiple active mappings are blocked before any Shopee write.
- Multiple Shopee seller warehouse locations pause the shop in v1.
- `warehouse.error_not_in_whitelist` means the shop does not use Shopee
  multi-warehouse. Nexflow treats it as the supported default seller-stock
  location and omits `location_id`, as required by Shopee's `update_stock`
  contract. Other warehouse errors still fail closed.
- Timed-out writes are `unknown_result`; Nexflow reads the product back before
  deciding whether the write succeeded.
- Only changed, blocked, error, and unknown attempts are retained for 90 days.
- Enabled shops refresh changed catalog rows hourly with a 10-minute overlap and
  run a full catalog reconciliation at least once every 24 hours.

## Activation Checklist

1. Confirm the tenant uses `SHOPEE_OPEN_API_MODE=gateway` and has an active shop.
2. Deploy `sml-api-bybos`, then the gateway, then Nexflow.
3. Open `/settings/shopee-stock` and run **Update Catalog**.
4. Resolve SKU/unit warnings for every model that should sync. Exact item code
   and barcode matching are case-sensitive; Nexflow does not guess units from
   product names. A manual unit factor is allowed only after an admin explicitly
   confirms it, and that override is audited.
   If multiple Shopee listings sell the same SML item, use **แบ่งสต๊อก**, review
   every member, and save an allocation totaling 100%.
5. Choose exactly one SML warehouse and one location. Orphan or blank-location
   balances are diagnostics only and are never included in the selected scope.
6. Run dry-run and review changed, blocked, and excluded balances. Nexflow lists
   every non-zero excluded balance by SML warehouse, location, and unit. These
   balances are informational only and are never included in the Shopee target.
   The latest item-level breakdown is stored with each Shopee model so the
   product row continues to show the warehouse and location after a page reload.
   Negative excluded balances should be corrected in SML. Blocked models
   remain untouched in Shopee and do not stop valid models from syncing.
   For set products, expand the calculation and verify every component balance,
   required quantity, possible sets, and the bottleneck component.
7. Enable the shop and save settings.
8. Run one manual sync and verify selected products in Shopee Seller Centre.

## Rollback

Turn off the shop toggle to stop future shop runs. For a single listing, choose
either the durable **set stock to zero then disable** flow (with Shopee read-back)
or explicitly acknowledge **keep current stock and manage manually**. The latter
cannot share an SML pool with managed listings without a proven allocation. For
an instance-wide stop, set
`SHOPEE_OPEN_API_ENABLED=false` and restart only that Nexflow instance. Database
migrations do not need to be rolled back.

## Set Product Release Gate

1. Keep `SML_SET_PRODUCT_EXPANSION_ENABLED=false` and
   `SHOPEE_SET_STOCK_ENABLED=false` during migration/catalog rollout.
2. Synchronize the full SML catalog and verify the parent set, component count,
   units, definition hash, document validity, and stock validity.
3. Compare one controlled invoice against SML Desktop before enabling document
   expansion for the tenant.
4. Run stock Dry-run for one controlled Shopee model and verify the bottleneck
   calculation before enabling set stock.
5. Turning either flag off is the immediate rollback; no migration rollback or
   SML data change is required.

AOY must keep `SHOPEE_SET_STOCK_ENABLED=false` until a real mapped set product
passes the controlled Seller Centre and SML component-balance checklist above.
