import type { MarketplaceAliasImpact } from '@/types'

export function marketplaceImpactFormulaLines(impact: MarketplaceAliasImpact) {
  const lines = [
    `สูตรเดิม: ${impact.before_formula || 'ยังไม่มีสูตร conversion ที่ยืนยันแล้ว'}`,
    `สูตรใหม่: ${impact.after_formula || 'ยังไม่มีสูตร conversion ที่พร้อมใช้'}`,
  ]
  if (impact.legacy_manual_factors > 0) {
    lines.push(`คำเตือน: พบ manual factor เดิม ${impact.legacy_manual_factors.toLocaleString()} รายการ หลังยืนยันระบบจะใช้สูตรใหม่แทน กรุณาตรวจจำนวนเดิมให้ตรงก่อนบันทึก`)
  }
  if (impact.legacy_exclusions > 0) {
    lines.push(`รายการที่ยกเว้นไว้เดิม ${impact.legacy_exclusions.toLocaleString()} รายการจะยังคงถูกยกเว้น`)
  }
  return lines
}
