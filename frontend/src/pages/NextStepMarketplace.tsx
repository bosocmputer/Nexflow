import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, RefreshCw, Search, Store } from 'lucide-react'

import client from '@/api/client'
import { DateRangePicker } from '@/components/common/DateRangePicker'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { type NotificationUnreadBySource, useNotificationsStore } from '@/lib/notifications-store'
import { cn } from '@/lib/utils'
import type { NextStepMarketplaceOrder, NextStepMarketplaceState, NextStepMarketplaceSummary } from '@/types'

type DateRange = {
  from: string
  to: string
}

type NotificationWriteResponse = {
  unread: number
  unread_by_source?: NotificationUnreadBySource
}

const PAGE_SIZE = 20

type NextStepStatusFilter = 'all' | 'pending' | 'packing' | 'payment' | 'success' | 'cancel'

const STATUS_FILTER_TABS: Array<{ value: NextStepStatusFilter; label: string }> = [
  { value: 'all', label: 'ทั้งหมด' },
  { value: 'pending', label: 'รอดำเนินการ' },
  { value: 'packing', label: 'จัดเตรียมสินค้า' },
  { value: 'payment', label: 'รอชำระ' },
  { value: 'success', label: 'สำเร็จ' },
  { value: 'cancel', label: 'ยกเลิก' },
]

const STATUS_FILTER_VALUES = new Set<NextStepStatusFilter>(STATUS_FILTER_TABS.map((tab) => tab.value))

