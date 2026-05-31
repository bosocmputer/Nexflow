# BillFlow

ระบบช่วยพนักงานลดเวลาคีย์บิลจาก **วันละ 100+ บิล** ลงเหลือ **เกือบ 0**
โดยใช้ AI extract ข้อมูลจากหลาย channel แล้วส่งเข้า ERP (SML) โดยอัตโนมัติ

---

## สารบัญ

1. [Overview](#1-overview)
2. [Tech Stack](#2-tech-stack)
3. [Server Info](#3-server-info)
4. [Architecture](#4-architecture)
5. [Quick Start (Local Dev)](#5-quick-start-local-dev)
6. [Environment Variables](#6-environment-variables)
7. [Database Schema](#7-database-schema)
8. [API Routes](#8-api-routes)
9. [Input Channels](#9-input-channels)
10. [AI Extraction](#10-ai-extraction)
11. [F1 — AI Learning Loop](#11-f1--ai-learning-loop)
12. [F2 — Anomaly Detection](#12-f2--anomaly-detection)
13. [F3 — Voice Input](#13-f3--voice-input)
14. [F4 — Daily AI Insights](#14-f4--daily-ai-insights)
15. [SML ERP Integration](#15-sml-erp-integration)
16. [File Import (Lazada/Shopee)](#16-file-import-lazadashopee)
17. [Background Jobs](#17-background-jobs)
18. [Role & Permissions](#18-role--permissions)
19. [Backup Strategy](#19-backup-strategy)
20. [Cloudflare Tunnel](#20-cloudflare-tunnel)
21. [Build Phases & Status](#21-build-phases--status)

---

## 1. Overview

| Input Channel | รายละเอียด | ประเภทบิล | Phase | สถานะ |
|---|---|---|---|---|
| LINE OA | human chat inbox: text/image/file/audio → `/messages` | บิลขาย | Phase 3 + session 13+ | ✅ chat 2 ทาง + เปิดบิลจาก chat |
| Email IMAP | multi-account attachment/body pipeline | บิลขาย/ซื้อตาม routing | Phase 5 + session 6+ | ✅ deployed |
| Shopee Excel | Export จาก Shopee Seller Center | บิลขาย | Phase 4a | ✅ deployed; SML send via Retry route |
| Lazada Excel | Export จาก Lazada Seller Center | บิลขาย | Phase 4b | ✅ deployed main + Henna |
| TikTok Excel/CSV | Export จาก TikTok Seller Center | บิลขาย | Phase 4c | ✅ deployed main + Henna |
| Shopee API direct | Shopee Open Platform order sync | บิลขาย | Phase 4d | ✅ live OAuth + multi-shop preview/import by selected shop; review-first before SML |


**Output:** สร้างบิลใน SML ERP ผ่าน JSON-RPC API + บันทึก log ลง PostgreSQL + แจ้ง admin ผ่าน LINE เมื่อเกิด error

---

## 2. Tech Stack

```
Backend:   Go 1.24  (Gin framework)
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
AI:        OpenRouter API (primary google/gemini-2.5-flash-lite; fallback google/gemini-2.5-flash)
LINE:      line-bot-sdk-go v8 (official)
Email:     IMAP polling (go-imap/v2)
Excel:     excelize v2.10.1
Deploy:    Docker Compose + Cloudflare Tunnel
```

---

## 3. Server Info

```
OS:      Ubuntu 24.04.4 LTS
Server:  192.168.2.109  (user: bosscatdog)
Docker:  29.3.0
SML #1:   http://192.168.2.213:3248  (JSON-RPC sale_reserve)
SML #2:   http://192.168.2.248:8080  (REST saleorder/saleinvoice/purchaseorder/product)
```

**Ports ที่ BillFlow ใช้ (ไม่ชนกับ project อื่นบน server)**

| Service | Port |
|---|---|
| billflow-backend | 8090 |
| billflow-frontend | 3010 |
| billflow-postgres | 5438 |

**Projects อื่นบน server ที่ห้ามกระทบ:** openclaw-admin (3000/5432), tcc (8080/5433/9092/8123/9000/6382), ledgioai (3004/5436/6381), centrix (3002/5001/5434/6380)

Server deploy folder: `/home/bosscatdog/billflow` (deployed copy, not a git checkout). See [docs/current-state.md](docs/current-state.md).

Phase 1 customer-test docs:

- [docs/phase1-guide.md](docs/phase1-guide.md) — คู่มือใช้งานบิลซื้อ Shopee ล่าสุด
- [docs/phase1-test-checklist.md](docs/phase1-test-checklist.md) — checklist ก่อน demo/customer test

---

## 4. Architecture

```
Cloudflare Tunnel
  api.your-domain.com → :8090  (backend)
  app.your-domain.com → :3010  (frontend)
         │
Go Backend (Gin) :8090
  ├── POST /webhook/line                    ← LINE OA events
  ├── POST /api/auth/login
  ├── GET  /api/bills                       ← cursor list (status/source/bill_type/date/archived)
  ├── GET  /api/bills/counts                ← queue counts for document lists
  ├── GET  /api/bills/:id
  ├── POST /api/bills/:id/retry             ← 4-way SML send (saleorder/saleinvoice/purchaseorder/sale_reserve)
  ├── POST /api/bills/bulk-send-jobs        ← async SML bulk send job
  ├── GET  /api/bills/bulk-send-jobs        ← list bulk send job history
  ├── GET  /api/bills/bulk-send-jobs/active ← resume active bulk send job
  ├── GET  /api/bills/bulk-send-jobs/:id    ← poll bulk send progress/results
  ├── POST /api/bills/bulk-send-jobs/:id/retry-failed
  ├── POST /api/bills/:id/archive           ← archive one bill
  ├── POST /api/bills/:id/restore           ← restore one bill
  ├── DEL  /api/bills/:id                   ← delete one bill
  ├── PUT  /api/bills/:id/items/:item_id    ← edit item + F1 auto-learn
  ├── POST /api/bills/:id/items             ← add new line item
  ├── DEL  /api/bills/:id/items/:item_id    ← delete line item
  ├── GET  /api/mappings
  ├── POST /api/mappings
  ├── PUT  /api/mappings/:id                ← edit raw_name + item_code + unit_code
  ├── DEL  /api/mappings/:id
  ├── POST /api/mappings/feedback           ← F1 (legacy; UpdateItem auto-saves now)
  ├── GET  /api/mappings/stats
  ├── GET  /api/catalog                     ← SML product catalog list
  ├── GET  /api/catalog/search              ← embedding similarity search
  ├── GET  /api/catalog/:code/units         ← valid SML units for product
  ├── GET  /api/catalog/:code/image         ← authenticated SML image proxy
  ├── GET  /api/catalog/:code/images        ← SML image metadata list
  ├── GET  /api/catalog/:code/images/:roworder ← authenticated SML image proxy
  ├── POST /api/catalog/products            ← create new SML product (+ embed bg)
  ├── POST /api/catalog/sync                ← bulk sync from SML 248
  ├── POST /api/catalog/embed-all           ← background embed
  ├── POST /api/catalog/reload-index        ← rebuild memory index
  ├── GET  /api/dashboard/stats
  ├── GET  /api/dashboard/insights
  ├── POST /api/dashboard/insights/generate
  ├── GET  /api/logs                        ← cursor Activity Log; total only with include_total=true
  ├── POST /api/import/upload               ← generic legacy Excel import
  ├── GET  /api/settings/lazada-config      ← Lazada SML route summary
  ├── POST /api/import/lazada/preview       ← Lazada Excel preview + dedup
  ├── POST /api/import/lazada/confirm       ← create local Lazada sale bills
  ├── GET  /api/settings/tiktok-config      ← TikTok SML route summary
  ├── POST /api/import/tiktok/preview       ← TikTok Excel/CSV preview + dedup
  ├── POST /api/import/tiktok/confirm       ← create local TikTok sale bills
  ├── GET  /api/settings/shopee-config
  ├── GET  /api/settings/shopee-api/status  ← Shopee Open API readiness
  ├── POST /api/shopee-api/auth-url         ← create Shopee OAuth URL
  ├── GET  /api/shopee-api/callback         ← Shopee OAuth callback (state auth)
  ├── GET  /api/shopee-api/connections      ← connected Shopee shop list
  ├── PATCH /api/shopee-api/connections/:id ← label/enable/disable shop
  ├── POST /api/import/shopee/api/preview   ← Shopee API preview, no bill writes
  ├── POST /api/import/shopee/preview       ← parse + dedup check
  ├── POST /api/import/shopee/confirm       ← ส่ง SML 248
  ├── GET  /api/settings/column-mappings/:platform
  ├── PUT  /api/settings/column-mappings/:platform
  ├── GET/PUT/DEL /api/settings/channel-defaults  ← per-channel cust_code (admin)
  ├── POST /api/settings/channel-defaults/quick-setup
  ├── GET  /api/sml/customers                      ← PartyCache search
  ├── GET  /api/sml/suppliers
  ├── POST /api/sml/refresh-parties
  └── GET  /api/sml/parties/last-sync
         │
    ┌────┴────┐
PostgreSQL   External APIs
  :5438        OpenRouter, SML 213 (sale_reserve), SML 248 (saleorder/saleinvoice/PO/product),
               LINE API, Mistral OCR, IMAP
```

---

## 5. Quick Start (Local Dev)

### Prerequisites
- Docker + Docker Compose
- Go 1.24+ (สำหรับ local build)
- Node.js 22+ (สำหรับ frontend dev)

### 1. Clone & configure

```bash
git clone <repo>
cd billflow
cp .env.example .env
# แก้ไข .env ใส่ credentials จริง
```

### 2. Start with Docker Compose

```bash
# Production mode
docker compose up -d

# Development mode (hot reload)
docker compose -f docker-compose.yml -f docker-compose.dev.yml up
```

### 3. Verify

```bash
curl http://localhost:8090/health
# → {"status":"ok","env":"development"}

curl http://localhost:3010
# → React app
```

### Default admin account

```
Email:    admin@billflow.local
Password: admin1234
```

### Run integration tests

```bash
bash scripts/test.sh all localhost:8090
```

---

## 6. Environment Variables

```bash
# Server
PORT=8090
ENV=development

# Database
DATABASE_URL=postgres://billflow:password@localhost:5438/billflow
DB_USER=billflow
DB_PASSWORD=changeme_strong_password

# JWT
JWT_SECRET=your-secret-key-min-32-chars
JWT_EXPIRE_HOURS=24

# LINE OA — reissue ก่อนใช้ทุกครั้ง
LINE_CHANNEL_SECRET=
LINE_CHANNEL_ACCESS_TOKEN=
LINE_ADMIN_USER_ID=

# Email IMAP config moved to /settings/email admin UI (multi-account)
# ไม่มี IMAP_* env vars อีกต่อไป

# OpenRouter
OPENROUTER_API_KEY=sk-or-xxx
OPENROUTER_MODEL=google/gemini-2.5-flash-lite
OPENROUTER_FALLBACK_MODEL=google/gemini-2.5-flash
OPENROUTER_AUDIO_MODEL=openai/whisper-1

# SML ERP #1 (LINE/Email — JSON-RPC)
SML_BASE_URL=http://192.168.2.213:3248
SML_ACCESS_MODE=sales

# SML ERP #2 (Shopee — REST saleorder default + saleinvoice legacy)
SHOPEE_SML_URL=http://192.168.2.248:8080
SHOPEE_SML_GUID=smlx
SHOPEE_SML_PROVIDER=SMLGOH
SHOPEE_SML_CONFIG_FILE=SMLConfigSMLGOH.xml
SHOPEE_SML_DATABASE=SML1_2026
SHOPEE_SML_DOC_FORMAT=INV
# cust_code per channel → managed via /settings/channels admin UI (channel_defaults table)
# SHOPEE_SML_CUST_CODE removed — set party_code in /settings/channels instead
SHOPEE_SML_SALE_CODE=       # รหัสพนักงานขาย
SHOPEE_SML_WH_CODE=WH-01    # รหัสคลัง (fallback)
SHOPEE_SML_SHELF_CODE=SH-01 # รหัสชั้นวาง (fallback)
SHOPEE_SML_UNIT_CODE=ถุง     # หน่วย fallback ⚠️ ต้องไม่ว่าง — SML reject unit_code=""
SHOPEE_SML_VAT_TYPE=0       # 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
SHOPEE_SML_VAT_RATE=7
SHOPEE_SML_DOC_TIME=09:00

# Auto-confirm
AUTO_CONFIRM_THRESHOLD=0.85

# Cron
INSIGHT_CRON_HOUR=8
BACKUP_CRON_HOUR=0
INSIGHT_LINE_NOTIFY=true
DISK_WARN_PERCENT=90

# Frontend
VITE_API_URL=http://localhost:8090
```

---

## 7. Database Schema

```sql
users                       -- auth + roles
bills                       -- all bills (status, source, bill_type, sml_order_id, remark)
bill_items                  -- line items + catalog candidates JSONB
bill_artifacts              -- original source artifacts on disk + metadata
mappings / mapping_feedback -- F1 learning
item_price_history          -- F2: avg/min/max price per item_code
daily_insights              -- F4: AI-generated daily summaries
platform_column_mappings    -- Lazada/Shopee column name config
audit_logs                  -- structured log (source, level, duration_ms, trace_id)
sml_catalog                 -- SML product catalog + 1536-dim embeddings
channel_defaults            -- per (channel, bill_type): party_code/name + endpoint URL + doc_format + doc_prefix + running format + WH/Shelf/VAT override
doc_counters                -- atomic per-prefix per-period sequential doc_no generator (avoids SML UI bug)
imap_accounts               -- multi-account IMAP config (replaces .env IMAP_* singleton)
line_oa_accounts            -- multi-OA credentials + mark-as-read toggle
chat_conversations          -- LINE human inbox threads
chat_messages / chat_media  -- chat messages and downloaded media
chat_quick_replies          -- canned replies for composer
chat_notes                  -- internal admin notes
chat_tags / chat_conversation_tags -- CRM tags
```

Key columns in `imap_accounts`: name, host, port, username, password, mailbox, filter_from,
filter_subjects[], channel (general/shopee/lazada), shopee_domains[], lookback_days (1-90),
poll_interval_seconds (≥300), enabled, last_polled_at, last_poll_status,
last_poll_found, last_poll_processed, last_poll_skipped, last_poll_details,
consecutive_failures.

Migrations (run in order, all idempotent):
- [001_init.sql](backend/internal/database/migrations/001_init.sql) — initial schema
- [002_audit_logging.sql](backend/internal/database/migrations/002_audit_logging.sql) — audit_logs structured columns
- [002_sml_catalog.sql](backend/internal/database/migrations/002_sml_catalog.sql) — sml_catalog + sml_order_id + candidates + extended source/status CHECK
- [003_channel_customer_defaults.sql](backend/internal/database/migrations/003_channel_customer_defaults.sql) — channel_customer_defaults (legacy, renamed to _v1 by 007)
- [004_shopee_shipped.sql](backend/internal/database/migrations/004_shopee_shipped.sql) — bills.source shopee_shipped
- [006_imap_accounts.sql](backend/internal/database/migrations/006_imap_accounts.sql) — imap_accounts table
- [007_channel_defaults.sql](backend/internal/database/migrations/007_channel_defaults.sql) — channel_defaults table (replaces .env SHOPEE_SML_CUST_CODE / SHIPPED_SML_CUST_CODE)
- [008_channel_defaults_doc_format.sql](backend/internal/database/migrations/008_channel_defaults_doc_format.sql) — adds doc_format_code column
- [009_channel_defaults_endpoint.sql](backend/internal/database/migrations/009_channel_defaults_endpoint.sql) — adds endpoint column
- [010_channel_defaults_endpoint_freeform.sql](backend/internal/database/migrations/010_channel_defaults_endpoint_freeform.sql) — drops CHECK; free-form URL/path
- [011_doc_no_format.sql](backend/internal/database/migrations/011_doc_no_format.sql) — doc_prefix + doc_running_format + doc_counters table
- [012_channel_defaults_inventory.sql](backend/internal/database/migrations/012_channel_defaults_inventory.sql) — adds wh_code + shelf_code + vat_type + vat_rate per channel (sentinel '' / -1 = "use server .env")
- [013_chat_inbox.sql](backend/internal/database/migrations/013_chat_inbox.sql) — drops chat_sessions, creates chat_conversations + chat_messages + chat_media (LINE chatbot → human chat refactor — session 13)
- [014_line_oa_accounts.sql](backend/internal/database/migrations/014_line_oa_accounts.sql) — line_oa_accounts table + chat_conversations.line_oa_id (multi-OA: 1 BillFlow ↔ N LINE OAs — session 13)
- [015_chat_quick_replies.sql](backend/internal/database/migrations/015_chat_quick_replies.sql) — chat_quick_replies table + 4 seed templates for the chat composer (Phase 4.4 — session 13)
- [016_chat_conversation_status.sql](backend/internal/database/migrations/016_chat_conversation_status.sql) — chat_conversations.status (open/resolved/archived) + auto-revive on inbound (Phase 4.2 — session 14)
- [017_chat_crm.sql](backend/internal/database/migrations/017_chat_crm.sql) — chat_conversations.phone + chat_notes + chat_tags + chat_conversation_tags m2m (CRM lite Phase 4.7+4.8+4.9 — session 14)
- [018_chat_reply_token.sql](backend/internal/database/migrations/018_chat_reply_token.sql) — chat_conversations.last_reply_token + last_reply_token_at + chat_messages.delivery_method (Hybrid Reply+Push API — session 15, makes admin replies free when within reply token window)
- [019_line_oa_mark_as_read.sql](backend/internal/database/migrations/019_line_oa_mark_as_read.sql) — line_oa_accounts.mark_as_read_enabled per-OA opt-in for LINE Premium "อ่านแล้ว" read receipts (session 17)
- [020_bill_remark.sql](backend/internal/database/migrations/020_bill_remark.sql) — bills.remark for admin-entered remark passed through to SML purchaseorder
- [037_data_lifecycle.sql](backend/internal/database/migrations/037_data_lifecycle.sql) — production data lifecycle: summary tables, log/bill indexes, cursor-friendly access paths
- [044_sml_bulk_jobs.sql](backend/internal/database/migrations/044_sml_bulk_jobs.sql) — DB-backed async SML bulk send jobs and per-bill progress/results
- [045_shopee_multi_shop.sql](backend/internal/database/migrations/045_shopee_multi_shop.sql) — Shopee Open API multi-shop metadata, active-shop index, and shop-scoped order uniqueness

Production DB note: server currently also has legacy `system_settings` and `sml_settings` tables that are not in current local migrations and are not referenced by current code.

Production data lifecycle:
- Detailed `/api/logs` and document lists are cursor-based to avoid expensive `COUNT(*) + OFFSET` patterns on large tables.
- Audit logs keep compact SML support fields instead of copying full `sml_payload` / `sml_response` into every row.
- Daily lifecycle job auto-archives `sent/skipped` bills older than 180 days and rollups/purges detailed audit + AI usage logs older than 90 days in batches.
- `/settings/old-data` exposes table size, row count, oldest rows, eligible archive/purge rows, and dry-run purge summaries. Purge is manual and nothing is selected by default.
- Bulk SML send uses `sml_bulk_jobs` and `sml_bulk_job_items`; jobs are capped at 100 bills, sent serially, and can retry failed rows only.

---

## 8. API Routes

### Auth

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/auth/login` | — | Login, returns JWT |
| GET | `/api/auth/me` | JWT | Current user info |

### Bills

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/bills` | JWT | List bills with cursor pagination; filters: status, source, bill_type, document_route, archived, date_from, date_to. Legacy page/per_page still works |
| GET | `/api/bills/counts` | JWT | Queue counts for needs_review/pending/sent/failed in one request |
| GET | `/api/bills/:id` | JWT | Bill detail with items |
| POST | `/api/bills/:id/retry` | JWT | Manual confirm/send → SML (4-way route: sale_reserve/saleorder/saleinvoice/purchaseorder) |
| POST | `/api/bills/bulk-send-jobs` | admin/staff | Create DB-backed async SML bulk send job from ordered bill IDs |
| GET | `/api/bills/bulk-send-jobs` | admin/staff | List bulk send job history with status/route filters and pagination |
| GET | `/api/bills/bulk-send-jobs/active` | admin/staff | Resume current active bulk send job for a document route/user |
| GET | `/api/bills/bulk-send-jobs/:job_id` | admin/staff | Poll bulk send progress, counts, and item results |
| POST | `/api/bills/bulk-send-jobs/:job_id/retry-failed` | admin/staff | Create a new bulk send job from failed rows only |
| POST | `/api/bills/:id/archive` | admin/staff | Archive one bill |
| POST | `/api/bills/:id/restore` | admin/staff | Restore one archived bill |
| DELETE | `/api/bills/:id` | admin/staff | Delete one bill |
| PUT | `/api/bills/:id/items/:item_id` | admin/staff | Edit qty/price/item_code (F1 auto-learn on item_code change) |
| POST | `/api/bills/:id/items` | admin/staff | Add a new line item |
| DELETE | `/api/bills/:id/items/:item_id` | admin/staff | Remove a line item |
| GET | `/api/bills/:id/timeline` | JWT | Audit timeline for a bill |
| GET | `/api/bills/:id/artifacts` | JWT | Source artifacts list |
| GET | `/api/bills/:id/artifacts/:artifact_id/preview` | JWT | Preview artifact |
| GET | `/api/bills/:id/artifacts/:artifact_id/download` | JWT | Download artifact |

### Mappings

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/mappings` | JWT | List all mappings |
| POST | `/api/mappings` | admin/staff | Create mapping |
| PUT | `/api/mappings/:id` | admin/staff | Update mapping |
| DELETE | `/api/mappings/:id` | admin | Delete mapping |
| GET | `/api/mappings/stats` | JWT | F1 accuracy stats |
| POST | `/api/mappings/feedback` | admin/staff | Human correction (F1) |

### Import

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/import/upload` | admin/staff | Upload Lazada Excel → preview bills |
| GET | `/api/settings/shopee-config` | JWT | Shopee SML defaults (pre-fill dialog) |
| GET | `/api/settings/shopee-api/status` | admin/staff | Shopee Open API readiness + connection state |
| POST | `/api/shopee-api/auth-url` | admin | Create Shopee OAuth URL with short-lived state |
| GET | `/api/shopee-api/callback` | state auth | Shopee OAuth callback; exchanges code for shop token |
| GET | `/api/shopee-api/connections` | admin/staff | List connected Shopee shops without exposing tokens |
| PATCH | `/api/shopee-api/connections/:id` | admin | Rename or soft-disable a connected Shopee shop |
| POST | `/api/import/shopee/api/preview` | admin/staff | Preview Shopee Open API orders for the selected shop without writing bills |
| POST | `/api/import/shopee/preview` | admin/staff | Parse Shopee Excel + shop-scoped dedup check |
| POST | `/api/import/shopee/confirm` | admin/staff | Create local Shopee bills with source shop metadata; SML send happens via bill Retry route |

### Logs

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/logs` | admin/staff | Activity log cursor pagination (`limit`, `cursor`, `has_more`, `next_cursor`); no total unless `include_total=true` |

### Settings

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/settings/status` | JWT | Connection status |
| GET | `/api/settings/column-mappings/:platform` | JWT | Column mapping config |
| PUT | `/api/settings/column-mappings/:platform` | admin | Update column mapping |
| GET | `/api/settings/line-oa` | admin | List LINE OA accounts |
| POST | `/api/settings/line-oa` | admin | Create LINE OA account |
| PUT | `/api/settings/line-oa/:id` | admin | Update LINE OA account |
| DELETE | `/api/settings/line-oa/:id` | admin | Delete LINE OA account |
| POST | `/api/settings/line-oa/:id/test` | admin | Test LINE OA token |
| GET/POST/PUT/DELETE | `/api/admin/quick-replies` | admin/staff, write admin | Quick reply templates |
| GET/POST/PUT/DELETE | `/api/settings/chat-tags` | admin/staff, write admin | Chat tag master |

### Settings — Channel Defaults (admin only)

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/settings/channel-defaults` | admin | List all (channel, bill_type) rows |
| PUT | `/api/settings/channel-defaults` | admin | Upsert row — set party_code/name per channel |
| DELETE | `/api/settings/channel-defaults/:channel/:bill_type` | admin | Delete row |
| POST | `/api/settings/channel-defaults/quick-setup` | admin | Auto-pair channels with SML placeholder AR00001-04 |
| GET | `/api/sml/customers` | admin/staff | Searchable customer list (PartyCache) |
| GET | `/api/sml/suppliers` | admin/staff | Searchable supplier list (PartyCache) |
| POST | `/api/sml/refresh-parties` | admin | Re-fetch party master from SML |
| GET | `/api/sml/parties/last-sync` | admin/staff | Last sync timestamp |
| GET | `/api/sml/units` | admin/staff | Searchable SML unit master from api-bybos `ic_unit` |

### Settings — Email Inboxes (admin only)

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/settings/imap-accounts` | admin | List all IMAP accounts |
| POST | `/api/settings/imap-accounts` | admin | Create new inbox |
| GET | `/api/settings/imap-accounts/:id` | admin | Inbox detail |
| PUT | `/api/settings/imap-accounts/:id` | admin | Update inbox |
| DELETE | `/api/settings/imap-accounts/:id` | admin | Delete inbox |
| POST | `/api/settings/imap-accounts/:id/poll` | admin | Manual poll-now |
| POST | `/api/settings/imap-accounts/test` | admin | Dry connect + auth + SELECT |
| POST | `/api/settings/imap-accounts/list-folders` | admin | List mailbox folders |

### Dashboard

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/dashboard/stats` | JWT | Stats overview |
| GET | `/api/dashboard/insights` | JWT | F4 AI insights list |
| POST | `/api/dashboard/insights/generate` | admin | Generate insight on-demand |

### Catalog (SML product catalog + smart matching)

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/catalog` | JWT | Paginated catalog list (filter: status, q) |
| GET | `/api/catalog/stats` | JWT | Total, embedded, pending, error counts |
| GET | `/api/catalog/search` | JWT | Cosine-similarity search (fallback to Levenshtein) |
| GET | `/api/catalog/:code` | JWT | Catalog item detail |
| GET | `/api/catalog/:code/units` | admin/staff | Valid units from SML `ic_unit_use`, with `ic_inventory.unit_standard` fallback |
| GET | `/api/catalog/:code/image` | JWT | Authenticated primary SML product image proxy |
| GET | `/api/catalog/:code/images` | JWT | SML product image metadata for gallery |
| GET | `/api/catalog/:code/images/:roworder` | JWT | Authenticated specific SML product image proxy |
| POST | `/api/catalog/sync` | admin | Sync from SML 248 product API |
| POST | `/api/catalog/import-csv` | admin | Bulk CSV upload |
| POST | `/api/catalog/embed-all` | admin | Background batch embedding |
| POST | `/api/catalog/reload-index` | admin | Rebuild in-memory index |
| POST | `/api/catalog/:code/embed` | admin | Embed single item |
| POST | `/api/catalog/:code/refresh` | admin | Refresh single item from SML |
| DELETE | `/api/catalog/:code` | admin | Delete catalog row |
| POST | `/api/bills/:id/items/:item_id/confirm-match` | admin/staff | Confirm catalog match (needs_review) |

### Chat Inbox + SSE

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/admin/conversations` | admin/staff | List LINE conversations |
| GET | `/api/admin/conversations/unread-count` | admin/staff | Unread count |
| GET | `/api/admin/conversations/:lineUserId/messages` | admin/staff | Message thread |
| POST | `/api/admin/conversations/:lineUserId/messages` | admin/staff | Send text reply |
| POST | `/api/admin/conversations/:lineUserId/messages/media` | admin/staff | Send media reply |
| POST | `/api/admin/conversations/:lineUserId/mark-read` | admin/staff | Mark conversation read |
| PATCH | `/api/admin/conversations/:lineUserId/status` | admin/staff | open/resolved/archived |
| PATCH | `/api/admin/conversations/:lineUserId/phone` | admin/staff | Save phone |
| GET/POST/PUT/DELETE | `/api/admin/conversations/:lineUserId/notes` | admin/staff | Internal notes |
| GET/PUT | `/api/admin/conversations/:lineUserId/tags` | admin/staff | Conversation tags |
| POST | `/api/admin/conversations/:lineUserId/messages/:messageId/extract` | admin/staff | Extract bill data from media |
| POST | `/api/admin/conversations/:lineUserId/bills` | admin/staff | Create bill from chat |
| GET | `/api/admin/conversations/:lineUserId/history` | admin/staff | Customer bill history |
| POST | `/api/admin/events/token` | JWT | Issue SSE token |
| GET | `/api/admin/events` | HMAC query token | Server-Sent Events stream |

### Webhook

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/webhook/line/:oaId` | LINE Signature | Multi-OA LINE events |
| POST | `/webhook/line` | LINE Signature | Legacy single-OA fallback |
| GET | `/public/media/:mediaID` | HMAC query token | Public media delivery for LINE |

---

## 9. Input Channels

### LINE OA — Human Chat Inbox (Phase 3 + session 13+)

```
LINE webhook → verify per-OA signature → save conversation/message/media
  → publish SSE to /messages
  → admin replies using cached Reply API token first
  → fallback to Push API when needed
  → admin can extract media and create a bill from chat

Old chatbot/cart/MCP flow was removed in session 13.
Webhook URLs:
  /webhook/line/:oaId  preferred multi-OA route
  /webhook/line        legacy fallback
```

### Email IMAP (Phase 5 + session 6: multi-account)

```
EmailCoordinator starts one goroutine per row in imap_accounts (enabled=true).
Each goroutine polls its mailbox every poll_interval_seconds (DB CHECK >= 300 s).

Per-account channel routing:
  subject "ถูกจัดส่งแล้ว" or "ยืนยันการชำระเงิน"
    → ProcessShopeeShippedEmailBody → purchaseorder (SML 248)
  from domain in shopee_domains && channel=shopee
    → ProcessShopeeEmailBody → create Shopee sale bill; Retry default saleorder (SML 248)
  else → download attachment → AI pipeline → pending/needs_review

Admin manages inboxes at /settings/email (no .env changes required).
AccountDialog: 4-section form, host-aware App Password popover,
test-connection button, list-folders button.
```

**Production inboxes (2026-04-28):**
- `bos.catdog@gmail.com` — channel=shopee — subjects: คำสั่งซื้อ, ถูกจัดส่งแล้ว
- `sutee.toe@gmail.com` — channel=shopee — subject: ยืนยันการชำระเงินคำสั่งซื้อหมายเลข #

**รองรับ email provider:**
- Gmail (ใช้ App Password — เปิด 2-Step Verification ก่อน)
- Outlook/Hotmail (`imap-mail.outlook.com:993`)
- IMAP ทั่วไป (port 993 TLS หรือ 143 STARTTLS)

**ข้อจำกัด Gmail:** poll ถี่เกินไป → `unexpected EOF` — min interval 5 min (enforced by DB CHECK)

### File Import — Shopee/Lazada/TikTok (Phase 4)

ดูรายละเอียดที่ [Section 16](#16-file-import-lazadashopee)

---

## 10. AI Extraction

**Model:** `google/gemini-2.5-flash-lite` first (fallback: `google/gemini-2.5-flash`)

**System Prompt Output Format:**
```json
{
  "doc_type": "sale | purchase",
  "customer_name": "string",
  "customer_phone": "string | null",
  "items": [
    { "raw_name": "string", "qty": 0, "unit": "string", "price": null }
  ],
  "total_amount": null,
  "note": null,
  "confidence": 0.95
}
```

- ถ้าข้อมูลไม่ชัดเจน → `confidence < 0.5`
- voice transcription → `confidence - 0.1`
- ไม่ใช่ JSON valid → retry ด้วย fallback model

---

## 11. F1 — AI Learning Loop

```
Match(rawName):
  1. Exact match      → confidence 1.0
  2. Fuzzy match (Levenshtein)
     + boost ถ้า usage_count สูง
     score >= 0.85  → auto map
     score 0.60-0.84 → needs_review
     score < 0.60   → unmapped

LearnFromFeedback(feedback):
  → INSERT/UPDATE mappings (source='ai_learned', confidence=1.0)
  → increment usage_count + update last_used_at

UpdatePriceHistory:
  → อัปเดต item_price_history ทุกครั้งที่ status='sent'
```

**Mapping ดู/แก้ได้ที่:** `/mappings` (Web UI)

---

## 12. F2 — Anomaly Detection

| Rule | Severity | เงื่อนไข |
|---|---|---|
| `price_zero` | block | ราคา = 0 |
| `qty_zero` | block | qty = 0 |
| `duplicate_bill` | block | same customer+items วันเดียวกัน |
| `price_too_high` | warn | > avg × 1.5 |
| `price_too_low` | warn | < avg × 0.5 |
| `qty_suspicious` | warn | > max_ever × 2 |
| `new_customer` | warn | ลูกค้าใหม่ |
| `new_item` | warn | สินค้าใหม่ |

**Auto-confirm ผ่านเมื่อ:**
- `final_confidence >= AUTO_CONFIRM_THRESHOLD` (default 0.85)
- ไม่มี block anomaly
- warn ไม่เกิน 1 รายการ

---

## 13. F3 — Voice Input

- LINE ส่ง audio message → download ทันที (มี expiry)
- ส่งไป OpenRouter Whisper → transcribe เป็น text
- ส่ง text ต่อไป AI extract pipeline
- voice > 60 วินาที → แจ้ง user ให้ส่งสั้นกว่า
- confidence ลด 0.1 อัตโนมัติสำหรับ voice

---

## 14. F4 — Daily AI Insights

**Cron 08:00 ทุกวัน** → generate + push LINE admin

```
ตัวอย่าง output:
📈 ยอดบิลสัปดาห์นี้สูงกว่าปกติ 23%
🏆 ปูนซีเมนต์ยังคงเป็นสินค้าขายดีอันดับ 1
⚠️ พบบิลราคาผิดปกติ 3 รายการรอ review
💡 ควรเพิ่ม stock CEM001 — ใช้ไปแล้ว 78%
```

สร้าง on-demand ได้ที่ `/dashboard` (admin) หรือ `POST /api/dashboard/insights/generate`

---

## 15. SML ERP Integration

### SML #1 — JSON-RPC (LINE OA + Email)

```
POST http://192.168.2.213:3248/api/sale_reserve
Headers:
  Content-Type: application/json
  mcp-access-mode: sales

Request body (JSON-RPC 2.0):
{
  "jsonrpc": "2.0", "id": 1, "method": "tools/call",
  "params": {
    "name": "create_sale_reserve",
    "arguments": {
      "contact_name": "...",
      "contact_phone": "...",
      "items": [{ "item_code": "...", "qty": 1, "unit_code": "...", "price": 100 }]
    }
  }
}

⚠️ response text เป็น JSON ซ้อนกัน → ต้อง parse 2 ชั้น
Success: {"success":true,"doc_no":"BS20260422XXXX","message":"create success"}
```

**Retry policy:** max 3 ครั้ง, backoff 1s/3s/5s
**หลัง fail 3 ครั้ง:** `status='failed'` + LINE admin push notify

### SML #2 — REST API (Shopee)

```
Base URL: http://192.168.2.248:8080
Auth headers (ทุก request): guid, provider, configFileName, databaseName

1. Product lookup  ← ✅ CONFIRMED WORKING
GET /SMLJavaRESTService/v3/api/product/{sku}
Response (flat — ไม่มี nested):
  {"success":true,"data":{"code":"...","unit_standard":"ถุง","start_sale_unit":"ถุง",
                           "start_sale_wh":"WH-01","start_sale_shelf":"SH-01"}}
  {"success":true,"data":null}  ← ถ้าไม่พบ SKU ใน SML
⚠️ ถ้า data=null → ใช้ fallback SHOPEE_SML_UNIT_CODE / WH_CODE / SHELF_CODE จาก .env
⚠️ ต้องตั้ง SHOPEE_SML_UNIT_CODE ไว้เสมอ (เช่น "ถุง") เพราะ SML reject unit_code=""

2. Create saleorder  ← default sale route for Shopee bills
POST /SMLJavaRESTService/v3/api/saleorder
Payload uses `items`. BillFlow chooses this route by default for `source=shopee` sale bills unless `channel_defaults.endpoint` explicitly contains `saleinvoice`.

3. Create saleinvoice  ← legacy/explicit endpoint
POST /SMLJavaRESTService/restapi/saleinvoice
{
  "doc_no": "",                    ← empty → SML auto-generates BS...
  "doc_format_code": "INV",
  "doc_date": "2026-04-24",        ← extracted from email body if available
  "cust_code": "AR00004",
  "is_permium": 0,                 ← int, typo intentional (matches SML)
  "vat_type": 0,                   ← 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
  "details": [{                    ← key ต้องเป็น "details" ไม่ใช่ "items"
    "item_code": "...", "unit_code": "ถุง",
    "wh_code": "WH-01", "shelf_code": "SH-01",
    "price_exclude_vat": ..., "sum_amount_exclude_vat": ...
  }]
}

4. Create purchaseorder  ← ✅ CONFIRMED WORKING (Shopee shipped emails)
POST /SMLJavaRESTService/v3/api/purchaseorder
Same payload shape as saleinvoice, except:
  - doc_no MUST be non-empty (v3 endpoint does NOT auto-generate; null
    triggers ic_trans NOT NULL constraint). BillFlow generates
    "BF-PO-YYYYMMDD-{8-char bill UUID}" client-side.
  - cust_code is semantically the supplier code
  - "buy_type" instead of "sale_type"
Client: backend/internal/services/sml/purchaseorder_client.go

5. Create product  ← ✅ CONFIRMED WORKING (used by MapItemModal)
POST /SMLJavaRESTService/v3/api/product
{
  "code": "TEST-001", "name": "ทดสอบ",
  "tax_type": 0, "item_type": 0, "unit_type": 1,
  "unit_cost": "ชิ้น", "unit_standard": "ชิ้น", "purchase_point": 0,
  "units": [{"unit_code":"ชิ้น","unit_name":"ชิ้น","stand_value":1,"divide_value":1}],
  "price_formulas": [{"unit_code":"ชิ้น","sale_type":0,"price_0":"99.5",
                      "tax_type":0,"price_currency":0}]
}
Response: {"success":true,"data":{"code":"..."}} (use response code as canonical)
Wired via POST /api/catalog/products → upserts sml_catalog + bg embed.
Client: backend/internal/services/sml/product_client.go
```

Unit dropdowns:
- `GET /api/sml/units` proxies api-bybos `GET /api/v1/ic/units` for the create-product unit dropdown.
- `GET /api/catalog/:code/units` proxies api-bybos `GET /api/v1/ic/products/:code/units` for Bill Detail / Sales Order / Sale Invoice item rows.
- api-bybos reads `ic_unit_use WHERE ic_code = :code`; if no unit-use row exists, it falls back to `ic_inventory.unit_standard`.
- The create-product quick form no longer asks for price; BillFlow sends initial `price: 0`.

**SML 248 config ที่ใช้งานจริง (confirmed 2026-04-24):**
```
guid=smlx  provider=SMLGOH  configFileName=SMLConfigSMLGOH.xml  databaseName=SML1_2026
doc_format=INV  cust_code=AR00004  wh_code=WH-01  shelf_code=SH-01
```

**SKU จริงใน SML 248:** CON-xxxxx (ถุง), STEEL-xxxxx (เส้น), PLUMB-xxxxx (ท่อน), ROOF-xxxxx (แผ่น)

**ดูรายละเอียด Shopee import:** [docs/shopee-import.md](docs/shopee-import.md)
⚠️ SML 248 (192.168.2.248) ต้องเปิดเครื่องก่อนใช้งาน

---

## 16. File Import

### Shopee Excel Import (Phase 4a) ✅ Deployed

```
URL: /import/shopee

1. กด "เลือกไฟล์ Shopee" → ระบบ GET /api/settings/shopee-config + ตรวจ channel_defaults
2. ถ้า Shopee sale config ยังไม่พร้อม UI จะ block file picker และพาไป `/settings/channels`
3. เปิด file picker → เลือก .xlsx จาก Shopee Seller Center
4. POST /api/import/shopee/preview
   - parse Excel (columns ภาษาไทย hardcoded)
   - ตรวจ duplicate: SELECT FROM bills WHERE source='shopee' AND order_id=?
   - exclude สถานะ: "ที่ต้องจัดส่ง", "ยกเลิกแล้ว"
5. Preview table: เห็นทุก order, duplicate badge สีเหลือง
6. เลือก orders (non-duplicate pre-checked) → ยืนยัน
7. POST /api/import/shopee/confirm
   - pre-fetch product info จาก SML 248
   - save bill + items ลง DB
   - SML send happens later from BillDetail Retry
8. Admin เปิดบิล → ตรวจ route preview → Retry ส่ง default saleorder
```

**Shopee column names (hardcoded — ไฟล์ Shopee Seller Center คงที่):**

| Field | Column Name |
|---|---|
| order_id | หมายเลขคำสั่งซื้อ |
| status | สถานะการสั่งซื้อ |
| order_date | วันที่ทำการสั่งซื้อ |
| product_name | ชื่อสินค้า |
| sku | เลขอ้างอิง SKU (SKU Reference No.) |
| price | ราคาขาย |
| qty | จำนวน |

**ดูรายละเอียด:** [docs/shopee-import.md](docs/shopee-import.md)

### Lazada Excel Import (Phase 4b) ✅ Local Implementation

Dedicated flow now mirrors Shopee Excel:
- `/api/import/lazada/preview` parses Lazada Seller Center export, groups by `orderNumber`, checks duplicates, and filters sale-ready statuses (`confirmed`, `shipped`, `delivered`).
- `/api/import/lazada/confirm` creates local sale bills with `source='lazada'`; SML send happens later through Bill Detail Retry.
- `/import/lazada` is enabled for Phase 1+ sales instances. Deploy target: BillFlow main + Henna, not Thaisunsport Phase 1.

### TikTok Excel/CSV Import (Phase 4c) ✅ Deployed Main + Henna

Dedicated flow mirrors Shopee/Lazada Excel:
- `/api/import/tiktok/preview` parses TikTok Seller Center `.csv` or `.xlsx`, groups by `Order ID`, checks duplicates, and filters sale-ready statuses (`จัดส่งแล้ว`, `shipped`, `delivered`).
- Uses `SKU ID` as source SKU fallback when `Seller SKU` is empty, and uses `SKU Subtotal After Discount / Quantity` as item price.
- Keeps `Order Amount` at order level so multi-row orders are not double-counted.
- `/api/import/tiktok/confirm` creates local sale bills with `source='tiktok'`; SML send happens later through Bill Detail Retry.
- Deployed target: BillFlow main + Henna, not Thaisunsport Phase 1.

---

## 17. Background Jobs

| Job | Schedule | Description |
|---|---|---|
| IMAP Coordinator | per-account interval (≥5 min) | N goroutines (one per imap_accounts row), each polls its mailbox every poll_interval_seconds |
| Daily Insight | Cron 08:00 | F4 generate + LINE notify |
| Backup | Cron 00:00 | `pg_dump` → `backups/YYYYMMDD.sql.gz` |
| Token Checker | ทุกอาทิตย์ | ตรวจ LINE token — แจ้งล่วงหน้า 7 วัน |
| Disk Monitor | ทุกวัน | แจ้ง admin ถ้า disk > `DISK_WARN_PERCENT` (90%) |
| SML Bulk Send Worker | on job create | ส่ง SML ทีละบิลจาก `sml_bulk_jobs`, เก็บ progress/result, และ retry failed ได้ |

---

## 18. Role & Permissions

| Permission | admin | staff | viewer |
|---|---|---|---|
| ดู bills ทั้งหมด | ✅ | ✅ | ✅ |
| confirm pending bills | ✅ | ✅ | — |
| retry failed bills | ✅ | ✅ | — |
| import Excel files | ✅ | ✅ | — |
| จัดการ mappings | ✅ | ✅ | — |
| ดู dashboard + insights | ✅ | ✅ | ✅ |
| generate insight on-demand | ✅ | — | — |
| settings (LINE/IMAP/SML/columns) | ✅ | — | — |
| จัดการ users | ✅ | — | — |
| ดู audit log | ✅ | — | — |

---

## 19. Backup Strategy

```bash
# สร้าง backup ทุกวัน 00:00
docker exec billflow-postgres \
  pg_dump -U billflow billflow \
  > ~/billflow/backups/$(date +%Y%m%d).sql

# เก็บ 30 วัน
find ~/billflow/backups -mtime +30 -delete
```

Backup files อยู่ที่: `~/billflow/backups/`

---

## 20. Cloudflare Tunnel

```bash
# cloudflared binary อยู่ที่ ~/cloudflared

# 1. Install
sudo cp ~/cloudflared/cloudflared /usr/local/bin/
sudo chmod +x /usr/local/bin/cloudflared

# 2. Login + สร้าง tunnel
cloudflared tunnel login
cloudflared tunnel create billflow

# 3. Config: ~/.cloudflared/config.yml
tunnel: <TUNNEL_ID>
credentials-file: /home/bosscatdog/.cloudflared/<TUNNEL_ID>.json
ingress:
  - hostname: api.your-domain.com
    service: http://localhost:8090
  - hostname: app.your-domain.com
    service: http://localhost:3010
  - service: http_status:404

# 4. systemd service
cloudflared service install
sudo systemctl enable cloudflared
sudo systemctl start cloudflared

# LINE Webhook URL:
# https://api.your-domain.com/webhook/line
```

---

## 21. Build Phases & Status

| Phase | Description | Status |
|---|---|---|
| 0 | Server prep (Go install, disk cleanup, cloudflared install) | ✅ Done |
| 1 | Foundation: Docker, DB migrations, JWT auth, Login UI | ✅ Done |
| 2 | Core AI pipeline: OpenRouter, MapperService (F1), AnomalyService (F2), SML client, WorkerPool | ✅ Done |
| 3 | LINE integration: human inbox, multi-OA, Reply/Push, media, quick replies, notes/tags, create bill from chat | ✅ Done |
| 4a | Shopee import: Excel → local bills → Retry default SML 248 saleorder | ✅ Done |
| 4b | Lazada import: Excel parser + Web UI | ✅ Deployed main + Henna |
| 4c | TikTok import: Excel/CSV parser + Web UI | ✅ Deployed main + Henna |
| 4d | Shopee API direct: Open Platform auth/token/order preview → local bills → existing review/SML retry flow | ✅ Live OAuth + multi-shop preview hardening; SML routing still shared v1 |
| 5 | Email IMAP polling + attachment pipeline (Mistral OCR + Shopee email order + Shopee shipped → PO) | ✅ Done |
| 5+ | Manual-confirm flow — auto-send removed; user confirms in BillDetail UI | ✅ Done |
| 6 | Web UI complete. Session 6: Tailwind/shadcn redesign + multi-account IMAP + artifacts. Session 7: channel_defaults + /settings/channels + SML party cache. Session 7-10: saleorder default + endpoint URL + doc_no generator + /logs redesign. Session 11: per-channel WH/Shelf/VAT override + ShopeeImport dialog removed + scrollable EditDialog. Session 12: marshalASCII permanent SML mojibake fix + catalog per-row Refresh/Delete. Session 13: LINE chatbot → human chat inbox + multi-OA (/messages, /settings/line-oa, webhook /webhook/line/:oaId, ~900 LOC chatbot removed) + Phase 4 quick wins (4.4 quick replies, 4.5 customer history panel, 4.11 browser notifications + chime). Session 14: composer redesign (auto-grow, paste, drag-drop) + admin send-image via Cloudflare Quick Tunnel + HMAC-signed /public/media/ URLs + conversation status (open/resolved/archived + auto-revive) + server-side inbox/thread search + CRM lite (phone detect, internal notes, tags + /settings/chat-tags). Session 15: hybrid Reply+Push API saving the 200/mo Free OA push quota (cached replyToken from inbound webhook → admin reply uses free Reply API; falls back to Push only when token expires). Session 16: closed audit log gaps for chat metadata (notes/tags/quick-reply CRUD + phone) — 17 new Thai-labeled actions in /logs; archived chats now disable composer with banner; CreateBillPanel auto-prefills phone from conversation; tag filter in inbox (multi-select with chips). Session 17: real-time inbox via Server-Sent Events (in-process broker + HMAC-signed token + EventSource singleton) — sub-second updates without WebSocket; polling kept at 30/60s safety net. Connection state indicator dot in sidebar. LINE markAsRead opt-in toggle per OA (Premium feature). Hourly stale reply-token cleanup. Server-restart pending-message recovery on boot. Self-tab dedup logic prevents duplicate bubbles when admin sends. Fixed major useEffect bug that was spamming mark-read every 30s and on every search keystroke. Session 18: 6-phase UX polish — /logs shows actual message content + Reply/Push chip; bill failures get a structured card (route badge + monospace error + copy-for-dev); sidebar reorganized into 5 domain groups (Overview / Bills / Chat / Master Data / System) with Thai labels + English tooltip hints; per-bill timeline reuses /logs ACTION_META; inline Retry button on /logs sml_failed rows; Dashboard "ต้อง action" widget with 4 click-through cards (บิลรอตรวจ / บิลล้มเหลว / ข้อความใหม่ / Email มีปัญหา) + URL-filter deep links. Session 19: heuristic evaluation pass — 16 fixes across all admin pages. Sprint A: lib/labels.ts SSOT (single status name everywhere), /settings rewrite with live multi-account status (LINE OA + IMAP counts) + Lazada column mapping moved into /import, composer disabled visual + Messages mobile responsive (back button), Catalog ↔ Mappings explainer banners, inline Retry on collapsed /logs rows. Sprint B: ShopeeImport preflight blocks file picker when channel config missing, Mappings empty-state CTA, tag-flow cross-links, Extract→CreateBill toast bridge, sidebar hints visible in expanded mode, BillDetail breathing room, Quick Setup tooltip. Sprint C: composer attachment count, catalog embedding async explainer, conversation freshness (relative time). Session 20: Send-to-SML validation guard (BillTotal disabled when items have unmapped item_code/unit_code, qty=0, or price=0; warning card lists each issue + "ดู →" jump-to-row; per-row AlertCircle icon in tiny status column). Route preview chip below Send button surfaces SML route + doc_no pattern so admins catch misconfigured channels before retry. Cloudflare Quick Tunnel drift monitor — daily 9am cron pings PUBLIC_BASE_URL/health and LINE-alerts admin (with inline recovery commands) when the tunnel URL has rolled. | ✅ Done |
| 7 | Background jobs: insight cron, backup cron (verified), token checker, disk monitor | ✅ Done |
| 8 | Production: Cloudflared named tunnel + systemd | ⏳ cloudflared installed, not configured (needs domain) |

### Latest Production Check (2026-05-20)

```
Server folder: /home/bosscatdog/billflow
Containers:
  billflow-backend   Up, :8090
  billflow-frontend  Up, :3010
  billflow-postgres  Up healthy, :5438
Health:
  {"database":"ok","env":"production","status":"ok"}
DB:
  migrations through 045 present in code, including Shopee Open API,
  SML catalog image metadata, async SML bulk job tables, and Shopee
  multi-shop metadata / shop-scoped duplicate protection.
SML images:
  active tenant SML1_2026 uses sml1_2026_images; image lookup index
  images_trim_image_id_order_roworder_file_idx verified.
Shopee Open API:
  live OAuth/token flow is available. /import/shopee lists connected shops,
  lets admin choose one shop for API/Excel preview, stores shop_id/label on
  created bills, and prevents duplicates per (shop_id, order_id).
Async SML bulk send:
  deployed on main; DB-backed jobs with progress/resume/retry-failed.
  /bulk-send-jobs history page deployed for admin/staff; list/detail API
  smoke-tested after deploy.
  Live smoke passed: job 128ceffe-5055-4863-8944-c6ce52301d26
  sent bill 20275aed-fe5f-402f-9160-a93a3f5b2ccb to SML PO BF-PO26050001.

Email IMAP pipeline (2026-04-24):
✅ SASL PLAIN auth (Gmail App Password)
✅ Poll ทันทีตอน start + ทุก 5 นาที
✅ Inline PDF attachment detected (Mistral OCR)
✅ AI extract → mapper → anomaly → DB → manual confirm
✅ Bill sent: BS20260424045830-FE5J + 2 others
✅ Unmapped items → needs_review + LINE notify
✅ เพิ่ม mapping ใหม่ + retry → SML สำเร็จ

Shopee email shipped → SML purchaseorder (2026-04-27):
✅ Subject "ถูกจัดส่งแล้ว" routes to ProcessShopeeShippedEmailBody
✅ AI extracts items + customer from HTML body
✅ doc_date extracted from "ถูกจัดส่งแล้วเมื่อวันที่ DD/MM/YYYY"
✅ Bill created: bill_type=purchase, source=shopee_shipped, status=needs_review
✅ Manual confirm → POST /SMLJavaRESTService/v3/api/purchaseorder
   doc_no="BF-PO-20260427-3e3a609a", HTTP 200, status=sent
✅ extractShopeeOrderID handles alphanumeric IDs (e.g. 26040408YSU4VR)

Create new SML product flow (2026-04-27):
✅ POST /api/catalog/products → POST /SMLJavaRESTService/v3/api/product
✅ Test code TEST-CREATE-1777266192 created in SML
✅ sml_catalog upsert + background embedding (status=done in <5s)
✅ /api/catalog/search returns it as top embedding match

F1 mapping feedback (2026-04-27):
✅ User changes item_code on a row in BillDetail
✅ ai_learned mapping created automatically
✅ Toast: "✓ จดจำการจับคู่นี้แล้ว — ครั้งถัดไประบบจะ map ให้อัตโนมัติ"

Add/Delete bill items (2026-04-27):
✅ POST /api/bills/:id/items + audit bill_item_added
✅ DELETE /api/bills/:id/items/:item_id + audit bill_item_deleted

Backup cron (2026-04-27):
✅ pg_dump from inside backend container (postgresql-client added to Dockerfile)
✅ 20 MB .sql.gz file produced + accessible on host volume mount

Marketplace import / next phase:
✅ Lazada Excel import deployed to main + Henna (Phase 4b)
✅ TikTok Excel/CSV import deployed to main + Henna (Phase 4c)
✅ Shopee Excel deployed; imports ready-to-ship rows and routes through local review/SML retry
✅ Shopee API direct deployed on BillFlow main; live OAuth, multi-shop selection, status/time filters, shipping/package/COD preview, and review-first import are ready
⬜ Thaisunsport remains Phase 1 purchase-only until explicitly enabled for sales
```

---

## 22. Gmail IMAP Setup สำหรับติดตั้งที่ร้านลูกค้า

### ทำไมต้องใช้ App Password แทน password จริง

Google บังคับใช้ **2-Step Verification** สำหรับ IMAP โดยตรง → ต้องสร้าง App Password แยกต่างหาก
ไม่ต้องผ่าน OAuth2 consent screen — เหมาะสำหรับ server-side automation

---

### ขั้นตอนสมัคร/ตั้งค่า Gmail สำหรับลูกค้า

#### ขั้นที่ 1 — เปิด 2-Step Verification (ถ้ายังไม่ได้เปิด)

1. เข้า [https://myaccount.google.com](https://myaccount.google.com)
2. เมนู **Security** → **2-Step Verification** → **Get started**
3. ทำตามขั้นตอน (ใช้ SMS หรือ Authenticator app)
4. ยืนยันว่า Status = **On**

#### ขั้นที่ 2 — สร้าง App Password

1. กลับไปที่ [https://myaccount.google.com/security](https://myaccount.google.com/security)
2. ค้นหา **App passwords** (หรือเข้าตรง [https://myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords))
3. กด **Create** → ตั้งชื่อ เช่น `BillFlow IMAP`
4. Google จะแสดง **password 16 หลัก** (ไม่มีช่องว่าง) เช่น `abcd efgh ijkl mnop`
5. **Copy ทันที** — แสดงครั้งเดียว ถ้าปิดหน้าต้อง generate ใหม่

#### ขั้นที่ 3 — เปิด IMAP ใน Gmail Settings

1. เปิด Gmail → **Settings** (ไอคอนฟัน) → **See all settings**
2. Tab **Forwarding and POP/IMAP**
3. **IMAP access** → เลือก **Enable IMAP**
4. กด **Save Changes**

#### ขั้นที่ 4 — เพิ่ม inbox ที่ `/settings/email` (admin UI)

⚠️ ตั้งแต่ session 6 (2026-04-28) **เลิกใช้ `.env` IMAP_*** — config ทั้งหมดเก็บใน DB
ผ่านตาราง `imap_accounts` แก้ผ่าน admin UI ที่ http://192.168.2.109:3010/settings/email

1. เข้า `/settings/email` (admin only)
2. คลิก **+ เพิ่ม Inbox** → เลือก preset "Gmail + Shopee" หรือ "Gmail + PDF" เพื่อ pre-fill
3. กรอก:
   - **ชื่อ inbox** — เพื่อแยกแยะ (เช่น "Shopee main")
   - **Username** — `อีเมล@gmail.com`
   - **Password** — App Password 16 หลักที่ copy มาจากขั้นที่ 2 (ไม่ใช่ password จริง)
   - **Channel** — `shopee` ถ้าเป็น inbox Shopee, `general` ถ้ารับ PDF/Excel แนบ
   - **Filter Subjects** — keyword ใน subject (เช่น `ถูกจัดส่งแล้ว`, `ยืนยันการชำระเงิน`)
   - **Poll interval** — 5+ นาที (ขั้นต่ำเพื่อกัน Gmail rate limit)
4. คลิก **ทดสอบการเชื่อมต่อ** → ต้องเห็น ✅ "เชื่อมต่อสำเร็จ"
5. คลิก **บันทึก** → coordinator จะ spawn poller goroutine ทันที

```bash
# ทดสอบ connection ผ่าน CLI
curl -v --ssl-reqd 'imaps://imap.gmail.com:993/INBOX' \
  --user 'ชื่ออีเมล@gmail.com:apppassword16หลัก' 2>&1 | head -20
# ต้องเห็น "* OK Gimap ready"

# ดู logs ยืนยันว่า account live
docker logs billflow-backend --tail=20 2>&1 | grep coordinator
# ต้องเห็น "coordinator_poller_started" สำหรับ account ใหม่
```

---

### ข้อควรระวัง Gmail IMAP

| ปัญหา | สาเหตุ | วิธีแก้ |
|---|---|---|
| `unexpected EOF` | poll ถี่เกินไป / Gmail rate limit | เพิ่ม Poll interval ใน `/settings/email` (≥ 5 นาที) |
| `auth_failed` ใน status | password ผิด / 2FA ไม่ได้เปิด | สร้าง App Password ใหม่ + แก้ใน `/settings/email` |
| `auth_failed` | ใส่ password จริงแทน App Password | ต้องใช้ App Password เท่านั้น (ดูปุ่ม "วิธีรับ App Password" ใน dialog) |
| อ่านเมลซ้ำ | เมล mark as read ไม่สำเร็จ | ตรวจ IMAP permission ใน Google account |
| ไม่เจอเมล | filter from / subject ไม่ตรง | กด "Poll ทันที" หลัง clear filter ใน dialog ของ inbox |
| Poll fail ≥ 3 รอบติด | account error | ระบบ push LINE แจ้ง admin อัตโนมัติ (throttle 1 ครั้ง/ชม.) |

---

### ใช้ email อื่นแทน Gmail

| Provider | IMAP Host | Port | หมายเหตุ |
|---|---|---|---|
| Gmail | `imap.gmail.com` | 993 | ต้องใช้ App Password |
| Outlook/Hotmail | `imap-mail.outlook.com` | 993 | ใช้ password ปกติ หรือ App Password |
| Yahoo Mail | `imap.mail.yahoo.com` | 993 | ต้องสร้าง App Password |
| Zoho Mail | `imap.zoho.com` | 993 | ใช้ password ปกติ |
| บริษัทมี mail server | `mail.company.com` | 993 | ถาม IT admin |

---

## 23. เอกสารเพิ่มเติม

| ไฟล์ | เนื้อหา |
|---|---|
| [docs/current-state.md](docs/current-state.md) | snapshot จาก code + server + production DB |
| [docs/overview.md](docs/overview.md) | ภาพรวมการทำงานทั้งระบบ (flow diagram + component map) |
| [docs/line-oa.md](docs/line-oa.md) | LINE OA human inbox, multi-OA, Reply/Push, media |
| [docs/email.md](docs/email.md) | Email IMAP workflow: poll, dedup, OCR, extract, SML |
| [docs/shopee-import.md](docs/shopee-import.md) | Shopee Excel import → local bills → SML 248 Retry route |
| [docs/sml-api-migration.md](docs/sml-api-migration.md) | BillFlow main → sml-api-bybos migration notes |
| [docs/sml-image-db-maintenance.md](docs/sml-image-db-maintenance.md) | SML product image DB index/runbook for tenant moves |
| [docs/sml-bulk-send-jobs.md](docs/sml-bulk-send-jobs.md) | Async SML bulk send job design, API, DB tables, QA, and latest smoke result |

---

## Project Structure

```
billflow/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── config/
│   │   ├── database/migrations/001_init.sql
│   │   ├── handlers/         (line, email, import, shopee_import, bills, mappings, dashboard, auth, log_handler)
│   │   ├── middleware/        (auth JWT, logger)
│   │   ├── models/            (bill, mapping, user, audit_log)
│   │   ├── repository/        (bill_repo [+DB()], mapping, user, audit_log_repo, insight, platform_mapping)
│   │   ├── services/
│   │   │   ├── ai/            (openrouter client + prompts)
│   │   │   ├── mapper/        (F1 fuzzy matching + learning)
│   │   │   ├── anomaly/       (F2 detection rules)
│   │   │   ├── sml/           (client.go JSON-RPC #1, saleinvoice_client.go REST #2)
│   │   │   ├── line/          (reply + push notify)
│   │   │   ├── email/         (IMAP polling + dedup by Message-ID)
│   │   │   └── insight/       (F4 daily AI summary)
│   │   ├── worker/pool.go     (semaphore rate limiting)
│   │   └── jobs/              (cron + background jobs)
│   ├── go.mod
│   └── Dockerfile
├── frontend/
│   └── src/
│       ├── pages/             (Login, Dashboard, Bills, BillDetail, Import, ShopeeImport, Logs, Mappings, Settings)
│       ├── components/
│       ├── hooks/
│       ├── api/client.ts
│       └── types/index.ts
├── scripts/test.sh
├── docker-compose.yml
├── docker-compose.dev.yml
├── .env.example
└── CLAUDE.md
```

---

## Deploy to Server

```bash
# Sync code
rsync -av --exclude 'node_modules' --exclude 'dist' --exclude '.git' \
  /Users/nontawatwongnuk/dev_bos/billflow/ \
  bosscatdog@192.168.2.109:~/billflow/

# Also sync go.sum (do not exclude)
rsync -av backend/go.sum bosscatdog@192.168.2.109:~/billflow/backend/go.sum

# Build + restart
ssh bosscatdog@192.168.2.109 \
  "cd ~/billflow && docker compose build backend frontend && docker compose up -d"

# Verify
curl http://192.168.2.109:8090/health
bash scripts/test.sh all 192.168.2.109:8090
```

---

*BillFlow v0.2.0 — Last updated: 2026-05-21 | Server: 192.168.2.109 | Ports: backend:8090 / frontend:3010 / postgres:5438*
