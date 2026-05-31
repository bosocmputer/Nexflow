# BillFlow — ภาพรวมการทำงาน

> อัพเดตล่าสุด: 2026-05-20 20:03 +07
> ดู snapshot จาก server จริงเพิ่มที่ [current-state.md](current-state.md)

---

## ระบบทำงานยังไง

BillFlow รับบิล/ออเดอร์จาก LINE OA, Email IMAP, Shopee Excel, Lazada Excel และ TikTok Excel/CSV แล้วช่วย admin ตรวจข้อมูลก่อนส่งเข้า SML ERP อัตโนมัติ จุดสำคัญของระบบตอนนี้คือ workflow แบบ human-in-the-loop: AI ช่วยอ่านเอกสารและจับคู่สินค้า แต่ admin ยังเห็นสถานะ, route, error, source artifact และกด Retry ได้จากหน้าเว็บ

สำหรับ customer test ปัจจุบัน BillFlow รองรับทั้ง Shopee email purchase flow, marketplace Excel sale flow, และ Shopee Open API direct preview แบบ multi-shop: ดึงข้อมูลเข้าเป็นเอกสาร local, review รายการสินค้า, เลือกลูกค้า/ผู้ขาย/คลัง/ภาษีก่อนส่ง, และส่งเข้า SML REST ตามเส้นทางเอกสารที่ตั้งไว้. Bulk send ตอนนี้เป็น async job ที่ backend เก็บสถานะจริง เห็น progress, ปิด dialog แล้วกลับมาดูต่อได้, และ retry เฉพาะรายการที่ fail ได้. Shopee Open API live ใช้งานบน BillFlow main แล้ว โดยยังคง confirm แบบ review-first และไม่ auto-send เข้า SML.

---

## Input → Process → Output

```
LINE OA / Email / Excel Upload
        │
        ▼
Ingest
  - LINE webhook: /webhook/line/:oaId หรือ /webhook/line
  - EmailCoordinator: one goroutine per enabled imap_accounts row
  - Import handlers: Lazada generic / Shopee/Lazada/TikTok preview+confirm
        │
        ▼
AI + Matching
  - OpenRouter text/image/audio extraction
  - Mistral OCR for PDFs
  - F1 mapper + SML catalog similarity candidates
  - F2 anomaly checks
        │
        ▼
Manual Review
  - /bills, /sales-orders, /sale-invoices
  - /bills/:id, /sales-orders/:id, /sale-invoices/:id
  - edit/add/delete items
  - map item, create product, inspect artifacts/timeline
  - route preview + validation guard before send
        │
        ▼
SML Retry Dispatch
  - sale_reserve  → SML #1 JSON-RPC 213
  - saleorder     → SML #2 REST 248 default sale route
  - saleinvoice   → SML #2 REST 248 saleinvoice v4 endpoint
  - purchaseorder → SML #2 REST 248 purchase route
  - bulk send     → DB-backed async job, serial worker, retry failed only
        │
        ▼
PostgreSQL + Audit Logs + LINE admin notifications
```

---

## Component Map

```
billflow/
├── backend/cmd/server/main.go
│   ├── routes, handlers, services, cron jobs
│   └── migrations auto-run from backend/internal/database/migrations
│
├── LINE OA Human Inbox
│   ├── handlers/line.go                 webhook, media download, conversation writes
│   ├── handlers/chat_inbox.go           /api/admin/conversations
│   ├── services/line/registry.go        multi-OA token/secret registry
│   ├── /messages                        admin inbox
│   ├── /settings/line-oa                multi-OA config
│   └── /settings/quick-replies, /settings/chat-tags
│
├── Email Pipeline
│   ├── services/email/coordinator.go    per-account pollers
│   ├── services/email/imap.go           connect/search/fetch/mark seen
│   ├── handlers/email.go                attachment AI pipeline
│   └── /settings/email                  IMAP account admin UI
│
├── Import
│   ├── handlers/import.go               generic Lazada WIP
│   └── handlers/shopee_import.go        Shopee preview/confirm into local bills
│
├── SML + Catalog
│   ├── services/sml/client.go           SML #1 JSON-RPC sale_reserve
│   ├── saleorder_client.go              SML #2 saleorder default
│   ├── saleinvoice_client.go            SML #2 saleinvoice v4
│   ├── purchaseorder_client.go          SML #2 purchaseorder
│   ├── product_client.go, party_client.go
│   ├── /api/catalog/:code/image         authenticated SML image proxy
│   └── services/catalog                 embeddings + in-memory cosine index
│
└── Web UI
    ├── /dashboard, /bills, /bills/:id, /logs
    ├── /messages
    ├── /import, /import/shopee
    └── /settings/*
```

