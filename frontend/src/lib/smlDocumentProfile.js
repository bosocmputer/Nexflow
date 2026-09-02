export const SML_DOCUMENT_PROFILE_TOKENS = ['{{channel}}', '{{order_ref}}', '{{bill_no}}']

export function validateProfileText(value, allowTokens = false) {
  if (typeof value !== 'string') return 'ต้องเป็นข้อความ'
  if (Array.from(value).length > 255) return 'ต้องไม่เกิน 255 ตัวอักษร'
  if (/\p{Cc}/u.test(value)) return 'ห้ามมีอักขระควบคุมหรือขึ้นบรรทัดใหม่'
  if (allowTokens) {
    let remainder = value
    for (const token of SML_DOCUMENT_PROFILE_TOKENS) remainder = remainder.replaceAll(token, '')
    if (remainder.includes('{{') || remainder.includes('}}')) return 'มี token ที่ระบบไม่รองรับ'
  }
  return ''
}

export function resolveProfileTemplate(template, context) {
  const error = validateProfileText(template, true)
  if (error) throw new Error(error)
  const values = {
    '{{channel}}': context.channel ?? '',
    '{{order_ref}}': context.order_ref ?? '',
    '{{bill_no}}': context.bill_no ?? '',
  }
  let resolved = template
  for (const token of SML_DOCUMENT_PROFILE_TOKENS) resolved = resolved.replaceAll(token, values[token])
  const resolvedError = validateProfileText(resolved)
  if (resolvedError) throw new Error(resolvedError)
  return resolved
}
