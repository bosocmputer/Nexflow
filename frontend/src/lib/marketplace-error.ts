type APIErrorPayload = {
  error?: string | { code?: string; message?: string }
  message?: string
}

export function marketplaceErrorMessage(error: unknown, fallback: string) {
  const payload = (error as { response?: { data?: APIErrorPayload } })?.response?.data
  if (typeof payload?.message === 'string' && payload.message.trim()) return payload.message
  if (typeof payload?.error === 'string' && payload.error.trim()) return payload.error
  const nestedError = typeof payload?.error === 'object' ? payload.error : undefined
  if (typeof nestedError?.message === 'string' && nestedError.message.trim()) return nestedError.message
  return fallback
}
