import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, BarChart3, CheckCircle2, Clock3, Copy, Download, FileText, ListChecks, ReceiptText, Send, ShoppingBag, Sparkles, TrendingUp } from 'lucide-react'
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
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import InsightCard from '@/components/InsightCard'
import LearningProgress from '@/components/LearningProgress'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { useAuthStore } from '@/store/auth'
import { ENABLE_LAZADA_EXCEL, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_SHOPEE_REALTIME_OPS, ENABLE_TIKTOK_EXCEL } from '@/lib/featureFlags'
import type { DailyInsight, DashboardStats, MappingStats, PlatformKey, PlatformSalesStat, PlatformSalesTrendPoint } from '@/types'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

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
    color: '#111817',
    softClass: 'bg-muted text-foreground border-border',
    to: '/import/tiktok',
  },
}

const PLATFORM_ORDER: PlatformKey[] = ['shopee', 'lazada', 'tiktok']

export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [insight, setInsight] = useState<DailyInsight | null>(null)
  const [mapStats, setMapStats] = useState<MappingStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [statsError, setStatsError] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null)
  const user = useAuthStore((s) => s.user)

  const loadInsight = () =>
    client
      .get<{ data: DailyInsight[] }>('/api/dashboard/insights')
      .then((r) => setInsight(r.data.data?.[0] ?? null))
      .catch(() => null)

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
      loadInsight(),
      client
        .get<MappingStats>('/api/mappings/stats')
        .then((r) => setMapStats(r.data))
        .catch(() => null),
      client
        .get<SetupStatus>('/api/setup/status')
        .then((r) => setSetupStatus(r.data))
        .catch(() => null),
    ]).finally(() => setLoading(false))
  }, [])

  const handleGenerate = async () => {
    setGenerating(true)
    try {
      await client.post('/api/dashboard/insights/generate')
      await loadInsight()
      toast.success('สร้าง Insight สำเร็จ')
    } catch {
      toast.error('ไม่สามารถสร้าง Insight ได้')
    } finally {
      setGenerating(false)
    }
  }

  const smlSetupIssue = setupStatus?.steps?.find((step) => step.key === 'instance' && !step.ready)

  return (
    <div className="space-y-5">
      <PageHeader
        title="ยอดขายตามแพลตฟอร์ม"
        description="ดูยอดเอกสารขายจาก Shopee, Lazada และ TikTok ใน Nexflow พร้อมคิวที่ต้องจัดการต่อก่อนส่งหรือ reconcile"
        actions={
          PHASE >= 2 && user?.role === 'admin' && (
            <Button size="sm" onClick={handleGenerate} disabled={generating}>
              <Sparkles className="h-4 w-4" />
              {generating ? 'กำลังสร้าง…' : 'สร้าง AI Insight'}
            </Button>
          )
        }
      />

      {setupStatus && !setupStatus.ready && (
        <Card className={smlSetupIssue ? 'border-warning/35 bg-warning/[0.07]' : 'border-border/70 bg-muted/25'}>
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              {smlSetupIssue ? (
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
              ) : (
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
              )}
              <div>
                <p className="text-sm font-semibold">
                  {smlSetupIssue ? 'ระบบหลักยังต้องตรวจ' : 'ยอดขายหลักพร้อมดูได้ งานเสริมยังตั้งค่าไม่ครบ'}
                </p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {smlSetupIssue
                    ? `SML ยังไม่พร้อม: ${smlSetupIssue.status}`
                    : `พร้อมแล้ว ${setupStatus.ready_count}/${setupStatus.total_count} ขั้น ส่วนที่เหลือเป็นช่องทางหรือ readiness เสริม`}
                </p>
              </div>
            </div>
            <Button asChild size="sm" variant={smlSetupIssue ? 'default' : 'outline'}>
              <Link to="/setup">ไปที่เริ่มต้นใช้งาน</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      {setupStatus?.ready && !loading && (stats?.total_bills ?? 0) === 0 && (
        <Card className="border-primary/25 bg-primary/[0.04]">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-accent-strong" />
              <div>
                <p className="text-sm font-semibold">ระบบพร้อมแล้ว แต่ยังไม่มีเอกสาร</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  เริ่มจากนำเข้า Marketplace หรือดึงออเดอร์ Shopee เพื่อสร้าง baseline ยอดขายตามแพลตฟอร์ม
                </p>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS && (
                <Button asChild size="sm">
                  <Link to={ENABLE_SHOPEE_REALTIME_OPS ? '/shopee-operations' : '/import/shopee'}>
                    {ENABLE_SHOPEE_REALTIME_OPS ? 'คำสั่งซื้อ Shopee' : 'นำเข้า Shopee ย้อนหลัง'}
                  </Link>
                </Button>
              )}
              {ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS && (
                <Button asChild size="sm" variant="outline">
                  <Link to="/import/lazada">นำเข้า Lazada Excel</Link>
                </Button>
              )}
              {ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS && (
                <Button asChild size="sm" variant="outline">
                  <Link to="/import/tiktok">นำเข้า TikTok Excel</Link>
                </Button>
              )}
              <Button asChild size="sm" variant="outline">
                <Link to="/settings/email">ดึงอีเมลรับบิล</Link>
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <PlatformSalesOverview stats={stats} loading={loading} error={statsError} />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-4">
          <PlatformSalesTrendCard stats={stats} loading={loading} error={statsError} />
          <ActionCenter stats={stats} setupStatus={setupStatus} loading={loading} />
        </div>
        <div className="space-y-4">
          <PlatformShareCard stats={stats} loading={loading} error={statsError} />
          <ProductionStatusCard stats={stats} setupStatus={setupStatus} loading={loading} />
        </div>
      </div>

      <details className="group rounded-lg border border-border/70 bg-card shadow-sm">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-semibold">
          <span className="flex items-center gap-2">
            <TrendingUp className="h-4 w-4 text-accent-strong" />
            รายงานและ diagnostics
          </span>
          <span className="text-xs font-normal text-muted-foreground group-open:hidden">เปิดดูเมื่อต้องสรุปผลหรือจูนระบบ</span>
          <span className="hidden text-xs font-normal text-muted-foreground group-open:inline">ซ่อนรายละเอียด</span>
        </summary>
        <div className="space-y-4 border-t border-border/70 p-4">
          <PilotResultCard stats={stats} loading={loading} />
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_360px]">
            <Card className="rounded-lg border-border/70 shadow-sm">
              <CardHeader className="pb-3">
                <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                  <Send className="h-4 w-4 text-accent-strong" />
                  เส้นทางงานเอกสาร
                </CardTitle>
              </CardHeader>
              <CardContent className="grid gap-3 sm:grid-cols-2">
                <FlowStep
                  icon={FileText}
                  title="Email รับบิล"
                  desc="กล่องอีเมลรับบิล → ตรวจสินค้า → ใบสั่งซื้อหรือเอกสารขายตามเส้นทางที่ตั้งไว้"
                />
                {ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS && (
                  <FlowStep
                    icon={ShoppingBag}
                    title="Shopee"
                    desc="ดึงผ่าน Open API หรืออัปโหลด Excel → แยกตามปลายทางที่ตั้งไว้ → ใบสั่งขายหรือขายสินค้าและบริการ"
                  />
                )}
                {ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS && (
                  <FlowStep
                    icon={ShoppingBag}
                    title="Lazada Excel"
                    desc="นำเข้าไฟล์จาก Lazada Seller Center → ตรวจสินค้า → ใบสั่งขายหรือขายสินค้าและบริการ"
                  />
                )}
                {ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS && (
                  <FlowStep
                    icon={ShoppingBag}
                    title="TikTok Excel"
                    desc="นำเข้าไฟล์ Excel/CSV จาก TikTok Seller Center → ตรวจสินค้า → ใบสั่งขายหรือขายสินค้าและบริการ"
                  />
                )}
              </CardContent>
            </Card>

            <div className="space-y-4">
              <InsightCard insight={insight} />
              {mapStats && <LearningProgress stats={mapStats} />}
            </div>
          </div>
        </div>
      </details>
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
  const meta = stats?.platform_sales_meta

  return (
    <section className="space-y-4" aria-label="ยอดขายตามแพลตฟอร์ม">
      <Card className="overflow-hidden border-border/70 shadow-sm">
        <CardContent className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="secondary" className="gap-1 rounded-full px-2.5">
                <ReceiptText className="h-3.5 w-3.5" />
                ยอดเอกสาร Nexflow
              </Badge>
              <span className="text-xs text-muted-foreground">
                {meta ? `${formatShortDate(meta.from_date)} - ${formatShortDate(meta.to_date)} · ${meta.timezone}` : 'เดือนนี้ถึงวันนี้'}
              </span>
            </div>
            <div className="mt-3 flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
              <div>
                <div className="text-sm font-medium text-muted-foreground">ยอดขายใน Nexflow เดือนนี้</div>
                <div className="mt-1 text-3xl font-semibold leading-tight tracking-normal text-foreground">
                  {loading ? '—' : formatCurrency(total)}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:min-w-[420px]">
                <HeroMetric label="ยอดวันนี้" value={loading ? '—' : formatCurrency(today, true)} />
                <HeroMetric label="ออเดอร์เดือนนี้" value={loading ? '—' : orders.toLocaleString('th-TH')} />
                <HeroMetric label="แพลตฟอร์ม" value="3" />
              </div>
            </div>
            <p className="mt-3 max-w-3xl text-xs leading-5 text-muted-foreground">
              {meta?.definition ?? 'ยอดนี้มาจากเอกสารขายใน Nexflow ตามวันที่สร้างเอกสาร ไม่ใช่ยอดรับชำระจริงหรือ payout จากแพลตฟอร์ม'}
            </p>
          </div>
          <div className="flex flex-wrap gap-2 lg:justify-end">
            <Button asChild size="sm">
              <Link to={ENABLE_SHOPEE_REALTIME_OPS ? '/shopee-operations' : '/import/shopee'}>
                ดูคำสั่งซื้อ Shopee
              </Link>
            </Button>
            <Button asChild size="sm" variant="outline">
              <Link to="/sale-invoices">ดูเอกสารขาย</Link>
            </Button>
          </div>
        </CardContent>
      </Card>

      {error && (
        <Card className="border-warning/35 bg-warning/[0.07] shadow-sm">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
              <div>
                <p className="text-sm font-semibold">โหลดตัวเลข dashboard ไม่สำเร็จ</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  ยังเข้าเมนูหลักได้ตามปกติ ลองรีเฟรชหรือเปิดหน้าคำสั่งซื้อเพื่อดูรายการล่าสุด
                </p>
              </div>
            </div>
            <Button asChild size="sm" variant="outline">
              <Link to="/shopee-operations">เปิดคำสั่งซื้อ</Link>
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
      className="group flex min-h-[196px] flex-col justify-between rounded-lg border border-border bg-card p-4 shadow-sm transition-colors hover:border-primary/50 hover:bg-background/80"
    >
      <div>
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className={`flex h-10 shrink-0 items-center justify-center rounded-md border border-border bg-background ${meta.iconBoxClass ?? 'w-10'}`}>
              <img src={meta.icon} alt="" className={`${meta.iconClass ?? 'max-h-6 max-w-7'} object-contain`} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">{meta.label}</div>
              <div className="mt-0.5 text-xs text-muted-foreground">ยอดเอกสารเดือนนี้</div>
            </div>
          </div>
          <Badge variant="outline" className={meta.softClass}>
            {loading ? '—' : `${formatPercent(stat.share_pct)} share`}
          </Badge>
        </div>

        <div className="mt-4 text-2xl font-semibold tabular-nums text-foreground">
          {loading ? '—' : formatCurrency(stat.total_amount)}
        </div>
        <div className="mt-2 grid grid-cols-2 gap-2">
          <MiniMetric label="วันนี้" value={loading ? '—' : formatCurrency(stat.today_amount, true)} />
          <MiniMetric label="ออเดอร์" value={loading ? '—' : stat.order_count.toLocaleString('th-TH')} />
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-border/70 pt-3">
        <div className="flex flex-wrap gap-1.5">
          {riskCount > 0 ? (
            <>
              {stat.needs_review_count > 0 && (
                <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/15">
                  ต้องตรวจ {stat.needs_review_count.toLocaleString('th-TH')}
                </Badge>
              )}
              {stat.failed_count > 0 && (
                <Badge variant="destructive">
                  ล้มเหลว {stat.failed_count.toLocaleString('th-TH')}
                </Badge>
              )}
            </>
          ) : (
            <Badge variant="secondary" className="bg-success/10 text-foreground hover:bg-success/10">
              ไม่มี risk ค้าง
            </Badge>
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
  const hasSales = data.some((point) => point.shopee_amount > 0 || point.lazada_amount > 0 || point.tiktok_amount > 0)

  return (
    <Card className="rounded-lg border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <BarChart3 className="h-4 w-4 text-accent-strong" />
          ยอดขายรายวันเดือนนี้ตามแพลตฟอร์ม
        </CardTitle>
        <p className="text-xs leading-5 text-muted-foreground">
          ใช้วันที่สร้างเอกสารใน Nexflow เพื่อดูจังหวะยอดขายที่เข้าระบบ ไม่ใช่ยอดเงินรับชำระ
        </p>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="h-[260px] rounded-md bg-muted/35" />
        ) : error ? (
          <ChartEmptyState title="โหลดกราฟไม่ได้" description="ยังดูรายการจากเมนูออเดอร์และเอกสารได้ตามปกติ" />
        ) : !hasSales ? (
          <ChartEmptyState title="ยังไม่มียอดขายในเดือนนี้" description="เมื่อมีเอกสารจาก Shopee, Lazada หรือ TikTok กราฟจะแสดงยอดรายวันทันที" />
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
      </CardContent>
    </Card>
  )
}

function ChartEmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex min-h-[260px] items-center justify-center rounded-md border border-dashed border-border bg-muted/20 px-4 text-center">
      <div>
        <div className="text-sm font-semibold text-foreground">{title}</div>
        <div className="mt-1 max-w-md text-xs leading-5 text-muted-foreground">{description}</div>
      </div>
    </div>
  )
}

function PlatformShareCard({
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

  return (
    <Card className="rounded-lg border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <TrendingUp className="h-4 w-4 text-accent-strong" />
          สัดส่วนยอดเดือนนี้
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {error ? (
          <div className="rounded-md border border-warning/30 bg-warning/[0.06] px-3 py-2 text-xs text-muted-foreground">
            โหลดสัดส่วนไม่ได้ในตอนนี้
          </div>
        ) : (
          platforms.map((platform) => {
            const meta = PLATFORM_META[platform.platform]
            const width = Math.max(0, Math.min(100, platform.share_pct))
            return (
              <div key={platform.platform} className="space-y-1.5">
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: meta.color }} />
                    <span className="truncate font-medium text-foreground">{meta.label}</span>
                  </div>
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {loading ? '—' : `${formatPercent(platform.share_pct)} · ${formatCurrency(platform.total_amount, true)}`}
                  </span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full transition-all"
                    style={{ width: loading || total <= 0 ? '0%' : `${width}%`, backgroundColor: meta.color }}
                  />
                </div>
              </div>
            )
          })
        )}
        <div className="rounded-md border border-border/70 bg-muted/25 px-3 py-2 text-xs leading-5 text-muted-foreground">
          Failed และต้องตรวจยังรวมในยอดเอกสาร เพื่อให้เห็นยอดขายที่เข้าระบบพร้อม risk ที่ต้องจัดการต่อ
        </div>
      </CardContent>
    </Card>
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

function ProductionStatusCard({
  stats,
  setupStatus,
  loading,
}: {
  stats: DashboardStats | null
  setupStatus: SetupStatus | null
  loading: boolean
}) {
  const failed = (stats?.purchase_failed ?? 0) + (stats?.sales_failed ?? 0)
  const needsReview = (stats?.purchase_needs_review ?? 0) + (stats?.sales_needs_review ?? 0)
  const pending = (stats?.purchase_pending ?? 0) + (stats?.sales_pending ?? 0)
  const optionalSetupMissing = Boolean(setupStatus && !setupStatus.ready && !setupStatus.steps?.some((step) => step.key === 'instance' && !step.ready))

  return (
    <Card className="border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <CheckCircle2 className="h-4 w-4 text-success" />
          สถานะใช้งานจริง
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-2 gap-2">
          <DeskMetric label="บิลทั้งหมด" value={stats?.total_bills ?? 0} icon={FileText} loading={loading} />
          <DeskMetric label="ส่ง SML แล้ว" value={stats?.sml_success ?? 0} icon={CheckCircle2} tone="success" loading={loading} />
          <DeskMetric label="ต้องตรวจ" value={needsReview} icon={AlertTriangle} tone={needsReview > 0 ? 'warning' : 'success'} loading={loading} />
          <DeskMetric label="ส่งไม่สำเร็จ" value={failed} icon={AlertTriangle} tone={failed > 0 ? 'danger' : 'success'} loading={loading} />
        </div>
        <div className="rounded-md border border-border/70 bg-muted/25 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
          {failed > 0
            ? 'มีเอกสารส่ง SML ไม่สำเร็จ ให้เปิด Logs หรือหน้าบิลเพื่อดู error ก่อน retry'
            : pending > 0
              ? `มีเอกสารพร้อมส่ง ${pending.toLocaleString()} ใบ ต้องตรวจ dialog ก่อนยืนยันส่งจริง`
              : optionalSetupMissing
                ? 'งานขายหลักใช้งานได้ ส่วนช่องทางเสริมหรือ readiness บางข้อยังตั้งค่าไม่ครบ'
                : 'ตอนนี้ไม่มีคิวเร่งด่วนสำหรับงานขายหลัก'}
        </div>
        <div className="grid gap-2">
          <Button asChild size="sm">
            <Link to={ENABLE_SHOPEE_REALTIME_OPS ? '/shopee-operations' : '/import/shopee'}>
              {ENABLE_SHOPEE_REALTIME_OPS ? 'คำสั่งซื้อ Shopee' : 'นำเข้า Shopee ย้อนหลัง'}
            </Link>
          </Button>
          <Button asChild size="sm" variant="outline">
            <Link to="/sale-invoices">ดูขายสินค้าและบริการ</Link>
          </Button>
          <Button asChild size="sm" variant="outline">
            <Link to="/logs">ดู Logs</Link>
          </Button>
        </div>
      </CardContent>
    </Card>
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
    tone: 'danger' | 'warning' | 'primary' | 'success'
  }> = []

  if (coreSetupIssue) {
    actions.push({
      title: 'ระบบหลักยังต้องตรวจ',
      desc: `SML หรือ instance ยังไม่พร้อม: ${coreSetupIssue.status}`,
      to: '/setup',
      cta: 'ตรวจ setup',
      tone: 'warning',
    })
  }
  if (emailErrors > 0) {
    actions.push({
      title: 'กล่องอีเมลมีปัญหา',
      desc: `${emailErrors} กล่องต้องตรวจ ดูรอบล่าสุดว่าข้ามเพราะผู้ส่ง, password, IMAP หรือหัวข้อไม่ตรง`,
      to: '/settings/email',
      cta: 'ตรวจ email',
      tone: 'danger',
    })
  }
  if (purchaseFailed + salesFailed > 0) {
    actions.push({
      title: 'มีเอกสารส่ง SML ไม่สำเร็จ',
      desc: `${purchaseFailed} ใบสั่งซื้อ · ${salesFailed} งานขาย เปิดดู error และ retry จากบิลที่มีปัญหา`,
      to: salesFailed > 0 && ENABLE_SALES_ORDERS ? '/sales-orders?status=failed' : '/bills?status=failed',
      cta: 'แก้รายการ fail',
      tone: 'danger',
    })
  }
  if (purchaseNeedsReview + salesNeedsReview > 0) {
    actions.push({
      title: 'จับคู่สินค้าที่ค้างก่อนส่ง',
      desc: `${purchaseNeedsReview} บิลซื้อ · ${salesNeedsReview} งานขาย ใช้หน้า mapping ดูชื่อที่ซ้ำและแก้ครั้งเดียวให้ลดงานรอบถัดไป`,
      to: '/mappings',
      cta: 'ดูจุดที่ยังต้องจับคู่',
      tone: 'warning',
    })
  }
  if (purchasePending + salesPending > 0) {
    actions.push({
      title: 'มีเอกสารสถานะพร้อมส่งเข้า SML',
      desc: `${purchasePending} ใบสั่งซื้อ · ${salesPending} งานขาย ต้องตรวจ preflight ใน dialog ส่งก่อนยืนยัน`,
      to: salesPending > 0 && ENABLE_SALES_ORDERS ? '/sale-invoices?status=pending' : '/bills?status=pending',
      cta: 'ไปส่ง SML',
      tone: 'primary',
    })
  }
  if (!loading && setupStatus?.ready && totalBills === 0) {
    actions.push({
      title: 'ระบบพร้อมแล้ว เริ่มนำเข้าข้อมูลชุดแรก',
      desc: ENABLE_SALES_ORDERS ? 'เริ่มจาก Marketplace Excel หรือดึงอีเมลรับบิลเพื่อสร้างคิวตรวจ' : 'เริ่มจากตั้งค่ากล่องอีเมลแล้วดึงบิลซื้อ',
      to: ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS ? '/import/shopee' : '/settings/email',
      cta: 'เริ่มงานแรก',
      tone: 'primary',
    })
  }

  const visible = actions.slice(0, 4)

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
          <div className="grid gap-2 md:grid-cols-2">
            {Array.from({ length: 2 }).map((_, i) => (
              <div key={i} className="h-20 rounded-md border border-border bg-muted/30" />
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="flex items-start gap-2 rounded-md border border-success/25 bg-success/[0.06] px-3 py-2 text-xs">
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
            <div>
              <div className="font-medium text-foreground">วันนี้ยังไม่มีงานเร่งด่วน</div>
              <div className="mt-0.5 text-muted-foreground">ระบบไม่พบเอกสารค้างตรวจ, ค้างส่ง, ส่งไม่สำเร็จ หรือกล่องอีเมลมีปัญหา</div>
            </div>
          </div>
        ) : (
          <div className="grid gap-2 md:grid-cols-2">
            {visible.map((action, index) => (
              <ActionCenterItem key={action.title} index={index + 1} {...action} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
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

function PilotResultCard({
  stats,
  loading,
}: {
  stats: DashboardStats | null
  loading: boolean
}) {
  const total = stats?.pilot_30d_total ?? 0
  const sent = stats?.pilot_30d_sent ?? 0
  const needsReview = stats?.pilot_30d_needs_review ?? 0
  const pending = stats?.pilot_30d_pending ?? 0
  const failed = stats?.pilot_30d_failed ?? 0
  const remaining = stats?.pilot_30d_remaining ?? needsReview + pending + failed
  const successRate = stats?.pilot_30d_success_rate ?? 0
  const hoursSaved = stats?.pilot_30d_estimated_hours_saved ?? 0
  const successPct = Math.max(0, Math.min(100, successRate))
  const canExport = Boolean(stats) && !loading
  const summaryMarkdown = buildPilotSummaryMarkdown(stats)

  const handleCopySummary = async () => {
    if (!canExport) return
    try {
      await navigator.clipboard.writeText(summaryMarkdown)
      toast.success('คัดลอกสรุป Pilot แล้ว')
    } catch {
      downloadPilotSummary(summaryMarkdown)
      toast.success('คัดลอกไม่ได้ จึงดาวน์โหลดไฟล์แทน')
    }
  }

  const handleDownloadSummary = () => {
    if (!canExport) return
    downloadPilotSummary(summaryMarkdown)
    toast.success('ดาวน์โหลดสรุป Pilot แล้ว')
  }

  return (
    <Card className="overflow-hidden rounded-lg border-primary/20 bg-card shadow-sm">
      <CardHeader className="border-b border-border/70 bg-primary/[0.035] pb-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <TrendingUp className="h-4 w-4 text-accent-strong" />
              ผลลัพธ์ Pilot 30 วัน
            </CardTitle>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              ตัวเลขสำหรับคุยกับลูกค้า: ระบบรับบิลได้กี่ใบ ส่ง SML สำเร็จกี่ใบ และยังเหลืองานที่ต้องช่วยจูนตรงไหน
            </p>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Button size="sm" variant="outline" className="shrink-0" onClick={handleCopySummary} disabled={!canExport}>
              <Copy className="h-4 w-4" />
              คัดลอกสรุป
            </Button>
            <Button size="sm" variant="outline" className="shrink-0" onClick={handleDownloadSummary} disabled={!canExport}>
              <Download className="h-4 w-4" />
              ดาวน์โหลด .md
            </Button>
            <Button asChild size="sm" variant="outline" className="shrink-0">
              <Link to="/logs">ดูหลักฐานใน logs</Link>
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <div className="grid gap-px bg-border/70 md:grid-cols-4">
          <PilotMetric
            label="บิลที่เข้าระบบ"
            value={total}
            sub="30 วันล่าสุด"
            icon={FileText}
            loading={loading}
          />
          <PilotMetric
            label="ส่ง SML สำเร็จ"
            value={sent}
            sub={`${successPct.toFixed(total > 0 ? 1 : 0)}% ของบิลที่เข้า`}
            icon={CheckCircle2}
            tone="success"
            loading={loading}
          />
          <PilotMetric
            label="ยังต้องจัดการ"
            value={remaining}
            sub={`ตรวจ ${needsReview} · สถานะพร้อมส่ง ${pending} · fail ${failed}`}
            icon={AlertTriangle}
            tone={remaining > 0 ? 'warning' : 'success'}
            loading={loading}
          />
          <PilotMetric
            label="เวลาที่ประหยัดได้"
            value={formatPilotHours(hoursSaved)}
            sub="ประมาณ 4 นาทีต่อบิลที่ส่งสำเร็จ"
            icon={Clock3}
            tone="primary"
            loading={loading}
          />
        </div>
        <div className="space-y-2 p-4">
          <div className="flex items-center justify-between gap-3 text-xs">
            <span className="font-medium text-foreground">อัตราส่ง SML สำเร็จในช่วง Pilot</span>
            <span className="tabular-nums text-muted-foreground">
              {loading ? '—' : `${successPct.toFixed(total > 0 ? 1 : 0)}%`}
            </span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-primary transition-all"
              style={{ width: loading ? '0%' : `${successPct}%` }}
            />
          </div>
          <div className="flex flex-col gap-1 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
            <span>
              {total > 0
                ? `ส่งสำเร็จ ${sent.toLocaleString()} จาก ${total.toLocaleString()} บิลที่รับเข้าใน 30 วันล่าสุด`
                : 'ยังไม่มีบิลในช่วง 30 วันล่าสุด เริ่มจาก import หรือดึง email เพื่อสร้าง baseline'}
            </span>
            {remaining > 0 && (
              <Link to="/mappings" className="inline-flex items-center gap-1 font-medium text-link">
                ลดงานค้างด้วย mapping
                <ArrowRight className="h-3 w-3" />
              </Link>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function PilotMetric({
  label,
  value,
  sub,
  icon: Icon,
  tone = 'primary',
  loading,
}: {
  label: string
  value: number | string
  sub: string
  icon: typeof FileText
  tone?: 'primary' | 'warning' | 'success'
  loading: boolean
}) {
  const toneCls = {
    primary: 'bg-primary/10 text-accent-strong',
    warning: 'bg-warning/10 text-warning',
    success: 'bg-success/10 text-success',
  }[tone]
  return (
    <div className="bg-card p-4">
      <div className={`mb-3 flex h-9 w-9 items-center justify-center rounded-lg ${toneCls}`}>
        <Icon className="h-4 w-4" />
      </div>
      <div className="text-xl font-semibold tabular-nums text-foreground">
        {loading ? '—' : typeof value === 'number' ? value.toLocaleString() : value}
      </div>
      <div className="mt-1 text-xs font-medium text-foreground">{label}</div>
      <div className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{loading ? 'กำลังโหลด...' : sub}</div>
    </div>
  )
}

function formatPilotHours(hours: number) {
  if (!Number.isFinite(hours) || hours <= 0) return '0 ชม.'
  if (hours < 10) return `${hours.toFixed(1)} ชม.`
  return `${Math.round(hours).toLocaleString()} ชม.`
}

function buildPilotSummaryMarkdown(stats: DashboardStats | null) {
  const total = stats?.pilot_30d_total ?? 0
  const sent = stats?.pilot_30d_sent ?? 0
  const needsReview = stats?.pilot_30d_needs_review ?? 0
  const pending = stats?.pilot_30d_pending ?? 0
  const failed = stats?.pilot_30d_failed ?? 0
  const remaining = stats?.pilot_30d_remaining ?? needsReview + pending + failed
  const successRate = Math.max(0, Math.min(100, stats?.pilot_30d_success_rate ?? 0))
  const hoursSaved = stats?.pilot_30d_estimated_hours_saved ?? 0
  const generatedAt = new Date().toLocaleString('th-TH', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })

  return [
    '# Nexflow Pilot Summary',
    '',
    `วันที่สรุป: ${generatedAt}`,
    'ช่วงข้อมูล: 30 วันล่าสุด',
    '',
    '## ผลลัพธ์',
    `- บิลที่เข้าระบบ: ${total.toLocaleString()} ใบ`,
    `- ส่ง SML สำเร็จ: ${sent.toLocaleString()} ใบ (${successRate.toFixed(total > 0 ? 1 : 0)}%)`,
    `- เวลาที่ประหยัดได้โดยประมาณ: ${formatPilotHours(hoursSaved)}`,
    `- งานที่ยังต้องจัดการ: ${remaining.toLocaleString()} ใบ`,
    '',
    '## งานที่ยังต้องจูนต่อ',
    `- รอตรวจ mapping / ข้อมูลสินค้า: ${needsReview.toLocaleString()} ใบ`,
    `- เอกสารสถานะพร้อมส่ง SML แต่ยังไม่ได้ส่ง: ${pending.toLocaleString()} ใบ`,
    `- ส่ง SML ไม่สำเร็จและต้องแก้ error: ${failed.toLocaleString()} ใบ`,
    '',
    '## หมายเหตุ',
    '- เวลาที่ประหยัดได้คำนวณแบบ conservative ที่ 4 นาทีต่อบิลที่ส่ง SML สำเร็จ',
    '- หลักฐานเพิ่มเติมดูได้จากหน้า Logs, SML payload และ SML response ในระบบ Nexflow',
  ].join('\n')
}

function downloadPilotSummary(markdown: string) {
  const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `nexflow-pilot-summary-${new Date().toISOString().slice(0, 10)}.md`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
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
  tone: 'danger' | 'warning' | 'primary' | 'success'
}) {
  const toneCls = {
    danger: 'border-destructive/30 bg-destructive/[0.05] text-destructive',
    warning: 'border-warning/35 bg-warning/[0.06] text-warning',
    primary: 'border-primary/25 bg-primary/[0.04] text-accent-strong',
    success: 'border-success/25 bg-success/[0.05] text-success',
  }[tone]
  return (
    <Link to={to} className="group block rounded-md border border-border bg-card px-3 py-2.5 transition-colors hover:bg-accent/55">
      <div className="flex items-start gap-3">
        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold ${toneCls}`}>
          {index}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <div className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{desc}</div>
          <div className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-link">
            {cta}
            <ArrowRight className="h-3 w-3 transition-transform group-hover:translate-x-0.5" />
          </div>
        </div>
      </div>
    </Link>
  )
}

function DeskMetric({
  label,
  value,
  icon: Icon,
  tone = 'primary',
  loading,
}: {
  label: string
  value: number
  icon: typeof FileText
  tone?: 'primary' | 'warning' | 'success' | 'danger'
  loading: boolean
}) {
  const toneCls = {
    primary: 'text-accent-strong bg-primary/10',
    warning: 'text-warning bg-warning/10',
    success: 'text-success bg-success/10',
    danger: 'text-destructive bg-destructive/10',
  }[tone]
  return (
    <div className="bg-card p-5">
      <div className={`mb-4 flex h-9 w-9 items-center justify-center rounded-lg ${toneCls}`}>
        <Icon className="h-4 w-4" />
      </div>
      <p className="text-2xl font-semibold tabular-nums">{loading ? '—' : value.toLocaleString()}</p>
      <p className="mt-1 text-xs text-muted-foreground">{label}</p>
    </div>
  )
}

function FlowStep({
  icon: Icon,
  title,
  desc,
}: {
  icon: typeof FileText
  title: string
  desc: string
}) {
  return (
    <div className="rounded-lg border border-border/70 bg-muted/25 p-4">
      <div className="mb-3 flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-accent-strong">
        <Icon className="h-4 w-4" />
      </div>
      <p className="text-sm font-semibold">{title}</p>
      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{desc}</p>
    </div>
  )
}