---

## Current Routes ที่ควรรู้

| Area | Routes |
|---|---|
| Health | `GET /health` |
| Auth | `POST /api/auth/login`, `GET /api/auth/me` |
| Bills | `GET /api/bills?limit=&cursor=`, `GET /api/bills/counts`, `GET /api/bills/:id`, `POST /api/bills/:id/retry`, async bulk send jobs, archive/restore/delete, item CRUD, timeline, artifact preview/download |
| Chat inbox | `/api/admin/conversations...` |
| LINE OA | `POST /webhook/line/:oaId`, `POST /webhook/line`, `/api/settings/line-oa...` |
| SSE | `POST /api/admin/events/token`, `GET /api/admin/events?t=...` |
| Email settings | `/api/settings/imap-accounts...` |
| Channel defaults | `/api/settings/channel-defaults...` |
| Catalog | `/api/catalog...` |
| Imports | `/api/import/upload`, `/api/import/confirm`, `/api/import/shopee/preview`, `/api/import/shopee/confirm` |
| Logs | `GET /api/logs?limit=&cursor=`; no `COUNT(*)` unless `include_total=true` |

---

## Background Jobs

| Job | เวลา/Trigger | หน้าที่ |
|---|---|---|
| EmailCoordinator | per `imap_accounts.poll_interval_seconds`, min 300s | poll IMAP, route general/Shopee/Shopee shipped |
| Daily Insight | `INSIGHT_CRON_HOUR`, default 08:00 | AI summary + optional LINE notify |
| Backup | `BACKUP_CRON_HOUR`, default 00:00 | `pg_dump` to `/app/backups` mounted as `~/billflow/backups` |
| Disk Monitor | daily 07:00 | LINE alert when disk usage exceeds threshold |
| LINE Token Checker | weekly | expiry reminder |
| Reply Token Cleanup | hourly | clear LINE reply tokens older than 1 hour |
| Tunnel Drift Monitor | daily 09:00 Bangkok | ping `PUBLIC_BASE_URL/health`, LINE alert if Cloudflare Quick Tunnel URL drifted |
| Data Lifecycle | daily, default 02:00 | archive `sent/skipped` docs older than 180 days, roll up/purge detailed audit + AI logs older than 90 days in batches |

---

## Production Data Lifecycle

- `/logs` uses cursor pagination with `limit`, `cursor`, `has_more`, and `next_cursor`. It avoids `COUNT(*)` by default; request `include_total=true` only when the UI explicitly needs a total.
- `/bills`, `/sales-orders`, and `/sale-invoices` use the same document list backend. Default lists show only active rows (`archived_at IS NULL`) and support `archived`, `date_from`, `date_to`, and cursor pagination while keeping legacy `page/per_page` compatibility.
- `/api/bills/counts` returns queue counts for `needs_review`, `pending`, `sent`, and `failed` in one query so list pages do not fire repeated count requests.
- SML audit rows keep compact debug fields such as `doc_no`, `route`, `error_code`, `message`, `request_id`, and `target_id`; full SML payload/response is not copied into every audit log row.
- `/settings/old-data` shows row counts, table sizes, oldest rows, retention policy, and dry-run purge summaries. Purge is manual, batch-safe, and nothing is selected by default.

---

## สถานะปัจจุบัน

