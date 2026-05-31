# BillFlow Phase 1 — Test Checklist

> ใช้ก่อนส่งให้ลูกค้าทดสอบ
> อัปเดตล่าสุด: 2026-05-20
> Scope: Shopee email purchase bill → BillFlow review → SML `purchaseorder`

---

## 0. เตรียมระบบ

- [ ] เปิด BillFlow UI: `http://192.168.2.109:3010`
- [ ] Backend health ผ่าน: `http://192.168.2.109:8090/health`
- [ ] SML server `http://192.168.2.248:8080` เปิดใช้งาน
- [ ] Login ด้วย user admin ได้
- [ ] `VITE_PHASE=1` และเมนู Phase 2+ ไม่รบกวน user test

---

## 1. ตรวจ Config ก่อนทดสอบ

### Email

- [ ] เข้า `/settings/email`
- [ ] IMAP account enabled
- [ ] Test connection ผ่าน
- [ ] Poll ตอนนี้แล้วไม่ error
- [ ] Subject/filter ตรงกับอีเมล Shopee ที่จะทดสอบ

### SML Route

- [ ] เข้า `/settings/channels`
- [ ] API/path เป็น `/SMLJavaRESTService/v3/api/purchaseorder`
- [ ] `doc_format_code` เป็น `PO`
- [ ] doc no format ใช้ prefix สำหรับทดสอบ เช่น `BF-PO`

### Catalog

- [ ] เข้า `/settings/catalog`
- [ ] ค้นหาสินค้าที่จะใช้ทดสอบเจอ
- [ ] ถ้าไม่เจอ ให้สร้างสินค้าใหม่หรือเตรียม mapping ในหน้าบิล

---

## 2. ทดสอบดึงอีเมล

- [ ] ส่ง/เตรียมอีเมล Shopee เข้ากล่องที่ตั้งไว้
- [ ] กด Poll ตอนนี้ หรือรอรอบ poll
- [ ] เข้า `/logs`
- [ ] เห็น log `รับอีเมล Shopee Shipped`
- [ ] เข้า `/bills`
- [ ] เห็นบิลใหม่จาก Shopee
- [ ] เปิดบิลแล้วเห็น artifact ต้นทาง เช่น `shopee-shipped.html` และ `envelope.json`

ข้อมูลที่ต้องเห็นในบิล:

- [ ] เลขคำสั่งซื้อ Shopee / `doc_ref`
- [ ] วันที่เอกสาร
- [ ] รายการสินค้า
- [ ] จำนวน
- [ ] ราคา
- [ ] ยอดรวม

---

## 3. ตรวจรายการสินค้าใน Bill Detail

- [ ] ทุก item มีชื่อสินค้าจากอีเมล
- [ ] ทุก item มีจำนวนถูกต้อง
- [ ] ทุก item มีราคาถูกต้อง
- [ ] ทุก item มีรหัสสินค้า SML (`item_code`)
- [ ] ทุก item มีหน่วย (`unit_code`)
- [ ] ถ้ารหัสสินค้าไม่ถูกต้อง ให้กดจัดการ/เลือกสินค้าจาก SML catalog
- [ ] ถ้าสินค้าไม่มีใน SML ให้สร้างสินค้าใหม่และ map กลับเข้าบิล

ก่อนส่ง ปุ่มส่ง SML ต้องไม่ติด validation เช่น:

- [ ] ไม่มี item ที่ยังไม่ได้จับคู่
- [ ] ไม่มี item ที่ไม่มีหน่วย
- [ ] ไม่มีจำนวน 0
- [ ] ไม่มีราคา 0

---

## 4. ทดสอบ Dialog ส่งเข้า SML

กดส่งเข้า SML แล้วตรวจ dialog:

