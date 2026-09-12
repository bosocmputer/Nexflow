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
- หน้า `/settings/tiktok-shop` และ feature flags แยกแต่ละ tenant

ยังไม่เปิด:

- webhook reconciliation และการแปลง snapshots เป็น Nexflow bills
- webhooks และ realtime queue
- stock write, fulfillment, shipping label, cancellation และ Auto SML
- finance/settlement

API อ้างอิงหลัก: [Authorization overview](https://partner.tiktokshop.com/docv2/page/authorization-overview-202407), [Create your app](https://partner.tiktokshop.com/docv2/page/create-your-app), [Access scope](https://partner.tiktokshop.com/docv2/page/access-scope), [Get Authorized Shops](https://partner.tiktokshop.com/docv2/page/get-authorized-shops), [Get Order List](https://partner.tiktokshop.com/docv2/page/get-order-list-202309), [Get Order Detail](https://partner.tiktokshop.com/docv2/page/get-order-detail-202507), [Get Price Detail](https://partner.tiktokshop.com/docv2/page/get-price-detail-202407), [API versioning](https://partner.tiktokshop.com/docv2/page/api-versioning)

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

`/internal/` และ `/webhook/` ตอบ 404 จาก edge จนกว่า webhook receiver ที่ตรวจ signature และ durable queue จะพร้อม

## AOY tenant configuration

ค่าของ AOY หลัง Central Gateway health ผ่าน:

```dotenv
TIKTOK_SHOP_OPEN_API_ENABLED=true
TIKTOK_SHOP_GATEWAY_BASE_URL=<internal or HTTPS gateway URL>
TIKTOK_SHOP_GATEWAY_PUBLIC_URL=https://tiktok-shop-gateway.nextstep-soft.com
TIKTOK_SHOP_GATEWAY_TENANT=aoy
TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET=<derived AOY secret>
TIKTOK_SHOP_ORDER_SYNC_ENABLED=false
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

## AOY OAuth UAT

1. Backup AOY database ก่อนใช้ migration 095-098
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

UAT รอบนี้ถือว่าผ่านเมื่อ OAuth สำเร็จหนึ่งครั้ง, connection metadata ตรงร้าน AOY, Order List/Detail/Price Detail แบบ read-only ตรงกับ Seller Center, snapshot replay เป็นหนึ่งแถว, manual และ scheduled reconciliation ผ่านโดย watermark/overlap ถูกต้อง, ไม่มี Bill/SML/notification side effect, ไม่มี duplicate/cross-tenant row และไม่มี secret/PII ที่ไม่จำเป็นใน tenant database หรือ browser response

## Rollback

- ปิด AOY แบบ atomic ด้วย `python3 scripts/tiktok_gateway_tenant_mode.py --target aoy --open-api-enabled false` แล้ว deploy AOY ใหม่
- หยุด Central Gateway หากพบ credential, routing หรือ callback anomaly
- ไม่ลบ migration 095-098 และไม่ลบ connection/snapshot/run rows ระหว่าง incident; เก็บไว้เป็น audit evidence
- Revoke seller authorization ใน Partner Center เมื่อ token อาจรั่วหรือผูกร้านผิด tenant
