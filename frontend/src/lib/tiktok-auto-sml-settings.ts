export type TikTokAutoSMLTriggerStatus = 'AWAITING_SHIPMENT' | 'AWAITING_COLLECTION' | 'IN_TRANSIT' | 'COMPLETED'

export function normalizeTikTokAutoSMLTriggerStatus(value?: string): TikTokAutoSMLTriggerStatus {
  const normalized = value?.trim().toUpperCase()
  if (normalized === 'AWAITING_SHIPMENT' || normalized === 'IN_TRANSIT' || normalized === 'COMPLETED') return normalized
  return 'AWAITING_COLLECTION'
}

export function tiktokAutoSMLTriggerLabel(value?: string) {
  switch (normalizeTikTokAutoSMLTriggerStatus(value)) {
    case 'AWAITING_SHIPMENT': return 'รอจัดส่ง'
    case 'IN_TRANSIT': return 'กำลังขนส่ง'
    case 'COMPLETED': return 'สำเร็จ'
    default: return 'รอรับพัสดุ'
  }
}

export function tiktokAutoSMLTriggerDescription(value?: string) {
  switch (normalizeTikTokAutoSMLTriggerStatus(value)) {
    case 'AWAITING_SHIPMENT': return 'เริ่มส่งเมื่อ TikTok Shop ยืนยันการชำระเงินและออเดอร์รอร้านจัดส่ง'
    case 'IN_TRANSIT': return 'รอขนส่งรับพัสดุและเริ่มนำส่งแล้วจึงส่ง SML'
    case 'COMPLETED': return 'รอคำสั่งซื้อเสร็จสมบูรณ์แล้วจึงส่ง SML'
    default: return 'รอร้านเตรียมจัดส่งและพัสดุพร้อมให้ขนส่งเข้ารับแล้วจึงส่ง SML'
  }
}
