# Nexflow — Current State

Updated: 2026-08-27

Production capability mode is **sales-only and deterministic**. AI, OCR,
embedding, Daily Insight, IMAP ingestion, LINE chat, and purchase workflows are
disabled. Historical schemas and AI usage logs remain for audit/rollback.

## Production UAT Status

- Demo, AOY, and Lanboon run the same application feature baseline `966b027`.
  Ploy remains on isolated bootstrap commit `5497558`, while the shared
  release/edge checkout is `c74ec6a` so the four-tenant registry remains
  current. Tenant databases, SML settings, credentials, channel routes, and
  feature flags remain isolated. Central SML Gateway baseline: `42992f5`.
- Demo, AOY, Lanboon, Ploy, and Central Shopee Gateway health endpoints returned
  HTTP 200 with database status `ok` on 2026-08-27.
- Migrations through 090 are installed on every tenant. AOY has grouped UI,
  versioned unit Catalog, active conversion, and the reservation ledger enabled
  after readiness validation. Demo has only the versioned unit Catalog enabled.
  Lanboon retains all Marketplace Release A gates disabled. The user explicitly
  enabled AOY Shopee set-stock for controlled testing; Demo and Lanboon remain
  disabled. Shared code never overrides these tenant-specific gates.
- AOY is the active production UAT tenant and the user is actively testing the
  current build. Feedback remains pending for the marketplace sales/SML stock
  flow, Product Master/Catalog channel tags, `/sale-invoices`, and the compact
  `/shopee-operations` UI.
- AOY shop `264993963` has Auto SML enabled at its unchanged
  `READY_TO_SHIP` trigger. Migration 090 adds an admin-selectable `PROCESSED`
  trigger with versioned future-transition cutoffs and immutable job snapshots.
  Future Auto SML LINE notifications derive shipping details only from the
  final SML bill shipping item, never Shopee fee estimates.
- AOY Shopee Operations now suppresses the redundant `Auto: ส่งด้วยมือ` badge
  on successful manual sends. Manual rows show `ส่ง SML แล้ว`; automatic rows
  continue to show `ส่ง SML แล้ว (AUTO)`. Actionable Auto SML states remain
  visible.
- AOY order `260827ECCFMCSC` passed the first real automatic cancellation UAT:
  `BF-INV26080060` produced exactly one TRANS_FLAG 48 credit note
  `CN26080002`, followed by successful SML stock recalculation and LINE delivery.
  Cancelled document cells now use two compact lines plus an accessible details
  popover. Future cancellation notifications use a red Flex payload; future
  Shopee new-order, Auto SML, and cancellation-document Flex messages omit the
  Nexflow footer button. Text fallbacks retain their recovery URL. Amount/VAT
  calculation and `vat_type` behavior are unchanged.
- AOY `/logs` and bill timelines now share Thai labels and concise summaries
  for Auto SML and cancelled-after-SML business milestones. The real
  `BF-INV26080060 → CN26080002` history shows Order SN, automatic-send method,
  cancellation document transition, and the two recalculated SML item codes;
  the SML quick view includes both Shopee cancellation events. Raw audit action
  keys and existing rows are unchanged. Internal leases/retry counters remain
  in durable job tables rather than adding noisy user-facing audit rows.
- Demo has no Shopee shop and is not a Marketplace functional-test tenant. Use
  Demo for Catalog/SML-unit checks; use AOY for controlled Shopee, TikTok, and
  Lazada UAT while preserving tenant isolation.
- AOY normal-product Shopee stock UAT passed on shop `264993963` for item/model
  `6278820512/43634992848` mapped to SML `AH-0033` at factor 1. A controlled
  manual run changed Shopee stock 0 -> 1, catalog read-back confirmed 1, and the
  post-write preview was unchanged 1 -> 1. Automatic stock sync is enabled on
  AOY at its preserved five-minute interval after a successful dry-run.
  Shopee set-product stock is enabled only for AOY's controlled first real
  mapped set-product UAT and is not yet production-validated end to end.
