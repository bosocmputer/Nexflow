export const SHOPEE_ORDER_STATUS_DEFINITIONS = [
  { value: 'UNPAID', label: 'รอชำระเงิน', detail: 'ผู้ซื้อยังชำระเงินไม่สำเร็จ ระบบยังไม่ให้สร้างเอกสารขาย' },
  { value: 'READY_TO_SHIP', label: 'รอจัดส่ง', detail: 'ออเดอร์พร้อมให้ร้านเตรียมจัดส่ง หากเป็นเก็บเงินปลายทาง สถานะนี้ไม่ได้แปลว่าร้านได้รับเงินแล้ว' },
  { value: 'PROCESSED', label: 'เตรียมจัดส่งแล้ว', detail: 'Shopee รับข้อมูลการจัดส่งแล้วและกำลังติดตามสถานะขั้นถัดไป' },
  { value: 'SHIPPED', label: 'กำลังจัดส่ง', detail: 'พัสดุอยู่ระหว่างการขนส่งไปยังผู้ซื้อ' },
  { value: 'COMPLETED', label: 'สำเร็จ', detail: 'คำสั่งซื้อเสร็จสมบูรณ์ใน Shopee' },
  { value: 'IN_CANCEL', label: 'กำลังยกเลิก', detail: 'คำสั่งซื้ออยู่ระหว่างกระบวนการยกเลิก' },
  { value: 'CANCELLED', label: 'ยกเลิกแล้ว', detail: 'คำสั่งซื้อถูกยกเลิกแล้ว' },
] as const

export function shopeeOrderStatusDefinition(status?: string) {
  const normalized = String(status ?? '').trim().toUpperCase()
  return SHOPEE_ORDER_STATUS_DEFINITIONS.find((item) => item.value === normalized)
}
