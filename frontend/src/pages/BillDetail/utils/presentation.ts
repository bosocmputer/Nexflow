const SALE_INQUIRY_TYPE_LABELS: Record<number, string> = {
  0: 'ขายเงินเชื่อ',
  1: 'ขายเงินสด',
  2: 'ขายเงินเชื่อ (สินค้าบริการ)',
  3: 'ขายเงินสด (สินค้าบริการ)',
}

const PURCHASE_INQUIRY_TYPE_LABELS: Record<number, string> = {
  0: 'ซื้อสินค้าเงินเชื่อ',
  1: 'ซื้อสินค้าเงินสด',
  2: 'ซื้อสินค้าเงินเชื่อ (สินค้าบริการ)',
  3: 'ซื้อสินค้าเงินสด (สินค้าบริการ)',
}

export function formatBangkokDateTime(value?: string | Date | null): string {
  if (!value) return ''
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)

  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Bangkok',
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date)
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((entry) => entry.type === type)?.value ?? ''

  return `${part('day')}/${part('month')}/${part('year')} ${part('hour')}:${part('minute')} น.`
}

export function billSMLStatusLabel(status: string, automatically?: boolean): string {
  if (status !== 'sent') return ''
  return automatically ? 'ส่งแล้ว (AUTO)' : 'ส่งแล้ว'
}

export function smlInquiryTypeLabel(value: unknown, billType: string): string {
  if (value == null || value === '') return '—'
  const numericValue = typeof value === 'number' ? value : Number(value)
  if (!Number.isInteger(numericValue)) return `${String(value)} · ไม่ทราบความหมาย`

  const labels = billType === 'purchase'
    ? PURCHASE_INQUIRY_TYPE_LABELS
    : SALE_INQUIRY_TYPE_LABELS
  return `${numericValue} · ${labels[numericValue] ?? 'ไม่ทราบความหมาย'}`
}
