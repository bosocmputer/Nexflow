# AGENTS.md — Nexflow

> อ่านไฟล์นี้ให้ครบก่อนเริ่ม code ทุกครั้ง — ห้าม assume สิ่งที่ไม่ได้ระบุ
> **local workspace:** `/Users/nontawatwongnuk/dev_bos/Nexflow`
> **production server:** `10.121.20.83` (`ubuntu`)
> **production folders:** `/mnt/data/nextstep-node-2/nexflow` (demo), `/mnt/data/nextstep-node-2/nexflow-aoy` (aoy), `/mnt/data/nextstep-node-2/nexflow-lanboon` (lanboon)
> **deploy flow:** ดู [docs/nextstep-server-deploy-flow.md](docs/nextstep-server-deploy-flow.md)
> **legacy DEV only:** `192.168.2.109` / ngrok / `/home/bosscatdog/billflow-henna`

---

## 1. Tech Stack

```
Backend:   Go 1.24 (Gin)  module: nexflow
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
AI:        Disabled — production is deterministic sales-only
Deploy:    Docker Compose + Cloudflare proxied domains
```

Production ports:

| instance | frontend | backend | postgres |
| --- | --- | --- | --- |
| demo | edge **6323**, debug **127.0.0.1:16323** | **8110** | **5440** |
| aoy | edge **6323**, debug **127.0.0.1:16324** | **8111** | **5441** |
| lanboon | edge **6323**, debug **127.0.0.1:16325** | **8112** | **5442** |

---

## 2. Key Database Tables

```sql
bills               -- source, bill_type, status, sml_doc_no
bill_items          -- item_code, qty, unit_code, price, discount_amount
mappings            -- verified exact raw_name → item_code fallback when SKU is absent
channel_defaults    -- per-(channel, bill_type): cust_code, endpoint, doc_format, WH/VAT overrides
imap_accounts       -- retained schema; runtime disabled in sales-only mode
app_settings        -- instance config UI (replaces most env vars)
sml_catalog         -- active SML products for deterministic lookup
sml_bulk_jobs       -- async bulk SML send jobs
shopee_api_connections   -- Shopee OAuth multi-shop
doc_counters        -- atomic doc_no per prefix/period
processed_email_keys -- retained historical email dedup data
audit_logs          -- all admin actions
shopee_order_snapshots         -- Shopee Realtime order state/timeline source
shopee_order_payment_snapshots -- cached Shopee escrow/payment breakdowns
shopee_sml_cancellations       -- Shopee cancelled-after-SML CN tracking
line_notification_deliveries   -- LINE notification outbox with Flex payload fallback
shopee_stock_settings          -- per-shop scope, percentage, interval, pause/dry-run state
shopee_stock_products          -- local Shopee item/model stock catalog
shopee_stock_mappings          -- Shopee model -> SML item/unit conversion
shopee_stock_runs/attempts     -- dry-run/sync history and changed/error/unknown writes
```

Migrations: **001–076** (all idempotent/re-runnable). Full schema in `docs/current-state.md`.

---

## 3. SML Sales Routing (bills.go)

3-way sales dispatch on `source` + `bill_type` + `channel_defaults.endpoint`:

| source | bill_type | default route | client |
| --- | --- | --- | --- |
| legacy line / email / lazada | sale | sale_reserve | SML #1 JSON-RPC :3248 |
| shopee / tiktok | sale | saleorder REST v3 | SML #2 :8080 |
| explicit endpoint | sale | saleinvoice v4 | SML #1 REST :8086 |

Purchase routing and ingestion are disabled. Historical purchase code/schema is
retained for a future redesign, but must not be started or exposed at runtime.

SML #1: `provider=BRSMLST, db=smlst2016` | SML #2: `provider=SMLGOH, db=SML1_2026`

---

## 4. Key Services (navigate code)

```
MapperService      exact user-verified raw-name fallback when marketplace SKU is absent
AnomalyService     F2 rules: price_zero/qty_zero/duplicate_bill=block; price_too_high/new_customer=warn
LineRegistry       oa_id → LINE service (multi-OA)
PartyCache         in-memory SML customers only in sales-only production
Catalog            database-only exact/prefix/contains product search
events/broker      in-process SSE pubsub (sync.RWMutex + buffered ch 16)
media/signer       HMAC-SHA256, /public/media/:id?t=, 1h TTL
WorkerPool         semaphore: 5 webhook tasks, 3 SML tasks
ShopeeOpenAPI      OAuth2 multi-shop + settlement reconciliation
```

