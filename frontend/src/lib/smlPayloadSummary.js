function asRecord(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

function firstPresent(...values) {
  return values.find((value) => value != null && value !== '')
}

export function documentItems(payload) {
  const record = asRecord(payload)
  if (Array.isArray(record.items) && record.items.length > 0) return record.items
  if (Array.isArray(record.details)) return record.details
  return Array.isArray(record.items) ? record.items : []
}

export function documentLocation(payload) {
  const record = asRecord(payload)
  const firstItem = asRecord(documentItems(record)[0])
  return {
    whCode: firstPresent(record.wh_code, record.wh_from, firstItem.wh_code, firstItem.wh_from),
    shelfCode: firstPresent(
      record.shelf_code,
      record.location_from,
      firstItem.shelf_code,
      firstItem.location_from,
    ),
  }
}

export function documentSendPresentation(bill) {
  const record = asRecord(bill)
  const coreComplete = record.status === 'sent'
    || ['created', 'already_exists', 'complete'].includes(record.sml_core_status)
  const hasProfile = Boolean(record.sml_profile_version || record.sml_profile_status)
  const profileComplete = !hasProfile || record.sml_profile_status === 'complete'
  const hasStockJob = Boolean(record.sml_stock_job_status)
  const stockComplete = !hasStockJob || record.sml_stock_job_status === 'completed'
  const complete = coreComplete && profileComplete && stockComplete

  return {
    complete,
    headline: record.sml_sent_automatically ? 'ส่งเข้า SML แล้ว (AUTO)' : 'ส่งเข้า SML แล้ว',
    detail: hasStockJob && stockComplete
      ? 'สร้างเอกสารและคำนวณต้นทุนสต๊อกเรียบร้อย'
      : 'สร้างเอกสารใน SML เรียบร้อย',
  }
}
