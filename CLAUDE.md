# CLAUDE.md — BillFlow
## Blueprint สำหรับ Claude Code ใน VSCode

> อ่านไฟล์นี้ให้ครบก่อนเริ่ม code ทุกครั้ง
> ห้าม assume สิ่งที่ไม่ได้ระบุ — ถามก่อนทำเสมอ
> **local workspace:** `/Users/nontawatwongnuk/dev_bos/billflow`
> **server deploy folder:** `/home/bosscatdog/billflow` (`~/billflow` on server; deployed copy, not a git checkout)

---

## 1. Project Overview

**BillFlow** — ระบบช่วยพนักงานลดเวลาคีย์บิลจาก **วันละ 100+ บิล** ลงเหลือ **เกือบ 0**
โดยใช้ AI extract ข้อมูลจากหลาย channel แล้วส่งเข้า ERP (SML) โดยอัตโนมัติ

### Input Channels
| Channel | รายละเอียด | ประเภทบิล | Phase | สถานะ |
|---------|-----------|----------|-------|-------|
| LINE OA (human chat) | text/image/file/audio → admin inbox `/messages` → reply ผ่าน Reply API/Push API | บิลขาย (sale) | Phase 3 + session 13+ | ✅ chat 2 ทาง + เปิดบิลขายจาก chat |
| Email (IMAP) | multi-account body/attachment PDF/Excel/รูป | sale/purchase ตาม routing | Phase 5 + session 6+ | ✅ deployed |
| Shopee Excel | Export จาก Shopee Seller Center → local bills → Retry default saleorder | บิลขาย (sale) | Phase 4a | ✅ deployed |
| Lazada Excel | Export จาก Lazada | บิลขาย + บิลซื้อ | Phase 4b | รอไฟล์จริงจากลูกค้า |

> ⚠️ ใช้ IMAP แทน Gmail API สำหรับ demo — ง่ายกว่า ไม่ต้องผ่าน Google OAuth2 consent
> Gmail API อาจเพิ่มใน Phase ถัดไปหลัง demo ลูกค้าแล้ว

### Output
- สร้างบิลใน SML ERP ผ่าน JSON-RPC API
- บันทึก log ทุก transaction ลง PostgreSQL
- แจ้ง admin ผ่าน LINE ทุกครั้งที่เกิด error

---

## 2. Tech Stack

```
Backend:   Go 1.24  (framework: Gin)
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
AI:        OpenRouter API (google/gemini-2.5-flash primary; server fallback anthropic/claude-3-5-haiku)
LINE:      line-bot-sdk-go (official)
Email:     IMAP polling — ไม่ใช้ Gmail API สำหรับ demo
Deploy:    Docker Compose + Cloudflare Tunnel
```

---

## 3. Server Info (Ubuntu 24.04 — 192.168.2.109)

```
OS:      Ubuntu 24.04.4 LTS (kernel 6.17.0)
CPU:     4 cores
RAM:     7.6GB total / ~4GB available
Disk:    109GB (เคลียร์แล้ว < 70%)
Docker:  29.3.0
Node:    v22.22.1 ✅
Go:      1.24.0 ✅
cloudflared: ✅ installed & configured
SML #1 (LINE/Email): http://192.168.2.213:3248  ✅ JSON-RPC, /api/sale_reserve
SML #2 (Shopee):     http://192.168.2.248:8080  ✅ REST API — saleorder/saleinvoice/purchaseorder/product
```

### Projects ที่รันอยู่บน Server — ห้ามกระทบ

| Project | Ports |
|---------|-------|
| openclaw-admin | 3000, 5432 |
| tcc | 8080, 5433, 9092, 8123, 9000, 6382 |
| ledgioai | 3004, 5436, 6381 |
| centrix | 3002, 5001, 5434, 6380 |

### Ports ที่ BillFlow ใช้ (ไม่ชนกับใคร)

```
billflow-backend   → 8090
billflow-frontend  → 3010
billflow-postgres  → 5438
```

---

## 4. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Cloudflare Tunnel                        │
│   api.your-domain.com → :8090  (backend)                   │
│   app.your-domain.com → :3010  (frontend)                  │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                  Go Backend (Gin)  :8090                    │
│                                                             │
│  Webhook Routes:                                            │
│    POST /webhook/line              ← LINE OA events         │
│                                                             │
│  API Routes (JWT protected unless noted):                   │
│    POST /api/auth/login            ← login (no auth)        │
│    GET  /api/auth/me               ← current user           │
│                                                             │
│    Bills (manual-confirm flow):                             │
│    GET  /api/bills                 ← cursor list (status,   │
│                                       source, bill_type,    │
│                                       date, archived)       │
│    GET  /api/bills/counts          ← queue counts for lists │
│    GET  /api/bills/:id             ← detail + items         │
│    POST /api/bills/:id/retry       ← 4-way SML send         │
│                                       (sale_reserve /       │
│                                        saleorder /          │
│                                        saleinvoice /        │
│                                        purchaseorder)       │
│    POST /api/bills/bulk-send-jobs  ← async bulk SML job    │
│    GET  /api/bills/bulk-send-jobs/active ← resume job      │
│    GET  /api/bills/bulk-send-jobs/:id ← progress/results   │
│    POST /api/bills/bulk-send-jobs/:id/retry-failed         │
│    PUT  /api/bills/:id/items/:iid  ← edit qty/price/code    │
│                                       (also fires F1 hook)  │
│    POST /api/bills/:id/items       ← add row                │
│    DEL  /api/bills/:id/items/:iid  ← delete row             │
│    POST /api/bills/:id/archive     ← archive one bill       │
│    POST /api/bills/:id/restore     ← restore one bill       │
│    DEL  /api/bills/:id             ← delete one bill        │
│                                                             │
│    Mappings + F1:                                           │
│    GET  /api/mappings              ← list (manual + ai_*)   │
│    POST /api/mappings              ← add                    │
│    PUT  /api/mappings/:id          ← edit (3-field)         │
│    DEL  /api/mappings/:id          ← delete                 │
│    POST /api/mappings/feedback     ← legacy F1 endpoint     │
│    GET  /api/mappings/stats        ← F1 accuracy stats      │
│                                                             │
│    Catalog (SML 248 product catalog + embeddings):          │
│    GET  /api/catalog               ← paginated list         │
│    GET  /api/catalog/stats         ← embed progress         │
│    GET  /api/catalog/search?q=     ← embedding similarity   │
│    GET  /api/catalog/:code         ← item detail            │
│    POST /api/catalog/products      ← create new SML product │
│                                       + sync + embed        │
│    POST /api/catalog/sync          ← bulk sync from SML 248 │
│    POST /api/catalog/import-csv    ← bulk CSV upload        │
│    POST /api/catalog/embed-all     ← background batch embed │
│    POST /api/catalog/reload-index  ← rebuild memory index   │
│    POST /api/catalog/:code/embed   ← embed single           │
│    POST /api/bills/:id/items/:iid/confirm-match  ← legacy   │
│                                                             │
│    Imports:                                                 │
│    POST /api/import/upload         ← generic Lazada (WIP)   │
│    POST /api/import/confirm        ← generic Lazada confirm │
│    GET  /api/settings/shopee-config← Shopee SML defaults    │
│    POST /api/import/shopee/preview ← parse Excel + dedup    │
│    POST /api/import/shopee/confirm ← send to SML 248        │
│                                                             │
│    Dashboard / Logs / Settings:                             │
│    GET  /api/dashboard/stats                                │
│    GET  /api/dashboard/insights                             │
│    POST /api/dashboard/insights/generate (admin)            │
│    GET  /api/logs                  ← cursor logs, no total  │
│                                       unless include_total  │
│    GET  /api/settings/status                                │
│    GET  /api/settings/column-mappings/:platform             │
│    PUT  /api/settings/column-mappings/:platform (admin)     │
│                                                             │
│    Email Inboxes (admin only):                              │
│    GET  /api/settings/imap-accounts        ← list          │
│    POST /api/settings/imap-accounts        ← create        │
│    GET  /api/settings/imap-accounts/:id    ← detail        │
│    PUT  /api/settings/imap-accounts/:id    ← update        │
│    DEL  /api/settings/imap-accounts/:id    ← delete        │
│    POST /api/settings/imap-accounts/:id/poll ← manual poll │
│    POST /api/settings/imap-accounts/test   ← dry connect   │
│    POST /api/settings/imap-accounts/list-folders ← enum   │
│                                                             │
│    Channel Defaults (admin only):                           │
│    GET  /api/settings/channel-defaults     ← list all rows │
│    PUT  /api/settings/channel-defaults     ← upsert by     │
│                                               (channel,     │
│                                                bill_type)   │
│    DEL  /api/settings/channel-defaults/    ← delete row    │
│         :channel/:bill_type                                 │
│    POST /api/settings/channel-defaults/    ← auto-pair     │
│         quick-setup                           AR00001-04    │
│                                                             │
│    SML Party Master (admin/staff):                          │
│    GET  /api/sml/customers?search=&limit=  ← searchable,   │
│                                               backed by     │
│                                               PartyCache    │
│    GET  /api/sml/suppliers?search=&limit=  ← same          │
│    POST /api/sml/refresh-parties           ← re-fetch SML  │
│    GET  /api/sml/parties/last-sync         ← sync time     │
│                                                             │
│    Webhook:                                                 │
│    POST /webhook/line              ← LINE OA events         │
│                                                             │
│  Background Jobs:                                           │
│    EmailCoordinator → one goroutine per imap_accounts row  │
│                    each polls its mailbox every             │
│                    poll_interval_seconds (≥ 300 s)         │
│                    per-account channel routing:             │
│                    "ถูกจัดส่งแล้ว" / "ยืนยันการชำระเงิน"  │
│                       → ShopeeShipped (purchaseorder 248)  │
│                    other Shopee → saleorder (248) default  │
│                    general     → attachment AI pipeline     │
│                    LINE admin notify on ≥ 3 consecutive    │
│                    failures (throttled 1/hour per inbox)   │
│    Cron 08:00    → F4 daily insight + LINE notify          │
│    Cron 00:00    → pg_dump backup (in-container, gzip)     │
│    Cron Mon 09   → LINE token expiry reminder              │
│    Cron daily 07 → disk usage monitor (root fs > 90%)      │
│                                                             │
│  Services:                                                  │
│    AIService         → OpenRouter (text/image/PDF/audio)   │
│    MapperService     → F1 fuzzy match + auto-learn loop    │
│    AnomalyService    → F2 anomaly (incl. new_customer)     │
│    SML Client        → JSON-RPC sale_reserve (213)         │
│    SML Invoice       → REST saleinvoice (248)              │
│    SML PurchaseOrd   → REST purchaseorder (248)            │
│    SML Product       → REST product create/lookup (248)    │
│    LineService       → reply / flex / push notify          │
│    EmailCoordinator  → multi-account IMAP (one goroutine   │
│                        per imap_accounts row)              │
│    InsightService    → F4 daily AI summary                 │
│    Catalog           → embed (1536-dim) + cosine index     │
│    PartyCache        → in-memory SML customer/supplier     │
│                        cache (boot + 6h refresh)           │
│    WorkerPool        → semaphore rate limiting             │
└──────┬──────────────────────────────┬───────────────────────┘
       │                              │
┌──────▼──────┐              ┌────────▼────────┐
│ PostgreSQL  │              │  External APIs  │
│   :5438     │              │                 │
│             │              │ OpenRouter API  │
│ tables:     │              │ SML :3248       │
│  bills      │              │ LINE API        │
│  bill_items │              │ IMAP server     │
│  mappings   │              └─────────────────┘
│  mapping_   │
│  feedback   │
│  users      │
│  audit_logs │
│  daily_     │
│  insights   │
│  item_price_│
│  history    │
└─────────────┘
       ▲
┌──────┴──────────────────────────────────────────────────────┐
│             React + Vite Frontend  :3010                    │
│                                                             │
│  /login            ← หน้า login                            │
│  /dashboard        ← stats + charts + F4 AI Insights       │
│  /bills            ← รายการบิล + filter + anomaly badge    │
│  /bills/:id        ← รายละเอียด + status + retry           │
│  /messages         ← LINE OA inbox (human chat) ✨session 13│
│  /import           ← upload Lazada/Shopee                  │
│  /mappings         ← จัดการ mapping + F1 learning stats    │
│  /settings         ← LINE, SML, threshold, columns         │
│  /settings/email   ← Email Inboxes (admin only)            │
│  /settings/channels← Channel Defaults (admin only)         │
│  /settings/line-oa ← LINE OA accounts (multi-OA, session 13)│
│  /settings/quick-replies ← chat reply templates (session 13)│
└─────────────────────────────────────────────────────────────┘
```

---

## 5. Database Schema (PostgreSQL)

```sql
-- Users & Auth
CREATE TABLE users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email         TEXT UNIQUE NOT NULL,
  name          TEXT NOT NULL,
  role          TEXT NOT NULL CHECK (role IN ('admin','staff','viewer')),
  password_hash TEXT NOT NULL,
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Item Mapping
CREATE TABLE mappings (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  raw_name      TEXT NOT NULL,
  item_code     TEXT NOT NULL,
  unit_code     TEXT NOT NULL,
  confidence    FLOAT DEFAULT 1.0,
  source        TEXT DEFAULT 'manual' CHECK (source IN ('manual','ai_learned')),
  usage_count   INT DEFAULT 0,
  last_used_at  TIMESTAMPTZ,
  learned_from_bill_id UUID,
  created_by    UUID REFERENCES users(id),
  created_at    TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(raw_name)
);

-- F1: Mapping Feedback
CREATE TABLE mapping_feedback (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_item_id   UUID REFERENCES bill_items(id),
  original_match TEXT,
  corrected_to   TEXT,
  corrected_by   UUID REFERENCES users(id),
  created_at     TIMESTAMPTZ DEFAULT NOW()
);

-- Bills
CREATE TABLE bills (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_type     TEXT NOT NULL CHECK (bill_type IN ('sale','purchase')),
  source        TEXT NOT NULL CHECK (source IN ('line','email','lazada','shopee','manual')),
  status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','confirmed','sent','failed','skipped')),
  raw_data      JSONB,
  sml_doc_no    TEXT,
  sml_payload   JSONB,
  sml_response  JSONB,
  ai_confidence FLOAT,
  anomalies     JSONB DEFAULT '[]',
  error_msg     TEXT,
  created_by    UUID REFERENCES users(id),
  created_at    TIMESTAMPTZ DEFAULT NOW(),
  sent_at       TIMESTAMPTZ
);

-- Bill Items
CREATE TABLE bill_items (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_id    UUID REFERENCES bills(id) ON DELETE CASCADE,
  raw_name   TEXT NOT NULL,
  item_code  TEXT,
  qty        NUMERIC NOT NULL,
  unit_code  TEXT,
  price      NUMERIC,
  mapped     BOOLEAN DEFAULT FALSE,
  mapping_id UUID REFERENCES mappings(id)
);