---

## 5. Critical Gotchas

1. **SML mojibake** — `marshalASCII()` escapes non-ASCII as `\uXXXX` in ALL 6 SML POST clients. SML Java reads body as Latin-1 always — `Content-Type charset` is ignored. File: `backend/internal/services/sml/json_ascii.go`. Storage (sml_payload, audit_logs) uses plain `json.Marshal`.

2. **doc_no SML bug** — pattern `prefix-YYYY` or `prefix-YY` silently drops docs in SML UI (never appears). Use `YYMM####` counter with no hyphen before year: `BF-SO260400001` ✅ vs `BF-SO-2026...` ❌. `doc_no` reuse on retry: bills.go saves to DB before SML call; retry reuses existing doc_no.

3. **channel_defaults empty** — sales retry routes fail with "ยังไม่ได้ตั้งค่า". Run Quick Setup at `/settings/channels`. `applyChannelOverrides()` overlays wh_code/shelf_code/vat_type/vat_rate per channel.

4. **Sales-only capability guard** — AI, embedding, OCR, IMAP, LINE chat, and purchase runtime are disabled. Run `bash scripts/check_sales_only_runtime.sh` before deploy; compatibility APIs return `410 Gone`.

5. **LINE Push quota** — Free OA = 200 push/month. Reply API is free. `last_reply_token` cached from webhook → admin reply tries Reply first, falls back to Push only on token error. `ConsumeReplyToken` uses CTE + `SELECT FOR UPDATE` to prevent race.

6. **SSE auth** — EventSource cannot send custom headers. Flow: `POST /api/admin/events/token` (JWT-auth) → HMAC token → `GET /api/admin/events?u=<userId>&t=<token>`.

7. **SML 248 product lookup** — `{"data":null}` = SKU not found (not an error). Always set `SHOPEE_SML_UNIT_CODE` fallback — SML rejects `unit_code=""`.

8. **Party quick-create DROPPED** — SML API requires ~25 fields / returns NPE. Create in SML UI, click "รีเฟรช" in Nexflow.

9. **`app_settings` vs `.env`** — `/settings/instance` แก้ได้เฉพาะชื่อร้านและช่องทางติดต่อ. ค่า SML tenant/URL, public URL, Shopee gateway และ infrastructure อื่นจัดการผ่าน deployment runbook; ค่าเดิมใน `app_settings`/`.env` ยังเป็น runtime source และห้าม serialize ไปหน้า instance.

10. **sml-api-bybos** — current production gateway is `nexflow-sml-api-bybos` on `10.121.20.83:8200` with `ALLOWED_TENANTS=demo,aoy,lbk63`. Nexflow instances call `http://172.17.0.1:8200` and select tenant through `app_settings.sml.database` (`demo`, `aoy`, or `lbk63`). The NextStep SQL uses `FROM ic_trans ic_qt`; `ic_qt` is an alias, not a physical table. Do not use the old `192.168.2.109` / ngrok deploy path for production.

11. **Webhook URL per OA** — `/webhook/line/<oa_id>`. Must be set in LINE Developer Console per OA.

12. **Shopee LINE notifications** — `/settings/line-notifications` is active even though LINE chat UI is disabled. It sends rich Flex from `line_notification_deliveries.flex_payload`, falls back to `message_text`, uses Asia/Bangkok time, and must not include buyer name, phone, address, or username.

13. **Shopee payment breakdown** — `shopee_order_payment_snapshots` is populated by worker/manual refresh from `get_escrow_detail`. Page render and LINE worker must read cached snapshots only, never call Shopee live APIs inline.

14. **Central Shopee gateway** — production target is `nexflow-shopee-gateway` at `shopee-gateway.nextstep-soft.com`. Gateway mode stores Partner Key and encrypted access/refresh tokens only in the gateway DB. Every tenant receives a derived HMAC identity even while in direct mode so the gateway can discover active shop routes and deliver the one app-wide push callback during staged rollout; legacy tokens are never copied. Tenant `.env` uses `SHOPEE_OPEN_API_MODE=gateway` only after explicit cutover, while direct mode remains rollback. Push is authenticated/deduped centrally and tenant reconciliation still fetches order detail as source of truth. See `docs/shopee-gateway-runbook.md`.

