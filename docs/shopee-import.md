# Shopee Import — การทำงาน

> อัพเดตล่าสุด: 2026-06-22
> สถานะ: ✅ Excel/API import fallback deployed, Shopee Open API live OAuth deployed, Shopee Realtime daily operations live at `/shopee-operations`

---

## ภาพรวม

พนักงานนำ Excel export จาก Shopee Seller Center มา upload ที่ `/import/shopee`
หรือดึง order ผ่าน Shopee Open API หลังเชื่อม OAuth สำเร็จ
ระบบ parse orders → ตรวจ duplicate → แสดง preview → พนักงาน confirm → สร้าง bills ใน Nexflow แล้วให้ admin ตรวจ/Retry ส่งเข้า SML 248
default sale route ปัจจุบันคือ `saleorder`; `saleinvoice` ใช้ได้เมื่อ admin ตั้ง `channel_defaults.endpoint` ให้ชี้ `/SMLJavaRESTService/saleinvoice/v4`

สำหรับ order ประจำวัน ให้เริ่มที่ `/shopee-operations` เป็นหลัก เพราะหน้านั้นรับ
Shopee Live Push/Scheduled sync, แสดง Timeline, payment breakdown snapshot,
สร้างเอกสารจาก order snapshot และจัดการ cancelled-after-SML. หน้า `/import/shopee`
ยังเป็น fallback สำหรับดึงย้อนหลัง/ซ่อมรายการตกหล่น/นำเข้า Excel.

Shopee Open API live cutover checklist อยู่ที่ [`docs/shopee-open-api-live-cutover.md`](shopee-open-api-live-cutover.md)

หน้า `/import/shopee` จะแสดง Open API readiness checklist และรายชื่อร้านที่เชื่อมไว้ ถ้ามีหลายร้านต้องเลือกร้านก่อนดึง API/นำเข้า Excel เพื่อให้ Nexflow บันทึก `shopee_shop_id` และกัน duplicate แยกตามร้าน. API preview ให้เลือกฐานวันที่ `create_time` หรือ `update_time` เท่านั้น, default สถานะเป็นกลุ่มพร้อมออกบิล (`SHIPPED`, `TO_CONFIRM_RECEIVE`, `COMPLETED`), และ Excel fallback ยังทำงานต่อได้เสมอเมื่อ API มีปัญหา.

---

## Flow ทั้งหมด

```
พนักงาน
  │
  ▼
/import/shopee (ShopeeImport.tsx)
  │
  ├── 1. กด "เลือกไฟล์ Shopee"
  │         │
  │         ▼
  │   GET /api/settings/shopee-config
  │   ← config pre-filled จาก env vars
  │
  ├── 2. Preflight config
  │   - ต้องมี channel_defaults สำหรับ Shopee sale ก่อน
  │   - UI block file picker ถ้า config ยังไม่พร้อม
  │   - Open API card แสดง readiness/error state + connected shops
  │   - ถ้ามีหลายร้าน active ต้องเลือกร้านก่อน preview/import
  │
  ├── 3. เลือกไฟล์ .xlsx จาก Shopee Seller Center
  │
  ├── 4. POST /api/import/shopee/preview
  │         │
  │         ▼
  │   Backend:
  │   - parse Excel (column names ภาษาไทย hardcoded)
  │   - exclude สถานะ: "ที่ต้องจัดส่ง", "ยกเลิกแล้ว"
  │   - dedup check: SELECT FROM bills WHERE source='shopee' AND shop_id + order_id ตรงกัน
  │   - stamp preview rows with shopee_shop_id / shopee_connection_id / shopee_shop_label เมื่อเลือก connection
  │   - ไม่ write DB ใน preview
  │         │
  │         ▼
  │   Preview table: แสดงทุก order
  │   - order_id, วันที่, ชื่อสินค้า, qty, ราคา
  │   - badge เหลือง "ซ้ำ" ถ้า order นั้น import แล้ว
  │   - non-duplicate pre-checked อัตโนมัติ
  │
  ├── 5. พนักงานเลือก orders ที่ต้องการ → กด "ยืนยัน Import"
  │
  └── 6. POST /api/import/shopee/confirm
            │
            ▼
      Backend (ต่อ order):
      - GET /SMLJavaRESTService/v3/api/product/{sku}
        → flat response: start_sale_unit/wh/shelf
        → data=null ถ้าไม่พบ SKU → ใช้ config defaults
      - บันทึก bill + items ลง DB พร้อม candidates/artifacts
      - raw_data เก็บ order_id + shopee_shop_id + shopee_connection_id + shopee_shop_label
      - ยังไม่ส่ง SML ทันที
      - admin เปิด bill → route preview → Retry
      - Retry default: POST /SMLJavaRESTService/v3/api/saleorder
            │
            ▼
      แสดง results: created / skipped / failed
```

