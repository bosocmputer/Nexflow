import { create } from 'zustand'

export type NotificationSeverity = 'info' | 'warning' | 'error'

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
  created_at: string
}

interface NotificationsState {
  unread: number
  items: AppNotification[]
  setUnread: (n: number) => void
  setItems: (items: AppNotification[]) => void
  upsertFromEvent: (notification: AppNotification, unread: number) => void
  markReadLocal: (id: string, unread: number) => void
  markAllReadLocal: () => void
}

export const useNotificationsStore = create<NotificationsState>((set) => ({
  unread: 0,
  items: [],
  setUnread: (n) => set({ unread: Math.max(0, Number(n) || 0) }),
  setItems: (items) => set({ items }),
  upsertFromEvent: (notification, unread) =>
    set((state) => ({
      unread: Math.max(0, Number(unread) || 0),
      items: [
        notification,
        ...state.items.filter((item) => item.id !== notification.id),
      ].slice(0, 50),
    })),
  markReadLocal: (id, unread) =>
    set((state) => ({
      unread: Math.max(0, Number(unread) || 0),
      items: state.items.map((item) =>
        item.id === id ? { ...item, read_at: item.read_at || new Date().toISOString() } : item,
      ),
    })),
  markAllReadLocal: () =>
    set((state) => ({
      unread: 0,
      items: state.items.map((item) => ({ ...item, read_at: item.read_at || new Date().toISOString() })),
    })),
}))
