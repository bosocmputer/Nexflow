import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, BarChart3, ReceiptText } from 'lucide-react'
import {
  CartesianGrid,
  Legend,
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
import type { DashboardStats, PlatformKey, PlatformSalesStat } from '@/types'

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

export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [statsError, setStatsError] = useState(false)
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null)
  const [dateRange, setDateRange] = useState<DashboardDateRange>(() => defaultDashboardDateRange())
  const dateRangeError = dashboardDateRangeError(dateRange)
  const dateRangeReady = Boolean(dateRange.from && dateRange.to && !dateRangeError)

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
        },
      })
      .then((r) => {
        if (!active) return
        setStats(r.data)
        setStatsError(false)
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
  }, [dateRange.from, dateRange.to, dateRangeReady])

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
        onDateRangeChange={setDateRange}
      />
      <PlatformSalesTrendCard stats={stats} loading={loading} error={statsError} />
    </div>
  )
}

function PlatformSalesOverview({
  stats,
  loading,
  error,
  dateRange,
  dateRangeError,
  onDateRangeChange,
}: {
  stats: DashboardStats | null
  loading: boolean
  error: boolean
  dateRange: DashboardDateRange
  dateRangeError: string
  onDateRangeChange: (range: DashboardDateRange) => void
}) {
  const platforms = platformStats(stats)
  const total = stats?.sales_mtd_total ?? 0
  const today = stats?.sales_today_total ?? 0
  const orders = stats?.sales_mtd_order_count ?? 0
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
            <DashboardDateFilter
              range={dateRange}
              error={dateRangeError}
              onChange={onDateRangeChange}
            />
          </div>

          <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_420px] lg:items-end">
            <div className="min-w-0">
              <div className="mt-1 text-3xl font-semibold leading-tight tracking-normal text-foreground">
                {loading ? '—' : formatCurrency(total)}
              </div>
              <p className="mt-2 max-w-3xl text-xs leading-5 text-muted-foreground">
                Shopee ใช้คำสั่งซื้อที่บันทึกใน Nexflow; Lazada/TikTok ใช้เอกสารขาย ไม่ใช่ยอดรับชำระหรือ payout
              </p>
            </div>
            <div className="grid grid-cols-3 gap-2">
              <HeroMetric label="วันสุดท้าย" value={loading ? '—' : formatCurrency(today, true)} />
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

      <div className="grid gap-3 lg:grid-cols-3">
        {platforms.map((platform) => (
          <PlatformSalesCard key={platform.platform} stat={platform} loading={loading} />
        ))}
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
        <div className="mt-2 grid grid-cols-2 gap-2">
          <MiniMetric label="วันสุดท้าย" value={loading ? '—' : formatCurrency(stat.today_amount, true)} />
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

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-muted/45 px-2.5 py-2">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-sm font-semibold tabular-nums">{value}</div>
    </div>
  )
}

function PlatformSalesTrendCard({
  stats,
  loading,
  error,
}: {
  stats: DashboardStats | null
  loading: boolean
  error: boolean
}) {
  const data = stats?.platform_sales_trend ?? []
  const platforms = platformStats(stats)
  const total = stats?.sales_mtd_total ?? 0
  const hasSales = data.some((point) => point.shopee_amount > 0 || point.lazada_amount > 0 || point.tiktok_amount > 0)

  return (
    <Card className="rounded-lg border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <BarChart3 className="h-4 w-4 text-accent-strong" />
          ยอดรายวันตามช่วงวันที่
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <div className="h-[260px] rounded-md bg-muted/35" />
        ) : error ? (
          <ChartEmptyState title="โหลดกราฟไม่ได้" description="ลองรีเฟรช หรือเปิดรายการจากเมนูแพลตฟอร์ม" />
        ) : !hasSales ? (
          <ChartEmptyState title="ยังไม่มียอดขายในช่วงวันที่นี้" description="เมื่อมีข้อมูลจากแพลตฟอร์ม กราฟจะแสดงยอดรายวัน" />
        ) : (
          <div className="h-[280px] w-full">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                <XAxis dataKey="date" tickFormatter={formatTrendDate} tickLine={false} axisLine={false} minTickGap={18} />
                <YAxis tickFormatter={(v) => formatCurrency(Number(v), true)} tickLine={false} axisLine={false} width={72} />
                <Tooltip content={<PlatformTrendTooltip />} />
                <Legend formatter={(value) => platformLegendLabel(String(value))} />
                <Line type="monotone" dataKey="shopee_amount" name="Shopee" stroke={PLATFORM_META.shopee.color} strokeWidth={2.25} dot={false} activeDot={{ r: 4 }} />
                <Line type="monotone" dataKey="lazada_amount" name="Lazada" stroke={PLATFORM_META.lazada.color} strokeWidth={2.25} dot={false} activeDot={{ r: 4 }} />
                <Line type="monotone" dataKey="tiktok_amount" name="TikTok" stroke={PLATFORM_META.tiktok.color} strokeWidth={2.25} dot={false} activeDot={{ r: 4 }} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}

        {!error && <PlatformShareBreakdown platforms={platforms} loading={loading} total={total} />}

        <div className="rounded-md border border-border/70 bg-muted/25 px-3 py-2 text-xs leading-5 text-muted-foreground">
          Failed และต้องตรวจยังรวมในยอด เพื่อให้เห็นยอดที่เข้าระบบพร้อมงานที่ต้องแก้
        </div>
      </CardContent>
    </Card>
  )
}