15. **Shopee direct mode is rollback only** — do not create a new Shopee Open Platform App for each customer. The old per-customer cutover helper and direct Partner credentials are retained only for an explicit rollback while gateway rollout is incomplete.

---

## 6. Deploy

```bash
# deploy the same committed code to all production instances
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target all

# deploy one instance only when intentionally isolated
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target demo
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target aoy
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target lanboon
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target gateway
```

Adding the next customer instance uses the registry helper first:

```bash
python3 scripts/nextstep_instance_registry.py suggest
python3 scripts/nextstep_instance_registry.py add \
  --name <shop-key> \
  --hostname nextflow-<shop-key>.nextstep-soft.com \
  --sml-tenant <sml-db-or-tenant> \
  --sml-host <customer-pg-host> \
  --sml-port <customer-pg-port>
```

Commit/push the registry change, then bootstrap the server runtime folder and
SML API env with secrets stored only on the server. Never commit customer PG
passwords.

---

## 6b. Graphify Auto-Lite

Use Graphify as a context map for cross-subsystem work, not as source of truth.

Use Graphify before broad raw searches when work spans Shopee Open API, settlement, logistics, SML routing, email, LINE, backend/frontend behavior, and deployment docs.

Skip Graphify for small single-file edits, exact symbol lookups, logs, or test failure triage where `rg` and source reads are faster.

Commands:

```bash
bash scripts/graphify-update.sh
bash scripts/graphify-query.sh "Shopee order sync"
bash scripts/graphify-preflight.sh
```

Rules:

- Always open source files before editing.
- If Graphify disagrees with code or docs, code/docs win.
- `graphify-out/` is local-only and must remain untracked.
- Update Graphify manually after flow or architecture changes.
- Do not install Graphify hooks until the manual workflow has proven stable.

---

## 7. API Routes (key)

```
POST /api/auth/login
GET  /api/bills                   -- cursor: status, source, bill_type, date, archived
GET  /api/bills/:id               -- includes route preview
POST /api/bills/:id/retry         -- sales SML dispatch; purchase returns 410
POST /api/bills/bulk-send-jobs    -- async bulk (cap 100)
PUT  /api/bills/:id/items/:iid    -- edit + F1 auto-learn
POST /api/bills/:id/archive | DEL /api/bills/:id

GET/POST/PUT/DEL /api/mappings
GET  /api/mappings/stats

GET  /api/catalog | /api/catalog/search?q=
POST /api/catalog/sync
POST /api/catalog/embed-all       -- compatibility only, returns 410 Gone

ANY  /api/settings/imap-accounts* -- compatibility only, returns 410 Gone
GET  /api/settings/channel-defaults | PUT ...
GET  /api/settings/instance | PUT ...
GET  /api/settings/line-oa  | POST ...
GET/POST/PUT/DEL /api/settings/line-notifications

POST /api/import/shopee-api/orders/preview | .../confirm
POST /api/import/shopee/preview | /confirm
POST /api/import/lazada/preview | /confirm
POST /api/import/tiktok/preview | /confirm

GET  /api/sml/customers | /suppliers | POST /api/sml/refresh-parties
GET  /api/dashboard/stats | /api/logs | /api/bills/:id/timeline
GET  /api/shopee-operations/:shop_id/:order_sn/timeline
POST /api/shopee-operations/:shop_id/:order_sn/payment-breakdown/refresh
GET/POST /api/shopee-operations/:shop_id/:order_sn/cancel-sml-document(/preview)

ANY  /api/admin/conversations*    -- compatibility only, returns 410 Gone
POST /api/admin/events/token | GET /api/admin/events  -- SSE

POST /webhook/line/:oaId
GET  /public/media/:id?t=   -- HMAC-signed, no JWT
GET  /health
```

---

Last updated: 2026-08-10 | Ports: edge 6323, backends 8110/8111/8112, postgres 5440/5441/5442