- The shared application baseline contains consistent input-channel tags across
  sale invoices, Product Master,
  and Catalog. Shopee mappings display `Shopee API (Henna.milkford)` together
  with `Shopee Excel`; Lazada and TikTok display their Excel channels. Generic
  `บัญชีหลัก` labels were removed. This flow is enabled and validated in AOY;
  the code originated at `e069f2b`.
- The shared baseline's expanded Shopee stock groups identify SML stock,
  current Shopee stock, and the absolute stock target that Nexflow will send.
  Wide screens use aligned column headers; laptop widths use labeled stacked
  values so no stock value or mapping action is clipped by the sidebar. This
  flow is enabled and validated in AOY; the UI originated at `44b5a72`, and
  production browser QA was read-only and did not write Shopee stock.
- Detailed resume notes, deferred work, and the per-session preflight checklist
  are maintained in `AGENTS.md` under `Current UAT Handoff`.

---

## Runtime

```text
Server:    10.121.20.83  (ubuntu)

demo:
  Folder:    /mnt/data/nextstep-node-2/nexflow
  Public:    https://nexflow.nextstep-soft.com
  Backend:   nexflow-backend       :8110  → {"database":"ok","env":"production","status":"ok"}
  Frontend:  nexflow-frontend      :127.0.0.1:16323  → HTTP 200
  Postgres:  nexflow-postgres      :5440  → healthy
  SML DB:    demo

aoy:
  Folder:    /mnt/data/nextstep-node-2/nexflow-aoy
  Public:    https://nexflow-aoy.nextstep-soft.com
  Backend:   nexflow-aoy-backend   :8111  → {"database":"ok","env":"production","status":"ok"}
  Frontend:  nexflow-aoy-frontend  :127.0.0.1:16324  → HTTP 200
  Postgres:  nexflow-aoy-postgres  :5441  → healthy
  SML DB:    aoy

lanboon:
  Folder:    /mnt/data/nextstep-node-2/nexflow-lanboon
  Public:    https://nextflow-lanboon.nextstep-soft.com
  Backend:   nexflow-lanboon-backend   :8112
  Frontend:  nexflow-lanboon-frontend  :127.0.0.1:16325
  Postgres:  nexflow-lanboon-postgres  :5442
  SML DB:    lbk63

ploy (isolated test tenant; public HTTPS active):
  Folder:    /mnt/data/nextstep-node-2/nexflow-ploy
  Public:    https://nexflow-ploy.nextstep-soft.com
  Backend:   nexflow-ploy-backend   :127.0.0.1:8113
  Frontend:  nexflow-ploy-frontend  :127.0.0.1:16326
  Postgres:  nexflow-ploy-postgres  :127.0.0.1:5443
  SML DB:    ploy (reachable, product Catalog currently empty)

edge:      nexflow-edge          :6323  → host-based routing for production domains
sml-api:   nexflow-sml-api-bybos  :8200  → tenants demo,aoy,lbk63,ploy
```

The old `192.168.2.109` / ngrok deployment is DEV/legacy only. Production deploys
use `scripts/deploy_nextstep_instances.py`; see
`docs/nextstep-server-deploy-flow.md`.

Ploy's current bootstrap administrator is `admin@nexflow.local`. Its unique
random password was rotated and delivered through the local operator clipboard;
the previous credential is invalid. Fresh tenants use the same bootstrap email
but always receive a different generated 32+ character password. Shared literal
passwords such as `admin1234` are prohibited from runtime templates and source
control.

---

## DB Schema

Migrations available on boot: **001–090** (all idempotent/re-runnable)

Key recent migrations:

