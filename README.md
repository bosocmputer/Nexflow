# Nexflow

ระบบช่วยลดเวลาคีย์บิล โดยใช้ AI extract ข้อมูลจากหลาย channel แล้วส่งเข้า SML ERP โดยอัตโนมัติ

---

## Input Channels

| Channel | ประเภทบิล | สถานะ |
| --- | --- | --- |
| Email (IMAP multi-account) | ขาย/ซื้อตาม routing | ✅ deployed |
| Shopee Excel | บิลขาย | ✅ deployed |
| Lazada Excel | บิลขาย | ✅ deployed |
| TikTok Excel/CSV | บิลขาย | ✅ deployed |
| Shopee Open API (OAuth) | บิลขาย | ✅ live |
| LINE OA (human chat) | บิลขาย | disabled (`VITE_ENABLE_CHAT=false`) |

---

## Tech Stack

```
Backend:   Go 1.24 (Gin)  —  module: nexflow
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
AI:        OpenRouter (gemini-2.5-flash-lite / gemini-2.5-flash / Mistral OCR / Whisper)
Deploy:    Docker Compose + ngrok (fixed domain)
```

---

## Server

```
Host:    192.168.2.109  (user: bosscatdog)
Folder:  /home/bosscatdog/billflow-henna
```

| Service | Container | Port |
| --- | --- | --- |
| backend | nexflow-backend | 8110 |
| frontend | nexflow-frontend | 3030 |
| postgres | nexflow-postgres | 5440 |

Public URL: `https://animal-galvanize-tameness.ngrok-free.dev`

---

## Quick Start

```bash
# 1. Configure
cp .env.example .env
# แก้ .env ใส่ credentials จริง

# 2. Start
docker compose up -d

# 3. Verify
curl http://localhost:8110/health
# → {"status":"ok","env":"production"}
```

Default admin credentials: retrieve from the local/deploy secret source. Do not store real passwords in tracked docs.

---

## Deploy to Server

```bash
python scripts/release_guard.py --local
NX_PASS='<server-password>' python scripts/deploy.py
curl http://192.168.2.109:8110/health
```

`scripts/deploy.py` backs up production, syncs frontend/backend with `rsync --delete`, rebuilds containers, and runs the production Nexflow brand guard so stale BillFlow/Henna assets do not remain on the server.

---

## Docs

| ไฟล์ | เนื้อหา |
| --- | --- |
| [CLAUDE.md](CLAUDE.md) | Blueprint สำหรับ Claude Code — architecture, gotchas, env vars |
| [docs/current-state.md](docs/current-state.md) | สถานะ deploy ล่าสุด |
| [docs/shopee-import.md](docs/shopee-import.md) | Shopee Excel + Open API flow |
| [docs/email.md](docs/email.md) | IMAP multi-account pipeline |
| [docs/line-oa.md](docs/line-oa.md) | LINE OA multi-OA + Reply/Push |

---

Last updated: 2026-05-31 | Ports: 8110 / 3030 / 5440