export default function NextStepMarketplace() {
  const [params, setParams] = useSearchParams()
  const initialRange = useMemo(() => defaultDateRange(), [])
  const [dateRange, setDateRange] = useState<DateRange>(() => ({
    from: params.get('from_date') || initialRange.from,
    to: params.get('to_date') || initialRange.to,
  }))
  const [searchDraft, setSearchDraft] = useState(params.get('search') || '')
  const [state, setState] = useState<NextStepMarketplaceState | null>(null)
  const [loading, setLoading] = useState(true)
  const [networkError, setNetworkError] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)
  const markedReadDocsRef = useRef<Set<string>>(new Set())
  const markNotificationEntityReadLocal = useNotificationsStore((s) => s.markEntityReadLocal)
  const page = Math.max(1, Number(params.get('page') || '1') || 1)
  const committedSearch = params.get('search') || ''
  const statusFilter = readStatusFilter(params)
  const rangeError = dateRangeError(dateRange)
  const rangeReady = Boolean(dateRange.from && dateRange.to && !rangeError)

  const setQuery = useCallback((next: Record<string, string | number | null | undefined>) => {
    setParams((current) => {
      const nextParams = new URLSearchParams(current)
      for (const [key, value] of Object.entries(next)) {
        if (value === null || value === undefined || value === '') {
          nextParams.delete(key)
        } else {
          nextParams.set(key, String(value))
        }
      }
      return nextParams
    })
  }, [setParams])

  useEffect(() => {
    const nextFrom = params.get('from_date') || initialRange.from
    const nextTo = params.get('to_date') || initialRange.to
    setDateRange((current) => (
      current.from === nextFrom && current.to === nextTo
        ? current
        : { from: nextFrom, to: nextTo }
    ))
    setSearchDraft(params.get('search') || '')
  }, [initialRange.from, initialRange.to, params])

  useEffect(() => {
    if (!rangeReady) {
      setLoading(false)
      return
    }

    let active = true
    setLoading(true)
    setNetworkError(false)
    client
      .get<NextStepMarketplaceState>('/api/nextstep-marketplace/orders', {
        params: {
          from_date: dateRange.from,
          to_date: dateRange.to,
          search: committedSearch || undefined,
          status: statusFilter === 'all' ? undefined : statusFilter,
          page,
          size: PAGE_SIZE,
        },
      })
      .then((r) => {
        if (!active) return
        setState(r.data)
      })
      .catch(() => {
        if (!active) return
        setState(null)
        setNetworkError(true)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [committedSearch, dateRange.from, dateRange.to, page, rangeReady, refreshTick, statusFilter])

  useEffect(() => {
    if (loading || !state?.available || !committedSearch.trim()) return
    const search = committedSearch.trim().toLowerCase()
    const order = (state.orders ?? []).find((row) => row.doc_no.toLowerCase() === search)
    if (!order?.doc_no || markedReadDocsRef.current.has(order.doc_no)) return

    markedReadDocsRef.current.add(order.doc_no)
    client
      .post<NotificationWriteResponse>(`/api/nextstep-marketplace/orders/${encodeURIComponent(order.doc_no)}/notifications/read`)
      .then((res) => {
        markNotificationEntityReadLocal('nextstep_order', order.doc_no, res.data.unread ?? 0, res.data.unread_by_source)
      })
      .catch(() => {
        markedReadDocsRef.current.delete(order.doc_no)
      })
  }, [committedSearch, loading, markNotificationEntityReadLocal, state?.available, state?.orders])

  const summary = state?.summary
  const meta = state?.meta
  const orders = state?.orders ?? []
  const total = meta?.total ?? summary?.total_orders ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const activeStatusLabel = statusTabLabel(statusFilter)

  const updateRange = (next: DateRange) => {
    setDateRange(next)
    if (next.from && next.to && !dateRangeError(next)) {
      setQuery({ from_date: next.from, to_date: next.to, page: 1 })
    }
  }

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setQuery({ search: searchDraft.trim(), page: 1 })
  }

  const setStatusFilter = (value: string) => {
    const nextStatus = readStatusFilterValue(value)
    setQuery({
      status: nextStatus === 'all' ? null : nextStatus,
      page: 1,
    })
  }

  return (
    <div className="space-y-3">
      <Card className="rounded-lg border-border/70 shadow-sm">
        <CardContent className="space-y-3 p-3">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div className="min-w-0">
              <Button asChild variant="ghost" size="sm" className="-ml-2 h-7 px-2 text-xs">
                <Link to={`/dashboard?from_date=${encodeURIComponent(dateRange.from)}&to_date=${encodeURIComponent(dateRange.to)}`}>
                  <ArrowLeft className="mr-1.5 h-3 w-3" />
                  กลับ Dashboard
                </Link>
              </Button>
              <div className="mt-1.5 flex min-w-0 items-center gap-2.5">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-border bg-background">
                  <Store className="h-5 w-5 text-primary" />
                </span>
                <div className="min-w-0">
                  <h1 className="truncate text-base font-semibold leading-5 text-foreground">NextStep Marketplace</h1>
                  <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                    <span>ยอดจากเอกสาร MQT/PREQT ใน SML</span>
                    {meta && <span>{formatShortDate(meta.date_from)} - {formatShortDate(meta.date_to)}</span>}
                    {meta && <span>{meta.date_basis}</span>}
                  </div>
                </div>
              </div>
            </div>

            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4 xl:min-w-[560px] xl:max-w-[680px]">
              <SummaryMetric label="ยอดสุทธิ MQT/PREQT" value={loading ? '—' : formatAmount(summary?.total_amount ?? 0)} />
              <SummaryMetric label="ออเดอร์" value={loading ? '—' : formatCount(summary?.total_orders ?? 0)} />
              <SummaryMetric label="CN หักแล้ว" value={loading ? '—' : formatAmount(summary?.cn_total_amount ?? 0)} />
              <SummaryMetric label="คงค้าง" value={loading ? '—' : formatCount(pendingStatusCount(summary))} />
            </div>
          </div>

          <div className="grid gap-2 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-start">
            <form onSubmit={handleSearch} className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              <div className="relative min-w-0">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={searchDraft}
                  onChange={(event) => setSearchDraft(event.target.value)}
                  placeholder="ค้นหา MQT, PREQT, invoice, sale code, remark"
                  className="h-9 pl-9"
                />
              </div>
              <div className="flex gap-2">
                <Button type="submit" size="sm" className="min-w-20" disabled={loading || Boolean(rangeError)}>
                  ค้นหา
                </Button>
                {committedSearch && (
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setSearchDraft('')
                      setQuery({ search: null, page: 1 })
                    }}
                  >
                    ล้าง
                  </Button>
                )}
              </div>
            </form>
            <div className="flex flex-wrap items-center gap-2 lg:justify-end">
              <DateRangePicker
                from={dateRange.from}
                to={dateRange.to}
                onFromChange={(from) => updateRange({ ...dateRange, from })}
                onToChange={(to) => updateRange({ ...dateRange, to })}
                onRangeChange={updateRange}
                className="w-full sm:w-[250px]"
                title="ช่วงวันที่ NextStep Marketplace"
                description="กรองเอกสาร MQT/PREQT ตามวันที่เอกสารใน SML"
                clearLabel="ล้างช่วงวันที่"
              />
              <Button
                type="button"
                variant="outline"
                size="icon"
                className="h-9 w-9"
                onClick={() => setRefreshTick((tick) => tick + 1)}
                disabled={loading || Boolean(rangeError)}
                aria-label="รีเฟรช NextStep Marketplace"
                title="รีเฟรช NextStep Marketplace"
              >
                <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
              </Button>
            </div>
          </div>
          {rangeError && <div className="text-xs text-destructive">{rangeError}</div>}
        </CardContent>
      </Card>

      <div className="rounded-lg border border-border bg-card px-3 pt-2 shadow-sm">
        <Tabs value={statusFilter} onValueChange={setStatusFilter}>
          <TabsList className="h-auto w-full justify-start overflow-x-auto rounded-none border-b border-border bg-transparent p-0">
            {STATUS_FILTER_TABS.map((tab) => (
              <TabsTrigger
                key={tab.value}
                value={tab.value}
                className="h-10 shrink-0 rounded-none border-b-2 border-transparent bg-transparent px-3 text-sm data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none"
              >
                <span>{tab.label}</span>
                <Badge variant="outline" className="ml-2 h-5 bg-background px-1.5 text-[10px]">
                  {formatCount(statusTabCount(summary, tab.value))}
                </Badge>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <Card className="rounded-lg border-border/70 shadow-sm">
        <CardContent className="space-y-3 p-3">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-foreground">รายการเอกสาร MQT/PREQT</div>
              <div className="mt-0.5 text-xs text-muted-foreground">
                {loading ? 'กำลังโหลด' : `${formatCount(total)} รายการ${statusFilter === 'all' ? '' : ` · ${activeStatusLabel}`}`}
              </div>
            </div>
            {statusFilter !== 'all' && <OrderStatus status={statusFilter} />}
          </div>

          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, index) => (
                <div key={index} className="h-12 rounded-md bg-muted/35" />
              ))}
            </div>
          ) : networkError ? (
            <StateMessage
              title="โหลดข้อมูลไม่สำเร็จ"
              description="ลองรีเฟรชอีกครั้ง หรือเช็ก backend log ถ้า SML API ไม่ตอบ"
            />
          ) : !state?.configured ? (
            <ConfigMessage message={state?.message || 'ยังไม่ได้ตั้งค่า SML API สำหรับ NextStep Marketplace'} />
          ) : !state.available ? (
            <StateMessage title="เชื่อมต่อ SML ไม่สำเร็จ" description={state.message || 'ตรวจ SML API แล้วลองรีเฟรชอีกครั้ง'} />
          ) : orders.length === 0 ? (
            <StateMessage
              title={statusFilter === 'all' ? 'ไม่พบออเดอร์ในเงื่อนไขนี้' : `ไม่พบออเดอร์สถานะ${activeStatusLabel}`}
              description="ช่วงวันที่ คำค้นหา และสถานะนี้ยังไม่มีเอกสาร MQT/PREQT ที่ตรงกัน"
            />
          ) : (
            <OrdersTable orders={orders} />
          )}

          {state?.available && totalPages > 1 && (
            <div className="flex flex-col gap-2 border-t border-border/70 pt-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-xs text-muted-foreground">
                หน้า {formatCount(page)} / {formatCount(totalPages)}
              </div>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={page <= 1}
                  onClick={() => setQuery({ page: page - 1 })}
                >
                  ก่อนหน้า
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={page >= totalPages}
                  onClick={() => setQuery({ page: page + 1 })}
                >
                  ถัดไป
                </Button>
              </div>
            </div>
          )}

          <div className="rounded-md border border-border/70 bg-muted/25 px-3 py-2 text-xs leading-5 text-muted-foreground">
            ยอดนี้อ่านจากเอกสาร MQT/PREQT ใน SML ตามวันที่เอกสาร ไม่ใช่ยอดรับชำระหรือ payout
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function OrdersTable({ orders }: { orders: NextStepMarketplaceOrder[] }) {
  return (
    <div className="overflow-hidden rounded-md border border-border/70">
      <div className="hidden grid-cols-[1.15fr_0.8fr_0.8fr_0.75fr_0.75fr_0.75fr] gap-3 border-b border-border bg-muted/30 px-3 py-1.5 text-xs font-medium text-muted-foreground lg:grid">
        <div>เอกสาร MQT/PREQT</div>
        <div>วันที่</div>
        <div>สถานะ</div>
        <div className="text-right">ยอดสุทธิ</div>
        <div className="text-right">CN</div>
        <div className="text-right">คงเหลือ</div>
      </div>
      <div className="divide-y divide-border/70">
        {orders.map((order) => (
          <div key={order.doc_no} className="grid gap-2 px-3 py-2 text-sm lg:grid-cols-[1.15fr_0.8fr_0.8fr_0.75fr_0.75fr_0.75fr] lg:items-center lg:gap-3">
            <div className="min-w-0">
              <div className="truncate font-semibold text-foreground">{order.doc_no}</div>
              <div className="mt-0.5 truncate text-xs text-muted-foreground">
                {order.inv_doc_no ? `Invoice ${order.inv_doc_no}` : order.remark_qt || 'ยังไม่มี invoice'}
              </div>
            </div>
            <div className="text-xs text-muted-foreground lg:text-sm lg:text-foreground">
              {formatShortDate(order.doc_date)}
              {order.doc_time && <span className="ml-1 text-muted-foreground">{order.doc_time}</span>}
            </div>
            <div>
              <OrderStatus status={order.status} />
            </div>
            <div className="font-semibold tabular-nums text-foreground lg:text-right">{formatAmount(order.total_amount, true)}</div>
            <div className="tabular-nums text-muted-foreground lg:text-right">{formatAmount(order.cn_total_amount ?? 0, true)}</div>
            <div className="tabular-nums text-muted-foreground lg:text-right">{formatAmount(order.balance ?? 0, true)}</div>
          </div>
        ))}
      </div>
    </div>
  )
}

function SummaryMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-md border border-border/70 bg-muted/25 px-3 py-2">
      <div className="truncate text-[11px] font-medium leading-4 text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-sm font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function StatusBadge({
  label,
  count,
  tone,
}: {
  label: string
  count?: number
  tone: 'muted' | 'info' | 'warning' | 'success' | 'danger'
}) {
  return (
    <Badge
      variant="outline"
      className={cn(
        'rounded-full bg-background',
        tone === 'info' && 'border-primary/25 bg-primary/10 text-primary',
        tone === 'warning' && 'border-warning/25 bg-warning/10 text-warning',
        tone === 'success' && 'border-success/25 bg-success/10 text-success',
        tone === 'danger' && 'border-destructive/25 bg-destructive/10 text-destructive',
      )}
    >
      {label}{typeof count === 'number' ? ` ${formatCount(count)}` : ''}
    </Badge>
  )
}

function OrderStatus({ status }: { status: string }) {
  const tone =
    status === 'success' ? 'success' :
      status === 'cancel' ? 'danger' :
        status === 'payment' ? 'warning' :
          status === 'packing' ? 'info' : 'muted'
  return <StatusBadge label={statusLabel(status)} tone={tone} />
}

function ConfigMessage({ message }: { message: string }) {
  return (
    <div className="flex flex-col gap-3 rounded-md border border-warning/35 bg-warning/[0.07] p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-start gap-2.5">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
        <div>
          <p className="text-sm font-semibold">ยังไม่แสดง NextStep Marketplace</p>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{message}</p>
        </div>
      </div>
      <Button asChild size="sm" variant="outline">
        <Link to="/settings/instance">ตั้งค่า</Link>
      </Button>
    </div>
  )
}

