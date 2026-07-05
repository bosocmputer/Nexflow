import { useCallback, useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, Database, RefreshCw, Search, Store } from 'lucide-react'

import client from '@/api/client'
import { DateRangePicker } from '@/components/common/DateRangePicker'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import type { NextStepMarketplaceOrder, NextStepMarketplaceState } from '@/types'

type DateRange = {
  from: string
  to: string
}

const PAGE_SIZE = 20

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
  const page = Math.max(1, Number(params.get('page') || '1') || 1)
  const committedSearch = params.get('search') || ''
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
  }, [committedSearch, dateRange.from, dateRange.to, page, rangeReady, refreshTick])

  const summary = state?.summary
  const meta = state?.meta
  const orders = state?.orders ?? []
  const total = meta?.total ?? summary?.total_orders ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const statusCounts = summary?.status_counts ?? {}

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

  return (
    <div className="space-y-4">
      <Card className="rounded-lg border-border/70 shadow-sm">
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="min-w-0">
              <Button asChild variant="ghost" size="sm" className="-ml-2 h-8 px-2">
                <Link to={`/dashboard?from_date=${encodeURIComponent(dateRange.from)}&to_date=${encodeURIComponent(dateRange.to)}`}>
                  <ArrowLeft className="mr-1.5 h-3.5 w-3.5" />
                  กลับ Dashboard
                </Link>
              </Button>
              <div className="mt-2 flex min-w-0 items-center gap-3">
                <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md border border-border bg-background">
                  <Store className="h-7 w-7 text-primary" />
                </span>
                <div className="min-w-0">
                  <h1 className="truncate text-lg font-semibold leading-6 text-foreground">NextStep Marketplace</h1>
                  <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <span>ยอดจาก SML MQT</span>
                    {meta && <span>{formatShortDate(meta.date_from)} - {formatShortDate(meta.date_to)}</span>}
                    {meta && <span>{meta.date_basis}</span>}
                  </div>
                </div>
              </div>
            </div>

            <div className="flex w-full flex-col gap-2 lg:w-auto lg:items-end">
              <div className="flex items-center justify-end gap-2">
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
                <DateRangePicker
                  from={dateRange.from}
                  to={dateRange.to}
                  onFromChange={(from) => updateRange({ ...dateRange, from })}
                  onToChange={(to) => updateRange({ ...dateRange, to })}
                  onRangeChange={updateRange}
                  className="w-full sm:w-[250px]"
                  title="ช่วงวันที่ NextStep"
                  description="กรองเอกสาร MQT ตามวันที่เอกสารใน SML"
                  clearLabel="ล้างช่วงวันที่"
                />
              </div>
              {rangeError && <div className="text-xs text-destructive">{rangeError}</div>}
            </div>
          </div>

          <form onSubmit={handleSearch} className="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
            <div className="relative min-w-0">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={searchDraft}
                onChange={(event) => setSearchDraft(event.target.value)}
                placeholder="ค้นหา MQT, invoice, sale code, remark"
                className="h-10 pl-9"
              />
            </div>
            <div className="flex gap-2">
              <Button type="submit" className="min-w-24" disabled={loading || Boolean(rangeError)}>
                ค้นหา
              </Button>
              {committedSearch && (
                <Button
                  type="button"
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
        </CardContent>
      </Card>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="ยอดสุทธิ MQT" value={loading ? '—' : formatCurrency(summary?.total_amount ?? 0)} icon={<Database className="h-4 w-4" />} />
        <MetricCard label="ออเดอร์" value={loading ? '—' : formatCount(summary?.total_orders ?? 0)} />
        <MetricCard label="CN หักแล้ว" value={loading ? '—' : formatCurrency(summary?.cn_total_amount ?? 0)} />
        <MetricCard label="คงค้าง" value={loading ? '—' : formatCount(pendingStatusCount(summary))} />
      </div>

      <Card className="rounded-lg border-border/70 shadow-sm">
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-foreground">รายการเอกสาร MQT</div>
              <div className="mt-0.5 text-xs text-muted-foreground">
                {loading ? 'กำลังโหลด' : `${formatCount(total)} รายการ`}
              </div>
            </div>
            <div className="flex flex-wrap gap-1.5">
              <StatusBadge label="รอดำเนินการ" count={statusCounts.pending ?? summary?.pending_count ?? 0} tone="muted" />
              <StatusBadge label="แพ็กของ" count={statusCounts.packing ?? summary?.packing_count ?? 0} tone="info" />
              <StatusBadge label="รอชำระ" count={statusCounts.payment ?? summary?.payment_count ?? 0} tone="warning" />
              <StatusBadge label="สำเร็จ" count={statusCounts.success ?? summary?.success_count ?? 0} tone="success" />
              <StatusBadge label="ยกเลิก" count={statusCounts.cancel ?? summary?.cancel_count ?? 0} tone="danger" />
            </div>
          </div>

          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 5 }).map((_, index) => (
                <div key={index} className="h-16 rounded-md bg-muted/35" />
              ))}
            </div>
          ) : networkError ? (
            <StateMessage
              title="โหลดข้อมูลไม่สำเร็จ"
              description="ลองรีเฟรชอีกครั้ง หรือเช็ก backend log ถ้า SML API ไม่ตอบ"
            />
          ) : !state?.configured ? (
            <ConfigMessage message={state?.message || 'ยังไม่ได้ตั้งค่า NextStep marketplace cust_code'} />
          ) : !state.available ? (
            <StateMessage title="เชื่อมต่อ SML ไม่สำเร็จ" description={state.message || 'ตรวจ SML API แล้วลองรีเฟรชอีกครั้ง'} />
          ) : orders.length === 0 ? (
            <StateMessage
              title="ไม่พบออเดอร์ในเงื่อนไขนี้"
              description="ช่วงวันที่และคำค้นหานี้ยังไม่มีเอกสาร MQT ที่ตรงกัน"
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
            ยอดนี้อ่านจาก SML เอกสาร MQT ตามวันที่เอกสาร ไม่ใช่ยอดรับชำระหรือ payout
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function OrdersTable({ orders }: { orders: NextStepMarketplaceOrder[] }) {
  return (
    <div className="overflow-hidden rounded-md border border-border/70">
      <div className="hidden grid-cols-[1.15fr_0.8fr_0.8fr_0.75fr_0.75fr_0.75fr] gap-3 border-b border-border bg-muted/30 px-3 py-2 text-xs font-medium text-muted-foreground lg:grid">
        <div>เอกสาร MQT</div>
        <div>วันที่</div>
        <div>สถานะ</div>
        <div className="text-right">ยอดสุทธิ</div>
        <div className="text-right">CN</div>
        <div className="text-right">คงเหลือ</div>
      </div>
      <div className="divide-y divide-border/70">
        {orders.map((order) => (
          <div key={order.doc_no} className="grid gap-2 px-3 py-3 text-sm lg:grid-cols-[1.15fr_0.8fr_0.8fr_0.75fr_0.75fr_0.75fr] lg:items-center lg:gap-3">
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
            <div className="font-semibold tabular-nums text-foreground lg:text-right">{formatCurrency(order.total_amount, true)}</div>
            <div className="tabular-nums text-muted-foreground lg:text-right">{formatCurrency(order.cn_total_amount ?? 0, true)}</div>
            <div className="tabular-nums text-muted-foreground lg:text-right">{formatCurrency(order.balance ?? 0, true)}</div>
          </div>
        ))}
      </div>
    </div>
  )
}

function MetricCard({ label, value, icon }: { label: string; value: string; icon?: ReactNode }) {
  return (
    <Card className="rounded-lg border-border/70 shadow-sm">
      <CardContent className="p-4">
        <div className="flex items-center justify-between gap-3">
          <div className="text-xs font-medium text-muted-foreground">{label}</div>
          {icon && <span className="text-primary">{icon}</span>}
        </div>
        <div className="mt-2 truncate text-2xl font-semibold tabular-nums text-foreground">{value}</div>
      </CardContent>
    </Card>
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
    <div className="flex min-h-[220px] items-center justify-center rounded-md border border-dashed border-border bg-muted/20 px-4 text-center">
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

const currencyFormatter = new Intl.NumberFormat('th-TH', {
  style: 'currency',
  currency: 'THB',
  maximumFractionDigits: 0,
})

const compactCurrencyFormatter = new Intl.NumberFormat('th-TH', {
  style: 'currency',
  currency: 'THB',
  notation: 'compact',
  maximumFractionDigits: 1,
})

function formatCurrency(value: number, compact = false): string {
  const n = Number(value || 0)
  if (compact && Math.abs(n) >= 100_000) {
    return compactCurrencyFormatter.format(n)
  }
  return currencyFormatter.format(n)
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
  if (value === 'packing') return 'แพ็กของ'
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
