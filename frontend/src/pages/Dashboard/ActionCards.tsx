import { Link } from 'react-router-dom'
import {
  AlertOctagon,
  ArrowUpRight,
  FileText,
  Mail,
  MessageSquare,
  ShoppingBag,
  type LucideIcon,
} from 'lucide-react'

import { cn } from '@/lib/utils'
import { ENABLE_CHAT, ENABLE_SALES_ORDERS } from '@/lib/featureFlags'
import type { DashboardStats } from '@/types'

// VITE_PHASE mirrors the same constant in Sidebar.tsx.
// 1 = Phase 1 only, 99 = all features (default).
const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

interface Props {
  stats: DashboardStats | null
  loading: boolean
}

interface Action {
  label: string
  count: number
  hint: string
  icon: LucideIcon
  to: string
  // tone — drives the accent on the count + ring on hover
  tone: 'neutral' | 'urgent'
  // minPhase — hide this card when VITE_PHASE < minPhase
  minPhase?: number
  enabled?: boolean
}

// ActionCards is the "ต้อง action" row at the top of the Dashboard.
// Each card is a clickable shortcut to the page where that work item lives.
// The intent is "Inbox-zero": the admin opens Nexflow and immediately knows
// what's waiting for them today, with one click to dive in.
//
// Tone:
//   neutral — grey number, no accent (e.g. unread chat — frequent, not alarming)
//   urgent  — red accent + animate-pulse on the dot when count > 0
//             (used for failures: bill failed, email inbox down)
export function ActionCards({ stats, loading }: Props) {
  const purchasePending = stats?.purchase_pending ?? 0
  const purchaseNeedsReview = stats?.purchase_needs_review ?? 0
  const purchaseFailed = stats?.purchase_failed ?? 0
  const salesPending = stats?.sales_pending ?? 0
  const salesNeedsReview = stats?.sales_needs_review ?? 0
  const salesFailed = stats?.sales_failed ?? 0
  const unread = stats?.unread_messages ?? 0
  const emailErrors = stats?.email_inbox_errors ?? 0

  const actions: Action[] = [
    {
      label: 'ใบสั่งซื้อ',
      count: purchasePending + purchaseNeedsReview,
      hint: `${purchaseNeedsReview} ต้องตรวจ · ${purchasePending} สถานะพร้อมส่ง`,
      icon: FileText,
      to: purchaseNeedsReview > 0 ? '/bills?status=needs_review' : '/bills?status=pending',
      tone: 'neutral',
    },
    {
      label: 'ใบสั่งขาย',
      count: salesPending + salesNeedsReview,
      hint: `${salesNeedsReview} ต้องตรวจ · ${salesPending} สถานะพร้อมส่ง`,
      icon: ShoppingBag,
      to: salesNeedsReview > 0 ? '/sales-orders?status=needs_review' : '/sales-orders?status=pending',
      tone: salesNeedsReview > 0 ? 'urgent' : 'neutral',
      enabled: ENABLE_SALES_ORDERS,
    },
    {
      label: 'ส่ง SML ไม่สำเร็จ',
      count: purchaseFailed + salesFailed,
      hint: `${purchaseFailed} ซื้อ · ${salesFailed} ขาย`,
      icon: AlertOctagon,
      to: ENABLE_SALES_ORDERS && salesFailed > 0 ? '/sales-orders?status=failed' : '/bills?status=failed',
      tone: 'urgent',
    },
    {
      label: 'ข้อความใหม่',
      count: unread,
      hint: 'ห้องที่ยังไม่ได้อ่าน',
      icon: MessageSquare,
      to: '/messages',
      tone: 'neutral',
      minPhase: 2,
      enabled: ENABLE_CHAT,
    },
    {
      label: 'อีเมลต้องดู',
      count: emailErrors,
      hint: 'ดึงเมลได้ แต่สร้างบิลไม่ครบ',
      icon: Mail,
      to: '/settings/email',
      tone: 'urgent',
    },
  ]

  const visibleActions = actions.filter((a) => a.enabled !== false && (!a.minPhase || PHASE >= a.minPhase))

  return (
    <div className={cn(
      'grid gap-3',
      visibleActions.length === 3 ? 'grid-cols-1 sm:grid-cols-3' : 'grid-cols-2',
    )}>
      {visibleActions.map((a) => (
        <ActionCard key={a.label} {...a} loading={loading} />
      ))}
    </div>
  )
}

function ActionCard({
  label,
  count,
  hint,
  icon: Icon,
  to,
  tone,
  loading,
}: Action & { loading: boolean }) {
  const isUrgent = tone === 'urgent' && count > 0
  const isQuiet = count === 0

  return (
    <Link
      to={to}
      className={cn(
        'group relative flex min-h-[112px] flex-col gap-2 rounded-lg border bg-background/70 p-4 transition-all',
        'hover:-translate-y-0.5 hover:bg-card hover:shadow-sm',
        isUrgent
          ? 'border-destructive/30 hover:border-destructive/50'
          : 'border-border hover:border-foreground/15',
      )}
      aria-label={`${label} ${count}`}
    >
      <div className="flex items-center justify-between">
        <span
          className={cn(
            'flex h-7 w-7 items-center justify-center rounded-md',
            isUrgent
              ? 'bg-destructive/10 text-destructive'
              : isQuiet
                ? 'bg-muted text-muted-foreground/60'
                : 'bg-accent text-foreground',
          )}
        >
          <Icon className="h-3.5 w-3.5" strokeWidth={2.25} />
        </span>
        <ArrowUpRight
          className={cn(
            'h-3.5 w-3.5 shrink-0 text-muted-foreground/50 transition-all',
            'group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground',
          )}
        />
      </div>

      <div className="flex items-baseline gap-2">
        <span
          className={cn(
            'tabular-nums text-2xl font-semibold leading-none',
            loading
              ? 'text-muted-foreground/30'
              : isQuiet
                ? 'text-muted-foreground/60'
                : isUrgent
                  ? 'text-destructive'
                  : 'text-foreground',
          )}
        >
          {loading ? '—' : count.toLocaleString()}
        </span>
        {isUrgent && !loading && (
          <span
            aria-hidden
            className="h-1.5 w-1.5 animate-pulse rounded-full bg-destructive"
            title="ต้องดำเนินการ"
          />
        )}
      </div>

      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-foreground">{label}</p>
        <p className="truncate text-[11px] text-muted-foreground">{hint}</p>
      </div>
    </Link>
  )
}
