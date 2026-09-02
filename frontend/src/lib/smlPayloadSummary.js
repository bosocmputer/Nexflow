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
