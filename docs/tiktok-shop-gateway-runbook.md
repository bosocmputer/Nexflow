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
- หน้า `/settings/tiktok-shop` และ feature flags แยกแต่ละ tenant

ยังไม่เปิด:

- ดึงและแปลง TikTok orders เข้า Nexflow bills
- webhooks และ realtime queue
- stock write, fulfillment, shipping label, cancellation และ Auto SML
- finance/settlement

API อ้างอิงหลัก: [Authorization overview](https://partner.tiktokshop.com/docv2/page/authorization-overview-202407), [Create your app](https://partner.tiktokshop.com/docv2/page/create-your-app), [Access scope](https://partner.tiktokshop.com/docv2/page/access-scope), [Get Authorized Shops](https://partner.tiktokshop.com/docv2/page/get-authorized-shops), [API versioning](https://partner.tiktokshop.com/docv2/page/api-versioning)

## Partner Center gates

ก่อน deploy ต้องเห็น app/service ที่เปิด API แล้ว และต้องตรวจค่าต่อไปนี้ใน Partner Center:

1. Service `Nextstep Software & Hardware`, Service ID `7683174272727025429`, สถานะ Open
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
TIKTOK_SHOP_GATEWAY_SERVICE_ID=7683174272727025429
TIKTOK_SHOP_GATEWAY_APP_KEY=<Partner Center App Key>
TIKTOK_SHOP_GATEWAY_APP_SECRET=<Partner Center App Secret>
```

Key ทั้งสามต้องไม่ซ้ำกัน Gateway จะไม่ start หาก config ไม่ครบ, URL ไม่ใช่ HTTPS หรือ key ไม่ถูกต้อง

## AOY tenant configuration

ค่าของ AOY หลัง Central Gateway health ผ่าน:

```dotenv
TIKTOK_SHOP_OPEN_API_ENABLED=true
TIKTOK_SHOP_GATEWAY_BASE_URL=<internal or HTTPS gateway URL>
TIKTOK_SHOP_GATEWAY_PUBLIC_URL=https://tiktok-shop-gateway.nextstep-soft.com
TIKTOK_SHOP_GATEWAY_TENANT=aoy
TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET=<derived AOY secret>
VITE_ENABLE_TIKTOK_SHOP_API=true
```

`TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET` ต้อง derive จาก Central Gateway internal master key ด้วย tenant slug `aoy` ห้าม reuse ค่า Shopee หรือคัดลอกจาก tenant อื่น

Demo, Lanboon และ Ploy ต้องคง `TIKTOK_SHOP_OPEN_API_ENABLED=false` และ `VITE_ENABLE_TIKTOK_SHOP_API=false` จนกว่าจะผ่าน preflight ของ tenant นั้นเอง

## AOY OAuth UAT

1. Backup AOY database ก่อนใช้ migration 095-096
2. Deploy Central Gateway และตรวจ `/health` ได้ HTTP 200 พร้อม database `ok`
3. Deploy AOY โดยยังปิด feature flag แล้วตรวจ backend/frontend health
4. เปิด AOY backend และ frontend flags เท่านั้น
5. เข้า `/settings/tiktok-shop` ด้วย admin
6. ตรวจ callback URL บนหน้าจอให้ตรง Partner Center
7. กดเชื่อมต่อและเลือกเฉพาะร้าน AOY
8. หลัง callback ตรวจว่าหน้าร้านแสดง Shop ID, shop code, region และสอง read scopes
9. ตรวจ Central Gateway ว่ามี connection ของ tenant `aoy` เท่านั้น และ token ไม่ปรากฏใน response/log
10. ทดสอบ OAuth state เดิมซ้ำ ต้องถูกปฏิเสธ

UAT รอบนี้ถือว่าผ่านเมื่อ OAuth สำเร็จหนึ่งครั้ง, connection metadata ตรงร้าน AOY, ไม่มี duplicate/cross-tenant row และไม่มี secret ใน tenant database หรือ browser response

## Rollback

- ปิด `TIKTOK_SHOP_OPEN_API_ENABLED` และ `VITE_ENABLE_TIKTOK_SHOP_API` ของ AOY
- หยุด Central Gateway หากพบ credential, routing หรือ callback anomaly
- ไม่ลบ migration 095-096 และไม่ลบ connection rows ระหว่าง incident; เก็บไว้เป็น audit evidence
- Revoke seller authorization ใน Partner Center เมื่อ token อาจรั่วหรือผูกร้านผิด tenant
