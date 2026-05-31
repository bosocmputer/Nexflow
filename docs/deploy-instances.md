# BillFlow Deploy Instances

Registry สำหรับจำว่าแต่ละร้านใช้ folder, port, container และ Cloudflare tunnel ไหนบน server `192.168.2.109`.

> หมายเหตุ: Main/Thaisunsport ยังใช้ Cloudflare Quick Tunnel (`trycloudflare.com`) และ URL จะเปลี่ยนเมื่อ process `cloudflared` restart. Henna ตอนนี้ใช้ `ngrok` (fixed dev domain) แทน.

## Summary

| Instance | ร้าน / วัตถุประสงค์ | Server folder | Frontend | Backend | PostgreSQL | Cloudflare URL ล่าสุด | Tunnel log |
| --- | --- | --- | ---: | ---: | ---: | --- | --- |
| `billflow` | BillFlow ปกติ / demo หลัก | `/home/bosscatdog/billflow` | `3010` | `8090` | `5438` | ดูจาก log | `/tmp/billflow-tunnel.log` |
| `billflow-thaisunsport` | Thaisunsport demo Phase 1 ฝั่งซื้อ | `/home/bosscatdog/billflow-thaisunsport` | `3020` | `8100` | `5448` | `https://pets-mini-museums-ships.trycloudflare.com` | `/tmp/billflow-thaisunsport-tunnel.log` |
| `billflow-henna` | Henna customer trial | `/home/bosscatdog/billflow-henna` | `3030` | `8110` | `5458` | `https://animal-galvanize-tameness.ngrok-free.dev` | `- (ngrok)` |

## Deploy Policy

ใช้ codebase เดียวสำหรับทุก instance และแยกความต่างด้วย environment / feature flags / instance config เท่านั้น.

| Change type | Deploy targets | Notes |
| --- | --- | --- |
| ทดสอบงานใหม่หรือแก้เฉพาะ demo หลัก | `billflow` | ใช้ main เป็นพื้นที่ทดสอบก่อน |
| Phase 1+ / งานฝั่งขาย / UX ที่เปิดทั้งซื้อและขาย | `billflow`, `billflow-henna` | Henna ต้องเทียบเท่า main และเปิดงานฝั่งซื้อ + งานฝั่งขาย |
| Shared Phase 1 bug/UX/backend/email/bills/logs/settings ที่ไม่ผูกกับงานฝั่งขาย | `billflow`, `billflow-henna`, `billflow-thaisunsport` | Thaisunsport รับเฉพาะสิ่งที่ใช้ร่วมกับ Phase 1 |
| งานเฉพาะร้าน เช่น credential, SML config, tunnel URL, env เฉพาะ instance | instance นั้นเท่านั้น | ห้ามกระทบ instance อื่น |

ก่อน deploy ทุกครั้งต้องระบุ `Change type`, `Deploy targets`, และ instance ที่ตั้งใจ skip ให้ชัดเจนในข้อความสรุป.

## Next Planned Phase — Shopee API Direct

- เริ่ม development/test บน `billflow` ก่อน เพราะเป็น demo หลักและใช้ตรวจ flow ใหม่ได้เร็วที่สุด.
- เมื่อ stable แล้ว deploy ไป `billflow` + `billflow-henna` เพราะ Henna ต้องเทียบเท่า main สำหรับ Phase 1+ / งานฝั่งขาย.
- ยังไม่ deploy ไป `billflow-thaisunsport` เพราะ instance นี้ยังเป็น Phase 1 ฝั่งซื้อ และปิด sales/import channel ด้วย feature flags.
- Shopee API direct ต้อง feed เข้า review/SML retry pipeline เดิมเหมือน Shopee Excel; ห้ามสร้าง SML send flow แยกถ้าไม่จำเป็น.
- Shopee Excel ต้องคงไว้เป็น fallback/manual import ระหว่าง UAT ของ API direct.

## Container Names