| # | Table/feature |
| --- | --- |
| 041 | shopee_api_connections, shopee_api_oauth_states |
| 044 | sml_bulk_jobs, sml_bulk_job_items |
| 045 | shopee multi-shop support |
| 047 | channel_defaults shipping item defaults |
| 048 | bill_items.discount_amount |
| 049 | imap_poll_jobs |
| 050–052 | shopee_settlement_runs, settlement items, hidden runs |
| 053 | cleanup sml_db settings |
| 054 | channel_defaults.inquiry_type |
| 055 | channel_defaults.remark_2 |
| 056–063 | Shopee Realtime operations, notifications, create-document, shipping actions |
| 064 | Shopee cancelled-after-SML tracking + `shopee_realtime_cancel` credit note route |
| 065 | structured LINE Flex payload outbox for Shopee order/settlement alerts |
| 066 | Shopee order payment breakdown snapshot queue from `get_escrow_detail` |
| 067 | LINE MyShop multi-account connections, snapshots, webhook events, `line_myshop` source/channel |
| 068–072 | multi-tenant notification, permissions, gateway, and operational hardening |
| 073 | sales-only mode, disabled embeddings, verified mappings, active aliases, catalog search indexes |
| 074 | SML warehouse stock catalog, Shopee stock mappings/settings, dry-run/sync history, DB leases |
| 075 | Require explicit SML warehouse/location selection for Shopee stock sync |
| 076 | Require exactly one SML warehouse/location pair per Shopee stock setting |
| 077 | scoped Marketplace Product Master, bill item identities, stock mapping links, impact indexes |
| 078 | SML set-product catalog/components, document validity and Shopee set-stock definition hashes |
| 079–084 | Shopee Gateway, Realtime Auto SML, and production hardening |
| 085–087 | Marketplace units/reservations and Shopee net stock from outstanding sales orders |
| 088 | Selectable Shopee cancellation destinations: TRANS_FLAG 45 or 48 |
| 089 | Durable final-CANCELLED SML document queue, immutable retries, and cancellation stock recalculation |
| 090 | Versioned per-shop Auto SML trigger (`READY_TO_SHIP` or `PROCESSED`) and immutable job transition snapshots |
| 079 | TikTok gross line amounts, durable amount review and one source artifact per import run |
| 080 | Safe shared SML stock allocation across multiple active Shopee listings |
| 081 | Persist excluded SML warehouse/location details per Shopee stock mapping |
| 082 | Durable per-shop Shopee READY_TO_SHIP Auto SML queue, cutoff, leases, retries and circuit breaker |
| 083 | Stable Asia/Bangkok document time persisted immediately before the first Auto SML write |
| 084 | Configurable Shopee stock schedules with persisted next run and calendar-month support |
| 085 | Versioned SML units, Marketplace conversion snapshots, immutable SML attempts, reservation ledger, fenced stock jobs, and grouped/async stock APIs |

---

## Feature Flags (current build)

```text
VITE_PHASE=2
VITE_ENABLE_SALES_ORDERS=true
VITE_ENABLE_SHOPEE_EXCEL=true
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_SHOPEE_REALTIME_OPS=true
VITE_ENABLE_LINE_MYSHOP=true
VITE_ENABLE_CHAT=false

ENABLE_SHOPEE_REALTIME_OPS=true
ENABLE_SHOPEE_CANCEL_AFTER_SML_ALERTS=true
ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=true
ENABLE_SHOPEE_RICH_LINE_FLEX=true
ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS=true
ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT=true
ENABLE_LINE_MYSHOP=true
SHOPEE_AUTO_SML_ENABLED=false
SHOPEE_AUTO_SML_CANCEL_ENABLED=false
Marketplace release gates (tenant-specific):
demo:    grouped=false, unit_catalog=true,  conversion=off,    ledger=false, set_stock=false
aoy:     grouped=true,  unit_catalog=true,  conversion=active, ledger=true,  set_stock=true
lanboon: grouped=false, unit_catalog=false, conversion=off,    ledger=false, set_stock=false
ploy:    grouped=true,  unit_catalog=false, conversion=off,    ledger=false, set_stock=false
```

`ENABLE_SHOPEE_RICH_LINE_FLEX`, `ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS`, and
`ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT` default to `true` in backend config. They
may be absent from `.env` while still active; set them explicitly to `false` for
rollback.

`ENABLE_LINE_MYSHOP` and `VITE_ENABLE_LINE_MYSHOP` also default to enabled. Set
either to `false` to hide or disable the LINE MyShop integration during rollback.

