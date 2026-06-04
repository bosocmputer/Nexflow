# Nexflow — Current State

Updated: 2026-05-31

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

Migrations applied: **001–055** (all idempotent)

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

---

## Feature Flags (current build)

```bash
VITE_PHASE=2
VITE_ENABLE_SALES_ORDERS=true
VITE_ENABLE_SHOPEE_EXCEL=true
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_CHAT=false
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

Verified on server: 2026-05-31

| Area | Count / state |
| --- | --- |
| `bills` | 84 |
| `bill_items` | 89 |
| `channel_defaults` | 4 |
| `imap_accounts` | 0 |
| `shopee_api_connections` | 1 active live connection |
| `sml_bulk_jobs` | 8 |
| `audit_logs` | 326 |

All 84 production bills are `sent` sale documents with SML payload/response
recorded. Marketplace sales route to **saleinvoice / SI**:

| source | bill_type | document_route | status | count |
| --- | --- | --- | --- | --- |
| shopee | sale | saleinvoice | sent | 46 |
| lazada | sale | saleinvoice | sent | 14 |
| tiktok | sale | saleinvoice | sent | 24 |

Current `channel_defaults` for marketplace sale routes point to
`/api/v1/ic/sale-invoices` with `doc_format_code=SI`. This is why the production
workflow should use `/sale-invoices` / `ขายสินค้าและบริการ` as the primary
marketplace path. `/sales-orders` remains enabled and must stay functional.

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
