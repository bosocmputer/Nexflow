# SML API Migration — billflow main + henna

## เป้าหมาย
เปลี่ยนจากเรียก SML REST API (192.168.2.248:8080) โดยตรง  
→ ไปใช้ **sml-api-bybos** (localhost:8200) เป็น proxy กลาง

sml-api-bybos expose endpoints ที่ `/api/v1/` ทั้งหมด  
Swagger docs: http://192.168.2.109:8200/docs

---

## สถานะ (2026-05-22) — ✅ พร้อมใช้งานบน BillFlow main และ BillFlow Henna

### การเปลี่ยนแปลงที่ทำแล้ว

| ส่วน | สิ่งที่เปลี่ยน |
|---|---|
| `services/sml/saleorder_client.go` | path: `/SMLJavaRESTService/v3/api/saleorder` → `/api/v1/ic/sale-orders` |
| `services/sml/saleinvoice_client.go` | path: `/SMLJavaRESTService/restapi/saleinvoice` → `/api/v1/ic/sale-invoices` |
| `services/sml/purchaseorder_client.go` | path: `/SMLJavaRESTService/v3/api/purchaseorder` → `/api/v1/ic/purchase-orders` |
| `services/sml/product_client.go` | path: `/SMLJavaRESTService/v3/api/product` → `/api/v1/ic/products` |
| `services/sml/party_client.go` | path: customer/supplier → `/api/v1/ar/customers`, `/api/v1/ap/suppliers` |
| `services/sml/warehouse_client.go` | path: `/SMLJavaRESTService/warehouse/v4` → `/api/v1/ic/warehouses` |
| `services/catalog/service.go` | path: `/product/v4` → `/api/v1/ic/products` |
| `config/config.go` | default `ShopeeSMLURL` ยังเป็น 192.168.2.248 (fallback) |
| **runtime app settings** | `sml.rest_base_url=http://172.24.0.1:8200` บน main + henna ✅ |
| **server `.env`** | ยังอาจมีค่าประวัติเดิมได้ แต่ runtime ใช้ค่าใน `app_settings` ก่อน |

### Auth headers — ไม่ต้องเปลี่ยน

billflow ส่ง `guid` + `databaseName` — sml-api-bybos รับทั้งสองแบบ:
- `guid: smlx` → ผ่าน API key check (`API_KEYS=dev-key,smlx`)
- `databaseName: SML1_2026` → ผ่าน tenant check

---

## Endpoints ที่เปลี่ยน

| เดิม (SML 248 direct) | ใหม่ (sml-api-bybos) |
|---|---|
| `POST /SMLJavaRESTService/v3/api/saleorder` | `POST /api/v1/ic/sale-orders` |
| `POST /SMLJavaRESTService/restapi/saleinvoice` | `POST /api/v1/ic/sale-invoices` |
| `POST /SMLJavaRESTService/v3/api/purchaseorder` | `POST /api/v1/ic/purchase-orders` |
| `POST /SMLJavaRESTService/v3/api/product` | `POST /api/v1/ic/products` |
| `GET /SMLJavaRESTService/v3/api/product/{code}` | `GET /api/v1/ic/products/{code}` |
| `GET /SMLJavaRESTService/v3/api/customer` | `GET /api/v1/ar/customers` |
| `GET /SMLJavaRESTService/v3/api/supplier` | `GET /api/v1/ap/suppliers` |
| `GET /SMLJavaRESTService/warehouse/v4` | `GET /api/v1/ic/warehouses` |
| `GET /SMLJavaRESTService/product/v4` (catalog sync) | `GET /api/v1/ic/products` |

---

## การตั้งค่า .env บน server

```
# ~/billflow/.env (ตั้งค่าแล้ว)
SHOPEE_SML_URL=http://172.24.0.1:8200   ← sml-api-bybos ผ่าน Docker gateway จาก billflow-backend container
SHOPEE_SML_GUID=smlx
SHOPEE_SML_PROVIDER=SMLGOH
SHOPEE_SML_CONFIG_FILE=SMLConfigSMLGOH.xml
SHOPEE_SML_DATABASE=SML1_2026
```

---

## ขั้นตอนทดสอบ

1. เปิด http://192.168.2.109:3010/bills
2. เลือกบิลที่ status = `pending` หรือ `failed`
3. กด Retry → ดู log

```bash
docker logs billflow-backend --tail=50 | grep -i "sml\|retry\|error"
```

4. ตรวจว่า doc ขึ้นใน SML UI
5. ทดสอบ import Shopee/Lazada/TikTok Excel ครบ flow
6. ทดสอบ `ส่ง SML ทั้งหมด` ผ่าน async bulk job อย่างน้อย 1 บิล แล้วค่อยขยับเป็น 5-10 บิล

---

## สถานะแต่ละ instance ล่าสุด

| Project | URL | สถานะ |
|---|---|---|
| billflow (main) | :3010 | ใช้ `sml-api-bybos` ผ่าน `sml.rest_base_url=http://172.24.0.1:8200` |
| billflow-henna | :3030 | ใช้ `sml-api-bybos` ผ่าน `sml.rest_base_url=http://172.24.0.1:8200` |
| billflow-thaisunsport | :3020 | ยังไม่ migrate รอบนี้ (คงสถานะเดิม Phase 1) |

