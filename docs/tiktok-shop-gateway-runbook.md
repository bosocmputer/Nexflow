# TikTok Shop Gateway Runbook

เอกสารนี้กำหนดการเปิด TikTok Shop Open API แบบหลาย tenant/หลายร้านของ Nexflow โดยใช้ AOY เป็น UAT แรก Credentials และ seller tokens ต้องอยู่ใน Central Gateway เท่านั้น

## สถานะ implementation

พร้อมแล้ว:

- สร้างลิงก์ OAuth แบบผูก `tenant`, `user_id`, `return_url`, nonce และอายุ 15 นาที
- consume OAuth state ได้ครั้งเดียว ป้องกัน callback replay
- แลก authorization code และอ่านร้านทั้งหมดจาก `GET /authorization/202309/shops`
- ตรวจ scope `seller.authorization.info` และ `seller.order.info` แบบ fail-closed
- เข้ารหัส access/refresh token ด้วย AES-GCM และ AAD ต่อ tenant + shop + token type
- ป้องกัน shop เดียวถูกผูกข้าม tenant และบันทึกหลายร้านใน transaction เดียว
- tenant อ่านเฉพาะ connection metadata ผ่าน signed internal request ไม่มี App Secret, token หรือ shop cipher
- refresh seller token ก่อนหมดอายุ 10 นาทีด้วย PostgreSQL advisory lock และอัปเดต token ของทุกร้านภายใต้ authorization เดียวใน transaction เดียว
- อ่านรายการออเดอร์ผ่าน `POST /order/202309/orders/search` และยืนยันข้อมูลจริงด้วย `GET /order/202507/orders` สูงสุดครั้งละ 50 Order IDs
- อ่านโครงสร้างราคาผ่าน `GET /order/202407/orders/{order_id}/price_detail` เพื่อแยกราคาสินค้า ส่วนลด ค่าส่ง ภาษี และยอดที่ผู้ซื้อชำระ
- ส่งกลับ tenant เฉพาะข้อมูลสถานะ รายการสินค้า และยอดเงินที่ต้องใช้ โดยไม่ส่งชื่อ ที่อยู่ โทรศัพท์ อีเมล ข้อความผู้ซื้อ หรือ buyer profile
- บันทึก snapshot แบบ manual/read-only ได้สูงสุด 20 Order IDs ต่อครั้ง โดย upsert ที่ `(shop_id, order_id)`, รวม quantity จาก line instances ด้วย `(product_id, sku_id)` และบันทึก Order Detail + Price Detail ที่ผ่าน typed allowlist เท่านั้น
- reconcile snapshot จาก Order List ด้วยช่วงเวลา `[update_time_ge, update_time_lt)` สูงสุด 24 ชั่วโมง, page size 20 และไม่เกิน 10 หน้า โดยอ่าน Detail + Price Detail ซ้ำทุกออเดอร์ก่อน upsert
- มี scheduler แบบสองชั้น: global `TIKTOK_SHOP_ORDER_SYNC_ENABLED` และ per-shop setting ซึ่งค่าเริ่มต้นปิดทั้งคู่; watermark ขยับหลังจบทุกหน้าเท่านั้น และ replay overlap 15 นาทีเพื่อรับ late update
- เก็บ durable run/progress พร้อม lease 30 นาทีและ safe error code; เก็บเฉพาะ SHA-256 ของ opaque page token ไม่เก็บ token ดิบ, upstream payload, credential หรือ buyer PII
- มี internal endpoints ที่ลงลายเซ็นแยก tenant สำหรับ Order List/Detail; public edge ปิด `/internal/` ทั้งหมด
- มี signed `ORDER_STATUS_CHANGE` receiver ที่ตรวจ `Authorization` จาก raw body ก่อน parse, เก็บ typed receipt + hash, deduplicate ด้วย `tts_notification_id`, ส่งต่อผ่าน durable tenant outbox และ refresh exact Order Detail + Price Detail เท่านั้น
- public receiver, Central Gateway delivery worker และ tenant reconciliation worker ใช้ `TIKTOK_SHOP_WEBHOOK_ENABLED=false` เป็นค่าเริ่มต้นและเปิดแยกกันได้
- Custom App ตั้ง `ORDER_STATUS_CHANGE` ต่อร้านผ่าน signed internal operation ซึ่งเรียก official `PUT /event/202309/webhooks`; Gateway บังคับ callback เป็น URL ของตัวเองและไม่รับ URL จาก tenant
- AOY real-webhook UAT ผ่านด้วย controlled order `586030483469993439`: Seller Center เปลี่ยน `AWAITING_SHIPMENT` เป็น `AWAITING_COLLECTION`, Gateway/AOY รับและ reconcile event จริงหนึ่งครั้งโดยไม่มี Marketplace side effect
- หน้า `/settings/tiktok-shop` และ feature flags แยกแต่ละ tenant

