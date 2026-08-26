export type ShopeeStockGroupSMLUnitTotal = {
  unit_code?: string
  unit_name?: string
  quantity: number
  source_count: number
}

export type ShopeeStockGroupSummaryInput = {
  summary_count: number
  sml_usable_total?: number | null
  sml_base_unit_code?: string
  sml_base_unit_name?: string
  sml_unit_totals?: ShopeeStockGroupSMLUnitTotal[]
  sml_total_status: string
  shopee_stock_total: number
  target_stock_total?: number | null
  target_count: number
  changed_count: number
}

export type ShopeeStockGroupSummaryView = {
  smlText: string
  smlWarningText: string
  smlTitle: string
  shopeeValue: number
  targetValue: number | null
  targetStatusText: string
  changeText: string
}

function unitLabel(code?: string, name?: string) {
  const normalizedCode = code?.trim() ?? ''
  const normalizedName = name?.trim() ?? ''
  if (normalizedName && normalizedCode && normalizedName !== normalizedCode) return normalizedName
  return normalizedName || normalizedCode || 'ไม่ทราบหน่วย'
}

function formatQuantity(value: number) {
  return new Intl.NumberFormat('th-TH', { maximumFractionDigits: 2 }).format(value)
}

function smlUnitTotals(group: ShopeeStockGroupSummaryInput) {
  const totals = (group.sml_unit_totals ?? []).filter((item) => Number.isFinite(item.quantity))
  if (totals.length > 0) return totals
  if (group.sml_usable_total == null || !Number.isFinite(group.sml_usable_total)) return []
  return [{
    unit_code: group.sml_base_unit_code,
    unit_name: group.sml_base_unit_name,
    quantity: group.sml_usable_total,
    source_count: group.summary_count,
  }]
}

export function buildShopeeStockGroupSummary(
  group: ShopeeStockGroupSummaryInput,
  previewStale: boolean,
): ShopeeStockGroupSummaryView {
  const totals = smlUnitTotals(group)
  const smlText = totals.length > 0
    ? totals.map((item) => `${formatQuantity(item.quantity)} ${unitLabel(item.unit_code, item.unit_name)}`).join(' · ')
    : 'รอตรวจ'

  let smlWarningText = ''
  let smlTitle = totals.length > 0 ? `ยอด SML พร้อมใช้รวม ${smlText}` : 'ยังไม่มียอดจากการตรวจสต๊อก'
  if (previewStale) {
    smlWarningText = totals.length > 0 ? 'ข้อมูลเดิม ควรตรวจใหม่' : 'ควรตรวจสต๊อกใหม่'
    smlTitle = totals.length > 0
      ? `ยอดจากการตรวจครั้งก่อน: ${smlText} · ข้อมูลเปลี่ยนแล้ว กรุณากดบันทึกและตรวจสต๊อกอีกครั้ง`
      : 'ข้อมูลเปลี่ยนแล้ว กรุณากดบันทึกและตรวจสต๊อกอีกครั้ง'
  } else if (group.sml_total_status === 'shared_source') {
    smlWarningText = 'ใช้ร่วมกับสินค้าอื่น · ควรตรวจตัวเลือก'
    smlTitle = totals.length > 0
      ? `ยอด SML พร้อมใช้ ${smlText} · ยอดนี้ใช้ร่วมกับสินค้า Shopee รายการอื่น ระบบไม่ได้นำยอดซ้ำมาบวกในแถวนี้`
      : 'ยังไม่มียอดจากการตรวจ และสินค้านี้ใช้สต๊อกร่วมกับรายการอื่น'
  } else if (group.sml_total_status === 'mixed_unit') {
    smlWarningText = 'มีหลายหน่วย · ควรตรวจตัวเลือก'
    smlTitle = totals.length > 0
      ? `ยอด SML พร้อมใช้แยกตามหน่วย: ${smlText} · หน่วยต่างกันจึงไม่นำมาบวกกัน`
      : 'ยังไม่มียอดจากการตรวจ และตัวเลือกใช้หน่วยต่างกัน'
  } else if (group.sml_total_status !== 'ready' || totals.length === 0) {
    smlWarningText = totals.length > 0 ? 'ข้อมูลยังไม่ครบ กดตรวจสต๊อก' : 'กดตรวจสต๊อก'
  }

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
    smlText,
    smlWarningText,
    smlTitle,
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
