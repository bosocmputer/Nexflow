# Shopee Stock Sync Runbook

## Purpose

Nexflow publishes a configurable percentage of usable SML stock to Shopee. SML
is the stock source of truth and returns balances in the smallest unit. Nexflow
does not subtract sales orders again.

Target stock:

```text
floor(max(SML scope balance, 0) * stock percentage / 100 / unit factor)
```

## Safety Model

- Every shop starts disabled with `stock_pct=80` and a 5-minute interval.
- Stock writes require Central Shopee Gateway mode.
- Selecting or changing a warehouse scope, percentage, or mapping disables the
  shop and requires a new dry-run.
- SML errors or malformed responses never become zero-stock updates.
- Minimum and maximum SML quantities are shown for the selected scope as
  reference values only; they never change the target-stock formula.
- A shop pauses if all previous positive stock becomes zero, or more than half
  of at least ten positive SKUs become zero.
- A model is blocked when reserved Shopee stock exceeds its calculated target.
- SKU/unit/mapping errors block only the affected model; valid models continue.
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
5. Choose **All warehouses** or explicit warehouse/location pairs. Acknowledge
   orphan or blank-location stock only after checking SML master data.
6. Run dry-run and review changed, blocked, and excluded balances. Blocked models
   remain untouched in Shopee and do not stop valid models from syncing.
7. Enable the shop and save settings.
8. Run one manual sync and verify selected products in Shopee Seller Centre.

## Rollback

Turn off the shop toggle. This stops future updates and does not alter the stock
already stored in Shopee. For an instance-wide stop, set
`SHOPEE_OPEN_API_ENABLED=false` and restart only that Nexflow instance. Database
migrations do not need to be rolled back.
