import type { SMLReadiness } from '@/types'

export function humanizeSMLConnectionError(value?: string | null) {
  const text = String(value ?? '').trim()
  const lower = text.toLowerCase()
  if (!text) return 'เครื่อง SML/Postgres ของร้านนี้อาจยังไม่เปิดหรือเชื่อมต่อไม่ได้'
  if (
    lower.includes('context deadline exceeded') ||
    lower.includes('timeout') ||
    lower.includes('connection refused') ||
    lower.includes('no route to host') ||
    lower.includes('eof') ||
    lower.includes('customer_count_failed') ||
    lower.includes('supplier_count_failed') ||
    lower.includes('count customers failed') ||
    lower.includes('count suppliers failed') ||
    lower.includes('request failed with status code 500')
  ) {
    return 'เครื่อง SML/Postgres ของร้านนี้อาจยังไม่เปิดหรือเชื่อมต่อไม่ได้'
  }
  return text
}

export function smlBlockedMessage(readiness?: SMLReadiness | null) {
  if (!readiness) return 'กำลังตรวจสถานะ SML ของร้านนี้'
  if (readiness.ready) return 'เชื่อมต่อ SML พร้อมใช้งาน'
  return readiness.message || 'เครื่อง SML/Postgres ของร้านนี้อาจยังไม่เปิดหรือเชื่อมต่อไม่ได้'
}

export function isSMLReady(readiness?: SMLReadiness | null) {
  return readiness?.ready === true
}
