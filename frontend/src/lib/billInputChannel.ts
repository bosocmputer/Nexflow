import type { Bill } from '@/types'

export type BillInputChannel = 'shopee' | 'shopee_excel' | 'lazada_excel' | 'tiktok_excel'

export const BILL_INPUT_CHANNEL_OPTIONS: Array<{
  value: BillInputChannel
  label: string
  source: 'shopee' | 'lazada' | 'tiktok'
  excel: boolean
}> = [
  { value: 'shopee', label: 'Shopee', source: 'shopee', excel: false },
  { value: 'shopee_excel', label: 'Shopee Excel', source: 'shopee', excel: true },
  { value: 'lazada_excel', label: 'Lazada Excel', source: 'lazada', excel: true },
  { value: 'tiktok_excel', label: 'TikTok Excel', source: 'tiktok', excel: true },
]

export function isBillInputChannel(value: string): value is BillInputChannel {
  return BILL_INPUT_CHANNEL_OPTIONS.some((option) => option.value === value)
}

export function billInputChannelSource(value: BillInputChannel | ''): string {
  return BILL_INPUT_CHANNEL_OPTIONS.find((option) => option.value === value)?.source ?? ''
}

export function billInputChannelLabel(value: string): string {
  return BILL_INPUT_CHANNEL_OPTIONS.find((option) => option.value === value)?.label ?? value
}

export function classifyBillInputChannel(
  bill: Pick<Bill, 'source' | 'raw_data'>,
): BillInputChannel | null {
  const flow = typeof bill.raw_data?.flow === 'string' ? bill.raw_data.flow.trim() : ''
  if (bill.source === 'shopee') {
    return flow === 'shopee_excel' ? 'shopee_excel' : 'shopee'
  }
  if (bill.source === 'lazada' && flow === 'lazada_excel') return 'lazada_excel'
  if (bill.source === 'tiktok' && flow === 'tiktok_excel') return 'tiktok_excel'
  return null
}