| Instance | Frontend container | Backend container | PostgreSQL container |
| --- | --- | --- | --- |
| `billflow` | `billflow-frontend` | `billflow-backend` | `billflow-postgres` |
| `billflow-thaisunsport` | `billflow-thaisunsport-frontend` | `billflow-thaisunsport-backend` | `billflow-thaisunsport-postgres` |
| `billflow-henna` | `billflow-henna-frontend` | `billflow-henna-backend` | `billflow-henna-postgres` |

## Quick Commands

### Check health

```bash
curl http://192.168.2.109:8090/health   # billflow
curl http://192.168.2.109:8100/health   # thaisunsport
curl http://192.168.2.109:8110/health   # henna
```

### Check running containers

```bash
docker ps --format '{{.Names}} {{.Ports}}' | grep billflow
```

### Get current Quick Tunnel URL

```bash
grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' /tmp/billflow-tunnel.log | tail -1
grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' /tmp/billflow-thaisunsport-tunnel.log | tail -1
# Henna currently uses ngrok domain from its own ngrok setup/config.
```

### Restart a Quick Tunnel

```bash
nohup cloudflared tunnel --url http://127.0.0.1:3010 --no-autoupdate > /tmp/billflow-tunnel.log 2>&1 &
nohup cloudflared tunnel --url http://127.0.0.1:3020 --no-autoupdate > /tmp/billflow-thaisunsport-tunnel.log 2>&1 &
nohup cloudflared tunnel --url http://127.0.0.1:3030 --no-autoupdate > /tmp/billflow-henna-tunnel.log 2>&1 &
```

## sml-api-bybos — Shared SML Gateway

**Location on server:** `~/sml-api-bybos/` — single Docker Compose project running at port `8200`.

**Architecture:** 1 process / 3 tenants — แต่ละ BillFlow instance ส่ง request ไปที่ `http://192.168.2.109:8200` (หรือ Docker gateway IP สำหรับ container ที่เรียกจาก backend: `http://172.24.0.1:8200`) และใช้ header `x-tenant` เพื่อระบุว่าจะเชื่อมต่อ DB ของร้านไหน. sml-api-bybos map tenant → DB connection โดยอ่านจาก env vars `SML_DB_HOST_<TENANT>` ตอน boot.

### Tenant Routing Table

| BillFlow Instance | URL ที่ backend เรียก | x-tenant header | DB host |
| --- | --- | --- | --- |
| `billflow` (main) | `http://172.24.0.1:8200` | `sml1_2026` | `192.168.2.248` (SML production DB) |
| `billflow-henna` | `http://172.24.0.1:8200` | `aoy` | `demserver.3bbddns.com` |
| `billflow-thaisunsport` | `http://192.168.2.109:8200` | `data1_test` | `thaisunsport.thddns.net:9983` |

> หมายเหตุ: `172.24.0.1` คือ Docker bridge gateway — ใช้จากภายใน container เพื่อเรียก service ที่รันบน host. `192.168.2.109` ใช้ได้จาก host โดยตรง.

### เพิ่ม Tenant ใหม่

1. เพิ่ม tenant slug ใน `ALLOWED_TENANTS` ใน `~/sml-api-bybos/.env` (comma-separated):

   ```env
   ALLOWED_TENANTS=sml1_2026,aoy,data1_test,<new_tenant>
   ```

2. เพิ่ม DB override สำหรับ tenant ใหม่:

   ```env
   SML_DB_HOST_<NEW_TENANT_UPPER>=<db_host>
   SML_DB_PORT_<NEW_TENANT_UPPER>=<port>
   SML_DB_USER_<NEW_TENANT_UPPER>=<user>
   SML_DB_PASSWORD_<NEW_TENANT_UPPER>=<password>
   SML_DB_SSLMODE_<NEW_TENANT_UPPER>=disable
   ```

   (ชื่อ env var ต้องเป็น uppercase ของ tenant slug เช่น `data1_test` → `DATA1_TEST`)

3. Force-recreate container เพื่อโหลด env ใหม่:

   ```bash
   cd ~/sml-api-bybos
   docker compose up -d --force-recreate
   ```

