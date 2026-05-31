import type { Bill } from '@/types'

export function rawString(raw: Record<string, unknown> | null | undefined, key: string): string {
  const value = raw?.[key]
  return typeof value === 'string' ? value : ''
}

export function rawNumber(raw: Record<string, unknown> | null | undefined, key: string): number | null {
  const value = raw?.[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const parsed = Number(value.replace(/,/g, ''))
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

export function isShopeePurchaseBill(bill: Pick<Bill, 'source' | 'bill_type'>): boolean {
  return bill.source === 'shopee_shipped' && bill.bill_type === 'purchase'
}

export function isShopeeSalesBill(bill: Pick<Bill, 'source' | 'bill_type'>): boolean {
  return (bill.source === 'shopee' || bill.source === 'lazada' || bill.source === 'tiktok') && bill.bill_type === 'sale'
}

export function shopeeOrderID(raw: Record<string, unknown> | null | undefined): string {
  return rawString(raw, 'order_id') || rawString(raw, 'shopee_order_id') || rawString(raw, 'lazada_order_id') || rawString(raw, 'tiktok_order_id')
}

export function shopeePayableTotal(bill: Bill): number | null {
  if (!isShopeePurchaseBill(bill) && !isShopeeSalesBill(bill)) return null
  return rawNumber(bill.raw_data, 'paid_total_amount')
}

export function shopeeGoodsTotal(bill: Bill): number | null {
  if (!isShopeePurchaseBill(bill)) return null
  return rawNumber(bill.raw_data, 'goods_total_amount')
}

export function money(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return `฿${value.toLocaleString('th-TH')}`
}
