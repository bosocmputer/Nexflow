# BillFlow Phase 1 — คู่มือใช้งานบิลซื้อ Shopee

> อัปเดตล่าสุด: 2026-05-20
> Scope ปัจจุบัน: ดึงอีเมล Shopee ฝั่งบิลซื้อ, ตรวจรายการ, จับคู่สินค้า, และส่งเข้า SML `purchaseorder`

---

## Phase 1 ทำอะไรได้แล้ว

| ความสามารถ | สถานะ | หมายเหตุ |
|---|---:|---|
| ดึงอีเมล Shopee ผ่าน IMAP | ✅ | ตั้งค่าที่ `/settings/email` |
| แยกอีเมล Shopee shipped/payment เป็นบิลซื้อ | ✅ | source เป็น `shopee_shipped` |
| เก็บหลักฐานต้นทาง | ✅ | HTML body และ envelope JSON ดูได้จากหน้าบิล |
| สร้างบิลซื้อและรายการสินค้า | ✅ | เปิดดูที่ `/bills` และ `/bills/:id` |
| จับคู่สินค้า SML | ✅ | เลือกจาก catalog หรือสร้างสินค้าใหม่ |
| เลือกผู้ขาย/คลัง/ภาษีก่อนส่ง | ✅ | ทำใน dialog “ยืนยันการส่งใบสั่งซื้อไปยัง SML” |
| ส่งเข้า SML Purchase Order | ✅ | `POST /SMLJavaRESTService/v3/api/purchaseorder` |
| ส่ง SML หลายบิลพร้อมกัน | ✅ | `/bills` ใช้ async job เห็น progress และ retry failed ได้ |
| ดู log และ payload ย้อนหลัง | ✅ | `/logs` และ section “ข้อมูลการส่งเข้า SML” ในหน้าบิล |

---

## เมนูที่ใช้ใน Phase 1

| เมนู | ใช้ทำอะไร |
|---|---|
| `/dashboard` ภาพรวม | ดูสถานะงานวันนี้, งานรอตรวจ, งานผิดพลาด |
| `/bills` บิลซื้อ Shopee | รายการบิลที่ระบบดึงจากอีเมล |
| `/bills/:id` รายละเอียดบิล | ตรวจสินค้า, จับคู่, ส่งเข้า SML, ดู payload/response |
| `/mappings` ตารางจับคู่สินค้า | ดู/เพิ่ม/แก้ mapping ชื่อสินค้าจากอีเมลกับรหัส SML |
| `/settings/catalog` สินค้าใน SML | ค้นหา, sync, สร้างสินค้าใหม่ |
| `/settings/email` อีเมลรับบิล | ตั้งค่า IMAP inbox และกด poll ทดสอบ |
| `/settings/channels` เส้นทาง SML บิลซื้อ Shopee | ตั้งค่า API path, `doc_format_code`, และรูปแบบเลขเอกสาร |
| `/settings/instance` การเชื่อมต่อระบบ | ตั้งค่า SML/OpenRouter ระดับ instance |
| `/logs` ประวัติการทำงาน | ตรวจย้อนหลังว่าดึงอีเมล/สร้างบิล/ส่ง SML สำเร็จหรือไม่ |

---

## Flow ใช้งานจริง

```text
Shopee email เข้า inbox
  ↓
BillFlow poll IMAP
  ↓
แยกอีเมลเป็น Shopee purchase bill
  ↓
AI/extractor อ่านเลขคำสั่งซื้อ, วันที่, รายการสินค้า, จำนวน, ราคา
  ↓
สร้างบิลและเก็บ artifact ต้นทาง
  ↓
Admin เปิดหน้าบิล
  ↓
ตรวจสินค้าและจับคู่กับ SML catalog
  ↓
กดส่งเข้า SML
  ↓
เลือกผู้ขาย, คลัง, พื้นที่เก็บ, ภาษี, เวลาเอกสาร, หมายเหตุ
  ↓
BillFlow ส่ง SML purchaseorder
  ↓
SML สร้างเอกสาร PO และ BillFlow บันทึก payload/response/log
```

---

## ตั้งค่าก่อนทดสอบ

### 1. อีเมลรับบิล

เข้า `/settings/email`

ค่าที่ควรตรวจ:

| ค่า | ต้องเป็นอย่างไร |
|---|---|
| Host/Port | IMAP server ถูกต้อง เช่น Gmail `imap.gmail.com:993` |
| Username/Password | ใช้ App Password ไม่ใช้รหัสผ่านจริง |
| Mailbox | `INBOX` หรือ folder ที่ Shopee ส่งเข้า |
| Channel | `shopee` หรือค่าที่ระบบ route ไป Shopee shipped ได้ |
| Filter subjects | มี keyword ของอีเมล Shopee ที่ต้องการดูด |
| Enabled | เปิดใช้งาน |
| Poll interval | ขั้นต่ำ 300 วินาที |

