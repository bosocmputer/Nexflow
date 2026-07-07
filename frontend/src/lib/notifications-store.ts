import { create } from 'zustand'

export type NotificationSeverity = 'info' | 'warning' | 'error'
export type NotificationUnreadBySource = Record<string, number>

export interface AppNotification {
  id: string
  source: string
  severity: NotificationSeverity
  title: string
  body: string
  action_url: string
  entity_type: string
  entity_id: string
  read_at?: string | null
  resolved_at?: string | null
  resolved_reason?: string | null
  created_at: string
}

interface NotificationsState {
  unread: number
  unreadBySource: NotificationUnreadBySource
  items: AppNotification[]
  setUnread: (n: number, bySource?: NotificationUnreadBySource) => void
  setItems: (items: AppNotification[]) => void
  upsertFromEvent: (notification: AppNotification, unread: number, bySource?: NotificationUnreadBySource) => void
  markReadLocal: (id: string, unread: number, bySource?: NotificationUnreadBySource) => void
  markEntityReadLocal: (entityType: string, entityID: string, unread: number, bySource?: NotificationUnreadBySource) => void
  markAllReadLocal: (bySource?: NotificationUnreadBySource) => void
}

export const useNotificationsStore = create<NotificationsState>((set) => ({
  unread: 0,
  unreadBySource: {},
  items: [],
  setUnread: (n, bySource) =>
    set({
      unread: Math.max(0, Number(n) || 0),
      unreadBySource: normalizeUnreadBySource(bySource),
    }),
  setItems: (items) => set({ items: items.filter((item) => !item.resolved_at) }),
  upsertFromEvent: (notification, unread, bySource) =>
    set((state) => ({
      unread: Math.max(0, Number(unread) || 0),
      unreadBySource: normalizeUnreadBySource(bySource),
      items: notification.resolved_at
        ? state.items.filter((item) => item.id !== notification.id)
        : [
            notification,
            ...state.items.filter((item) => item.id !== notification.id),
          ].slice(0, 50),
    })),
  markReadLocal: (id, unread, bySource) =>
    set((state) => ({
      unread: Math.max(0, Number(unread) || 0),
      unreadBySource: normalizeUnreadBySource(bySource),
      items: state.items.map((item) =>
        item.id === id ? { ...item, read_at: item.read_at || new Date().toISOString() } : item,
      ),
    })),
  markEntityReadLocal: (entityType, entityID, unread, bySource) =>
    set((state) => {
      const readAt = new Date().toISOString()
      return {
        unread: Math.max(0, Number(unread) || 0),
        unreadBySource: normalizeUnreadBySource(bySource),
        items: state.items.map((item) =>
          item.entity_type === entityType && item.entity_id === entityID
            ? { ...item, read_at: item.read_at || readAt }
            : item,
        ),
      }
    }),
  markAllReadLocal: (bySource) =>
    set((state) => ({
      unread: 0,
      unreadBySource: normalizeUnreadBySource(bySource),
      items: state.items.map((item) => ({ ...item, read_at: item.read_at || new Date().toISOString() })),
    })),
}))

function normalizeUnreadBySource(value?: NotificationUnreadBySource): NotificationUnreadBySource {
  if (!value || typeof value !== 'object') return {}
  return Object.fromEntries(
    Object.entries(value)
      .map(([source, count]) => [source, Math.max(0, Number(count) || 0)] as const)
      .filter(([source]) => source.trim() !== ''),
  )
}
