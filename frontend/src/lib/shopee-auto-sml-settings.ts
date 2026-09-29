export type AutoSMLTriggerStatus = 'READY_TO_SHIP' | 'PROCESSED' | 'SHIPPED' | 'COMPLETED'

export function normalizeAutoSMLTriggerStatus(value?: string): AutoSMLTriggerStatus {
  const normalized = value?.trim().toUpperCase()
  if (normalized === 'PROCESSED' || normalized === 'SHIPPED' || normalized === 'COMPLETED') return normalized
  return 'READY_TO_SHIP'
}

export function autoSMLTriggerLabel(value?: string) {
  switch (normalizeAutoSMLTriggerStatus(value)) {
    case 'PROCESSED': return 'เตรียมจัดส่งแล้ว (PROCESSED)'
    case 'SHIPPED': return 'กำลังจัดส่ง (SHIPPED)'
    case 'COMPLETED': return 'สำเร็จ (COMPLETED)'
    default: return 'รอจัดส่ง (READY_TO_SHIP)'
  }
}

export function autoSMLTriggerDescription(value?: string) {
  switch (normalizeAutoSMLTriggerStatus(value)) {
    case 'PROCESSED': return 'รอร้านเตรียมจัดส่งใน Shopee แล้วจึงเริ่มส่ง SML'
    case 'SHIPPED': return 'รอขนส่งรับพัสดุแล้วจึงเริ่มส่ง SML'
    case 'COMPLETED': return 'รอคำสั่งซื้อเสร็จสมบูรณ์แล้วจึงเริ่มส่ง SML'
    default: return 'เริ่มส่ง SML เมื่อ Shopee แจ้งว่าออเดอร์พร้อมให้ร้านเตรียมสินค้า'
  }
}

export function requiredAutoSMLConfirmation(
  beforeEnabled: boolean,
  beforeTrigger: string | undefined,
  afterEnabled: boolean,
  afterTrigger: string | undefined,
  pausedReason?: string,
) {
  if (!afterEnabled) return ''
  if (!beforeEnabled) return 'ENABLE_AUTO_SML'
  if (pausedReason) return 'RESUME_AUTO_SML'
  if (normalizeAutoSMLTriggerStatus(beforeTrigger) !== normalizeAutoSMLTriggerStatus(afterTrigger)) {
    return 'UPDATE_AUTO_SML_TRIGGER'
  }
  return ''
}
