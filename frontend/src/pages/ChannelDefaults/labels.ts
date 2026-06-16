// Shared types + labels for the /settings/channels page.
// Channel × bill_type keys are the contract with the backend
// (channel_defaults table CHECK constraint).

export type ChannelKey =
  | 'line'
  | 'email'
  | 'shopee'
  | 'shopee_realtime'
  | 'shopee_realtime_cancel'
  | 'shopee_email'
  | 'shopee_shipped'
  | 'lazada'
  | 'tiktok'
  | 'manual'
  | 'shopee_settlement'

export type ChannelBillType = 'sale' | 'purchase' | 'ar_receipt'

export interface ChannelDefaultRow {
  channel: string
  bill_type: ChannelBillType
  party_code: string
  party_name: string
  party_phone: string
  party_address: string
  party_tax_id: string
  doc_format_code: string
  endpoint: string  // tested API path selected from SML_DESTINATION_OPTIONS
  doc_prefix: string         // e.g. "NX-SO"
  doc_running_format: string // e.g. "YYMM####"
  branch_code: string
  sale_code: string
  unit_code: string
  doc_time: string
  shipping_item_enabled?: boolean
  shipping_item_code?: string
  shipping_item_unit_code?: string
  passbook_code?: string
  passbook_name?: string
  bank_code?: string
  bank_branch?: string
  expense_code?: string
  expense_name?: string
  // Inventory + VAT overrides (sentinel: '' / -1 = "use server default")
  wh_code: string
  shelf_code: string
  vat_type: number      // -1 = use default; 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
  vat_rate: number      // -1 = use default; else percent (e.g. 7)
  inquiry_type: number  // -1 = use default; 0-3 = ประเภทรายการ
  remark_2?: string     // '' = ไม่ระบุ; 'tax'|'notax'|'re'
  updated_by?: string | null
  updated_at?: string
}

