import type { AppNotification } from '@/lib/notifications-store'

type NotificationIdentity = Pick<AppNotification, 'source' | 'entity_type' | 'entity_id'>

const storagePrefix = 'nexflow.notification-toast-suppression.v1:'
const defaultTtlMs = 90_000

function storageKey(identity: NotificationIdentity) {
  return `${storagePrefix}${identity.source}:${identity.entity_type}:${identity.entity_id}`
}

// A confirmed action already owns a centered progress dialog. Suppress only the
// matching SSE toast in this browser tab; the notification is still stored and
// remains visible in the topbar (and in other tabs/devices).
export function suppressNotificationToast(identity: NotificationIdentity, ttlMs = defaultTtlMs) {
  if (typeof window === 'undefined') return
  try {
    window.sessionStorage.setItem(storageKey(identity), String(Date.now() + ttlMs))
  } catch {
    // Browser privacy/storage settings must never block an irreversible action.
  }
}

export function shouldSuppressNotificationToast(identity: NotificationIdentity) {
  if (typeof window === 'undefined') return false
  const key = storageKey(identity)
  try {
    const expiresAt = Number(window.sessionStorage.getItem(key))
    if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
      window.sessionStorage.removeItem(key)
      return false
    }
    window.sessionStorage.removeItem(key)
    return true
  } catch {
    return false
  }
}
