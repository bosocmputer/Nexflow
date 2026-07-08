import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, Bell, CheckCheck, CircleDot, RadioTower, Volume2, VolumeX } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useAuth } from '@/hooks/useAuth'
import { type ServerEventType, useEventsStore } from '@/lib/events-store'
import { type AppNotification, type NotificationUnreadBySource, useNotificationsStore } from '@/lib/notifications-store'
import { cn } from '@/lib/utils'

type NotificationListResponse = {
  data: AppNotification[]
  unread: number
  unread_by_source?: NotificationUnreadBySource
}

type NotificationWriteResponse = {
  unread: number
  unread_by_source?: NotificationUnreadBySource
}

const SOUND_STORAGE_KEY = 'nexflow.notifications.sound_enabled'
const ORDER_ALERT_SEEN_STORAGE_KEY = 'nexflow.notifications.order_alert_seen_at'
const SOUND_BURST_WINDOW_MS = 900
const ORDER_ALARM_REPEAT_MS = 6000

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
  const [soundEnabled, setSoundEnabled] = useState(() => {
    if (typeof window === 'undefined') return true
    try {
      const stored = window.localStorage.getItem(SOUND_STORAGE_KEY)
      return stored === null ? true : stored === '1'
    } catch {
      return true
    }
  })
  const [orderAlertSeenAt, setOrderAlertSeenAt] = useState(() => {
    if (typeof window === 'undefined') return 0
    try {
      return Math.max(0, Number(window.localStorage.getItem(ORDER_ALERT_SEEN_STORAGE_KEY)) || 0)
    } catch {
      return 0
    }
  })
  const audioCtxRef = useRef<AudioContext | null>(null)
  const lastSoundAtRef = useRef(0)
  const alarmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const subscribe = useEventsStore((s) => s.subscribe)
  const unread = useNotificationsStore((s) => s.unread)
  const items = useNotificationsStore((s) => s.items)
  const setUnread = useNotificationsStore((s) => s.setUnread)
  const setItems = useNotificationsStore((s) => s.setItems)
  const upsertFromEvent = useNotificationsStore((s) => s.upsertFromEvent)
  const markReadLocal = useNotificationsStore((s) => s.markReadLocal)
  const markAllReadLocal = useNotificationsStore((s) => s.markAllReadLocal)

  const canUseNotifications = user?.role === 'admin' || user?.role === 'staff'

  useEffect(() => {
    if (typeof window === 'undefined') return
    try {
      window.localStorage.setItem(SOUND_STORAGE_KEY, soundEnabled ? '1' : '0')
    } catch {
      /* localStorage can be unavailable in private contexts. */
    }
  }, [soundEnabled])

  const stopOrderAlarm = useCallback(() => {
    if (alarmTimerRef.current) {
      clearTimeout(alarmTimerRef.current)
      alarmTimerRef.current = null
    }
  }, [])

  const getAudioContext = useCallback(() => {
    if (typeof window === 'undefined') return null
    if (!audioCtxRef.current) {
      const Ctx = window.AudioContext || (window as any).webkitAudioContext
      if (!Ctx) return null
      audioCtxRef.current = new Ctx()
    }
    return audioCtxRef.current
  }, [])

  const playBellStrike = useCallback((ctx: AudioContext, startAt: number) => {
    const master = ctx.createGain()
    master.gain.setValueAtTime(0.0001, startAt)
    master.gain.exponentialRampToValueAtTime(0.32, startAt + 0.015)
    master.gain.exponentialRampToValueAtTime(0.0001, startAt + 0.28)
    master.connect(ctx.destination)

    const partials = [
      { frequency: 1046.5, type: 'sine' as OscillatorType },
      { frequency: 1568, type: 'triangle' as OscillatorType },
    ]
    partials.forEach(({ frequency, type }) => {
      const osc = ctx.createOscillator()
      osc.type = type
      osc.frequency.setValueAtTime(frequency, startAt)
      osc.frequency.exponentialRampToValueAtTime(frequency * 0.985, startAt + 0.25)
      osc.connect(master)
      osc.start(startAt)
      osc.stop(startAt + 0.3)
    })
  }, [])

  const playBellBurstOnContext = useCallback((ctx: AudioContext) => {
    const start = ctx.currentTime
    playBellStrike(ctx, start)
    playBellStrike(ctx, start + 0.34)
    playBellStrike(ctx, start + 0.68)
  }, [playBellStrike])

  useEffect(() => {
    if (!soundEnabled || typeof window === 'undefined') return
    const unlock = () => {
      try {
        const ctx = getAudioContext()
        if (ctx?.state === 'suspended') {
          void ctx.resume().catch(() => {
            /* Browser may still block until a later gesture. */
          })
        }
      } catch {
        /* Audio unlock is best effort only. */
      }
    }
    window.addEventListener('pointerdown', unlock, { once: true, passive: true })
    window.addEventListener('keydown', unlock, { once: true })
    return () => {
      window.removeEventListener('pointerdown', unlock)
      window.removeEventListener('keydown', unlock)
    }
  }, [getAudioContext, soundEnabled])

  const playNotificationSound = useCallback((force = false) => {
    if (!force && !soundEnabled) return
    const nowMs = Date.now()
    if (!force && nowMs - lastSoundAtRef.current < SOUND_BURST_WINDOW_MS) return
    lastSoundAtRef.current = nowMs
    try {
      const ctx = getAudioContext()
      if (!ctx) return
      if (ctx.state === 'suspended') {
        void ctx.resume().then(() => playBellBurstOnContext(ctx)).catch(() => {
          /* Browser may still block until the next user gesture. */
        })
        return
      }
      playBellBurstOnContext(ctx)
    } catch {
      /* Audio is a convenience, never a blocking notification path. */
    }
  }, [getAudioContext, playBellBurstOnContext, soundEnabled])

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

  const latestUnreadOrderAlertAt = useMemo(() => {
    return items.reduce((latest, item) => {
      if (item.read_at || item.resolved_at || !isOrderAlertNotification(item)) return latest
      return Math.max(latest, notificationTimeMs(item.created_at))
    }, 0)
  }, [items])

  const orderAttentionActive = latestUnreadOrderAlertAt > orderAlertSeenAt

  const acknowledgeOrderAlerts = useCallback(() => {
    const nextSeenAt = Math.max(Date.now(), latestUnreadOrderAlertAt)
    setOrderAlertSeenAt(nextSeenAt)
    stopOrderAlarm()
    if (typeof window === 'undefined') return
    try {
      window.localStorage.setItem(ORDER_ALERT_SEEN_STORAGE_KEY, String(nextSeenAt))
    } catch {
      /* localStorage can be unavailable in private contexts. */
    }
  }, [latestUnreadOrderAlertAt, stopOrderAlarm])

  useEffect(() => {
    if (!canUseNotifications || open || !soundEnabled || !orderAttentionActive) {
      stopOrderAlarm()
      return
    }
    let cancelled = false
    const tick = () => {
      if (cancelled) return
      playNotificationSound(true)
      alarmTimerRef.current = setTimeout(tick, ORDER_ALARM_REPEAT_MS)
    }
    stopOrderAlarm()
    tick()
    return () => {
      cancelled = true
      stopOrderAlarm()
    }
  }, [canUseNotifications, open, orderAttentionActive, playNotificationSound, soundEnabled, stopOrderAlarm])

  useEffect(() => {
    if (open && orderAttentionActive) {
      acknowledgeOrderAlerts()
    }
  }, [acknowledgeOrderAlerts, open, orderAttentionActive])

  const loadNotifications = async () => {
    if (!canUseNotifications) return
    setLoading(true)
    try {
      const res = await client.get<NotificationListResponse>('/api/notifications?limit=30')
      setItems(res.data.data ?? [])
      setUnread(res.data.unread ?? 0, res.data.unread_by_source)
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
        setUnread(payload?.total ?? 0, payload?.unread_by_source)
        return
      }
      if (type !== 'notification_created') return
      const notification = payload?.notification as AppNotification | undefined
      if (!notification?.id) return
      const nextUnread = Number(payload?.unread_count ?? unread + 1)
      upsertFromEvent(notification, nextUnread, payload?.unread_by_source)
      const opts = notification.body ? { description: notification.body } : undefined
      if (notification.severity === 'error') toast.error(notification.title, opts)
      else if (notification.severity === 'warning') toast.warning(notification.title, opts)
      else toast.info(notification.title, opts)
    })
  }, [canUseNotifications, setUnread, subscribe, unread, upsertFromEvent])

  const markOneRead = async (notification: AppNotification, navigateToAction: boolean) => {
    try {
      const res = await client.post<NotificationWriteResponse>(`/api/notifications/${notification.id}/read`)
      markReadLocal(notification.id, res.data.unread ?? Math.max(0, unread - 1), res.data.unread_by_source)
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
      const res = await client.post<NotificationWriteResponse>('/api/notifications/read-all')
      markAllReadLocal(res.data.unread_by_source)
      acknowledgeOrderAlerts()
    } catch {
      toast.error('อ่าน notification ทั้งหมดไม่สำเร็จ')
    }
  }

  const toggleSound = () => {
    const next = !soundEnabled
    setSoundEnabled(next)
    if (!next) {
      stopOrderAlarm()
      return
    }
    playNotificationSound(true)
  }

  if (!canUseNotifications) return null

  return (
    <Popover open={open} onOpenChange={(next) => {
      setOpen(next)
      if (next) {
        acknowledgeOrderAlerts()
        void loadNotifications()
      }
    }}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            'relative inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-card text-foreground shadow-sm transition-colors hover:bg-accent/70',
            orderAttentionActive && 'notification-bell-attention border-warning/60 bg-warning/10 text-warning hover:bg-warning/15',
          )}
          aria-label="เปิดการแจ้งเตือน"
        >
          {orderAttentionActive && (
            <span className="notification-bell-pulse pointer-events-none absolute inset-0 rounded-md border border-warning/50" />
          )}
          <Bell className={cn('h-4 w-4', orderAttentionActive && 'notification-bell-icon-attention')} />
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
              ออเดอร์ใหม่และงานที่ต้องตรวจ
            </div>
          </div>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 px-2 text-xs"
              onClick={toggleSound}
              aria-pressed={soundEnabled}
              aria-label={soundEnabled ? 'ปิดเสียงแจ้งเตือน' : 'เปิดเสียงแจ้งเตือน'}
              title={soundEnabled ? 'ปิดเสียงแจ้งเตือน' : 'เปิดเสียงแจ้งเตือน'}
            >
              {soundEnabled ? <Volume2 className="h-3.5 w-3.5" /> : <VolumeX className="h-3.5 w-3.5" />}
              <span className="hidden sm:inline">{soundEnabled ? 'เสียงเปิด' : 'เสียงปิด'}</span>
            </Button>
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
        </div>
        <div className="max-h-[min(70vh,520px)] overflow-y-auto overscroll-contain">
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
        </div>
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

function isOrderAlertNotification(notification: AppNotification): boolean {
  const key = notification.dedupe_key?.trim() ?? ''
  return key.startsWith('nextstep:new_order:') || key.startsWith('shopee:new_order:')
}

function notificationTimeMs(value: string): number {
  if (!value) return 0
  const n = new Date(value).getTime()
  return Number.isFinite(n) ? n : 0
}
