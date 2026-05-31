# Release Checklist: Pilot Dashboard + Shopee Purchase PO Hardening

วันที่เตรียม release: 2026-05-25

สถานะ: พร้อม commit แล้ว แต่ยังไม่ถือว่า production complete จนกว่า deploy และ SML smoke test จริงจะผ่าน

## Scope

- เพิ่ม Pilot Dashboard metrics 30 วันล่าสุด
- เพิ่ม export สรุป Pilot เป็น Markdown จากหน้า Dashboard
- เพิ่ม pricing package document สำหรับเสนอขาย
- รวม backend changes ที่เกี่ยวกับ Shopee purchase email → SML purchaseorder:
  - `inquiry_type`
  - `remark_5`
  - line `discount_amount`
  - shipping fee เป็น SML item line ตาม `channel_defaults`

## Deploy Targets

| Instance | Target รอบนี้ | เหตุผล |
| --- | --- | --- |
| `billflow` main | ใช่ | demo หลักและใช้ smoke กับ SML จริง |
| `billflow-henna` | หลัง main ผ่าน | ควรรักษา parity สำหรับ Phase 1+ / งานฝั่งซื้อและขาย |
| `billflow-thaisunsport` | ข้ามก่อน | ยังเป็น Phase 1 ฝั่งซื้อและควร deploy เฉพาะเมื่อผู้ใช้สั่ง |

## Local Gate

รันจาก local workspace:

```bash
cd /Users/nontawatwongnuk/dev_bos/billflow/backend
go test ./...

cd /Users/nontawatwongnuk/dev_bos/billflow/frontend
npm run build

cd /Users/nontawatwongnuk/dev_bos/billflow
git diff --check
```

ผลล่าสุดในรอบเตรียม release นี้:

- `go test ./...` ผ่าน
- `npm run build` ผ่าน
- `git diff --check` ผ่าน
- Browser QA local ผ่านบน desktop/mobile สำหรับ Dashboard Pilot export
- Vite ยังมี warning เดิมเรื่อง chunk ใหญ่และ `sonner` dynamic import; ไม่ block deploy

## Migration Gate

Backend จะ run migration ทุกไฟล์ใน `backend/internal/database/migrations` ตอน startup ผ่าน `database.Connect()`.

Migration ที่ต้อง verify บน target DB:

- `047_channel_shipping_item_defaults.sql`
  - `channel_defaults.shipping_item_enabled`
  - `channel_defaults.shipping_item_code`
  - `channel_defaults.shipping_item_unit_code`
- `048_bill_item_discount_amount.sql`
  - `bill_items.discount_amount`

SQL ทั้งสองไฟล์เป็น `ADD COLUMN IF NOT EXISTS` จึง idempotent.

หลัง deploy/restart backend ให้ verify schema:

```bash
ssh bosscatdog@192.168.2.109

docker exec billflow-postgres psql -U billflow -d billflow -c "
SELECT table_name, column_name, data_type
FROM information_schema.columns
WHERE
  (table_name = 'channel_defaults'
   AND column_name IN ('shipping_item_enabled', 'shipping_item_code', 'shipping_item_unit_code'))
  OR
  (table_name = 'bill_items'
   AND column_name = 'discount_amount')
ORDER BY table_name, column_name;
"
```

Expected: เห็นครบ 4 columns.

## Deploy Commands

ต้องมี `BF_PASS` สำหรับ SSH password auth:

```bash
cd /Users/nontawatwongnuk/dev_bos/billflow
BF_PASS='***' python scripts/deploy.py
```

Script นี้ deploy ไป main instance:

- remote folder: `/home/bosscatdog/billflow`
- backend: `8090`
- frontend: `3010`
- postgres: `5438`

ถ้าไม่มี `BF_PASS` หรือ SSH access ให้หยุดที่ขั้นนี้ ห้าม mark release ว่า complete.

## Post-Deploy Preflight

รันจาก local:

```bash
cd /Users/nontawatwongnuk/dev_bos/billflow
scripts/preflight-main.sh
```

หรือรันแบบ explicit:

```bash
BF_HOST=192.168.2.109 BACKEND_PORT=8090 FRONTEND_PORT=3010 SML_API_PORT=8200 SML_TENANT=SML1_2026 scripts/preflight-main.sh
```

Expected:

- backend `/health` ok
- frontend `/login` HTTP 200
- `sml-api-bybos` `/health` ok
- `sml-api-bybos` `/health/ready` ok สำหรับ tenant `SML1_2026`
- `/openapi.json` ok

ตรวจ backend migration logs:

```bash
ssh bosscatdog@192.168.2.109
docker logs billflow-backend 2>&1 | grep -i 'migration applied' | tail -20
docker logs billflow-backend 2>&1 | grep -i 'fatal\|panic\|error' | tail -50
```

## Browser QA

หลัง deploy ให้เปิด main frontend จริง:

- `http://192.168.2.109:3010/login`
- หรือ Quick Tunnel URL ล่าสุดจาก:

```bash
ssh bosscatdog@192.168.2.109
grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' /tmp/billflow-tunnel.log | tail -1
```

Checklist:

- Login ได้
- `/dashboard` เห็นการ์ด `ผลลัพธ์ Pilot 30 วัน`
- ปุ่ม `คัดลอกสรุป` ใช้งานได้ หรือ fallback ไม่ error
- ปุ่ม `ดาวน์โหลด .md` ได้ไฟล์ `billflow-pilot-summary-YYYY-MM-DD.md`
- `/logs` เปิดได้จากปุ่ม `ดูหลักฐานใน logs`
- Mobile width ไม่มี horizontal overflow