ยังไม่เปิดใช้งานจริง:

- การแปลง snapshots เป็น Nexflow bills
- Nexflow stock write, fulfillment/shipping label API, cancellation และ Auto SML
- finance/settlement

API อ้างอิงหลัก: [Authorization overview](https://partner.tiktokshop.com/docv2/page/authorization-overview-202407), [Create your app](https://partner.tiktokshop.com/docv2/page/create-your-app), [Access scope](https://partner.tiktokshop.com/docv2/page/access-scope), [Get Authorized Shops](https://partner.tiktokshop.com/docv2/page/get-authorized-shops), [Get Order List](https://partner.tiktokshop.com/docv2/page/get-order-list-202309), [Get Order Detail](https://partner.tiktokshop.com/docv2/page/get-order-detail-202507), [Get Price Detail](https://partner.tiktokshop.com/docv2/page/get-price-detail-202407), [Webhook configuration](https://partner.tiktokshop.com/docv2/page/configuration-guide), [Webhook overview/signature](https://partner.tiktokshop.com/docv2/page/tts-webhooks-overview), [Order status change](https://partner.tiktokshop.com/docv2/page/1-order-status-change), [API versioning](https://partner.tiktokshop.com/docv2/page/api-versioning)

สำหรับ Custom App ให้ใช้ [Update Shop Webhook](https://partner.tiktokshop.com/docv2/page/update-shop-webhook) ต่อ `shop_cipher`: `PUT /event/202309/webhooks` ด้วย `event_type=ORDER_STATUS_CHANGE` และ callback `https://tiktok-shop-gateway.nextstep-soft.com/webhook/tiktok-shop`. หน้า Manage Webhook ใน Partner Center ระบุว่าเป็น public webhook; หากบันทึก URL แล้วค่าไม่คงอยู่และ switch ยัง disabled ห้ามถือว่า subscribe สำเร็จ ให้ใช้ Event API และเก็บ upstream `request_id` เป็นหลักฐานแทน

## Partner Center gates

ก่อน deploy ต้องเห็น app/service ที่เปิด API แล้ว และต้องตรวจค่าต่อไปนี้ใน Partner Center:

1. Custom API App `Nexflow TikTok Shop Connector`, Service ID `7683750742427944722`, สถานะ Open (Thailand Beta Testing; สูงสุด 25 ผู้ขายที่อนุญาต)
2. Target market Thailand
3. API enabled
4. Redirect URL ตรงทุกตัวอักษรกับ `https://tiktok-shop-gateway.nextstep-soft.com/api/tiktok-shop/callback`
5. Scope อย่างน้อย Seller Authorization Information และ Order Information
6. App Key และ App Secret แสดงในหน้ารายละเอียด app

ห้ามคัดลอก App Secret ลง `.env` ของ AOY หรือ tenant อื่น

การ Save/Publish app, เปลี่ยน scope, เปลี่ยน callback, เปิด webhook และกด authorize ร้าน เป็น external action ต้องตรวจค่าหน้าจอและยืนยันก่อนกดทุกครั้ง

## Central Gateway configuration

ใช้ template `deploy/tiktok-shop-gateway/.env.example` และสร้าง secret ใหม่แยกจาก Shopee:

```bash
openssl rand -base64 32  # token encryption key
openssl rand -base64 32  # internal master key
openssl rand -base64 32  # OAuth signing key
openssl rand -base64 32  # database password
```

ค่าหลัก:

```dotenv
PUBLIC_BASE_URL=https://tiktok-shop-gateway.nextstep-soft.com
TIKTOK_SHOP_API_BASE_URL=https://open-api.tiktokglobalshop.com
TIKTOK_SHOP_GATEWAY_SERVICE_ID=7683750742427944722
TIKTOK_SHOP_GATEWAY_APP_KEY=<Partner Center App Key>
TIKTOK_SHOP_GATEWAY_APP_SECRET=<Partner Center App Secret>
TIKTOK_SHOP_WEBHOOK_ENABLED=false
TIKTOK_SHOP_GATEWAY_TENANT_HTTP_TIMEOUT=10s
```

Key ทั้งสามต้องไม่ซ้ำกัน Gateway จะไม่ start หาก config ไม่ครบ, URL ไม่ใช่ HTTPS หรือ key ไม่ถูกต้อง

### Deploy target และ edge

TikTok Gateway เป็น target แยกและ **ไม่รวมใน `--target all`** เพื่อไม่ให้การ deploy ระบบเดิมพยายาม start service ที่ยังไม่มี credential:

```bash
NX_PASS=... python3 scripts/deploy_nextstep_instances.py --target tiktok-gateway --ref <reviewed-commit>
```

คำสั่งจะตรวจ `.env`, backup `.env` และฐานข้อมูลเดิม, build/recreate เฉพาะ TikTok Gateway, ตรวจ health บน Docker network แล้วจึงเพิ่ม host ไปที่ edge โดยเปิดสาธารณะเฉพาะ:

- `/health`
- `/api/tiktok-shop/callback`

`/internal/` และ `/webhook/` อื่นยังตอบ 404 จาก edge เสมอ; เปิด proxy เฉพาะ `/webhook/tiktok-shop` เท่านั้น เมื่อ Gateway flag ปิด endpoint นี้ตอบ 404 และเมื่อเปิดแต่ไม่มี/ผิด signature ตอบ 401 body ว่าง

## AOY tenant configuration

ค่าของ AOY หลัง Central Gateway health ผ่าน:

```dotenv
TIKTOK_SHOP_OPEN_API_ENABLED=true
TIKTOK_SHOP_GATEWAY_BASE_URL=<internal or HTTPS gateway URL>
TIKTOK_SHOP_GATEWAY_PUBLIC_URL=https://tiktok-shop-gateway.nextstep-soft.com
TIKTOK_SHOP_GATEWAY_TENANT=aoy
TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET=<derived AOY secret>
TIKTOK_SHOP_ORDER_SYNC_ENABLED=false
TIKTOK_SHOP_WEBHOOK_ENABLED=false
VITE_ENABLE_TIKTOK_SHOP_API=true
```

`TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET` ต้อง derive จาก Central Gateway internal master key ด้วย tenant slug `aoy` ห้าม reuse ค่า Shopee หรือคัดลอกจาก tenant อื่น

Demo, Lanboon และ Ploy ต้องคง `TIKTOK_SHOP_OPEN_API_ENABLED=false` และ `VITE_ENABLE_TIKTOK_SHOP_API=false` จนกว่าจะผ่าน preflight ของ tenant นั้นเอง

Provision identity อย่างเดียวโดยไม่เปลี่ยน feature flags:

```bash
python3 scripts/tiktok_gateway_tenant_mode.py --target aoy --identity-only
```

เมื่อ Central Gateway health ผ่าน, Partner Center scopes/callback ผ่านการตรวจ และพร้อมเริ่ม UAT แล้ว จึงเปิด backend/frontend พร้อมกันอย่างชัดเจน:

```bash
python3 scripts/tiktok_gateway_tenant_mode.py --target aoy --open-api-enabled true
NX_PASS=... python3 scripts/deploy_nextstep_instances.py --target aoy --ref <reviewed-commit>
```

deployment จะเพิ่ม network `nexflow-tiktok-shop-gateway_default` ให้ backend เฉพาะ tenant ที่มี `TIKTOK_SHOP_OPEN_API_ENABLED=true` และตรวจ health จากภายใน backend หลัง recreate

AOY UAT ใช้ authenticated tenant routes ต่อไปนี้ (role `admin` หรือ `staff`) โดยทุก route เป็น read-only และยังไม่สร้าง bill:

- `POST /api/tiktok-shop-api/orders/search`
- `POST /api/tiktok-shop-api/orders/detail`
- `POST /api/tiktok-shop-api/orders/price-detail`
- `POST /api/tiktok-shop-api/orders/snapshot` — บันทึก 1–20 Order IDs แบบ atomic; ถ้าออเดอร์ใดโหลด/ตรวจยอดไม่ผ่าน จะไม่บันทึกทั้งชุด
- `POST /api/tiktok-shop-api/orders/reconcile` — explicit window สูงสุด 24 ชั่วโมง; อ่าน Order List แบบเรียง `update_time ASC` แล้ว refresh typed snapshots
- `GET /api/tiktok-shop-api/order-sync-settings` — แสดง global worker state และ setting ของแต่ละร้าน
- `PUT /api/tiktok-shop-api/order-sync-settings/:shop_id` — Admin เท่านั้น; ใช้ `config_version` เพื่อป้องกันการแก้ไขทับกัน

การเปิด scheduled reconciliation ต้องทำตามลำดับ: deploy โดย global flag ยังเป็น `false`, ผ่าน manual canary และตรวจ side-effect counts ก่อน, เปลี่ยน global flag ของ AOY เป็น `true`, แล้วจึงเปิดเฉพาะ Shop ID ของ AOY ผ่าน settings API ค่าแนะนำเริ่มต้นคือ interval 300 วินาทีและ overlap 900 วินาที ห้ามเปิดร้านอื่นหรือ tenant อื่นจากค่าของ AOY

### Order/amount evidence ที่ยืนยันแล้วใน AOY

- Order Detail รุ่น `202507` ส่งหนึ่ง `line_items` entry ต่อสินค้าหนึ่งชิ้น ไม่ได้ส่ง quantity ที่เชื่อถือได้ ต้องรวมจำนวนด้วย `(product_id, sku_id)` และเก็บ line ID รายชิ้นเป็นหลักฐาน
- `seller_sku` ของร้าน AOY อาจว่าง ให้ใช้ `product_id + sku_id` เป็น external variant identity
- ตัวอย่าง production UAT วันที่ 2026-09-12 ตรงกับ Seller Center: ราคาสินค้า 300 บาท, ค่าส่งเดิม 29 บาท, ส่วนลดค่าส่งแพลตฟอร์ม 29 บาท, `item_insurance_fee` 7.49 บาท และผู้ซื้อชำระรวม 307.49 บาท
- `item_insurance_fee` เป็นค่าประกัน/คุ้มครองที่ผู้ซื้อจ่ายให้แพลตฟอร์ม เก็บไว้เพื่อ reconcile ยอดรวม แต่ห้ามสร้างเป็นบรรทัดขายหรือค่าส่งใน SML
- migration 097 เก็บเฉพาะสถานะ เวลา external identity, ยอด reconcile, typed safe JSON, grouped SKU evidence, TikTok request IDs และ content hash ไม่มี buyer/recipient fields และไม่มี queue ที่สร้าง Bill/SML

### Durable snapshot UAT (2026-09-12)

- AOY deploy commit `0b2ae2b`; database backup `pre-deploy-20260912-102557.sql.gz`
- controlled order `585684843131602849` ถูกเรียกผ่าน endpoint snapshot สองครั้งและตอบ HTTP 200 ทั้งคู่ แต่ `(shop_id, order_id)` เหลือหนึ่งแถวพร้อม content hash เดิม
- snapshot ตรงกับหลักฐานจริง: `COMPLETED`, 1 line / 1 SKU / quantity 1, product subtotal 250.00 THB, shipping 0, `item_insurance_fee` 6.42 THB และ buyer payment 256.42 THB
- typed `safe_order` + `safe_price_detail` มี PII key hits 0; จำนวน Bill คงที่ 323 และ `bill_sml_attempts` คงที่ 27
- structured success log 2 รายการ, severe log 0, AOY health HTTP 200 และการเรียก endpoint โดยไม่ authenticate ตอบ HTTP 401
- UAT นี้ไม่ได้เปิด polling, webhook, Bill/SML conversion, LINE notification, fulfillment หรือ stock write

### Bounded reconciliation UAT (2026-09-12)

- AOY deploy commit `e8d3efd` พร้อม additive migration 098; backups คือ `pre-deploy-20260912-105604.sql.gz` และ `pre-deploy-20260912-110050.sql.gz`
- manual run `f6442b20-ebd4-4bde-915c-27699c017118` ใช้ explicit 2-second window รอบ controlled order: HTTP 200, 1 หน้า, พบและ snapshot 1 ออเดอร์
- เปิด global worker ก่อนโดยร้านยัง disabled แล้วผ่าน fail-closed check: ไม่มี scheduled run เกิดขึ้น
- เปิดเฉพาะ AOY shop `7494619203789490654` (`henna_milkford`) ที่ interval 300 วินาที / overlap 900 วินาที; config version เปลี่ยนจาก 1 เป็น 2
- scheduled run แรก `8cee9c57-0293-476b-a10d-e87ecd79cf61` สำเร็จ 1 หน้า, พบและ snapshot 4 ออเดอร์ และ watermark ตรงกับ exclusive window end
- หลัง canary มี snapshot 4 แถวและ PII-key hit 0; Bill 323, SML attempts 27, notifications 537 และ LINE deliveries 252 ไม่เปลี่ยน
- scheduled severe log 0, success log 1; Demo, Lanboon และ Ploy มี global worker เป็น `false`
- public `/internal/` และ `/webhook/` ยังตอบ 404; polling นี้ยังไม่สร้าง Bill/SML/notification/fulfillment/stock ใด ๆ

## AOY OAuth UAT

1. Backup AOY database ก่อนใช้ migration 095-100
2. Deploy Central Gateway ด้วย target `tiktok-gateway` และตรวจ `/health` ได้ HTTP 200 พร้อม database `ok`
3. Deploy AOY โดยยังปิด feature flag แล้วตรวจ backend/frontend health
4. เปิด AOY backend และ frontend flags เท่านั้น
5. เข้า `/settings/tiktok-shop` ด้วย admin
6. ตรวจ callback URL บนหน้าจอให้ตรง Partner Center
7. กดเชื่อมต่อและเลือกเฉพาะร้าน AOY
8. หลัง callback ตรวจว่าหน้าร้านแสดง Shop ID, shop code, region และสอง read scopes
9. เรียก Order List ช่วงเวลาสั้น แล้วเรียก Order Detail ด้วย Order ID ที่ได้; ตรวจ `upstream_request_id`, status, line items และยอดเงิน โดยไม่มี buyer PII ใน response/log
10. ทำซ้ำเมื่อ access token เข้า refresh window; ตรวจว่าร้านทุกแห่งใต้ `open_id` เดียวมี `last_refreshed_at` และ expiry ชุดเดียวกัน ไม่มี partial update
11. ตรวจ Central Gateway ว่ามี connection ของ tenant `aoy` เท่านั้น และ token ไม่ปรากฏใน response/log
12. ทดสอบ OAuth state เดิมซ้ำ ต้องถูกปฏิเสธ
13. เรียก snapshot ของ controlled order เดิมสองครั้ง ต้องเหลือหนึ่งแถว, hash/quantity/yอดตรงกัน, JSON ไม่มี buyer PII และจำนวน Bill/SML attempt ไม่เปลี่ยน
14. เรียก reconciliation แบบ manual ในช่วงเวลาสั้นที่มี controlled order; ต้องสำเร็จ, snapshot ยัง idempotent และจำนวน Bill/SML/notification ไม่เปลี่ยน
15. เปิด global worker ของ AOY แต่ยังปิดทุกร้าน ตรวจว่าไม่มี run ถูก claim จากนั้นเปิดเฉพาะ AOY Shop ID และรอ scheduled run แรก
16. ตรวจ scheduled run ว่า watermark เท่ากับ exclusive window end เฉพาะเมื่อสำเร็จ, ไม่มี raw page token/PII และการ restart/replay ไม่สร้าง snapshot ซ้ำ
17. Deploy migration 100 และโค้ด webhook โดยทั้ง Central Gateway/AOY flag ยังปิด; ตรวจ endpoint ผ่าน edge ตอบ 404 และ side-effect counts ไม่เปลี่ยน
18. เปิด AOY tenant flag ด้วย `python3 scripts/tiktok_gateway_tenant_mode.py --target aoy --webhook-enabled true`, deploy AOY แล้วเปิด Central Gateway flag; request ที่ไม่มี signature ต้องตอบ 401 body ว่าง
19. ส่ง synthetic payload ที่ลงลายเซ็นจากภายใน Gateway โดยไม่แสดง App Secret; ส่งซ้ำ byte-for-byte แล้วต้องมี receipt/outbox/AOY job เพียงหนึ่งชุด และ exact snapshot สำเร็จ
20. ตั้ง Partner Center เฉพาะ `ORDER_STATUS_CHANGE` ไปที่ `https://tiktok-shop-gateway.nextstep-soft.com/webhook/tiktok-shop`; ตรวจ Webhook Log และ real AOY transition หนึ่งรายการก่อนคงสถานะเปิด
21. หลัง real event ตรวจ Bill/SML attempt/notification/LINE/fulfillment/cancellation/stock-write counts ไม่เปลี่ยน และ scheduled polling ยังทำงาน

UAT รอบนี้ถือว่าผ่านเมื่อ OAuth สำเร็จหนึ่งครั้ง, connection metadata ตรงร้าน AOY, Order List/Detail/Price Detail แบบ read-only ตรงกับ Seller Center, snapshot replay เป็นหนึ่งแถว, manual/scheduled/webhook reconciliation ผ่าน, webhook replay เหลือหนึ่ง receipt/job, ไม่มี Bill/SML/notification side effect, ไม่มี duplicate/cross-tenant row และไม่มี secret/PII ที่ไม่จำเป็นใน Gateway, tenant database, log หรือ browser response

### Webhook shadow UAT evidence — 2026-09-12–13

- เปิด `TIKTOK_SHOP_WEBHOOK_ENABLED` เฉพาะ Central Gateway และ AOY; Demo, Lanboon และ Ploy ยังปิด
- public health ของ Central Gateway/AOY ตอบ HTTP 200 และ unsigned webhook ตอบ HTTP 401 body ว่าง
- signed canary สอง notification ถูก replay notification ละสองครั้ง แต่คงเหลือเพียง 2 Gateway receipts, 2 delivered outbox rows และ 2 AOY jobs ที่ `succeeded`
- snapshot/Bill/SML attempt/in-app notification/LINE delivery หลัง canary เท่ากับ `4/323/27/537/252`; ไม่มี side effect เพิ่มและ severe-log scan เป็นศูนย์
- สมัครร้าน AOY `7494619203789490654` (`henna_milkford`) สำหรับ `ORDER_STATUS_CHANGE` ผ่าน official Event API สำเร็จ โดย upstream request ID `20260912210121D15E3BAD860DAB28023E`
- callback ที่ Gateway บังคับใช้คือ `https://tiktok-shop-gateway.nextstep-soft.com/webhook/tiktok-shop`; internal configure operation สำเร็จ HTTP 200 หนึ่งครั้ง
- Central Gateway อยู่ที่ `fd7d858`; backups ล่าสุดคือ `pre-deploy-20260912-124259.sql.gz` และ `pre-deploy-20260912-130002.sql.gz`. AOY backups ก่อน rollout คือ `pre-deploy-20260912-123656.sql.gz` และ `pre-deploy-20260912-123949.sql.gz`
- วันที่ 2026-09-13 ผู้ใช้กำหนด controlled order `586030483469993439` แล้วกดเตรียมจัดส่ง/พิมพ์ฉลากใน Seller Center; TikTok เปลี่ยนสถานะ `AWAITING_SHIPMENT` เป็น `AWAITING_COLLECTION` เวลา 08:55:48 Asia/Bangkok
- Gateway รับ notification `7684832722092099336` เวลา 08:55:49, สร้าง/ส่ง outbox สำเร็จหนึ่งครั้ง; AOY job สำเร็จครั้งแรกเวลา 08:55:52 และ snapshot สดตรงกับ Seller Center
- หลัง real event มี 9 snapshots และ 5 AOY webhook jobs; Bill/SML attempt/in-app notification/LINE delivery ยังคง `323/27/537/252`, severe-log scan เป็นศูนย์ และ Seller Center แสดงรายการ `รอจัดส่ง` เหลือ 0
- real `ORDER_STATUS_CHANGE` shadow UAT ผ่านแล้ว; การพิมพ์ฉลากครั้งนี้ทำโดยผู้ใช้ใน Seller Center และไม่ได้เปิด Nexflow fulfillment/shipping API

### Bill Shadow Preview UAT evidence — 2026-09-13

- AOY deploy commit `abd547e`; database backup `pre-deploy-20260913-022021.sql.gz`
- route `GET /api/tiktok-shop-api/orders/:shop_id/:order_id/bill-shadow-preview` อ่านเฉพาะ local typed snapshot และ local Product Master/Catalog/channel settings ไม่มี TikTok call ใน page-render path
- response บังคับ `shadow_mode=true` และ `can_create_bill=false` ทุกกรณี; UI มีเพียงการตรวจสอบและปิด dialog ไม่มีปุ่มสร้าง Bill หรือส่ง SML
- mapping สำหรับ TikTok API ต้องตรง `source=tiktok`, `account_key=shop:<shop_id>`, `product_id` และ `sku_id` พร้อม active Catalog item/unit; alias ที่ scope `default` ใช้เป็น migration candidate ได้ แต่ห้ามนับว่า API-ready
- controlled order `586030483469993439` แสดงราคาสินค้า 300.00 THB, ค่าส่ง 0, ยอด Bill ที่เสนอ 300.00, ผู้ซื้อจ่าย 307.49 และแยก `item_insurance_fee` 7.49 เป็น platform-only charge ที่ไม่สร้างบรรทัด SML
- blocker เดียวคือยังไม่มี shop-scoped mapping สำหรับ product `1729429119195974110` / SKU `1729429118580984286`; ไม่พบ Bill เดิมของ order นี้
- browser QA ผ่านทั้ง desktop และ 390px โดยไม่ overflow และไม่มี console warning/error; structured log บันทึก `tiktok_shop_bill_shadow_preview_blocked` โดยไม่มี buyer PII หรือ raw payload
- side-effect counts ก่อนและหลัง preview คงเดิมที่ snapshot/webhook job/Bill/SML attempt/in-app notification/LINE delivery = `9/5/323/27/537/252`; severe-log scan เป็นศูนย์
- ขั้นถัดไปคือทำ UI สำหรับให้ผู้ใช้ยืนยัน Product Master item/unit แบบ scoped ต่อ TikTok shop แล้วเรียก shadow preview ซ้ำจนไม่มี blocker; ขั้นนี้ยังห้ามเปิด Bill/SML creation

## Rollback

- ปิด webhook AOY ด้วย `python3 scripts/tiktok_gateway_tenant_mode.py --target aoy --webhook-enabled false`, ตั้ง Central Gateway `TIKTOK_SHOP_WEBHOOK_ENABLED=false`, แล้ว deploy ทั้งสอง service ใหม่
- ลบ/คืนค่า `ORDER_STATUS_CHANGE` callback ใน Partner Center; scheduled polling ยังเป็น recovery path
- หากต้องปิด TikTok ทั้งหมด ให้ใช้ `python3 scripts/tiktok_gateway_tenant_mode.py --target aoy --open-api-enabled false` แล้ว deploy AOY ใหม่
- หยุด Central Gateway หากพบ credential, routing หรือ callback anomaly
- ไม่ลบ migration 095-100 และไม่ลบ connection/snapshot/run/webhook rows ระหว่าง incident; เก็บไว้เป็น audit evidence
- Revoke seller authorization ใน Partner Center เมื่อ token อาจรั่วหรือผูกร้านผิด tenant