หมายเหตุ: Thaisunsport จะ migrate เมื่อเปิด scope Phase 1+ ฝั่งขายและมี UAT ตามแผน

---

## sml-api-bybos reference

- **Container**: `sml-api-bybos-sml-api-1` (healthy)
- **Port**: 8200
- **Health**: `curl http://localhost:8200/health` → `{"status":"ok"}`
- **Swagger**: http://192.168.2.109:8200/docs
- **Source**: `/home/bosscatdog/sml-api-bybos`
- **API_KEYS**: `dev-key,smlx`

## Latest verification — 2026-05-18

- Deployed `sml-api-bybos` to `192.168.2.109:8200`.
- Swagger UI serves local embedded official Swagger UI assets, not CDN; OpenAPI is available at `/docs/openapi.json` and `/openapi.json`.
- Live smoke passed: `/health`, `/health/ready` with `SML1_2026`, `/docs`, `/openapi.json`.
- Master reads passed on `SML1_2026`: customers `1004`, suppliers `500`, products `3005`, warehouses `4`, shelves `4`.
- Golden writes passed:
  - SO `BF-APIQA-SO-260518-001` (`trans_flag=36`)
  - SI `BF-APIQA-SI-260518-001` (`trans_flag=44`)
  - PO `BF-APIQA-PO-260518-001` (`trans_flag=6`)
- Product create passed: `BFAPIQAPRD260518001`; duplicate returns `duplicate_product_code`.
- BillFlow main backend was rebuilt/restarted with the new response parser; startup cache shows `warehouse_cache_refreshed warehouses=4 shelves=4` and `party_cache_refreshed customers=1004 suppliers=500`.

## Product image DB operations — 2026-05-20

- BillFlow main now lazy-loads SML product images through `sml-api-bybos`; BillFlow stores image metadata only in `sml_catalog`.
- The active product tenant `SML1_2026` uses image DB `sml1_2026_images`.
- Apply and verify the image lookup index with [../scripts/apply-sml-image-index.sh](../scripts/apply-sml-image-index.sh) before switching `/settings/instance` to a new SML tenant or a restored SML PostgreSQL server.
- The operational runbook is [sml-image-db-maintenance.md](sml-image-db-maintenance.md).

## doc_format_code — dynamic from /settings/channels (2026-05-22)

ก่อนหน้านี้ `doc_format_code` ถูก hardcode ใน `.env` (`SHOPEE_SML_DOC_FORMAT=INV` เป็นต้น)
ตอนนี้ดึงจาก `channel_defaults.doc_format_code` ซึ่ง admin เลือกได้จาก `/settings/channels`

**Flow:**

```text
admin เปิด /settings/channels → แก้ไขแถว → dropdown "รูปแบบเอกสาร"
→ fetch GET /api/sml/doc-formats?screen_code=PO|SI|SR  (proxy ไป sml-api-bybos)
→ แสดง code + name_1 จาก erp_doc_format ใน SML DB
→ เลือก → prefix = code (เช่น POL), running format = format ตัด @ นำหน้าออก (เช่น YYMM####)
→ บันทึก → channel_defaults.doc_format_code = "POL"

ตอน retry บิล:
bills.go → อ่าน def.DocFormatCode → cfg.DocFormat = "POL"
→ purchaseorder_client / saleorder_client ส่ง doc_format_code: "POL" ใน payload
→ sml-api-bybos insert ลง ic_trans_header.doc_format_code = "POL"
```

**Endpoints ใหม่ที่เพิ่ม:**

| Service | Endpoint | หน้าที่ |
| --- | --- | --- |
| sml-api-bybos | `GET /api/v1/ic/doc-formats?screen_code=PO/SI/SR` | query `erp_doc_format` table ใน SML DB |
| billflow backend | `GET /api/sml/doc-formats?screen_code=` | proxy → sml-api-bybos + ใช้ SML config ที่มีอยู่ |

**SML format field:** `@YYMM####` — `@` หมายถึง "ใช้ code เป็น prefix" ซึ่ง BillFlow ทำอยู่แล้วผ่าน `doc_prefix`
ดังนั้น frontend ตัด `@` ออกก่อน set `doc_running_format`

**calc_flag:** สม-api-bybos คำนวณจาก `route.transType` อัตโนมัติ (ขาย → -1, ซื้อ → 1) ไม่ hardcode

---

## Async bulk SML send — 2026-05-20

- BillFlow main now sends `ส่ง SML ทั้งหมด` through DB-backed async jobs instead of one long frontend request.
- Job data is stored in BillFlow tables `sml_bulk_jobs` and `sml_bulk_job_items`; SML writes still flow through the same BillFlow SML clients and `sml-api-bybos` routes as single-bill retry.
- Worker concurrency is `1` to avoid SML duplicate/race issues.
- Live smoke passed: bulk job `128ceffe-5055-4863-8944-c6ce52301d26` sent bill `20275aed-fe5f-402f-9160-a93a3f5b2ccb` and created SML purchaseorder `BF-PO26050001`.
- Runbook: [sml-bulk-send-jobs.md](sml-bulk-send-jobs.md).
