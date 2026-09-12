export type TikTokSyncState = 'active' | 'error' | 'server_disabled' | 'shop_disabled'
export type TikTokStatusGroup = 'all' | 'unpaid' | 'to_ship' | 'shipping' | 'completed' | 'cancelled'

export interface TikTokStatusCounts {
  total: number
  unpaid: number
  to_ship: number
  shipping: number
  completed: number
  cancelled: number
}

const TIKTOK_STATUS_GROUPS: TikTokStatusGroup[] = ['all', 'unpaid', 'to_ship', 'shipping', 'completed', 'cancelled']

const STATUS_LABELS: Record<string, string> = {
  UNPAID: 'ยังไม่ชำระ',
  ON_HOLD: 'พักรายการ',
  AWAITING_SHIPMENT: 'รอจัดส่ง',
  PARTIALLY_SHIPPING: 'จัดส่งบางส่วน',
  AWAITING_COLLECTION: 'รอรับพัสดุ',
  IN_TRANSIT: 'กำลังขนส่ง',
  DELIVERED: 'จัดส่งแล้ว',
  COMPLETED: 'สำเร็จ',
  CANCELLED: 'ยกเลิก',
}

export function tiktokOrderStatusLabel(value: string): string {
  return STATUS_LABELS[value] ?? (value || 'ไม่ระบุสถานะ')
}

export function formatTikTokMoney(value: string, currency: string): string {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '—'
  try {
    return new Intl.NumberFormat('th-TH', {
      style: 'currency',
      currency: currency || 'THB',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    return '—'
  }
}

export function tiktokSyncState(workerEnabled: boolean, shopEnabled: boolean, errorCode?: string): TikTokSyncState {
  if (!workerEnabled) return 'server_disabled'
  if (!shopEnabled) return 'shop_disabled'
  if (errorCode?.trim()) return 'error'
  return 'active'
}

export function normalizeTikTokStatusGroup(value: string | null | undefined): TikTokStatusGroup {
  return TIKTOK_STATUS_GROUPS.includes(value as TikTokStatusGroup) ? value as TikTokStatusGroup : 'all'
}

export function tiktokStatusGroupCount(counts: TikTokStatusCounts, group: TikTokStatusGroup): number {
  return group === 'all' ? counts.total : counts[group]
}