## Shopee Open API Preview

`POST /api/import/shopee/api/preview` เป็น preview-only และยังไม่สร้าง bill:

| Field | Behavior |
|---|---|
| `connection_id` | ระบุร้านที่เชื่อม OAuth แล้ว; จำเป็นเมื่อมีหลายร้าน |
| `time_from` / `time_to` | ช่วงวันที่ไม่เกิน 15 วันตามข้อจำกัด Shopee |
| `time_range_field` | รองรับเฉพาะ `create_time` และ `update_time`; `pay_time` จะถูก reject ด้วย error อ่านง่าย เพราะ Shopee `get_order_list` ไม่รองรับ |
| `order_status` | `ready_to_bill` default = `SHIPPED`, `TO_CONFIRM_RECEIVE`, `COMPLETED`; รองรับ `all` และสถานะแยกที่ UI แสดง |
| `page_size` | default 50 และไม่เกิน 50 ตาม detail batch limit |

เมื่อ Shopee ตอบ `more=true` หรือผลรวมหลาย status เกิน 50 รายการ UI จะเตือนและ block confirm เพื่อกัน import ตกหล่น ให้ลดช่วงวันที่หรือเลือกสถานะแยกก่อนนำเข้า.
หมายเหตุ live API: Shopee `get_order_list` ไม่รับ `TO_CONFIRM_RECEIVE` เป็น filter โดยตรง แม้ order detail มีสถานะนี้จริง ดังนั้น Nexflow จะ fetch รายการกว้างแล้วกรองสถานะจาก order detail สำหรับกลุ่มพร้อมออกบิลและสถานะนี้.

API preview mapping เพิ่มเติม:

- `shipping_amount` ใช้ `actual_shipping_fee` และ fallback เป็น `estimated_shipping_fee`.
- `package_number` อ่านจาก `package_list[0].package_number`; `tracking_no` fallback เป็น tracking ใน package หรือ package number.
- `shipping_carrier` อ่านจาก `shipping_carrier` และ fallback เป็น `checkout_shipping_carrier`.
- `cod` แสดงใน expanded row เพื่อให้ admin ตรวจวิธีชำระเงิน.
- `amount_mismatch` เทียบ `item_gross + shipping_amount - discount_amount` กับยอดชำระ ไม่ false-positive จากค่าส่ง.
- สินค้าที่ไม่มี SKU จะใช้ชื่อสินค้า + model name เป็น raw matching key และยังเข้า review-first ตาม threshold mapping/catalog เดิม.

## Shopee Open API Readiness

หน้า `/import/shopee` อ่านสถานะจาก:

```
GET /api/settings/shopee-api/status
GET /api/shopee-api/connections
```

ค่าที่ UI ใช้ตัดสินใจ:

| Field | ใช้ทำอะไร |
|---|---|
| `can_connect` | เปิด/ปิดปุ่มเชื่อมต่อ OAuth |
| `can_fetch` | เปิด/ปิดปุ่มดึง order preview |
| `blocking_reason` | ข้อความหลักในกล่อง “สิ่งที่ต้องทำต่อ” |
| `checks[]` | checklist แยกข้อ: config, redirect, environment, approval, OAuth, token, last sync |
| `token_state` | แสดง token `valid`, `refresh_required`, `expired`, `missing` |

`/api/shopee-api/connections` คืนรายชื่อร้านแบบไม่ส่ง token กลับ frontend:

| Field | ใช้ทำอะไร |
|---|---|
| `id` | connection id ที่ frontend ส่งกลับใน preview/confirm |
| `shop_id` / `merchant_id` | ตัวตนร้านจาก Shopee |
| `label` | ชื่อที่ admin ตั้งเองเพื่อแยกร้าน |
| `disabled_at` | soft-disable ร้านที่ยังไม่ต้องใช้ |
| `token_state` / `can_fetch` | บอกว่าร้านนั้นดึง order ได้หรือควร reconnect |

Admin สามารถเปลี่ยน label หรือ soft-disable ร้านผ่าน `PATCH /api/shopee-api/connections/:id` ได้. Token, Partner Key, Access Token และ Refresh Token ห้ามส่งออก UI หรือ log.

Error จาก Shopee API ส่งกลับแบบ structured:

```json
{
  "error": "ข้อความที่ admin อ่านแล้วรู้ว่าต้องทำอะไร",
  "error_code": "RATE_LIMIT",
  "retryable": true
}
```

