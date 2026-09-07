import { useEffect, useMemo, useState, type ReactNode } from 'react'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ClipboardCopy,
  Clock3,
  Download,
  Loader2,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Skeleton } from '@/components/ui/skeleton'
import {
  ACTION_META,
  TONE_DOT,
  type AuditLog,
  summarize,
} from '@/lib/audit-log-meta'
import {
  groupSMLTimelineEvents,
  type BillTimelineItem,
  type SMLAttemptTimelineItem,
} from '@/lib/sml-timeline'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/store/auth'
import type { ShopeeOrderEvent } from '@/types'
import { stockJobSummary } from '../utils/presentation'

interface Props {
  billId: string
  shopeeEvents?: ShopeeOrderEvent[]
  stockJobStatus?: string
}

interface SMLExchangeEvidence {
  id: string
  sequence: number
  trace_id?: string
  status: 'started' | 'succeeded' | 'failed' | 'unknown'
  started_at: string
  finished_at?: string
  duration_ms?: number
  request: {
    method: string
    canonical_path: string
    content_type?: string
    correlation_id?: string
  }
  response_status?: number
  response_headers?: Record<string, string>
  response_json?: unknown
  response_hash?: string
  response_size: number
  response_truncated: boolean
  error_code?: string
  error_class?: string
  safe_error_summary?: string
}

interface SMLDiagnosticPackage {
  diagnostic_package_version: 1
  generated_at: string
  bill_id: string
  sml_summary: {
    attempt_id?: string
    document_number?: string
    route?: string
    core_status?: string
    profile_status?: string
    stock_status?: string
    resolution_status?: string
    failure_count: number
    can_retry: boolean
  }
  request_payload?: unknown
  exchanges: SMLExchangeEvidence[]
}

interface SupportPackage {
  diagnostic_package_version: 1
  generated_at: string
  bill_id: string
  attempt: {
    attempt_id: string
    summary: string
    resolution_status: string
    failure_count: number
    can_retry: boolean
  }
  timeline: AuditLog[]
  http_diagnostics?: SMLDiagnosticPackage
  note?: string
}

// The timeline keeps the original event count for auditability while presenting
// unambiguous SML retries as one expandable attempt. HTTP evidence is lazy-loaded
// only for admins, so the initial bill view never waits for a diagnostics query.
export function BillTimeline({ billId, shopeeEvents = [], stockJobStatus }: Props) {
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
  const groupedAuditEvents = useMemo(
    () => groupSMLTimelineEvents(auditEvents),
    [auditEvents],
  )
  const sourceEvents = [...shopeeEvents].sort((a, b) => {
    const aTime = dayjs(a.email_date || a.created_at).valueOf()
    const bTime = dayjs(b.email_date || b.created_at).valueOf()
    return bTime - aTime
  })
  const visibleCount = sourceEvents.length + auditEvents.length
  const stockSummary = stockJobSummary(stockJobStatus)

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
        {stockSummary && (
          <div className="mb-4 rounded-md border border-border bg-muted/30 p-3 text-xs" role="status">
            <p className="font-medium">{stockSummary}</p>
            <p className="mt-1 text-muted-foreground">สถานะล่าสุดจากงานสต๊อกของบิลนี้ แสดงแยกจากเหตุการณ์ย้อนหลัง ไม่ใช่การส่งบิลซ้ำ</p>
          </div>
        )}
        {loading ? (
          <div className="space-y-2" aria-label="กำลังโหลดประวัติบิล">
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
                <AuditTimeline billId={billId} items={groupedAuditEvents} />
              </TimelineSection>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">ยังไม่มีประวัติของบิลนี้</p>
        )}
      </CardContent>
    </Card>
  )
}

function TimelineSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <div className="mb-2 text-xs font-medium text-foreground">{title}</div>
      {children}
    </section>
  )
}

function AuditTimeline({ billId, items }: { billId: string; items: BillTimelineItem[] }) {
  return (
    <ol className="relative space-y-3">
      <span aria-hidden className="absolute left-[5px] top-1.5 h-[calc(100%-12px)] w-px bg-border" />
      {items.map((item, index) => item.kind === 'sml_attempt' ? (
        <SMLAttemptEvent key={`attempt-${item.attempt_id}`} billId={billId} item={item} />
      ) : (
        <Event key={item.event.id} event={item.event} isLast={index === items.length - 1} />
      ))}
    </ol>
  )
}