`SHOPEE_AUTO_SML_ENABLED` defaults to `false`. A tenant-level value of `true`
only starts the durable worker; each shop remains disabled until an admin passes
the readiness check and confirms activation in `/shopee-operations`. The
per-shop trigger defaults to `READY_TO_SHIP` and may be changed to `PROCESSED`.
Changing it creates a new cutoff for future transitions only; queued jobs keep
their snapshotted trigger and configuration version.

`SHOPEE_AUTO_SML_CANCEL_ENABLED` defaults to `false`. When enabled, a verified
transition to Shopee `CANCELLED` for an order whose SML sale was already sent is
queued durably and processed using the current `shopee_realtime_cancel / sale`
route. `IN_CANCEL` never creates an SML document. The first attempted `doc_no`,
payload, route, and document format are immutable across retries; a route change
or conflicting attempt stops for reconciliation instead of allocating a new
document. A separate durable post-success job retries SML stock recalculation
without resending the cancellation document.

Marketplace conversion flags default to disabled. `MARKETPLACE_CONVERSION_MODE`
accepts `off`, `shadow`, or `active`; `active` fails backend startup unless the
unit generation, mapping backfill, and reservation ledger are ready. Set-stock
remains a separate per-tenant release gate.

SML stock availability defaults to `physical_v1`. `shadow` and
`net_sale_order_v1` require an approved per-tenant
`SML_STOCK_SOURCE_FINGERPRINT`. Net mode stores physical balance, active SML
sales-order outstanding, SML usable, Nexflow calculation usable, source
snapshot, and fingerprint per Shopee stock run line. Reservations are not
released after `processstockrequest` until exact immutable-document evidence is
persisted.

---

## SML Config

```text
SML #1 (sale_reserve):   http://192.168.2.213:3248
  provider=BRSMLST  db=smlst2016  cust_code=CASH

SML #1 REST (saleinvoice v4):  http://192.168.2.213:8086

SML #2 (Shopee REST):    http://192.168.2.248:8080
  provider=SMLGOH  db=SML1_2026  cust_code=AR00004  wh=WH-01  shelf=SH-01

sml-api-bybos:  http://172.17.0.1:8200  x-tenant from app_settings.sml.database
  tenant aoy DB: nextstep.iszai.com:6843 / database aoy
  tenant lbk63 DB: chk562595.totddns.com:12831 / database lbk63
  provider/config: NEXT / SMLConfigNEXT.xml
  stock_request_url: http://nextstep.iszai.com:8093
  health/ready: {"database":"aoy","status":"ok"}
  sale invoice cancel preview/create:
    POST /api/v1/ic/sale-invoices/:doc_no/cancel/preview
    POST /api/v1/ic/sale-invoices/:doc_no/cancel
```

---

## Shopee Open API

- Partner ID: `2034838`, env: `live`
- Redirect URL: `https://animal-galvanize-tameness.ngrok-free.dev/api/shopee-api/callback`
- Connected shops managed in `shopee_api_connections` table via `/import/shopee`
- Production snapshot: 2 live shop connections total; 1 active live shop connection,
  label `Henna.milkford`, shop_id `264993963`. Disabled live connection:
  `Semicolon Constructions` / `1029622928`.
- Review-first import flow — confirm writes local bills, SML send via Retry

## LINE MyShop / LINE SHOPPING API

- Source/channel key: `line_myshop`. Do not overload `line`, which remains LINE
  OA chat/notification source.
- API reference: OA Plus Open API / LINE SHOPPING API, base
  `https://developers-oaplus.line.biz`, authenticated by `X-API-KEY`.
- Multi-account settings live at `/settings/line-myshop`; each account has its
  own API key, optional webhook secret, metadata, enabled flag, and webhook URL.
  Admins can clear a saved webhook secret from the edit dialog to return to API
  key based signature verification.
- Webhook endpoint: `/webhook/line-myshop/:connection_id`. Verify
  `x-myshop-signature` using HMAC-SHA256 Base64 before reading the event.
- Manual reconciliation is available per account from `/settings/line-myshop`
  via `POST /api/settings/line-myshop/connections/:id/sync`. It pulls a bounded
  48h updated-time window by default, fetches order detail before bill creation,
  and records `last_sync_at` / `last_error`.