// previewDocNo renders a sample doc_no with seq=1 — mirrors the backend
// repository.GenerateDocNo logic (kept in sync; do not diverge).
export function previewDocNo(prefix: string, format: string, now = new Date()): string {
  if (!prefix) prefix = 'BF'
  if (!format) format = 'YYMM####'
  const yyyy = String(now.getFullYear())
  const yy = String(now.getFullYear() % 100).padStart(2, '0')
  const mm = String(now.getMonth() + 1).padStart(2, '0')
  const dd = String(now.getDate()).padStart(2, '0')
  let out = format
    .replace(/YYYY/g, yyyy)
    .replace(/YY/g, yy)
    .replace(/MM/g, mm)
    .replace(/DD/g, dd)
  out = out.replace(/#+/, (m: string) => '1'.padStart(m.length, '0'))
  return prefix + out
}

// docNoPatternWarning checks if the chosen prefix+format combo will hit the
// SML bug we discovered (UI silently drops docs whose doc_no contains a
// `-YYYY` or `-YYMM` segment after a hyphen). Returns warning text or ''.
export function docNoPatternWarning(prefix: string, format: string): string {
  if (!prefix || !format) return ''
  // Failure pattern: prefix ends with '-' AND format starts with YY/YYYY
  if (prefix.endsWith('-') && (format.startsWith('YYYY') || format.startsWith('YY'))) {
    return 'รูปแบบนี้อาจถูก SML ปฏิเสธ — เคยพบ bug ที่ doc_no มี "-YYMM" หรือ "-YYYYMM" ตามหลังเครื่องหมาย "-" จะ save ผ่านแต่กดดูไม่ได้. แนะนำให้ลบ "-" ท้าย prefix ออก (เช่น "NX-SO" + "YYMM####" → "NX-SO260400001")'
  }
  return ''
}

export type EndpointKind =
  | 'saleorder'
  | 'saleinvoice'
  | 'purchaseorder'
  | 'arreceipt'
  | 'creditnote'

export interface SmlDestinationOption {
  value: EndpointKind
  billType: ChannelBillType
  label: string
  apiPath: string
  docFormatCode: string
  docPrefix: string
  docRunningFormat: string
  statusLabel: string
  description: string
  phase1Enabled: boolean
}

// Business-level SML destinations. The backend still stores endpoint +
// doc_format_code, but admins pick from tested destinations instead of typing
// raw paths. Add new rows here only after that SML menu has been tested.
export const SML_DESTINATION_OPTIONS: SmlDestinationOption[] = [
  {
    value: 'saleorder',
    billType: 'sale',
    label: 'ขาย -> ใบสั่งขาย',
    apiPath: '/api/v1/ic/sale-orders',
    docFormatCode: 'SR',
    docPrefix: 'NX-SO',
    docRunningFormat: 'YYMM####',
    statusLabel: 'ทดสอบผ่านแล้ว',
    description: 'ส่งบิลขาย Marketplace Excel เข้าเมนู ขาย -> ใบสั่งขาย ใน SML',
    phase1Enabled: true,
  },
  {
    value: 'saleinvoice',
    billType: 'sale',
    label: 'ขาย -> ขายสินค้าและบริการ',
    apiPath: '/api/v1/ic/sale-invoices',
    docFormatCode: 'SI',
    docPrefix: 'NX-INV',
    docRunningFormat: 'YYMM####',
    statusLabel: 'ทดสอบผ่านแล้ว',
    description: 'ส่งบิลขาย Marketplace Excel เข้าเมนู ขาย -> ขายสินค้าและบริการ ใน SML',
    phase1Enabled: true,
  },
  {
    value: 'purchaseorder',
    billType: 'purchase',
    label: 'ซื้อ -> ใบสั่งซื้อ',
    apiPath: '/api/v1/ic/purchase-orders',
    docFormatCode: 'PO',
    docPrefix: 'NX-PO',
    docRunningFormat: 'YYMM####',
    statusLabel: 'ทดสอบผ่านแล้ว',
    description: 'ส่งบิลซื้อ Shopee เข้าเมนู ซื้อ -> ใบสั่งซื้อ ใน SML',
    phase1Enabled: true,
  },
  {
    value: 'arreceipt',
    billType: 'ar_receipt',
    label: 'ลูกหนี้ -> รับชำระหนี้',
    apiPath: '/api/v1/ar/receipts',
    docFormatCode: 'RC',
    docPrefix: 'RC',
    docRunningFormat: '@YYMM####',
    statusLabel: 'ทดสอบผ่านแล้ว',
    description: 'ส่งรายการ Shopee payout เข้าเมนู ลูกหนี้ -> รับชำระหนี้ ใน SML',
    phase1Enabled: true,
  },
  {
    value: 'creditnote',
    billType: 'sale',
    label: 'ขาย -> ยกเลิกขายสินค้าและบริการ',
    apiPath: '/api/v1/ic/sale-invoices/:doc_no/cancel',
    docFormatCode: 'CN',
    docPrefix: 'CN',
    docRunningFormat: 'YYMM####',
    statusLabel: 'ทดสอบกับ SML test DB แล้ว',
    description: 'สร้างเอกสารยกเลิกขายสินค้าและบริการ อ้างใบขายเดิมหลัง Shopee ยกเลิก order',
    phase1Enabled: true,
  },
]

export function destinationFor(
  channel: ChannelKey,
  billType: ChannelBillType,
  endpoint = '',
  docFormatCode = '',
): SmlDestinationOption | undefined {
  const kind = destinationKindFor(endpoint, channel, billType)
  return SML_DESTINATION_OPTIONS.find((option) => {
    if (option.value !== kind) return false
    if (!docFormatCode) return true
    return option.docFormatCode.toLowerCase() === docFormatCode.toLowerCase()
  }) ?? SML_DESTINATION_OPTIONS.find((option) => option.value === kind)
}

export function destinationOptionsFor(
  billType?: ChannelBillType,
): SmlDestinationOption[] {
  return SML_DESTINATION_OPTIONS.filter((option) => (
    option.phase1Enabled && (!billType || option.billType === billType)
  ))
}

// destinationKindFor resolves existing backend rows to one of the tested
// dropdown destinations. Empty / unknown endpoints fall back to the default
// destination for the channel and bill type.
export function destinationKindFor(
  override: string,
  channel: ChannelKey,
  billType: ChannelBillType,
): EndpointKind {
  const lower = (override || '').toLowerCase()
  if (channel === 'shopee_settlement' || billType === 'ar_receipt' || lower.includes('ar/receipts')) return 'arreceipt'
  if (channel === 'shopee_realtime_cancel' || lower.includes('creditnote') || lower.includes('cancel')) return 'creditnote'
  if (lower.includes('purchaseorder') || lower.includes('purchase-orders')) return 'purchaseorder'
  if (lower.includes('saleinvoice') || lower.includes('sale-invoices')) return 'saleinvoice'
  if (lower.includes('saleorder') || lower.includes('sale-orders')) return 'saleorder'
  // No keyword match → default by channel+bill_type
  if (channel === 'shopee_shipped' || billType === 'purchase') return 'purchaseorder'
  if (channel === 'shopee' || channel === 'shopee_realtime' || channel === 'shopee_email' || channel === 'lazada' || channel === 'tiktok') return 'saleorder'
  return 'saleorder'
}

export const CHANNEL_LABELS: Record<ChannelKey, string> = {
  line: 'LINE OA',
  email: 'Email',
  shopee: 'นำเข้า Shopee ย้อนหลัง',
  shopee_realtime: 'คำสั่งซื้อ Shopee',
  shopee_realtime_cancel: 'Shopee ยกเลิกหลังส่ง SML',
  shopee_email: 'Shopee Order',
  shopee_shipped: 'Email บิลซื้อ Shopee',
  lazada: 'Lazada Excel',
  tiktok: 'TikTok Excel',
  manual: 'Manual',
  shopee_settlement: 'Shopee รับชำระหนี้',
}
