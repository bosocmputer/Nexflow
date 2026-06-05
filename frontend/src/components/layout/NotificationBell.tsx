import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Bell, CheckCheck, CircleDot, RadioTower } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useAuth } from '@/hooks/useAuth'
import { type ServerEventType, useEventsStore } from '@/lib/events-store'
import { type AppNotification, useNotificationsStore } from '@/lib/notifications-store'
import { cn } from '@/lib/utils'

type NotificationListResponse = {
  data: AppNotification[]
  unread: number
}

const severityMeta = {
  error: {
    label: 'ต้องแก้',
    icon: AlertTriangle,
    tone: 'border-destructive/30 bg-destructive/10 text-destructive',
  },
  warning: {
    label: 'งานใหม่',
    icon: CircleDot,
    tone: 'border-warning/40 bg-warning/10 text-foreground',
  },
  info: {
    label: 'ข้อมูลอัปเดต',
    icon: RadioTower,
    tone: 'border-info/35 bg-info/10 text-info',
  },
} satisfies Record<AppNotification['severity'], { label: string; icon: typeof Bell; tone: string }>

export function NotificationBell() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const subscribe = useEventsStore((s) => s.subscribe)
  const unread = useNotificationsStore((s) => s.unread)
  const items = useNotificationsStore((s) => s.items)
  const setUnread = useNotificationsStore((s) => s.setUnread)
  const setItems = useNotificationsStore((s) => s.setItems)
  const upsertFromEvent = useNotificationsStore((s) => s.upsertFromEvent)
  const markReadLocal = useNotificationsStore((s) => s.markReadLocal)
  const markAllReadLocal = useNotificationsStore((s) => s.markAllReadLocal)

  const canUseNotifications = user?.role === 'admin' || user?.role === 'staff'

  const grouped = useMemo(() => {
    const groups: Record<AppNotification['severity'], AppNotification[]> = {
      error: [],
      warning: [],
      info: [],
    }
    items.forEach((item) => {
      groups[item.severity || 'info'].push(item)
    })
    return groups
  }, [items])

  const loadNotifications = async () => {
    if (!canUseNotifications) return
    setLoading(true)
    try {
      const res = await client.get<NotificationListResponse>('/api/notifications?limit=30')
      setItems(res.data.data ?? [])
      setUnread(res.data.unread ?? 0)
    } catch {
      /* avoid noisy topbar errors */
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadNotifications()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canUseNotifications])

  useEffect(() => {
    if (!canUseNotifications) return
    return subscribe((type: ServerEventType, payload: any) => {
      if (type === 'notification_unread_changed') {
        setUnread(payload?.total ?? 0)
        return
      }
      if (type !== 'notification_created') return
      const notification = payload?.notification as AppNotification | undefined
      if (!notification?.id) return
      const nextUnread = Number(payload?.unread_count ?? unread + 1)
      upsertFromEvent(notification, nextUnread)
      const opts = notification.body ? { description: notification.body } : undefined
      if (notification.severity === 'error') toast.error(notification.title, opts)
      else if (notification.severity === 'warning') toast.warning(notification.title, opts)
      else toast.info(notification.title, opts)
    })
  }, [canUseNotifications, setUnread, subscribe, unread, upsertFromEvent])

  const markOneRead = async (notification: AppNotification, navigateToAction: boolean) => {
    try {
      const res = await client.post<{ unread: number }>(`/api/notifications/${notification.id}/read`)
      markReadLocal(notification.id, res.data.unread ?? Math.max(0, unread - 1))
    } catch {
      /* navigation remains useful even if read write fails */
    }
    if (navigateToAction && notification.action_url) {
      setOpen(false)
      navigate(notification.action_url)
    }
  }

  const markAllRead = async () => {
    try {
      await client.post('/api/notifications/read-all')
      markAllReadLocal()
    } catch {
      toast.error('อ่าน notification ทั้งหมดไม่สำเร็จ')
    }
  }

  if (!canUseNotifications) return null

  return (
    <Popover open={open} onOpenChange={(next) => {
      setOpen(next)
      if (next) void loadNotifications()
    }}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="relative inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-card text-foreground shadow-sm transition-colors hover:bg-accent/70"
          aria-label="เปิดการแจ้งเตือน"
        >
          <Bell className="h-4 w-4" />
          {unread > 0 && (
            <span className="absolute -right-1 -top-1 flex h-5 min-w-5 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-semibold text-destructive-foreground">
              {unread > 99 ? '99+' : unread}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(92vw,420px)] p-0">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div>
            <div className="text-sm font-semibold text-foreground">การแจ้งเตือน</div>
            <div className="text-xs text-muted-foreground">
              คำสั่งซื้อ Shopee และงานที่ต้องตรวจ
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-8 text-xs"
            onClick={markAllRead}
            disabled={unread === 0}
          >
            <CheckCheck className="h-3.5 w-3.5" />
            อ่านทั้งหมด
          </Button>
        </div>
        <ScrollArea className="max-h-[min(70vh,520px)]">
          <div className="space-y-3 p-3">
            {loading && items.length === 0 && (
              <div className="rounded-md border border-border bg-muted/30 px-3 py-5 text-center text-sm text-muted-foreground">
                กำลังโหลด
              </div>
            )}
            {!loading && items.length === 0 && (
              <div className="rounded-md border border-border bg-muted/30 px-3 py-6 text-center">
                <div className="text-sm font-medium text-foreground">ยังไม่มีการแจ้งเตือน</div>
                <div className="mt-1 text-xs text-muted-foreground">
                  เมื่อมี order ใหม่หรือจุดที่ต้องแก้ ระบบจะแจ้งที่นี่
                </div>
              </div>
            )}
            {(['error', 'warning', 'info'] as const).map((severity) => {
              const list = grouped[severity]
              if (list.length === 0) return null
              const meta = severityMeta[severity]
              const Icon = meta.icon
              return (
                <section key={severity} className="space-y-2">
                  <div className="flex items-center gap-2 px-1 text-xs font-semibold text-muted-foreground">
                    <Icon className="h-3.5 w-3.5" />
                    {meta.label}
                    <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                      {list.length}
                    </Badge>
                  </div>
                  <div className="space-y-1.5">
                    {list.map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        className={cn(
                          'w-full rounded-md border px-3 py-2 text-left transition-colors hover:bg-accent/60',
                          item.read_at ? 'border-border bg-card' : meta.tone,
                        )}
                        onClick={() => void markOneRead(item, true)}
                      >
                        <div className="flex items-start gap-2">
                          <span
                            className={cn(
                              'mt-1 h-1.5 w-1.5 shrink-0 rounded-full',
                              item.read_at ? 'bg-muted-foreground/30' : 'bg-current',
                            )}
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block text-sm font-medium text-foreground">
                              {item.title}
                            </span>
                            {item.body && (
                              <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">
                                {item.body}
                              </span>
                            )}
                            <span className="mt-1 block text-[11px] text-muted-foreground">
                              {formatNotificationTime(item.created_at)}
                            </span>
                          </span>
                        </div>
                      </button>
                    ))}
                  </div>
                </section>
              )
            })}
          </div>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  )
}

function formatNotificationTime(value: string): string {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat('th-TH', {
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(d)
}