function StateMessage({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex min-h-[160px] items-center justify-center rounded-md border border-dashed border-border bg-muted/20 px-4 text-center">
      <div>
        <div className="text-sm font-semibold text-foreground">{title}</div>
        <div className="mt-1 max-w-md text-xs leading-5 text-muted-foreground">{description}</div>
      </div>
    </div>
  )
}

function pendingStatusCount(summary?: { pending_count?: number; packing_count?: number; payment_count?: number; status_counts?: Record<string, number> }): number {
  const counts = summary?.status_counts ?? {}
  return (
    (counts.pending ?? summary?.pending_count ?? 0) +
    (counts.packing ?? summary?.packing_count ?? 0) +
    (counts.payment ?? summary?.payment_count ?? 0)
  )
}

function statusTabCount(summary: NextStepMarketplaceSummary | undefined, status: NextStepStatusFilter): number {
  if (!summary) return 0
  if (status === 'all') {
    const fromCounts = STATUS_FILTER_TABS
      .filter((tab) => tab.value !== 'all')
      .reduce((sum, tab) => sum + statusTabCount(summary, tab.value), 0)
    return fromCounts || summary.total_orders || 0
  }

  const counts = summary.status_counts ?? {}
  const fallback = `${status}_count` as keyof NextStepMarketplaceSummary
  return counts[status] ?? Number(summary[fallback] ?? 0)
}

