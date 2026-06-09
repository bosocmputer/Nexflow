# AGENTS.md — Nexflow

> อ่านไฟล์นี้ให้ครบก่อนเริ่ม code ทุกครั้ง — ห้าม assume สิ่งที่ไม่ได้ระบุ
> **local workspace:** `/Users/nontawatwongnuk/dev_bos/Nexflow`
> **server folder:** `/home/bosscatdog/billflow-henna`
> **runtime/ports/env:** ดู [docs/current-state.md](docs/current-state.md) | **deploy:** ดู [docs/deploy-instances.md](docs/deploy-instances.md)

---

## 1. Tech Stack

```
Backend:   Go 1.24 (Gin)  module: nexflow
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
AI:        OpenRouter — gemini-2.5-flash-lite / gemini-2.5-flash / Mistral OCR / Whisper
Deploy:    Docker Compose + ngrok (fixed: animal-galvanize-tameness.ngrok-free.dev)
```

Ports: backend **8110**, frontend **3030**, postgres **5440**

---

## 2. Key Database Tables

```sql
bills               -- source, bill_type, status, sml_doc_no
bill_items          -- item_code, qty, unit_code, price, discount_amount
mappings            -- raw_name → item_code (F1 learning)
channel_defaults    -- per-(channel, bill_type): cust_code, endpoint, doc_format, WH/VAT overrides
imap_accounts       -- multi-account IMAP (DB-driven, not .env)
app_settings        -- instance config UI (replaces most env vars)
sml_catalog         -- SML products + 1536-dim embeddings
sml_bulk_jobs       -- async bulk SML send jobs
shopee_api_connections   -- Shopee OAuth multi-shop
doc_counters        -- atomic doc_no per prefix/period
processed_email_keys -- email dedup by Message-ID
audit_logs          -- all admin actions
```

Migrations: **001–055** (all idempotent). Full schema in `docs/current-state.md`.

---

## 3. SML Retry Routing (bills.go)

4-way dispatch on `source` + `bill_type` + `channel_defaults.endpoint`:

| source | bill_type | default route | client |
| --- | --- | --- | --- |
| line / email / lazada | sale | sale_reserve | SML #1 JSON-RPC :3248 |
| shopee / tiktok | sale | saleorder REST v3 | SML #2 :8080 |
| explicit endpoint | sale | saleinvoice v4 | SML #1 REST :8086 |
| shopee_shipped email | purchase | purchaseorder REST v3 | SML #2 :8080 |

SML #1: `provider=BRSMLST, db=smlst2016` | SML #2: `provider=SMLGOH, db=SML1_2026`

---

## 4. Key Services (navigate code)

```
MapperService      F1 fuzzy match (levenshtein ≥0.85 auto, 0.60-0.84 needs_review) + auto-learn
AnomalyService     F2 rules: price_zero/qty_zero/duplicate_bill=block; price_too_high/new_customer=warn
EmailCoordinator   one goroutine per imap_accounts row, polls ≥300s
LineRegistry       oa_id → LINE service (multi-OA)
PartyCache         in-memory SML customers/suppliers, boot + 6h refresh
Catalog            cosine-similarity index (1536-dim, text-embedding-3-small)
events/broker      in-process SSE pubsub (sync.RWMutex + buffered ch 16)
media/signer       HMAC-SHA256, /public/media/:id?t=, 1h TTL
WorkerPool         semaphore: 5 OpenRouter, 3 SML
ShopeeOpenAPI      OAuth2 multi-shop + settlement reconciliation
```

---

## 5. Critical Gotchas

1. **SML mojibake** — `marshalASCII()` escapes non-ASCII as `\uXXXX` in ALL 6 SML POST clients. SML Java reads body as Latin-1 always — `Content-Type charset` is ignored. File: `backend/internal/services/sml/json_ascii.go`. Storage (sml_payload, audit_logs) uses plain `json.Marshal`.

2. **doc_no SML bug** — pattern `prefix-YYYY` or `prefix-YY` silently drops docs in SML UI (never appears). Use `YYMM####` counter with no hyphen before year: `BF-SO260400001` ✅ vs `BF-SO-2026...` ❌. `doc_no` reuse on retry: bills.go saves to DB before SML call; retry reuses existing doc_no.

3. **channel_defaults empty** — all 4 retry paths fail with "ยังไม่ได้ตั้งค่า". Run Quick Setup at `/settings/channels`. `applyChannelOverrides()` overlays wh_code/shelf_code/vat_type/vat_rate per channel.

4. **IMAP Gmail** — App Password required (not real password). `poll_interval_seconds >= 300` enforced by DB CHECK. Mark-read after process prevents duplicates. `processed_email_keys` table provides durable dedup by Message-ID.

5. **LINE Push quota** — Free OA = 200 push/month. Reply API is free. `last_reply_token` cached from webhook → admin reply tries Reply first, falls back to Push only on token error. `ConsumeReplyToken` uses CTE + `SELECT FOR UPDATE` to prevent race.

6. **SSE auth** — EventSource cannot send custom headers. Flow: `POST /api/admin/events/token` (JWT-auth) → HMAC token → `GET /api/admin/events?u=<userId>&t=<token>`.

7. **SML 248 product lookup** — `{"data":null}` = SKU not found (not an error). Always set `SHOPEE_SML_UNIT_CODE` fallback — SML rejects `unit_code=""`.

8. **Party quick-create DROPPED** — SML API requires ~25 fields / returns NPE. Create in SML UI, click "รีเฟรช" in Nexflow.

9. **`app_settings` vs `.env`** — `SeedFromEnv()` removed. Config via `/settings/instance` UI. Locked fields (guid, etc.) still read from `.env`.

10. **sml-api-bybos** — use `--force-recreate` not `restart` after `.env` change. Docker gateway IP: `http://172.24.0.1:8200`, header `x-tenant: aoy`.

11. **Webhook URL per OA** — `/webhook/line/<oa_id>`. Must be set in LINE Developer Console per OA.

---

## 6. Deploy

```bash
# on server
cd ~/billflow-henna
docker compose build backend frontend && docker compose up -d
curl http://localhost:8110/health
docker logs nexflow-backend --tail=50
```

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
POST /api/bills/:id/retry         -- 4-way SML dispatch
POST /api/bills/bulk-send-jobs    -- async bulk (cap 100)
PUT  /api/bills/:id/items/:iid    -- edit + F1 auto-learn
POST /api/bills/:id/archive | DEL /api/bills/:id

GET/POST/PUT/DEL /api/mappings
GET  /api/mappings/stats

GET  /api/catalog | /api/catalog/search?q=
POST /api/catalog/sync | /api/catalog/embed-all

GET  /api/settings/imap-accounts  | POST ... | POST .../:id/poll
GET  /api/settings/channel-defaults | PUT ...
GET  /api/settings/instance | PUT ...
GET  /api/settings/line-oa  | POST ...

POST /api/import/shopee-api/orders/preview | .../confirm
POST /api/import/shopee/preview | /confirm
POST /api/import/lazada/preview | /confirm
POST /api/import/tiktok/preview | /confirm

GET  /api/sml/customers | /suppliers | POST /api/sml/refresh-parties
GET  /api/dashboard/stats | /api/logs | /api/bills/:id/timeline

POST /api/admin/conversations/:user/messages
POST /api/admin/events/token | GET /api/admin/events  -- SSE

POST /webhook/line/:oaId
GET  /public/media/:id?t=   -- HMAC-signed, no JWT
GET  /health
```

---

Last updated: 2026-05-31 | Ports: 8110/3030/5440
