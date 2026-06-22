# คำสั่งซื้อ Shopee UAT Checklist

Updated: 2026-06-22

ใช้ checklist นี้ตอนทดสอบ order จริงกับลูกค้า โดยไม่กด action ที่เปลี่ยนข้อมูลเกินกว่าที่ตั้งใจไว้

## 1. Order เข้า Nexflow

- เปิด `https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations`
- ตรวจว่า order ใหม่อยู่ใน tab ที่ถูกต้อง:
  - `ยังไม่ชำระ` ถ้า Shopee ยังเป็น `UNPAID`
  - `ที่ต้องจัดส่ง` ถ้า Shopee เป็น `READY_TO_SHIP`
  - `กำลังจัดส่ง` ถ้า Shopee เป็น `PROCESSED` หรือ `SHIPPED`
  - `สำเร็จ` ถ้า Shopee เป็น `COMPLETED`
  - `ยกเลิก/คืนเงิน/คืนสินค้า` ถ้า Shopee เป็น `CANCELLED` หรือ `IN_CANCEL`
- เปิด Timeline ของ order แล้วตรวจว่า lifecycle current ตรงกับ Shopee
- Card `ข้อมูลชำระเงิน Shopee` ใน Timeline:
  - ถ้าพร้อม ต้องแสดงยอดลูกค้าชำระ, ยอดสุทธิตาม Shopee escrow, ส่วนต่าง และ fee/voucher/shipping เท่าที่ Shopee ส่งมาจริง
  - ถ้ายังไม่พร้อม กด refresh ได้แบบ read-only และต้องไม่ทำให้หน้า list ช้าหรือ error
- ดู badge แหล่งข้อมูล:
  - `Push` = ได้จาก Shopee Live Push
  - `Sync` = ได้จาก scheduled sync fallback

## 2. Notification

- ตรวจ bell notification ใน Nexflow ว่ามี order ใหม่หรือไม่
- ถ้าตั้ง LINE recipient แล้ว ให้ตรวจว่า LINE ได้ Shopee rich Flex:
  - ไม่มีชื่อ เบอร์ ที่อยู่ หรือ buyer username
  - เวลาเป็น Asia/Bangkok
  - มี payment method/COD, total, shipping/package/logistics, items
  - ถ้า payment snapshot พร้อม มี `ยอดสุทธิตาม Shopee escrow` และ fee breakdown
- ถ้า LINE ไม่เข้า ให้ตรวจหน้า `/settings/line-notifications` และ delivery ล่าสุดก่อนทดสอบซ้ำ
- ปุ่มทดสอบใน `/settings/line-notifications` ส่งตัวอย่าง rich Flex ไม่ใช่ event ออเดอร์จริง

## 3. สร้างเอกสารใน Nexflow

- ในหน้า `คำสั่งซื้อ Shopee` กด `สร้างเอกสาร` เฉพาะ order ที่ต้องการทดสอบ
- Dialog ต้องบอกชัดว่า “ยังไม่ส่งเข้า SML”
- หลังสำเร็จ กด `เปิดเอกสาร`
- ตรวจว่าเอกสารไปอยู่ route ที่ตั้งไว้ใน `เส้นทางเอกสาร SML`

## 4. ส่ง SML จากคิวเอกสารเดิม

- เปิดเอกสารจาก `/sales-orders` หรือ `/sale-invoices` ตาม route ที่ตั้งไว้
- ตรวจรายการสินค้า, mapping, customer, ยอดเงิน และ warning
- กดส่ง SML เฉพาะ order ที่อนุมัติให้ทดสอบจริง
- กลับมาที่ `คำสั่งซื้อ Shopee` แล้วเปิด Timeline
- Milestone `ส่ง SML` ต้องเปลี่ยนเป็นส่งแล้ว และมีเลขเอกสาร SML

## 5. จัดส่งและใบปะหน้า

- จัดส่งและพิมพ์ใบปะหน้าจาก Shopee Seller Center
- Nexflow ใช้สำหรับติดตาม status/tracking กลับมาเท่านั้น
- ใน Timeline กด `ตรวจสถานะล่าสุด` ถ้าต้องการดึงสถานะกลับมาทันที

## 6. ยกเลิกหลังส่ง SML

- ถ้า Shopee เปลี่ยนเป็น `CANCELLED` หรือ `IN_CANCEL` หลังมีเลข SML แล้ว หน้า
  `/shopee-operations` ต้องแสดง badge `ต้องสร้างเอกสารยกเลิก SML`
- กด preview ได้เพื่อดูใบขายเดิม, เลข CN, route และยอดก่อนยืนยัน
- Create CN เปิดใน production แต่ต้องใช้ checkbox/confirm และต้องผ่าน SML
  readiness ของ tenant `aoy`; ถ้า SML ไม่พร้อมต้อง block โดยไม่เขียน partial CN

## 7. สิ่งที่ห้ามกดระหว่าง UAT ถ้าไม่ได้อนุมัติชัดเจน

- import confirm
- bulk send
- delete/archive/purge
- settings save/restart
- disable Shopee connection
- ship order ผ่าน API
- create SML cancel document / credit note
- refresh payment breakdown ถ้าไม่ได้ระบุ order ที่จะทดสอบ

## Expected Result

- Order ใหม่เข้าหน้า `คำสั่งซื้อ Shopee` โดยไม่ต้องเข้า Seller Center เพื่อดู order
- ถ้า Live Push ทำงาน `last_update_source` ควรเป็น `push`
- ถ้า Push หลุด scheduled sync ต้องดึง order เข้ามาภายในรอบ sync
- Notification และ LINE ต้องไม่ส่งซ้ำสำหรับ order เดิม
- สร้างเอกสารไม่สร้าง duplicate bill
- SML ยังส่งจากหน้าคิวเอกสารเดิมเท่านั้น ยกเว้น create-CN flow ที่ทำผ่าน
  `/shopee-operations` สำหรับ cancelled-after-SML โดยเฉพาะ