- Eligible order webhook events create `source='line_myshop'`, `bill_type='sale'`
  bills in `needs_review`. SML send remains user-driven through existing Retry.
- SML route is controlled by `/settings/channels` using
  `channel_defaults(line_myshop, sale)`. No default route is seeded by migration.
- Notifications use the existing LINE notification outbox with
  `Source: line_myshop` and `EntityType: line_myshop_order`. The message builder
  must remain MyShop-specific and must not include recipient name, phone, email,
  shipping address, or other buyer/shipping PII.
- `line_myshop_order_snapshots.raw_webhook` may retain the full signed webhook
  payload for audit/reconciliation. Do not copy shipping PII into `bills.raw_data`,
  audit details, logs, or LINE notification text.

---

## Production Data Snapshot

Verified on server: 2026-06-22

| Area | Count / state |
| --- | --- |
| `bills` | 93 |
| `bill_items` | 98 |
| `channel_defaults` | 6 |
| `imap_accounts` | 0 |
| `shopee_api_connections` | 2 total, 1 active live connection |
| `shopee_order_snapshots` | 62 |
| `shopee_order_payment_snapshots` | 27 ready |
| `sml_bulk_jobs` | 8 |
| `shopee_sml_cancellations` | 0 |
| `line_notification_recipients` | 2 |
| `line_notification_deliveries` | 52 sent legacy text rows; next new order uses Flex payload v1/v2 |
| `audit_logs` | 367 |

LINE MyShop tables are new in migration 067 and start empty until an admin adds
accounts in `/settings/line-myshop` and registers each generated webhook URL in
OA Plus.

Most production marketplace sale documents are `sent` saleinvoice documents with
SML payload/response recorded. Shopee Realtime also has pending saleorder
documents awaiting the user-driven SML send flow:

| source | bill_type | document_route | status | count |
| --- | --- | --- | --- | --- |
| shopee | sale | saleinvoice | sent | 49 |
| shopee | sale | saleorder | pending | 6 |
| lazada | sale | saleinvoice | sent | 14 |
| tiktok | sale | saleinvoice | sent | 24 |

Current `channel_defaults` separate legacy/import routes from Shopee Realtime:

| channel | bill_type | endpoint | doc_format_code | doc_prefix | doc_running_format |
| --- | --- | --- | --- | --- | --- |
| lazada | sale | `/api/v1/ic/sale-invoices` | SI | BF-INV | YYMM#### |
| shopee | sale | `/api/v1/ic/sale-orders` | BS | BS | YYMM#### |
| shopee_realtime | sale | `/api/v1/ic/sale-invoices` | BF-INV | BF-INV | YYMM#### |
| tiktok | sale | `/api/v1/ic/sale-invoices` | SI | BF-INV | YYMM#### |

Production Shopee Realtime workflow should use `/shopee-operations` to create
local documents, then `/sale-invoices` / `ขายสินค้าและบริการ` for the SML send
step. The legacy Shopee import route may still create Sales Order documents, so
`/sales-orders` remains enabled and must stay functional.

Shopee cancelled-after-SML uses a separate route:

| channel | bill_type | destination | endpoint | SML screen | trans_flag |
| --- | --- | --- | --- | --- | --- |
| shopee_realtime_cancel | sale | ยกเลิกขายสินค้าและบริการ | `/api/v1/ic/sale-invoices/:doc_no/void` | SIC | 45 |
| shopee_realtime_cancel | sale | รับคืนสินค้า/ลดหนี้ | `/api/v1/ic/sale-invoices/:doc_no/cancel` | ST | 48 |

`doc_format_code` is loaded from the selected SML screen so tenants with more
than one format can choose the correct one. The create feature flag still gates
all writes. If tenant `aoy` cannot reach SML, the UI blocks creation with the
SML readiness error rather than writing a partial document.

Shopee payment breakdown snapshots are active. `shopee_order_payment_snapshots`
is populated from Shopee `get_escrow_detail` by queue/manual refresh and is read
by the `/shopee-operations` timeline drawer and by new-order LINE Flex when ready.
Page render and LINE delivery must not call Shopee live APIs directly.