- [ ] ผู้ขายบังคับเลือก
- [ ] คลังบังคับกรอก
- [ ] พื้นที่เก็บบังคับกรอก
- [ ] ประเภทภาษีบังคับเลือก
- [ ] อัตราภาษี default เป็น `7`
- [ ] เวลาเอกสารเป็นเวลาปัจจุบันหรือแก้ได้
- [ ] Branch code ว่างได้ และถ้าว่างต้องส่ง `""`
- [ ] Sale code ว่างได้ และถ้าว่างต้องส่ง `""`
- [ ] หมายเหตุส่งเข้า SML ได้

ค่าทดสอบตัวอย่าง:

| Field | Value |
|---|---|
| ผู้ขาย | `V-001` |
| คลัง | `WH-01` |
| พื้นที่เก็บ | `SH-01` |
| VAT type | `1` รวมใน หรือ `0` แยกนอก ตามเคสทดสอบ |
| VAT rate | `7` |
| Branch code | ว่างหรือ `001` ตามเคส |
| Sale code | ว่าง |

---

## 5. ตรวจหลังส่ง SML

ใน BillFlow:

- [ ] บิลเปลี่ยนสถานะเป็นส่งแล้ว
- [ ] มีเลขเอกสาร SML เช่น `BF-PO2605....`
- [ ] หัวข้อ “ข้อมูลการส่งเข้า SML” แสดง summary
- [ ] Summary มีผู้ขาย, คู่ค้า, คลัง/พื้นที่เก็บ, ภาษี, จำนวนรายการ, ยอดสุทธิ
- [ ] เปิดข้อมูลที่ส่งเข้า SML แล้วเห็นรายละเอียดที่ส่งจริง
- [ ] `/logs` มี `ส่ง SML สำเร็จ`
- [ ] Expand log แล้วอ่าน key facts ได้ ไม่ต้องไล่ JSON ก่อน

ใน SML database:

```sql
select * from ic_trans where doc_no = '<DOC_NO>';
select * from ic_trans_detail where doc_no = '<DOC_NO>';
```

ตรวจ `ic_trans`:

- [ ] `doc_no` ตรงกับ BillFlow
- [ ] `doc_ref` เป็นเลขคำสั่งซื้อ Shopee
- [ ] `doc_ref_date` ถูกต้อง
- [ ] `cust_code` เป็น supplier ที่เลือก
- [ ] `branch_code` เป็นค่าที่ส่งจาก dialog
- [ ] `vat_type` และ `vat_rate` ถูกต้อง
- [ ] `total_value`, `total_before_vat`, `total_vat_value`, `total_amount` ถูกต้อง
- [ ] `remark` ถูกต้อง
- [ ] `doc_format_code = PO`

ตรวจ `ic_trans_detail`:

- [ ] `item_code` ถูกต้อง
- [ ] `item_name` ถูกต้อง
- [ ] `unit_code` ถูกต้อง
- [ ] `qty` ถูกต้อง
- [ ] `price` ถูกต้อง
- [ ] `sum_amount` ถูกต้อง
- [ ] `wh_code` / `shelf_code` เข้าตามที่ SML persist ได้

---

## 5b. ทดสอบ Bulk Send แบบ Async

ทำหลัง single-bill send ผ่านแล้ว:

- [ ] เปิด `/bills?status=pending`
- [ ] กด `ส่ง SML ทั้งหมด`
- [ ] ตรวจ preview ว่ารายการพร้อมส่ง/ต้องข้ามถูกต้อง
- [ ] เริ่มจาก 1 บิล หรือ 5-10 บิลก่อน ไม่เริ่มที่ 100 ทันที
- [ ] ระหว่างส่ง เห็น progress sent/failed/skipped/remaining
- [ ] ปิด dialog แล้วเปิดใหม่ ยังเห็นสถานะ job เดิมหรือผลล่าสุด
- [ ] ถ้ามี failed row ให้กด `Retry failed` แล้วระบบส่งเฉพาะ failed bills
- [ ] เปิด `/bulk-send-jobs` แล้วเห็น job ล่าสุด พร้อมเปิด detail ดูผลรายบิลและลิงก์กลับไปบิลต้นทางได้
- [ ] `/logs` มี `via=bulk_job` และ actor ของผู้กด
- [ ] บิลที่สำเร็จไม่ถูกส่งซ้ำตอน retry failed

