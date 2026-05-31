import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, CheckCircle2, Clock3, Copy, Download, FileText, ListChecks, Mail, Send, ShoppingBag, Sparkles, TrendingUp } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import InsightCard from '@/components/InsightCard'
import LearningProgress from '@/components/LearningProgress'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { useAuthStore } from '@/store/auth'
import { ENABLE_LAZADA_EXCEL, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_TIKTOK_EXCEL } from '@/lib/featureFlags'
import type { DailyInsight, DashboardStats, MappingStats } from '@/types'
import { ActionCards } from './Dashboard/ActionCards'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

type SetupStatus = {
  ready: boolean
  ready_count: number
  total_count: number
  steps?: { key: string; ready: boolean; status: string }[]
}

export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [insight, setInsight] = useState<DailyInsight | null>(null)
  const [mapStats, setMapStats] = useState<MappingStats | null>(null)
  const [loading, setLoading] = useState(true)
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
        .then((r) => setStats(r.data))
        .catch(() => null),
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

  const awaitingReview = (stats?.pending ?? 0) + (stats?.needs_review ?? 0)
  const smlSetupIssue = setupStatus?.steps?.find((step) => step.key === 'instance' && !step.ready)

  return (
    <div className="space-y-5">
      <PageHeader
        title="Nexflow Review Desk"
        description={ENABLE_SALES_ORDERS ? 'โต๊ะงานสำหรับตรวจเอกสารจากทุกช่องทาง แล้วส่งเป็นใบสั่งซื้อ/ใบสั่งขายเข้า SML' : 'โต๊ะงานสำหรับตรวจบิลซื้อจากอีเมล แล้วส่งเป็นใบสั่งซื้อเข้า SML'}
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
        <Card className="border-warning/35 bg-warning/[0.07]">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
              <div>
                <p className="text-sm font-semibold">ระบบยังตั้งค่าไม่ครบ</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {smlSetupIssue
                    ? `SML ยังไม่พร้อม: ${smlSetupIssue.status}`
                    : `พร้อมแล้ว ${setupStatus.ready_count}/${setupStatus.total_count} ขั้น กรุณาตรวจหน้าเริ่มต้นใช้งานก่อนเริ่มรับบิลจริง`}
                </p>
              </div>
            </div>
            <Button asChild size="sm">
              <Link to="/setup">ไปที่เริ่มต้นใช้งาน</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      {setupStatus?.ready && !loading && (stats?.total_bills ?? 0) === 0 && (
        <Card className="border-primary/25 bg-primary/[0.04]">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-2.5">
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
              <div>
                <p className="text-sm font-semibold">ระบบพร้อมแล้ว แต่ยังไม่มีเอกสาร</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  เริ่มจากนำเข้าไฟล์ Marketplace หรือดึงอีเมลรับบิลเพื่อสร้างเอกสารเข้าคิวตรวจ
                </p>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS && (
                <Button asChild size="sm">
                  <Link to="/import/shopee">เปิด Shopee</Link>
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

      <ActionCenter stats={stats} setupStatus={setupStatus} loading={loading} />

      <PilotResultCard stats={stats} loading={loading} />

      <Card className="overflow-hidden rounded-2xl border-border/70 bg-card shadow-sm">
        <CardContent className="grid gap-0 p-0 lg:grid-cols-[1.15fr_0.85fr]">
          <div className="border-b border-border/70 p-5 lg:border-b-0 lg:border-r">
            <div className="mb-4 flex items-center justify-between gap-3">
              <div>
                <p className="text-xs font-semibold uppercase tracking-wide text-primary">
                  งานที่ต้องทำตอนนี้
                </p>
                <h2 className="mt-1 text-xl font-semibold tracking-tight">
                  ตรวจคิวเอกสารตามช่องทางให้จบในที่เดียว
                </h2>
              </div>
              <div className="rounded-full bg-primary/10 px-3 py-1 text-xs font-medium text-primary">
                Multi-channel
              </div>
            </div>
            <ActionCards stats={stats} loading={loading} />
          </div>
          <div className="grid grid-cols-2 gap-px bg-border/70">
            <DeskMetric
              label="บิลในระบบ"
              value={stats?.total_bills ?? 0}
              icon={FileText}
              loading={loading}
            />
            <DeskMetric
              label="ต้องจัดการ"
              value={awaitingReview}
              icon={AlertTriangle}
              tone="warning"
              loading={loading}
            />
            <DeskMetric
              label="ส่งแล้ว"
              value={stats?.sml_success ?? 0}
              icon={CheckCircle2}
              tone="success"
              loading={loading}
            />
            <DeskMetric
              label="อีเมลมีปัญหา"
              value={stats?.email_inbox_errors ?? 0}
              icon={Mail}
              tone="danger"
              loading={loading}
            />
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_360px]">
        <Card className="rounded-2xl border-border/70 shadow-sm">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Send className="h-4 w-4 text-primary" />
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

  const actions: Array<{
    title: string
    desc: string
    to: string
    cta: string
    tone: 'danger' | 'warning' | 'primary' | 'success'
  }> = []

  if (setupStatus && !setupStatus.ready) {
    actions.push({
      title: 'ตั้งค่าระบบให้ครบก่อนรับงานจริง',
      desc: `พร้อมแล้ว ${setupStatus.ready_count}/${setupStatus.total_count} ขั้น ตรวจ SML, email, สินค้า และ AI ให้ครบ`,
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
    <Card className="rounded-2xl border-border/70 shadow-sm">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <ListChecks className="h-4 w-4 text-primary" />
          Action Center
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
    <Card className="overflow-hidden rounded-2xl border-primary/20 bg-card shadow-sm">
      <CardHeader className="border-b border-border/70 bg-primary/[0.035] pb-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <TrendingUp className="h-4 w-4 text-primary" />
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
              <Link to="/mappings" className="inline-flex items-center gap-1 font-medium text-primary">
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
    primary: 'bg-primary/10 text-primary',
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
    primary: 'border-primary/25 bg-primary/[0.04] text-primary',
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
          <div className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-primary">
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
    primary: 'text-primary bg-primary/10',
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
    <div className="rounded-xl border border-border/70 bg-muted/25 p-4">
      <div className="mb-3 flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <Icon className="h-4 w-4" />
      </div>
      <p className="text-sm font-semibold">{title}</p>
      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{desc}</p>
    </div>
  )
}