LINE notification outbox is active with structured payload columns
`alt_text`, `flex_payload`, and `payload_version`. The worker sends the enqueued
Flex payload first and falls back to `message_text` only when Flex delivery fails.
Existing production deliveries are legacy `payload_version=0`; the next real
Shopee order with rich Flex enabled should enqueue version 1 or version 2 when
payment breakdown is ready.

LINE MyShop notifications share this outbox but use `source=line_myshop` and a
text-only, PII-redacted MyShop builder. Shopee Flex builders and Shopee delivery
dedupe keys must remain untouched.

---

## Archived IMAP Schema

Production has **0 enabled IMAP accounts**. The coordinator is not started,
`/settings/email` redirects to the dashboard, and `/api/settings/imap-accounts*`
returns `410 Gone`. Tables and historical rows remain only for audit and a
possible future purchase-flow redesign. See `docs/email.md` for the archived
implementation notes; do not configure an inbox in the current release.

---

## Known Operational Notes

- **SML mojibake** — `marshalASCII()` in all 6 SML POST clients. Do not remove.
- **doc_no bug** — never use `prefix-YYYY` pattern (SML silently drops docs). Use `YYMM####` counter.
- **channel_defaults** — must be populated before Retry works. Use Quick Setup at `/settings/channels`.
- **Production marketplace route** — Shopee Realtime, Lazada, and TikTok current
  production sale data is primarily `saleinvoice`; legacy Shopee import may still
  create `saleorder` documents through its own `channel_defaults` route.
- **Marketplace sale amounts** — Shopee Excel/API/Realtime, Lazada, and TikTok
  persist the full product amount in `bill_items.gross_amount`, the marketplace
  and seller discount in `bill_items.discount_amount`, and buyer-paid shipping as
  a separate configured SML item. Shopee API/Realtime use only
  `buyer_paid_shipping_fee` from escrow; actual or estimated carrier fees are not
  sale revenue. Confirmation reparses the server-owned source artifact/token and
  never trusts amounts or channel routing returned by the browser.
- **Shipping item readiness** — configure a distinct active SML item and unit in
  `channel_defaults` for Shopee import, Shopee Realtime, Lazada, and TikTok before
  importing an order with positive buyer-paid shipping. Missing configuration
  blocks only affected orders rather than omitting shipping silently.
- **legacy dev/ngrok** — `192.168.2.109` and ngrok are retired for production; do not deploy or configure callbacks there.
- **IMAP** — runtime and APIs are disabled in sales-only mode; retained tables are historical only.
- **LINE chat** — disabled (`VITE_ENABLE_CHAT=false`). Backend code is present but UI is hidden.
- **sml-api-bybos** — must use `--force-recreate` (not `restart`) when changing `.env`.
  Current Aoy tenant points to `nextstep.iszai.com:6843`, not the old demserver
  host. Nexflow calls it through Docker gateway `http://172.24.0.1:8200`.
- **Shopee cancelled after SML** — alerts and cancellation creation are enabled.
  `shopee_realtime_cancel / sale` can route to TRANS_FLAG 45 or 48 through
  `sml-api-bybos`; SML readiness must be OK before the create action is allowed.
  Automatic creation is separately gated by `SHOPEE_AUTO_SML_CANCEL_ENABLED`,
  accepts only final `CANCELLED`, retries the same immutable attempt, then runs a
  separate durable stock recalculation job.
- **Shopee LINE alerts** — `/settings/line-notifications` manages admin/team
  recipients separately from LINE chat. New-order Flex uses order snapshot data
  and cached payment breakdown when ready, shows times in Asia/Bangkok, and must
  not include buyer name, phone, address, or buyer username.
- **Shopee payment breakdown** — manual refresh is read-only and rate-limited by
  cache freshness. Failures must not block order sync, document creation, SML
  send, settlement, cancellation, or LINE fallback notifications.
- **LINE MyShop** — webhook signature is mandatory. `line_myshop` must map to
  its own channel defaults and must never fall back to `line`. MyShop order bills
  start as `needs_review`; SML route and doc format come from `/settings/channels`.
