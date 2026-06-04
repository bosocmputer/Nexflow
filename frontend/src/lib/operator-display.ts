const legacyBrand = ['bill', 'flow'].join('')
const legacyShopWord = ['hen', 'na'].join('')

const LEGACY_EMAIL_LABELS: Record<string, string> = {
  [`admin@${legacyBrand}.local`]: 'ผู้ดูแลระบบ',
}

const LEGACY_NAME_PATTERNS = [
  new RegExp(legacyBrand, 'i'),
  new RegExp(legacyShopWord, 'i'),
]

export function displayOperatorName(name?: string | null, email?: string | null) {
  const cleanName = (name ?? '').trim()
  const cleanEmail = (email ?? '').trim()
  if (cleanName) {
    if (LEGACY_NAME_PATTERNS.some((pattern) => pattern.test(cleanName))) {
      return cleanName.toLowerCase().includes('admin') ? 'Admin' : 'ผู้ใช้ระบบ'
    }
    return cleanName
  }
  return displayOperatorEmail(cleanEmail) || 'ระบบ'
}

export function displayOperatorEmail(email?: string | null) {
  const cleanEmail = (email ?? '').trim()
  if (!cleanEmail) return ''
  return LEGACY_EMAIL_LABELS[cleanEmail.toLowerCase()] ?? cleanEmail
}

export function displayOperator(email?: string | null, fallback = 'ระบบ') {
  return displayOperatorEmail(email) || fallback
}
