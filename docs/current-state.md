# Nexflow — Current State

Updated: 2026-06-21

---

## Runtime

```text
Server:    192.168.2.109  (bosscatdog)
Folder:    /home/bosscatdog/billflow-henna
Backend:   nexflow-backend   :8110  → {"database":"ok","env":"production","status":"ok"}
Frontend:  nexflow-frontend   :3030  → HTTP 200
Postgres:  nexflow-postgres   :5440  → healthy
Public:    https://animal-galvanize-tameness.ngrok-free.dev  (ngrok, fixed)
```

Server folder `/home/bosscatdog/billflow-henna` currently has no git metadata,
so deploy checks must compare files explicitly before replacing sources.

---

## DB Schema

Migrations applied: **001–065** (all idempotent/re-runnable)

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

---

## Feature Flags (current build)

```bash
VITE_PHASE=2
VITE_ENABLE_SALES_ORDERS=true
VITE_ENABLE_SHOPEE_EXCEL=true
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_CHAT=false

ENABLE_SHOPEE_CANCEL_AFTER_SML_ALERTS=true
ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=true
ENABLE_SHOPEE_RICH_LINE_FLEX=true
ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS=true
ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT=true
```

---

## SML Config

```text
SML #1 (sale_reserve):   http://192.168.2.213:3248
  provider=BRSMLST  db=smlst2016  cust_code=CASH

SML #1 REST (saleinvoice v4):  http://192.168.2.213:8086

SML #2 (Shopee REST):    http://192.168.2.248:8080
  provider=SMLGOH  db=SML1_2026  cust_code=AR00004  wh=WH-01  shelf=SH-01

sml-api-bybos:  http://172.24.0.1:8200  x-tenant=aoy
  sale invoice cancel preview/create:
    POST /api/v1/ic/sale-invoices/:doc_no/cancel/preview
    POST /api/v1/ic/sale-invoices/:doc_no/cancel
```

---

## Shopee Open API

- Partner ID: `2034838`, env: `live`
- Redirect URL: `https://animal-galvanize-tameness.ngrok-free.dev/api/shopee-api/callback`
- Connected shops managed in `shopee_api_connections` table via `/import/shopee`
- Production snapshot: 1 active live shop connection, label `Henna.milkford`, shop_id `264993963`
- Review-first import flow — confirm writes local bills, SML send via Retry

---

## Production Data Snapshot

Verified on server: 2026-06-16

| Area | Count / state |
| --- | --- |
| `bills` | 93 |
| `bill_items` | 98 |
| `channel_defaults` | 6 |
| `imap_accounts` | 0 |
| `shopee_api_connections` | 1 active live connection |
| `sml_bulk_jobs` | 8 |
| `shopee_sml_cancellations` | 0 |
| `audit_logs` | 365 |

Most production marketplace sale documents are `sent` saleinvoice documents with
SML payload/response recorded. Shopee Realtime also has pending saleorder
documents awaiting the user-driven SML send flow:

| source | bill_type | document_route | status | count |
| --- | --- | --- | --- | --- |
| shopee | sale | saleinvoice | sent | 49 |
| shopee | sale | saleorder | pending | 6 |
| lazada | sale | saleinvoice | sent | 14 |
| tiktok | sale | saleinvoice | sent | 24 |

Current `channel_defaults` for marketplace sale routes point to
`/api/v1/ic/sale-invoices` with `doc_format_code=SI`. This is why the production
workflow should use `/sale-invoices` / `ขายสินค้าและบริการ` as the primary
marketplace path. `/sales-orders` remains enabled and must stay functional.

Shopee cancelled-after-SML uses a separate route:

| channel | bill_type | endpoint | doc_format_code | doc_prefix | doc_running_format |
| --- | --- | --- | --- | --- | --- |
| shopee_realtime_cancel | sale | creditnote | CN | CN | YYMM#### |

The create-CN feature flag is enabled in production. The action still checks SML
readiness before creating a credit note; if tenant `aoy` cannot reach SML, the UI
will block creation with the SML readiness error rather than writing a partial CN.

---

## Active IMAP Inboxes

Managed via `/settings/email` (DB-driven, `imap_accounts` table):

Production currently has **0 IMAP accounts**. `/settings/email` must therefore
render the empty state and remain actionable for adding a Gmail App Password
inbox later.

Poll interval is still constrained to ≥ 300s (5 min). Routing rules remain:
`ถูกจัดส่งแล้ว` / `ยืนยันการชำระเงิน` → purchaseorder (SML #2); other Shopee
email → configured sales route; general email → AI pipeline.

---

## Known Operational Notes

- **SML mojibake** — `marshalASCII()` in all 6 SML POST clients. Do not remove.
- **doc_no bug** — never use `prefix-YYYY` pattern (SML silently drops docs). Use `YYMM####` counter.
- **channel_defaults** — must be populated before Retry works. Use Quick Setup at `/settings/channels`.
- **Production marketplace route** — current Shopee/Lazada/TikTok sale data is `saleinvoice` / `SI`, not Sales Order.
- **ngrok** — URL is fixed (`animal-galvanize-tameness.ngrok-free.dev`). If it ever changes, update `PUBLIC_BASE_URL` in `.env` and rebuild.
- **IMAP Gmail** — requires App Password, not real password. Min poll 5 min.
- **LINE chat** — disabled (`VITE_ENABLE_CHAT=false`). Backend code is present but UI is hidden.
- **sml-api-bybos** — must use `--force-recreate` (not `restart`) when changing `.env`.
- **Shopee cancelled after SML** — alerts and create-CN are enabled. CN creation
  uses `shopee_realtime_cancel / sale` and writes through `sml-api-bybos`; SML
  readiness must be OK before the create action is allowed.
