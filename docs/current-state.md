# Nexflow — Current State

Updated: 2026-08-05

Production capability mode is **sales-only and deterministic**. AI, OCR,
embedding, Daily Insight, IMAP ingestion, LINE chat, and purchase workflows are
disabled. Historical schemas and AI usage logs remain for audit/rollback.

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

edge:      nexflow-edge          :6323  → host-based routing for production domains
sml-api:   nexflow-sml-api-bybos  :8200  → tenants demo,aoy,lbk63
```

The old `192.168.2.109` / ngrok deployment is DEV/legacy only. Production deploys
use `scripts/deploy_nextstep_instances.py`; see
`docs/nextstep-server-deploy-flow.md`.

---

## DB Schema

Migrations available/applied on boot: **001–076** (all idempotent/re-runnable)

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

---

## Feature Flags (current build)

```bash
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
```

`ENABLE_SHOPEE_RICH_LINE_FLEX`, `ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS`, and
`ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT` default to `true` in backend config. They
may be absent from `.env` while still active; set them explicitly to `false` for
rollback.

`ENABLE_LINE_MYSHOP` and `VITE_ENABLE_LINE_MYSHOP` also default to enabled. Set
either to `false` to hide or disable the LINE MyShop integration during rollback.

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

| channel | bill_type | endpoint | doc_format_code | doc_prefix | doc_running_format |
| --- | --- | --- | --- | --- | --- |
| shopee_realtime_cancel | sale | creditnote | CN | CN | YYMM#### |

The create-CN feature flag is enabled in production. The action still checks SML
readiness before creating a credit note; if tenant `aoy` cannot reach SML, the UI
will block creation with the SML readiness error rather than writing a partial CN.

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
- **legacy dev/ngrok** — `192.168.2.109` and ngrok are retired for production; do not deploy or configure callbacks there.
- **IMAP** — runtime and APIs are disabled in sales-only mode; retained tables are historical only.
- **LINE chat** — disabled (`VITE_ENABLE_CHAT=false`). Backend code is present but UI is hidden.
- **sml-api-bybos** — must use `--force-recreate` (not `restart`) when changing `.env`.
  Current Aoy tenant points to `nextstep.iszai.com:6843`, not the old demserver
  host. Nexflow calls it through Docker gateway `http://172.24.0.1:8200`.
- **Shopee cancelled after SML** — alerts and create-CN are enabled. CN creation
  uses `shopee_realtime_cancel / sale` and writes through `sml-api-bybos`; SML
  readiness must be OK before the create action is allowed.
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