Live smoke ล่าสุดบน BillFlow main:

- Job `128ceffe-5055-4863-8944-c6ce52301d26`
- Bill `20275aed-fe5f-402f-9160-a93a3f5b2ccb`
- SML doc `BF-PO26050001`
- sent `1`, failed `0`, skipped `0`

---

## 6. Test Cases ที่ควรผ่านก่อน demo

ก่อนเริ่ม test ให้เปิด `/setup`:

- [ ] ความพร้อมสำคัญผ่านครบ: ข้อมูลร้าน/SML, เส้นทางเอกสาร, อีเมล, สินค้าใน SML
- [ ] ฐานข้อมูล SML / ชื่อร้าน / AI ที่ใช้งาน ตรงกับร้านที่จะทดสอบ
- [ ] ถ้าต้องเริ่มทดสอบใหม่ กด `ล้างข้อมูลทดสอบ` แล้วเลือกเฉพาะตัวเลือกที่จำเป็น
- [ ] ไม่รีเซ็ตเลขรันเอกสารถ้าเคยส่งเข้า SML จริงแล้ว
- [ ] ไม่ล้างประวัติอีเมลที่เคยอ่านแล้ว ถ้าไม่ต้องการให้อีเมลเก่าถูกนำเข้าซ้ำ

| Case | สิ่งที่ต้องทดสอบ | Expected |
|---|---|---|
| 1 item, VAT แยกนอก | สินค้า 1 รายการ, `vat_type=0` | ส่งผ่าน, total รวม VAT ถูก |
| 1 item, VAT รวมใน | สินค้า 1 รายการ, `vat_type=1` | ส่งผ่าน, total before/vat/amount ถูก |
| หลายรายการ | อย่างน้อย 2-3 items | line totals ถูกทุกบรรทัด |
| สินค้าไม่เคย map | ต้องเลือกหรือสร้างสินค้า | ส่งไม่ได้จนกว่า map ครบ |
| ผู้ขายไม่เลือก | เปิด dialog แล้วไม่เลือก supplier | ส่งไม่ได้ |
| คลัง/พื้นที่เก็บว่าง | ลองไม่กรอก | ส่งไม่ได้ |
| SML ปิดหรือปลายทาง API ผิด | ตั้งปลายทาง API ผิดชั่วคราว | log อ่านง่าย, retry ได้หลังแก้ |

---

## 7. เกณฑ์ผ่าน Phase 1

ถือว่าพร้อมให้ลูกค้าทดสอบเมื่อ:

- [ ] ส่ง SML ผ่านอย่างน้อย 3 บิล
- [ ] มีอย่างน้อย 1 บิลที่หลายรายการ
- [ ] มีอย่างน้อย 1 บิลที่ต้อง map/สร้างสินค้าใหม่
- [ ] ตรวจ SML DB แล้ว header/detail เข้าในระดับที่ลูกค้าต้องใช้
- [ ] `/setup` แสดงชื่อร้าน/สถานะถูกต้อง และปุ่มล้างข้อมูลทดสอบใช้งานได้ตามสิทธิ์ admin
- [ ] User สามารถดูประวัติ/สาเหตุ error จาก `/logs` ได้โดยไม่ต้องอ่าน JSON ดิบ
- [ ] เอกสาร `phase1-guide.md` และ checklist นี้ตรงกับระบบล่าสุด

---

## 8. บันทึกผลทดสอบ

| Date | Bill ID | SML doc_no | Case | Result | Note |
|---|---|---|---|---|---|
|  |  |  |  |  |  |
|  |  |  |  |  |  |
|  |  |  |  |  |  |
