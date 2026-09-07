export type LineQuotaType = 'limited' | 'unlimited' | 'none'
export type LineQuotaStatus = 'ok' | 'stale' | 'error'

export interface LineOAQuota {
  line_oa_id: string
  name: string
  quota_type?: LineQuotaType
  limit: number | null
  used: number | null
  remaining: number | null
  status: LineQuotaStatus
  checked_at: string | null
  is_stale: boolean
  error_code?: string
  retry_after_seconds?: number
}

export interface LineQuotaPresentation {
  headline: string
  detail: string
  percentage: number | null
  severity: 'normal' | 'warning' | 'full' | 'error'
}

export function presentLineQuota(quota: LineOAQuota): LineQuotaPresentation {
  if (quota.status === 'error' || !quota.quota_type) {
    return {
      headline: 'ตรวจโควตาไม่สำเร็จ',
      detail: lineQuotaErrorLabel(quota.error_code),
      percentage: null,
      severity: 'error',
    }
  }
  if (quota.quota_type === 'unlimited') {
    return {
      headline: 'ไม่จำกัด',
      detail: quota.used == null ? 'LINE ไม่ได้ส่งยอดใช้กลับมา' : `ใช้แล้วประมาณ ${formatCount(quota.used)} ข้อความ`,
      percentage: null,
      severity: 'normal',
    }
  }
  if (quota.quota_type === 'none') {
    return {
      headline: 'ไม่มีโควตาส่งแบบ Push',
      detail: quota.used == null ? 'LINE ไม่ได้ส่งยอดใช้กลับมา' : `ใช้แล้วประมาณ ${formatCount(quota.used)} ข้อความ`,
      percentage: null,
      severity: 'warning',
    }
  }

  const limit = Math.max(0, quota.limit ?? 0)
  const used = Math.max(0, quota.used ?? 0)
  const remaining = Math.max(0, quota.remaining ?? limit - used)
  const rawPercentage = limit > 0 ? (used / limit) * 100 : 100
  const percentage = Math.min(100, Math.max(0, rawPercentage))
  return {
    headline: `ใช้แล้ว ${formatCount(used)} / ${formatCount(limit)} ข้อความ`,
    detail: `เหลือประมาณ ${formatCount(remaining)} ข้อความ`,
    percentage,
    severity: percentage >= 100 ? 'full' : percentage >= 80 ? 'warning' : 'normal',
  }
}

export function lineQuotaErrorLabel(code?: string): string {
  switch (code) {
    case 'line_token_invalid':
    case 'line_token_missing':
      return 'Channel access token ใช้งานไม่ได้ กรุณาตรวจสอบการตั้งค่า OA'
    case 'line_forbidden':
      return 'LINE ไม่อนุญาตให้บัญชีนี้อ่านโควตา'
    case 'line_rate_limited':
      return 'LINE จำกัดการเรียกชั่วคราว กรุณาลองใหม่ภายหลัง'
    case 'line_timeout':
    case 'request_cancelled':
      return 'LINE ตอบกลับช้าเกินไป กรุณาลองใหม่'
    case 'refresh_cooldown':
      return 'เพิ่งรีเฟรชไป กรุณารอสักครู่'
    case 'invalid_quota_response':
    case 'invalid_consumption_response':
    case 'unsupported_quota_type':
      return 'รูปแบบข้อมูลโควตาจาก LINE ไม่ถูกต้อง'
    default:
      return 'ติดต่อ LINE ไม่สำเร็จ กรุณาลองใหม่'
  }
}

function formatCount(value: number): string {
  return Math.max(0, Math.trunc(value)).toLocaleString('th-TH')
}