function PlatformShareBreakdown({
  platforms,
  loading,
  total,
}: {
  platforms: PlatformSalesStat[]
  loading: boolean
  total: number
}) {
  return (
    <div className="grid gap-2 md:grid-cols-3">
      {platforms.map((platform) => {
        const meta = PLATFORM_META[platform.platform]
        const width = Math.max(0, Math.min(100, platform.share_pct))
        return (
          <div key={platform.platform} className="rounded-md border border-border/70 bg-background/65 px-3 py-2">
            <div className="flex items-center justify-between gap-3 text-xs">
              <div className="flex min-w-0 items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: meta.color }} />
                <span className="truncate font-medium text-foreground">{meta.label}</span>
              </div>
              <span className="shrink-0 tabular-nums text-muted-foreground">
                {loading ? '—' : formatPercent(platform.share_pct)}
              </span>
            </div>
            <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full transition-all"
                style={{ width: loading || total <= 0 ? '0%' : `${width}%`, backgroundColor: meta.color }}
              />
            </div>
            <div className="mt-1.5 text-xs font-semibold tabular-nums text-foreground">
              {loading ? '—' : formatCurrency(platform.total_amount, true)}
            </div>
          </div>
        )
      })}
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

function PlatformTrendTooltip({ active, payload, label }: { active?: boolean; payload?: Array<{ name?: string; value?: number; dataKey?: string }>; label?: string }) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-md">
      <div className="mb-1 font-medium text-popover-foreground">{formatTrendDate(String(label ?? ''))}</div>
      <div className="space-y-1">
        {payload.map((item) => (
          <div key={item.dataKey} className="flex min-w-[180px] items-center justify-between gap-4">
            <span className="text-muted-foreground">{platformLegendLabel(String(item.name ?? item.dataKey ?? ''))}</span>
            <span className="font-medium tabular-nums text-foreground">{formatCurrency(Number(item.value ?? 0), true)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function platformStats(stats: DashboardStats | null): PlatformSalesStat[] {
  const byPlatform = new Map<PlatformKey, PlatformSalesStat>()
  for (const item of stats?.platform_sales ?? []) {
    byPlatform.set(item.platform, item)
  }
  return PLATFORM_ORDER.map((platform) => {
    const meta = PLATFORM_META[platform]
    return byPlatform.get(platform) ?? {
      platform,
      label: meta.label,
      total_amount: 0,
      today_amount: 0,
      order_count: 0,
      sent_count: 0,
      pending_count: 0,
      needs_review_count: 0,
      failed_count: 0,
      share_pct: 0,
    }
  })
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

function formatPercent(value: number): string {
  return `${Number(value || 0).toLocaleString('th-TH', { maximumFractionDigits: 1 })}%`
}

function formatShortDate(value: string): string {
  const [year, month, day] = value.split('-')
  if (!year || !month || !day) return value
  return `${day}/${month}/${year.slice(2)}`
}

function formatTrendDate(value: string): string {
  const [year, month, day] = value.split('-')
  if (!year || !month || !day) return value
  return `${day}/${month}`
}

function platformLegendLabel(value: string): string {
  if (value === 'shopee_amount') return 'Shopee'
  if (value === 'lazada_amount') return 'Lazada'
  if (value === 'tiktok_amount') return 'TikTok'
  return value
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