หลังแก้ให้กด `ทดสอบการเชื่อมต่อ` หรือ `Poll ตอนนี้`

### 2. เส้นทาง SML บิลซื้อ Shopee

เข้า `/settings/channels`

Phase 1 ให้หน้านี้เหลือเฉพาะค่าของ channel:

| ค่า | ตัวอย่าง |
|---|---|
| API ที่ส่งเข้า SML | `/SMLJavaRESTService/v3/api/purchaseorder` |
| `doc_format_code` | `PO` |
| เลขเอกสาร / doc_no format | เช่น prefix `BF-PO`, running `YYMM####` |

ค่าเอกสารต่อบิล เช่น ผู้ขาย, คลัง, ภาษี ไม่ได้ตั้งที่หน้านี้แล้ว ให้เลือกใน dialog ตอนส่งจากหน้าบิล

### 3. สินค้าใน SML

เข้า `/settings/catalog`

ควรทำก่อนเริ่มทดสอบ:

1. กด sync สินค้าจาก SML
2. ตรวจว่าสินค้าที่ใช้ทดสอบค้นหาเจอ
3. ถ้าไม่เจอ ให้สร้างสินค้าใหม่จากหน้าบิลหรือหน้า catalog

---

## Dialog ส่งเข้า SML

เปิดบิล แล้วกดส่งเข้า SML ระบบจะแสดง dialog “ยืนยันการส่งใบสั่งซื้อไปยัง SML”

| Field | บังคับ | ค่าเริ่มต้น/พฤติกรรม |
|---|---:|---|
| ผู้ขาย | ✅ | เลือก supplier จาก SML |
| คลัง | ✅ | user ต้องระบุเอง |
| พื้นที่เก็บ | ✅ | user ต้องระบุเอง |
| ประเภทภาษี | ✅ | ต้องเลือก เช่น แยกนอก/รวมใน/อัตรา 0 |
| อัตราภาษี | ✅ | default `7` |
| เวลาเอกสาร | ✅ | default เป็นเวลาปัจจุบัน |
| Branch code | ❌ | ถ้าว่าง ส่ง `""` |
| Sale code | ❌ | ถ้าว่าง ส่ง `""` |
| หมายเหตุ | ❌ | ส่งเข้า `remark` และเก็บในบิล |

## ส่ง SML หลายบิล

หน้า `/bills` มีปุ่ม `ส่ง SML ทั้งหมด` สำหรับบิลสถานะพร้อมส่ง (`pending`).

- ระบบ preview และ validate ก่อนส่งจริง.
- จำกัดงานละไม่เกิน 100 บิล.
- หลังยืนยัน ระบบสร้าง async job ใน backend แล้วแสดง progress: ส่งสำเร็จ / ไม่สำเร็จ / ข้าม / คงเหลือ.
- ปิด dialog แล้วกลับมาเปิดใหม่ได้ ระบบจะพยายาม resume job ที่ยัง active หรือ job ล่าสุด.
- ถ้าบางบิล fail ให้กด `Retry failed` เพื่อส่งเฉพาะบิลที่ไม่สำเร็จ ไม่ยิงซ้ำบิลที่ส่งแล้ว.
- เมนู `ประวัติส่ง SML` (`/bulk-send-jobs`) ใช้ตรวจย้อนหลังว่า batch ไหนส่งสำเร็จ/ล้มเหลว/ข้าม และเปิดกลับไปดูบิลต้นทางได้.
- Live smoke ล่าสุดสร้าง SML PO `BF-PO26050001` สำเร็จจาก bulk job หนึ่งบิล.

---

## SML Purchase Order Endpoint

BillFlow ส่งบิลซื้อ Shopee ไปที่:

```text
POST http://192.168.2.248:8080/SMLJavaRESTService/v3/api/purchaseorder
```

Headers ที่ backend ใช้จริง:

```http
guid: smlx
provider: SMLGOH
configFileName: SMLConfigSMLGOH.xml
databaseName: SML1_2026
Content-Type: application/json; charset=utf-8
```

Payload สำคัญ:

