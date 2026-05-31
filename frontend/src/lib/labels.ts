// labels.ts — single source of truth for domain labels shown in the admin UI.
//
// Why this exists: before this file, the same status had three different
// labels across pages (Bills filter said "ล้มเหลว", Dashboard ActionCards
// said "บิลล้มเหลว", Logs ACTION_META said "ส่ง SML ล้มเหลว"). Three labels
// for the same concept makes the UI feel half-finished. Every page now
// imports from here so the same word is used everywhere.
//
// Only put domain words here — primary nouns, status verbs, source names.
// Don't put button copy ("บันทึก", "ลบ") or page-specific microcopy.

// Bill lifecycle status — DB enum: pending / needs_review / confirmed / sent / failed / skipped
export const BILL_STATUS_LABEL: Record<string, string> = {
  pending:      'พร้อมส่ง',
  needs_review: 'ต้องตรวจสินค้า',
  confirmed:    'ยืนยันแล้ว',
  sent:         'ส่งเข้า SML แล้ว',
  failed:       'ส่งไม่สำเร็จ',
  skipped:      'ข้ามแล้ว',
}

// Short variants for tight UI (badges, table cells, action cards).
// Use only where space matters; prefer BILL_STATUS_LABEL for descriptive contexts.
export const BILL_STATUS_LABEL_SHORT: Record<string, string> = {
  pending:      'พร้อมส่ง',
  needs_review: 'ตรวจสินค้า',
  confirmed:    'ยืนยันแล้ว',
  sent:         'ส่งแล้ว',
  failed:       'ไม่สำเร็จ',
  skipped:      'ข้ามแล้ว',
}

// Bill type — DB CHECK: sale / purchase
export const BILL_TYPE_LABEL: Record<string, string> = {
  sale:     'บิลขาย',
  purchase: 'บิลซื้อ',
}

// Source channel — DB CHECK on bills.source.
// Aligned with /logs SOURCE_LABELS (which lives in audit-log-meta.ts) so the
// same word appears whether you're filtering bills or scanning logs.
export const BILL_SOURCE_LABEL: Record<string, string> = {
  line:           'LINE OA',
  email:          'Email',
  shopee:         'Shopee',
  shopee_email:   'Shopee Order',
  shopee_shipped: 'Email บิลซื้อ Shopee',
  lazada:         'Lazada',
  tiktok:         'TikTok Excel',
  manual:         'Manual',
}

// Page titles — referenced in PageHeader so the title shown matches the
// sidebar label exactly (no more "Mapping สินค้า" page title vs "ตารางจับคู่
// สินค้า" sidebar label drift).
export const PAGE_TITLE = {
  dashboard:        'ภาพรวม',
  bills:            'ใบสั่งซื้อ',
  salesOrders:      'ใบสั่งขาย',
  saleInvoices:     'ขายสินค้าและบริการ',
  billDetail:       'รายละเอียดบิล',
  messages:         'ข้อความลูกค้า',
  importLazada:     'นำเข้า Lazada',
  importShopee:     'นำเข้า Shopee',
  importTikTok:     'นำเข้า TikTok',
  mappings:         'ตารางจับคู่สินค้า',
  catalog:          'สินค้าใน SML',
  channelDefaults:  'เส้นทางเอกสาร SML',
  emailInboxes:     'กล่องอีเมลรับบิล',
  lineOA:           'บัญชี LINE OA',
  quickReplies:     'ข้อความสำเร็จรูป',
  chatTags:         'ป้ายลูกค้า',
  logs:             'ประวัติการทำงาน',
  bulkSendJobs:     'ประวัติส่ง SML แบบกลุ่ม',
  settings:         'ตั้งค่าระบบ',
}

// helper — fall back to the raw key when an unknown value sneaks in. Keeps
// the UI from showing "undefined" badges if the backend adds a new enum
// value before the frontend ships.
export function billStatusLabel(s: string | null | undefined, short = false): string {
  if (!s) return ''
  const map = short ? BILL_STATUS_LABEL_SHORT : BILL_STATUS_LABEL
  return map[s] ?? s
}

export function billSourceLabel(s: string | null | undefined): string {
  if (!s) return ''
  return BILL_SOURCE_LABEL[s] ?? s
}

export function billTypeLabel(t: string | null | undefined): string {
  if (!t) return ''
  return BILL_TYPE_LABEL[t] ?? t
}
