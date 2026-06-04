import { useCallback, useEffect, useRef, useState } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import {
  ChevronsLeft,
  ChevronsRight,
  LogOut,
} from 'lucide-react'

import { useChatEvents } from '@/hooks/useChatEvents'
import { useEventsStore, type EventsConnectionState } from '@/lib/events-store'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { ThemeToggle } from '@/components/common/ThemeToggle'
import { NexflowLogo } from '@/components/common/NexflowLogo'
import { useAuth } from '@/hooks/useAuth'
import { useNotificationsStore } from '@/lib/notifications-store'
import { useUIStore } from '@/lib/ui-store'
import { cn } from '@/lib/utils'
import { WORK_QUEUE_CHANGED_EVENT } from '@/lib/work-queue-events'
import { ENABLE_CHAT, ENABLE_SALES_ORDERS } from '@/lib/featureFlags'
import { visibleNavGroups, type NavGroup } from '@/lib/navigation'
import client from '@/api/client'

// VITE_PHASE controls which nav items are visible.
//   1  = Phase 1 only (Email → PO) — hides LINE chat, marketplace imports
//   99 = all features (default when unset)
const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

async function countDocumentQueue(base: Record<string, string>) {
  const statuses = ['pending', 'needs_review', 'failed']
  const results = await Promise.all(
    statuses.map(async (status) => {
      const params = new URLSearchParams({ ...base, status, page: '1', per_page: '1' })
      const res = await client.get<{ total: number }>(`/api/bills?${params}`)
      return res.data.total ?? 0
    }),
  )
  return results.reduce((sum, n) => sum + n, 0)
}

async function countMarketplaceAliasQueue() {
  if (!ENABLE_SALES_ORDERS) return 0
  try {
    const params = new URLSearchParams({ bill_type: 'sale', page: '1', per_page: '1' })
    const res = await client.get<{ total?: number }>(`/api/marketplace-aliases/review-groups?${params}`)
    return res.data.total ?? 0
  } catch {
    return 0
  }
}

const URGENT_BADGES = new Set([
  'bills',
  'purchase',
  'saleorder',
  'saleinvoice',
  'marketplace_aliases',
  'shopee_realtime',
])

const ROLE_LABEL: Record<string, string> = {
  admin: 'ผู้ดูแลระบบ',
  staff: 'พนักงาน',
  viewer: 'ผู้ดูข้อมูล',
}

type QueueCounts = {
  purchase: number
  saleorder: number
  saleinvoice: number
  marketplaceAliases: number
}

function navBadgeCount(
  badgeKind: string | boolean | null | undefined,
  queueCounts: QueueCounts,
  unreadMessages: number,
  unreadNotifications: number,
) {
  const kind = badgeKind === true ? 'bills' : badgeKind || null
  if (kind === 'messages') return unreadMessages
  if (kind === 'shopee_realtime') return unreadNotifications
  if (kind === 'purchase') return queueCounts.purchase
  if (kind === 'saleorder') return queueCounts.saleorder
  if (kind === 'saleinvoice') return queueCounts.saleinvoice
  if (kind === 'marketplace_aliases') return queueCounts.marketplaceAliases
  if (kind === 'bills') return queueCounts.purchase
  return 0
}