| Channel | สถานะ |
|---|---|
| Shopee purchase email | ✅ Phase 1 customer-test focus; sends SML `purchaseorder` through `192.168.2.248:8080` |
| Email IMAP | ✅ multi-account DB-driven, Shopee email routing, artifacts, logs |
| LINE OA | ✅ code exists for human chat 2 ทาง, multi-OA, media, quick replies, status, notes, tags, create bill from chat; hidden/not central in Phase 1 |
| Shopee Excel | ✅ preview/dedup/create local bills; routes to `saleorder` or `saleinvoice` based on `/settings/channels` |
| Lazada Excel | ✅ local implementation for sale Excel: preview/dedup/create local bills; routes to `saleorder` or `saleinvoice` based on `/settings/channels`; deploy target is main + Henna |
| TikTok Excel/CSV | ✅ local-ready for sale Excel/CSV: preview/dedup/create local bills; routes to `saleorder` or `saleinvoice` based on `/settings/channels`; deploy target is main + Henna |
| Shopee API direct | ✅ live OAuth + multi-shop preview on BillFlow main; default ready-to-bill statuses, shipping/package/COD preview, no-SKU review-first, no auto-send SML |

## Current Document Menus

| กลุ่ม Sidebar | Menu | URL | SML route |
|---|---|---|---|
| งานฝั่งซื้อ | ใบสั่งซื้อ | `/bills` | `purchaseorder` |
| งานฝั่งขาย | ใบสั่งขาย | `/sales-orders` | `saleorder` |
| งานฝั่งขาย | ขายสินค้าและบริการ | `/sale-invoices` | `saleinvoice` |

ทั้ง 3 เมนูมีปุ่ม `ส่ง SML ทั้งหมด` สำหรับเอกสารสถานะพร้อมส่ง (`pending`) โดยมี preview/validation ก่อนส่งจริง.

## Async Bulk SML Send

- ใช้ใน `/bills`, `/sales-orders`, และ `/sale-invoices`.
- Frontend ยัง preview/validate รายการก่อนส่ง และจำกัดงานละไม่เกิน 100 บิล.
- เมื่อกดส่ง ระบบสร้าง job ใน `sml_bulk_jobs` และรายการย่อยใน `sml_bulk_job_items`.
- Backend worker ส่งเข้า SML ทีละบิล (`concurrency=1`) เพื่อลดความเสี่ยง duplicate/โหลดกระแทก SML.
- Dialog poll progress ทุก 1 วินาที แสดง sent/failed/skipped/remaining และสามารถปิดแล้วกลับมาดูต่อได้.
- ถ้าบางบิล fail ระบบสรุป error ให้ copy ได้ และ retry เฉพาะ failed rows เป็น job ใหม่.
- หน้า `/bulk-send-jobs` แสดงประวัติ batch ทั้งหมดแบบ read-only พร้อม filter สถานะ/ปลายทาง, progress, ผู้ทำรายการ, รายละเอียดรายบิล, และลิงก์กลับไปบิลต้นทาง.
- Live smoke ล่าสุดบน BillFlow main สร้าง SML purchaseorder `BF-PO26050001` สำเร็จจาก bulk job หนึ่งบิล.

---

## เอกสารอื่น

| ไฟล์ | เนื้อหา |
|---|---|
| [current-state.md](current-state.md) | snapshot จาก code + server + production DB |
| [deploy-instances.md](deploy-instances.md) | registry port/folder/container/tunnel ของแต่ละร้าน |
| [billflow-main-sml-api-architecture.md](billflow-main-sml-api-architecture.md) | architecture + data flow diagram ของ BillFlow main + sml-api-bybos |
| [sml-bulk-send-jobs.md](sml-bulk-send-jobs.md) | async bulk send SML: endpoints, DB tables, worker behavior, QA, and rollback notes |
| [phase1-test-checklist.md](phase1-test-checklist.md) | checklist สำหรับทดสอบ Phase 1 ก่อน demo/customer test |
| [line-oa.md](line-oa.md) | LINE OA human inbox |
| [email.md](email.md) | Email IMAP pipeline |
| [shopee-import.md](shopee-import.md) | Shopee Excel import |
| [phase1-guide.md](phase1-guide.md) | คู่มือใช้งาน Phase 1 |
| [README.md](../README.md) | setup, API, deploy notes |
| [AGENTS.md](../AGENTS.md) | blueprint สำหรับ Codex |