UI จะแสดง error เหล่านี้บน Open API card และ reload status หลังเกิด error เพื่อให้เห็น last sync error/token state ล่าสุด.

---

## Column Names (Hardcoded)

ไฟล์ Excel จาก Shopee Seller Center ใช้ชื่อ column ภาษาไทยคงที่ — ไม่ต้อง configure:

| Field ในระบบ | Column Name ใน Excel |
|---|---|
| order_id | หมายเลขคำสั่งซื้อ |
| status | สถานะการสั่งซื้อ |
| order_date | วันที่ทำการสั่งซื้อ |
| product_name | ชื่อสินค้า |
| sku | เลขอ้างอิง SKU (SKU Reference No.) |
| price | ราคาขาย |
| qty | จำนวน |

**หมายเหตุ:** Lazada ใช้ column mapping จาก DB (admin config ได้จาก `/settings`) เพราะ format อาจต่างกัน

---

## สถานะที่ Exclude

orders ที่มีสถานะเหล่านี้จะถูกข้ามโดยอัตโนมัติ (ไม่แสดงใน preview):

- `ที่ต้องจัดส่ง`
- `ยกเลิกแล้ว`

---

## Dedup Logic

ก่อน preview/confirm แต่ละ order ระบบตรวจสอบซ้ำใน scope ร้านเดียวกัน:

```sql
SELECT COUNT(*) FROM bills
WHERE source = 'shopee'
AND COALESCE(NULLIF(raw_data->>'shopee_shop_id', ''), 'legacy') = $1
AND raw_data->>'order_id' = $2
```

ถ้า > 0 → แสดง badge สีเหลือง "ซ้ำ" ใน preview table และ uncheck อัตโนมัติ

Migration `045_shopee_multi_shop.sql` เปลี่ยน unique index จาก `order_id` อย่างเดียวเป็น `(shop_id, order_id)` เพื่อรองรับหลายร้านที่อาจมีเลขคำสั่งซื้อชนกันในอนาคต. Records เก่าที่ไม่มี `shopee_shop_id` จะอยู่ใน bucket `legacy`.

---

## Legacy SML 248 Connection

ส่วนนี้ใช้กับ Shopee Excel/API import fallback ที่สร้าง `saleorder` ผ่าน SML 248.
Shopee Realtime daily flow และ cancelled-after-SML CN ใช้ route/config ที่ระบุใน
[`docs/current-state.md`](current-state.md) และ `sml-api-bybos` tenant `aoy`.

```
Base URL:  http://192.168.2.248:8080
Headers (ทุก request):
  guid:             SHOPEE_SML_GUID        (smlx)
  provider:         SHOPEE_SML_PROVIDER    (SMLGOH)
  configFileName:   SHOPEE_SML_CONFIG_FILE  (SMLConfigSMLGOH.xml)
  databaseName:     SHOPEE_SML_DATABASE    (SML1_2026)
```

**Legacy config ที่ใช้กับ SML 248 route นี้:**
```bash
SHOPEE_SML_URL=http://192.168.2.248:8080
SHOPEE_SML_DOC_FORMAT=INV
SHIPPED_SML_DOC_FORMAT=PO
```

ค่า `party_code`, endpoint, doc prefix/running format, WH/Shelf/VAT override อยู่ใน `channel_defaults` และจัดการผ่าน `/settings/channels`

**ทดสอบ connection:**
```bash
curl "http://192.168.2.248:8080/SMLJavaRESTService/v3/api/product/CON-01000" \
  -H "guid: smlx" \
  -H "provider: SMLGOH" \
  -H "configFileName: SMLConfigSMLGOH.xml" \
  -H "databaseName: SML1_2026"
```

**SKU จริงใน SML 248 (ic_inventory):**

| Series | ตัวอย่าง SKU | หน่วย |
|---|---|---|
| CON-xxxxx | CON-01000 | ถุง |
| STEEL-xxxxx | STEEL-01001 | เส้น |
| PLUMB-xxxxx | PLUMB-01002 | ท่อน |
| ROOF-xxxxx | ROOF-01006 | แผ่น |

⚠️ **ไฟล์ Excel ทดสอบต้องใช้ SKU ที่มีอยู่จริงใน ic_inventory** — REST-00002 ไม่มีใน SML 248

---

## Product Lookup

```
GET /SMLJavaRESTService/v3/api/product/{sku}

Response (flat — ไม่มี nested object):
  {"success":true,"data":{"code":"CON-01000","unit_standard":"ถุง",
                           "start_sale_unit":"ถุง","start_sale_wh":"WH-01",
                           "start_sale_shelf":"SH-01"}}

  {"success":true,"data":null}  ← ไม่พบ SKU ใน SML

Response fields ที่ใช้ (priority):
  data.start_sale_unit   → unit_code  (ก่อน)
  data.unit_standard     → unit_code  (fallback ถ้า start_sale_unit ว่าง)
  data.start_sale_wh     → wh_code
  data.start_sale_shelf  → shelf_code
```

