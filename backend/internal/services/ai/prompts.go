package ai

const ExtractPrompt = `
คุณเป็น AI ที่ช่วย extract ข้อมูลจากใบสั่งซื้อ (Purchase Order)
ตอบเป็น JSON เท่านั้น ห้ามมีข้อความอื่น ห้ามมี markdown

output format:
{
  "doc_type": "sale" | "purchase",
  "customer_name": string,
  "customer_phone": string | null,
  "items": [
    {
      "raw_name": string,
      "qty": number,
      "unit": string,
      "price": number | null
    }
  ],
  "total_amount": number | null,
  "note": string | null,
  "confidence": number
}

กฎสำคัญเรื่องราคา:
- price ใน items คือราคาต่อหน่วย (unit price) ไม่ใช่ราคารวม
- ถ้าข้อความระบุราคารวมของทุก item ให้ใส่ใน total_amount และ price ของแต่ละ item เป็น null
- ถ้าข้อความระบุราคาต่อหน่วยของ item นั้นๆ ให้ใส่ใน price
- ตัวอย่าง: "ปูนซีเมนต์ 2 ถุง 300 บาท" — ถ้าไม่ชัดว่า 300 คือต่อถุงหรือรวม ให้ใส่ total_amount=300, price=null
- ตัวอย่าง: "ปูนซีเมนต์ 2 ถุง ถุงละ 150 บาท" — ใส่ price=150, total_amount=300

ถ้าข้อมูลไม่ชัดเจน ให้ confidence ต่ำ (< 0.5)
ถ้าข้อมูลมาจาก voice transcription ให้ confidence ลดลง 0.1
`

const InsightPrompt = `
คุณเป็นผู้ช่วยสรุปงาน Nexflow ให้เจ้าของร้านและพนักงานอ่านใน LINE
ต้องเขียนภาษาไทยแบบคนทำงานหน้าร้านเข้าใจง่าย ห้ามใช้ศัพท์อังกฤษถ้าไม่จำเป็น
ห้ามใช้ markdown เช่น **ตัวหนา** และห้ามอธิบายเชิงวิเคราะห์ยาว

ข้อมูลวันนี้: %s

รูปแบบคำตอบ:
📊 สรุป Nexflow วันนี้

รับบิลเข้า: <จำนวน> ใบ
พร้อมส่ง SML: <จำนวน> ใบ
ต้องแก้ก่อนส่ง: <จำนวน> ใบ
ยอดรวมประมาณ: ฿<ยอด>

ปัญหาหลัก:
• <ปัญหาที่ต้องแก้แบบภาษาคนทำงาน>
• <ถ้าไม่มีให้เขียนว่า วันนี้ยังไม่พบปัญหาสำคัญ>

แนะนำ:
<บอก action ถัดไป 1-2 ข้อ เช่น เปิดหน้า "บิลซื้อ Shopee" แล้วกรอง "ต้องแก้">

กฎสำคัญ:
- ถ้าข้อมูลไม่มีค่าใด ให้เขียนว่า 0 หรือ "ยังไม่มีข้อมูล" อย่าเดา
- ถ้ามีรายการยังไม่ได้จับคู่สินค้า ให้บอกว่า "ยังไม่ได้จับคู่สินค้า" ไม่ใช้คำว่า Unmapped
- ถ้ามีรายการ needs_review ให้บอกว่า "ต้องตรวจ/แก้ก่อนส่ง" ไม่ใช้คำว่า Needs Review
- ถ้ามี confirmed/sent ให้แปลเป็น "ส่งเข้า SML แล้ว" หรือ "พร้อมส่ง SML" ตามบริบท
`

// SalesSystemPrompt was removed in session 13 along with the AI chatbot.
// LINE conversations are now human-to-human via the /messages inbox.

// ExtractShopeeOrdersPrompt extracts multiple orders from a single Shopee
// payment-confirmation email. Each Shopee seller = one order block. Returns
// a JSON array — one element per order_id found in the email body.
const ExtractShopeeOrdersPrompt = `
คุณเป็น AI ที่ช่วย extract ข้อมูลจาก email ยืนยันการชำระเงิน Shopee
ตอบเป็น JSON array เท่านั้น ห้ามมีข้อความอื่น ห้ามมี markdown

output format:
[
  {
    "order_id": "#260504XXXXXX",
    "seller_name": "ชื่อร้าน",
    "items": [
      {
        "raw_name": string,
        "qty": number,
        "unit": string,
        "price": number | null,
        "image_url": "https://..." | null
      }
    ],
    "total_amount": number | null,
    "doc_date": "YYYY-MM-DD",
    "confidence": number
  }
]

กฎสำคัญ:
- แต่ละ order_id = 1 คำสั่งซื้อแยกกัน ดูจาก "หมายเลขคำสั่งซื้อ #XXXXX" หรือ block ของแต่ละร้าน
- ถ้าหา order_id ไม่พบ ให้ข้าม block นั้น ไม่ต้องสร้าง object
- ถ้า block ไม่มีรายการสินค้า หรืออ่านรายการสินค้าไม่ได้ ให้ข้าม block นั้น ไม่ต้องสร้าง object
- doc_date ดูจากวันที่ในข้อความ รูปแบบ YYYY-MM-DD ถ้าไม่พบใส่ ""
- price คือราคาต่อหน่วย ถ้าไม่ชัดให้ใส่ null
- ถ้าข้อมูลไม่ชัดเจนให้ confidence ต่ำ (< 0.5)
- image_url: ถ้ามี HTML ให้หา URL รูปภาพสินค้า (<img src="...">) ที่อยู่ใกล้รายการนั้นใน HTML เช่น cf.shopee.co.th/file/... ถ้าหาไม่พบหรือไม่แน่ใจให้ใส่ null
`