export default function Sidebar() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const toggle = useUIStore((s) => s.toggleSidebar)
  const mobileOpen = useUIStore((s) => s.mobileNavOpen)
  const setMobileOpen = useUIStore((s) => s.setMobileNavOpen)
  const unreadNotifications = useNotificationsStore((s) => s.unread)
  const [queueCounts, setQueueCounts] = useState({ purchase: 0, saleorder: 0, saleinvoice: 0, marketplaceAliases: 0 })
  const [unreadMessages, setUnreadMessages] = useState(0)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Document queue counts + unread messages. SSE pushes unread changes
  // (UnreadChanged event) so the badge updates instantly when admin opens
  // a thread or a customer messages in. The 60s poll exists as a safety
  // net to refresh pending count (which has no SSE source) and to recover
  // if the SSE stream silently drops.
  const fetchStats = useCallback(async () => {
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
      return
    }
    try {
      const [stats, purchase, saleorder, saleinvoice, marketplaceAliases] = await Promise.all([
        client.get<{ unread_messages?: number }>('/api/dashboard/stats'),
        countDocumentQueue({ source: 'shopee_shipped', bill_type: 'purchase' }),
        countDocumentQueue({ bill_type: 'sale', document_route: 'saleorder' }),
        countDocumentQueue({ bill_type: 'sale', document_route: 'saleinvoice' }),
        countMarketplaceAliasQueue(),
      ])
      setQueueCounts({ purchase, saleorder, saleinvoice, marketplaceAliases })
      setUnreadMessages(stats.data.unread_messages ?? 0)
    } catch {
      /* silent */
    }
  }, [])

  useEffect(() => {
    fetchStats()
    intervalRef.current = setInterval(fetchStats, 60_000)

    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        fetchStats()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [fetchStats])

  useEffect(() => {
    const onWorkQueueChanged = () => {
      void fetchStats()
    }
    window.addEventListener(WORK_QUEUE_CHANGED_EVENT, onWorkQueueChanged)
    return () => window.removeEventListener(WORK_QUEUE_CHANGED_EVENT, onWorkQueueChanged)
  }, [fetchStats])

  // SSE — instant unread badge updates. Server publishes UnreadChanged on
  // mark-read + on every inbound webhook.
  useChatEvents({
    onUnreadChanged: useCallback((p: { total: number }) => {
      setUnreadMessages(p.total ?? 0)
    }, []),
  })

  // Hotkey [ to toggle sidebar
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLElement) {
        const tag = e.target.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || e.target.isContentEditable)
          return
      }
      if (e.key === '[' && !e.metaKey && !e.ctrlKey && !e.altKey) {
        e.preventDefault()
        toggle()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [toggle])

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

  const initials =
    user?.name
      ? user.name
          .split(' ')
          .map((w) => w[0])
          .join('')
          .slice(0, 2)
          .toUpperCase()
      : '?'

  const navCollapsed = collapsed
  const sidebarWidth = navCollapsed ? 'w-14' : 'w-60'
  const navGroups = visibleNavGroups(user?.role)

  return (
    <TooltipProvider delayDuration={0}>
      <MobileNavDrawer
        open={mobileOpen}
        onOpenChange={setMobileOpen}
        navGroups={navGroups}
        queueCounts={queueCounts}
        unreadMessages={unreadMessages}
        unreadNotifications={unreadNotifications}
        userEmail={user?.email}
        userRole={user?.role}
        userInitials={initials}
        onLogout={handleLogout}
      />
      <aside
        className={cn(
          'hidden shrink-0 flex-col border-r border-border bg-sidebar text-sidebar-foreground transition-[width] duration-150 md:flex',
          sidebarWidth,
        )}
      >
        {/* Logo */}
        <div className={cn('flex h-14 items-center gap-2 px-3', navCollapsed && 'justify-center px-0')}>
          <NexflowLogo />
          {!navCollapsed && (
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold leading-tight">Nexflow</div>
              <div className="truncate text-[10px] text-sidebar-foreground/55">
                Operations Console
              </div>
            </div>
          )}
        </div>

        <Separator />

        {/* Nav */}
        <nav className="flex-1 overflow-y-auto p-2">
          {navGroups.map((group, gi) => (
            <div key={group.label} className={cn('flex flex-col gap-0.5', gi > 0 && 'mt-4')}>
              {!navCollapsed && (
                <div className="px-2 pb-1 pt-2 text-[11px] font-medium text-sidebar-foreground/50">
                  {group.label}
                </div>
              )}
              {navCollapsed && gi > 0 && <Separator className="my-2" />}

              {group.items.map((item) => {
                const Icon = item.icon
                const badgeKind =
                  item.hasBadge === true ? 'bills' : item.hasBadge || null
                const badgeCount = navBadgeCount(badgeKind, queueCounts, unreadMessages, unreadNotifications)
                const showBadge = !!badgeKind && badgeCount > 0
                const urgentBadge = !!badgeKind && URGENT_BADGES.has(badgeKind)

                const linkInner = (active: boolean) => (
                  <span
                    // title= shows the English/setup-name hint as a native
                    // browser tooltip even in expanded mode. Cheap way to
                    // give devs the original feature name without adding
                    // a separate (?) icon to every nav item.
                    title={item.hint ? `${item.label} — ${item.hint}` : item.label}
                    className={cn(
                      'group relative flex h-8 items-center gap-2.5 rounded-md px-2 text-sm transition-colors',
                      active
                        ? 'bg-primary text-primary-foreground font-semibold shadow-sm'
                        : 'text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-foreground',
                      navCollapsed && 'justify-center px-0',
                    )}
                  >
                    {active && !navCollapsed && (
                      <span className="absolute inset-y-1 left-0 w-0.5 rounded-r-full bg-[hsl(var(--automation))]" />
                    )}
                    <span className="relative">
                      <Icon className="h-[15px] w-[15px] shrink-0" strokeWidth={2} />
                      {showBadge && navCollapsed && (
                        <span
                          className={cn(
                            'absolute -right-0.5 -top-0.5 h-1.5 w-1.5 rounded-full',
                            urgentBadge ? 'bg-destructive' : 'bg-warning',
                          )}
                        />
                      )}
                    </span>
                    {!navCollapsed && (
                      <>
                        <span className="flex-1 truncate">{item.label}</span>
                        {showBadge && (
                          <Badge
                            variant={urgentBadge ? 'destructive' : 'secondary'}
                            className="h-5 min-w-[20px] justify-center px-1.5 text-[10px]"
                          >
                            {badgeCount > 99 ? '99+' : badgeCount}
                          </Badge>
                        )}
                      </>
                    )}
                  </span>
                )

                const link = (
                  <NavLink key={item.to} to={item.to} end={item.end}>
                    {({ isActive }) => linkInner(isActive)}
                  </NavLink>
                )

                if (!navCollapsed) return link
                return (
                  <Tooltip key={item.to}>
                    <TooltipTrigger asChild>{link}</TooltipTrigger>
                    <TooltipContent side="right" className="text-xs">
                      <div className="font-medium">{item.label}</div>
                      {item.hint && (
                        <div className="mt-0.5 text-[10px] text-muted-foreground">
                          {item.hint}
                        </div>
                      )}
                    </TooltipContent>
                  </Tooltip>
                )
              })}
            </div>
          ))}
        </nav>

        <Separator />

        {/* Real-time connection state indicator. Reads from the shared
            events-store; tooltip explains what each state means. Hidden
            when sidebar collapsed — the dot still shows so admins notice
            'reconnecting' / 'offline'. */}
        {ENABLE_CHAT && PHASE >= 2 && (
          <div className={cn('px-2 py-1.5', navCollapsed ? 'flex justify-center' : '')}>
            <ConnectionDot collapsed={navCollapsed} />
          </div>
        )}

        {/* Collapse toggle */}
        <div className="px-2 py-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={toggle}
            className={cn('h-8 w-full justify-start gap-2 px-2 text-xs text-sidebar-foreground/62 hover:bg-sidebar-accent hover:text-sidebar-foreground', navCollapsed && 'justify-center px-0')}
            aria-label={navCollapsed ? 'ขยาย sidebar' : 'ยุบ sidebar'}
          >
            {navCollapsed ? <ChevronsRight className="h-4 w-4" /> : <ChevronsLeft className="h-4 w-4" />}
            {!navCollapsed && <span>ยุบเมนู</span>}
          </Button>
        </div>

        {/* User block */}
        <div className="border-t border-sidebar-border p-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className={cn(
                  'flex w-full items-center gap-2 rounded-md border border-transparent p-1.5 text-left text-sidebar-foreground/75 transition-colors hover:border-sidebar-border hover:bg-sidebar-accent hover:text-sidebar-foreground data-[state=open]:border-sidebar-border data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-foreground',
                  !navCollapsed && 'bg-sidebar-accent',
                  navCollapsed && 'justify-center',
                )}
                aria-label="เมนูผู้ใช้"
              >
                <Avatar className="h-7 w-7">
                  <AvatarFallback className="bg-primary text-primary-foreground text-[11px]">
                    {initials}
                  </AvatarFallback>
                </Avatar>
                {!navCollapsed && (
                  <div className="min-w-0 flex-1 leading-tight">
                    <div className="truncate text-xs font-medium">
                      {user?.name || user?.email}
                    </div>
                    <div className="truncate text-[10px] text-sidebar-foreground/55">
                      {ROLE_LABEL[user?.role ?? ''] ?? user?.role}
                    </div>
                  </div>
                )}
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="start"
              side="right"
              className="min-w-[220px] border-sidebar-border bg-sidebar text-sidebar-foreground shadow-xl"
            >
              <DropdownMenuLabel className="text-xs font-normal text-sidebar-foreground/60">
                {user?.email}
              </DropdownMenuLabel>
              <DropdownMenuSeparator className="bg-sidebar-border" />
              <ThemeToggle variant="menu-item" tone="sidebar" />
              <DropdownMenuSeparator className="bg-sidebar-border" />
              <DropdownMenuItem
                onClick={handleLogout}
                className="gap-2 text-destructive focus:bg-destructive/10 focus:text-destructive"
              >
                <LogOut className="h-3.5 w-3.5" />
                ออกจากระบบ
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </aside>
    </TooltipProvider>
  )
}