function statusTabLabel(status: NextStepStatusFilter): string {
  return STATUS_FILTER_TABS.find((tab) => tab.value === status)?.label ?? 'ทั้งหมด'
}

function readStatusFilter(params: URLSearchParams): NextStepStatusFilter {
  return readStatusFilterValue(params.get('status') || 'all')
}

function readStatusFilterValue(value: string): NextStepStatusFilter {
  const normalized = value.trim().toLowerCase() as NextStepStatusFilter
  return STATUS_FILTER_VALUES.has(normalized) ? normalized : 'all'
}

const amountFormatter = new Intl.NumberFormat('th-TH', {
  maximumFractionDigits: 0,
})

function formatAmount(value: number, compact = false): string {
  const n = Number(value || 0)
  void compact
  return amountFormatter.format(n)
}

function formatCount(value: number): string {
  return Number(value || 0).toLocaleString('th-TH')
}

function formatShortDate(value: string): string {
  const [year, month, day] = value.split('-')
  if (!year || !month || !day) return value
  return `${day}/${month}/${year}`
}

function statusLabel(value: string): string {
  if (value === 'packing') return 'จัดเตรียมสินค้า'
  if (value === 'payment') return 'รอชำระ'
  if (value === 'success') return 'สำเร็จ'
  if (value === 'cancel') return 'ยกเลิก'
  return 'รอดำเนินการ'
}

function defaultDateRange(): DateRange {
  const today = new Date()
  const firstDay = new Date(today.getFullYear(), today.getMonth(), 1)
  return {
    from: formatDateInput(firstDay),
    to: formatDateInput(today),
  }
}

function dateRangeError(range: DateRange): string {
  if (!range.from || !range.to) return ''
  const from = new Date(`${range.from}T00:00:00`)
  const to = new Date(`${range.to}T00:00:00`)
  if (Number.isNaN(from.getTime()) || Number.isNaN(to.getTime())) return 'รูปแบบวันที่ไม่ถูกต้อง'
  if (range.from > range.to) return 'วันที่เริ่มต้นต้องไม่เกินวันที่สิ้นสุด'
  if (to.getTime() - from.getTime() > 366 * 24 * 60 * 60 * 1000) return 'เลือกช่วงวันที่ได้ไม่เกิน 366 วัน'
  return ''
}

function formatDateInput(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