> ⚠️ **`docker compose restart` ไม่โหลด env_file ใหม่** — ต้องใช้ `--force-recreate` เท่านั้น. ถ้าใช้ restart แล้ว tenant ยังไม่ผ่าน จะได้รับ `{"error":"tenant_not_allowed"}`.

## Port Allocation Rule

ใช้ pattern นี้สำหรับร้านถัดไป:

| Instance order | Frontend | Backend | PostgreSQL |
| ---: | ---: | ---: | ---: |
| Main | `3010` | `8090` | `5438` |
| Customer 1 | `3020` | `8100` | `5448` |
| Customer 2 | `3030` | `8110` | `5458` |
| Customer 3 | `3040` | `8120` | `5468` |

ให้ตั้งชื่อ folder/container/volume ตาม slug ร้าน เช่น `billflow-henna-*` เพื่อไม่ชน instance อื่น.

## Henna Notes

- Latest deploy verified: 2026-05-22 15:26 +07.
- Created from current normal BillFlow version, not Thaisunsport branch/config.
- Deployed as isolated Docker Compose project in `/home/bosscatdog/billflow-henna`.
- Database is separate PostgreSQL volume `billflow-henna_billflow_henna_pgdata`.
- `PUBLIC_BASE_URL` in `/home/bosscatdog/billflow-henna/.env` is set to `https://animal-galvanize-tameness.ngrok-free.dev`.
- App settings seeded:
  - `instance.name = BillFlow Henna`
  - `instance.slug = billflowhenna`
- Runtime parity notes (2026-05-22):
  - Synced from the same local source snapshot as BillFlow main and rebuilt `billflow-henna-backend` + `billflow-henna-frontend`.
  - Runtime SML base URL in DB: `sml.rest_base_url=http://172.24.0.1:8200`.
  - Health checks passed: `http://192.168.2.109:8110/health`, `http://192.168.2.109:3030/login`.

## Thaisunsport Notes

- Latest deploy verified: 2026-05-11 15:32 +07.
- Current purpose: customer demo for Phase 1 purchase flow only.
- Keep sale features disabled until the user explicitly asks to open Phase 1+ for this customer:
  - `VITE_PHASE=1`
  - `VITE_ENABLE_SALES_ORDERS=false`
  - `VITE_ENABLE_SHOPEE_EXCEL=false`
- AI model config on server:
  - `OPENROUTER_MODEL=google/gemini-2.5-flash-lite`
  - `OPENROUTER_FALLBACK_MODEL=google/gemini-2.5-flash`
