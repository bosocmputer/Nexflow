import { useEffect, useState, type ReactNode } from 'react'
import dayjs from 'dayjs'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import type { ShopeeOrderEvent } from '@/types'
import {
  ACTION_META,
  TONE_DOT,
  type AuditLog,
  summarize,
} from '@/lib/audit-log-meta'

interface Props {
  billId: string
  shopeeEvents?: ShopeeOrderEvent[]
}

// BillTimeline renders an activity feed of every audit_log row tied to one
// bill (target_id = bill.id). Replaces the cross-page "open /logs and grep"
// flow — admin can answer "ทำไมบิลนี้ถึงเป็นแบบนี้" without leaving the page.
//
// Layout: a vertical rail with one event per row. Each row shows
//   ● (toned dot) │  Time (HH:mm:ss + relative)
//                 │  📥 Action label
//                 │  Optional summary (italic muted)
//
// Reuses ACTION_META + summarize() so the timeline visuals match /logs
// exactly — no double-maintenance when new actions are added.
export function BillTimeline({ billId, shopeeEvents = [] }: Props) {
  const [events, setEvents] = useState<AuditLog[] | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    client
      .get<{ data: AuditLog[] | null }>(`/api/bills/${billId}/timeline`)
      .then((res) => {
        if (alive) setEvents(res.data.data ?? [])
      })
      .catch(() => {
        if (alive) setEvents([])
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [billId])

  const auditEvents = events ?? []
  const sourceEvents = [...shopeeEvents].sort((a, b) => {
    const aTime = dayjs(a.email_date || a.created_at).valueOf()
    const bTime = dayjs(b.email_date || b.created_at).valueOf()
    return bTime - aTime
  })
  const visibleCount = sourceEvents.length + auditEvents.length

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-semibold">
          ประวัติของบิลนี้
          {!loading && visibleCount > 0 && (
            <span className="ml-2 text-xs font-normal text-muted-foreground">
              ({visibleCount} เหตุการณ์)
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="pt-0">
        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-3/4" />
            <Skeleton className="h-8 w-2/3" />
          </div>
        ) : visibleCount > 0 ? (
          <div className="space-y-5">
            {sourceEvents.length > 0 && (
              <TimelineSection title="สถานะจาก Shopee">
                <ShopeeTimeline events={sourceEvents} />
              </TimelineSection>
            )}
            {auditEvents.length > 0 && (
              <TimelineSection title="ประวัติระบบ">
                <AuditTimeline events={auditEvents} />
              </TimelineSection>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            ยังไม่มีประวัติของบิลนี้
          </p>
        )}
      </CardContent>
    </Card>
  )
}

function TimelineSection({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section>
      <div className="mb-2 text-xs font-medium text-foreground">{title}</div>
      {children}
    </section>
  )
}

function AuditTimeline({ events }: { events: AuditLog[] }) {
  return (
    <ol className="relative space-y-3">
      {/* Vertical rail behind every dot — the events position dots on top */}
      <span
        aria-hidden
        className="absolute left-[5px] top-1.5 h-[calc(100%-12px)] w-px bg-border"
      />
      {events.map((ev, idx) => (
        <Event key={ev.id} event={ev} isLast={idx === events.length - 1} />
      ))}
    </ol>
  )
}

function ShopeeTimeline({ events }: { events: ShopeeOrderEvent[] }) {
  return (
    <ol className="relative space-y-3">
      <span
        aria-hidden
        className="absolute left-[5px] top-1.5 h-[calc(100%-12px)] w-px bg-border"
      />
      {events.map((ev) => (
        <ShopeeEvent key={ev.id} event={ev} />
      ))}
    </ol>
  )
}

function ShopeeEvent({ event }: { event: ShopeeOrderEvent }) {
  const time = dayjs(event.email_date || event.created_at)
  const subject = event.subject?.trim()
  const from = event.from_addr?.trim()
  const summary = [subject, from ? `จาก ${from}` : ''].filter(Boolean).join(' · ')

  return (
    <li className="relative flex gap-3 pl-0">
      <span className="relative z-10 mt-1.5 inline-block h-2.5 w-2.5 shrink-0 rounded-full bg-info ring-[3px] ring-card" />
      <div className="min-w-0 flex-1 pb-0.5">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="text-sm font-medium text-foreground">
            {shopeeEventLabel(event)}
          </span>
          <span
            className="ml-auto text-[11px] tabular-nums text-muted-foreground"
            title={time.format('YYYY-MM-DD HH:mm:ss')}
          >
            {time.format('DD/MM/YY HH:mm')}
          </span>
        </div>
        {summary && (
          <p
            className="mt-0.5 truncate text-xs text-muted-foreground"
            title={summary}
          >
            {summary}
          </p>
        )}
      </div>
    </li>
  )
}

export function shopeeEventLabel(event: ShopeeOrderEvent): string {
  if (event.event_type === 'payment_confirmed') return 'ยืนยันการชำระเงินแล้ว'
  if (event.event_type === 'shipped') return 'ถูกจัดส่งแล้ว'
  return event.status_label || event.event_type || 'สถานะ Shopee'
}

function Event({ event, isLast }: { event: AuditLog; isLast: boolean }) {
  const meta = ACTION_META[event.action] ?? {
    label: event.action,
    emoji: '•',
    tone: 'muted' as const,
  }
  const summary = summarize(event)
  const time = dayjs(event.created_at)
  const isError = event.level === 'error'

  return (
    <li className="relative flex gap-3 pl-0">
      {/* Tone-colored dot. Larger ring so it sits cleanly over the rail. */}
      <span
        className={cn(
          'relative z-10 mt-1.5 inline-block h-2.5 w-2.5 shrink-0 rounded-full ring-[3px] ring-card',
          TONE_DOT[meta.tone],
        )}
      />
      <div className="min-w-0 flex-1 pb-0.5">
        {/* Header line: action label + time */}
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="text-base leading-none">{meta.emoji}</span>
          <span
            className={cn(
              'text-sm font-medium',
              isError ? 'text-destructive' : 'text-foreground',
            )}
          >
            {meta.label}
          </span>
          {event.duration_ms != null && (
            <span className="text-[10px] tabular-nums text-muted-foreground">
              {event.duration_ms}ms
            </span>
          )}
          <span
            className="ml-auto text-[11px] tabular-nums text-muted-foreground"
            title={time.format('YYYY-MM-DD HH:mm:ss')}
          >
            {time.format('HH:mm:ss')}
          </span>
        </div>
        {/* Summary line — only when summarize() returned something useful */}
        {summary && (
          <p
            className={cn(
              'mt-0.5 truncate text-xs',
              isError ? 'text-destructive' : 'text-muted-foreground',
            )}
            title={summary}
          >
            {summary}
          </p>
        )}
      </div>
      {/* Suppress unused isLast warning — reserved for future "join line above" tweaks */}
      {!isLast && <span aria-hidden className="hidden" />}
    </li>
  )
}
