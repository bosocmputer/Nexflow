import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { AlertTriangle, ArrowRight, BarChart3, ReceiptText, RefreshCw, Store } from 'lucide-react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DateRangePicker } from '@/components/common/DateRangePicker'
import client from '@/api/client'
import { ENABLE_SHOPEE_REALTIME_OPS } from '@/lib/featureFlags'
import { cn } from '@/lib/utils'
import type { DashboardStats, NextStepMarketplaceState, PlatformKey, PlatformSalesStat } from '@/types'

type SetupStatus = {
  ready: boolean
  ready_count: number
  total_count: number
  steps?: { key: string; ready: boolean; status: string }[]
}

type PlatformMeta = {
  key: PlatformKey
  label: string
  icon: string
  color: string
  softClass: string
  to: string
}

type DashboardDateRange = {
  from: string
  to: string
}

type ShareBreakdownItem = {
  key: string
  label: string
  color: string
  amount: number
  sharePct: number
}

type SalesTrendPlatformKey = 'shopee' | 'lazada' | 'tiktok' | 'nextstep'

type SalesTrendSeriesKey =
  | 'shopee_amount'
  | 'previous_shopee_amount'
  | 'lazada_amount'
  | 'previous_lazada_amount'
  | 'tiktok_amount'
  | 'previous_tiktok_amount'
  | 'nextstep_amount'
  | 'previous_nextstep_amount'

type SalesTrendPlatformSeries = {
  key: SalesTrendPlatformKey
  label: string
  color: string
  currentKey: SalesTrendSeriesKey
  previousKey: SalesTrendSeriesKey
}

const PLATFORM_META: Record<PlatformKey, PlatformMeta> = {
  shopee: {
    key: 'shopee',
    label: 'Shopee',
    icon: '/shopee2.svg',
    color: '#ee4d2d',
    softClass: 'bg-[#fff1eb] text-[#9f2f16] border-[#f3c4b6]',
    to: ENABLE_SHOPEE_REALTIME_OPS ? '/shopee-operations' : '/import/shopee',
  },
  lazada: {
    key: 'lazada',
    label: 'Lazada',
    icon: '/lazada2.png',
    color: '#1d3491',
    softClass: 'bg-[#e5ebff] text-link border-[#cbd7ff]',
    to: '/import/lazada',
  },
  tiktok: {
    key: 'tiktok',
    label: 'TikTok',
    icon: '/tiktok2.png',
    color: 'hsl(var(--foreground))',
    softClass: 'bg-muted text-foreground border-border',
    to: '/import/tiktok',
  },
}

const PLATFORM_ORDER: PlatformKey[] = ['shopee', 'lazada', 'tiktok']
const NEXTSTEP_TREND_COLOR = '#0f766e'
const SALES_TREND_PLATFORMS: SalesTrendPlatformSeries[] = [
  { key: 'shopee', label: 'Shopee', color: PLATFORM_META.shopee.color, currentKey: 'shopee_amount', previousKey: 'previous_shopee_amount' },
  { key: 'lazada', label: 'Lazada', color: PLATFORM_META.lazada.color, currentKey: 'lazada_amount', previousKey: 'previous_lazada_amount' },
  { key: 'tiktok', label: 'TikTok', color: PLATFORM_META.tiktok.color, currentKey: 'tiktok_amount', previousKey: 'previous_tiktok_amount' },
  { key: 'nextstep', label: 'NextStep', color: NEXTSTEP_TREND_COLOR, currentKey: 'nextstep_amount', previousKey: 'previous_nextstep_amount' },
]

const DEFAULT_VISIBLE_TREND_PLATFORMS: Record<SalesTrendPlatformKey, boolean> = {
  shopee: true,
  lazada: true,
  tiktok: true,
  nextstep: true,
}

