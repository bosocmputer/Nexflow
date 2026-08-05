# Nexflow

ระบบบริหารยอดขายหลาย marketplace และส่งเอกสารขายเข้า SML ERP ด้วยการจับคู่ SKU แบบ deterministic โดยไม่ใช้ AI

---

## Input Channels

| Channel | ประเภทบิล | สถานะ |
| --- | --- | --- |
| Shopee Excel | บิลขาย | ✅ deployed |
| Lazada Excel | บิลขาย | ✅ deployed |
| TikTok Excel/CSV | บิลขาย | ✅ deployed |
| Shopee Open API (OAuth) | บิลขาย | ✅ live |
| Shopee Realtime Operations | คำสั่งซื้อ/สร้างเอกสาร/Timeline | ✅ live |
| LINE Shopee Notifications | แจ้งทีมงานด้วย rich Flex | ✅ live |
| NextStep Marketplace | dashboard, order detail, notification | ✅ live |
| LINE OA (human chat) | แชท/สกัดเอกสาร | ปิดใช้งาน |
| Email / Purchase Order | ฝั่งซื้อ | ปิดใช้งาน |

---

## Tech Stack

```
Backend:   Go 1.24 (Gin)  —  module: nexflow
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
Matching:  exact SKU → confirmed alias → verified exact name (no SKU only)
Deploy:    Docker Compose + Cloudflare + host-based edge Nginx
```

---

## Server

```
Host:    10.121.20.83 (user: ubuntu)
Folders: /mnt/data/nextstep-node-2/nexflow*
```

| Service | Container | Port |
| --- | --- | --- |
| edge | nexflow-edge | 6323 |
| demo backend / postgres | nexflow-backend / nexflow-postgres | 8110 / 5440 |
| AOY backend / postgres | nexflow-aoy-backend / nexflow-aoy-postgres | 8111 / 5441 |
| Lanboon backend / postgres | nexflow-lanboon-backend / nexflow-lanboon-postgres | 8112 / 5442 |

Production domains are registered in `deploy/nextstep-instances.json` and routed through public port `6323`.

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
bash scripts/check_sales_only_runtime.sh
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target demo
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target aoy
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target lanboon
```

Deploy uses the committed Git revision, backs up `.env` and PostgreSQL, removes disabled runtime secrets, rebuilds containers, validates health, and smoke-tests each hostname through the edge.

---

## Docs

| ไฟล์ | เนื้อหา |
| --- | --- |
| [docs/current-state.md](docs/current-state.md) | สถานะ deploy ล่าสุด |
| [docs/sales-only-no-ai.md](docs/sales-only-no-ai.md) | runtime และ release guard แบบ sales-only |
| [docs/nextstep-server-deploy-flow.md](docs/nextstep-server-deploy-flow.md) | production deploy flow |
| [docs/shopee-import.md](docs/shopee-import.md) | Shopee Excel + Open API flow |
| [docs/shopee-realtime-uat.md](docs/shopee-realtime-uat.md) | Shopee Realtime UAT/checklist |
| [docs/line-oa.md](docs/line-oa.md) | LINE OA สำหรับ Push notification |
| [docs/deploy-instances.md](docs/deploy-instances.md) | Production deploy, SML gateway, feature flags |

---

Last updated: 2026-08-05 | Public edge: 6323
