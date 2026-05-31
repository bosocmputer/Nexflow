export const REMARK2_NONE = '__none__'

export const SML_REMARK2_OPTIONS = [
  { value: 'tax', label: 'tax — ใบกำกับภาษี' },
  { value: 'notax', label: 'notax — ไม่ใช่ใบกำกับภาษี' },
  { value: 're', label: 're — ใบเสร็จรับเงิน' },
] as const

const ALLOWED_REMARK2 = new Set<string>(SML_REMARK2_OPTIONS.map((option) => option.value))

export function normalizeRemark2(value?: string | null) {
  return value && ALLOWED_REMARK2.has(value) ? value : REMARK2_NONE
}

export function remark2Label(value?: string | null) {
  if (!value) return 'ไม่ระบุ'
  return SML_REMARK2_OPTIONS.find((option) => option.value === value)?.label ?? value
}

export function remark2PayloadValue(value: string) {
  return value === REMARK2_NONE ? undefined : value
}