| Field | ที่มา |
|---|---|
| `doc_no` | BillFlow doc counter |
| `doc_format_code` | `/settings/channels` |
| `doc_ref` | เลขคำสั่งซื้อ Shopee เช่น `#2604306XDKEKW1` |
| `doc_ref_date` | วันที่เอกสารจาก Shopee |
| `cust_code` | ผู้ขายที่ user เลือก |
| `supplier_name` | ชื่อผู้ขายที่ user เลือก |
| `branch_code` | จาก dialog, ถ้าว่างส่ง `""` |
| `sale_code` | จาก dialog, ถ้าว่างส่ง `""` |
| `wh_code`, `wh_from` | คลังจาก dialog |
| `shelf_code`, `location_from` | พื้นที่เก็บจาก dialog |
| `vat_type`, `vat_rate` | จาก dialog |
| `items[].item_code` | สินค้า SML ที่จับคู่แล้ว |
| `items[].wh_code`, `items[].shelf_code` | คลัง/พื้นที่เก็บของ line item |
| `items[].wh_code_2`, `items[].shelf_code_2` | ส่งตามรูปแบบที่ทดสอบกับ SML |
| `user_request` | `""` ใน Phase 1 |

---

## การตรวจผลหลังส่ง

ใน BillFlow:

1. เปิดหน้าบิล
2. ดูหัวข้อ “ข้อมูลการส่งเข้า SML”
3. ตรวจ summary:
   - เลขเอกสาร SML
   - อ้างอิง Shopee
   - ผู้ขาย / คู่ค้า
   - คลัง / พื้นที่เก็บ
   - ภาษี
   - จำนวนรายการ
   - ยอดสุทธิ
4. ถ้าต้อง debug field ให้เปิด “ข้อมูลที่ส่งไป SML” และ “ผลตอบกลับจาก SML”

ใน SML database:

```sql
select * from ic_trans where doc_no = '<DOC_NO>';
select * from ic_trans_detail where doc_no = '<DOC_NO>';
```

ควรตรวจ:

| Table | Field |
|---|---|
| `ic_trans` | `doc_no`, `doc_ref`, `doc_ref_date`, `cust_code`, `branch_code`, `vat_type`, `vat_rate`, totals |
| `ic_trans_detail` | `item_code`, `item_name`, `unit_code`, `qty`, `price`, `wh_code`, `shelf_code`, totals |

---

## ประวัติการทำงาน

เข้า `/logs`

ใช้ดู:

- อีเมลถูกดึงเข้ามาเมื่อไร
- ระบบสร้างบิลแล้วหรือยัง
- mapping / product create เกิดเมื่อไร
- ส่ง SML สำเร็จหรือ error อะไร
- trace id สำหรับตามปัญหา

กด expand log เพื่อดูข้อมูลสำคัญก่อน raw JSON เช่น:

- บิล
- เลขเอกสาร SML
- route
- วิธีส่ง
- error
- trace

---

## Troubleshooting

### ส่ง SML ไม่ได้

ตรวจตามลำดับ:

1. ทุก item มี `item_code` และ `unit_code`
2. จำนวนและราคามากกว่า 0
3. เลือกผู้ขายแล้ว
4. กรอกคลังและพื้นที่เก็บแล้ว
5. เลือกประเภทภาษีและอัตราภาษีแล้ว
6. `/settings/channels` endpoint เป็น purchaseorder
7. SML server `192.168.2.248:8080` เปิดอยู่
8. ดู `/logs` แล้ว expand row `ส่ง SML ล้มเหลว`

### อีเมลไม่เข้า

1. เข้า `/settings/email`
2. ดูสถานะล่าสุดของ inbox
3. กด Poll ตอนนี้
4. ตรวจ App Password / IMAP enabled / subject filter
5. ดู `/logs` ว่ามี `รับอีเมล Shopee Shipped` หรือไม่

### สินค้าจับคู่ผิด

1. เปิดบิล
2. ไปที่ตารางรายการสินค้า
3. กดจัดการ/เลือกสินค้า
4. เลือกสินค้าจาก SML catalog
5. บันทึก mapping เพื่อให้ครั้งต่อไปเรียนรู้

---

## ขอบเขตที่ยังไม่ใช่ Phase 1

Phase 1 ยังไม่เน้น:

- LINE OA chat/bill flow
- Shopee Excel sale flow
- Lazada
- Multi-tenant platform
- Approval workflow หลายระดับ

ฟีเจอร์เหล่านี้ยังมี code บางส่วนอยู่ แต่ UI ฝั่ง Phase 1 ควรซ่อนหรือไม่ใช้ในการทดสอบลูกค้า

---

## ไฟล์ checklist

ใช้ไฟล์ [phase1-test-checklist.md](phase1-test-checklist.md) เป็นรายการตรวจทีละข้อก่อน demo/test ลูกค้า
