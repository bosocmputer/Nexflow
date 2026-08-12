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

## Change safety

- Master writes are admin-only.
- Every change previews open bills, linked stock mappings, and duplicate stock conflicts.
- Only `pending` and `needs_review` bill items may change.
- Sent and archived documents never change.
- Changing or disabling a Master linked to Shopee stock pauses that shop and requires a new Dry-run.
- Stock unit and conversion factor remain in `shopee_stock_mappings`; they are not copied into the sales Master.

## Rollout

1. Back up the instance database and record alias, legacy mapping, and stock mapping counts.
2. Deploy migration 077 with `PRODUCT_MAPPING_MASTER_MODE=shadow`.
3. Run real import previews and inspect aggregate `shadow_mismatch` logs.
4. Confirm that unmatched real orders appear in the `รอจับคู่` tab. Legacy name
   hints remain in storage and must never appear as an unresolved admin queue by
   themselves.
5. Switch to `active`, rebuild the backend, and repeat sales and stock Dry-run smoke tests.
6. Roll back code if required; migration 077 is additive and can remain in place.
