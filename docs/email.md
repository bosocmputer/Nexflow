# Email IMAP — การทำงานของ Email Pipeline

> อัพเดตล่าสุด: 2026-05-06
> สถานะ: ✅ multi-account IMAP deployed; config อยู่ใน `/settings/email` และ `imap_accounts` table

---

## ภาพรวม

Nexflow polls Gmail/Outlook/IMAP อื่นตาม inbox ที่ admin เพิ่มใน `/settings/email` เพื่อตรวจหา email ใหม่ที่มี attachment
เมื่อพบ → ส่ง AI อ่าน → map รหัสสินค้า → ส่งสร้างบิลใน SML โดยอัตโนมัติ

---

## Flow ทั้งหมด

```
┌────────────────────────────────────────────────────────────┐
│  Background: EmailCoordinator                                │
│                                                            │
│  ► one goroutine per enabled imap_accounts row             │
│  ► poll ทุก poll_interval_seconds (ขั้นต่ำ 300 วินาที)    │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ▼
        IMAP.Poll(account) — connect → mailbox TLS
                 │
                 ▼
        SELECT INBOX WHERE UNSEEN
        (ค้นหา email ที่ยังไม่อ่าน)
                 │
          ┌──────┴──────┐
          │ ไม่มี email  │  → disconnect → รอ poll ถัดไป
          └─────────────┘
                 │
          มี email UNSEEN
                 │
                 ▼
        ┌ Loop ทุก message ┐
        │                  │
        │  filter ตาม account config:
        │  - filter_from
        │  - filter_subjects[]
        │  - channel: general / shopee / lazada
        │  - shopee_domains[] สำหรับ Shopee routing
        │  ถ้าไม่ผ่าน filter → ข้ามไป
        │                  │
        │  parse email body → หา attachments
        │  รองรับ:
        │    application/pdf (AttachmentHeader หรือ InlineHeader)
        │    image/jpeg, image/png
        │    application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
        │                  │
        │  ┌ Loop ทุก attachment ┐
        │  │                     │
        │  │  ProcessAttachment(data, mimeType, filename)
        │  │     │
        │  │     ▼
        │  │  ถ้า PDF → Mistral OCR → markdown text
        │  │  ถ้า รูป → ส่ง Gemini โดยตรง (base64)
        │  │     │
        │  │     ▼
        │  │  AI Extract (Gemini 2.5 Flash)
        │  │  → {customer_name, items[{raw_name, qty, unit, price}], confidence}
        │  │     │
        │  │     ▼
        │  │  F1 Mapper: raw_name → item_code/unit_code
        │  │     │
        │  │     ▼
        │  │  F2 Anomaly: ตรวจราคา, qty, ซ้ำ
        │  │     │
        │  │     ▼
        │  │  Save bill + items → PostgreSQL
        │  │     │
        │  │     ├── allMapped AND confidence ≥ 0.85 AND ไม่มี block
        │  │     │         │
        │  │     │         ▼
        │  │     │   SML CreateSaleReserve
        │  │     │   → success: status = 'sent', doc_no = BS...
        │  │     │   → fail:    status = 'failed' + LINE admin notify ⚠️
        │  │     │
        │  │     └── ไม่ผ่าน condition
        │  │               │
        │  │               ▼
        │  │         status = 'pending'
        │  │         LINE admin notify 📋
        │  │
        │  └────────────────────┘
        │
        │  mark email เป็น SEEN (อ่านแล้ว)
        │  ← เฉพาะเมื่อ process สำเร็จ
        │        │  dedup check: Message-ID
        │  SELECT COUNT(*) FROM bills WHERE raw_data->>'message_id' = ?
        │  ← ป้องกัน process ซ้ำ ถ้า email ถูก mark unread โดยไม่ตั้งใจ
        │        └───────────────────────┘
                 │
                 ▼
        disconnect IMAP
```

---

## คำถามที่พบบ่อย

### ถ้า mark email กลับเป็น unread แล้วรอ 5 นาที จะส่ง SML ได้เลยไหม?

**ได้เลย** — IMAP poller ค้นหา `UNSEEN` (unread) messages
ถ้า mark กลับเป็น unread → email กลายเป็น UNSEEN → poll ถัดไป (ภายใน 5 นาที) จะ pick up ใหม่

```
timeline:
  14:00  ← email ถูก process → mark SEEN → bill = pending (unmapped)
  14:05  ← poll: ไม่เจออะไร (email SEEN อยู่)
  14:10  ← พนักงานเพิ่ม mapping ใน /mappings
  14:10  ← mark email กลับเป็น UNREAD
  14:15  ← poll: เจอ UNSEEN → process ใหม่ → allMapped = true → SML ✅
```

> **หรือใช้ Retry Handler แทน** (ไม่ต้อง unread email):
> `POST /api/bills/:id/retry` → re-map items ด้วย mapping ใหม่ → ส่ง SML ทันที

---

### ถ้าเพิ่ม mapping แล้ว ต้องรอ 5 นาทีไหม?

ไม่ต้องรอ — ใช้ **retry handler** แทน:
1. เปิด Web UI → `/bills` → เลือก bill ที่ pending
2. กด **Retry** → ระบบ re-map ด้วย mapping ใหม่ → ส่ง SML ทันที

---

### poll ถี่ได้ไหม?

Gmail มี rate limit — ถ้า poll ถี่กว่า 5 นาที จะเกิด `unexpected EOF`
ระบบบังคับ `poll_interval_seconds >= 300` ใน DB

---

## PDF ทำงานยังไง (Mistral OCR)