function SMLAttemptEvent({ billId, item }: { billId: string; item: SMLAttemptTimelineItem }) {
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const [open, setOpen] = useState(false)
  const [diagnostics, setDiagnostics] = useState<SMLDiagnosticPackage | null>(null)
  const [diagnosticsLoaded, setDiagnosticsLoaded] = useState(false)
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false)
  const succeeded = item.resolution_status === 'resolved' || item.resolution_status === 'succeeded'
  const unknown = item.resolution_status === 'unknown'
  const orderedEvents = [...item.events].sort(
    (left, right) => dayjs(left.created_at).valueOf() - dayjs(right.created_at).valueOf(),
  )
  const firstTime = dayjs(orderedEvents[0]?.created_at)
  const lastTime = dayjs(orderedEvents[orderedEvents.length - 1]?.created_at)
  const route = firstText(orderedEvents, (event) => event.detail?.route)
  const documentNumber = firstText(orderedEvents, (event) =>
    event.detail?.doc_no || event.detail?.doc_no_attempted,
  )

  async function loadDiagnostics(): Promise<SMLDiagnosticPackage | null> {
    if (!isAdmin || diagnosticsLoaded) return diagnostics
    setDiagnosticsLoading(true)
    try {
      const response = await client.get<SMLDiagnosticPackage>(`/api/bills/${billId}/sml-diagnostics`)
      const current = response.data.sml_summary.attempt_id === item.attempt_id ? response.data : null
      setDiagnostics(current)
      setDiagnosticsLoaded(true)
      return current
    } catch {
      setDiagnosticsLoaded(true)
      return null
    } finally {
      setDiagnosticsLoading(false)
    }
  }

  async function copyForSupport() {
    const httpDiagnostics = isAdmin ? await loadDiagnostics() : null
    const payload: SupportPackage = {
      diagnostic_package_version: 1,
      generated_at: new Date().toISOString(),
      bill_id: billId,
      attempt: {
        attempt_id: item.attempt_id,
        summary: item.summary,
        resolution_status: item.resolution_status,
        failure_count: item.failure_count,
        can_retry: item.can_retry,
      },
      timeline: orderedEvents,
      ...(httpDiagnostics ? { http_diagnostics: httpDiagnostics } : {}),
      ...(!httpDiagnostics ? {
        note: isAdmin
          ? 'ไม่มี HTTP exchange history สำหรับ attempt นี้'
          : 'ชุดข้อมูลสำหรับ Staff ไม่มี raw payload, response หรือข้อมูลรับรองระบบ',
      } : {}),
    }
    const body = JSON.stringify(payload, null, 2)
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(body)
      toast.success('คัดลอกข้อมูลสำหรับฝ่ายเทคนิคแล้ว')
    } catch {
      downloadJSON(body, `nexflow-sml-${documentNumber || item.attempt_id}.json`)
      toast.info('เบราว์เซอร์ไม่อนุญาตให้คัดลอก จึงดาวน์โหลดไฟล์ให้แทน')
    }
  }

  function handleOpenChange(next: boolean) {
    setOpen(next)
    if (next && isAdmin && !diagnosticsLoaded && !diagnosticsLoading) void loadDiagnostics()
  }

  return (
    <li className="relative flex gap-3 pl-0">
      <span className={cn(
        'relative z-10 mt-2 inline-block h-2.5 w-2.5 shrink-0 rounded-full ring-[3px] ring-card',
        succeeded ? 'bg-success' : unknown ? 'bg-warning' : 'bg-destructive',
      )} />
      <Collapsible open={open} onOpenChange={handleOpenChange} className="min-w-0 flex-1">
        <div className="rounded-lg border border-border/70 bg-card">
          <CollapsibleTrigger asChild>
            <button
              type="button"
              className="flex w-full min-w-0 items-start gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 motion-reduce:transition-none"
              aria-label={`${item.summary} เปิดดู ${orderedEvents.length} เหตุการณ์ย่อย`}
            >
              <span className={cn(
                'mt-0.5 shrink-0',
                succeeded ? 'text-success' : unknown ? 'text-warning' : 'text-destructive',
              )}>
                {succeeded ? <CheckCircle2 className="h-4 w-4" /> : unknown ? <Clock3 className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
              </span>
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="text-sm font-semibold text-foreground">{item.summary}</span>
                  {item.resolution_status === 'resolved' && (
                    <Badge variant="outline" className="border-success/30 bg-success/10 text-success">แก้ไขแล้ว</Badge>
                  )}
                  {unknown && (
                    <Badge variant="outline" className="border-warning/30 bg-warning/10 text-warning">ต้องตรวจสอบ</Badge>
                  )}
                </span>
                <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
                  {documentNumber && <span className="font-mono">{documentNumber}</span>}
                  {route && <span>{friendlyRoute(route)}</span>}
                  <span>{orderedEvents.length} เหตุการณ์ย่อย</span>
                  {firstTime.isValid() && lastTime.isValid() && (
                    <span title={`${firstTime.format('YYYY-MM-DD HH:mm:ss')} – ${lastTime.format('YYYY-MM-DD HH:mm:ss')}`}>
                      {lastTime.format('DD/MM/YY HH:mm')}
                    </span>
                  )}
                </span>
              </span>
              <ChevronDown className={cn(
                'mt-0.5 h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200 motion-reduce:transition-none',
                open && 'rotate-180',
              )} aria-hidden />
            </button>
          </CollapsibleTrigger>

          <CollapsibleContent className="border-t border-border/70 px-3 pb-3 pt-3 data-[state=open]:animate-collapsible-down data-[state=closed]:animate-collapsible-up motion-reduce:animate-none">
            <AttemptFacts attemptID={item.attempt_id} route={route} documentNumber={documentNumber} events={orderedEvents} />
            <div className="mt-4">
              <h4 className="text-xs font-semibold text-foreground">ลำดับการทำงาน</h4>
              <ol className="mt-2 space-y-2" aria-label="ลำดับเหตุการณ์ของการส่ง SML ครั้งนี้">
                {orderedEvents.map((event, index) => (
                  <AttemptChildEvent key={event.id} event={event} index={index + 1} resolved={item.resolution_status === 'resolved'} />
                ))}
              </ol>
            </div>

            <div className="mt-4 rounded-md border border-border/70 bg-muted/30 p-3">
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="text-xs font-medium text-foreground">ข้อมูลสำหรับฝ่ายเทคนิค</p>
                  {diagnosticsLoading ? (
                    <p className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                      <Loader2 className="h-3 w-3 animate-spin motion-reduce:animate-none" />
                      กำลังตรวจหลักฐาน HTTP ที่บันทึกไว้
                    </p>
                  ) : isAdmin && diagnosticsLoaded && diagnostics?.exchanges.length ? (
                    <p className="mt-1 text-[11px] text-muted-foreground">
                      พบหลักฐาน HTTP {diagnostics.exchanges.length} ครั้ง · ไม่มี credential ในข้อมูลที่แสดง
                    </p>
                  ) : isAdmin && diagnosticsLoaded ? (
                    <p className="mt-1 text-[11px] text-muted-foreground">
                      เหตุการณ์นี้เกิดก่อนระบบบันทึก HTTP exchange จึงสร้างข้อมูลย้อนหลังไม่ได้
                    </p>
                  ) : (
                    <p className="mt-1 text-[11px] text-muted-foreground">
                      ชุดข้อมูลสำหรับพนักงานไม่มี raw payload, response หรือข้อมูลส่วนตัวของผู้ซื้อ
                    </p>
                  )}
                </div>
                <Button type="button" variant="outline" size="sm" className="h-8 shrink-0" onClick={() => void copyForSupport()} disabled={diagnosticsLoading}>
                  {typeof navigator !== 'undefined' && navigator.clipboard ? <ClipboardCopy className="h-3.5 w-3.5" /> : <Download className="h-3.5 w-3.5" />}
                  คัดลอกข้อมูลสำหรับฝ่ายเทคนิค
                </Button>
              </div>
              {isAdmin && diagnostics?.exchanges.length ? <ExchangeHistory exchanges={diagnostics.exchanges} /> : null}
            </div>
          </CollapsibleContent>
        </div>
      </Collapsible>
    </li>
  )
}