function MobileNavDrawer({
  open,
  onOpenChange,
  navGroups,
  queueCounts,
  unreadMessages,
  unreadNotifications,
  userEmail,
  userRole,
  userInitials,
  onLogout,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  navGroups: NavGroup[]
  queueCounts: QueueCounts
  unreadMessages: number
  unreadNotifications: number
  userEmail?: string
  userRole?: string
  userInitials: string
  onLogout: () => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="left" className="flex w-[min(92vw,380px)] flex-col gap-0 bg-sidebar p-0 text-sidebar-foreground sm:max-w-sm">
        <SheetHeader className="border-b border-sidebar-border px-4 py-4 text-left">
          <div className="flex items-center gap-3 pr-8">
            <NexflowLogo className="h-10 w-10" />
            <div className="min-w-0">
              <SheetTitle className="text-sidebar-foreground">Nexflow</SheetTitle>
              <SheetDescription className="text-sidebar-foreground/60">
                Operations Console
              </SheetDescription>
            </div>
          </div>
        </SheetHeader>

        <nav className="flex-1 overflow-y-auto px-3 py-4">
          {navGroups.map((group) => (
            <div key={group.label} className="mb-5 last:mb-0">
              <div className="px-2 pb-2 text-[12px] font-semibold text-sidebar-foreground/55">
                {group.label}
              </div>
              <div className="space-y-1">
                {group.items.map((item) => {
                  const Icon = item.icon
                  const badgeKind = item.hasBadge === true ? 'bills' : item.hasBadge || null
                  const badgeCount = navBadgeCount(badgeKind, queueCounts, unreadMessages, unreadNotifications)
                  const urgentBadge = !!badgeKind && URGENT_BADGES.has(badgeKind)
                  return (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      end={item.end}
                      onClick={() => onOpenChange(false)}
                      className={({ isActive }) =>
                        cn(
                          'flex min-h-11 items-center gap-3 rounded-md px-3 text-sm font-medium transition-colors',
                          isActive
                            ? 'bg-primary text-primary-foreground shadow-sm'
                            : 'text-sidebar-foreground/78 hover:bg-sidebar-accent hover:text-sidebar-foreground',
                        )
                      }
                    >
                      <Icon className="h-4 w-4 shrink-0" strokeWidth={2.2} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{item.label}</span>
                        {item.hint && (
                          <span className="mt-0.5 block truncate text-[11px] font-normal opacity-70">
                            {item.hint}
                          </span>
                        )}
                      </span>
                      {badgeCount > 0 && (
                        <Badge
                          variant={urgentBadge ? 'destructive' : 'secondary'}
                          className="h-5 min-w-[22px] justify-center px-1.5 text-[10px]"
                        >
                          {badgeCount > 99 ? '99+' : badgeCount}
                        </Badge>
                      )}
                    </NavLink>
                  )
                })}
              </div>
            </div>
          ))}
        </nav>

        <div className="border-t border-sidebar-border p-3">
          <div className="mb-2 flex items-center gap-2 rounded-md bg-sidebar-accent px-2 py-2">
            <Avatar className="h-8 w-8">
              <AvatarFallback className="bg-primary text-primary-foreground text-[11px]">
                {userInitials}
              </AvatarFallback>
            </Avatar>
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-medium">{userEmail}</div>
              <div className="truncate text-[10px] text-sidebar-foreground/55">
                {ROLE_LABEL[userRole ?? ''] ?? userRole}
              </div>
            </div>
          </div>
          <div className="space-y-2">
            <ThemeToggle variant="menu-item" className="rounded-md bg-card text-card-foreground" />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onLogout}
              className="w-full justify-start gap-2 text-destructive hover:bg-destructive/10 hover:text-destructive"
            >
              <LogOut className="h-3.5 w-3.5" />
              ออกจากระบบ
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

// ConnectionDot renders the live SSE connection status as a small colored
// dot ± label. Reading from the events-store keeps state in one place;
// every page that uses Layout (i.e. all authenticated routes) sees the
// same indicator.
const STATE_META: Record<EventsConnectionState, { label: string; cls: string; tip: string }> = {
  connecting: {
    label: 'กำลังเชื่อมต่อ…',
    cls: 'bg-muted-foreground/40',
    tip: 'กำลังเปิดการเชื่อมต่อ real-time',
  },
  live: {
    label: 'เชื่อมต่อแล้ว',
    cls: 'bg-success',
    tip: 'รับข้อความ real-time แล้ว — ไม่ต้องรีเฟรช',
  },
  reconnecting: {
    label: 'กำลังเชื่อมต่อใหม่',
    cls: 'bg-warning',
    tip: 'การเชื่อมต่อหลุด — ระบบกำลังลองใหม่ (ระหว่างนี้จะใช้ polling สำรอง)',
  },
  offline: {
    label: 'ออฟไลน์',
    cls: 'bg-destructive',
    tip: 'ขาดการเชื่อมต่อ real-time — ใช้ polling สำรอง (อัปเดตทุก 60 วินาที)',
  },
}

function ConnectionDot({ collapsed }: { collapsed: boolean }) {
  const status = useEventsStore((s) => s.status)
  const meta = STATE_META[status]
  const dot = (
    <span
      className={cn(
        'inline-block h-2 w-2 shrink-0 rounded-full',
        meta.cls,
        status === 'connecting' || status === 'reconnecting' ? 'animate-pulse' : '',
      )}
    />
  )
  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="cursor-help">{dot}</span>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs">
          {meta.tip}
        </TooltipContent>
      </Tooltip>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex w-full cursor-help items-center gap-1.5 px-2 text-[10px] uppercase tracking-wider text-muted-foreground">
          {dot}
          <span>{meta.label}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="right" className="text-xs">
        {meta.tip}
      </TooltipContent>
    </Tooltip>
  )
}
