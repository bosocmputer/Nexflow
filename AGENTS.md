# AGENTS.md — Nexflow

> อ่านไฟล์นี้ให้ครบก่อนเริ่ม code ทุกครั้ง — ห้าม assume สิ่งที่ไม่ได้ระบุ
> **local workspace:** `/Users/nontawatwongnuk/dev_bos/Nexflow`
> **production server:** `10.121.20.83` (`ubuntu`)
> **production folders:** `/mnt/data/nextstep-node-2/nexflow` (demo), `/mnt/data/nextstep-node-2/nexflow-aoy` (aoy), `/mnt/data/nextstep-node-2/nexflow-lanboon` (lanboon)
> **deploy flow:** ดู [docs/nextstep-server-deploy-flow.md](docs/nextstep-server-deploy-flow.md)
> **legacy DEV only:** `192.168.2.109` / ngrok / `/home/bosscatdog/billflow-henna`

---

## 0. Current UAT Handoff (2026-08-25)

Read this section first when resuming work in a new session.

- Application baseline is intentionally split for AOY-only UAT: AOY runs
  `e069f2b` (`fix: bind Shopee shop name to API tag`), while Demo and
  Lanboon remain on `82baa4a` (`Simplify marketplace stock quantity setup`). The
  Central SML Gateway is on `a53db50`
  (`fix: include usable SML unit status conventions`).
- Tenant databases, SML tenants, credentials, channel routes, feature flags, and
  deployment revisions remain isolated and must never be copied between
  instances without an explicit migration request.
- Verified on 2026-08-25 after deploying application commit `82baa4a`:
  Demo, AOY, Lanboon, and both Central Gateway health endpoints returned HTTP
  200. Each tenant backend resolved the Central Shopee Gateway on the shared
  Docker network.
- Migration 085 is applied on all three tenant PostgreSQL 16 databases. The new
  Catalog-generation, exact SML attempt, reservation, mapping-job, stock-policy
  job, and asynchronous stock-run tables/columns exist. Post-deploy checks found
  zero queued mapping/backfill/policy/stock jobs and zero active reservations.
- Marketplace activation remains tenant-scoped and fail-closed:
  - Demo: unit Catalog enabled for SML validation; grouped UI, conversion,
    reservation ledger, and set-stock remain disabled. Active generation 3 has
    206 products, 371 units, and 115 multi-unit products with no invalid units.
  - AOY: grouped UI and unit Catalog enabled; conversion is `active`; reservation
    ledger enabled; set-stock enabled under the user's explicit controlled-UAT
    risk acceptance. Runtime readiness is true for Catalog, mapping, and
    reservations. Of 72 aliases, 71 are ready and one TikTok alias needs review
    because its saved unit is no longer valid for the mapped item.
  - Lanboon: application code updated, but all five marketplace feature gates
    remain disabled pending tenant-specific preflight.
- Pre-deploy database backups from 2026-08-25 are stored under
  `/mnt/data/nextstep-node-2/nexflow-backups`: Demo
  `pre-deploy-20260825-080734.sql.gz`, AOY
  `pre-deploy-20260825-080912.sql.gz`, and Lanboon
  `pre-deploy-20260825-081050.sql.gz`.
- Activation backups are also stored under the same tenant folders. Demo and AOY
  use the `pre-marketplace-activate-20260825-044809.sql.gz` snapshots; Lanboon's
  code-alignment backup is `pre-marketplace-code-20260825-054410.sql.gz`.
- AOY is the active production UAT tenant. As of 2026-08-25 the user is actively
  testing the current AOY build. Do not treat silence as acceptance; inspect the
  reported tenant, record, and runtime logs before fixing any new feedback.

Current AOY UAT scope:

1. Marketplace sales flow: Marketplace input -> Nexflow bill -> Product Master
   mapping -> SML sale invoice -> SML stock recalculation.
2. Marketplace amount correctness: full product amount, discount, buyer-paid
   shipping item, and net document totals for Shopee API/Excel, Lazada Excel, and
   TikTok Excel.
