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

type CancellationDocumentSummaryInput = {
  status?: string
  documentType?: string
  triggerSource?: string
  saleDocNo?: string
  cancelDocNo?: string
}

export function cancellationDocumentSummary(input: CancellationDocumentSummaryInput) {
  const status = input.status?.trim().toLowerCase() ?? ''
  const normalizedDocumentType = input.documentType?.trim().toLowerCase() ?? ''
  const documentType = normalizedDocumentType === 'sale_cancel' || normalizedDocumentType === 'credit_note'
    ? cancellationDocumentTypeLabel(normalizedDocumentType)
    : 'เอกสารหลังยกเลิก'
  const trigger = cancellationTriggerLabel(input.triggerSource)
  const triggerSuffix = trigger ? ` (${trigger})` : ''

  let headline = `รอสร้าง${documentType}${triggerSuffix}`
  let tone: 'danger' | 'info' = 'danger'
  if (status === 'created' || status === 'already_exists') {
    headline = `${documentType}แล้ว${triggerSuffix}`
  } else if (status === 'creating') {
    headline = `กำลังสร้าง${documentType}${triggerSuffix}`
    tone = 'info'
  } else if (status === 'pending' || status === 'previewed') {
    headline = `รอสร้าง${documentType}${triggerSuffix}`
    tone = 'info'
  } else if (status === 'failed') {
    headline = `สร้าง${documentType}ไม่สำเร็จ${triggerSuffix}`
  } else if (status === 'blocked') {
    headline = `${documentType}ต้องตรวจ${triggerSuffix}`
  }

  const saleDocNo = input.saleDocNo?.trim() || 'ไม่มีเลขใบขาย'
  const cancelDocNo = input.cancelDocNo?.trim() || 'รอเลขเอกสาร'
  return {
    headline,
    documentLine: `${saleDocNo} → ${cancelDocNo}`,
    tone,
  }
}
