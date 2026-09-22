# Marketplace Stock Control v2 — AOY Runbook

## Scope and safety state

This runbook covers the unified Shopee and TikTok Shop stock workspace at
`/settings/marketplace-stock`. AOY uses SML warehouse `AB-1` and location `001`
as the intended source, with a default 10% buffer. A database migration and a
feature flag do not activate a pool: every new pool is a draft and Auto remains
off.

The page only reads local snapshots. The **ตรวจ SML** action records a durable,
read-only plan: it uses SML net available inventory, subtracts active Nexflow
reservations, converts from base units, applies the buffer, and rounds targets
down. It does not read or write Marketplace inventory. A plan must never be
used for a later write once it is older than 60 seconds.

## Roles

- View: inspect settings, pools, and run history.
- Operate: run read-only SML checks and, after the write phase is enabled, start
  a manually confirmed sync.
- Admin: change source location, buffer, member, quota/shared mode, kill
  switches, and Auto schedule.

The server resolves permissions from `user_menu_permissions`; the browser is
not an authorization boundary.

## Pool configuration

1. Confirm the Product Master SKU has an active SML item/unit conversion and a
   fresh channel catalog snapshot.
2. Create one draft pool for one SML item/unit. A member is an exact shop plus
   product/SKU identity; do not use product names as identity.
3. Use **แบ่งโควตา** unless the operator deliberately accepts shared-stock
   oversell risk. Enabled quota members must total no more than 100%.
4. Recheck the SML warehouse/location and buffer. Saving any source, unit,
   mapping, allocation, or mode change pauses the affected pool and invalidates
   prior plans.
5. Run **ตรวจ SML** and resolve every blocked condition. A successful SML-only
   plan is not permission to write Marketplace stock.

## First write and Auto pilot (not enabled by this foundation)

Only after the write worker and relevant tenant feature gates are deployed:

1. Verify one selected member has current catalog inventory, exact warehouse
   readiness, API grant, and no legacy Shopee writer.
2. Create a fresh plan, then manually confirm a changed-target write. The
   worker must recheck config, reservation, SML, and Marketplace current stock
   immediately before sending an absolute target.
3. Read back every changed SKU. Treat timeout, missing SKU, response mismatch,
   or TikTok per-SKU `data.errors` as an unknown/failed result; pause the pool.
   Never guess a rollback.
4. Inspect audit log, persisted run lines, SML balance, and terminal-error
   notification behavior. Do not send normal-success LINE messages.
5. An Admin may then explicitly enable the five-minute schedule for that pool.
   The global and pool kill switches must cancel queued, not-started work.

## Emergency stop

Turn on the store kill switch to stop new work for the tenant. Turn off a pool
or its Auto setting to isolate one SML item. Existing external requests may
finish; inspect the durable run and read-back instead of issuing compensating
writes. Keep the record and configuration history for investigation.

## Current rollout boundary

The initial AOY implementation includes schema, RBAC, drafts, local catalog
selection, durable SML-only plans, reservation/buffer arithmetic, and audits.
Marketplace write workers, Auto scheduling, nightly reconciliation, and
Lazada API stock integration remain intentionally disabled until their separate
preflight/read-back implementation and controlled pilot are complete. Lazada
Excel behavior is unchanged.
