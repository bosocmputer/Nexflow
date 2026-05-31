// ── Shared constants and pure helpers for BillDetail ─────────────────────────

export const SOURCE_LABELS: Record<string, string> = {
  line: 'LINE OA',
  email: 'Email',
  lazada: 'Lazada',
  tiktok: 'TikTok Excel',
  shopee: 'Shopee',
  shopee_email: 'Shopee Email',
  shopee_shipped: 'Email บิลซื้อ Shopee',
  manual: 'เพิ่มเอง',
}

export const FLOW_META: Record<
  string,
  { label: string; icon: string; variant: string }
> = {
  email_pdf: {
    label: 'Email + PDF',
    icon: '📎',
    variant: 'bg-blue-100 text-blue-800',
  },
  shopee_email_order: {
    label: 'Shopee Email (Order)',
    icon: '🛒',
    variant: 'bg-orange-100 text-orange-800',
  },
  shopee_shipped: {
    // Both COD-shipped emails ("ถูกจัดส่งแล้ว") and pay-now confirmation
    // emails ("ยืนยันการชำระเงิน") route to this flow, so the label can't
    // claim the package shipped. Frame it by outcome: produces a PO bill.
    label: 'Email บิลซื้อ Shopee',
    icon: '📦',
    variant: 'bg-amber-100 text-amber-800',
  },
  shopee_excel: {
    label: 'Shopee',
    icon: '📊',
    variant: 'bg-green-100 text-green-700',
  },
  tiktok_excel: {
    label: 'TikTok Excel',
    icon: '📊',
    variant: 'bg-slate-100 text-slate-700',
  },
}

export const KIND_META: Record<
  string,
  { icon: string; label: string; desc: string }
> = {
  email_pdf: {
    icon: '📄',
    label: 'PDF ต้นฉบับ',
    desc: 'ไฟล์แนบ PDF จากอีเมล (เช่นใบสั่งซื้อ/ใบเสร็จ) — bytes เดียวกับที่ลูกค้าได้รับ',
  },
  email_html: {
    icon: '📧',
    label: 'อีเมลต้นฉบับ',
    desc: 'เนื้ออีเมลฉบับเต็ม เปิดดูหรือพิมพ์เพื่อย้อนตรวจหลักฐานจากต้นทาง',
  },
  email_text: {
    icon: '📧',
    label: 'อีเมลต้นฉบับ',
    desc: 'เนื้ออีเมลต้นฉบับแบบข้อความ',
  },
  email_envelope: {
    icon: '📨',
    label: 'Email envelope',
    desc: 'Metadata ของอีเมล (subject / from / message_id) เก็บแยกเป็น JSON เผื่อย้อนตรวจที่มาได้แม้ตัว body ใหญ่เกินบันทึก',
  },
  xlsx: {
    icon: '📊',
    label: 'Marketplace Excel',
    desc: 'ไฟล์ Excel ต้นฉบับที่ผู้ใช้อัปโหลด',
  },
  csv: {
    icon: '📊',
    label: 'Marketplace CSV',
    desc: 'ไฟล์ CSV ต้นฉบับที่ผู้ใช้อัปโหลด',
  },
  image: {
    icon: '🖼️',
    label: 'รูปภาพ',
    desc: 'รูปต้นฉบับที่ส่งเข้า LINE OA',
  },
  audio: {
    icon: '🎙️',
    label: 'ไฟล์เสียง',
    desc: 'voice message ต้นฉบับจาก LINE OA',
  },
  chat_history: {
    icon: '💬',
    label: 'LINE chat',
    desc: 'ประวัติแชท LINE ที่นำมาสร้างบิลนี้',
  },
}

const HIDDEN_USER_ARTIFACT_KINDS = new Set([
  'email_envelope',
  'email_raw',
  'email_debug',
  'debug_json',
  'api_payload',
  'sml_payload',
])

export function isUserVisibleArtifact(kind: string): boolean {
  return !HIDDEN_USER_ARTIFACT_KINDS.has(kind)
}

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

/** Returns Tailwind classes for a score value */
export function scoreStyle(score: number | null): {
  color: string
  bg: string
  label: string
  icon: string
} {
  if (score == null)
    return { color: 'text-muted-foreground', bg: 'bg-muted', label: 'เลือกเอง', icon: '✎' }
  const pct = Math.round(score * 100)
  if (score >= 0.85)
    return { color: 'text-green-700', bg: 'bg-green-100', label: `${pct}%`, icon: '✓' }
  if (score >= 0.6)
    return { color: 'text-amber-700', bg: 'bg-amber-100', label: `${pct}%`, icon: '⚠' }
  return { color: 'text-red-700', bg: 'bg-red-100', label: `${pct}%`, icon: '⚠' }
}

/** Returns raw hex/css color for inline status accents. */
export function scoreColor(score: number | null): string {
  if (score == null) return '#94a3b8'
  if (score >= 0.85) return '#15803d'
  if (score >= 0.6) return '#a16207'
  return '#b91c1c'
}

/** Returns a border-color class string for catalog result buttons */
export function scoreBorderClass(score: number): string {
  if (score >= 0.85) return 'border-green-500'
  if (score >= 0.6) return 'border-amber-400'
  return 'border-red-400'
}
