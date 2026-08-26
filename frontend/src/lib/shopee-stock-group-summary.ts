export type ShopeeStockGroupSummaryInput = {
  summary_count: number
  sml_usable_total?: number | null
  sml_base_unit_code?: string
  sml_base_unit_name?: string
  sml_total_status: string
  shopee_stock_total: number
  target_stock_total?: number | null
  target_count: number
  changed_count: number
}

export type ShopeeStockGroupSummaryView = {
  smlValue: number | null
  smlUnit: string
  smlStatusText: string
  shopeeValue: number
  targetValue: number | null
  targetStatusText: string
  changeText: string
}

function unitLabel(code?: string, name?: string) {
  const normalizedCode = code?.trim() ?? ''
  const normalizedName = name?.trim() ?? ''
  if (normalizedName && normalizedCode && normalizedName !== normalizedCode) return normalizedName
  return normalizedName || normalizedCode
}

export function buildShopeeStockGroupSummary(
  group: ShopeeStockGroupSummaryInput,
  previewStale: boolean,
): ShopeeStockGroupSummaryView {
  let smlStatusText = previewStale ? 'ตรวจใหม่' : ''
  if (!previewStale && group.sml_total_status === 'shared_source') smlStatusText = 'สต๊อกร่วม'
  else if (!previewStale && group.sml_total_status === 'mixed_unit') smlStatusText = 'หลายหน่วย'
  else if (!previewStale && (group.sml_total_status !== 'ready' || group.sml_usable_total == null)) smlStatusText = 'รอตรวจ'

  const targetMissingCount = Math.max(0, group.summary_count - group.target_count)
  const targetStatusText = previewStale
    ? 'ตรวจใหม่'
    : group.target_stock_total == null
      ? targetMissingCount > 0
        ? `รอตรวจ ${targetMissingCount.toLocaleString('th-TH')} ตัวเลือก`
        : 'รอตรวจ'
      : ''
  const targetReady = !previewStale && group.target_stock_total != null

  return {
    smlValue: smlStatusText ? null : group.sml_usable_total ?? null,
    smlUnit: smlStatusText ? '' : unitLabel(group.sml_base_unit_code, group.sml_base_unit_name),
    smlStatusText,
    shopeeValue: group.shopee_stock_total,
    targetValue: targetReady ? group.target_stock_total ?? null : null,
    targetStatusText,
    changeText: targetReady
      ? group.changed_count > 0
        ? `เปลี่ยน ${group.changed_count.toLocaleString('th-TH')} ตัวเลือก`
        : 'ตรงกันแล้ว'
      : '',
  }
}