ถ้า data=null → ใช้ค่า config defaults (WHCode, ShelfCode, UnitCode จาก env)

⚠️ **SHOPEE_SML_UNIT_CODE ต้องไม่ว่าง** — SML reject เมื่อ `unit_code=""`
ตั้ง fallback เช่น `SHOPEE_SML_UNIT_CODE=ถุง`

---

## VAT Types

| vat_type | ความหมาย |
|---|---|
| 0 | แยกนอก (ราคาก่อน VAT + VAT แยก) |
| 1 | รวมใน (ราคารวม VAT แล้ว) |
| 2 | ศูนย์% (ไม่มี VAT) |

---

## Saleorder Payload (default retry route)

Shopee sale bills ส่งผ่าน `saleorder_client.go` โดย default endpoint เป็น SML 248 `v3/api/saleorder` และใช้ `items` array

```json
{
  "doc_no": "BF-SO2605xxxx",
  "doc_format_code": "INV",
  "doc_date": "2026-05-06",
  "cust_code": "AR00004",
  "items": [
    {
      "item_code": "CON-01000",
      "unit_code": "ถุง",
      "wh_code": "WH-01",
      "shelf_code": "SH-01",
      "qty": 2,
      "price": 100
    }
  ]
}
```

## Saleinvoice Payload (v4 endpoint)

```json
{
  "doc_no": "250424SHOPEE001",
  "doc_format_code": "INV",
  "doc_date": "2026-04-24",
  "cust_code": "AR00004",
  "is_permium": 0,
  "vat_type": 0,
  "details": [
    {
      "item_code": "CON-01000",
      "unit_code": "ถุง",
      "wh_code": "WH-01",
      "shelf_code": "SH-01",
      "qty": 2,
      "price_exclude_vat": 93.46,
      "sum_amount_exclude_vat": 186.92
    }
  ]
}
```

⚠️ หมายเหตุสำคัญ:
- key ต้องเป็น **`"details"`** ไม่ใช่ `"items"`
- `is_permium` เป็น **int** (0/1) ไม่ใช่ bool — typo ตาม SML API จริง
- ใช้ path `POST /SMLJavaRESTService/saleinvoice/v4` เมื่อ `channel_defaults.endpoint` มีคำว่า `saleinvoice`
- ไม่มี `qty` field แยก (ราคาคำนวณไว้ใน `price_exclude_vat` และ `sum_amount_exclude_vat`)

---

## Retry Policy

- max 3 ครั้ง
- ถ้า fail ทั้ง 3 ครั้ง → bill `status='failed'` บันทึกลง DB
- สามารถ retry ด้วยตนเองผ่าน `POST /api/bills/:id/retry` ใน Web UI

---

## สิ่งที่ต้องทำก่อน Go-Live (Phase 4a)

1. ตั้ง `SHOPEE_SML_UNIT_CODE=ถุง` (หรือหน่วยที่เหมาะสม) ใน `/home/bosscatdog/billflow/.env`
2. ใช้ไฟล์ Excel ที่มี SKU จริง (CON-xxxxx / STEEL-xxxxx ฯลฯ) — ไม่ใช่ REST-00002
3. `docker compose up -d backend` (ไม่ต้อง build ใหม่)
4. ทดสอบที่ `http://192.168.2.109:3010/import/shopee`

**ตรวจสอบ SKU ที่มีใน SML 248:**
```bash
docker run --rm postgres:16-alpine psql \
  'postgresql://postgres:sml@192.168.2.248:5432/sml1_2026' \
  -c "SELECT code, name_1, unit_standard FROM ic_inventory ORDER BY code LIMIT 20;"
```

---

## ไฟล์ที่เกี่ยวข้อง

| ไฟล์ | หน้าที่ |
|---|---|
| `backend/internal/handlers/shopee_import.go` | GetConfig, Preview, Confirm handlers |
| `backend/internal/services/sml/saleorder_client.go` | REST client default สำหรับ Shopee sale |
| `backend/internal/services/sml/saleinvoice_client.go` | REST client สำหรับ `POST /SMLJavaRESTService/saleinvoice/v4` |
| `frontend/src/pages/ShopeeImport.tsx` | UI page + config dialog + preview table |
| `backend/cmd/server/main.go` | routes: shopee-config, shopee/preview, shopee/confirm |