function AttemptFacts({ attemptID, route, documentNumber, events }: { attemptID: string; route: string; documentNumber: string; events: AuditLog[] }) {
  const traceID = firstText(events, (event) => event.trace_id)
  const duration = events.reduce((total, event) => total + (event.duration_ms ?? 0), 0)
  return (
    <dl className="grid grid-cols-1 gap-x-4 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-4">
      {documentNumber && <Fact label="เลขเอกสาร SML" value={documentNumber} mono />}
      {route && <Fact label="เส้นทาง" value={friendlyRoute(route)} />}
      <Fact label="Attempt ID" value={attemptID} mono />
      {traceID && <Fact label="Trace ID" value={traceID} mono />}
      {duration > 0 && <Fact label="เวลาที่ใช้รวมในระบบ" value={`${duration.toLocaleString()} ms`} />}
    </dl>
  )
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-[10px] text-muted-foreground">{label}</dt>
      <dd className={cn('mt-0.5 break-all text-xs font-medium text-foreground', mono && 'font-mono')}>{value}</dd>
    </div>
  )
}

function AttemptChildEvent({ event, index, resolved }: { event: AuditLog; index: number; resolved: boolean }) {
  const meta = ACTION_META[event.action] ?? { label: event.action, emoji: '•', tone: 'muted' as const }
  const transientFailure = resolved && event.action === 'sml_failed'
  const time = dayjs(event.created_at)
  const summary = summarize(event)
  const cause = errorCause(event)
  const impact = eventImpact(event, resolved)

  return (
    <li className="rounded-md border border-border/60 px-2.5 py-2 text-xs">
      <div className="flex flex-wrap items-start gap-x-2 gap-y-1">
        <span className="mt-0.5 w-5 shrink-0 text-center text-[10px] tabular-nums text-muted-foreground" aria-label={`ลำดับที่ ${index}`}>{index}</span>
        <span className="text-sm leading-none" aria-hidden>{meta.emoji}</span>
        <span className={cn(
          'font-medium',
          transientFailure ? 'text-warning' : event.level === 'error' ? 'text-destructive' : 'text-foreground',
        )}>
          {transientFailure ? 'ส่ง SML ล้มเหลวชั่วคราว' : meta.label}
        </span>
        {event.duration_ms != null && <span className="text-[10px] tabular-nums text-muted-foreground">{event.duration_ms} ms</span>}
        <time className="ml-auto text-[10px] tabular-nums text-muted-foreground" dateTime={event.created_at} title={time.format('YYYY-MM-DD HH:mm:ss')}>
          {time.format('DD/MM/YY HH:mm:ss')}
        </time>
      </div>
      <div className="ml-9 mt-1 space-y-1 text-[11px] leading-relaxed">
        {summary && <p className="text-muted-foreground">ผลลัพธ์: {summary}</p>}
        {cause && <p className="text-muted-foreground">สาเหตุ: {cause}</p>}
        {impact && <p className="text-muted-foreground">ผลกระทบ: {impact}</p>}
        <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[10px] text-muted-foreground">
          {event.trace_id && <span className="font-mono">Trace: {event.trace_id}</span>}
          {event.exchange_id && <span className="font-mono">Exchange: {event.exchange_id}</span>}
        </div>
      </div>
    </li>
  )
}