export default function Dashboard() {
  const [searchParams, setSearchParams] = useSearchParams()
  const initialDateRange = defaultDashboardDateRange()
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [statsError, setStatsError] = useState(false)
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null)
  const [dateRange, setDateRange] = useState<DashboardDateRange>(() => ({
    from: searchParams.get('from_date') || initialDateRange.from,
    to: searchParams.get('to_date') || initialDateRange.to,
  }))
  const [refreshTick, setRefreshTick] = useState(0)
  const [lastUpdatedAt, setLastUpdatedAt] = useState<string | null>(null)
  const dateRangeError = dashboardDateRangeError(dateRange)
  const dateRangeReady = Boolean(dateRange.from && dateRange.to && !dateRangeError)

  useEffect(() => {
    const fallback = defaultDashboardDateRange()
    const nextFrom = searchParams.get('from_date') || fallback.from
    const nextTo = searchParams.get('to_date') || fallback.to
    setDateRange((current) => (
      current.from === nextFrom && current.to === nextTo
        ? current
        : { from: nextFrom, to: nextTo }
    ))
  }, [searchParams])

  const handleDateRangeChange = useCallback((range: DashboardDateRange) => {
    setDateRange(range)
    if (range.from && range.to && !dashboardDateRangeError(range)) {
      setSearchParams((current) => {
        const next = new URLSearchParams(current)
        next.set('from_date', range.from)
        next.set('to_date', range.to)
        return next
      })
    }
  }, [setSearchParams])

  const refreshStats = useCallback(() => {
    if (dateRangeReady) {
      setRefreshTick((tick) => tick + 1)
    }
  }, [dateRangeReady])

  useEffect(() => {
    if (!dateRangeReady) {
      setLoading(false)
      return
    }

    let active = true
    setLoading(true)
    client
      .get<DashboardStats>('/api/dashboard/stats', {
        params: {
          from_date: dateRange.from,
          to_date: dateRange.to,
          include_nextstep: '1',
        },
      })
      .then((r) => {
        if (!active) return
        setStats(r.data)
        setStatsError(false)
        setLastUpdatedAt(new Date().toISOString())
      })
      .catch(() => {
        if (!active) return
        setStatsError(true)
        setStats(null)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [dateRange.from, dateRange.to, dateRangeReady, refreshTick])

  useEffect(() => {
    client
      .get<SetupStatus>('/api/setup/status')
      .then((r) => setSetupStatus(r.data))
      .catch(() => null)
  }, [])

  const smlSetupIssue = setupStatus?.steps?.find((step) => step.key === 'instance' && !step.ready)

  return (
    <div className="space-y-4">
      {smlSetupIssue && (
        <Card className="border-warning/35 bg-warning/[0.07] shadow-sm">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
              <div>
                <p className="text-sm font-semibold">ระบบหลักยังต้องตรวจ</p>
                <p className="mt-0.5 text-xs text-muted-foreground">SML ยังไม่พร้อม: {smlSetupIssue.status}</p>
              </div>
            </div>
            <Button asChild size="sm">
              <Link to="/setup">ตรวจ setup</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      <PlatformSalesOverview
        stats={stats}
        loading={loading}
        error={statsError}
        dateRange={dateRange}
        dateRangeError={dateRangeError}
        lastUpdatedAt={lastUpdatedAt}
        onRefresh={refreshStats}
        onDateRangeChange={handleDateRangeChange}
      />
      <SalesTrendCard stats={stats} loading={loading} error={statsError} />
    </div>
  )
}

function PlatformSalesOverview({
  stats,
  loading,
  error,
  dateRange,
  dateRangeError,
  lastUpdatedAt,
  onRefresh,
  onDateRangeChange,
}: {
  stats: DashboardStats | null
  loading: boolean
  error: boolean
  dateRange: DashboardDateRange
  dateRangeError: string
  lastUpdatedAt: string | null
  onRefresh: () => void
  onDateRangeChange: (range: DashboardDateRange) => void
}) {
  const platforms = platformStats(stats)
  const total = dashboardCurrentTotal(stats)
  const previousTotal = dashboardPreviousTotal(stats)
  const today = dashboardEndDateTotal(stats)
  const orders = dashboardOrderCount(stats)
  const changePct = dashboardChangePct(total, previousTotal)
  const riskCount = platforms.reduce((sum, p) => sum + p.needs_review_count + p.failed_count, 0)
  const meta = stats?.platform_sales_meta

  return (
    <section className="space-y-3" aria-label="ยอดขายตามแพลตฟอร์ม">
      <Card className="overflow-hidden border-border/70 shadow-sm">
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="secondary" className="gap-1 rounded-full px-2.5">
                  <ReceiptText className="h-3.5 w-3.5" />
                  ยอดขาย Nexflow
                </Badge>
                <span className="text-xs text-muted-foreground">
                  {meta ? `${formatShortDate(meta.from_date)} - ${formatShortDate(meta.to_date)} · ${meta.timezone}` : 'เดือนนี้ถึงวันนี้'}
                </span>
              </div>
              <div className="mt-2 text-sm font-medium text-muted-foreground">ยอดขายใน Nexflow ตามช่วงวันที่</div>
            </div>
            <div className="flex w-full flex-col gap-2 xl:w-auto xl:items-end">
              <div className="flex items-center justify-between gap-2 xl:justify-end">
                <div className="text-xs text-muted-foreground">
                  {lastUpdatedAt ? `อัปเดตล่าสุดเมื่อ ${formatUpdatedAt(lastUpdatedAt)}` : 'ยังไม่เคยอัปเดต'}
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 shrink-0"
                  onClick={onRefresh}
                  disabled={loading || Boolean(dateRangeError)}
                  aria-label="รีเฟรชยอดขาย"
                  title="รีเฟรชยอดขาย"
                >
                  <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
                </Button>
              </div>
              <DashboardDateFilter
                range={dateRange}
                error={dateRangeError}
                onChange={onDateRangeChange}
              />
            </div>
          </div>

          <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_420px] lg:items-end">
            <div className="min-w-0">
              <div className="mt-1 text-3xl font-semibold leading-tight tracking-normal text-foreground">
                {loading ? '—' : formatCurrency(total)}
              </div>
              <ComparisonLine
                loading={loading}
                current={total}
                previous={previousTotal}
                changePct={changePct}
                previousFrom={meta?.previous_from_date}
                previousTo={meta?.previous_to_date}
              />
              <p className="mt-2 max-w-3xl text-xs leading-5 text-muted-foreground">
                Shopee ใช้คำสั่งซื้อที่บันทึกใน Nexflow; Lazada/TikTok ใช้เอกสารขาย ไม่ใช่ยอดรับชำระหรือ payout
              </p>
            </div>
            <div className="grid grid-cols-3 gap-2">
              <HeroMetric label="ยอดวันสิ้นสุด" value={loading ? '—' : formatCurrency(today, true)} />
              <HeroMetric label="ออเดอร์" value={loading ? '—' : formatCount(orders)} />
              <HeroMetric label="รายการเสี่ยง" value={loading ? '—' : formatCount(riskCount)} />
            </div>
          </div>
        </CardContent>
      </Card>

      {error && (
        <Card className="border-warning/35 bg-warning/[0.07] shadow-sm">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
              <div>
                <p className="text-sm font-semibold">โหลดตัวเลขยอดขายไม่สำเร็จ</p>
                <p className="mt-0.5 text-xs text-muted-foreground">ยังเปิดรายการจากเมนูแพลตฟอร์มได้ตามปกติ</p>
              </div>
            </div>
            <Button asChild size="sm" variant="outline">
              <Link to={ENABLE_SHOPEE_REALTIME_OPS ? '/shopee-operations' : '/import/shopee'}>เปิด Shopee</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {platforms.map((platform) => (
          <PlatformSalesCard key={platform.platform} stat={platform} loading={loading} />
        ))}
        <NextStepSalesCard
          state={stats?.nextstep_marketplace}
          loading={loading}
          dateRange={dateRange}
          sharePct={nextStepSharePct(stats)}
        />
      </div>
    </section>
  )
}

function DashboardDateFilter({
  range,
  error,
  onChange,
}: {
  range: DashboardDateRange
  error: string
  onChange: (range: DashboardDateRange) => void
}) {
  return (
    <div className="w-full xl:w-auto">
      <DateRangePicker
        from={range.from}
        to={range.to}
        onFromChange={(from) => onChange({ ...range, from })}
        onToChange={(to) => onChange({ ...range, to })}
        onRangeChange={onChange}
        className="w-full sm:w-[250px]"
        title="ช่วงวันที่ยอดขาย"
        description="เลือกวันที่เริ่มต้นและสิ้นสุดเพื่อดูยอดขาย Nexflow"
        clearLabel="ล้างช่วงวันที่"
      />
      {error && <div className="mt-2 text-xs text-destructive">{error}</div>}
    </div>
  )
}

function HeroMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-background/70 px-3 py-2">
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function ComparisonLine({
  loading,
  current,
  previous,
  changePct,
  previousFrom,
  previousTo,
  compact = false,
}: {
  loading: boolean
  current: number
  previous: number
  changePct?: number | null
  previousFrom?: string
  previousTo?: string
  compact?: boolean
}) {
  const tone = comparisonTone(current, previous, changePct)
  const label = comparisonLabel(current, previous, changePct)
  const previousRange = previousFrom && previousTo
    ? ` (${formatShortDate(previousFrom)} - ${formatShortDate(previousTo)})`
    : ''
  const previousLabel = previous > 0
    ? `ก่อนหน้า ${formatCurrency(previous, compact)}${compact ? '' : previousRange}`
    : `ไม่มีข้อมูลช่วงก่อนหน้า${compact ? '' : previousRange}`

  return (
    <div className={cn('mt-1.5 flex flex-wrap items-center gap-2 text-xs', compact && 'mt-1')}>
      <span
        className={cn(
          'inline-flex h-6 items-center rounded-full border px-2 font-semibold tabular-nums',
          tone === 'up' && 'border-success/25 bg-success/10 text-success',
          tone === 'down' && 'border-destructive/25 bg-destructive/10 text-destructive',
          tone === 'flat' && 'border-border bg-muted/50 text-muted-foreground',
        )}
      >
        {loading ? '—' : label}
      </span>
      <span className="text-muted-foreground tabular-nums">
        {loading ? 'เทียบช่วงก่อนหน้า' : previousLabel}
      </span>
    </div>
  )
}

function PlatformSalesCard({
  stat,
  loading,
}: {
  stat: PlatformSalesStat
  loading: boolean
}) {
  const meta = PLATFORM_META[stat.platform]
  const riskCount = stat.needs_review_count + stat.failed_count

  return (
    <Link
      to={meta.to}
      className="group flex min-h-[174px] flex-col justify-between rounded-lg border border-border bg-card p-4 shadow-sm transition-colors hover:border-primary/50 hover:bg-background/80"
    >
      <div>
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md border border-border bg-background">
              <img src={meta.icon} alt="" className="h-8 w-8 object-contain" />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">{meta.label}</div>
              <div className="mt-0.5 text-xs text-muted-foreground">ตามช่วงวันที่</div>
            </div>
          </div>
          <Badge variant="outline" className={meta.softClass}>
            {loading ? '—' : formatPercent(stat.share_pct)}
          </Badge>
        </div>

        <div className="mt-4 text-2xl font-semibold tabular-nums text-foreground">
          {loading ? '—' : formatCurrency(stat.total_amount)}
        </div>
        <ComparisonLine
          loading={loading}
          current={stat.total_amount}
          previous={stat.previous_total_amount ?? 0}
          changePct={stat.change_pct}
          compact
        />
        <div className="mt-2 grid grid-cols-2 gap-2">
          <MiniMetric label="ยอดวันสิ้นสุด" value={loading ? '—' : formatCurrency(stat.today_amount, true)} />
          <MiniMetric label="ออเดอร์" value={loading ? '—' : formatCount(stat.order_count)} />
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border/70 pt-3">
        <div className="flex flex-wrap gap-1.5">
          {riskCount > 0 ? (
            <>
              {stat.needs_review_count > 0 && (
                <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/15">
                  ต้องตรวจ {formatCount(stat.needs_review_count)}
                </Badge>
              )}
              {stat.failed_count > 0 && (
                <Badge variant="destructive">
                  ล้มเหลว {formatCount(stat.failed_count)}
                </Badge>
              )}
            </>
          ) : (
            <span className="text-xs text-muted-foreground">ไม่มีรายการเสี่ยง</span>
          )}
        </div>
        <span className="inline-flex items-center gap-1 text-xs font-semibold text-foreground">
          เปิดรายการ
          <ArrowRight className="h-3 w-3 transition-transform group-hover:translate-x-0.5" />
        </span>
      </div>
    </Link>
  )
}

function NextStepSalesCard({
  state,
  loading,
  dateRange,
  sharePct,
}: {
  state?: NextStepMarketplaceState
  loading: boolean
  dateRange: DashboardDateRange
  sharePct: number
}) {
  const summary = state?.summary
  const statusCounts = summary?.status_counts ?? {}
  const pendingCount =
    (statusCounts.pending ?? summary?.pending_count ?? 0) +
    (statusCounts.packing ?? summary?.packing_count ?? 0) +
    (statusCounts.payment ?? summary?.payment_count ?? 0)
  const cancelCount = statusCounts.cancel ?? summary?.cancel_count ?? 0
  const href = `/nextstep-marketplace?from_date=${encodeURIComponent(dateRange.from)}&to_date=${encodeURIComponent(dateRange.to)}`

  return (
    <Link
      to={href}
      className="group flex min-h-[174px] flex-col justify-between rounded-lg border border-border bg-card p-4 shadow-sm transition-colors hover:border-primary/50 hover:bg-background/80"
    >
      <div>
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md border border-border bg-background">
              <Store className="h-7 w-7 text-primary" />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">NextStep Marketplace</div>
              <div className="mt-0.5 text-xs text-muted-foreground">ตามช่วงวันที่</div>
            </div>
          </div>
          <Badge variant="outline" className="bg-muted text-foreground">
            {loading ? '—' : state?.available ? formatPercent(sharePct) : state?.configured ? 'SML' : 'ตั้งค่า'}
          </Badge>
        </div>

        <div className="mt-4 text-2xl font-semibold tabular-nums text-foreground">
          {loading ? '—' : formatCurrency(summary?.total_amount ?? 0)}
        </div>
        <ComparisonLine
          loading={loading}
          current={summary?.total_amount ?? 0}
          previous={state?.previous_summary?.total_amount ?? 0}
          changePct={state?.change_pct}
          compact
        />
        <div className="mt-1.5 text-xs leading-5 text-muted-foreground">
          {state?.available ? 'ยอดจากเอกสาร MQT/PREQT ใน SML' : state?.message || 'ข้อมูลจาก SML marketplace'}
        </div>
        <div className="mt-2 grid grid-cols-2 gap-2">
          <MiniMetric label="CN หักแล้ว" value={loading ? '—' : formatCurrency(summary?.cn_total_amount ?? 0, true)} />
          <MiniMetric label="ออเดอร์" value={loading ? '—' : formatCount(summary?.total_orders ?? 0)} />
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border/70 pt-3">
        <div className="flex flex-wrap gap-1.5">
          {loading ? (
            <span className="text-xs text-muted-foreground">กำลังโหลด</span>
          ) : !state?.configured ? (
            <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/15">
              ยังไม่ตั้งค่า
            </Badge>
          ) : !state.available ? (
            <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/15">
              โหลดไม่ได้
            </Badge>
          ) : pendingCount > 0 || cancelCount > 0 ? (
            <>
              {pendingCount > 0 && (
                <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/15">
                  ค้าง {formatCount(pendingCount)}
                </Badge>
              )}
              {cancelCount > 0 && <Badge variant="destructive">ยกเลิก {formatCount(cancelCount)}</Badge>}
            </>
          ) : (
            <span className="text-xs text-muted-foreground">ไม่มีงานค้าง</span>
          )}
        </div>
        <span className="inline-flex items-center gap-1 text-xs font-semibold text-foreground">
          เปิดรายละเอียด
          <ArrowRight className="h-3 w-3 transition-transform group-hover:translate-x-0.5" />
        </span>
      </div>
    </Link>
  )
}

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-muted/45 px-2.5 py-2">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-sm font-semibold tabular-nums">{value}</div>
    </div>
  )
}

function SalesTrendCard({
  stats,
  loading,
  error,
}: {
  stats: DashboardStats | null
  loading: boolean
  error: boolean
}) {
  const [visiblePlatforms, setVisiblePlatforms] = useState<Record<SalesTrendPlatformKey, boolean>>(DEFAULT_VISIBLE_TREND_PLATFORMS)
  const data = useMemo(() => salesTrendData(stats), [stats])
  const shareBreakdown = useMemo(() => platformShareBreakdown(stats), [stats])
  const hasSales = salesTrendHasValue(data)
  const hasVisibleSeries = SALES_TREND_PLATFORMS.some((platform) => visiblePlatforms[platform.key])
  const meta = stats?.platform_sales_meta
  const togglePlatform = useCallback((key: SalesTrendPlatformKey) => {
    setVisiblePlatforms((current) => ({
      ...current,
      [key]: !current[key],
    }))
  }, [])

  return (
    <Card className="rounded-lg border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <div className="flex flex-col gap-1 sm:flex-row sm:items-start sm:justify-between">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <BarChart3 className="h-4 w-4 text-accent-strong" />
            ยอดขายรายวันเทียบช่วงก่อนหน้า
          </CardTitle>
          {meta?.previous_from_date && meta.previous_to_date && (
            <div className="text-xs text-muted-foreground">
              เทียบ {formatShortDate(meta.previous_from_date)} - {formatShortDate(meta.previous_to_date)}
            </div>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {!error && (
          <ChartPlatformToggle
            platforms={SALES_TREND_PLATFORMS}
            visiblePlatforms={visiblePlatforms}
            loading={loading}
            onToggle={togglePlatform}
          />
        )}

        {loading ? (
          <div className="h-[260px] rounded-md bg-muted/35" />
        ) : error ? (
          <ChartEmptyState title="โหลดกราฟไม่ได้" description="ลองรีเฟรช หรือเปิดรายการจากเมนูแพลตฟอร์ม" />
        ) : !hasVisibleSeries ? (
          <ChartEmptyState title="ปิดเส้นกราฟทั้งหมด" description="เลือกช่องทางด้านบนเพื่อเปิดเส้นกราฟกลับมา" />
        ) : !hasSales ? (
          <ChartEmptyState title="ไม่มีข้อมูลช่วงนี้หรือช่วงก่อนหน้า" description="เมื่อมีเอกสารขายในช่วงวันที่ กราฟเทียบจะแสดงที่นี่" />
        ) : (
          <div className="h-[280px] w-full">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                <XAxis dataKey="date" tickFormatter={formatTrendDate} tickLine={false} axisLine={false} minTickGap={18} />
                <YAxis tickFormatter={(v) => formatCurrency(Number(v), true)} tickLine={false} axisLine={false} width={96} />
                <Tooltip content={<SalesTrendTooltip visiblePlatforms={visiblePlatforms} />} />
                {SALES_TREND_PLATFORMS.filter((platform) => visiblePlatforms[platform.key]).map((platform) => (
                  <Fragment key={platform.key}>
                    <Line
                      type="monotone"
                      dataKey={platform.currentKey}
                      name={`${platform.label} ช่วงที่เลือก`}
                      stroke={platform.color}
                      strokeWidth={2.2}
                      dot={false}
                      activeDot={{ r: 4 }}
                      connectNulls
                    />
                    <Line
                      type="monotone"
                      dataKey={platform.previousKey}
                      name={`${platform.label} ช่วงก่อนหน้า`}
                      stroke={platform.color}
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      dot={false}
                      activeDot={{ r: 3.5 }}
                      connectNulls
                    />
                  </Fragment>
                ))}
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}

        {!error && <PlatformShareBreakdown items={shareBreakdown.items} loading={loading} total={shareBreakdown.total} />}

        <div className="rounded-md border border-border/70 bg-muted/25 px-3 py-2 text-xs leading-5 text-muted-foreground">
          Failed และต้องตรวจยังรวมในยอด Nexflow; NextStep Marketplace อ่านจากเอกสาร MQT/PREQT ใน SML
        </div>
      </CardContent>
    </Card>
  )
}

function PlatformShareBreakdown({
  items,
  loading,
  total,
}: {
  items: ShareBreakdownItem[]
  loading: boolean
  total: number
}) {
  return (
    <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-4">
      {items.map((item) => {
        const width = Math.max(0, Math.min(100, item.sharePct))
        return (
          <div key={item.key} className="rounded-md border border-border/70 bg-background/65 px-3 py-2">
            <div className="flex items-center justify-between gap-3 text-xs">
              <div className="flex min-w-0 items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: item.color }} />
                <span className="truncate font-medium text-foreground">{item.label}</span>
              </div>
              <span className="shrink-0 tabular-nums text-muted-foreground">
                {loading ? '—' : formatPercent(item.sharePct)}
              </span>
            </div>
            <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full transition-all"
                style={{ width: loading || total <= 0 ? '0%' : `${width}%`, backgroundColor: item.color }}
              />
            </div>
            <div className="mt-1.5 text-xs font-semibold tabular-nums text-foreground">
              {loading ? '—' : formatCurrency(item.amount, true)}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function ChartPlatformToggle({
  platforms,
  visiblePlatforms,
  loading,
  onToggle,
}: {
  platforms: SalesTrendPlatformSeries[]
  visiblePlatforms: Record<SalesTrendPlatformKey, boolean>
  loading: boolean
  onToggle: (key: SalesTrendPlatformKey) => void
}) {
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {platforms.map((item) => {
          const active = visiblePlatforms[item.key]
          return (
            <button
              key={item.key}
              type="button"
              aria-pressed={active}
              disabled={loading}
              onClick={() => onToggle(item.key)}
              className={cn(
                'inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-xs font-medium transition-colors',
                active
                  ? 'border-border bg-background text-foreground shadow-sm'
                  : 'border-border/70 bg-muted/35 text-muted-foreground hover:bg-muted/60',
                loading && 'cursor-not-allowed opacity-60',
              )}
            >
              <span className="relative h-3 w-6 shrink-0">
                <span className="absolute left-0 right-0 top-1 h-0.5" style={{ backgroundColor: item.color }} />
                <span className="absolute left-0 right-0 bottom-0 border-t border-dashed" style={{ borderColor: item.color }} />
              </span>
              <span>{item.label}</span>
            </button>
          )
        })}
      </div>
      <div className="text-xs text-muted-foreground">
        เส้นทึบคือช่วงที่เลือก · เส้นประคือช่วงก่อนหน้า
      </div>
    </div>
  )
}

function ChartEmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex min-h-[220px] items-center justify-center rounded-md border border-dashed border-border bg-muted/20 px-4 text-center">
      <div>
        <div className="text-sm font-semibold text-foreground">{title}</div>
        <div className="mt-1 max-w-md text-xs leading-5 text-muted-foreground">{description}</div>
      </div>
    </div>
  )
}

type SalesTrendPoint = {
  date: string
  previous_date: string
  shopee_amount: number
  previous_shopee_amount: number
  lazada_amount: number
  previous_lazada_amount: number
  tiktok_amount: number
  previous_tiktok_amount: number
  nextstep_amount: number
  previous_nextstep_amount: number
  current_total: number
  previous_total: number
}

function SalesTrendTooltip({
  active,
  payload,
  label,
  visiblePlatforms,
}: {
  active?: boolean
  payload?: Array<{ payload?: SalesTrendPoint }>
  label?: string
  visiblePlatforms: Record<SalesTrendPlatformKey, boolean>
}) {
  if (!active || !payload?.length) return null
  const point = payload[0]?.payload
  if (!point) return null

  const rows = SALES_TREND_PLATFORMS
    .filter((platform) => visiblePlatforms[platform.key])
    .map((series) => ({
      key: series.key,
      label: series.label,
      currentAmount: Number(point[series.currentKey] || 0),
      previousAmount: Number(point[series.previousKey] || 0),
      color: series.color,
    }))
  const visibleCurrentTotal = rows.reduce((sum, item) => sum + item.currentAmount, 0)
  const visiblePreviousTotal = rows.reduce((sum, item) => sum + item.previousAmount, 0)

  return (
    <div className="rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-md">
      <div className="mb-1 font-medium text-popover-foreground">
        {formatShortDate(String(label ?? ''))}
        {point.previous_date ? ` เทียบกับ ${formatShortDate(point.previous_date)}` : ''}
      </div>
      <div className="space-y-1">
        {rows.map((item) => (
          <div key={item.key} className="grid min-w-[260px] grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-3">
            <span className="flex min-w-0 items-center gap-2 font-medium text-popover-foreground">
              <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: item.color }} />
              <span className="truncate">{item.label}</span>
            </span>
            <span className="tabular-nums text-foreground">{formatCurrency(item.currentAmount, true)}</span>
            <span className="tabular-nums text-muted-foreground">{formatCurrency(item.previousAmount, true)}</span>
          </div>
        ))}
        <div className="mt-1 border-t border-border pt-1">
          <div className="grid min-w-[260px] grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-3">
            <span className="font-semibold text-popover-foreground">รวมที่เปิดอยู่</span>
            <span className="font-semibold tabular-nums text-popover-foreground">{formatCurrency(visibleCurrentTotal, true)}</span>
            <span className="font-semibold tabular-nums text-muted-foreground">{formatCurrency(visiblePreviousTotal, true)}</span>
          </div>
          <div className="mt-0.5 grid min-w-[260px] grid-cols-[minmax(0,1fr)_auto_auto] gap-3 text-[11px] text-muted-foreground">
            <span />
            <span>ช่วงนี้</span>
            <span>ก่อนหน้า</span>
          </div>
        </div>
      </div>
    </div>
  )
}

function salesTrendData(stats: DashboardStats | null): SalesTrendPoint[] {
  const previousNextStepByDate = new Map<string, number>()

  for (const point of stats?.nextstep_marketplace?.previous_trend ?? []) {
    previousNextStepByDate.set(point.date, Number(point.total_amount || 0))
  }

  const byDate = new Map<string, SalesTrendPoint>()
  for (const point of platformTrendData(stats)) {
    const current = emptySalesTrendPoint(point.date)
    current.shopee_amount = Number(point.shopee_amount || 0)
    current.lazada_amount = Number(point.lazada_amount || 0)
    current.tiktok_amount = Number(point.tiktok_amount || 0)
    current.nextstep_amount = Number(point.nextstep_amount || 0)
    current.current_total = salesTrendCurrentTotal(current)
    byDate.set(point.date, current)
  }

  for (const point of stats?.sales_comparison_trend ?? []) {
    const current = byDate.get(point.date) ?? emptySalesTrendPoint(point.date)
    current.previous_date = point.previous_date
    current.previous_shopee_amount = Number(point.previous_shopee_amount || 0)
    current.previous_lazada_amount = Number(point.previous_lazada_amount || 0)
    current.previous_tiktok_amount = Number(point.previous_tiktok_amount || 0)
    current.previous_nextstep_amount = Number(previousNextStepByDate.get(point.previous_date) ?? 0)
    const stackedTotal = salesTrendCurrentTotal(current)
    current.current_total = stackedTotal !== 0 ? stackedTotal : Number(point.current_total || 0)
    current.previous_total = Number(point.previous_total || 0) + current.previous_nextstep_amount
    byDate.set(point.date, current)
  }

  return Array.from(byDate.values()).sort((a, b) => a.date.localeCompare(b.date))
}

function emptySalesTrendPoint(date: string): SalesTrendPoint {
  return {
    date,
    previous_date: '',
    shopee_amount: 0,
    previous_shopee_amount: 0,
    lazada_amount: 0,
    previous_lazada_amount: 0,
    tiktok_amount: 0,
    previous_tiktok_amount: 0,
    nextstep_amount: 0,
    previous_nextstep_amount: 0,
    current_total: 0,
    previous_total: 0,
  }
}

function salesTrendCurrentTotal(point: SalesTrendPoint): number {
  return (
    Number(point.shopee_amount || 0) +
    Number(point.lazada_amount || 0) +
    Number(point.tiktok_amount || 0) +
    Number(point.nextstep_amount || 0)
  )
}

function salesTrendHasValue(data: SalesTrendPoint[]): boolean {
  return data.some((point) => (
    SALES_TREND_PLATFORMS.some((platform) => (
      Math.abs(Number(point[platform.currentKey] || 0)) > 0 ||
      Math.abs(Number(point[platform.previousKey] || 0)) > 0
    ))
  ))
}

function platformTrendData(stats: DashboardStats | null) {
  const byDate = new Map<string, {
    date: string
    shopee_amount: number
    lazada_amount: number
    tiktok_amount: number
    nextstep_amount: number
  }>()

  for (const point of stats?.platform_sales_trend ?? []) {
    byDate.set(point.date, {
      date: point.date,
      shopee_amount: point.shopee_amount,
      lazada_amount: point.lazada_amount,
      tiktok_amount: point.tiktok_amount,
      nextstep_amount: Number(point.nextstep_amount ?? 0),
    })
  }

  for (const point of stats?.nextstep_marketplace?.trend ?? []) {
    const current = byDate.get(point.date) ?? {
      date: point.date,
      shopee_amount: 0,
      lazada_amount: 0,
      tiktok_amount: 0,
      nextstep_amount: 0,
    }
    current.nextstep_amount = point.total_amount
    byDate.set(point.date, current)
  }

  return Array.from(byDate.values()).sort((a, b) => a.date.localeCompare(b.date))
}

function platformShareBreakdown(stats: DashboardStats | null): { items: ShareBreakdownItem[]; total: number } {
  const rawItems = [
    ...platformStats(stats).map((platform) => {
      const meta = PLATFORM_META[platform.platform]
      return {
        key: platform.platform,
        label: meta.label,
        color: meta.color,
        amount: platform.total_amount,
      }
    }),
    {
      key: 'nextstep',
      label: 'NextStep Marketplace',
      color: NEXTSTEP_TREND_COLOR,
      amount: stats?.nextstep_marketplace?.available ? (stats.nextstep_marketplace.summary?.total_amount ?? 0) : 0,
    },
  ]
  const total = rawItems.reduce((sum, item) => sum + Number(item.amount || 0), 0)
  return {
    total,
    items: rawItems.map((item) => ({
      ...item,
      sharePct: total > 0 ? (Number(item.amount || 0) / total) * 100 : 0,
    })),
  }
}

function dashboardCurrentTotal(stats: DashboardStats | null): number {
  return Number(stats?.sales_mtd_total ?? 0) + nextStepCurrentTotal(stats)
}

function dashboardPreviousTotal(stats: DashboardStats | null): number {
  return Number(stats?.sales_previous_total ?? 0) + nextStepPreviousTotal(stats)
}

function dashboardOrderCount(stats: DashboardStats | null): number {
  const nextStepOrders = stats?.nextstep_marketplace?.available
    ? Number(stats.nextstep_marketplace.summary?.total_orders ?? 0)
    : 0
  return Number(stats?.sales_mtd_order_count ?? 0) + nextStepOrders
}

function dashboardEndDateTotal(stats: DashboardStats | null): number {
  const endDate = stats?.platform_sales_meta?.to_date
  const nextStepEndDateAmount = stats?.nextstep_marketplace?.available
    ? Number((stats.nextstep_marketplace.trend ?? []).find((point) => point.date === endDate)?.total_amount ?? 0)
    : 0
  return Number(stats?.sales_today_total ?? 0) + nextStepEndDateAmount
}

function dashboardChangePct(current: number, previous: number): number | null {
  if (previous <= 0) return null
  return Math.round(((current - previous) / previous * 100) * 10) / 10
}

function nextStepCurrentTotal(stats: DashboardStats | null): number {
  return stats?.nextstep_marketplace?.available
    ? Number(stats.nextstep_marketplace.summary?.total_amount ?? 0)
    : 0
}

function nextStepPreviousTotal(stats: DashboardStats | null): number {
  return stats?.nextstep_marketplace?.available
    ? Number(stats.nextstep_marketplace.previous_summary?.total_amount ?? 0)
    : 0
}

function nextStepSharePct(stats: DashboardStats | null): number {
  const total = dashboardCurrentTotal(stats)
  return total > 0 ? nextStepCurrentTotal(stats) / total * 100 : 0
}

function platformStats(stats: DashboardStats | null): PlatformSalesStat[] {
  const byPlatform = new Map<PlatformKey, PlatformSalesStat>()
  for (const item of stats?.platform_sales ?? []) {
    byPlatform.set(item.platform, item)
  }
  const total = dashboardCurrentTotal(stats)
  return PLATFORM_ORDER.map((platform) => {
    const meta = PLATFORM_META[platform]
    const item = byPlatform.get(platform) ?? {
      platform,
      label: meta.label,
      total_amount: 0,
      today_amount: 0,
      previous_total_amount: 0,
      change_pct: null,
      order_count: 0,
      sent_count: 0,
      pending_count: 0,
      needs_review_count: 0,
      failed_count: 0,
      share_pct: 0,
    }
    return {
      ...item,
      share_pct: total > 0 ? Number(item.total_amount || 0) / total * 100 : 0,
    }
  })
}

const currencyFormatter = new Intl.NumberFormat('th-TH', {
  style: 'currency',
  currency: 'THB',
  maximumFractionDigits: 0,
})

function formatCurrency(value: number, compact = false): string {
  const n = Number(value || 0)
  void compact
  return currencyFormatter.format(n)
}

function formatCount(value: number): string {
  return Number(value || 0).toLocaleString('th-TH')
}

function formatPercent(value: number): string {
  return `${Number(value || 0).toLocaleString('th-TH', { maximumFractionDigits: 1 })}%`
}

function formatSignedPercent(value: number): string {
  const sign = value > 0 ? '+' : ''
  return `${sign}${Number(value || 0).toLocaleString('th-TH', { maximumFractionDigits: 1 })}%`
}

function comparisonTone(current: number, previous: number, changePct?: number | null): 'up' | 'down' | 'flat' {
  if (typeof changePct === 'number') {
    if (changePct > 0) return 'up'
    if (changePct < 0) return 'down'
    return 'flat'
  }
  if (previous <= 0 && current > 0) return 'up'
  return 'flat'
}

function comparisonLabel(current: number, previous: number, changePct?: number | null): string {
  if (typeof changePct === 'number') return formatSignedPercent(changePct)
  if (previous <= 0 && current > 0) return 'ใหม่'
  return 'เท่าช่วงก่อนหน้า'
}

function formatUpdatedAt(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('th-TH-u-ca-gregory', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}

function formatShortDate(value: string): string {
  const [year, month, day] = value.split('-')
  if (!year || !month || !day) return value
  return `${day}/${month}/${year}`
}

function formatTrendDate(value: string): string {
  const [year, month, day] = value.split('-')
  if (!year || !month || !day) return value
  return `${day}/${month}`
}

function defaultDashboardDateRange(): DashboardDateRange {
  const today = new Date()
  const firstDay = new Date(today.getFullYear(), today.getMonth(), 1)
  return {
    from: formatDateInput(firstDay),
    to: formatDateInput(today),
  }
}

function dashboardDateRangeError(range: DashboardDateRange): string {
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