## SML Purchaseorder Smoke

ต้องทดสอบกับบิลซื้อ Shopee shipped email จริงหรือบิล test ที่สร้างจาก email pipeline หลัง deploy.

ก่อนส่ง:

1. เปิด `/settings/channels`
2. ตรวจ row:
   - channel = `shopee_shipped`
   - bill_type = `purchase`
   - endpoint = purchaseorder route
   - doc format / prefix ถูกต้อง
   - warehouse / shelf / VAT ถูกต้อง
   - shipping item enabled เฉพาะเมื่อมี item code ที่ SML รับจริง
3. เปิด `/bills?status=pending` หรือ `/bills?status=needs_review`
4. เลือกบิล Shopee purchase ที่มี:
   - order id
   - seller name
   - shipping fee ถ้าจะ smoke shipping item
   - discount ถ้าจะ smoke line discount
5. ตรวจ item mapping ให้ครบ
6. กดส่ง SML และใส่ purchase payload fields ให้ครบ โดยเฉพาะ `inquiry_type`

หลังส่งสำเร็จ ให้ verify DB payload:

```bash
ssh bosscatdog@192.168.2.109

docker exec billflow-postgres psql -U billflow -d billflow -c "
WITH cfg AS (
  SELECT shipping_item_code
  FROM channel_defaults
  WHERE channel = 'shopee_shipped' AND bill_type = 'purchase'
),
latest AS (
  SELECT id, status, sml_doc_no, sml_payload, sml_response, sent_at
  FROM bills
  WHERE source = 'shopee_shipped'
    AND bill_type = 'purchase'
    AND status = 'sent'
  ORDER BY sent_at DESC NULLS LAST, created_at DESC
  LIMIT 1
)
SELECT
  latest.id,
  latest.status,
  latest.sml_doc_no,
  latest.sml_payload->>'inquiry_type' AS inquiry_type,
  latest.sml_payload->>'remark_5' AS remark_5,
  COALESCE(jsonb_array_length(latest.sml_payload->'items'), 0) AS item_count,
  EXISTS (
    SELECT 1
    FROM jsonb_array_elements(latest.sml_payload->'items') item
    WHERE NULLIF(item->>'discount_amount', '')::numeric > 0
  ) AS has_discount_line,
  CASE
    WHEN cfg.shipping_item_code = '' THEN NULL
    ELSE EXISTS (
      SELECT 1
      FROM jsonb_array_elements(latest.sml_payload->'items') item
      WHERE item->>'item_code' = cfg.shipping_item_code
    )
  END AS has_shipping_item
FROM latest CROSS JOIN cfg;
"
```

Expected:

- `status = sent`
- `sml_doc_no` ไม่ว่าง
- `inquiry_type` มีค่าตามที่ส่ง
- `remark_5` เป็นเลขคำสั่งซื้อ Shopee
- `has_discount_line = true` เมื่อบิลทดสอบมี discount จริง
- `has_shipping_item = true` เมื่อเปิด shipping item และบิลมี shipping fee จริง

ตรวจ SML response ล่าสุด:

```bash
docker exec billflow-postgres psql -U billflow -d billflow -c "
SELECT id, sml_doc_no, sml_response
FROM bills
WHERE source = 'shopee_shipped'
  AND bill_type = 'purchase'
  AND status = 'sent'
ORDER BY sent_at DESC NULLS LAST, created_at DESC
LIMIT 1;
"
```

## Rollback

Rollback code:

```bash
git log --oneline -5
# เลือก commit ก่อน release นี้
git revert <release_commit_sha>
BF_PASS='***' python scripts/deploy.py
```

Rollback DB:

- ไม่ควร drop columns ทันที เพราะเป็น additive migration และ old code ทนต่อ columns เกินได้
- ถ้าจำเป็นจริง ให้ backup ก่อน:

```bash
ssh bosscatdog@192.168.2.109
mkdir -p /home/bosscatdog/billflow/backups
docker exec billflow-postgres pg_dump -U billflow billflow > /home/bosscatdog/billflow/backups/pre_rollback_$(date +%Y%m%d_%H%M%S).sql
```

## Known Risks Before Marking Complete

- ยังต้องมี server credential (`BF_PASS` หรือ SSH path อื่น) เพื่อ deploy จริง
- SML smoke ต้องใช้บิลจริงที่มี discount/shipping fee ไม่อย่างนั้นจะ verify `has_discount_line` และ `has_shipping_item` ไม่ครบ
- บิลเก่าที่สร้างก่อน `discount_amount` อาจมีค่า default `0`; ควรใช้บิลที่สร้างใหม่หลัง deploy หรือวางแผน backfill อย่างตั้งใจ
- Dashboard Pilot metrics จะเป็น 0 บน frontend ถ้า backend production ยังไม่ได้ deploy code ที่เพิ่ม fields ใหม่
- Vite chunk warning ยังอยู่ แต่ไม่ block deploy

## Release Complete Criteria

ห้ามถือว่า complete จนกว่าครบทุกข้อ:

- Commit ถูกสร้างแล้ว
- Deploy main สำเร็จ
- Migration columns 047/048 verified บน target DB
- `scripts/preflight-main.sh` ผ่าน
- Browser QA หลัง deploy ผ่าน
- SML purchaseorder smoke ส่งจริงผ่าน
- DB payload ยืนยัน `inquiry_type`, `remark_5`, discount, shipping item ตาม scenario
- Logs/SML response ตรวจย้อนหลังได้