function ExchangeHistory({ exchanges }: { exchanges: SMLExchangeEvidence[] }) {
  return (
    <div className="mt-3 border-t border-border/60 pt-3">
      <p className="text-[11px] font-medium text-foreground">หลักฐาน HTTP ที่บันทึกใหม่</p>
      <ol className="mt-2 space-y-2">
        {exchanges.map((exchange) => (
          <li key={exchange.id} className="rounded-md bg-background p-2 text-[10px] text-muted-foreground">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="font-medium text-foreground">ครั้งที่ {exchange.sequence}</span>
              <Badge variant="outline" className={cn(
                'px-1.5 py-0 text-[9px]',
                exchange.status === 'succeeded' && 'border-success/30 text-success',
                exchange.status === 'failed' && 'border-destructive/30 text-destructive',
                (exchange.status === 'unknown' || exchange.status === 'started') && 'border-warning/30 text-warning',
              )}>{exchangeStatusLabel(exchange.status)}</Badge>
              {exchange.response_status != null && <span>HTTP {exchange.response_status}</span>}
              {exchange.duration_ms != null && <span>{exchange.duration_ms} ms</span>}
            </div>
            <div className="mt-1 break-all font-mono">{exchange.request.method} {exchange.request.canonical_path}</div>
            {exchange.safe_error_summary && <div className="mt-1 font-sans text-warning">{exchange.safe_error_summary}</div>}
            {exchange.response_truncated && <div className="mt-1 font-sans">Response ถูกจำกัดขนาดเพื่อความปลอดภัย</div>}
          </li>
        ))}
      </ol>
    </div>
  )
}