-- Audit Log
CREATE TABLE audit_logs (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID REFERENCES users(id),
  action     TEXT NOT NULL,
  target_id  UUID,
  detail     JSONB,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- F2: Item Price History
CREATE TABLE item_price_history (
  item_code    TEXT PRIMARY KEY,
  avg_price    NUMERIC,
  min_price    NUMERIC,
  max_price    NUMERIC,
  sample_count INT DEFAULT 0,
  last_updated TIMESTAMPTZ DEFAULT NOW()
);

-- F4: Daily AI Insights
CREATE TABLE daily_insights (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  date       DATE UNIQUE NOT NULL,
  stats_json JSONB,
  insight    TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Platform Column Mapping (Lazada/Shopee — admin config)
CREATE TABLE platform_column_mappings (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  platform     TEXT NOT NULL CHECK (platform IN ('lazada','shopee')),
  field_name   TEXT NOT NULL,   -- 'order_id', 'buyer_name', 'item_name', etc.
  column_name  TEXT NOT NULL,   -- ชื่อ column จริงในไฟล์ Excel
  updated_by   UUID REFERENCES users(id),
  updated_at   TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(platform, field_name)
);

-- chat_sessions (legacy AI chatbot state) — DROPPED in migration 013 along
-- with the AI chatbot refactor. Replaced by chat_conversations + chat_messages
-- + chat_media (human chat inbox) below.

-- LINE OA accounts (multi-OA support, session 13)
-- One BillFlow can serve multiple LINE OAs (e.g., a chain with 5 stores).
-- Webhook URL per OA: /webhook/line/<id>. Each OA has its own credentials.
CREATE TABLE line_oa_accounts (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name                  TEXT NOT NULL,                  -- admin label e.g. "ร้านสาขา A"
  channel_secret        TEXT NOT NULL,                  -- webhook signature secret
  channel_access_token  TEXT NOT NULL,                  -- Push API token (long-lived)
  bot_user_id           TEXT NOT NULL DEFAULT '',       -- auto-fetched from /v2/bot/info on save
  admin_user_id         TEXT NOT NULL DEFAULT '',       -- LINE userID for error notifications (optional)
  greeting              TEXT NOT NULL DEFAULT '',       -- one-time auto-reply on first contact (optional)
  enabled               BOOLEAN NOT NULL DEFAULT TRUE,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Chat conversations — one row per (LINE userID, LINE OA). Same person on
-- different OAs has different LINE userIDs anyway, so PK on line_user_id alone
-- works. line_oa_id tells us which OA's token to use for outbound replies.
CREATE TABLE chat_conversations (
  line_user_id          TEXT PRIMARY KEY,
  line_oa_id            UUID REFERENCES line_oa_accounts(id),  -- which OA owns this convo
  display_name          TEXT NOT NULL DEFAULT '',
  picture_url           TEXT NOT NULL DEFAULT '',
  last_message_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_inbound_at       TIMESTAMPTZ,
  last_admin_reply_at   TIMESTAMPTZ,
  unread_admin_count    INT NOT NULL DEFAULT 0,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX chat_conversations_last_message_idx ON chat_conversations(last_message_at DESC);
CREATE INDEX chat_conversations_unread_idx ON chat_conversations(unread_admin_count) WHERE unread_admin_count > 0;

-- Chat messages — every event in a conversation (incoming/outgoing/system).
-- For media (kind=image/file/audio) the binary lives in chat_media.
CREATE TABLE chat_messages (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  line_user_id      TEXT NOT NULL REFERENCES chat_conversations(line_user_id) ON DELETE CASCADE,
  direction         TEXT NOT NULL CHECK (direction IN ('incoming','outgoing','system')),
  kind              TEXT NOT NULL CHECK (kind IN ('text','image','file','audio','system')),
  text_content      TEXT NOT NULL DEFAULT '',
  line_message_id   TEXT NOT NULL DEFAULT '',  -- LINE inbound message ID for media download
  line_event_ts     BIGINT,                    -- LINE event timestamp ms (tie-break under retry)
  sender_admin_id   UUID REFERENCES users(id),
  delivery_status   TEXT NOT NULL DEFAULT 'sent'
                    CHECK (delivery_status IN ('sent','failed','pending')),
  delivery_error    TEXT NOT NULL DEFAULT '',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX chat_messages_thread_idx ON chat_messages(line_user_id, created_at DESC);

-- Chat media (parallel to bill_artifacts; not retrofitted because that table
-- has bill_id NOT NULL and chat media has no bill).
CREATE TABLE chat_media (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id        UUID NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
  filename          TEXT NOT NULL,
  content_type      TEXT NOT NULL,
  size_bytes        BIGINT NOT NULL,
  sha256            TEXT NOT NULL,                      -- {root}/chat-media/YYYY/MM/<sha256>.<ext>
  storage_path      TEXT NOT NULL,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Quick reply templates for the chat composer (Phase 4.4)
-- Admin-curated canned responses ("สวัสดีค่ะ", "เช็คสต๊อกให้สักครู่", etc.).
-- Composer in /messages opens a popover to inject these into the textarea.
CREATE TABLE chat_quick_replies (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  label       TEXT NOT NULL,                            -- short display name in picker
  body        TEXT NOT NULL,                            -- full text injected into composer
  sort_order  INT NOT NULL DEFAULT 0,
  created_by  UUID REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- 4 seed rows on first boot: ทักทาย / เช็คสต๊อก / แจ้งราคา / ปิดบิล

-- Conversation lifecycle status (Phase 4.2 / migration 016)
-- ALTER TABLE chat_conversations ADD COLUMN status TEXT NOT NULL DEFAULT 'open'
--   CHECK (status IN ('open','resolved','archived'));
-- - open      → active, default inbox tab
-- - resolved  → admin marked done; auto-revive on inbound (handlers/line.go)
-- - archived  → sticky (no auto-revive); for spam/blocked threads

-- CRM lite — phone + notes + tags (Phase 4.7+4.8+4.9 / migration 017)
-- ALTER TABLE chat_conversations ADD COLUMN phone TEXT NOT NULL DEFAULT '';
-- (saved by "บันทึกเบอร์" button when regex matches incoming text)

-- Internal admin annotations on a conversation (never sent to LINE)
CREATE TABLE chat_notes (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  line_user_id TEXT NOT NULL REFERENCES chat_conversations(line_user_id) ON DELETE CASCADE,
  body         TEXT NOT NULL,
  created_by   UUID REFERENCES users(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Global tag list (admin-curated) + many-to-many with conversations.
-- Used for inbox filtering (VIP / ขายส่ง / spam ฯลฯ).
CREATE TABLE chat_tags (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  label      TEXT NOT NULL UNIQUE,
  color      TEXT NOT NULL DEFAULT 'gray',  -- gray/red/orange/yellow/green/blue/purple
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE chat_conversation_tags (
  line_user_id TEXT NOT NULL REFERENCES chat_conversations(line_user_id) ON DELETE CASCADE,
  tag_id       UUID NOT NULL REFERENCES chat_tags(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (line_user_id, tag_id)
);

-- SML Catalog (smart matching — Shopee email + manual review)
-- ทำงานเป็น in-memory cosine-similarity index (1536-dim vectors)
CREATE TABLE sml_catalog (
  item_code        TEXT PRIMARY KEY,
  item_name        TEXT NOT NULL,
  item_name2       TEXT NOT NULL DEFAULT '',
  unit_code        TEXT NOT NULL DEFAULT '',
  wh_code          TEXT NOT NULL DEFAULT '',
  shelf_code       TEXT NOT NULL DEFAULT '',
  price            NUMERIC(14,4),
  group_code       TEXT NOT NULL DEFAULT '',
  balance_qty      NUMERIC(14,4),
  embedding_status TEXT NOT NULL DEFAULT 'pending'
                   CHECK (embedding_status IN ('pending','done','error')),
  embedded_at      TIMESTAMPTZ,
  embedding        JSONB,           -- text-embedding-3-small (OpenRouter)
  embedding_model  TEXT,
  synced_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Channel Defaults — per (channel, bill_type) SML config
-- Replaces .env SHOPEE_SML_CUST_CODE / SHIPPED_SML_CUST_CODE (removed session 7)
-- Admin manages via /settings/channels UI. All 4 SML routes read from here.
-- Final shape after migrations 007-011.
CREATE TABLE channel_defaults (
  channel              TEXT NOT NULL
                       CHECK (channel IN ('line','email','shopee','lazada','shopee_shipped')),
  bill_type            TEXT NOT NULL
                       CHECK (bill_type IN ('sale','purchase')),
  party_code           TEXT NOT NULL,   -- AR-prefixed for customers, V-prefixed for suppliers
  party_name           TEXT NOT NULL,
  party_phone          TEXT NOT NULL DEFAULT '',
  party_address        TEXT NOT NULL DEFAULT '',
  party_tax_id         TEXT NOT NULL DEFAULT '',
  doc_format_code      TEXT NOT NULL DEFAULT '',  -- e.g. "SR", "INV", "PO"
  endpoint             TEXT NOT NULL DEFAULT '',  -- free-form URL/path, keyword-detected by bills.go
  doc_prefix           TEXT NOT NULL DEFAULT '',  -- e.g. "BF-SO"
  doc_running_format   TEXT NOT NULL DEFAULT '',  -- e.g. "YYMM####"
  -- Inventory + VAT overrides (sentinel '' / -1 = "use server .env default").
  -- bills.go applyChannelOverrides() overlays these on cfg.ShopeeSML* per channel.
  wh_code              TEXT NOT NULL DEFAULT '',
  shelf_code           TEXT NOT NULL DEFAULT '',
  vat_type             INT  NOT NULL DEFAULT -1,   -- -1=use env, 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
  vat_rate             NUMERIC(6,3) NOT NULL DEFAULT -1,
  updated_by           UUID REFERENCES users(id),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (channel, bill_type)
);
-- old channel_customer_defaults (migration 003) renamed to channel_customer_defaults_v1 by 007

-- Doc Counters — atomic per-prefix per-period sequential doc_no generator
-- Avoids SML UI bug: "prefix-YYYY..." pattern silently dropped.
-- Period resets by tokens: DD=daily, MM=monthly, YY=yearly.
CREATE TABLE doc_counters (
  prefix       TEXT NOT NULL,
  period       TEXT NOT NULL,    -- "2604" for YYMM, "260428" for YYMMDD, etc.
  last_used_seq INT NOT NULL DEFAULT 0,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (prefix, period)
);

-- IMAP Accounts — multi-account email config (replaces .env IMAP_* singleton)
-- Admin manages via /settings/email UI. One goroutine per enabled row.
CREATE TABLE imap_accounts (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name                  TEXT NOT NULL,
  host                  TEXT NOT NULL,
  port                  INT NOT NULL DEFAULT 993,
  username              TEXT NOT NULL,
  password              TEXT NOT NULL,                  -- plaintext, LAN-only
  mailbox               TEXT NOT NULL DEFAULT 'INBOX',
  filter_from           TEXT NOT NULL DEFAULT '',
  filter_subjects       TEXT[] NOT NULL DEFAULT '{}',
  channel               TEXT NOT NULL DEFAULT 'general'
                        CHECK (channel IN ('general','shopee','lazada')),
  shopee_domains        TEXT[] NOT NULL DEFAULT '{}',
  lookback_days         INT NOT NULL DEFAULT 30
                        CHECK (lookback_days BETWEEN 1 AND 90),
  poll_interval_seconds INT NOT NULL DEFAULT 300
                        CHECK (poll_interval_seconds >= 300),
  enabled               BOOLEAN NOT NULL DEFAULT TRUE,
  -- runtime status (updated by coordinator after each poll)
  last_polled_at        TIMESTAMPTZ,
  last_poll_status      TEXT CHECK (last_poll_status IN ('ok','error')),
  last_poll_error       TEXT,
  last_poll_messages    INT,
  consecutive_failures  INT NOT NULL DEFAULT 0,
  last_admin_alert_at   TIMESTAMPTZ,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

> **Migration files** (run in order, all idempotent):
> - [001_init.sql](backend/internal/database/migrations/001_init.sql) — initial schema (users, bills, bill_items, mappings, mapping_feedback, item_price_history, daily_insights, platform_column_mappings, audit_logs, chat_sessions)
> - [002_audit_logging.sql](backend/internal/database/migrations/002_audit_logging.sql) — audit_logs structured columns (source, level, duration_ms, trace_id) + indexes
> - [002_sml_catalog.sql](backend/internal/database/migrations/002_sml_catalog.sql) — sml_catalog table + bills.sml_order_id + bill_items.candidates + extended source/status CHECK
> - [003_channel_customer_defaults.sql](backend/internal/database/migrations/003_channel_customer_defaults.sql) — channel_customer_defaults table (legacy, renamed to _v1 by 007)
> - [004_shopee_shipped.sql](backend/internal/database/migrations/004_shopee_shipped.sql) — extends bills.source CHECK to include shopee_shipped
> - [006_imap_accounts.sql](backend/internal/database/migrations/006_imap_accounts.sql) — imap_accounts table (multi-account IMAP, replaces .env singleton)
> - [007_channel_defaults.sql](backend/internal/database/migrations/007_channel_defaults.sql) — channel_defaults table (session 7); renames channel_customer_defaults → _v1
> - [008_channel_defaults_doc_format.sql](backend/internal/database/migrations/008_channel_defaults_doc_format.sql) — adds doc_format_code column
> - [009_channel_defaults_endpoint.sql](backend/internal/database/migrations/009_channel_defaults_endpoint.sql) — adds endpoint column (initially CHECK-constrained)
> - [010_channel_defaults_endpoint_freeform.sql](backend/internal/database/migrations/010_channel_defaults_endpoint_freeform.sql) — drops CHECK so admins can type any URL/path
> - [011_doc_no_format.sql](backend/internal/database/migrations/011_doc_no_format.sql) — adds doc_prefix + doc_running_format columns + doc_counters table
> - [012_channel_defaults_inventory.sql](backend/internal/database/migrations/012_channel_defaults_inventory.sql) — adds wh_code + shelf_code + vat_type + vat_rate per channel (sentinel '' / -1 falls back to server .env)
> - [013_chat_inbox.sql](backend/internal/database/migrations/013_chat_inbox.sql) — drops chat_sessions, creates chat_conversations + chat_messages + chat_media (session 13 chatbot → human chat refactor)
> - [014_line_oa_accounts.sql](backend/internal/database/migrations/014_line_oa_accounts.sql) — line_oa_accounts table + chat_conversations.line_oa_id (multi-OA support, session 13)
> - [015_chat_quick_replies.sql](backend/internal/database/migrations/015_chat_quick_replies.sql) — chat_quick_replies table + 4 seed templates (Phase 4.4)
> - [016_chat_conversation_status.sql](backend/internal/database/migrations/016_chat_conversation_status.sql) — chat_conversations.status (open/resolved/archived) + auto-revive (Phase 4.2 — session 14)
> - [017_chat_crm.sql](backend/internal/database/migrations/017_chat_crm.sql) — chat_conversations.phone + chat_notes + chat_tags + chat_conversation_tags (CRM lite Phase 4.7+4.8+4.9 — session 14)
> - [018_chat_reply_token.sql](backend/internal/database/migrations/018_chat_reply_token.sql) — chat_conversations.last_reply_token + last_reply_token_at + chat_messages.delivery_method (Hybrid Reply+Push API — session 15)
> - [019_line_oa_mark_as_read.sql](backend/internal/database/migrations/019_line_oa_mark_as_read.sql) — line_oa_accounts.mark_as_read_enabled per-OA opt-in toggle for LINE Premium "อ่านแล้ว" read receipts (session 17)
> - [037_data_lifecycle.sql](backend/internal/database/migrations/037_data_lifecycle.sql) — production data lifecycle: summary tables, log/bill indexes, cursor-friendly access paths
> - [044_sml_bulk_jobs.sql](backend/internal/database/migrations/044_sml_bulk_jobs.sql) — DB-backed async SML bulk send jobs and per-bill item progress/results

> **Production data lifecycle**
> - `/api/logs` uses cursor pagination (`limit`, `cursor`, `has_more`, `next_cursor`) and does not run `COUNT(*)` unless `include_total=true`.
> - `/api/bills` supports cursor pagination plus legacy `page/per_page`, filters `archived`, `date_from`, `date_to`, and defaults to active rows only.
> - `/api/bills/counts` returns queue counts for bills/sales-orders/sale-invoices in one request.
> - `/api/bills/bulk-send-jobs` creates async SML jobs capped at 100 bills; worker sends serially, stores progress, supports resume after dialog close, and retries failed rows only.
> - SML audit rows store compact support fields instead of full `sml_payload` / `sml_response`.
> - The daily lifecycle job auto-archives `sent/skipped` older than 180 days and rollups/purges detailed audit + AI logs older than 90 days in batch-safe chunks.
> - `/settings/old-data` shows row count, table size, oldest row, policy, dry-run summary, and never selects purge by default.

---

## 6. Use Cases (ละเอียด)

### UC1 — LINE OA human chat inbox (session 13 refactor)

> ✅ DEPLOYED — chat 2 ทาง + เปิดบิลขายจาก chat ทดสอบผ่าน (2026-04-29)
> AI chatbot "น้องบิล" ถูกลบออก — admin คุยเองผ่าน BillFlow `/messages`

```
Flow:
1. ลูกค้าทักผ่าน LINE OA ใด ๆ ที่ admin เพิ่มไว้ใน /settings/line-oa
2. LINE webhook POST /webhook/line/:oaId
3. verify X-Line-Signature ด้วย channel_secret ของ OA นั้น
4. respond HTTP 200 ทันที → async worker store message
5. Insert/upsert chat_conversations (line_user_id PK + line_oa_id) + chat_messages
6. ดึง LINE profile (display_name + picture) บน first contact
7. ถ้ามี media (image/file/audio) → download bytes → save chat_media
8. (optional) auto-greeting จาก env LINE_GREETING ใน OA นั้น
9. ที่ฝั่ง admin: /messages page แสดง inbox ครบทุก OA รวมกัน
   - เลือกห้อง → reply ผ่าน Composer → POST /api/admin/conversations/:userId/messages
   - backend ใช้ LineRegistry หา service ของ OA ที่ห้องนั้นอยู่ → Push API
10. media bubble มีปุ่ม "🔍 สร้างบิลจากสื่อนี้" → manual AI extract → preview
11. header มีปุ่ม "เปิดบิลขาย" → catalog picker → POST .../bills
    → bill source="line" status=pending raw_data.line_user_id+line_oa_id
12. /bills/:id Retry → SML 213 sale_reserve (flow เดิม, channel_defaults party)

Multi-LINE OA (session 13):
- 1 BillFlow รองรับหลาย LINE OA ได้ (เช่น 5 ร้านในเครือเดียว)
- /settings/line-oa จัดการ secret + token + name + bot_user_id ต่อ OA
- Webhook URL ต่อ OA: https://your-domain/webhook/line/<oa_id>
  → admin ใส่ใน LINE Developer Console ของแต่ละ OA แยกกัน
- Conversation ผูกกับ line_oa_id → reply กลับใช้ access_token ของ OA นั้น
- Inbox รวมทุก OA — ConversationList แสดง badge OA ต่อ row

AI methods (ExtractImage / ExtractPDF / ExtractText / TranscribeAudio):
- ลบจาก auto-pipeline ของ LINE → แต่ยังใช้ใน Email IMAP + manual extract button
- ChatSales / ChatSalesWithContext / ExtractOrderFromHistory / SalesSystemPrompt: ลบทั้งหมด

Error / fallback:
- LINE Push fail → chat_messages.delivery_status='failed' + delivery_error → UI ⚠ + retry button
- Webhook signature mismatch → 400 (ลูกค้าไม่ได้รับผลกระทบ; admin debug ผ่าน /logs)
- Customer blocked us → Push ตอบ 403 → mark failed; admin เห็น

Phase 4 features (planned):
- Quick replies (template) + customer history panel + browser notifications
- Conversation status (open/resolved/archived) + search + tags
- Admin ส่ง media กลับ (ต้อง public URL — รอ Cloudflare Tunnel)
```

### UC2 — Email IMAP (multi-account)

```
Config: admin จัดการ inbox ได้ที่ /settings/email (EmailCoordinator อ่าน imap_accounts จาก DB)
        ไม่มี IMAP_* ใน .env อีกต่อไป — ทุกอย่างอยู่ใน DB

Flow (ต่อ account):
1. EmailCoordinator spawn goroutine ต่อ row ที่ enabled=true
2. ทุก poll_interval_seconds → SELECT unseen messages จาก mailbox
3. filter ตาม filter_from / filter_subjects ของ account นั้น
4. per-account channel routing:
   - subject matches "ถูกจัดส่งแล้ว" หรือ "ยืนยันการชำระเงิน"
       → ProcessShopeeShippedEmailBody → purchaseorder (SML 248)
   - from domain ใน shopee_domains && channel=shopee
       → ProcessShopeeEmailBody → create Shopee sale bill; Retry default saleorder (SML 248)
   - else → download attachment → AI pipeline → pending/needs_review
5. mark_read หลัง process (กัน process ซ้ำ)
6. consecutive_failures ≥ 3 → LINE admin notify (throttle 1/hour per inbox)

Production inboxes (2026-04-28):
  "Shopee Inbox" — bos.catdog@gmail.com — channel=shopee
    filter_subjects: คำสั่งซื้อ, ถูกจัดส่งแล้ว
  "Shopee Inbox (sutee — pay-now)" — sutee.toe@gmail.com — channel=shopee
    filter_subjects: ยืนยันการชำระเงินคำสั่งซื้อหมายเลข #
```

### UC3 — Import Lazada/Shopee (บิลขาย)

```
Flow:
1. พนักงาน login → /import → upload Excel
2. Backend ดึง column mapping จาก DB (ไม่ hardcode)
3. parse + แสดง preview + anomaly warning
4. พนักงาน confirm → loop สร้างบิล → SML
5. สรุปผล: สำเร็จ X / ล้มเหลว Y + download error report

⚠️ รอไฟล์จริงจากลูกค้าก่อน Phase 4
   admin config column mapping ได้จาก /settings
```

### UC4 — Import Lazada/Shopee (บิลซื้อ)

```
เหมือน UC3 แต่ bill_type = 'purchase'
SML endpoint อาจต่างกัน — รอ confirm จาก SML doc
```

---

## 7. AI Extraction — System Prompt

```go
// backend/internal/ai/prompts.go

const ExtractPrompt = `
คุณเป็น AI ที่ช่วย extract ข้อมูลจากใบสั่งซื้อ (Purchase Order)
ตอบเป็น JSON เท่านั้น ห้ามมีข้อความอื่น ห้ามมี markdown

output format:
{
  "doc_type": "sale" | "purchase",
  "customer_name": string,
  "customer_phone": string | null,
  "items": [
    {
      "raw_name": string,
      "qty": number,
      "unit": string,
      "price": number | null
    }
  ],
  "total_amount": number | null,
  "note": string | null,
  "confidence": number
}

ถ้าข้อมูลไม่ชัดเจน ให้ confidence ต่ำ (< 0.5)
ถ้าข้อมูลมาจาก voice transcription ให้ confidence ลดลง 0.1
`
```

---

## 8. F1 — AI Learning Loop

```go
func (s *MapperService) Match(rawName string) MatchResult {
  // 1. Exact match → confidence 1.0
  // 2. Fuzzy match (levenshtein)
  //    boost score ถ้า usage_count สูง
  //    score >= 0.85 → auto map
  //    score 0.60-0.84 → needs_review
  //    score < 0.60 → unmapped
}

func (s *MapperService) LearnFromFeedback(f MappingFeedback) error {
  // human confirm → INSERT หรือ UPDATE
  // source='ai_learned', confidence=1.0
  // increment usage_count + update last_used_at
}

// อัปเดต item_price_history ทุกครั้ง status='sent'
func (s *MapperService) UpdatePriceHistory(items []BillItem) error {}
```

---

## 9. F2 — Anomaly Detection

```go
var AnomalyRules = []AnomalyRule{
  // block — บังคับ manual confirm
  {"price_zero",      "block"},  // ราคา = 0
  {"qty_zero",        "block"},  // qty = 0
  {"duplicate_bill",  "block"},  // same customer+items วันเดียวกัน

  // warn — แสดง badge แต่ไม่ block อัตโนมัติ
  {"price_too_high",  "warn"},   // > avg * 1.5
  {"price_too_low",   "warn"},   // < avg * 0.5
  {"qty_suspicious",  "warn"},   // > max_ever * 2
  {"new_customer",    "warn"},   // ลูกค้าใหม่
  {"new_item",        "warn"},   // สินค้าใหม่
}

// Auto-confirm ผ่านเมื่อ:
// final_confidence >= threshold
// AND ไม่มี block
// AND warn ไม่เกิน 1 รายการ
```

---

## 10. SML API Integration

### SML #1 — JSON-RPC (LINE OA + Email)
```go
// POST http://192.168.2.213:3248/api/sale_reserve
// Headers: Content-Type: application/json
//          mcp-access-mode: sales

// Request
{
  "jsonrpc": "2.0", "id": 1, "method": "tools/call",
  "params": {
    "name": "create_sale_reserve",
    "arguments": {
      "contact_name": string,
      "contact_phone": string,
      "items": [{ "item_code": string, "qty": number,
                  "unit_code": string, "price": number }]
    }
  }
}

// ⚠️ response text เป็น JSON string ซ้อนกัน ต้อง parse 2 ชั้น
// Success: {"success":true,"doc_no":"BS20260422XXXX","message":"create success"}

// Retry: max 3 ครั้ง, backoff 1s/3s/5s
// หลัง fail 3 ครั้ง → status='failed' + LINE admin push notify
```

### SML #2 — REST API (Shopee)
```go
// Base URL: http://192.168.2.248:8080
// Auth headers (ทุก request):
//   guid, provider, configFileName, databaseName

// 1. Product lookup  ← CONFIRMED WORKING (2026-04-24)
// GET /SMLJavaRESTService/v3/api/product/{sku}
// Response (flat — ไม่มี nested):
//   {"success":true,"data":{"code":"...","unit_standard":"ถุง",
//                           "start_sale_unit":"ถุง","start_sale_wh":"WH-01",
//                           "start_sale_shelf":"SH-01"}}
//   {"success":true,"data":null}  ← ถ้าไม่พบ SKU ใน SML
// ⚠️ ต้องตั้ง SHOPEE_SML_UNIT_CODE เป็น fallback (เช่น "ถุง") เมื่อ data=null

// 1b. Create saleorder  ← CONFIRMED WORKING (2026-04-28) — DEFAULT for Shopee email flow
// POST /SMLJavaRESTService/v3/api/saleorder
// Payload: same structure as saleinvoice but field is "items" (not "details"),
//          "sale_type" field; doc_no MUST be non-empty (use doc_counter YYMM#### format).
// ⚠️ Bills.go keyword-detects endpoint: URL contains "saleorder"→SaleOrderClient,
//    "saleinvoice"→InvoiceClient, "purchaseorder"→POClient, else→SMLClient(213).
// ⚠️ doc_no format: NEVER use "prefix-YYYY..." or "prefix-YY..." — SML UI silently
//    drops docs matching that pattern. Use prefix WITHOUT trailing hyphen + YYMM####.
//    Example: prefix="BF-SO", format="YYMM####" → "BF-SO260400001".
// ⚠️ SML 248 mojibake: charset=utf-8 header was NOT enough (SML ignores it).
//    Fix: marshalASCII helper escapes non-ASCII as \uXXXX so wire body is pure ASCII.
//    Used in all 6 POST clients (sale_reserve, saleinvoice, saleorder,
//    purchaseorder, product, MCP). See §22 #13 for the full story.
//    File: backend/internal/services/sml/json_ascii.go
// Client: backend/internal/services/sml/saleorder_client.go
// Default prefix: BF-SO; counter managed by doc_counter_repo.go

// 2. Create saleinvoice  ← CONFIRMED WORKING (2026-04-24) — legacy ใบกำกับภาษี
// POST /SMLJavaRESTService/restapi/saleinvoice
// {
//   "doc_format_code": "INV",
//   "doc_date": "2026-04-24",
//   "cust_code": "AR00004",
//   "is_permium": 0,         ← int (ไม่ใช่ bool), typo intentional
//   "vat_type": 0,           // 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
//   "details": [             ← key ต้องเป็น "details" ไม่ใช่ "items"
//     {
//       "item_code": string,
//       "unit_code": string,
//       "wh_code": "WH-01",
//       "shelf_code": "SH-01",
//       "price_exclude_vat": number,
//       "sum_amount_exclude_vat": number
//     }
//   ]
// }

// Client: backend/internal/services/sml/saleinvoice_client.go
// Retry: max 3 ครั้ง
// Config จริง: guid=smlx / SMLGOH / SMLConfigSMLGOH.xml / SML1_2026
// SKU จริงใน SML 248: CON-xxxxx (ถุง), STEEL-xxxxx (เส้น), PLUMB-xxxxx (ท่อน)

// 3. Create purchaseorder  ← CONFIRMED WORKING (2026-04-27)
// POST /SMLJavaRESTService/v3/api/purchaseorder
// Same payload shape as saleinvoice, except:
//   - field "buy_type" instead of "sale_type"
//   - cust_code semantically = supplier
//   - ⚠️ doc_no MUST be non-empty (unlike saleinvoice). The v3 endpoint
//     does NOT auto-generate. Sending null/empty triggers ic_trans NOT NULL.
// Client: backend/internal/services/sml/purchaseorder_client.go
// doc_no convention: "BF-PO-YYYYMMDD-{8-char bill UUID prefix}" generated
// in bills.go retryPurchaseOrder. SML response data.doc_no often empty;
// fall back to the request doc_no in that case.
// Used by: shopee_shipped emails (handlers/shipped_email.go).

// 4. Create product  ← CONFIRMED WORKING (2026-04-27)
// POST /SMLJavaRESTService/v3/api/product
// {
//   "code": "TEST-001",            ← user-supplied
//   "name": "ทดสอบ",
//   "tax_type": 0, "item_type": 0, "unit_type": 1,
//   "unit_cost": "ชิ้น", "unit_standard": "ชิ้น",
//   "purchase_point": 0,
//   "units": [{"unit_code":"ชิ้น","unit_name":"ชิ้น","stand_value":1,"divide_value":1}],
//   "price_formulas": [{"unit_code":"ชิ้น","sale_type":0,"price_0":"99.5",
//                       "tax_type":0,"price_currency":0}]
// }
// Response: {"success":true,"data":{"code":"..."}} (SML may return
// a different code than requested — use response code as canonical).
// Client: backend/internal/services/sml/product_client.go
// Wired to BillFlow: POST /api/catalog/products handler upserts into
// sml_catalog with status='pending' + triggers background embedding.
// Used by: BillDetail's MapItemModal "+ สร้างสินค้าใหม่" form.

// 5. Customer / Supplier lookup (Party Master)  ← CONFIRMED WORKING (2026-04-28)
// GET /SMLJavaRESTService/v3/api/customer?page=1&size=100
// GET /SMLJavaRESTService/v3/api/supplier?page=1&size=100
// Auth headers same as above (guid, provider, configFileName, databaseName)
// Response:
//   {"success":true,"data":[{"code":"AR00001","name":"ลูกค้า จาก AI",...}],
//    "pages":11,"page":1,"size":100}
// PartyClient fetches all pages on boot (paginated, size=100).
// PartyCache wraps it: in-memory map, boot fetch + 6h background refresh,
//   sub-ms search by prefix/substring (case-insensitive).
//
// Production counts (verified 2026-04-28):
//   Customers: 1004 records, AR-prefixed
//     AR00001 "ลูกค้า จาก AI"
//     AR00002 "ลูกค้า จาก Line"
//     AR00003 "ลูกค้า จาก Email"
//     AR00004 "ลูกค้า จาก Shopee"  ← Quick-setup uses AR00001-04
//   Suppliers:  500 records, V-prefixed (V-001, V-002, …)
//
// ⚠️ cust_code for SML 248 (saleinvoice / purchaseorder) MUST come from
//    channel_defaults table — NOT hardcoded env vars (removed session 7).
// ⚠️ contact_name for SML 213 (sale_reserve) is overridden with
//    channel_defaults.party_name to prevent AR pollution from AI chat.
//    Real customer info remains in bills.raw_data only.
// ⚠️ Quick-create customer/supplier via API was DROPPED — SML legacy
//    /restapi/customer requires ~25 fields; v3 returns NullPointerException.
//    Users create parties in SML manually, then click "รีเฟรช" in BillFlow UI.
// Client: backend/internal/services/sml/party_client.go
//         backend/internal/services/sml/party_cache.go
```

---

## 11. Worker Pool

```go
// backend/internal/worker/pool.go
// จำกัด concurrent requests ป้องกัน spike

type WorkerPool struct {
  openrouterSem chan struct{}  // max 5 concurrent
  smlSem        chan struct{}  // max 3 concurrent
}
// LINE webhook respond 200 ก่อนเสมอ → job เข้า pool
```

---

## 12. LINE Admin Notifications

```go
// push message (ไม่ใช่ reply) ไปหา LINE_ADMIN_USER_ID
// กรณีที่แจ้ง:
// - SML fail หลัง retry 3 ครั้ง
// - bill anomaly block ถูก reject
// - IMAP connection fail
// - disk usage > 90%
// - LINE token จะหมดอายุใน 7 วัน
// - F4 daily insight สร้างเสร็จ (08:00)
```

---

## 13. F3 — Voice Input

```go
// handlers/line.go
case linebot.MessageTypeAudio:
  audioData, _ := s.lineService.DownloadContent(msg.ID)
  text, _ := s.aiService.TranscribeAudio(audioData)
  // ส่ง text ต่อไป extract pipeline

// ⚠️ audio มี expiry → download ทันที
// voice > 60 วินาที → แจ้ง user
// confidence ลดลง 0.1 สำหรับ voice
```

---

## 14. F4 — Daily AI Insights

```go
// Cron 08:00 ทุกวัน
const InsightPrompt = `
คุณเป็น business analyst สรุปข้อมูลธุรกิจเป็นภาษาไทย
กระชับ 3-5 ประโยค ใช้ emoji นำหน้าแต่ละประเด็น
ข้อมูลวันนี้ vs สัปดาห์ที่แล้ว: %s
สรุป: trend / สินค้าขายดี-แย่ / บิลมีปัญหา / คำแนะนำ
`
// ตัวอย่าง output:
// 📈 ยอดบิลสัปดาห์นี้สูงกว่าปกติ 23%
// 🏆 ปูนซีเมนต์ยังคงเป็นสินค้าขายดีอันดับ 1
// ⚠️ พบบิลราคาผิดปกติ 3 รายการรอ review
// 💡 ควรเพิ่ม stock CEM001 — ใช้ไปแล้ว 78%
```

---

## 15. Backup Strategy

```bash
# Cron 00:00 ทุกวัน
docker exec billflow-postgres \
  pg_dump -U billflow billflow \
  > ~/billflow/backups/$(date +%Y%m%d).sql

# เก็บ 30 วัน
find ~/billflow/backups -mtime +30 -delete

# สร้าง folder ก่อน
mkdir -p ~/billflow/backups
```

---

## 16. Project Structure

```
~/billflow/
│
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── config/config.go
│   │   ├── database/
│   │   │   ├── postgres.go
│   │   │   └── migrations/001_init.sql
│   │   ├── handlers/
│   │   │   ├── line.go              ← LINE webhook + human inbox ingest
│   │   │   ├── email.go             ← general email handler + dedup
│   │   │   ├── shipped_email.go     ← Shopee shipped/pay-now → purchaseorder
│   │   │   ├── import.go            ← Lazada import
│   │   │   ├── shopee_import.go     ← Shopee Excel → local bills; Retry sends SML 248
│   │   │   ├── bills.go             ← bill CRUD + retry (3-way routed)
│   │   │   ├── mappings.go
│   │   │   ├── dashboard.go
│   │   │   ├── log_handler.go       ← GET /api/logs
│   │   │   ├── imap_settings.go     ← /api/settings/imap-accounts CRUD ✨NEW
│   │   │   ├── channel_defaults.go  ← /api/settings/channel-defaults CRUD ✨NEW
│   │   │   ├── sml_party.go         ← /api/sml/customers|suppliers search ✨NEW
│   │   │   ├── chat_inbox.go        ← /api/admin/conversations/* CRUD + extract ✨session 13
│   │   │   │                            (+ SendMedia + SetStatus + SetPhone session 14)
│   │   │   ├── chat_quick_reply.go  ← /api/admin/quick-replies CRUD ✨session 13
│   │   │   ├── chat_notes.go        ← /api/admin/conversations/:user/notes CRUD ✨session 14
│   │   │   ├── chat_tags.go         ← /api/settings/chat-tags + per-conv m2m ✨session 14
│   │   │   ├── public_media.go      ← GET /public/media/:id?t= (HMAC, no JWT) ✨session 14
│   │   │   ├── line_oa.go           ← /api/settings/line-oa CRUD + test-connect ✨session 13
│   │   │   └── auth.go
│   │   ├── middleware/
│   │   │   ├── auth.go
│   │   │   └── logger.go
│   │   ├── models/
│   │   │   ├── bill.go
│   │   │   ├── mapping.go
│   │   │   ├── user.go
│   │   │   ├── audit_log.go
│   │   │   ├── imap_account.go      ← ImapAccount model ✨NEW
│   │   │   ├── channel_default.go   ← ChannelDefault + ChannelDefaultUpsert ✨NEW
│   │   │   └── chat_crm.go          ← ChatNote + ChatTag types ✨session 14
│   │   ├── services/
│   │   │   ├── ai/
│   │   │   │   ├── openrouter.go
│   │   │   │   └── prompts.go
│   │   │   ├── mapper/mapper.go        ← F1
│   │   │   ├── anomaly/detector.go     ← F2
│   │   │   ├── sml/
│   │   │   │   ├── client.go                ← SML #1 JSON-RPC
│   │   │   │   ├── saleinvoice_client.go    ← SML #2 REST saleinvoice (legacy)
│   │   │   │   ├── saleorder_client.go      ← SML #2 REST saleorder (default Shopee) ✨NEW
│   │   │   │   ├── purchaseorder_client.go  ← SML #2 REST purchaseorder
│   │   │   │   ├── product_client.go        ← SML #2 REST product CRUD
│   │   │   │   ├── party_client.go          ← GET customer/supplier paginated ✨NEW
│   │   │   │   └── party_cache.go           ← in-memory cache + 6h refresh ✨NEW
│   │   │   ├── line/service.go         ← reply + push notify (+ PushImage session 14)
│   │   │   ├── media/signer.go         ← HMAC-SHA256 signed URL helper ✨session 14
│   │   │   ├── email/
│   │   │   │   ├── coordinator.go      ← starts one goroutine per account ✨NEW
│   │   │   │   ├── account.go          ← per-account poll loop ✨NEW
│   │   │   │   ├── processors.go       ← Shopee/general message routing ✨NEW
│   │   │   │   ├── folders.go          ← list-folders helper ✨NEW
│   │   │   │   └── imap.go             ← stateless PollOnce (refactored)
│   │   │   └── insight/service.go      ← F4
│   │   ├── worker/pool.go
│   │   ├── jobs/
│   │   │   ├── insight_cron.go
│   │   │   ├── backup_cron.go
│   │   │   ├── token_checker.go
│   │   │   └── disk_monitor.go
│   │   │   (email_poller.go — DELETED; replaced by EmailCoordinator)
│   │   └── repository/
│   │       ├── bill_repo.go
│   │       ├── mapping_repo.go
│   │       ├── user_repo.go
│   │       ├── audit_log_repo.go
│   │       ├── imap_account_repo.go   ← CRUD + status update ✨NEW
│   │       ├── channel_default_repo.go← CRUD + IsEmpty ✨NEW
│   │       ├── chat_note_repo.go      ← chat_notes CRUD ✨session 14
│   │       ├── chat_tag_repo.go       ← chat_tags + per-conv m2m ✨session 14
│   │       └── doc_counter_repo.go    ← GenerateDocNo atomic counter ✨NEW
│   │   ├── models/  (see models/ above)
│   ├── go.mod
│   ├── Dockerfile
│   └── .air.toml
│
├── frontend/
│   ├── src/
│   │   ├── pages/
│   │   │   ├── Login.tsx
│   │   │   ├── Dashboard.tsx
│   │   │   ├── Bills.tsx
│   │   │   ├── BillDetail/              ← decomposed from 1234-line monolith ✨NEW
│   │   │   │   ├── index.tsx
│   │   │   │   ├── components/          ← BillHeader, BillItems, BillTotal, etc.
│   │   │   │   ├── hooks/
│   │   │   │   └── utils/formatters.ts  ← SOURCE_LABELS, FLOW_META, etc.
│   │   │   ├── Import.tsx
│   │   │   ├── ShopeeImport.tsx
│   │   │   ├── Logs.tsx
│   │   │   ├── Mappings.tsx
│   │   │   ├── Settings.tsx
│   │   │   ├── EmailAccounts.tsx        ← /settings/email admin page ✨NEW
│   │   │   ├── EmailAccounts/
│   │   │   │   └── AccountDialog.tsx    ← add/edit inbox dialog ✨NEW
│   │   │   ├── ChannelDefaults.tsx      ← /settings/channels admin page ✨NEW
│   │   │   ├── ChannelDefaults/
│   │   │   │   ├── PartyPicker.tsx      ← searchable combobox (cmdk, 250ms debounce) ✨NEW
│   │   │   │   ├── EditDialog.tsx       ← edit row dialog ✨NEW
│   │   │   │   └── labels.ts            ← CHANNEL_LABELS, CHANNEL_SLOTS, channelHelp() ✨NEW
│   │   │   ├── LineOA.tsx               ← /settings/line-oa multi-OA admin page ✨session 13
│   │   │   ├── LineOA/
│   │   │   │   └── AccountDialog.tsx    ← add/edit OA + secret/token + greeting ✨session 13
│   │   │   ├── QuickReplies.tsx         ← /settings/quick-replies template CRUD ✨session 13
│   │   │   ├── ChatTags.tsx             ← /settings/chat-tags admin (color picker) ✨session 14
│   │   │   ├── Messages/                ← /messages chat inbox ✨session 13
│   │   │   │   ├── index.tsx            ← 2-pane layout, deep-link ?u=
│   │   │   │   ├── ConversationList.tsx ← inbox list, status tabs, server search ✨session 14
│   │   │   │   ├── MessageThread.tsx    ← drag-drop, search bar, status header ✨session 14
│   │   │   │   ├── MessageBubble.tsx    ← + phone-detect "บันทึกเบอร์" button ✨session 14
│   │   │   │   ├── Composer.tsx         ← inline compact redesign (auto-grow) ✨session 14
│   │   │   │   ├── NotesPanel.tsx       ← Phase 4.8 — internal notes ✨session 14
│   │   │   │   ├── TagsBar.tsx          ← Phase 4.9 — tag chips + picker ✨session 14
│   │   │   │   ├── CustomerHistoryPanel.tsx  ← Phase 4.5 — past bills inline
│   │   │   │   ├── CreateBillPanel.tsx  ← catalog picker → POST .../bills (status=pending)
│   │   │   │   ├── ExtractPreviewDialog.tsx ← manual AI extract from media
│   │   │   │   ├── useNotifications.ts  ← Phase 4.11 — Notification API + chime
│   │   │   │   └── types.ts             ← shared types (ChatMessage, ChatNote, ChatTag, ...)
│   │   │   └── Showcase.tsx             ← dev-only component gallery
│   │   ├── components/
│   │   │   ├── layout/
│   │   │   │   ├── Sidebar.tsx          ← redesigned sidebar (shadcn) ✨NEW
│   │   │   │   └── Topbar.tsx           ← topbar + breadcrumbs ✨NEW
│   │   │   ├── common/                  ← shared primitives ✨NEW
│   │   │   │   ├── StatusDot.tsx
│   │   │   │   ├── KeyboardShortcut.tsx
│   │   │   │   ├── EmptyState.tsx
│   │   │   │   ├── PageHeader.tsx
│   │   │   │   ├── JsonViewer.tsx
│   │   │   │   ├── DataTable.tsx
│   │   │   │   ├── LoadingSkeleton.tsx
│   │   │   │   ├── ConfirmDialog.tsx
│   │   │   │   ├── ThemeToggle.tsx
│   │   │   │   └── TagInput.tsx
│   │   │   ├── CommandPalette.tsx       ← ⌘K palette ✨NEW
│   │   │   ├── ui/                      ← 25 shadcn/ui primitives ✨NEW
│   │   │   ├── BillTable.tsx
│   │   │   ├── BillStatusBadge.tsx
│   │   │   ├── AnomalyBadge.tsx
│   │   │   ├── InsightCard.tsx
│   │   │   ├── LearningProgress.tsx
│   │   │   ├── FileUploader.tsx
│   │   │   └── StatsCard.tsx
│   │   ├── hooks/
│   │   │   ├── useAuth.ts
│   │   │   ├── useBills.ts
│   │   │   └── useHotkeys.ts            ← two-key chord hotkeys ✨NEW
│   │   ├── lib/                         ← shared utilities ✨NEW
│   │   │   ├── utils.ts                 ← cn() helper (clsx + tailwind-merge)
│   │   │   ├── theme.ts                 ← Zustand dark/light theme store
│   │   │   ├── breadcrumbs.tsx          ← route → breadcrumb label map
│   │   │   └── ui-store.ts              ← command palette open state
│   │   ├── api/client.ts
│   │   ├── store/auth.ts
│   │   └── types/index.ts
│   ├── package.json
│   ├── tailwind.config.ts               ← Tailwind 3 + shadcn theme ✨NEW
│   ├── postcss.config.js                ✨NEW
│   ├── vite.config.ts
│   └── Dockerfile
│
├── backups/
├── docker-compose.yml
├── docker-compose.dev.yml
├── .env.example
├── .gitignore
└── CLAUDE.md
```

---

## 17. Environment Variables

```bash
# .env.example

PROJECT_NAME=billflow

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

# LINE OA
# ⚠️ credentials ด้านล่างนี้ถูก expose แล้ว — REISSUE ก่อนใช้
# LINE Developer Console → Messaging API → Reissue token/secret
LINE_CHANNEL_SECRET=
LINE_CHANNEL_ACCESS_TOKEN=
LINE_ADMIN_USER_ID=    ← User ID ของ admin สำหรับ push notify

# Email IMAP — managed via /settings/email (DB-driven, multi-account)
# ไม่มี IMAP_* env vars อีกต่อไป — admin เพิ่ม inbox ผ่าน UI ได้เลย

# OpenRouter
OPENROUTER_API_KEY=sk-or-xxx
OPENROUTER_MODEL=google/gemini-2.5-flash
OPENROUTER_FALLBACK_MODEL=anthropic/claude-3-5-haiku
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
# cust_code per channel managed via /settings/channels (channel_defaults table — session 7)
# SHOPEE_SML_CUST_CODE and SHIPPED_SML_CUST_CODE REMOVED — no longer in config.go
SHOPEE_SML_SALE_CODE=           ← รหัสพนักงานขาย
SHOPEE_SML_WH_CODE=WH-01        ← รหัสคลัง (fallback — overridable per channel via /settings/channels — session 11)
SHOPEE_SML_SHELF_CODE=SH-01     ← รหัสชั้นวาง (fallback — overridable per channel)
SHOPEE_SML_UNIT_CODE=ถุง        ← หน่วย (fallback)
SHOPEE_SML_VAT_TYPE=0           ← 0=แยกนอก, 1=รวมใน, 2=ศูนย์% (fallback — overridable per channel)
SHOPEE_SML_VAT_RATE=7           ← (fallback — overridable per channel)
SHOPEE_SML_DOC_TIME=09:00

# LINE chat — admin send media (Phase 4.1 session 14)
# PUBLIC_BASE_URL must be reachable by LINE servers (jp/sg). When admin sends
# image, BillFlow constructs originalContentUrl/previewImageUrl pointing here.
# Discover the active Cloudflare Quick Tunnel URL once:
#   grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' /tmp/billflow-tunnel.log
# URL changes when cloudflared restarts (rare; current uptime 6+ days)
PUBLIC_BASE_URL=
# Signs short-lived /public/media/:id?t=... tokens (HMAC-SHA256). When empty,
# falls back to JWT_SECRET so single-secret deployments work without extra config.
MEDIA_SIGNING_KEY=

# Shopee shipped → SML purchaseorder (reuses all SHOPEE_SML_* above)
# supplier_code per channel managed via /settings/channels (channel_defaults table — session 7)
# SHIPPED_SML_CUST_CODE REMOVED — no longer in config.go
SHIPPED_SML_DOC_FORMAT=PO

# Mistral OCR — required for PDF extraction (model: mistral-ocr-2512)
MISTRAL_API_KEY=

# Shopee email domains — now per-account in imap_accounts.shopee_domains (DB)

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

## 18. Docker Compose

```yaml
# docker-compose.yml
# ⚠️ ทุก container ชื่อ billflow-* ไม่กระทบ project อื่น

services:
  postgres:
    image: postgres:16-alpine
    container_name: billflow-postgres
    volumes:
      - billflow_pgdata:/var/lib/postgresql/data
    environment:
      POSTGRES_DB: billflow
      POSTGRES_USER: ${DB_USER}
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    ports:
      - "5438:5432"
    mem_limit: 512m
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DB_USER}"]
      interval: 10s
      retries: 5
    restart: unless-stopped

  backend:
    build: ./backend
    container_name: billflow-backend
    ports:
      - "8090:8090"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
    mem_limit: 512m
    restart: unless-stopped

  frontend:
    build: ./frontend
    container_name: billflow-frontend
    ports:
      - "3010:80"
    depends_on:
      - backend
    mem_limit: 256m
    restart: unless-stopped

volumes:
  billflow_pgdata:
```

```yaml
# docker-compose.dev.yml
services:
  backend:
    volumes:
      - ./backend:/app
    command: air
  frontend:
    volumes:
      - ./frontend:/app
    command: npm run dev
    ports:
      - "5173:5173"
```

---

## 19. Cloudflare Tunnel

```bash
# cloudflared มีอยู่แล้วใน ~/cloudflared

# 1. install binary
sudo cp ~/cloudflared/cloudflared /usr/local/bin/
sudo chmod +x /usr/local/bin/cloudflared
cloudflared --version

# 2. login + สร้าง tunnel
cloudflared tunnel login
cloudflared tunnel create billflow

# 3. ~/.cloudflared/config.yml
tunnel: <TUNNEL_ID>
credentials-file: /home/bosscatdog/.cloudflared/<TUNNEL_ID>.json
ingress:
  - hostname: api.your-domain.com
    service: http://localhost:8090
  - hostname: app.your-domain.com
    service: http://localhost:3010
  - service: http_status:404

# 4. install systemd service
cloudflared service install
sudo systemctl enable cloudflared
sudo systemctl start cloudflared

# LINE webhook URL:
# https://api.your-domain.com/webhook/line
```

---

## 20. Role & Permissions

```
admin:
  - ดู/แก้ไข bills ทั้งหมด + retry
  - จัดการ mappings + ดู F1 learning stats
  - จัดการ users
  - settings (LINE, IMAP, SML, threshold, column mapping)
  - generate F4 insight on-demand
  - ดู audit log

staff:
  - ดู bills ทั้งหมด
  - confirm pending bills
  - import files
  - เพิ่ม/แก้ไข mappings

viewer:
  - ดู bills (read-only)
  - ดู dashboard + insights
```

---

## 21. Libraries

### Backend (Go)
```
github.com/gin-gonic/gin              ← HTTP framework
github.com/golang-jwt/jwt/v5          ← JWT
github.com/lib/pq                     ← PostgreSQL driver
github.com/line/line-bot-sdk-go/v8    ← LINE official SDK
github.com/emersion/go-imap/v2        ← IMAP client
github.com/xuri/excelize/v2           ← Excel parser
github.com/lithammer/fuzzysearch      ← fuzzy matching
github.com/joho/godotenv              ← .env loader
go.uber.org/zap                       ← structured logging
github.com/robfig/cron/v3             ← cron jobs
```

### Frontend (React + Vite)
```
react-router-dom         ← routing
zustand                  ← state management (with persist middleware)
axios                    ← HTTP client (shared client.ts with 401 interceptor)
recharts                 ← charts
react-dropzone           ← file upload (Lazada Import เท่านั้น)
sonner                   ← toast notifications (replaces react-hot-toast)
dayjs                    ← date formatting
cmdk                     ← ⌘K command palette primitive
vaul                     ← drawer primitive (shadcn)
tailwindcss @^3.4        ← utility-first CSS (Linear/Vercel style)
tailwindcss-animate      ← animation utilities
class-variance-authority ← component variant helper (shadcn)
clsx + tailwind-merge    ← cn() className utility
lucide-react             ← icon set (shadcn default)
@radix-ui/*              ← 25 shadcn/ui component primitives
```
> **Styling:** Tailwind 3 + shadcn/ui + HSL design tokens (dark mode ready)
> CSS bundle: 89 KB → 44 KB after migration
> **ทุก data fetching:** manual `useState`/`useEffect` (ไม่ใช้ @tanstack/react-query)
> **Hotkeys:** two-key chord system — g d (dashboard), g b (bills), g i (import),
>   g s (settings), g m (mappings), g l (logs), g c (catalog), g x (export)

---

## 22. ข้อควรระวัง

```
1. LINE Webhook
   - verify X-Line-Signature ทุก request
   - respond HTTP 200 ใน 1 วินาที → processing async เสมอ
   - ⚠️ credentials ที่แชร์ใน chat ถูก expose → REISSUE ก่อน

2. SML API
   - 192.168.2.213 อยู่ใน internal network เท่านั้น
   - response text เป็น JSON ซ้อนกัน → parse 2 ชั้น
   - retry max 3 → failed + LINE admin notify

3. OpenRouter
   - image → base64 หรือ URL
   - PDF → plugins: file-parser
   - ไม่ใช่ JSON → retry ด้วย fallback model

4. IMAP (multi-account — DB-driven)
   - จัดการ inbox ผ่าน /settings/email (admin only) — ไม่ต้องแก้ .env
   - DB CHECK enforces poll_interval_seconds >= 300 (5 min minimum)
   - mark_read หลัง process กัน process ซ้ำ
   - consecutive_failures >= 3 → LINE admin notify (throttle 1/hour per inbox)
   - Gmail ต้องใช้ App Password (ไม่ใช่ password จริง)
     → myaccount.google.com → Security → App passwords → Create
     → ต้องเปิด 2-Step Verification ก่อน
     → AccountDialog มี App Password popover ช่วย guide ตามประเภท host
   - Outlook: imap-mail.outlook.com:993 (App Password เช่นกัน)
   - Gmail rate limit: poll ถี่กว่า 5m → unexpected EOF
   - ลูกค้าเพิ่ม inbox ใหม่ได้เองจาก UI (test-connection + list-folders buttons)

5. Disk & Memory
   - เคลียร์ disk ก่อน Phase 1 (เป้าหมาย < 70%)
   - mem_limit ใน docker-compose กัน OOM
   - cron ตรวจ disk ทุกวัน → แจ้งถ้า > 90%

6. LINE Token
   - rotate ทุก 90 วัน
   - cron check รายสัปดาห์ → แจ้งล่วงหน้า 7 วัน

7. Shopee Column Mapping (hardcoded — ไฟล์จาก Shopee Seller Center คงที่)
   - หมายเลขคำสั่งซื้อ, สถานะการสั่งซื้อ, วันที่ทำการสั่งซื้อ
   - ชื่อสินค้า, เลขอ้างอิง SKU (SKU Reference No.), ราคาขาย, จำนวน
   - exclude: "ที่ต้องจัดส่ง", "ยกเลิกแล้ว"
   - dedup: ตรวจ bills WHERE source='shopee' AND raw_data->>'order_id' = ?

8. Lazada Column Mapping
   - ❌ ห้าม hardcode column names
   - ✅ เก็บใน DB → admin แก้ได้จาก /settings
   - รอไฟล์จริงจากลูกค้าก่อน Phase 4b

9. SML 248 Machine (Shopee)
   - เครื่อง 192.168.2.248 — ✅ CONFIRMED WORKING (2026-04-24)
   - Product lookup: curl "http://192.168.2.248:8080/SMLJavaRESTService/v3/api/product/CON-01000"
     -H "guid: smlx" -H "provider: SMLGOH" -H "configFileName: SMLConfigSMLGOH.xml" -H "databaseName: SML1_2026"
   - ⚠️ SKU ในไฟล์ test ต้องมีอยู่ใน ic_inventory ของ SML 248
     REST-00002 ไม่มีใน DB → ต้องใช้ CON-xxxxx / STEEL-xxxxx ฯลฯ
   - ⚠️ SHOPEE_SML_UNIT_CODE ต้องไม่ว่าง (ตั้ง "ถุง" เป็น fallback)
   - DB check: docker run --rm postgres:16-alpine psql
     'postgresql://postgres:sml@192.168.2.248:5432/sml1_2026'
     -c "SELECT code, unit_standard FROM ic_inventory LIMIT 10"

10. SML Party Master (Quick-create DROPPED)
    - SML legacy /restapi/customer ต้องการ ~25 fields (name_1, tambon, zip_code ฯลฯ)
    - SML v3 /api/customer POST returns NullPointerException ถ้าขาด field ใด
    - BillFlow ไม่ implement create — ให้ผู้ใช้สร้าง party ใน SML เองแล้วกด "รีเฟรช"
    - PartyCache ดึงข้อมูลทั้งหมดตอน boot + refresh ทุก 6 ชั่วโมง

11. Channel Defaults (สำคัญมาก — ทุก SML send อ่านจาก table นี้)
    - ทุก SML send (4 routes) อ่าน cust_code/contact_name จาก channel_defaults table
    - ถ้าตารางว่าง → retry บิลจะ error ทันที "ยังไม่ได้ตั้งค่าลูกค้า default"
    - แก้: เข้า /settings/channels กดปุ่ม "ตั้งค่าอัตโนมัติ" (Quick setup)
      จะ pair AR00001-04 ตามชื่อ channel ให้อัตโนมัติ
    - SML 213 path (sale_reserve) override contact_name ด้วย party_name จาก table
      เพื่อกัน AR pollution จาก AI-extracted name; ข้อมูลลูกค้าจริงอยู่ใน raw_data
    - SML 248 paths (saleorder/saleinvoice/purchaseorder) set cfg.CustCode = party_code จาก table
    - per-channel WH/Shelf/VAT override (session 11): wh_code, shelf_code,
      vat_type, vat_rate ใน table จะ overlay ทับ env. Sentinel '' / -1 = "ใช้
      ค่าจาก server .env" — เลือก override เฉพาะที่ต้องการ. applyChannelOverrides()
      ใน bills.go ทำให้ทั้ง 3 SML 248 retry paths
    - production placeholders: AR00001 "ลูกค้า จาก AI" / AR00002 "ลูกค้า จาก Line"
      AR00003 "ลูกค้า จาก Email" / AR00004 "ลูกค้า จาก Shopee"

12. doc_no SML bug (ค้นพบ session 7-10)
    - Pattern "prefix-YYYY..." หรือ "prefix-YY..." ใน doc_no → SML POST returns success
      แต่ doc ไม่แสดงใน SML UI (silently dropped)
    - Trigger: hyphen ตามด้วย 4-digit year หรือ 2-digit year + month ทันที
    - วิธีแก้: ใช้ prefix ที่ไม่มี trailing hyphen แล้วต่อด้วย YYMM#### ทันที
      เช่น "BF-SO" + "YYMM####" → "BF-SO260400001" ✅
      ห้ามใช้: "BF-SO-" + "YYYY-MM..." → "BF-SO-2026..." ❌
    - doc_counter_repo.go ใช้ GenerateDocNo(prefix, format, now) จัดการให้อัตโนมัติ

13. SML mojibake — ASCII-escape JSON workaround (session 12 — 2026-04-29)
    - **เคยคิดว่าแก้ด้วย Content-Type: application/json; charset=utf-8** (session 7-10)
      แต่ verify จริง session 12 พบว่า **SML 248 server ignore charset header เลย**
      ไม่ว่าจะ application/json หรือ application/json; charset=utf-8 ก็ mojibake เหมือนกัน
      เพราะ SML's Java backend อ่าน body ด้วย Latin-1 (ISO-8859-1) ตลอดเวลา
    - **ทางแก้จริง: marshalASCII helper** ที่ [json_ascii.go](backend/internal/services/sml/json_ascii.go)
      → escape ทุก non-ASCII rune เป็น \uXXXX (และ surrogate pair สำหรับ BMP+)
      → body กลายเป็น pure ASCII → Latin-1 vs UTF-8 byte-identical → no mojibake
      → JSON parser ของ SML จะ unescape ส → "ส" ตาม spec ก่อน server code เห็น
    - ใช้ใน 6 POST clients ทั้งหมด: sale_reserve (213), saleinvoice + saleorder +
      purchaseorder + product (248), MCP — แทนที่ json.Marshal ทุกที่ที่ส่ง body ไป SML
    - **บันทึกใน bills.sml_payload + audit_logs ใช้ json.Marshal ปกติ** (UTF-8 raw Thai)
      เพื่อให้ admin debug อ่านง่าย — ไม่กระทบ wire bytes ที่ส่งจริง
    - GET-only client (party_client.go) ไม่ต้องตั้ง — ไม่มี request body
    - Existing master records ที่ corrupt จาก session ก่อนหน้า (สร้างผ่าน product create
      ก่อน session 12) ต้องลบ + สร้างใหม่ใน SML — แก้ใน SML ผ่าน BillFlow ไม่ได้
      เพราะถ้าส่ง update ผ่าน old code path ก็ยัง mojibake

14. doc_no reuse on retry (idempotent)
    - bills.go บันทึก sml_doc_no ลง DB ก่อน SML call
    - ถ้า retry บิลที่มี sml_doc_no อยู่แล้ว → ใช้ doc_no เดิม (ไม่ increment counter)
    - กัน doc_no inflation กรณี transient network fail แล้ว retry
    - ถ้า retry แล้ว SML ตอบ "duplicate" → user เห็น error ชัด ไม่ใช่ silent skip

15. Catalog per-row actions (session 12 — 2026-04-29)
    - /settings/catalog แต่ละ row มีปุ่ม Refresh (🔄) + Delete (🗑️)
    - Refresh: POST /api/catalog/:code/refresh → GET /v3/api/product/{code} จาก SML
      → upsert ลง sml_catalog (preserve price; sml endpoint ไม่ return price)
      → SML ตอบ data:null (= product ไม่มีใน SML แล้ว) → 404 + not_found:true
        → UI แนะนำ "ลบจาก BillFlow"
    - Delete: DELETE /api/catalog/:code → ลบเฉพาะ BillFlow's sml_catalog row
      → SML 248 ไม่ถูกแตะ — ใช้ prune zombie ที่ admin ลบจาก SML แล้ว
      → ทุก action: reload catalog index + audit log
    - Bulk "Sync จาก SML" ตอนนี้ก็ reload index หลัง upsert ทั้งก้อน

16. Multi-LINE OA (session 13 — 2026-04-29)
    - ทุก inbound webhook resolve OA โดย:
      1. URL :oaId ถ้ามี (`/webhook/line/<oa_id>`)
      2. payload `destination` field (bot's own userID) → registry.GetByBotUserID
      3. registry.Any() (legacy single-OA fallback)
    - **Webhook URL ต้องตั้งให้ตรงต่อ OA** — admin ต้องเข้า LINE Developer Console
      ของแต่ละ OA แล้ววาง URL `/webhook/line/<id>` (copy จาก `/settings/line-oa`)
    - chat_conversations.line_oa_id ตั้งเฉพาะตอน insert ครั้งแรก — ลูกค้าเดียวกัน
      ใน OA ต่างกันมี LINE userID ต่างกันอยู่แล้ว ไม่ต้องกังวลเรื่อง row collision
    - **legacy conversation rows** (ก่อน migration 014) มี line_oa_id = NULL —
      `pushService(conv)` fallback เป็น `registry.Any()` กัน reply พังตอน admin
      ตอบห้องเก่า. รอ row ตอบ inbound ครั้งใหม่จะ backfill อัตโนมัติ
    - Default OA seeded จาก env LINE_* บน first boot (`line_oa_accounts.IsEmpty()`)
      → ถ้า admin ตั้ง LINE_* ใน .env ไว้ ระบบสร้าง row "Default (from .env)" ให้
      → admin แก้ name/greeting/enabled ผ่าน UI ได้, secret/token enter เพียงครั้งเดียว
    - LINE Push API quota — **Free OA = 200 push/เดือน** (Light Plan). Reply API
      ฟรีไม่นับ quota — ดู #21 hybrid send. Phase 4.1 (admin ส่ง media) จะใช้
      quota เพิ่ม. ใส่ banner เตือนใน UI หลัง 4.1 ship

17. Chat inbox features (Phase 4 — session 13)
    - **Quick replies** (`/api/admin/quick-replies`): 4 seed templates +
      admin CRUD. Composer popover เปิดด้วยปุ่ม 💬 → click template → fill textarea
    - **Customer history panel**: collapsible ใต้ thread header แสดง 10 บิล
      ล่าสุดของลูกค้าคนนี้ (query `bills WHERE raw_data->>'line_user_id' = ?`)
      → คลิก doc_no → /bills/:id
    - **Browser notification + chime**: hook `useNotifications` ใน Messages/.
      Toggle ปุ่ม 🔔 บน thread header (persisted ใน localStorage). Notification
      API ใช้ตอน tab hidden เท่านั้น (เลี่ยง double-notify with sonner toast)

18. Admin send-media + Cloudflare Quick Tunnel (session 14 — Phase 4.1)
    - **LINE Push API รองรับเฉพาะ image/video/audio** — ไม่มี file (PDF/doc) type
      → admin ส่ง PDF กลับลูกค้าไม่ได้ผ่าน Push (workaround: Flex link, deferred)
    - Image limits: ≤10MB JPEG/PNG/WebP, ≤4096×4096 (originalContentUrl)
      Preview: ≤1MB JPEG ≤240×240 — ใช้ URL เดียวได้ถ้าไฟล์เล็ก
    - **Cloudflare Quick Tunnel** ทำงานบน server (PID 2265016, log /tmp/billflow-tunnel.log)
      → URL: https://recorders-thinks-distance-injuries.trycloudflare.com
      → URL **เปลี่ยนเมื่อ cloudflared restart** — admin ต้อง re-paste ลง .env
      → uptime ปัจจุบัน 6+ วัน (started manually, no systemd)
      → v2/Phase 8: replace ด้วย named tunnel + domain
    - **HMAC-signed public URL**: /public/media/:id?t=<token> ที่ token = exp.signature
      → 1h expiry; LINE มักดึงรูปใน 1-2 วินาที, expiry ยาวพอกัน retry
      → Signing key: `MEDIA_SIGNING_KEY` env, fallback เป็น JWT_SECRET
    - **Drag-drop overlay** บน MessageThread + paste image (clipboard) บน textarea
    - non-image upload → toast warning ทันที (โดยไม่ส่ง LINE)
    - ค่า PUBLIC_BASE_URL ว่าง → SendMedia handler ตอบ 503 "ยังไม่ได้ตั้ง"

19. Conversation lifecycle status (session 14 — Phase 4.2)
    - migration 016: chat_conversations.status (open/resolved/archived)
    - **Auto-revive**: ลูกค้าส่ง inbound → status='resolved' → กลับเป็น 'open' อัตโนมัติ
      ใน processMessage (`AutoReviveOnInbound`). 'archived' sticky — ไม่ revive
    - ConversationList tabs: เปิดอยู่ / ปิดแล้ว / Archive
    - Thread header buttons: ✓ ปิดเรื่อง / ↺ เปิดอีกครั้ง / 🗄 Archive / ↻ unarchive

20. CRM lite (session 14 — Phase 4.7 + 4.8 + 4.9)
    - migration 017: phone column + chat_notes + chat_tags + chat_conversation_tags
    - **Phone detect**: regex `(\+?\d{1,3}[\s-]?)?\d{2,3}[\s-]?\d{3,4}[\s-]?\d{4}` ที่ MessageBubble
      → ปุ่ม "บันทึกเบอร์" ปรากฏใน incoming text bubbles ที่ match
      → click → PATCH /api/admin/conversations/:user/phone
      → header แสดง 📞 <เบอร์> แทน LINE userID
      → CreateBillPanel prefill จาก conversation.phone
    - **Notes**: collapsible bar (ซ่อนถ้าไม่มี notes), warning yellow tint
      ไม่ส่ง LINE; เห็นได้ทุก admin/staff (no per-admin private)
    - **Tags**: global table + m2m. /settings/chat-tags admin CRUD page
      → 7 colors (gray/red/orange/yellow/green/blue/purple)
      → Tags row ใน thread header ใต้ status; chips + "+ tag" combobox
      → ไม่มี inbox filter by tag ใน v1 (deferred)

21. Hybrid Reply + Push API (session 15 — quota optimization)
    - **LINE Reply API ฟรี ไม่นับ quota** (verified docs: "Sending methods that
      are not counted as message count: Reply messages"). Free OA quota = **200
      push/เดือน** (Light Plan; เอกสารเดิม CLAUDE.md เคยเขียน 500 ผิด — แก้แล้ว)
    - **migration 018**: chat_conversations.last_reply_token + last_reply_token_at
      + chat_messages.delivery_method ('reply' | 'push' default 'push')
    - **Webhook caching**: line.go.processMessage cache event.ReplyToken ลง
      conversation row หลัง insert message — เว้นกรณี:
      - `deliveryContext.isRedelivery=true` (token อาจ stale แล้ว ทับของจริง)
      - greeting reply consumed token ไปแล้ว (`greetingSent=true`)
    - **Atomic consume**: ConsumeReplyToken ใช้ CTE `WITH cur AS (SELECT ... FOR UPDATE),
      upd AS (UPDATE ... RETURNING 1) SELECT FROM cur` เพื่อ:
      1. Lock row (กัน race เมื่อ admin 2 คนตอบพร้อมกัน)
      2. Capture **OLD** value (PostgreSQL RETURNING ให้ post-update value)
      3. Clear column atomic
    - **Send flow** (chat_inbox.sendOutgoingText/sendOutgoingImage):
      ```
      token := convRepo.ConsumeReplyToken(userID)
      if token != "" {
        err := svc.ReplyText/ReplyImage(token, ...)
        if err == nil → method='reply', done
        if lineservice.IsReplyTokenError(err) → fallback to Push
        else (auth/429/network) → fail without push (don't burn quota)
      }
      svc.PushText/PushImage → method='push'
      ```
    - **lineservice.IsReplyTokenError**: substring match บน err.Error() หา
      "reply token" — permissive เพราะ LINE อาจเปลี่ยนข้อความ. **ไม่** match
      401 (auth)/429 (rate limit) เพื่อหลีกเลี่ยง burning push quota แบบไร้ประโยชน์
    - **ReplyToken validity**: LINE docs ไม่ระบุเป็นเลขแน่นอน — แค่บอก "subject
      to change without notice". กลยุทธ์: **try-then-fallback** ดีกว่า hardcode
      timeout — ถ้า expired LINE บอกเอง
    - **UI badge** (MessageBubble): outgoing bubble ที่ status=sent แสดง
      - "ฟรี" สีเขียวอ่อน (delivery_method='reply') — admin รู้ว่าไม่กิน quota
      - "Push" สีเทาอ่อน (delivery_method='push') — รู้ว่ากินไปแล้ว 1 ใน 200
      Tooltip อธิบายเพิ่มทั้งสองกรณี
    - **Behavior change**: admin ที่ตอบไวภายใน reply window จะใช้ Reply API
      เกือบทั้งหมด → กิน push quota น้อยมาก → free OA ใช้งานได้ทั้งเดือน
    - **Deferred**: quota dashboard widget ดึงจาก /v2/bot/message/quota และ
      /v2/bot/message/quota/consumption แสดง "ใช้ไป X / 200" — Phase 2

22. Audit log coverage + UX consistency + tag filter (session 16)
    - **Audit log gaps closed**: 11 chat metadata endpoints ที่เคยไม่ log
      ตอนนี้บันทึก audit_logs ครบแล้ว — chat_note_*, chat_tag_*,
      chat_conv_tags_set, chat_quick_reply_*, chat_phone_saved.
      auditRepo wired เข้า ChatNotesHandler, ChatTagsHandler,
      ChatQuickReplyHandler ผ่าน constructor ใน main.go
    - **Body/label snapshots ตอน DELETE**: ก่อน repo.Delete() จะ ListAll
      หาก่อนแล้วเก็บ body_preview / label / color ลง audit detail —
      /logs จะแสดงสิ่งที่หายไปแม้ row จริงถูกลบจาก DB แล้ว
    - **MarkRead ไม่ log โดยเจตนา**: เกิดทุกครั้งที่ admin คลิกห้อง — log
      จะรกเกินไป ไม่มีคุณค่า audit
    - **Logs.tsx ACTION_META + summarize**: 17 entries ใหม่ (LINE OA CRUD,
      admin reply/send-media, status, message receive, CRM lite metadata)
      พร้อม emoji + ภาษาไทย + tone. summarize() handles per-action detail:
      reply method แสดง "ฟรี (Reply API)" / "Push", chat_conv_tags_set
      แสดง list ของ labels ที่เหลือ
    - **Composer disable เมื่อ status='archived'**: MessageThread แสดง
      banner muted-tinted "🗄 ห้องนี้ archived แล้ว — กดเปิดอีกครั้งก่อนตอบ"
      + ปุ่ม "↺ เปิดอีกครั้ง" inline ที่ flip status เป็น 'open' ทันที.
      Composer prop disabled=true ตอน archived (textarea + send disabled).
      สอดคล้องกับ semantic ของ Archive (= spam/blocked, ไม่ควรตอบโดยลืม)
    - **CreateBillPanel phone fallback**: ทั้ง path มี-extract และ
      ไม่มี-extract ใช้ `prefill.customer_phone ?? conversation.phone ?? ''`
      → admin ไม่ต้องพิมพ์เบอร์ซ้ำที่บันทึกไว้แล้วผ่าน "บันทึกเบอร์"
    - **Tag filter ใน inbox**: Phase 3 (เคย defer ใน Phase 4.9 ตอน session 14)
      - Backend: `ConversationListFilter.TagIDs []string` + EXISTS subquery
        บน chat_conversation_tags (ANY-match — มีอย่างน้อย 1 tag ตรง).
        ListConversations parse `?tags=id1,id2,id3` (comma-separated UUID).
        CountAll mirror logic เพื่อ pagination accuracy
      - Frontend: ConversationList ดึง /api/settings/chat-tags ครั้งเดียว
        ตอน mount, ปุ่ม "🏷 Tag" + count badge เปิด popover checkbox list,
        chip row ใต้ search bar แสดง tag ที่เลือกพร้อม × ลบ. Disabled
        เมื่อยังไม่มี tag (with hint /settings/chat-tags)
      - **ANY-match (OR) ไม่ใช่ AND**: เลือก 2 tags = ห้องที่มี tag A หรือ B
        (ทั้งคู่หรือแค่หนึ่ง). v2 อาจเพิ่ม toggle "all-match"
    - **ChatTags description sync**: ก่อนหน้านี้ description page เขียน
      "ใช้ filter ใน /messages" แต่ filter ไม่มีอยู่จริง — แก้ให้ตรงกับ
      Phase 3 ที่ ship จริงแล้ว ("กรอง inbox ตาม tag ได้ที่ /messages")

23. Real-time inbox via SSE + production polish (session 17)
    - **Why SSE not WebSocket**: admin inbox ต้องการ one-way push (server →
      client) เท่านั้น. SSE ใช้ HTTP/1.1 streaming + browser-native
      EventSource (auto-reconnect built-in), ไม่ต้อง upgrade handshake
      หรือ framing layer. Cloudflare Tunnel ผ่านได้โดยตรงไม่ต้อง config.
      WS = overkill สำหรับ use case นี้
    - **Architecture**:
      ```
      LINE webhook → handlers/line.go.processMessage
        → chat_messages INSERT
        → broker.Publish(MessageReceived)  ← in-process pubsub
                  ↓ fan-out non-blocking (drop on full buffer)
      handlers/sse.go.Stream (per admin tab)
        → text/event-stream wire format
                  ↓
      lib/events-store.ts (Zustand singleton EventSource)
        → useChatEvents hook subscribers
        → MessageThread / ConversationList / Sidebar
      ```
    - **In-process broker** (`services/events/broker.go`): sync.RWMutex +
      map[uint64]*subscriber. Subscribe returns buffered channel (16) +
      cleanup func. Publish non-blocking — full buffer = drop event for
      that subscriber (heartbeat will re-open SSE if truly stuck).
      Single-container deploy = no Redis needed; if scale out later swap
      this struct for a Redis-backed implementation, interface stays same
    - **HMAC token auth for SSE**: EventSource ไม่ support custom headers
      → JWT ใน query string ไม่ปลอดภัย. ใช้ pattern เดียวกับ media URL:
      POST /api/admin/events/token (JWT-protected) → reuse media.Signer
      กับ subject = adminUserID, TTL 5 min → return token. Frontend
      เปิด `EventSource('/api/admin/events?u=<userID>&t=<token>')`.
      Stream endpoint อยู่นอก JWT group — token IS the auth
    - **Wire format**:
      ```
      event: hello
      data: {}

      event: message_received
      data: {"line_user_id":"U…","message":{...}}

      :heartbeat                            ← every 20s, ignored by EventSource
      ```
      `X-Accel-Buffering: no` header → nginx/Cloudflare ไม่ buffer
    - **Events fan-out** (current 6 publish points):
      - line.go processMessage → MessageReceived + UnreadChanged on inbound
      - chat_inbox SendReply / SendMedia → MessageReceived (admin tab อื่น
        เห็น), self-tab dedup ที่ฝั่ง client
      - chat_inbox SetStatus / SetPhone / MarkRead → ConversationUpdated
      - chat_inbox MarkRead → UnreadChanged (sidebar badge)
      - chat_tags SetTagsForConversation → ConversationUpdated
    - **Self-tab duplicate dedup** (MessageThread.tsx onSSEMessage):
      เมื่อ admin ส่งข้อความ — optimistic insert tmp-row → HTTP response
      มา replace tmp ด้วย real id → SSE event มาด้วยรายการเดียวกัน. ถ้า
      ไม่ dedup จะเห็น 2 bubble. **3-way dedup**:
      1. Real id อยู่ใน list แล้ว → skip
      2. Outgoing event match tmp- row โดย kind+content/filename →
         **REPLACE tmp** (ไม่ append). HTTP response .map() ที่ตามมา
         หา tmp ไม่เจอ = harmless no-op
      3. ถ้าไม่ match ทั้งคู่ → append (incoming หรือ admin tab อื่นส่ง)
      Polling fetchDelta ก็มี defensive dedup by id เผื่อ race ระหว่าง
      ?since= และ SSE
    - **Frontend store** (`lib/events-store.ts`): Zustand singleton —
      EventSource เดียวต่อ tab. State: connecting / live / reconnecting
      / offline. Reconnect backoff [3,6,12,20,30]s; หลัง 5 รอบ →
      offline (polling 60s safety net รับช่วง). useChatEvents hook
      subscribe → handler dispatch by event type
    - **MessageThread useEffect bug fix**: เดิม useEffect deps =
      [lineUserID, fetchInitial, fetchDelta] — fetchDelta ใช้
      `conversation` (prop จาก parent) + `searchQ` (state) ใน deps →
      rebuild ทุกครั้งที่ ConversationList polling 30s update parent
      หรือ admin พิมพ์ search → useEffect re-run → mark-read + initial
      fetch ยิงซ้ำ. **Fix**: split เป็น 2 effect (ทั้งคู่ key เฉพาะ
      lineUserID) + ref pattern (`fetchDeltaRef.current`) ให้ interval
      ใช้ closure ล่าสุดได้โดยไม่ restart timer. ลด API call ~95%
    - **Polling intervals (safety net หลัง SSE)**:
      - MessageThread delta poll: 5s → 30s
      - ConversationList: 30s → 60s
      - Sidebar /dashboard/stats: 30s → 60s
      SSE drives real-time; polling รับเฉพาะกรณี broker drop / SSE
      silent break
    - **LINE markAsRead** (อ่านแล้ว ✓✓): service.go MarkMessagesAsRead
      → POST /v2/bot/message/markAsRead. **Premium feature เท่านั้น**
      (LINE Official Account Plus). Free OA ตอบ 403. ใส่ toggle ต่อ OA
      ใน /settings/line-oa (มี warning "OA Plus only"). Default OFF เพราะ
      ส่วนใหญ่เป็น Free. Best-effort call จาก MarkRead handler — error
      log + swallow
    - **Stale reply-token cron** (`jobs/reply_token_cleanup.go`): hourly
      `UPDATE chat_conversations SET last_reply_token = ''
       WHERE last_reply_token_at < NOW() - INTERVAL '1 hour'` —
      LINE token หมดอายุ subjective period; 1h เป็น safe upper bound.
      ลด wasted Reply API round-trip ตอน admin ตอบหลัง pause นาน
    - **Pending message cleanup on boot**: server crash ตอน admin กำลัง
      ส่ง → bubble ค้าง 'pending' ตลอด. Startup SQL flips outgoing
      pending > 5 min → 'failed' พร้อม delivery_error. ไม่กระทบ rows
      ปัจจุบัน (Reply/Push เสร็จในมิลลิวินาที)
    - **Connection state indicator** (Sidebar): จุดเล็ก ๆ ล่าง sidebar
      reading จาก events-store. 4 states (connecting/live/reconnecting/
      offline) + tooltip ภาษาไทย + animate-pulse ตอน reconnecting.
      Visible ทุกหน้า admin login จึงรู้สถานะ real-time ทุกที่

24. UX polish — 6-phase admin experience pass (session 18 — 2026-04-30)
    หลัง real-time inbox ลื่นแล้ว session 18 มุ่ง surface "งานที่ admin
    ต้องทำ" ในที่ที่ตามองเห็นทันที + ลด round-trip ไปมาในการ debug
    ปัญหาบิลล้มเหลว
    - **Logs preview** (Phase 1): line_message_received + line_admin_reply
      detail เพิ่ม `text_preview` (100 chars rune-aware) + filename/size
      สำหรับ media. Frontend summarize() แสดง `"สวัสดีครับ ราคา..."`
      ตรงๆ + chip "ฟรี"/"Push" บน outgoing rows. **ก่อน**: เห็นแค่
      message_id ดิบ — ต้องเปิด /messages เพื่อรู้ว่าใครพูดอะไร
    - **Bill failure card** (Phase 2): bills.go recordFailure เปลี่ยน
      error_msg เป็น JSON `{route, doc_no_attempted, error, occurred_at}`.
      Frontend BillFailureCard component — AlertCircle + route badge
      (SaleOrder/SaleInvoice/...) + monospace `<pre>` block + copy button
      ที่ assemble multi-line block (route + doc_no + timestamp + error)
      สำหรับส่ง dev. ลบ inline red text ออกจาก BillHeader. **Backwards
      compat**: parse JSON; legacy plain-text fall back to displaying as-is
    - **Sidebar reorg 5 groups** (Phase 3): เดิม "จัดการระบบ" มี 9 รายการ
      ผสมกัน. ใหม่:
      ภาพรวม / บิลขาย-ซื้อ / แชทลูกค้า / ข้อมูลตั้งต้น / ตั้งค่าระบบ
      Labels Thai-first; new optional `hint` field shows English/setup
      name in collapsed-mode tooltip (e.g. "ตารางจับคู่สินค้า" → tooltip
      shows "Item Mapping (raw_name → SML code)") เพื่อให้ admin/dev
      โยง Thai labels กลับไปยัง feature เดิม. Activity Log ขึ้นไปอยู่
      "ภาพรวม" — admin เปิดดูตอนมีปัญหา ไม่ใช่ตั้งค่า
    - **Bill Timeline** (Phase 4): GET /api/bills/:id/timeline →
      audit_logs WHERE target_id ASC, cap 200. Frontend BillTimeline
      vertical rail + tone-colored dots + relative time + summary.
      **Reuses ACTION_META + summarize()** ผ่าน `lib/audit-log-meta.ts`
      (extracted shared module) ดังนั้น /logs และ timeline render
      สอดคล้องกันโดยอัตโนมัติเมื่อเพิ่ม action ใหม่. ไม่ต้องไป /logs
      grep bill_id อีกแล้ว
    - **Inline retry on /logs** (Phase 5): expanded sml_failed row
      แสดงปุ่ม "🔄 Retry บิลนี้" ติดกับ error label. POST ไป /api/bills/
      :id/retry → toast result → refresh page logs ปัจจุบัน. ไม่ต้องไป
      /bills/:id แล้วกดอีกที
    - **Dashboard "ต้อง action" widget** (Phase 6): backend extends
      /api/dashboard/stats ด้วย `email_inbox_errors`
      (imap_accounts.consecutive_failures > 0 count). Frontend ActionCards
      บน Dashboard — 4 click-through cards (บิลรอตรวจ / บิลล้มเหลว /
      ข้อความใหม่ / Email มีปัญหา). Failed + email-error → urgent accent
      (red number + animate-pulse dot) ตอน count > 0. Bills.tsx parse
      ?status=/?source=/?bill_type= จาก URL on mount → dashboard shortcuts
      ลง pre-filtered. ลด "เปิดแล้วต้องไปคลิกแต่ละเมนูเอง"
    - **UI polish guidelines applied**:
      - Lucide icons เท่านั้นใน UI หลัก (emoji เก็บไว้สำหรับ semantic
        markers ใน /logs ACTION_META)
      - HSL token colors ทุกจุด (`bg-destructive/[0.03]`, `text-success`)
      - Typography hierarchy: text-[10px] meta → text-[11px] hint →
        text-xs body → text-sm label → text-2xl count
      - `tabular-nums` สำหรับ counts ทุกที่
      - `animate-pulse` เฉพาะ urgent dots — ไม่ใช้กับ neutral
      - micro-interaction: `hover:-translate-y-0.5` บน ActionCards +
        ArrowUpRight เคลื่อนตาม hover

25. Heuristic Evaluation pass — 16 fixes across 3 sprints (session 19 — 2026-04-30)
    Full audit ของทุก admin page (Dashboard, Bills, BillDetail, Logs,
    Messages, LineOA, QuickReplies, ChatTags, Settings, EmailAccounts,
    ChannelDefaults, CatalogSettings, Mappings, Import, ShopeeImport)
    หา redundancy + workflow gap + naming inconsistency + discoverability
    issues. ผลลัพธ์ = 16 patches กระจายใน 3 sprint ภายใน session เดียว
    - **Sprint A — Critical (5 fixes)**:
      - **A1 lib/labels.ts SSOT**: 1 หน้าเดิมเรียก "ล้มเหลว", หน้าอื่น
        เรียก "บิลล้มเหลว", หน้าที่สาม "ส่ง SML ล้มเหลว" — 3 ชื่อสำหรับ
        status เดียว. ใหม่: `BILL_STATUS_LABEL` / `BILL_SOURCE_LABEL` /
        `BILL_TYPE_LABEL` / `PAGE_TITLE` ใน lib/labels.ts. Bills, Dashboard,
        ActionCards, BillStatusBadge, Mappings ทั้งหมด import จากที่นี่
      - **A2 /settings root rewrite**: ลบ "ข้อมูลผู้ใช้" card (ซ้ำ avatar
        dropdown), ลบ "สรุประบบ" card (ซ้ำ Dashboard). Backend
        /api/settings/status เปลี่ยนจาก env-flag boolean เป็น live
        multi-account counts: `line_oa_total/enabled` + `imap_total/
        enabled/failing`. Frontend แสดง subsystem rows คลิกไปยังหน้า
        จัดการได้. Lazada column mapping ย้ายไป /import/Lazada เป็น
        collapsible card — workflow อยู่ที่เดียวกัน
      - **A3 Composer disabled + mobile responsive**: composer disabled
        state ได้ `opacity-60 + dashed border + pointer-events-none`
        เมื่อ archived (เดิมมองไม่ออก). Messages/index.tsx เปลี่ยน
        `grid-cols-[320px_1fr]` → `md:grid-cols-[320px_1fr]` +
        responsive show/hide logic. Thread header เพิ่มปุ่ม `←` มี
        `md:hidden` (mobile only) — onBackToList prop ที่ parent ลบ
        ?u= จาก URL
      - **A4 Catalog ↔ Mappings explainer banners**: 2 หน้านี้สำคัญ
        แต่ไม่มีใครอธิบายว่าใช้คู่กันยังไง. ใหม่: banner ฟ้าบนทั้ง 2 หน้า
        cross-link กันได้ + workflow steps สำหรับ Catalog ("① Sync →
        ② Embed All")
      - **A5 Inline Retry on collapsed /logs row**: เดิม Retry button
        ซ่อนใน expanded section. ใหม่: Retry icon button (RotateCw)
        แสดงในแถว collapsed สำหรับ sml_failed rows ที่มี target_id.
        Outer button → div ที่มี role=button + onKeyDown (Enter/Space)
        เพราะ HTML ห้าม nested button
    - **Sprint B — Workflow improvements (7 fixes)**:
      - **B1 ShopeeImport preflight**: ถ้า /api/settings/shopee-config
        ตอบ cust_code ว่าง → block file picker + แสดง warning Alert
        พร้อมปุ่ม "ไปตั้งค่าตอนนี้" → /settings/channels. เดิม upload
        เริ่มได้แล้ว fail late ตอน confirm
      - **B2 Mappings empty state CTA**: เพิ่มปุ่ม "ไปยืนยันบิลที่
        รอตรวจสอบ" → /bills?status=needs_review เพราะ mappings เกิดจาก
        การยืนยันบิล ไม่ใช่ manual entry
      - **B3 Tag flow cross-link**: ConversationList tag-filter popover
        เพิ่ม hint "💡 ติด tag ให้ห้องแชทได้ที่ header ของห้อง" เพื่อโยง
        2 tag UIs (filter inbox vs attach to thread) เข้าด้วยกัน
      - **B4 Extract → CreateBill smooth transition**: เพิ่ม toast
        "โหลด N รายการลงในฟอร์มแล้ว" ระหว่าง dialog swap เพื่อให้
        admin เห็น data carry-over
      - **B5 Sidebar hints visible expanded**: เพิ่ม `title=` attribute
        ที่ link wrapper — hover label → native tooltip แสดง
        "ตารางจับคู่สินค้า — Item Mapping (raw_name → SML code)".
        ก่อนหน้านี้ hint โผล่เฉพาะ collapsed mode tooltip
      - **B6 BillDetail spacing**: outer wrapper `space-y-4` → `space-y-6`
        — 8 stacked sections เคยแน่นเกินไป
      - **B7 ChannelDefaults Quick Setup tooltip**: tooltip บน button
        อธิบาย "ค้นหา AR00001–04 ใน SML แล้วผูกกับ channel ที่ยังว่าง
        — ปลอดภัย ไม่ทับของเดิม"
    - **Sprint C — Polish (4 fixes)**:
      - **C1 Outlook+Shopee preset**: ตรวจแล้ว preset `outlook-shopee`
        มีอยู่แล้วใน EmailAccounts/AccountDialog (audit อ่าน state เก่า)
      - **C2 Composer attachment count badge**: header "แนบไป N ไฟล์"
        + ปุ่ม "ล้างทั้งหมด" เหนือ thumbnail strip. เพราะ paste 5+ รูป
        เกิน strip width admin ไม่รู้ว่าแนบครบหรือยัง
      - **C3 Catalog embedding async explainer**: card "กำลัง Embed…"
        เพิ่มข้อความ "Catalog ใหญ่ ใช้เวลาเป็นนาที · รันใน background
        ปิดหน้านี้ได้" — กัน admin คิดว่าค้าง
      - **C4 Conversation freshness indicator**: thread header
        เปลี่ยนจาก absolute timestamp `30/04 14:32` เป็น relative time
        "อัปเดตเมื่อสักครู่" via `dayjs.fromNow()`. Tooltip คงข้อความ
        absolute timestamp ไว้สำหรับ debug
    - **Verified end-to-end on prod 109**: /api/settings/status returns
      live counts (line_oa_total: 1, imap_failing: 0, ai_configured:
      true). All 16 patches built + deployed in single session

26. Send-to-SML validation guard + route preview + tunnel drift monitor (session 20 — 2026-04-30)
    Three patches landed after the session 19 heuristic-eval pass: a
    validation guard on BillDetail, a route preview chip, and a daily
    Cloudflare Quick Tunnel drift cron.
    - **Validation guard** (`BillDetail/utils/validation.ts`):
      `validateForSML(bill)` mirrors the rules backend retry handler
      enforces — items.length ≥ 1; every item has non-empty item_code +
      unit_code; qty > 0; price > 0 (mirrors F2 anomaly block-rules).
      Returns `{ canSend, issues, firstBlockingItemId }`. Memoized on
      `bill` in BillDetail/index.tsx so children don't re-validate on
      unrelated parent renders. Tolerates `bill=null` during loading.
    - **BillTotal warning card**: when validation fails, Send button is
      disabled (wrapped in span so Tooltip still fires) + a warning card
      lists each issue grouped by kind ("3 รายการยังไม่ได้จับคู่") with
      a "ดู →" link per issue. Click → scroll to first offending row +
      flash 1.5s ring/tint. handleJumpToItem null-then-id transition
      ensures clicking the same "ดู" twice in a row re-fires the row's
      useEffect (otherwise React skips a no-op state update).
    - **Per-row ⚠ icon** (`BillItemRow.tsx`): new tiny status column
      (w-6) shows AlertCircle when `rowIssueReason(item)` returns
      non-empty. Tooltip says exactly which rules the row violates
      ("ยังไม่ได้ map · ขาด unit_code"). Editing-row variant keeps an
      empty placeholder cell so column alignment stays stable.
    - **Route preview chip** (backend extension to GET /api/bills/:id):
      Response now wraps the bill object with a `preview` field
      containing `{ channel, bill_type, route, endpoint, doc_format,
      doc_format_code }` resolved against the live channel_defaults row
      using the SAME logic as the retry handler (resolveEndpoint +
      mapSourceToChannel). Frontend renders a small chip below the Send
      button: "↳ SML 248 · ใบสั่งขาย (saleorder) · doc_no BF-SO-#####".
      Catches misconfigured channels (e.g. shopee bill that would fall
      through to sale_reserve because endpoint string mismatch) BEFORE
      retry, not after. When channel_default row is missing, preview
      sets `error` field instead of `route` so UI degrades gracefully.
    - **Hook ordering bug fix**: First version of BillDetail/index.tsx
      put `useMemo(...)` AFTER `if (loading) return <Skeleton />` early
      return → React error #310 ("Rendered more hooks than during the
      previous render"). Fix: hoist all hooks (useState, useMemo) above
      every early return. Lesson reflected in the comments at the top of
      BillDetail.
    - **Cloudflare Quick Tunnel drift monitor** (`internal/jobs/
      tunnel_drift_monitor.go`): daily cron at `0 2 * * *` UTC (= 9am
      Bangkok) GETs `$PUBLIC_BASE_URL/health` and pushes a LINE admin
      alert when the request fails. Why ping the public URL instead of
      reading /tmp/billflow-tunnel.log: log lives on the host (would
      need a docker-compose volume mount), and pinging tests the
      end-to-end DNS → Cloudflare → tunnel → backend path which is
      what we actually care about. Throttled to 1 push per 24h via
      sync.Mutex + lastAlerted so prolonged drift doesn't spam the
      channel. Skips registration entirely when PUBLIC_BASE_URL is
      empty (dev env). The recovery message inline-includes the exact
      4-step shell pipeline so the recipient doesn't need to dig
      through docs to fix it.
    - **HEAD vs GET gotcha**: Initial smoke test of the new tunnel
      check used `curl -sI` which sends HEAD — Gin's `r.GET("/health")`
      doesn't bind HEAD, so it returned 404 and looked like drift had
      occurred. Tunnel + URL were fine; cron uses `http.MethodGet`.
      Worth knowing for future debugging.
```

---

## 23. Build Phases

```
Phase 0 — Server Prep (ทำบน server ก่อน code บรรทัดแรก)
  [x] 0.1 เคลียร์ disk ✅
  [x] 0.2 ติดตั้ง Go 1.24.0 ✅
  [x] 0.3 Setup cloudflared ✅
  [x] 0.4 Reissue LINE credentials ✅
  [x] 0.5 mkdir -p ~/billflow/backups ✅
  [x] 0.6 verify: curl http://192.168.2.213:3248 ✅

Phase 1 — Foundation
  [x] 1.1 สร้าง ~/billflow/ structure + .gitignore ✅
  [x] 1.2 docker-compose.yml (ports 8090/3010/5438 + mem_limit) ✅
  [x] 1.3 Go: config + database + migrations ✅
  [x] 1.4 Go: JWT auth endpoint ✅
  [x] 1.5 React: Vite + React Router + Zustand ✅
  [x] 1.6 React: Login page + auth flow ✅
  [x] 1.7 ✅ ทดสอบ: docker compose up → login สำเร็จ

Phase 2 — Core AI Pipeline
  [x] 2.1 Go: OpenRouter client (text + image + PDF + audio) ✅
  [x] 2.2 Go: MapperService (exact + fuzzy + F1 learning) ✅
  [x] 2.3 Go: AnomalyService (F2 rules) ✅
  [x] 2.4 Go: SML client (JSON-RPC + retry + admin notify) ✅
  [x] 2.5 Go: WorkerPool (semaphore) ✅
  [ ] 2.6 Unit tests: extract + mapper + anomaly
  [x] 2.7 ✅ ทดสอบ: text → chatbot → SML (bill created BS20260423101501-UELM)

Phase 3 — LINE Integration
  [x] 3.1 Go: LINE webhook (verify + async) ✅
  [x] 3.2 Go: image + PDF download ✅ (code deployed, untested)
  [x] 3.3 Go: voice → Whisper (F3) ✅ (code deployed, untested)
  [x] 3.4 Go: Sales chatbot "น้องบิล" (inquiry/select/qty/cart/checkout) ✅ TESTED
  [x] 3.5 Go: LINE admin push notify ✅
  [x] 3.6b Go: Cart edit — ลบรายการที่ N, แก้จำนวนรายการที่ N เป็น Y ✅
  [ ] 3.7 ทดสอบ LINE loop: รูป / PDF / voice ← ยังไม่ test

Phase 4a — Shopee Import ← ✅ DEPLOYED (SML 248 API confirmed working)
  [x] 4a.1 Go: saleinvoice_client.go (SML 248 REST) ✅
  [x] 4a.2 Go: shopee_import.go (Preview + Confirm + GetConfig) ✅
  [x] 4a.3 React: ShopeeImport.tsx (config dialog + preview + confirm) ✅
  [x] 4a.4 Routes wired in main.go ✅
  [ ] 4a.5 ทดสอบ end-to-end กับ SKU จริง ← ต้องแก้ไฟล์ Excel + ตั้ง UNIT_CODE
  [ ] 4a.6 เพิ่ม SHOPEE_SML_UNIT_CODE=ถุง ใน server .env

Phase 4b — Lazada Import (รอไฟล์จริงจากลูกค้า)
  [ ] 4b.1 Go: Excel parser (column mapping จาก DB)
  [ ] 4b.2 React: Import page + preview + anomaly badge
  [ ] 4b.3 React: Settings → column mapping editor
  [ ] 4b.4 ✅ ทดสอบ bulk import + error report

Phase 5 — Email IMAP
  [x] 5.1 Go: IMAP poller (single account, .env-driven) ✅
  [x] 5.2 Go: attachment download + parse ✅
  [x] 5.2b Go: PDF extraction ใช้ Mistral OCR (mistral-ocr-2512) ✅
  [x] 5.2c Go: dedup by Message-ID (email) ✅
  [x] 5.3 ✅ ทดสอบ email → บิล loop (3 sent on 2026-04-24)
  [x] 5.4 Shopee shipped email → purchaseorder (separate handler)
  [x] 5.5 doc_date extracted from email body (not time.Now())
  [x] 5.6 Multi-account IMAP — DB-driven (migration 006) ✅ (session 6)
           EmailCoordinator + AccountDialog + /settings/email admin UI
           "ยืนยันการชำระเงิน" subject → ShopeeShipped flow (session 6)

Phase 6 — Web UI Complete
  [x] 6.1–6.13 initial UI complete + UX cleanup pass (13 issues) ✅
  [x] 6.14 UI redesign — Tailwind 3 + shadcn/ui (Linear/Vercel style) ✅ (session 6)
            CSS bundle 89 KB → 44 KB; dark mode tokens; ⌘K palette; chord hotkeys
            14-file BillDetail decomposition; 25 shadcn primitives; Sonner toasts
  [x] 6.15 Email Inboxes page (/settings/email) ✅ (session 6)
  [x] 6.16 Shopee Excel artifact storage (.xlsx archived per bill) ✅ (session 6)
  [x] 6.17 UTF-8 charset fix for HTML artifacts (text/html; charset=utf-8) ✅ (session 6)
  [x] 6.18 PO → ใบสั่งซื้อ/สั่งจอง relabeling across all UI components ✅ (session 6)
  [x] 6.19 Per-channel cust_code/contact_name from SML party master + /settings/channels admin UI ✅ (session 7)
  [x] 6.20 Per-channel endpoint URL + doc_format_code + doc_prefix + doc_running_format (free-form) ✅ (session 7-10)
            Shopee email default → saleorder (was saleinvoice); 4-way Retry dispatch in bills.go
            doc_no SML bug fixed: YYMM#### format, no hyphen+year pattern
            SML UTF-8 charset fix on all 6 POST clients (sale_reserve, saleinvoice,
            saleorder, purchaseorder, product, MCP — fixes Thai mojibake on product
            create + chatbot bills, not just Shopee retry)
            resolveItemName: looks up sml_catalog.item_name by item_code before falling back to raw_name
            IMAP isShopeeFrom fix: shopee_domains accepts full emails (no @ prepend bug)
            /logs redesign: 15 action labels, 1-line summary, date groups, stats bar, row expand
  [x] 6.21 Per-channel WH / Shelf / VAT override + ShopeeImport dialog removed ✅ (session 11)
            Migration 012 adds wh_code, shelf_code, vat_type, vat_rate columns to channel_defaults
            (sentinel '' / -1 = "use server .env"). bills.go applyChannelOverrides() overlays them
            on saleinvoice + saleorder + purchaseorder retry paths.
            ShopeeImport.tsx config dialog REMOVED (was misleading — only unit_code had effect).
            Replaced with read-only summary card + link to /settings/channels.
            EditDialog gains 4 new fields + scrollable body (max-h-[90vh] + grid-rows[auto,1fr,auto]).
            ChannelDefaults table action button now "แก้ไข" / "ตั้งค่า" (text + icon, default-styled
            for unset rows) instead of icon-only pencil.
  [x] 6.22 marshalASCII workaround for SML 248 mojibake + catalog per-row actions ✅ (session 12)
            ⭐ marshalASCII helper at backend/internal/services/sml/json_ascii.go — escapes
            non-ASCII as \uXXXX. Replaces json.Marshal in all 6 POST clients (saleorder,
            saleinvoice, purchaseorder, product, sale_reserve, MCP). Reason: SML 248
            Java backend reads request body as Latin-1 ALWAYS — Content-Type charset is
            ignored. Earlier "fix" via charset=utf-8 header (session 7-10) was
            confirmation bias; verified session 12 that mojibake persists with both
            'application/json' and 'application/json; charset=utf-8'. ASCII-only body
            with \uXXXX escapes is byte-identical in Latin-1 vs UTF-8 → SML's JSON
            parser unescapes back to correct codepoints.
            Catalog per-row actions: POST /api/catalog/:code/refresh + DELETE
            /api/catalog/:code. UI: 🔄/🗑️ buttons per row + ConfirmDialog before delete.
            Refresh GETs single product from SML 248, preserves price, reloads index.
            Delete is BillFlow-side only — SML 248 untouched (use for zombies).
            End-to-end verified: created TEST-THAI-001 with Thai name → SML master
            stored Thai correctly → reused as bill_item.item_code → SML doc Thai correct.
  [x] 6.23 LINE chatbot → human chat inbox + multi-OA support ✅ (session 13)
            ⭐ Replaces AI chatbot ("น้องบิล") with admin-driven inbox at /messages.
            Customer sends LINE message → store in chat_conversations + chat_messages
            (+ chat_media for binary). Admin replies via Composer → LINE Push API.
            Manual AI extract from media via "🔍 สร้างบิลจากสื่อนี้" button.
            "เปิดบิลขาย" panel: catalog picker → POST .../bills (status=pending) →
            existing /bills/:id Retry → SML 213 sale_reserve.
            Multi-OA (migration 014): /settings/line-oa CRUD; webhook URL per OA
            (/webhook/line/<oa_id>); LineRegistry routes Push by conversation's OA.
            Drops: chatbot path (~900 LOC), ChatSales / ChatSalesWithContext /
            ExtractOrderFromHistory / SalesSystemPrompt, chat_sessions table,
            chat_session_repo.go, MCPClient injection in line.go.
            Polling: 1 endpoint (dashboard/stats merged with unread_messages),
            paused when tab hidden, refresh on regain focus.
  [x] 6.24 Composer redesign + admin send-media + status/search/CRM lite ✅ (session 14)
            ⭐ Phase A — composer UX: replaced 3 hardcoded h-[60px] elements
            (popover btn / textarea / send btn) with a single rounded wrapper
            (rounded-2xl, focus-within ring). Auto-grow textarea (1→6 rows max,
            scrolls past 6) via useLayoutEffect + scrollHeight. Toolbar 📎 + 💬
            (h-8 w-8 ghost icon) on left, send button collapses h-8 w-8 → h-8
            px-3 with "ส่ง" label when canSend. PendingAttachment[] state +
            thumbnail strip + × remove. onPaste captures clipboard images.
            ⭐ Phase B — admin send-media (image/JPEG/PNG/WebP, ≤10MB):
            Backend: services/media/signer.go (HMAC-SHA256 token, exp_unix.sig
            base64url, default 1h TTL); handlers/public_media.go GET /public/
            media/:id?t= (no JWT — token IS the auth, Cache-Control max-age=300);
            chat_inbox.SendMedia (multipart upload → save chat_media → insert
            outgoing chat_message kind=image → build signed URL using
            cfg.PublicBaseURL → svc.PushImage); lineservice.PushImage(userID,
            originalContentURL, previewImageURL).
            Frontend: drag-drop overlay on MessageThread (dragCounterRef for
            nested children); optimistic UI via _localPreviewURL blob URL; URL.
            revokeObjectURL on cleanup; MessageBubble localPreview branch checked
            before API URL pattern.
            Cloudflare Quick Tunnel: discovered already running on 109 (PID
            2265016, --url http://localhost:8090, log /tmp/billflow-tunnel.log).
            URL: https://recorders-thinks-distance-injuries.trycloudflare.com.
            v1 = admin pastes URL into PUBLIC_BASE_URL .env (re-paste only on
            cloudflared restart — current uptime 6+ days). MEDIA_SIGNING_KEY
            falls back to JWT_SECRET when empty.
            LIMITATION: LINE Push ไม่มี file type — รองรับเฉพาะ image/video/
            audio/sticker/template/flex. v1 = image only. PDF/file outbound
            defer (workaround Flex Message link in v2).
            ⭐ Phase C — conversation status (migration 016):
            chat_conversations.status (open/resolved/archived). On inbound:
            convRepo.AutoReviveOnInbound flips resolved→open (archived sticky).
            ConversationList: 3-tab strip top (เปิดอยู่ default / ปิดแล้ว /
            Archive). Thread header: status badges (ปิดแล้ว / Archive) + actions
            (✓ ปิดเรื่อง / ↺ เปิดอีกครั้ง / Archive / unarchive).
            ⭐ Phase D — search (no schema):
            Inbox-level: ConversationListFilter.Q → ILIKE on display_name +
            EXISTS subquery on chat_messages.text_content. Replaces client-side
            filter with server-side ?q=. Thread-level: chat_message_repo.
            ListByUser accepts q → ILIKE branch; MessageThread search bar
            collapsible, 250ms debounce, pauses delta polling while q!=''.
            ⭐ Phase E — CRM lite (migration 017):
            4.7 Phone — chat_conversations.phone column. PHONE_RE in
            MessageBubble matches incoming text → "บันทึกเบอร์ <num>" button →
            PATCH .../phone. (CreateBillPanel prefill deferred.)
            4.8 Notes — chat_notes table; CRUD endpoints; NotesPanel.tsx
            collapsible warning-tinted bar (auto-hides when empty + closed).
            Internal-only — never sent to LINE.
            4.9 Tags — chat_tags (global) + chat_conversation_tags (m2m).
            TagsBar.tsx chips + popover picker; 7 colors (gray/red/orange/
            yellow/green/blue/purple). /settings/chat-tags admin page (sidebar:
            "Chat Tags").
            New routes (App.tsx + Sidebar.tsx): /settings/chat-tags.
            Verified end-to-end on production (109): composer auto-grow, paste
            image preview, drag-drop, public/media HMAC token round-trip,
            status flip + auto-revive, server search, notes/tags CRUD.
  [x] 6.25 Hybrid Reply+Push API (LINE quota optimization) ✅ (session 15)
            ⭐ LINE Reply API ฟรีไม่นับ quota; Push = 200/mo Free OA.
            Migration 018 caches replyToken on chat_conversations + adds
            delivery_method to chat_messages. Webhook caches token (skip on
            isRedelivery + when greeting consumes). ConsumeReplyToken atomic
            CTE pattern (SELECT FOR UPDATE → UPDATE → return OLD value) so
            concurrent admins don't race. Send flow tries Reply first, falls
            back to Push only on token errors (auth/429 don't fall through).
            UI: "ฟรี"/"Push" badge on outgoing bubbles tells admin which
            transport was used. Fixed CLAUDE.md quota number 500→200 (was
            wrong; Light Plan = 200/mo per LINE pricing docs).
  [x] 6.26 Audit log coverage + UX consistency + tag filter ✅ (session 16)
            ⭐ Audit gaps closed: 11 chat metadata endpoints (notes/tags/
            quick-reply CRUD + chat_phone_saved) now log to audit_logs.
            Body/label snapshots captured on DELETE so /logs preserves what
            disappeared. MarkRead intentionally not logged (too noisy).
            Logs.tsx ACTION_META + summarize(): 17 new chat-related entries
            with Thai labels + emoji + tone — actions that previously
            rendered as raw strings now show properly.
            UX: Composer disabled when status='archived' with banner +
            inline "↺ เปิดอีกครั้ง". CreateBillPanel falls back to
            conversation.phone when no extract prefill. ChatTags page
            description fixed (was misleading).
            Tag filter in inbox (lifted from Phase 4.9 deferral):
            ConversationListFilter.TagIDs + EXISTS subquery (ANY-match);
            ConversationList Tag popover w/ multi-select + chip row.
            Backend ?tags=id1,id2 query param. CountAll mirrors filter
            logic for pagination accuracy.
  [x] 6.27 Real-time inbox via SSE + production polish ✅ (session 17)
            ⭐ Replaces aggressive polling (5s thread / 30s inbox / 30s
            sidebar) with Server-Sent Events for sub-second admin updates.
            Polling kept at 30/60s as safety net.
            Backend: services/events/broker.go in-process pubsub +
            handlers/sse.go SSE stream + token issuer (HMAC via media.Signer
            with subject=adminUserID, 5min TTL). Publish points: webhook
            inbound, SendReply, SendMedia, SetStatus, SetPhone, MarkRead,
            SetTagsForConversation. Migration 019 adds line_oa_accounts.
            mark_as_read_enabled per-OA opt-in for LINE Premium "อ่านแล้ว"
            (Free OA returns 403, hence opt-in). jobs/reply_token_cleanup
            hourly clears tokens >1h. Startup cleanup flips outgoing
            chat_messages stuck >5min in 'pending' → 'failed' (recovery
            from server crashes).
            Frontend: lib/events-store.ts Zustand singleton EventSource
            (one per tab) + reconnect backoff [3,6,12,20,30]s; hooks/
            useChatEvents.ts typed subscription hook. Layout connects
            on mount. Connection state indicator dot in Sidebar.
            BUG FIX: MessageThread useEffect was re-running on every
            parent render (mark-read spam). Split into 2 effects keyed
            on lineUserID + ref pattern for fetchDelta.
            Self-tab dedup: SSE event for admin's own send replaces the
            optimistic tmp- row instead of adding (matches kind+content
            for text, kind+filename for image). Fixes the "2 bubbles
            until refresh" duplicate.
            AccountDialog mark-as-read checkbox in /settings/line-oa.
            Verified end-to-end: SSE token round-trip, hello event arrives,
            heartbeats every 20s, dedup works for self-tab.
  [x] 6.30 Send-to-SML validation guard + route preview + tunnel drift cron ✅ (session 20)
            ⭐ Three follow-up patches after the heuristic-eval pass.
            Validation guard (frontend): BillDetail validates items against
            the same rules the backend retry handler enforces (item_code +
            unit_code non-empty, qty > 0, price > 0) BEFORE allowing
            "Send to SML". Disabled button + tooltip + warning card listing
            each issue + "ดู →" link that scrolls + flashes the offending row.
            Per-row AlertCircle icon in a new tiny status column with hover
            tooltip showing the exact reason. Caught at the cheapest point
            (frontend) instead of via a confusing "failed" bill afterwards.
            Route preview (backend extension to GET /api/bills/:id): adds
            preview {channel, route, endpoint, doc_format} resolved against
            live channel_defaults using the same routing logic as retry.
            Frontend chip below Send button shows "↳ SML 248 · ใบสั่งขาย ·
            doc_no BF-SO-#####" so admins catch misconfigured channels
            BEFORE retry. Graceful when channel_default row is missing.
            Hook ordering bug fix: First validation patch put useMemo
            after early return → React error #310 in production. Hoisted
            all hooks above early returns; comment in source documents
            the trap for future contributors.
            Cloudflare Quick Tunnel drift cron (jobs/tunnel_drift_monitor.go):
            daily 9am Bangkok GETs PUBLIC_BASE_URL/health and pushes a LINE
            admin alert (with inline recovery commands) when the request
            fails. Throttled 1 push / 24h. Skips registration when
            PUBLIC_BASE_URL is unset.

  [x] 6.29 Heuristic Evaluation pass — 16 fixes across all admin pages ✅ (session 19)
            ⭐ Full audit pass identified 6 critical + 7 high-impact + 4 polish
            issues across 13 admin pages. Three sprints landed in one session.
            Sprint A (5 critical):
              - lib/labels.ts SSOT eliminated 3 different labels for the same
                bill status (Bills/Dashboard/Logs/Badge now agree).
              - /settings root rewrite — dropped 2 duplicate cards (user info
                + system stats), made connection status live + multi-account
                aware (line_oa_total/enabled, imap_total/enabled/failing).
                Lazada column mapping moved to /import where it belongs.
              - Composer disabled visual + Messages mobile responsive (back
                button, single-pane <md breakpoint).
              - Catalog ↔ Mappings explainer banners cross-link the
                two-stage matching pipeline.
              - /logs sml_failed rows show Retry button at row level (was
                hidden in expanded section). Refactored outer button → div
                so the inner Retry button is valid HTML.
            Sprint B (7 workflow improvements):
              - ShopeeImport preflight blocks file picker when channel
                config is missing (was: late-fail at confirm).
              - Mappings empty state CTA links to /bills?status=needs_review.
              - Tag-filter popover hints where to attach tags to threads.
              - Extract → CreateBill toast bridges dialog swap so data
                carry-over is visible.
              - Sidebar hints visible in expanded mode via title= attr.
              - BillDetail space-y-4 → space-y-6 (was cramped).
              - ChannelDefaults Quick Setup tooltip explains "safe, won't
                overwrite existing rows".
            Sprint C (4 polish):
              - Composer attachment count badge "แนบไป N ไฟล์".
              - Catalog embedding card explains async + reassures admin.
              - Conversation header relative time "อัปเดตเมื่อสักครู่".
              - C1 (Outlook+Shopee preset) verified already shipped.
            Verified on prod: /api/settings/status returns multi-account
            counts; all renames consistent across pages.

  [x] 6.28 UX polish — surface admin work + reduce debug round-trips ✅ (session 18)
            ⭐ Six-phase admin experience pass on top of the real-time
            inbox shipped in 6.27. Goal: make work-to-do impossible to
            miss + cut clicks for common debug flows.
            Phase 1 — /logs preview: line_message_received + line_admin_
            reply detail include text_preview (rune-aware); media events
            include filename + size_bytes. Frontend summarize() shows
            quoted text, media as "filename (123 KB)", and a Reply/Push
            chip on outgoing rows. No more raw message_ids.
            Phase 2 — Bill failure card: bills.go recordFailure stores
            error_msg as JSON {route, doc_no_attempted, error,
            occurred_at}. Frontend BillFailureCard with monospace error
            block + multi-line copy button (assembles route+doc_no+
            timestamp+error for dev triage). Backwards-compat: legacy
            plain-text rows still render verbatim.
            Phase 3 — Sidebar reorg into 5 domain groups (Overview /
            Bills / Chat / Master Data / System) with Thai-first labels
            + English hint tooltips. Activity Log moves to Overview.
            Phase 4 — Bill Timeline: GET /api/bills/:id/timeline + new
            BillTimeline component on BillDetail. Reuses ACTION_META +
            summarize() via extracted lib/audit-log-meta.ts so /logs
            and the timeline stay aligned.
            Phase 5 — Inline retry on /logs sml_failed rows. POST to
            /api/bills/:id/retry without leaving the page.
            Phase 6 — Dashboard "ต้อง action" widget: 4 ActionCards
            (บิลรอตรวจ / บิลล้มเหลว / ข้อความใหม่ / Email มีปัญหา) with
            urgent red accent + pulse dot when count > 0. Backend
            ImapAccountRepo.CountFailing surfaces email_inbox_errors.
            Bills.tsx reads ?status=/?source=/?bill_type= from URL so
            dashboard shortcuts land pre-filtered.

Phase 7 — Background Jobs
  [x] 7.1 Cron 08:00 daily insight + LINE notify (F4) ✅
  [x] 7.2 Cron 00:00 pg_dump | gzip backup (.sql.gz) ✅ (verified 20 MB output)
  [x] 7.3 Cron weekly LINE token expiry check ✅
  [x] 7.4 Cron daily disk monitor → notify > 90% ✅

Phase 8 — Production Ready
  [ ] 8.1 Cloudflare Tunnel + systemd (cloudflared installed, not configured)
  [ ] 8.2 docker-compose production build
  [x] 8.3 Structured logging (zap) — used everywhere
  [x] 8.4 Health check endpoint (/health, used by deploy.py probe)
  [ ] 8.5 ✅ Demo ลูกค้า
```

---

## 24. Demo Checklist

```
Core Flow — LINE OA Text (Chatbot):
[x] ค้นหาสินค้าจาก catalog SML ผ่าน LINE ✅
[x] เลือกสินค้า + ใส่จำนวน (รับ "3", "10 ถุง", "สิบถุง") ✅
[x] ราคา/หน่วยถูกต้อง (ถุง, เส้น, บาท) ✅
[x] ตะกร้าสะสมหลายรายการ ✅
[x] Checkout → ส่ง SML → bill created ✅ (BS20260423101501-UELM)
[x] SML fail → LINE แจ้ง admin ทันที ✅

Core Flow — ยังไม่ test:
[ ] ส่งรูป PO ใน LINE → บิลสร้างอัตโนมัติ
[ ] ส่ง PDF ใน LINE → extract ถูกต้อง
[ ] ส่ง voice message ใน LINE → สร้างบิล (F3)
[x] ลบรายการที่ N จาก cart ✅
[x] แก้จำนวนรายการที่ N เป็น Y ✅
[ ] Email มี PO attachment → บิลสร้างอัตโนมัติ ← Phase 5 กำลัง test
[ ] Upload Shopee Excel → bulk import สำเร็จ ← Phase 4a (รอ SML 224 เปิด)
[ ] Upload Lazada Excel → bulk import สำเร็จ ← Phase 4b

AI Features:
[ ] item map ไม่ได้ → confirm → ระบบเรียนรู้ (F1)
[ ] Learning Progress แสดง accuracy เพิ่มขึ้น (F1)
[ ] บิลราคาผิดปกติ → anomaly warning (F2)
[ ] บิลซ้ำ → block อัตโนมัติ (F2)
[ ] Dashboard แสดง AI Insights ภาษาไทย (F4)

System:
[x] SML retry 3 ครั้ง + LINE admin notify ✅
[x] Web UI แสดง bill list + detail + status ✅
[ ] SML fail → Web UI retry ได้
[ ] Login แต่ละ role → permission ถูกต้อง
[ ] pg_dump backup ทำงานได้
[ ] disk monitor แจ้ง admin ถ้า > 90%
```

---

## 25. คำสั่งแรกสำหรับ Claude Code ใน VSCode

```
Read CLAUDE.md completely.
Then execute Phase 0 on server 192.168.2.109:
1. Clear docker disk space (target < 70%)
2. Install Go 1.22
3. Setup cloudflared from ~/billflow
4. Verify SML API health
Report disk usage before and after, then ask before Phase 1.
```

---

---

## 26. Current Status (2026-04-28 — session 10)

```
Deployed & Running on 192.168.2.109:
  backend  → billflow-backend  :8090  ✅
  frontend → billflow-frontend :3010  ✅
  postgres → billflow-postgres :5438  ✅
  health:  {"database":"ok","env":"production","status":"ok"}

AI Models (OpenRouter):
  primary  → google/gemini-2.5-flash
  fallback → anthropic/claude-3-5-haiku
  audio    → openai/whisper-1
  embed    → openai/text-embedding-3-small  (1536-dim, in-memory cosine index)
  PDF      → Mistral OCR (mistral-ocr-2512) → markdown → ExtractText

SML servers (both confirmed working):
  SML #1 (LINE/Email JSON-RPC) — http://192.168.2.213:3248
    POST /api/sale_reserve   (line / email / lazada source)
  SML #2 (Shopee REST)        — http://192.168.2.248:8080
    POST /SMLJavaRESTService/v3/api/saleorder       (Shopee sale default)
    POST /SMLJavaRESTService/restapi/saleinvoice    (legacy/explicit endpoint)
    POST /SMLJavaRESTService/v3/api/purchaseorder   (Shopee shipped / pay-now email)
    POST /SMLJavaRESTService/v3/api/product         (create new product)
    GET  /SMLJavaRESTService/v3/api/product/{sku}   (product lookup)
    GET  /SMLJavaRESTService/v3/api/customer         (party master — 1004 records)
    GET  /SMLJavaRESTService/v3/api/supplier         (party master — 500 records)
  Config (SML #2): guid=smlx / SMLGOH / SMLConfigSMLGOH.xml / SML1_2026
                   wh=WH-01, shelf=SH-01
                   (cust_code now from channel_defaults table — not .env)

Multi-account IMAP (session 6 — BREAKING change from session 5):
  .env IMAP_* vars removed. Admin adds/edits inboxes via /settings/email.
  EmailCoordinator: one goroutine per enabled imap_accounts row.
  Active production inboxes:
    "Shopee Inbox"                  bos.catdog@gmail.com  channel=shopee
      subjects: คำสั่งซื้อ, ถูกจัดส่งแล้ว
    "Shopee Inbox (sutee — pay-now)" sutee.toe@gmail.com  channel=shopee
      subject: ยืนยันการชำระเงินคำสั่งซื้อหมายเลข #
  Both "ถูกจัดส่งแล้ว" AND "ยืนยันการชำระเงิน" → ShopeeShipped → purchaseorder

UI redesign (session 6):
  Tailwind 3 + shadcn/ui — Linear/Vercel aesthetic
  CSS bundle: 89 KB → 44 KB
  Dark mode: tokens in place (toggle UI shipped; production test pending)
  ⌘K command palette; two-key chord hotkeys (g d/b/i/s/m/l/c/x)
  BillDetail decomposed from 1234-line monolith → 14-file folder
  Sonner replaces react-hot-toast

PO label: "PO" → "ใบสั่งซื้อ/สั่งจอง" across all UI components

Shopee Excel artifact: original .xlsx archived per imported bill
  (pendingUploads sync.Map, SHA-256 key, 30-min TTL)

UTF-8 fix: all HTML artifacts now served with "text/html; charset=utf-8"
  Frontend Blob rebuilt with explicit type from Content-Type header

Channel defaults (session 7 — BREAKING change from session 6):
  .env SHOPEE_SML_CUST_CODE and SHIPPED_SML_CUST_CODE REMOVED from config.go.
  All 3 SML send routes now read party_code/party_name from channel_defaults table.
  Admin manages via /settings/channels (ChannelDefaults.tsx page, sidebar "ลูกค้า / ผู้ขาย").
  PartyCache: boots from SML 248 GET /v3/api/customer + /v3/api/supplier (all pages),
    refreshes every 6 hours; PartyPicker uses debounced search.
  Quick-setup button: auto-pairs AR00001-04 placeholder customers to channels.
  Production SML placeholder names (verified 2026-04-28):
    AR00001 "ลูกค้า จาก AI"     → (line, sale) or general fallback
    AR00002 "ลูกค้า จาก Line"   → (line, sale)
    AR00003 "ลูกค้า จาก Email"  → (email, sale)
    AR00004 "ลูกค้า จาก Shopee" → (shopee, sale)
  Behavior change for LINE: bills now link to AR00002 instead of AI-extracted name.
    Real customer info still in bills.raw_data.
  Per-channel WH/Shelf/VAT override (session 11): wh_code, shelf_code, vat_type, vat_rate
    in channel_defaults table overlay env per channel. Sentinel '' / -1 = use server .env.
    EditDialog exposes them inline (only shown for SML 248 endpoints — sale_reserve never
    consumed WH/VAT). GetConfig in shopee_import.go returns the overlaid values so the
    /import/shopee summary card mirrors what'll actually post.
  Deferred: Test-send (would create real SML docs); per-channel sale_code/branch override.
  Quick-create customer/supplier DROPPED (SML API requires ~25 fields / NPE).
  Shopee Import config dialog REMOVED (session 11): the dialog let users edit cust_code,
    doc_format, server_url, etc., but the Confirm handler only used unit_code as catalog
    fallback — every other field was a UI lie. Replaced with read-only summary + link.

Bill flow → SML routing (bills.go Retry handler — 4-way dispatch):
  source                   bill_type   endpoint (default)             cust_code source
  ──────────────────────   ─────────   ────────────────────────────   ─────────────────────────
  line / email / lazada    sale        sale_reserve (213)             contact_name from channel_defaults
  shopee / shopee_email    sale        saleorder (248) [was saleinvoice until session 7-10]
                                       override via channel_defaults.endpoint  party_code from channel_defaults
  shopee_shipped           purchase    purchaseorder (248)            party_code from channel_defaults
  any                      any         endpoint URL overridable per (channel,bill_type) in /settings/channels

Migrations applied (20 files):
  001_init.sql
  002_audit_logging.sql                    (audit_logs structured columns)
  002_sml_catalog.sql                      (sml_catalog + extended CHECK)
  003_channel_customer_defaults.sql        (legacy — renamed to _v1 by 007)
  004_shopee_shipped.sql                   (bills.source shopee_shipped)
  006_imap_accounts.sql                    (imap_accounts table — session 6)
  007_channel_defaults.sql                 (channel_defaults table — session 7)
  008_channel_defaults_doc_format.sql      (adds doc_format_code — session 7-10)
  009_channel_defaults_endpoint.sql        (adds endpoint column — session 7-10)
  010_channel_defaults_endpoint_freeform.sql (drops CHECK on endpoint — session 7-10)
  011_doc_no_format.sql                    (doc_prefix + doc_running_format + doc_counters table)
  012_channel_defaults_inventory.sql       (wh_code + shelf_code + vat_type + vat_rate — session 11)
  013_chat_inbox.sql                       (drop chat_sessions, add chat_conversations + chat_messages + chat_media — session 13)
  014_line_oa_accounts.sql                 (line_oa_accounts + chat_conversations.line_oa_id — session 13)
  015_chat_quick_replies.sql               (chat_quick_replies + 4 seed templates — Phase 4.4 session 13)
  016_chat_conversation_status.sql         (chat_conversations.status open/resolved/archived — Phase 4.2 session 14)
  017_chat_crm.sql                         (chat_conversations.phone + chat_notes + chat_tags + chat_conversation_tags — Phase 4.7+4.8+4.9 session 14)
  018_chat_reply_token.sql                 (chat_conversations.last_reply_token + last_reply_token_at + chat_messages.delivery_method — Hybrid Reply+Push session 15)
  019_line_oa_mark_as_read.sql             (line_oa_accounts.mark_as_read_enabled per-OA toggle for LINE Premium "อ่านแล้ว" — session 17)
  020_bill_remark.sql                         (bills.remark for admin-entered remark passed to SML purchaseorder)

Phases:
  Phase 0–7  ✅
  Phase 6    ✅ UI redesign (Tailwind/shadcn) + multi-account IMAP + artifacts + channel defaults +
              SML mojibake permanent fix via marshalASCII + catalog per-row actions (6.19-6.22)
              + LINE chatbot → human chat refactor + multi-OA + /messages page (6.23, session 13)
              + composer redesign + admin send-media (Cloudflare Quick Tunnel) + status/search/
                CRM lite (6.24, session 14)
              + hybrid Reply+Push API saving 200/mo quota (6.25, session 15)
              + audit log coverage + UX consistency + tag filter in inbox (6.26, session 16)
              + real-time SSE + connection indicator + LINE markAsRead +
                stale-token cron + pending cleanup (6.27, session 17)
              + UX polish — log preview + failure card + sidebar reorg +
                bill timeline + inline retry + dashboard action cards (6.28, session 18)
              + heuristic evaluation pass — labels SSOT + /settings live status
                + mobile responsive + 13 cross-page polish (6.29, session 19)
              + Send-to-SML validation guard + route preview chip + Cloudflare
                Quick Tunnel drift cron (6.30, session 20)
              + fresh-install setup UX — remove SeedFromEnv, test-connection card,
                ConfirmDialog before save/restart, locked fields hidden (6.31, session 21)
  Phase 8    ⏳ cloudflared named tunnel + systemd (need domain decision)

Pending (carry-over):
  ⏳ Phase 3 — test รูป/PDF/voice ใน LINE chat (auto-extract removed; manual extract works)
  ⏳ Phase 4b — Lazada Excel import (waiting customer files)
  ⏳ Phase 8 — cloudflared named tunnel + systemd (Quick Tunnel works for now)
  ⏳ Phase 4 carry-over: 4.6 LINE↔SML party link, 4.12 keyboard shortcuts (j/k/e/),
       4.13 mobile responsive, 4.14 profile refresh, 4.15 block/spam (overlap with archived)
  ⏳ LINE Push quota dashboard (free OA = 200/month — Reply API path is free)
  ⏳ Auto-discover Cloudflare URL from /tmp/billflow-tunnel.log (defer; admin paste works)

Recent work (session 21 — 2026-05-18):
  ⭐ feat(instance-settings): fresh-install setup UX — remove SeedFromEnv, test-connection, confirm dialogs
       Fresh installs now start with empty app_settings — admin fills all values
       via /settings/instance UI instead of having .env pre-seeded values appear
       automatically (SeedFromEnv() removed from main.go startup). This makes
       onboarding explicit: admin sees empty fields, fills them, saves.
       - Remove SeedFromEnv(): app_settings_repo.go had INSERT ... ON CONFLICT DO NOTHING
         that copied all .env values to DB on every boot — breaks fresh-install UX
         because admin never sees which values are "required" vs "optional"
       - TestConnection fallback for locked fields: sml.guid has Locked:true so it's
         never written to DB. TestConnection now reads from cfgFallback map (h.cfg.ShopeeSMLGUID)
         when DB value is empty — fixes "ยังไม่ได้ตั้งค่า guid" error on fresh installs
       - Fix false-positive pending_restart banner: Get handler compared locked field's
         empty DB value vs non-empty runtime value → triggered restart banner. Fixed by
         adding `&& !def.Locked` to the comparison condition
       - InstanceSettings.tsx UX improvements:
         - Test connection button → POST /api/settings/instance/test-connection →
           shows result card with SML/LINE/OpenRouter per-service status (CheckCircle2/XCircle)
         - ConfirmDialog before save (when changed keys are restart_required/secret) and
           before restart-only action — uses ConfirmDialog component
         - Locked fields now completely hidden from grouped filter (not just disabled)
           — prevents confusion from "API Key (guid)* ค่าตายตัว จำเป็นต้องกรอก" that admin can't fill
         - Missing required fields highlighted with red border + "จำเป็นต้องกรอกก่อนบันทึก"
       - MarketplaceAliases.tsx overflow fix: w-[35%] min-w-[240px] on product column header,
         max-w-[380px] on product cell, title= tooltips on all truncated text
       Verified: fresh DB (reset), SML ✅ LINE ✅ OpenRouter ✅ (credit: 2.54), all
       3 backend fixes deployed, UI hides locked fields correctly.
       Docker networking note: SML test connection requires http://172.24.0.1:8200 (Docker
       gateway IP), not localhost:8200, since backend runs inside Docker container.

Recent work (session 20 — 2026-04-30):
  ⭐ feat(bill): block Send-to-SML when items are invalid + route preview
       Prevents the most common admin error pattern: clicking
       "ส่งไปยัง SML" on a bill with unmapped items, hitting a generic
       SML rejection, ending up with a confusing "failed" bill that has
       to be debugged via /logs.
       Validation rules mirror backend retry handler + F2 anomaly rules:
       items.length ≥ 1, every item has non-empty item_code + unit_code,
       qty > 0, price > 0. New utility lib (validation.ts) lifted out so
       BillTotal + BillItemRow can both consume it.
       BillTotal: Send button disabled (wrapping span keeps tooltip
       firing despite raw <button disabled>). Inline warning card lists
       each issue grouped by kind ("3 รายการยังไม่ได้จับคู่") with a
       "ดู →" link per issue. Click → scroll + 1.5s flash on the first
       offending row.
       Per-row AlertCircle icon in a new w-6 status column. Hover surfaces
       the exact reason ("ยังไม่ได้ map · ขาด unit_code"). Editing-row
       variant gets an empty placeholder cell so column alignment stays
       stable.
       handleJumpToItem: null-then-id transition forces the row's
       useEffect to refire when admin clicks the same "ดู →" twice.
  ⭐ feat(bill): route preview chip surfacing SML routing decisions
       Backend GET /api/bills/:id wraps the bill in a JSON object that
       includes a preview field with channel/route/endpoint/doc_format
       resolved against live channel_defaults — same logic as the retry
       handler (resolveEndpoint + new mapSourceToChannel helper) so the
       chip shows what retry will actually do.
       Frontend chip below Send button: "↳ SML 248 · ใบสั่งขาย
       (saleorder) · doc_no BF-SO-#####". Catches:
         1. Misconfigured channel (e.g. shopee bill that would fall
            through to sale_reserve because endpoint string mismatch)
         2. Expected doc_no pattern, so admin can spot collisions
         3. Custom URL when admin set one in /settings/channels
       Bill type extended (BillRoutePreview interface) — preview only
       returned by single-bill GET, not list response.
  ⭐ fix(bill-detail): hoist hooks above early returns to fix React #310
       First version of the validation patch put useMemo AFTER the
       `if (loading) return <Skeleton />` early return. On first render
       loading=true → useMemo never called (hooks count = 1). On second
       render loading=false → useMemo called (hooks count = 2). React
       aborts with error #310 ("Rendered more hooks than during the
       previous render") and the page never renders.
       Fix: move useState + useMemo to the top of the component, before
       any early returns. validateForSML now tolerates bill=null with a
       no-op fallback so the call is safe pre-load. Added a code comment
       at the hook block explaining the rule for future contributors.
  ⭐ feat(jobs): daily Cloudflare Quick Tunnel drift monitor
       When `cloudflared --url http://localhost:8090` restarts (manually
       or machine reboot), trycloudflare.com hands out a new random URL.
       PUBLIC_BASE_URL in .env then points at nothing → admins keep
       sending images that LINE never delivers, with no warning until
       a customer asks why pictures are missing days later.
       jobs/tunnel_drift_monitor.go: cron at "0 2 * * *" UTC (= 9am
       Bangkok, off the hour to stagger from other crons) GETs
       $PUBLIC_BASE_URL/health and pushes a LINE admin alert when the
       request errors or returns non-200. Throttled to 1 push / 24h via
       sync.Mutex + lastAlerted. Recovery message inline-includes the
       exact 4-step shell pipeline so the recipient can act on it
       without digging through docs.
       Skips registration entirely when PUBLIC_BASE_URL is empty (dev
       env) rather than no-oping every tick.
       Why ping the public URL instead of reading /tmp/billflow-tunnel.log
       directly: log lives on the host, not inside the backend container;
       reading it would require a docker-compose volume mount. Pinging
       tests the end-to-end DNS → Cloudflare → tunnel → backend path
       which is what we actually care about.
       Smoke-test gotcha: initial verification used `curl -sI` (HEAD)
       which Gin's `r.GET("/health")` doesn't bind, returning 404. Cron
       uses http.MethodGet so it works correctly. Comment in code
       documents this for future debugging.

Recent work (session 19 — 2026-04-30):
  ⭐ ux: 3-sprint heuristic-evaluation pass — labels SSOT, /settings refactor, mobile, polish
       Goal: act on the audit, not just write it. Three parallel agents
       audited Bills/Dashboard/Logs + Chat/LINE + Settings/Imports/Catalog
       and reported 6 critical + 7 high-impact + 4 polish issues. Shipped
       all 16 fixes in a single session (some — like the Outlook+Shopee
       preset — turned out to already exist).
       Sprint A — Critical (5):
         A1 lib/labels.ts SSOT — central BILL_STATUS_LABEL / SOURCE /
         TYPE / PAGE_TITLE consumed by Bills, Dashboard, ActionCards,
         BillStatusBadge, Mappings, Catalog. Eliminated three different
         labels for the same status that drifted across pages.
         A2 /settings root rewrite — dropped duplicate user-info card
         (avatar dropdown already shows it) and duplicate system-stats
         card (Dashboard owns it). Backend GET /api/settings/status now
         returns line_oa_total/enabled + imap_total/enabled/failing
         instead of env-flag booleans. Frontend renders live subsystem
         rows with click-through to manage pages. Lazada column mapping
         relocated to /import as a collapsible card so the import
         workflow lives on one page.
         A3 Composer disabled visual + Messages mobile responsive —
         disabled composer now opacity-60 + dashed border + pointer-
         events-none (previously visually identical to enabled).
         /messages grid switches to single-pane below md breakpoint;
         thread header gets ArrowLeft "back to inbox" button (md:hidden).
         A4 Catalog ↔ Mappings explainer banners cross-link the
         two-stage matching pipeline so admins finally see the
         relationship.
         A5 Inline Retry on collapsed /logs sml_failed rows — Retry
         icon button now visible at row level (was hidden in expanded
         body). Refactored outer button → div role=button so nested
         Retry button is valid HTML.
       Sprint B — Workflow (7):
         B1 ShopeeImport preflight — blocks file picker + shows warning
         Alert when /api/settings/shopee-config has empty cust_code.
         B2 Mappings empty-state CTA links to /bills?status=needs_review.
         B3 ConversationList tag-filter popover hints where to attach
         tags (TagsBar in thread header).
         B4 Extract → CreateBill toast bridges the dialog swap.
         B5 Sidebar items show hint via native title= even in expanded
         mode (was collapsed-only tooltip).
         B6 BillDetail space-y-4 → space-y-6.
         B7 ChannelDefaults Quick Setup tooltip explains it's safe.
       Sprint C — Polish (4):
         C1 Outlook+Shopee preset verified already shipped.
         C2 Composer attachment-strip header "แนบไป N ไฟล์" + clear-all.
         C3 Catalog embedding card "Catalog ใหญ่ ใช้เวลาเป็นนาที — ปิด
         หน้านี้ได้".
         C4 Conversation thread-header timestamp uses relative time
         (dayjs.fromNow()) so admin sees data freshness; tooltip keeps
         the absolute time for debug.
       Backend changes:
         dashboard.go gains lineOARepo dep injection + multi-account
         live counts in SettingsStatus. Build green, deployed to prod
         109, /api/settings/status returns multi-account state.

Recent work (session 18 — 2026-04-30):
  ⭐ ux: 6-phase polish — log previews, structured failures, sidebar, timeline, retry, dashboard
       Goal: surface "admin work to do" where the eye lands first, and cut
       round-trips for common debug flows. No new features, all pure UX.
       Phase 1 — /logs preview shows what was actually said:
         backend line_message_received + line_admin_reply detail now include
         text_preview (rune-aware 100 chars); media events get filename +
         size_bytes. Frontend summarize() renders quoted text and "filename
         (123 KB)". Reply/Push chip on outgoing rows shows delivery method
         without expanding. No more raw message_ids.
       Phase 2 — Bill SML failure card on BillDetail:
         bills.go recordFailure now takes a doc_no_attempted argument and
         persists error_msg as JSON {route, doc_no_attempted, error,
         occurred_at}. Backwards-compat: frontend tries JSON.parse, falls
         back to plain string for legacy rows. New BillFailureCard component
         with AlertCircle header + route badge (SaleOrder / SaleInvoice /
         PurchaseOrder / SaleReserve) + monospace pre block + copy button
         that writes a multi-line block (route + doc_no + timestamp + error)
         tailored for sending to dev. Removes the old inline red text from
         BillHeader so the error has room to breathe.
       Phase 3 — Sidebar reorg into 5 domain groups:
         Old "จัดการระบบ" group had 9 mixed items (mappings + email + chat
         configs + parties + catalog + logs + settings). New grouping by
         daily-frequency:
           ภาพรวม         → Dashboard, ประวัติการทำงาน
           บิลขาย/ซื้อ     → บิลทั้งหมด + Lazada + Shopee imports
           แชทลูกค้า      → Inbox + LINE OA + Quick Replies + Chat Tags
           ข้อมูลตั้งต้น   → Mappings + Catalog + Channel defaults
           ตั้งค่าระบบ     → Email Inboxes + General settings
         Labels are Thai-first with new optional `hint` field shown in the
         collapsed-mode tooltip (English/setup name) so admin/devs can map
         Thai labels back to underlying features.
       Phase 4 — Bill Timeline on BillDetail:
         GET /api/bills/:id/timeline → audit_logs WHERE target_id ASC,
         capped 200. New BillTimeline component renders a vertical rail
         with tone-colored dots, action label + emoji + relative time, and
         optional summary. Reuses ACTION_META + summarize() — extracted to
         lib/audit-log-meta.ts so /logs and the timeline stay in sync.
         Answers "ทำไมบิลนี้ถึงเป็นแบบนี้" without leaving the page.
       Phase 5 — Inline Retry on /logs sml_failed rows:
         Expanded row shows "🔄 Retry บิลนี้" button next to the error
         label. POSTs to /api/bills/:id/retry, toasts the result, refreshes
         the current page of logs. Saves the round-trip through /bills/:id.
       Phase 6 — Dashboard "ต้อง action" widget:
         Backend extends /api/dashboard/stats with email_inbox_errors
         (count of enabled imap_accounts with consecutive_failures > 0)
         via new ImapAccountRepo.CountFailing.
         Frontend ActionCards row at top of Dashboard: 4 click-through
         cards (บิลรอตรวจ / บิลล้มเหลว / ข้อความใหม่ / Email มีปัญหา).
         Failed + email-error cards get urgent accent (red number + pulsing
         dot) when count > 0; quiet cards (count=0) fade muted.
         Bills.tsx reads ?status=/?source=/?bill_type= from URL on mount
         so the dashboard shortcuts land pre-filtered.
       UI polish standards applied throughout:
         - Lucide icons only in main UI (emoji reserved for /logs ACTION_META
           semantic markers)
         - HSL token colors throughout (bg-destructive/[0.03], text-success,
           border-border) — never raw colors
         - Typography hierarchy: text-[10px] meta → text-[11px] hint →
           text-xs body → text-sm label → text-2xl count
         - tabular-nums on every count
         - animate-pulse only on urgent dots
         - micro-interactions: hover:-translate-y-0.5 on ActionCards +
           ArrowUpRight that translates on hover
       Verified end-to-end on prod 109: timeline endpoint returns events,
       stats endpoint returns email_inbox_errors, all 6 phases visible.

Recent work (session 17 — 2026-04-29):
  ⭐ chat: real-time inbox via SSE + production-grade polish
       Replaces aggressive polling (5s thread / 30s inbox / 30s sidebar)
       with Server-Sent Events for sub-second admin updates. Polling kept
       at 30/60s as safety net.
       Backend: services/events/broker.go in-process pubsub (sync.RWMutex
       + buffered channels). handlers/sse.go SSE stream with 20s heartbeat
       + X-Accel-Buffering=no. Token via POST /api/admin/events/token
       (JWT-required) reuses media.Signer with subject=adminUserID, 5min
       TTL. Stream itself outside JWT group — token IS the auth.
       Publish points: webhook inbound (MessageReceived + UnreadChanged),
       SendReply/SendMedia (MessageReceived), SetStatus/SetPhone/MarkRead
       (ConversationUpdated), MarkRead (UnreadChanged), tags
       SetTagsForConversation (ConversationUpdated).
       Migration 019: line_oa_accounts.mark_as_read_enabled — per-OA opt-in
       for LINE Premium markAsRead API (Free OA returns 403). Wired through
       repo + AccountDialog UI checkbox with "OA Plus only" warning.
       service.go MarkMessagesAsRead(userID) — best-effort POST /v2/bot/
       message/markAsRead from MarkRead handler when OA has it enabled.
       jobs/reply_token_cleanup.go hourly cron clears reply tokens >1h.
       Startup SQL flips chat_messages outgoing pending >5min → 'failed'
       (recovers from server crashes leaving "กำลังส่ง…" forever).
       Frontend: lib/events-store.ts Zustand singleton EventSource (one
       per tab regardless of subscribers). Reconnect with exponential
       backoff [3,6,12,20,30]s; after 5 failures → status='offline' and
       polling fallback owns updates. hooks/useChatEvents.ts typed
       subscription. Layout.tsx connects on mount, disconnects on unmount.
       BUG FIX MessageThread useEffect: was listing fetchInitial+fetchDelta
       in deps, causing re-runs every parent render (ConversationList
       polling 30s → new conversation reference → fetchDelta useCallback
       rebuilds → useEffect re-fires → mark-read spammed every 30s and
       on every search keystroke). Split into 2 effects keyed only on
       lineUserID + ref pattern for fetchDelta. ~95% fewer API calls.
       MessageThread polling 5s → 30s safety net. ConversationList
       30s → 60s. Sidebar 30s → 60s. SSE drives real-time.
       MessageBubble subscribes via useChatEvents.onMessage. Self-tab
       dedup: 3-way logic in onSSEMessage —
         1. Real id already present → skip
         2. Outgoing matches optimistic tmp- row by kind+content (text)
            or kind+filename (image) → REPLACE tmp instead of adding
         3. Otherwise → append (genuinely new)
       Fixes the "2 bubbles until refresh" race between SSE event +
       HTTP response. Defensive dedup by id added to fetchDelta too.
       ConversationList subscribes to onMessage + onConvUpdated → refetch
       inbox (cheap query, simpler than patching rows).
       Sidebar subscribes to onUnreadChanged for instant badge updates.
       Adds ConnectionDot — pulsing dot reading from events-store with
       Thai tooltip explaining each state (live/reconnecting/offline).
       Visible in collapsed + expanded sidebar.
       Verified end-to-end on prod 109: SSE token round-trip works,
       hello event arrives, heartbeats every 20s.

Recent work (session 16 — 2026-04-29):
  ⭐ chat: audit log coverage + UX consistency + tag filter in inbox
       Phase 1 — Audit log gaps closed: 11 chat metadata endpoints
       (chat_notes 3, chat_tags 4, chat_quick_reply 3, chat_phone_saved 1)
       now write to audit_logs. auditRepo wired into ChatNotesHandler,
       ChatTagsHandler, ChatQuickReplyHandler via main.go constructors.
       Body/label snapshots captured BEFORE delete (ListAll → find by ID →
       cache body/label/color → repo.Delete → audit with snapshot in
       detail) so /logs preserves what disappeared even after row gone.
       MarkRead intentionally NOT logged — fires every thread open, would
       drown out signal in /logs.
       Logs.tsx: 17 new ACTION_META entries (line_admin_reply,
       line_admin_send_media, line_conversation_status, line_message_received,
       line_oa_*, chat_phone_saved, chat_note_*, chat_tag_*, chat_conv_tags_set,
       chat_quick_reply_*) with Thai labels + emoji + tone. summarize() per
       action: reply method shows "ฟรี (Reply API)" / "Push (นับ quota)";
       chat_conv_tags_set shows resulting tag labels list; deletions use
       body_preview / label snapshots from audit detail.
       Phase 2 — UX consistency:
         - MessageThread renders banner above Composer when conversation.
           status='archived' with inline "↺ เปิดอีกครั้ง" button. Composer
           prop disabled=true → textarea + send + attach all disabled.
           Sound vs Archive semantics (= spam/blocked).
         - CreateBillPanel both paths (with-extract + without-extract) now
           use `prefill?.customer_phone ?? conversation?.phone ?? ''` so
           admin doesn't retype a phone they already saved via the
           "บันทึกเบอร์" button.
         - ChatTags page description was misleading ("ใช้ filter ใน
           /messages" — filter didn't exist). Updated to match Phase 3 ship.
       Phase 3 — Tag filter in inbox (lifted from session 14 deferral):
         - Backend: ConversationListFilter.TagIDs []string + EXISTS subquery
           against chat_conversation_tags (ANY-match — at least one matches).
           ListConversations parses ?tags=id1,id2,id3 (comma-sep UUIDs).
           CountAll mirrors filter logic for pagination accuracy.
         - Frontend: ConversationList fetches /api/settings/chat-tags once
           on mount. "🏷 Tag" Popover button with multi-select + count badge,
           chip row beneath search bar showing selected tags w/ × remove.
           Disabled when no tags exist (with hint to /settings/chat-tags).
       Verified deploy on 109; backend healthy, no migration needed
       (Phase 1+3 are pure code changes on existing schema).

Recent work (session 15 — 2026-04-29):
  ⭐ feat(line): hybrid Reply+Push API to save 200/mo push quota
       LINE Reply API ฟรีไม่นับ quota (verified docs: "Sending methods that
       are not counted as message count: Reply messages"). Free OA = 200
       push/month (Light Plan; old CLAUDE.md "500/mo" was wrong — fixed).
       Migration 018 adds chat_conversations.last_reply_token + _at and
       chat_messages.delivery_method ('reply' | 'push' default 'push').
       Webhook caching (line.go.processMessage): cache event.ReplyToken
       AFTER greeting check, skip when:
         - deliveryContext.isRedelivery=true (token may be stale)
         - greeting reply consumed token (greetingSent=true)
       Atomic consume (chat_conversation_repo.ConsumeReplyToken): CTE pattern
         WITH cur AS (SELECT ... FOR UPDATE),
              upd AS (UPDATE ... RETURNING 1)
         SELECT FROM cur
       so two admins replying simultaneously can't both consume the same
       token — SELECT FOR UPDATE serializes; second tx sees empty column.
       Send flow (chat_inbox.sendOutgoingText/sendOutgoingImage):
         token := convRepo.ConsumeReplyToken(userID)
         if token != "" {
           err := svc.ReplyText/ReplyImage(token, ...)
           if err == nil → method='reply', done
           if lineservice.IsReplyTokenError(err) → fallback to Push
           else (auth/429/network) → fail without push (don't burn quota)
         }
         svc.PushText/PushImage → method='push'
       lineservice.IsReplyTokenError: substring match on err.Error() for
       "reply token" — permissive (LINE may change wording). Does NOT
       match 401 (auth) or 429 (rate limit) — those errors mean Push will
       also fail or burn quota for nothing.
       UI: outgoing bubble "ฟรี" (success-tinted, delivery_method='reply')
       or "Push" (muted, delivery_method='push') with tooltips explaining
       quota impact.
       Verified end-to-end on prod 109: schema migrated, code compiles,
       health check green. Real Reply path will trigger after next inbound.

Recent work (session 14 — 2026-04-29):
  ⭐ feat(chat): composer redesign + admin send-media + Cloudflare Quick Tunnel
       Phase A — Composer.tsx full rewrite. Replaced 3 hardcoded h-[60px]
       elements with single rounded-2xl wrapper (focus-within ring). Auto-grow
       textarea via useLayoutEffect + scrollHeight (1→6 rows max, scrolls past).
       📎 + 💬 toolbar (h-8 w-8 ghost) on left, send button collapses to icon
       when empty / expands h-8 px-3 with "ส่ง" label when canSend. Added
       PendingAttachment[] state, thumbnail strip with × remove, onPaste handler.
       Phase B — admin send-media (image only, JPEG/PNG/WebP, ≤10MB):
       New backend/internal/services/media/signer.go — HMAC-SHA256 signed URL
       (exp_unix.sig in base64url, 1h default TTL, constant-time verify).
       Falls back from MEDIA_SIGNING_KEY → JWT_SECRET when key empty.
       New handlers/public_media.go — GET /public/media/:id?t=<token> outside
       /api JWT group; token IS the auth (LINE servers fetch this URL).
       Cache-Control: public, max-age=300.
       chat_inbox.SendMedia: multipart upload → save chat_media → insert
       outgoing chat_message kind=image → build `cfg.PublicBaseURL +
       "/public/media/<id>?t=<sig>"` → svc.PushImage(originalURL, previewURL).
       lineservice.PushImage(userID, originalContentURL, previewImageURL).
       Frontend: drag-drop overlay on MessageThread (dragCounterRef for nested
       child enter/leave); optimistic UI via _localPreviewURL blob URL on the
       outgoing ChatMessage; URL.revokeObjectURL on cleanup; MessageBubble
       checks localPreview first before falling back to API URL pattern.
       Cloudflare Quick Tunnel: discovered already running on host 109
       (PID 2265016, --url http://localhost:8090, log /tmp/billflow-tunnel.log,
       URL https://recorders-thinks-distance-injuries.trycloudflare.com).
       v1 = admin pastes URL into PUBLIC_BASE_URL .env (re-paste only on
       cloudflared restart — current uptime 6+ days, log persists).
       LIMITATION: LINE Push has no `file` type — text/image/video/audio/
       sticker/template/flex only. v1 = image only. PDF/file outbound deferred
       (Flex Message link workaround in v2).
  feat(chat): conversation status (Phase 4.2)
       Migration 016 adds chat_conversations.status (open/resolved/archived)
       with default 'open' + index on (status, last_message_at DESC).
       New chat_conversation_repo.SetStatus + AutoReviveOnInbound
       (UPDATE WHERE status='resolved' — archived sticky).
       handlers/line.go.processMessage: after IncrementUnread, calls
       AutoReviveOnInbound to flip resolved→open on new inbound.
       New PATCH /api/admin/conversations/:user/status endpoint.
       Frontend: ConversationList tab strip (เปิดอยู่ default / ปิดแล้ว /
       Archive); MessageThread header status badges + actions
       (✓ ปิดเรื่อง / ↺ เปิดอีกครั้ง / Archive / unarchive).
       Refetches list on tab change; pauses delta polling unrelated.
  feat(chat): inbox + thread search (Phase 4.3)
       No schema. ConversationListFilter.Q → ILIKE on display_name +
       EXISTS subquery against chat_messages.text_content. Inbox search
       moves from client-side filter → server-side ?q=. Thread search:
       chat_message_repo.ListByUser accepts q → ILIKE branch (renamed local
       var query to avoid collision with parameter); MessageThread search
       bar collapsible above message list, 250ms debounce, pauses delta
       polling while q != ''. v1 ILIKE only — pg_trgm GIN deferred (volume
       low).
  feat(chat): CRM lite — phone + notes + tags (Phase 4.7+4.8+4.9)
       Migration 017 adds chat_conversations.phone (TEXT NOT NULL DEFAULT '')
       + chat_notes table + chat_tags table (color enum hint) +
       chat_conversation_tags m2m (PK on (line_user_id, tag_id)).
       4.7 Phone — MessageBubble PHONE_RE matches Thai phone in incoming
            text bubbles only → "บันทึกเบอร์ <num>" button → PATCH .../phone.
            Outgoing/system bubbles never get the button (admin replies
            obviously aren't customer phones).
       4.8 Notes — chat_note_repo.go (ListByUser/Create/Update/Delete) +
            handlers/chat_notes.go CRUD endpoints. NotesPanel.tsx
            collapsible warning-tinted bar; auto-hides when empty + closed.
            Internal-only — never sent to LINE.
       4.9 Tags — chat_tag_repo.go (global CRUD + TagsForConversation +
            SetTagsForConversation tx-based replace-set) +
            handlers/chat_tags.go (global + per-conv m2m).
            TagsBar.tsx chips + popover picker, 7 colors (gray/red/orange/
            yellow/green/blue/purple). New /settings/chat-tags admin page
            (ChatTags.tsx) with color picker grid + "ดูตัวอย่าง" preview.
            Sidebar nav: "Chat Tags" (Tag icon).
  Verified end-to-end on prod 109:
       - Composer auto-grow + paste image preview + drag-drop work
       - HMAC token round-trip: GET /public/media/<id>?t=<sig> → 200 + bytes;
         tampered token → 403
       - Status flip + auto-revive on inbound (tested resolved→open and
         archived stays sticky)
       - Server search returns matching convos by display_name OR text
       - Notes CRUD + tags m2m via /settings/chat-tags
       Migration 016 + 017 idempotent (IF NOT EXISTS / ALTER TABLE ADD COLUMN
       IF NOT EXISTS) so re-runs safe.

Recent work (session 13 — 2026-04-29):
  ⭐ feat(line): replace AI chatbot with human chat inbox + multi-OA support
       Removes ~900 LOC of chatbot path from line.go (ChatSales, pending order
       cart, regex-based delete/edit, MCP product search). Replaces with simple
       store-incoming-message + ack flow. Optional first-contact greeting via
       LINE_GREETING env (per-OA in line_oa_accounts).
       New schema (migration 013): chat_conversations + chat_messages + chat_media.
       Drops legacy chat_sessions (24h prune meant no long-lived data anyway).
       New schema (migration 014): line_oa_accounts table + chat_conversations.line_oa_id.
       Multi-OA: 1 BillFlow ↔ N LINE OAs (e.g. chain with 5 stores). Webhook URL
       per OA: /webhook/line/<oa_id>. LineRegistry maps oa_id → service instance
       so reply uses correct access_token.
       Backend: chat_inbox.go handler with 8 endpoints (list, messages, reply,
       mark-read, media download, AI extract from media, create-bill-from-chat,
       unread-count). PushText + GetProfile added to lineservice.
       Frontend: /messages page (3-pane: list, thread, composer). CreateBillPanel
       with catalog picker. ExtractPreviewDialog for manual AI on media. Sidebar
       nav "ข้อความลูกค้า" + unread badge. Polling: 30s inbox, 5s active thread,
       paused when tab hidden.
       /settings/line-oa CRUD page (new) — admin manages OAs (secret + token +
       greeting + enabled). Test-connect button verifies token via /v2/bot/info.
       Default OA seeded from existing LINE_* env vars on first boot.
       Removed: ChatSales / ChatSalesWithContext / ExtractOrderFromHistory from
       openrouter.go. SalesSystemPrompt from prompts.go. chat_session_repo.go
       deleted. MCP client construction removed from main.go (code kept in
       services/sml/mcp.go in case future flows need it).
       Verified end-to-end: customer text → BillFlow inbox → admin reply → LINE
       Push → customer receives. เปิดบิลขาย → /bills/:id pending → Retry → SML
       213 sale_reserve → bill 59d20f8a... created with Thai display correct.
       Polling consolidation: dashboard/stats now returns unread_messages too,
       so Sidebar uses 1 endpoint instead of 2. Polling pauses when tab hidden,
       refreshes immediately on regain focus.
  feat(line-oa): multi-OA support — 1 BillFlow ↔ N LINE OAs
       Migration 014 adds line_oa_accounts table + chat_conversations.line_oa_id.
       New `lineservice.Registry` maps oa_id → *Service so each push uses the
       right access_token. Webhook URL per OA: /webhook/line/<id>. Default OA
       seeded from .env LINE_* on first boot (idempotent — only when table empty).
       /settings/line-oa CRUD page + Test button (calls /v2/bot/info to verify
       token + cache bot_user_id for webhook routing-by-Destination fallback).
       ConversationList shows OA badge per row.
       Verified end-to-end: POST /api/settings/line-oa/<id>/test returns
       basic_id="@027faszn", bot_user_id="Ub35fdd84..." display_name="ผู้ช่วย
       ฝ่ายขาย" — token valid + caching works.
  feat(chat): Phase 4 quick wins (4.4 + 4.5 + 4.11)
       4.4 Quick replies — migration 015 + 4 seed templates (ทักทาย / เช็คสต๊อก /
            แจ้งราคา / ปิดบิล). Composer 💬 popover lazy-fetches templates,
            click → injects body into textarea. Admin CRUD via /api/admin/quick-replies.
       4.5 Customer history panel — collapsible under thread header. Shows last
            10 bills tied to this LINE userID via raw_data->>'line_user_id'.
            Click row → /bills/:id. Verified: existing convo "บอส เฉย ๆ" shows
            BS20260429070949-ZPWE bill correctly.
       4.11 Browser notification + audio chime — useNotifications hook with
            persisted toggle (localStorage). Notification API only fires when
            tab is hidden (avoids double-notify with in-app toast). WebAudio
            two-note chime synthesized inline (no asset files needed).
            🔔/🔕 toggle button in thread header.

Recent work (session 12 — 2026-04-29):
  ⭐ fix(sml): permanent Thai-display fix via marshalASCII helper
       Discovered SML 248 Java backend reads request body as Latin-1 ALWAYS,
       ignoring Content-Type charset. The session 7-10 "fix" via charset=utf-8
       header was confirmation bias — verified mojibake persists with both
       'application/json' and 'application/json; charset=utf-8'. Real fix:
       new helper backend/internal/services/sml/json_ascii.go that escapes
       non-ASCII as \uXXXX (and surrogate pairs for non-BMP). Body becomes
       pure ASCII → identical bytes in Latin-1 vs UTF-8 → SML's JSON parser
       unescapes ส → "ส" before server code sees the string. Applied in
       all 6 POST clients (saleorder, saleinvoice, purchaseorder, product,
       sale_reserve, MCP). bills.sml_payload + audit_logs still use
       json.Marshal (UTF-8) for human-readable storage — only wire bytes
       changed. Includes unit tests in json_ascii_test.go.
       Verified end-to-end: created TEST-THAI-001 via /api/catalog/products
       → SML master stored Thai correctly → mapped in bill 13cddb09… → SML
       saleorder doc displays Thai correctly.
       Older HENNA001-style master records that were created with mojibake
       must be deleted + re-created in SML directly (BillFlow can't fix them
       in-place because update would also go through old corrupt path before
       this session).
  feat(catalog): per-row Refresh + Delete actions on /settings/catalog
       POST /api/catalog/:code/refresh — GET single product from SML 248,
       upsert (preserve price; v3 single-product endpoint doesn't return prices),
       reload memory index. SML returning {"data":null} → 404 + not_found:true,
       UI prompts admin to "ลบจาก BillFlow ได้".
       DELETE /api/catalog/:code — local-only delete (SML untouched). Use to
       prune zombies left after admin deletes products in SML directly.
       UI: 🔄/🗑️ buttons per row, ConfirmDialog before delete, busyRow tracker
       for spinners. Action column widened 80px → 200px.
  ui(import): /import/shopee config dialog REMOVED (was misleading — only
       unit_code had real effect). Replaced with read-only summary card linking
       to /settings/channels.
  Audit (session 12 close): verified all 6 SML POST clients use marshalASCII;
       no stale references to removed env vars (SHOPEE_SML_CUST_CODE etc);
       all 4 retry paths in bills.go apply channel_defaults party_code +
       applyChannelOverrides + resolveDocNo + def.DocFormatCode consistently;
       no orphan frontend pages. System is consistent end-to-end.

Recent work (session 11 — 2026-04-28):
  feat(channels): per-channel WH/Shelf/VAT override (migration 012)
       channel_defaults gains wh_code, shelf_code, vat_type, vat_rate; sentinel '' / -1
       falls back to server .env. bills.go applyChannelOverlay() applies on saleinvoice +
       saleorder + purchaseorder retry paths. shopee_import.go GetConfig also overlays
       so /import/shopee summary mirrors actual SML payload.
       EditDialog: 4 new fields in a "คลัง / ภาษี" group, only rendered for SML 248
       endpoints (sale_reserve never consumed WH/VAT).
  fix(ui): EditDialog body now scrolls — DialogContent gets max-h-[90vh] +
       grid-rows-[auto_minmax(0,1fr)_auto], body div gets overflow-y-auto.
  ui(channels): row action button shows "แก้ไข" / "ตั้งค่า" text instead of icon-only;
       unset rows highlight default-styled (call to action).
  feat(import): Shopee Import config dialog removed — only unit_code had any effect
       on Confirm. Replaced with read-only summary card + link to /settings/channels
       (single source of truth). Drops ~135 lines of misleading UI.
  Verified end-to-end via curl: PUT override → GET round-trip → /api/settings/shopee-config
       returns overlaid values (WH-99/SH-99/vat_type=1/vat_rate=7.5). Reset to sentinels
       falls back to env (WH-01/SH-01/0/7).
  NOT verified: actual SML payload on Retry — no test bill in DB at session end.

Recent commits (session 7-10):
  feat: per-channel cust/supplier defaults from SML — /settings/channels + party cache + Quick-setup
  feat: saleorder_client.go — POST /v3/api/saleorder (new default for Shopee email flow)
  feat: 4-way Retry dispatch in bills.go (saleorder / saleinvoice / purchaseorder / sale_reserve)
  feat: doc_counter_repo — GenerateDocNo atomic YYMM#### counter (avoids SML doc_no UI bug)
  feat: channel_defaults.endpoint + doc_prefix + doc_running_format (migrations 008-011)
  fix: SML UTF-8 charset — Content-Type: application/json; charset=utf-8 on all 6 POST clients
       (sale_reserve, saleinvoice, saleorder, purchaseorder, product, MCP — covers product create
       legacy LINE flow, not just Shopee retry which was caught earlier)
       ⚠️ SUPERSEDED in session 12: charset header is ignored by SML 248. Real fix is
       marshalASCII (escape non-ASCII as \uXXXX). Header is kept for hygiene but does nothing.
  fix: resolveItemName — looks up sml_catalog.item_name before falling back to raw_name
  fix: IMAP isShopeeFrom — shopee_domains entries no longer have @ prepended (fixed full-email match)
  ui: /logs redesign — 15 action labels Thai+emoji, 1-line summary, date groups, stats bar, row expand
  ui: /settings/channels EditDialog — endpoint URL, doc_format, prefix, running format + live preview
  ui: validation warning on "prefix-YY..." SML bug pattern in EditDialog

Recent commits (session 6):
  0819450 ui: sync 'PO' wording across FLOW_META + source labels
  6ac1e8d ui: friendlier email-inbox setup + Thai 'ใบสั่งซื้อ/สั่งจอง' label
  1fcf2ba fix(artifact): force Blob type to include charset=utf-8 on preview
  e72eab5 fix(artifact): force UTF-8 charset on text artifacts so Thai renders
  082f86f feat(imap): route Shopee 'ยืนยันการชำระเงิน' to ShopeeShipped flow
  8df8d23 ui(email): inline App Password help popover next to password field
  9bada07 scripts: deploy.py also wipes orphan email_poller.go after IMAP refactor
  b0c77ef feat(imap): multi-account email config with admin UI
  faf906d feat: archive Shopee Excel as artifact for every imported bill
  decb685 scripts: deploy.py also wipes orphan BillDetail.tsx after decomposition
```

---

*Last updated: 2026-05-18 (session 21)*
*Server: 192.168.2.109 | Project: billflow | Folder: ~/billflow*
*Ports: backend:8090 / frontend:3010 / postgres:5438*
*⚠️ LINE credentials ต้อง reissue ก่อนใช้ทุกครั้ง*