3. Unified Marketplace Product Master shared by imports, Bill Detail, and Shopee
   stock mapping.
4. Shopee stock sync: exactly one SML warehouse/location, dry-run before writes,
   full stock numbers, shared-stock allocation, and excluded-location details.
5. SML set-product document expansion. Controlled AOY document
   `BF-INV26080028` was verified in SML and then deleted by the user. AOY document
   expansion is enabled; Shopee set-stock still lacks a real Shopee set-product
   UAT case.
6. UI under feedback: `/sale-invoices` input-channel filters/tags and compact
   `/shopee-operations` rows/status explanations. Shopee brand color is
   `#EE4D2D`.
7. Shopee READY_TO_SHIP Auto SML migrations 082–083 and UI are deployed. Demo and
   Lanboon have the global flag disabled. AOY has
   `SHOPEE_AUTO_SML_ENABLED=true`, but both AOY shop settings remain disabled,
   have no cutoff, and the durable queue is empty. Do not enable a shop without
   a controlled new order and the checklist in
   `docs/shopee-auto-sml-runbook.md`.
8. Auto SML persists `doc_time` immediately before its first SML write using
   Asia/Bangkok and reuses it across retries. It is not a fixed channel setting.
9. Controlled normal-product Shopee stock UAT passed for shop `264993963`, item
   `6278820512`, model `43634992848`, mapped to SML `AH-0033` / `แท่ง` at factor
   1. Preview `cd5a5944-8955-4660-b32d-baaccbe2e672` proved target stock 1 from
   SML balance 1 and pending demand 0. Manual sync
   `63ca255f-c56c-496f-b5f4-011bb79f2a99` changed Shopee 0 -> 1 with no error or
   unknown result; catalog read-back confirmed seller stock 1. Final preview
   `8a03d431-7263-4f9e-a7c7-d6cac213ecfb` was unchanged 1 -> 1. Automatic stock
   sync remains disabled.
10. Marketplace unit and Shopee-stock configuration UX was simplified in
    `9f2461f`–`82baa4a`: normal users only choose the SML item, unit, and one of
    two quantity modes: follow Marketplace quantity (multiplier 1) or set an
    integer SML quantity per Marketplace item. Sales/stock policy controls are
    no longer shown in the normal flow. A stock check promotes safe ready
    listings to managed and creates equal shared-stock allocation only for a
    completely unconfigured duplicate group; existing partial or explicit pool
    configuration is preserved. Manual sync is independent from the automatic
    schedule but still requires a fresh successful stock check and no safety
    pause.
11. AOY post-deploy stock check on 2026-08-25 passed for all 44 active Shopee
    listings: 6 targets would change, 38 were unchanged, and 0 were blocked.
    The previous 37 `stock_policy_blocked` listings no longer require a manual
    policy choice. Existing shared pools passed validation. The check found an
    excluded negative balance of -3 `กล่อง` in `AB-2 / 002`; it is displayed but
    excluded from the selected `AB-1 / 001` calculation. No Shopee stock write
    was performed during this validation. For shop `264993963`, automatic stock
    sync remains disabled and there is no active pause. On 2026-08-25 the user
    explicitly accepted the pending set-product UAT risk and enabled
    `SHOPEE_SET_STOCK_ENABLED=true` for the AOY instance only so AOY staff can
    test the first real mapped set product manually. Demo and Lanboon were not
    changed. Treat AOY's first real set-product preview/write as controlled UAT;
    do not enable the automatic schedule until the Seller Centre read-back and
    SML component-balance checklist pass.
    Desktop and 390px mobile QA passed for `/marketplace-aliases` and
    `/settings/shopee-stock`; the browser console had no new warnings/errors.
