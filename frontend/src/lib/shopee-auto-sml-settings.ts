export type AutoSMLTriggerStatus = 'READY_TO_SHIP' | 'PROCESSED'

export function normalizeAutoSMLTriggerStatus(value?: string): AutoSMLTriggerStatus {
  return value?.trim().toUpperCase() === 'PROCESSED' ? 'PROCESSED' : 'READY_TO_SHIP'
}

export function autoSMLTriggerLabel(value?: string) {
  return normalizeAutoSMLTriggerStatus(value) === 'PROCESSED'
    ? 'เตรียมจัดส่งแล้ว (PROCESSED)'
    : 'รอจัดส่ง (READY_TO_SHIP)'
}

export function autoSMLTriggerDescription(value?: string) {
  return normalizeAutoSMLTriggerStatus(value) === 'PROCESSED'
    ? 'รอร้านเตรียมจัดส่งใน Shopee แล้วจึงเริ่มสร้างบิล'
    : 'เริ่มสร้างบิลเมื่อ Shopee แจ้งว่าออเดอร์พร้อมให้ร้านเตรียมสินค้า'
}

export function requiredAutoSMLConfirmation(
  beforeEnabled: boolean,
  beforeTrigger: string | undefined,
  afterEnabled: boolean,
  afterTrigger: string | undefined,
) {
  if (!afterEnabled) return ''
  if (!beforeEnabled) return 'ENABLE_AUTO_SML'
  if (normalizeAutoSMLTriggerStatus(beforeTrigger) !== normalizeAutoSMLTriggerStatus(afterTrigger)) {
    return 'UPDATE_AUTO_SML_TRIGGER'
  }
  return ''
}
