import type { Bill, BillItem } from '@/types'

export const SHOPEE_SHIPPING_SOURCE_SKU = '__shopee_shipping__'

// Validation issue surfaced to the admin BEFORE sending to SML.
// `kind` drives the warning copy; `firstItemId` is the first row that
// triggered this kind so the UI can scroll to + highlight it.
export type IssueKind =
  | 'no_items'
  | 'unmapped_item_code'
  | 'unconfirmed_match'
  | 'unmapped_unit_code'
  | 'qty_zero'
  | 'price_zero'

export interface ValidationIssue {
  kind: IssueKind
  // How many items hit this rule (used in the warning copy "3 รายการ…").
  count: number
  // First offending item's id — the "ดู" link scrolls to this row.
  // null when the issue is bill-level (no_items).
  firstItemId: string | null
}

export interface ValidationResult {
  canSend: boolean
  // Empty array iff canSend === true.
  issues: ValidationIssue[]
  // The id of the FIRST problematic item across ALL kinds — used as a
  // fallback target for "ดู" when the warning card is at the top of the
  // page and the admin clicks any issue link.
  firstBlockingItemId: string | null
}

const ISSUE_LABEL: Record<IssueKind, string> = {
  no_items: 'ยังไม่มีรายการในบิล: เพิ่มอย่างน้อย 1 รายการก่อน',
  unmapped_item_code: 'ยังไม่ได้จับคู่กับสินค้าใน SML',
  unconfirmed_match: 'ที่ระบบแนะนำสินค้าแล้วแต่ยังไม่ได้ยืนยัน',
  unmapped_unit_code: 'ยังไม่ได้ตั้งหน่วย',
  qty_zero: 'จำนวนต้องมากกว่า 0',
  price_zero: 'ราคาต้องมากกว่า 0',
}

// Per-issue copy used inline on the row tooltip (shorter than ISSUE_LABEL
// so it fits as hover text without truncation).
const ISSUE_TOOLTIP: Record<IssueKind, string> = {
  no_items: '',
  unmapped_item_code: 'ยังไม่ได้จับคู่สินค้า',
  unconfirmed_match: 'ระบบแนะนำสินค้าแล้ว ต้องยืนยันก่อนส่ง',
  unmapped_unit_code: 'ยังไม่ได้ตั้งหน่วย',
  qty_zero: 'จำนวนต้องมากกว่า 0',
  price_zero: 'ราคา ≤ 0',
}

export function issueLabel(kind: IssueKind): string {
  return ISSUE_LABEL[kind]
}

export function allowsZeroPrice(item: BillItem): boolean {
  return item.source_sku === SHOPEE_SHIPPING_SOURCE_SKU
}

export function hasInvalidPrice(item: BillItem): boolean {
  if (item.price == null) return true
  if (allowsZeroPrice(item)) return item.price < 0
  return item.price <= 0
}

// Per-row reason string — concatenates all issues found on this item so the
// row indicator's tooltip is informative (e.g. "ยังไม่ได้ map · ขาด unit_code").
// Returns "" if the row is fine.
export function rowIssueReason(item: BillItem): string {
  const reasons: string[] = []
  if (!item.item_code || item.item_code.trim() === '') {
    reasons.push(item.source_sku ? `SKU ต้นทาง ${item.source_sku} ยังไม่พบในสินค้า SML` : ISSUE_TOOLTIP.unmapped_item_code)
  } else if (item.mapped !== true) {
    reasons.push(ISSUE_TOOLTIP.unconfirmed_match)
  }
  if (!item.unit_code || item.unit_code.trim() === '') {
    reasons.push(ISSUE_TOOLTIP.unmapped_unit_code)
  }
  if (!item.qty || item.qty <= 0) {
    reasons.push(ISSUE_TOOLTIP.qty_zero)
  }
  if (hasInvalidPrice(item)) {
    reasons.push(ISSUE_TOOLTIP.price_zero)
  }
  return reasons.join(' · ')
}

// validateForSML mirrors what the backend retry handler will reject.
// Lifting this to the client lets us disable the Send button + jump to the
// offending row, instead of the admin only finding out via a failed SML
// round-trip.
//
// Rules (must match backend bills.go retry handler + F2 anomaly rules):
//   - bill must have ≥ 1 item
//   - every item must have non-empty item_code (SML required)
//   - every item with item_code must be human-confirmed (mapped=true)
//   - every item must have non-empty unit_code (SML required)
//   - every item must have qty > 0  (F2 qty_zero block-level anomaly)
//   - every normal item must have price > 0
//   - Shopee shipping lines may have price = 0 when the email explicitly says
//     "ค่าจัดส่งสินค้า: ฿0"
export function validateForSML(bill: Bill): ValidationResult {
  const items = bill.items ?? []

  if (items.length === 0) {
    return {
      canSend: false,
      issues: [{ kind: 'no_items', count: 1, firstItemId: null }],
      firstBlockingItemId: null,
    }
  }

  // Tally per kind so the warning card shows "3 รายการยังไม่ได้จับคู่"
  // instead of repeating the same message for every offending row.
  const counts: Record<IssueKind, number> = {
    no_items: 0,
    unmapped_item_code: 0,
    unconfirmed_match: 0,
    unmapped_unit_code: 0,
    qty_zero: 0,
    price_zero: 0,
  }
  const firsts: Record<IssueKind, string | null> = {
    no_items: null,
    unmapped_item_code: null,
    unconfirmed_match: null,
    unmapped_unit_code: null,
    qty_zero: null,
    price_zero: null,
  }
  let firstBlocking: string | null = null

  for (const it of items) {
    const itemHas = (kind: IssueKind) => {
      counts[kind]++
      if (firsts[kind] === null) firsts[kind] = it.id
      if (firstBlocking === null) firstBlocking = it.id
    }
    if (!it.item_code || it.item_code.trim() === '') {
      itemHas('unmapped_item_code')
    } else if (it.mapped !== true) {
      itemHas('unconfirmed_match')
    }
    if (!it.unit_code || it.unit_code.trim() === '') itemHas('unmapped_unit_code')
    if (!it.qty || it.qty <= 0) itemHas('qty_zero')
    if (hasInvalidPrice(it)) itemHas('price_zero')
  }

  const issues: ValidationIssue[] = (
    ['unmapped_item_code', 'unconfirmed_match', 'unmapped_unit_code', 'qty_zero', 'price_zero'] as IssueKind[]
  )
    .filter((k) => counts[k] > 0)
    .map((k) => ({ kind: k, count: counts[k], firstItemId: firsts[k] }))

  return {
    canSend: issues.length === 0,
    issues,
    firstBlockingItemId: firstBlocking,
  }
}