Gmail ส่ง PDF บางฉบับเป็น `Content-Disposition: inline` (ไม่ใช่ attachment)
Nexflow รองรับทั้ง 2 กรณี:

```
Email → Part header
  AttachmentHeader (Content-Disposition: attachment)  → ดาวน์โหลด
  InlineHeader     (Content-Disposition: inline)      → ดาวน์โหลด (ถ้าเป็น PDF หรือรูป)
```

หลัง download PDF:
```
PDF bytes (base64)
    │
    ▼
Mistral OCR API (mistral-ocr-2512)
    │
    ▼
Markdown text (ข้อความจากทุกหน้า)
    │
    ▼
Gemini ExtractText(markdownText)
    │
    ▼
{customer_name, items, confidence} JSON
```

เหตุผลที่ใช้ Mistral OCR แทน Gemini PDF:
OpenRouter route Gemini ผ่าน Amazon Bedrock → ไม่รองรับ `application/pdf` MIME type โดยตรง

---

## IMAP Authentication

ใช้ **SASL PLAIN** (ไม่ใช่ `Login` command ธรรมดา)

```go
// go-imap/v2 beta.8 + go-sasl
c.Authenticate(sasl.NewPlainClient("", user, password))
```

สาเหตุ: Gmail advertises `AUTH=PLAIN AUTH=XOAUTH2` via CAPABILITY
`Login` command ถูก reject → ต้องใช้ `AUTHENTICATE PLAIN` แทน

---

## Error Handling

| กรณี | การจัดการ |
|---|---|
| IMAP connect ล้มเหลว | log error + LINE admin notify (throttle 1 ครั้ง/ชม.) |
| AI extract ล้มเหลว | log error + LINE admin notify, ไม่สร้าง bill |
| ไม่มี items ใน extract | log warning, ไม่สร้าง bill |
| Items ไม่ match mapping | bill = 'pending' + LINE admin notify 📋 |
| SML ล้มเหลว (3 retry) | bill = 'failed' + LINE admin notify ⚠️ |
| Email mark SEEN ล้มเหลว | ถูก process ซ้ำใน poll ถัดไป (idempotent ถ้า auto-confirm ผ่าน) |

---

## Config ที่เกี่ยวข้อง

IMAP ไม่มี `.env IMAP_*` singleton แล้ว ให้ตั้งผ่าน UI:

| Field | Table column |
|---|---|
| Host/Port/User/Password/Mailbox | `imap_accounts.host`, `port`, `username`, `password`, `mailbox` |
| Filters | `filter_from`, `filter_subjects[]` |
| Routing | `channel`, `shopee_domains[]` |
| Timing | `poll_interval_seconds` |
| Runtime status | `last_polled_at`, `last_poll_status`, `last_poll_error`, `consecutive_failures` |

AI/OCR ยังมาจาก env:

```bash
OPENROUTER_MODEL=google/gemini-2.5-flash
OPENROUTER_FALLBACK_MODEL=anthropic/claude-3-5-haiku  # server current value
MISTRAL_API_KEY=
AUTO_CONFIRM_THRESHOLD=0.85
```

---

## ขั้นตอน Debug เมื่อ email ไม่ถูก process

```bash
# 1. ดู logs
docker logs nexflow-backend --tail=50 2>&1 | grep -i "imap\|email\|poll"

# 2. ตรวจ IMAP config/status ใน DB
docker exec nexflow-postgres psql -U nexflow -d nexflow \
  -c "SELECT name, enabled, last_poll_status, consecutive_failures, last_poll_error FROM imap_accounts;"

# 3. ทดสอบ IMAP connection ด้วย curl
curl -v --ssl-reqd 'imaps://imap.gmail.com:993/INBOX' \
  --user 'email@gmail.com:apppassword16หลัก' 2>&1 | head -20

# 4. ดูบิลใน DB
docker exec nexflow-postgres psql -U nexflow -d nexflow \
  -c "SELECT id, status, error_msg, created_at FROM bills WHERE source='email' ORDER BY created_at DESC LIMIT 5;"
```

**Checklist:**
- [ ] `poll_interval_seconds` ≥ 300
- [ ] Gmail: 2FA เปิดอยู่ + ใช้ App Password (ไม่ใช่ password จริง)
- [ ] Gmail: เปิด IMAP ใน Settings → Forwarding and POP/IMAP
- [ ] Email เป็น UNSEEN (ยังไม่ได้อ่าน)
- [ ] Filter ตรงกับ email ที่ส่ง (หรือลอง clear filter ก่อน)

---

## ไฟล์ที่เกี่ยวข้อง

| ไฟล์ | หน้าที่ |
|---|---|
| `backend/internal/services/email/coordinator.go` | one goroutine per enabled account |
| `backend/internal/services/email/account.go` | account runtime/update helpers |
| `backend/internal/services/email/imap.go` | IMAP connect, search UNSEEN, fetch, parse, mark SEEN |
| `backend/internal/handlers/email.go` | AttachmentProcessor: OCR → extract → map → anomaly → DB → SML |
| `backend/internal/handlers/imap_settings.go` | `/settings/email` APIs |
| `backend/internal/services/mistral/ocr.go` | Mistral OCR API (PDF → markdown) |
| `backend/internal/services/ai/openrouter.go` | ExtractText, ExtractImage, ExtractPDF |
| `backend/internal/repository/bill_repo.go` | Create, UpdateStatus, UpdateBillItem, UpdatePriceHistory |
| `backend/internal/handlers/bills.go` | Retry handler — re-map + re-send SML |