12. AOY-only Catalog Marketplace visibility is deployed at `990c028`. Migration
    086 adds an active-alias lookup index; `/settings/catalog` now shows bounded
    Shopee/Lazada/TikTok mapping counts for each SML item and lazy-loads the
    matched account, product, variant, and SKU details. Production browser QA
    found 41 mapped rows and 9 unmapped rows on the first 50-item page; opening
    an item showed five cross-channel variants correctly. Catalog remains at 65
    active products and the alias table remains at 72 rows. The pre-deploy AOY
    backup is `pre-deploy-20260825-084650.sql.gz`. Demo and Lanboon did not
    receive migration 086 or the Catalog UI/API change.
13. AOY-only excluded-location product identity is deployed at `039b9cf`.
    `/settings/shopee-stock` keeps the exact excluded warehouse/location summary
    while also showing bounded SML item details: item code, item name, unit,
    warehouse/location, and balance. Details are capped at 200 rows in the run
    summary with exact total/negative counts to protect preview response size.
    Production dry-run `ae495fbe-6240-4014-94dd-7058f98d1358` checked all 44
    listings with 0 changed and 0 blocked. The excluded AB-2/002 aggregate -3
    `กล่อง` was resolved into `AH-0001` (-1 กล่อง) and `AH-0003` (-2 กล่อง),
    including their SML names. Only the preview endpoint ran; no Shopee stock
    write endpoint was called. The pre-deploy backup is
    `pre-deploy-20260825-090208.sql.gz`. Demo and Lanboon were not deployed.
14. AOY-only Marketplace channel-label cleanup is deployed at `e069f2b`.
    `/sale-invoices`, `/marketplace-aliases`, and `/settings/catalog` use one
    input-channel vocabulary: `Shopee API`, `Shopee Excel`, `Lazada Excel`, and
    `TikTok Excel`. Because a scoped Shopee Product Master mapping is shared by
    API and Excel, both tags are shown; the connected shop is displayed inside
    the API tag as `Shopee API (Henna.milkford)`. Generic `บัญชีหลัก` labels and
    the detached shop-name line were removed. Production browser QA passed for
    the grouped Product Master, Catalog summary, and Catalog link dialog; AOY
    health and both routes returned HTTP 200. The two latest pre-deploy AOY
    backups are `pre-deploy-20260825-091802.sql.gz` and
    `pre-deploy-20260825-092638.sql.gz`. Demo and Lanboon remain on `82baa4a`.

Known deferred or incomplete validation:

- Lazada Open API is pending approval; current Lazada flow is Excel import.
- Marketplace settlement / accounts-receivable posting is deferred. Do not infer
  payment posting from the sales invoice flow.
- Cross-environment duplicate prevention was intentionally deferred after the
  user removed 13 restored/legacy duplicate SML documents.
- Shopee set-stock needs a future tenant/shop with a real set product before it
  can be declared production-validated end to end.
- Demo and Lanboon currently run the older `82baa4a` application revision and
  may also have paid features or Shopee runtime flags disabled. Verify both the
  deployed revision and per-instance settings instead of assuming AOY behavior.

Resume checklist:

1. Run `git status --short` and `git log -1 --oneline`.
2. Check `/health` for all three instances and the Shopee Gateway.
3. Ask for or inspect the newest AOY user feedback and the exact affected order,
   bill, SKU, shop, or SML document number.
4. Preserve tenant isolation and deploy the same committed code with
   `scripts/deploy_nextstep_instances.py`; change runtime flags only for the
   explicitly requested tenant.

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
marketplace_item_aliases -- scoped Marketplace Product Master shared by imports, bills, and Shopee stock
channel_defaults    -- per-(channel, bill_type): cust_code, endpoint, doc_format, WH/VAT overrides
imap_accounts       -- retained schema; runtime disabled in sales-only mode
app_settings        -- instance config UI (replaces most env vars)
sml_catalog         -- active SML products for deterministic lookup
sml_catalog_set_components -- normalized SML item_type=3 component definitions and diagnostics
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

Migrations: **001–084** (all idempotent/re-runnable). Full schema in `docs/current-state.md`.

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

Last updated: 2026-08-25 | Ports: edge 6323, backends 8110/8111/8112, postgres 5440/5441/5442
