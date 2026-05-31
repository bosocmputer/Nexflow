# LINE OA — Human Chat Inbox

> อัพเดตล่าสุด: 2026-05-06
> สถานะ: ✅ human chat inbox + multi-OA deployed. Chatbot/cart/MCP flow เก่าถูกเอาออกแล้วตั้งแต่ migration/session 13.

---

## ภาพรวม

LINE OA ใน BillFlow ตอนนี้เป็นระบบแชท 2 ทางระหว่างลูกค้ากับ admin ไม่ใช่ bot สั่งซื้ออัตโนมัติ ลูกค้าส่ง text/image/file/audio เข้ามา → ระบบบันทึกใน `/messages` → admin ตอบกลับผ่าน Reply API หรือ Push API และสามารถเปิดบิลขายจาก conversation ได้

---

## Webhook Flow

```
LINE Platform
  POST /webhook/line/:oaId      preferred, ระบุ OA ชัดเจน
  POST /webhook/line            legacy fallback
        │
        ▼
handlers/line.go
  - verify X-Line-Signature ด้วย channel_secret ของ OA นั้น
  - resolve OA จาก URL :oaId หรือ destination fallback
  - create/update chat_conversations
  - save inbound chat_messages
  - download media into chat_media when needed
  - cache replyToken in chat_conversations.last_reply_token
  - publish SSE event to admin browser
        │
        ▼
/messages
  - conversation list
  - message thread
  - quick replies
  - notes/tags/phone
  - extract media → preview → create bill
```

---

## Admin Reply Flow

```
Admin sends text/media from /messages
        │
        ▼
POST /api/admin/conversations/:lineUserId/messages
POST /api/admin/conversations/:lineUserId/messages/media
        │
        ▼
chat_inbox.go
  - create outgoing chat_messages with pending status
  - try LINE Reply API first if cached replyToken is still usable
  - fallback to Push API when replyToken is expired/consumed/unavailable
  - update delivery_status and delivery_method ('reply' | 'push')
  - publish SSE update
```

Reply API ไม่กิน Push quota ของ LINE OA; Push API ใช้เมื่อ reply token ใช้ไม่ได้เท่านั้น

---

## Media Reply

Admin ส่งรูปให้ลูกค้าได้เมื่อ server ตั้งค่า `PUBLIC_BASE_URL` แล้ว เพราะ LINE servers ต้อง fetch รูปจาก public HTTPS URL

```
chat_media file
  → signed URL /public/media/:mediaID?t=<HMAC token>
  → originalContentUrl / previewImageUrl
  → LINE Push image message
```

`MEDIA_SIGNING_KEY` ใช้ sign token; ถ้าไม่ตั้งจะ fallback เป็น `JWT_SECRET`

---

## Multi-OA

| Feature | รายละเอียด |
|---|---|
| UI | `/settings/line-oa` |
| Webhook ต่อ OA | `/webhook/line/<oa_id>` |
| Credentials | `line_oa_accounts.channel_secret`, `channel_access_token` |
| Default seed | ถ้า table ว่างและมี `LINE_*` env ระบบ seed "Default (from .env)" ตอน boot |
| Read receipts | `mark_as_read_enabled` ต่อ OA, default OFF เพราะต้องใช้ LINE OA Plus |

---

## Conversation Features

| Feature | Table/Route |
|---|---|
| status: open/resolved/archived | `chat_conversations.status`, `PATCH /api/admin/conversations/:lineUserId/status` |
| unread count | `chat_conversations.unread_admin_count`, `/unread-count` |
| phone | `chat_conversations.phone`, `PATCH /phone` |
| internal notes | `chat_notes`, `/notes` |
| tags | `chat_tags`, `chat_conversation_tags`, `/settings/chat-tags` |
| quick replies | `chat_quick_replies`, `/api/admin/quick-replies` |
| customer history | `GET /api/admin/conversations/:lineUserId/history` |
| create bill from chat | `POST /api/admin/conversations/:lineUserId/bills` |
| extract from media | `POST /api/admin/conversations/:lineUserId/messages/:messageId/extract` |

---

## Admin Notifications

Legacy single LINE service from `LINE_CHANNEL_SECRET`, `LINE_CHANNEL_ACCESS_TOKEN`, `LINE_ADMIN_USER_ID` ยังใช้สำหรับ push แจ้ง admin จาก background jobs เช่น:

| กรณี | ตัวอย่าง |
|---|---|
| SML send failed | bill failed after retry |
| Bill pending | unmapped/anomaly needs review |
| Email inbox failing | ≥ 3 consecutive failures, throttled |
| Disk usage high | root fs > threshold |
| LINE token expiry | weekly reminder |
| Daily insight | F4 daily summary |
| Tunnel drift | `PUBLIC_BASE_URL/health` ใช้งานไม่ได้ |

---

## Config ที่เกี่ยวข้อง

```bash
# Default OA seed + admin notifications
LINE_CHANNEL_SECRET=
LINE_CHANNEL_ACCESS_TOKEN=
LINE_ADMIN_USER_ID=
LINE_GREETING=

# Public media URLs for LINE image delivery
PUBLIC_BASE_URL=https://<cloudflare-quick-tunnel>.trycloudflare.com
MEDIA_SIGNING_KEY=        # optional; fallback JWT_SECRET

# AI extraction from chat media/audio
OPENROUTER_MODEL=google/gemini-2.5-flash
OPENROUTER_AUDIO_MODEL=openai/whisper-1
MISTRAL_API_KEY=
```

---

## ไฟล์ที่เกี่ยวข้อง

| ไฟล์ | หน้าที่ |
|---|---|
| `backend/internal/handlers/line.go` | LINE webhook handler |
| `backend/internal/handlers/chat_inbox.go` | admin conversation APIs |
| `backend/internal/handlers/line_oa.go` | LINE OA CRUD/test |
| `backend/internal/handlers/chat_quick_reply.go` | quick reply templates |
| `backend/internal/handlers/chat_notes.go` | internal notes |
| `backend/internal/handlers/chat_tags.go` | tags |
| `backend/internal/handlers/public_media.go` | signed public media endpoint |
| `backend/internal/handlers/sse.go` | admin event stream |
| `backend/internal/services/line/registry.go` | multi-OA service registry |
| `frontend/src/pages/Messages` | chat inbox UI |
| `frontend/src/pages/LineOA.tsx` | `/settings/line-oa` |
| `frontend/src/pages/QuickReplies.tsx` | `/settings/quick-replies` |
| `frontend/src/pages/ChatTags.tsx` | `/settings/chat-tags` |
