import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, BarChart3, CheckCircle2, ListChecks, ReceiptText } from 'lucide-react'
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
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_SHOPEE_REALTIME_OPS } from '@/lib/featureFlags'
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
  iconBoxClass?: string
  iconClass?: string
  color: string
  softClass: string
  to: string
}

const PLATFORM_META: Record<PlatformKey, PlatformMeta> = {
  shopee: {
    key: 'shopee',
    label: 'Shopee',
    icon: '/shopee-logo.svg',
    color: '#ee4d2d',
    softClass: 'bg-[#fff1eb] text-[#9f2f16] border-[#f3c4b6]',
    to: ENABLE_SHOPEE_REALTIME_OPS ? '/shopee-operations' : '/import/shopee',
  },
  lazada: {
    key: 'lazada',
    label: 'Lazada',
    icon: '/lazada.svg',
    iconBoxClass: 'w-16',
    iconClass: 'max-h-5 max-w-14',
    color: '#1d3491',
    softClass: 'bg-[#e5ebff] text-link border-[#cbd7ff]',
    to: '/import/lazada',
  },
  tiktok: {
    key: 'tiktok',
    label: 'TikTok',
    icon: '/tiktok.svg',
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

  useEffect(() => {
    Promise.all([
      client
        .get<DashboardStats>('/api/dashboard/stats')
        .then((r) => {
          setStats(r.data)
          setStatsError(false)
        })
        .catch(() => {
          setStatsError(true)
          setStats(null)
        }),
      client
        .get<SetupStatus>('/api/setup/status')
        .then((r) => setSetupStatus(r.data))
        .catch(() => null),
    ]).finally(() => setLoading(false))
  }, [])

  const smlSetupIssue = setupStatus?.steps?.find((step) => step.key === 'instance' && !step.ready)

  return (
    <div className="space-y-4">
      <PageHeader
        title="ยอดขายตามแพลตฟอร์ม"
        description="ยอดเอกสารขายเดือนนี้จาก Shopee, Lazada และ TikTok พร้อมงานที่ต้องจัดการต่อ"
      />

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

      <PlatformSalesOverview stats={stats} loading={loading} error={statsError} />
      <PlatformSalesTrendCard stats={stats} loading={loading} error={statsError} />
      <ActionCenter stats={stats} setupStatus={setupStatus} loading={loading} />
    </div>
  )
}

function PlatformSalesOverview({
  stats,
  loading,
  error,
}: {
  stats: DashboardStats | null
  loading: boolean
  error: boolean
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
        <CardContent className="p-4">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary" className="gap-1 rounded-full px-2.5">
              <ReceiptText className="h-3.5 w-3.5" />
              ยอดเอกสาร Nexflow
            </Badge>
            <span className="text-xs text-muted-foreground">
              {meta ? `${formatShortDate(meta.from_date)} - ${formatShortDate(meta.to_date)} · ${meta.timezone}` : 'เดือนนี้ถึงวันนี้'}
            </span>
          </div>

          <div className="mt-3 grid gap-3 lg:grid-cols-[minmax(0,1fr)_420px] lg:items-end">
            <div className="min-w-0">
              <div className="text-sm font-medium text-muted-foreground">ยอดขายใน Nexflow เดือนนี้</div>
              <div className="mt-1 text-3xl font-semibold leading-tight tracking-normal text-foreground">
                {loading ? '—' : formatCurrency(total)}
              </div>
              <p className="mt-2 max-w-3xl text-xs leading-5 text-muted-foreground">
                ยอดจากเอกสารขายใน Nexflow ไม่ใช่ยอดรับชำระหรือ payout จากแพลตฟอร์ม
              </p>
            </div>
            <div className="grid grid-cols-3 gap-2">
              <HeroMetric label="วันนี้" value={loading ? '—' : formatCurrency(today, true)} />
              <HeroMetric label="ออเดอร์" value={loading ? '—' : formatCount(orders)} />
              <HeroMetric label="ต้องจัดการ" value={loading ? '—' : formatCount(riskCount)} />
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
            <span className={`flex h-10 shrink-0 items-center justify-center rounded-md border border-border bg-background ${meta.iconBoxClass ?? 'w-10'}`}>
              <img src={meta.icon} alt="" className={`${meta.iconClass ?? 'max-h-6 max-w-7'} object-contain`} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">{meta.label}</div>
              <div className="mt-0.5 text-xs text-muted-foreground">เดือนนี้</div>
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
          <MiniMetric label="วันนี้" value={loading ? '—' : formatCurrency(stat.today_amount, true)} />
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
          ยอดรายวันเดือนนี้
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <div className="h-[260px] rounded-md bg-muted/35" />
        ) : error ? (
          <ChartEmptyState title="โหลดกราฟไม่ได้" description="ลองรีเฟรช หรือเปิดรายการจากเมนูแพลตฟอร์ม" />
        ) : !hasSales ? (
          <ChartEmptyState title="ยังไม่มียอดขายในเดือนนี้" description="เมื่อมีเอกสารจากแพลตฟอร์ม กราฟจะแสดงยอดรายวัน" />
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
          Failed และต้องตรวจยังรวมในยอดเอกสาร เพื่อให้เห็นยอดที่เข้าระบบพร้อมงานที่ต้องแก้
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

function ActionCenter({
  stats,
  setupStatus,
  loading,
}: {
  stats: DashboardStats | null
  setupStatus: SetupStatus | null
  loading: boolean
}) {
  const purchaseNeedsReview = stats?.purchase_needs_review ?? 0
  const purchasePending = stats?.purchase_pending ?? 0
  const purchaseFailed = stats?.purchase_failed ?? 0
  const salesNeedsReview = stats?.sales_needs_review ?? 0
  const salesPending = stats?.sales_pending ?? 0
  const salesFailed = stats?.sales_failed ?? 0
  const emailErrors = stats?.email_inbox_errors ?? 0
  const totalBills = stats?.total_bills ?? 0
  const coreSetupIssue = setupStatus?.steps?.find((step) => step.key === 'instance' && !step.ready)

  const actions: Array<{
    title: string
    desc: string
    to: string
    cta: string
    tone: 'danger' | 'warning' | 'primary'
  }> = []

  if (coreSetupIssue) {
    actions.push({
      title: 'ระบบหลักยังไม่พร้อม',
      desc: coreSetupIssue.status,
      to: '/setup',
      cta: 'ตรวจ setup',
      tone: 'warning',
    })
  }
  if (emailErrors > 0) {
    actions.push({
      title: 'กล่องอีเมลมีปัญหา',
      desc: `${formatCount(emailErrors)} กล่องต้องตรวจ`,
      to: '/settings/email',
      cta: 'ตรวจ email',
      tone: 'danger',
    })
  }
  if (purchaseFailed + salesFailed > 0) {
    actions.push({
      title: 'เอกสารส่ง SML ไม่สำเร็จ',
      desc: `ซื้อ ${formatCount(purchaseFailed)} · ขาย ${formatCount(salesFailed)}`,
      to: salesFailed > 0 && ENABLE_SALES_ORDERS ? '/sales-orders?status=failed' : '/bills?status=failed',
      cta: 'แก้รายการ',
      tone: 'danger',
    })
  }
  if (purchaseNeedsReview + salesNeedsReview > 0) {
    actions.push({
      title: 'ต้องตรวจสินค้า/ข้อมูล',
      desc: `ซื้อ ${formatCount(purchaseNeedsReview)} · ขาย ${formatCount(salesNeedsReview)}`,
      to: '/mappings',
      cta: 'ไปตรวจ',
      tone: 'warning',
    })
  }
  if (purchasePending + salesPending > 0) {
    actions.push({
      title: 'พร้อมส่งเข้า SML',
      desc: `ซื้อ ${formatCount(purchasePending)} · ขาย ${formatCount(salesPending)}`,
      to: salesPending > 0 && ENABLE_SALES_ORDERS ? '/sale-invoices?status=pending' : '/bills?status=pending',
      cta: 'ไปส่ง',
      tone: 'primary',
    })
  }
  if (!loading && setupStatus?.ready && totalBills === 0) {
    actions.push({
      title: 'เริ่มนำเข้าข้อมูล',
      desc: 'ยังไม่มีเอกสารในระบบ',
      to: ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS ? '/import/shopee' : '/settings/email',
      cta: 'เริ่มงานแรก',
      tone: 'primary',
    })
  }

  const visible = actions.slice(0, 3)

  return (
    <Card className="rounded-lg border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <ListChecks className="h-4 w-4 text-accent-strong" />
          งานที่ต้องจัดการต่อ
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {loading ? (
          <div className="grid gap-2 md:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="h-16 rounded-md border border-border bg-muted/30" />
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="flex items-start gap-2 rounded-md border border-success/25 bg-success/[0.06] px-3 py-2 text-xs">
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
            <div>
              <div className="font-medium text-foreground">ไม่มีงานเร่งด่วน</div>
              <div className="mt-0.5 text-muted-foreground">ไม่มีค้างตรวจ, ค้างส่ง, ส่งไม่สำเร็จ หรือกล่องอีเมลมีปัญหา</div>
            </div>
          </div>
        ) : (
          <div className="grid gap-2 md:grid-cols-3">
            {visible.map((action, index) => (
              <ActionCenterItem key={action.title} index={index + 1} {...action} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ActionCenterItem({
  index,
  title,
  desc,
  to,
  cta,
  tone,
}: {
  index: number
  title: string
  desc: string
  to: string
  cta: string
  tone: 'danger' | 'warning' | 'primary'
}) {
  const toneCls = {
    danger: 'border-destructive/30 bg-destructive/[0.05] text-destructive',
    warning: 'border-warning/35 bg-warning/[0.06] text-warning',
    primary: 'border-primary/25 bg-primary/[0.04] text-accent-strong',
  }[tone]
  return (
    <Link to={to} className="group block rounded-md border border-border bg-card px-3 py-2.5 transition-colors hover:bg-accent/55">
      <div className="flex items-start gap-3">
        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold ${toneCls}`}>
          {index}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-foreground">{title}</div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">{desc}</div>
          <div className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-link">
            {cta}
            <ArrowRight className="h-3 w-3 transition-transform group-hover:translate-x-0.5" />
          </div>
        </div>
      </div>
    </Link>
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