- Verified after latest deploy:
  - backend health on `8100` is ok
  - frontend on `3020` serves HTML
  - frontend flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`
  - containers `billflow-thaisunsport-frontend`, `billflow-thaisunsport-backend`, and `billflow-thaisunsport-postgres` are up

## Latest Shared Deploy

- 2026-05-22 15:26 +07: Main + Henna parity deploy and verification completed.
- Scope: deployed local backend/frontend/docs/scripts snapshot to `billflow` then `billflow-henna`; rebuilt and restarted only those two instances.
- Verification: `go test ./...`, `npm run build`, main preflight (`scripts/preflight-main.sh`), Henna preflight override (`BF_HOST=192.168.2.109 BACKEND_PORT=8110 FRONTEND_PORT=3030 SML_API_PORT=8200 SML_TENANT=aoy`), and API smoke for login/bills/channel-defaults on both passed.
- Runtime checks: both main and Henna now use `sml.rest_base_url=http://172.24.0.1:8200`; schema columns from migrations `047` and `048` verified on both.
- Operational note: Thaisunsport remains unchanged in this round (no restart/deploy).
- 2026-05-21 10:32 +07: BillFlow main Shopee live OAuth callback fallback deployed and verified.
- Scope: `billflow` only. Backend now handles Shopee live callbacks that return `code` + `shop_id` but omit `state` by consuming exactly one matching unexpired OAuth state for the current environment and redirect URL; missing/ambiguous state still fails safely.
- Verification: `go test ./...`, backend-only deploy/restart, backend `/health`, `scripts/preflight-main.sh`, browser OAuth retry, `/api/settings/shopee-api/status`, and preview-only fetch smoke passed.
- Operational status: Shopee Open API is now live and connected to shop `1029622928`; status reports `connected=true`, `token_state=access_valid`, and `can_fetch=true`. Preview smoke for `2026-05-20` to `2026-05-21` returned zero orders, not an API error.
- 2026-05-21 09:25 +07: BillFlow main Bulk Send Job History deployed and verified.
- Scope: `billflow` only. Added `/bulk-send-jobs` read-only history page for admin/staff, sidebar/command-palette entry, and backend list endpoint `GET /api/bills/bulk-send-jobs`.
- Verification: backend `/health`, frontend `/login`, `scripts/preflight-main.sh`, API smoke for list/detail/invalid-status, and browser QA on `/bulk-send-jobs` detail dialog passed.
- Operational status at that time: Shopee Open API still reported sandbox/not connected in BillFlow readiness status; superseded by the 10:32 live OAuth deploy above.
- 2026-05-20 20:03 +07: BillFlow main async SML bulk send jobs deployed, verified, committed, and pushed.
- Scope: `billflow` only. `/bills`, `/sales-orders`, and `/sale-invoices` now create DB-backed bulk jobs for `ส่ง SML ทั้งหมด`; UI shows progress, can resume after close/reload, and can retry failed rows only.
- Backend: migration `044_sml_bulk_jobs.sql`, `sml_bulk_jobs`, `sml_bulk_job_items`, serial worker, duplicate `client_request_id` guard, active-job conflict guard, startup recovery for interrupted jobs, and audit detail `via=bulk_job`.
- Verification: `go test ./...`, `npm --prefix frontend run build`, `scripts/preflight-main.sh`, and live SML smoke test passed.
- Live smoke: bulk job `128ceffe-5055-4863-8944-c6ce52301d26` sent bill `20275aed-fe5f-402f-9160-a93a3f5b2ccb` to SML purchaseorder `BF-PO26050001` with sent `1`, failed `0`, skipped `0`.
- Git: commit `871e8f5 feat: add async SML bulk send jobs` pushed to `main`.
- 2026-05-20 15:04 +07: BillFlow main Shopee API readiness, SML product images, and instance settings hardening deployed.
- Scope: `billflow` only. Shopee Open API readiness is available on `/import/shopee` with approval/config gates, OAuth callback/token storage, preview-only API fetch, and friendly error UX; live connection is blocked until Shopee Go-Live approval and live key cutover.
- Scope: SML product images now lazy-load through `sml-api-bybos`; BillFlow stores image metadata only, `/settings/catalog` and the bill item picker show thumbnails/full preview/gallery, and `sml1_2026_images.public.images` has the `images_trim_image_id_order_roworder_file_idx` expression index.
- Scope: `/settings/instance` can test draft SML REST URL/database tenant before saving and documents the `{database}_images` image DB pattern.
- Operational docs: [sml-image-db-maintenance.md](sml-image-db-maintenance.md), [sml-api-migration.md](sml-api-migration.md), [sml-bulk-send-jobs.md](sml-bulk-send-jobs.md), and [shopee-open-api-live-cutover.md](shopee-open-api-live-cutover.md).
- Verification: `go test ./...`, `npm run build`, `scripts/preflight-main.sh`, browser QA for `/import/shopee`, `/settings/catalog`, bill item picker, and `/settings/instance`; image index script verified against `sml1_2026_images`.
- 2026-05-12 11:34 +07: Audit actor + production log accountability deployed to all three instances.
- Scope: backend `/api/logs` now returns `actor` with user name/email/role when `user_id` exists, classifies background entries as worker/system, and supports `user_id` filtering.
- Scope: `/logs` shows the actor badge in each row, adds a `ผู้ทำรายการ` filter, removes playful emoji from action labels, and keeps DEV payload copyable for admins/devs.
- Scope: SML send success/failure, bill item add/delete, and mapping feedback logs now write the current `user_id`; backend also blocks malformed `doc_no` with hidden/Thai mark characters before sending to SML.
- Change type: Shared Phase 1+ audit/accountability/data-quality hardening.
- Local verification: `npm run build` passed; `GOCACHE=... go test ./...` passed.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend `/logs` HTTP 200 on `3010`, `3030`, `3020`; main `/api/logs` verified returning `actor` for Admin entries.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12 11:11 +07: Logs Action View deployed to all three instances.
- Scope: `/logs` now has summary cards, quick filters, DEV toggle, grouped import runs, SML failure incident cards, copyable DEV payload, and data-quality warning for malformed/hidden-character `doc_no`.
- Change type: Shared Phase 1+ frontend UX/debug clarity.
- Local verification: `npm run build` passed; browser check on local `/logs` confirmed new summary/DEV/filter controls render.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend `/logs` HTTP 200 on `3010`, `3030`, `3020`.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12 10:50 +07: Docs/status update for next phase; no application deploy.
- Scope: documented latest production status and Shopee API direct handoff plan.
- Current import readiness: Shopee Excel, Lazada Excel, and TikTok Excel/CSV are ready for BillFlow main + Henna UAT; Thaisunsport remains Phase 1 purchase-only.
- Next deploy policy: Shopee API direct starts on `billflow`, then `billflow` + `billflow-henna` after UAT; skip Thaisunsport until sales features are explicitly enabled.
- 2026-05-12 10:35 +07: Action Center and expanded error playbook deployed to all three instances.
- Scope: Dashboard now has `Action Center` that ranks next-best actions across setup, email errors, SML failures, mapping review, and pending SML sends.
- Scope: `/logs` guidance now classifies more failure causes: SML timeout/network, doc format, customer/supplier, VAT, warehouse/shelf, item/unit, Gmail App Password, and AI quota/credit.
- Change type: Shared frontend UX/workflow clarity.
- Local verification: `npm run build` passed; browser check confirmed Dashboard `Action Center` and Logs page render.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend Dashboard ok on `3010`, `3030`, `3020`, and main `/logs` ok.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12 10:12 +07: Bulk send summary/results and mapping hotspot dashboard deployed to all three instances.
- Scope: bulk send dialog now shows a pre-send summary, clearer post-send success/fail/skip result panel, copyable SML error summary, and direct link to the first failed bill.
- Scope: `/mappings` now shows top raw product names still blocking `needs_review` bills, with counts and a link to the first affected bill for faster mapping cleanup.
- Change type: Shared frontend UX/workflow clarity.
- Local verification: `npm run build` passed; browser check confirmed `/mappings` renders the new hotspot panel and `/sale-invoices` loads.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend route ok on main/Henna `/sale-invoices` and Thaisunsport `/bills`.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12 09:48 +07: Bulk Send dialog readability follow-up deployed to all three instances.
- Scope: `/bills`, `/sales-orders`, and `/sale-invoices` bulk-send dialog uses a wider modal, reduces noisy helper text, and shows ready rows as a table with send sequence, order no, expected `doc_no`, and status.
- Fix: frontend computes sequential expected `doc_no` from the backend preview so bulk rows no longer all appear to use the same next document number before the real send reserves numbers.
- Change type: Shared frontend UX/preview clarity.
- Local verification: `npm run build` passed.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend route ok on main/Henna `/sale-invoices` and Thaisunsport `/bills`.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12: Bulk Send dialog UI parity deployed to all three instances.
- Scope: `/bills`, `/sales-orders`, and `/sale-invoices` bulk-send dialog now follows the same structure and visual language as the Bill Detail send dialog while keeping the bulk-only ready/skipped result list.
- Follow-up: each ready/skipped row now shows upstream order number plus `doc_no` preview so users can verify document numbers before bulk sending.
- Change type: Shared frontend UX parity.
- Local verification: `npm run build` passed.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend route ok on main/Henna `/sale-invoices` and Thaisunsport `/bills`.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12 follow-up: deployed doc_no preview per row in the same bulk-send dialog; verified the same health/routes and Thaisunsport flags.
- 2026-05-12: Verified Mapping Loop backend fix deployed to all three instances.
- Scope: saving a bill item now learns mapping even when the AI-prefilled code did not change, then applies the verified mapping to open bills with the same source/bill_type/raw_name and promotes fully mapped bills from `needs_review` to `pending`.
- Change type: Shared mapping workflow bug for marketplace/import/email bill detail review.
- Local verification: `GOCACHE=... go test ./...` passed.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`.
- Henna data follow-up: applied existing Shopee saleinvoice mapping to 3 open item rows and promoted 3 bills to `pending`; remaining `needs_review` bills are 3 distinct raw names/options with no confirmed mapping yet.
- 2026-05-12: Shared Bill Detail item mapping UI fix deployed to all three instances.
- Scope: when editing a bill item and selecting a new SML product, the edit row and saved item table now show the newly selected product name/score immediately without requiring a page refresh.
- Change type: Shared Bill Detail UX bug, applies to purchase, saleorder, and saleinvoice item rows.
- Local verification: `npm run build` passed.
- Deploy verification: backend health ok on `8090`, `8110`, `8100`; frontend `/bills` returns HTTP 200 on `3010`, `3030`, `3020`.
- Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-12 09:40 +07: Shopee Excel status filter updated on `billflow` and `billflow-henna`.
- Scope: `/import/shopee` now imports rows with status `ที่ต้องจัดส่ง`; only `ยกเลิกแล้ว` remains filtered out.
- Added parser test `TestParseShopeeExcelKeepsReadyToShipStatus`.
- Skipped `billflow-thaisunsport` intentionally because Shopee Excel remains disabled in Phase 1.
- 2026-05-12 09:24 +07: Shared Phase 1 IMAP poll detail UX deployed to all three instances.
- Scope: migration `031_imap_poll_details.sql` adds `imap_accounts.last_poll_details`; `/settings/email` can expand `ผลรอบล่าสุด` to show subject/from/date plus processed/skipped reason per email.
- Deploy targets: `billflow`, `billflow-henna`, `billflow-thaisunsport`.
- Also patched old source-check migrations `002_sml_catalog.sql` and `004_shopee_shipped.sql` to include `tiktok`, so boot migrations remain idempotent after TikTok bills exist.
- Verified backend health: `8090`, `8110`, `8100`.
- Verified frontend route `/settings/email`: HTTP 200 on `3010`, `3030`, `3020`.
- Verified `last_poll_details` column exists in all three PostgreSQL containers; latest poll details stored on main and Thaisunsport.
- 2026-05-11 20:19 +07: TikTok Excel/CSV import deployed to `billflow` and `billflow-henna` only.
- Scope: `/import/tiktok`, TikTok preview/confirm API, `tiktok` channel sale routing, migration `030_tiktok_import.sql`, and parser tests for real TikTok CSV shape.
- Verified backend health: `8090`, `8110`.
- Verified frontend route: `/import/tiktok` returns HTTP 200 on `3010`, `3030`.
- Verified migration `030_tiktok_import.sql` applied on main and Henna.
- Skipped `billflow-thaisunsport` intentionally because it remains Phase 1 purchase-only; backend `8100` was health-checked but containers were not restarted.
- 2026-05-11 16:42 +07: Phase 1+ Lazada Excel sales flow deployed to `billflow` and `billflow-henna` only.
- Scope: `/import/lazada`, Lazada preview/confirm API, `lazada` channel sale routing, sales queue counts including Lazada, and migration `029_lazada_import.sql`.
- Verified backend health: `8090`, `8110`.
- Verified frontend health: `3010`, `3030`.
- Verified migration `029_lazada_import.sql` applied on main and Henna.
- Skipped `billflow-thaisunsport` intentionally because it remains Phase 1 purchase-only; backend `8100` was health-checked but containers were not restarted.
- 2026-05-11 16:49 +07: Frontend-only follow-up for Lazada menu visibility on `billflow` and `billflow-henna`.
- Scope: remove `VITE_PHASE >= 2` gating from Lazada nav/command-palette items so the menu appears when sales features are enabled, matching Shopee Excel behavior.
- Verified built assets contain `Lazada Excel` and `/import/lazada` returns HTTP 200 on ports `3010` and `3030`.
- Skipped `billflow-thaisunsport`; frontend container uptime unchanged from before this follow-up.
- 2026-05-11 15:32 +07: Shared SML API payload requirement deployed to all three instances.
- Scope: all SML item/detail payloads now hardcode `is_get_price: 1` per line item for `sale_reserve`, `saleorder`, `saleinvoice`, and `purchaseorder`.
- Verified local `go test ./...`; added backend test `TestSMLLinePayloadsHardcodeIsGetPrice`.
- Verified backend health: `8090`, `8110`, `8100`.
- Verified Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-11 14:37 +07: End-of-day verification completed for all three instances.
- Scope: no new code after the Shopee product-image fix; verified latest deployed state, docs synced, and BillFlow main purchase bill `#260404V08VQU10` remains `pending` with product image URL stored.
- Verified backend health: `8090`, `8110`, `8100`.
- Verified frontend health: `3010`, `3030`, `3020`.
- Verified Thaisunsport flags remain `VITE_PHASE=1`, `VITE_ENABLE_SALES_ORDERS=false`, `VITE_ENABLE_SHOPEE_EXCEL=false`.
- 2026-05-11 12:45 +07: Shared Phase 1 UX/Logs clarity deployed to all three instances.
- Scope: `/logs` now explains `demo_test_data_reset` as `ล้างข้อมูลทดสอบ`, shows source `Setup`, and expands into user-readable facts/guidance instead of forcing users to interpret raw JSON.
- Verified backend health: `8090`, `8110`, `8100`.
- Verified frontend health: `3010`, `3030`, `3020`.
- 2026-05-11 12:59 +07: Shared Phase 1 Gmail IMAP onboarding fix deployed to all three instances.
- Scope: `/settings/email` now guides users through 2-Step Verification, Google App Password, and Gmail IMAP setup; Gmail App Passwords are normalized by removing spaces/dashes before test/save.
- Verified browser UI on BillFlow main and health on all three backend/frontend ports.
- 2026-05-11 13:10 +07: Shared Phase 1 bills email-date display deployed to all three instances.
- Scope: new email-created bills store `raw_data.email_date` from the IMAP envelope date; `/bills` shows that date with prefix `อีเมล` when available and falls back to `created_at` for older bills.
- Verified browser `/bills` on BillFlow main and health on all three backend/frontend ports.
- 2026-05-11 13:43 +07: Shared Phase 1 IMAP read/unread polling fix deployed to all three instances.
- Scope: IMAP now searches both read and unread messages within `lookback_days` instead of only unread messages, while keeping durable dedup through `processed_email_keys` to prevent duplicate bills.
- Verified health on all three backend/frontend ports. Production main logs confirmed account `fe6a36b8-7092-483a-9265-2e63e248dccb` found 6 messages and saw order email `260404V08VQU10`; it was skipped because a dedup tombstone already existed from 2026-05-08.
- 2026-05-11 14:20 +07: Shared Phase 1 IMAP poll-status clarity deployed to all three instances.
- Scope: migration `028_imap_poll_stats.sql` adds `last_poll_found`, `last_poll_processed`, `last_poll_skipped`; `/settings/email` now shows `ผลรอบล่าสุด` as `พบ / ประมวลผล / ข้าม` instead of implying `last_poll_messages` equals created bills.
- Verified health on all three backend/frontend ports and Thaisunsport Phase 1 flags. On BillFlow main, cleared dedup only for message/order `260404V08VQU10`, restarted backend, and poll created bill `67c0be5b-9247-4945-9dc0-85ad498243cf` with status `pending`.
- 2026-05-11 14:34 +07: Shared Phase 1 Shopee product-image extraction fix deployed to all three instances.
- Scope: exclude Shopee tracking/open pixel URLs from `source_image_url` and prefer product CDN URLs such as `cf.shopee.co.th/file/th-*`; existing BillFlow main item `ef15ccb5-c164-476e-84bb-3ef6657e531d` was updated to the verified product image URL.
- Verified backend health on all three instances; product image URL returned `HTTP/2 200` and `content-type: image/jpeg`.
