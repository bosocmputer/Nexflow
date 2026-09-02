export function validateProfileText(value) {
  if (typeof value !== 'string') return 'ต้องเป็นข้อความ'
  if (Array.from(value).length > 255) return 'ต้องไม่เกิน 255 ตัวอักษร'
  if (/\p{Cc}/u.test(value)) return 'ห้ามมีอักขระควบคุมหรือขึ้นบรรทัดใหม่'
  return ''
}
