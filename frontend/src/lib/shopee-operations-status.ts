export function shouldMergeAutoSMLSuccessStatus(
  erpStatus: string,
  smlDocNo?: string,
  autoSMLStatus?: string,
) {
  return erpStatus.trim().toLowerCase() === 'sent'
    && Boolean(smlDocNo?.trim())
    && autoSMLStatus?.trim().toLowerCase() === 'succeeded'
}

export function shouldShowAutoSMLStatusBadge(
  autoSMLStatus?: string,
  mergedIntoERPStatus = false,
) {
  if (mergedIntoERPStatus) return false
  const normalized = autoSMLStatus?.trim().toLowerCase() ?? ''
  return normalized !== '' && normalized !== 'manual_required'
}

export function cancellationDocumentTypeLabel(documentType?: string) {
  const normalized = documentType?.trim().toLowerCase() ?? ''
  if (normalized === 'sale_cancel') {
    return 'ยกเลิกขายสินค้าและบริการ'
  }
  if (normalized === 'credit_note') {
    return 'รับคืนสินค้า/ลดหนี้'
  }
  return 'รอสร้างตามเส้นทางที่ตั้งไว้'
}

export function cancellationTriggerLabel(source?: string) {
  switch (source?.trim().toLowerCase()) {
    case 'auto':
      return 'AUTO'
    case 'manual':
      return 'ผู้ใช้สร้าง'
    default:
      return ''
  }
}

export function cancellationStockRecalcLabel(status?: string) {
  switch (status?.trim().toLowerCase()) {
    case 'succeeded':
      return { label: 'คำนวณสต๊อก SML แล้ว', tone: 'success' as const }
    case 'running':
      return { label: 'กำลังคำนวณสต๊อก SML', tone: 'info' as const }
    case 'pending':
    case 'failed':
      return { label: 'รอคำนวณสต๊อก SML', tone: 'info' as const }
    case 'manual_reconciliation':
      return { label: 'ต้องตรวจการคำนวณสต๊อก', tone: 'danger' as const }
    default:
      return null
  }
}