function ShopeeTimeline({ events }: { events: ShopeeOrderEvent[] }) {
  return (
    <ol className="relative space-y-3">
      <span aria-hidden className="absolute left-[5px] top-1.5 h-[calc(100%-12px)] w-px bg-border" />
      {events.map((event) => <ShopeeEvent key={event.id} event={event} />)}
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
          <span className="text-sm font-medium text-foreground">{shopeeEventLabel(event)}</span>
          <span className="ml-auto text-[11px] tabular-nums text-muted-foreground" title={time.format('YYYY-MM-DD HH:mm:ss')}>
            {time.format('DD/MM/YY HH:mm')}
          </span>
        </div>
        {summary && <p className="mt-0.5 truncate text-xs text-muted-foreground" title={summary}>{summary}</p>}
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
  const meta = ACTION_META[event.action] ?? { label: event.action, emoji: '•', tone: 'muted' as const }
  const summary = summarize(event)
  const time = dayjs(event.created_at)
  const isError = event.level === 'error'
  return (
    <li className="relative flex gap-3 pl-0">
      <span className={cn('relative z-10 mt-1.5 inline-block h-2.5 w-2.5 shrink-0 rounded-full ring-[3px] ring-card', TONE_DOT[meta.tone])} />
      <div className="min-w-0 flex-1 pb-0.5">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="text-base leading-none" aria-hidden>{meta.emoji}</span>
          <span className={cn('text-sm font-medium', isError ? 'text-destructive' : 'text-foreground')}>{meta.label}</span>
          {event.duration_ms != null && <span className="text-[10px] tabular-nums text-muted-foreground">{event.duration_ms}ms</span>}
          <span className="ml-auto text-[11px] tabular-nums text-muted-foreground" title={time.format('YYYY-MM-DD HH:mm:ss')}>{time.format('HH:mm:ss')}</span>
        </div>
        {summary && <p className={cn('mt-0.5 truncate text-xs', isError ? 'text-destructive' : 'text-muted-foreground')} title={summary}>{summary}</p>}
      </div>
      {!isLast && <span aria-hidden className="hidden" />}
    </li>
  )
}

function firstText(events: AuditLog[], read: (event: AuditLog) => unknown): string {
  for (const event of events) {
    const value = read(event)
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function friendlyRoute(route: string): string {
  const normalized = route.toLowerCase()
  if (normalized.includes('sale-orders') || normalized === 'saleorder') return 'ใบสั่งขาย'
  if (normalized.includes('sale-invoices') || normalized === 'saleinvoice') return 'ขายสินค้าและบริการ'
  return route
}

function errorCause(event: AuditLog): string {
  const detail = event.detail ?? {}
  const value = detail.error_message || detail.error || detail.message || detail.error_code
  return typeof value === 'string' ? value.trim() : ''
}

function eventImpact(event: AuditLog, resolved: boolean): string {
  if (event.action === 'sml_failed' && resolved) return 'ระบบลองใหม่ด้วย Attempt เดิมและสำเร็จแล้ว ไม่ต้องส่งเอกสารซ้ำ'
  if (event.resolution_status === 'unknown') return 'ยังสรุปไม่ได้ว่า SML สร้างเอกสารหรือไม่ ต้องตรวจสอบก่อนดำเนินการต่อ'
  if (event.action === 'sml_erp_log_warning') return 'เอกสารหลักถูกสร้างแล้ว ระบบจะซ่อมเฉพาะข้อมูลประกอบโดยไม่ส่งบิลซ้ำ'
  if (event.action === 'sml_stock_recalc_failed') return 'เอกสารหลักไม่ถูกส่งซ้ำ ตรวจและลองเฉพาะงานคำนวณสต๊อก'
  return ''
}

function exchangeStatusLabel(status: SMLExchangeEvidence['status']): string {
  if (status === 'succeeded') return 'สำเร็จ'
  if (status === 'failed') return 'ล้มเหลว'
  if (status === 'started') return 'ผลไม่แน่นอน'
  return 'ต้องตรวจสอบ'
}

function downloadJSON(body: string, filename: string) {
  const blob = new Blob([body], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename.replace(/[^a-zA-Z0-9._-]/g, '-')
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}
