import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'
import {
  ArrowRight,
  CheckCircle2,
  CircleAlert,
  CircleDot,
  ClipboardCheck,
  Database,
  FileClock,
  History,
  Loader2,
  Mail,
  PackageCheck,
  RefreshCw,
  ReceiptText,
  RotateCcw,
  ServerCog,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Upload,
} from 'lucide-react'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { PageHeader } from '@/components/common/PageHeader'
import { cn } from '@/lib/utils'
import type { SMLReadiness } from '@/types'

type SetupStep = {
  key: string
  title: string
  description: string
  href: string
  ready: boolean
  status: string
  blocking?: boolean
  missing?: string[]
}

type SetupSystem = {
  instance_name: string
  instance_slug: string
  env: string
  sml_rest_url: string
  sml_database: string
  public_base_url: string
  openrouter_model: string
  pending_restart: boolean
  pending_restart_settings?: string[]
  last_catalog_sync?: string
  last_email_poll?: string
  last_import_run?: string
}

type SetupCounters = {
  total: number
  pending: number
  needs_review: number
  failed: number
  sent: number
  purchase: number
  saleorder: number
  saleinvoice: number
}

type ImportCounters = {
  shopee_runs: number
  shopee_running: number
  shopee_failed: number
  email_dedup_keys: number
  audit_logs: number
}

type SetupStatus = {
  ready: boolean
  ready_count: number
  total_count: number
  blocking_ready_count: number
  blocking_total_count: number
  pending_restart?: boolean
  pending_restart_settings?: string[]
  steps: SetupStep[]
  system: SetupSystem
  documents: SetupCounters
  imports: ImportCounters
  sml_readiness?: SMLReadiness
}

const iconByStep: Record<string, typeof ServerCog> = {
  instance: ServerCog,
  channels: FileClock,
  email: Mail,
  catalog: Database,
  ai: Sparkles,
  uat: ClipboardCheck,
  operations: ClipboardCheck,
}

function fmtDate(value?: string) {
  if (!value) return 'ยังไม่มีข้อมูล'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return new Intl.DateTimeFormat('th-TH', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(d)
}

function n(value?: number) {
  return new Intl.NumberFormat('th-TH').format(value ?? 0)
}

function StatTile({
  label,
  value,
  tone = 'default',
}: {
  label: string
  value: string | number
  tone?: 'default' | 'ok' | 'warn' | 'danger'
}) {
  return (
    <div
      className={cn(
        'rounded-md border border-border/70 px-3 py-2',
        tone === 'ok' && 'bg-success/[0.05]',
        tone === 'warn' && 'bg-warning/[0.06]',
        tone === 'danger' && 'bg-destructive/[0.05]',
      )}
    >
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div
        className={cn(
          'mt-1 text-lg font-semibold tabular-nums',
          tone === 'ok' && 'text-success',
          tone === 'warn' && 'text-warning',
          tone === 'danger' && 'text-destructive',
        )}
      >
        {value}
      </div>
    </div>
  )
}

function ReadinessTile({
  icon: Icon,
  label,
  value,
  detail,
  tone = 'default',
  to,
}: {
  icon: typeof ServerCog
  label: string
  value: string
  detail: string
  tone?: 'default' | 'ok' | 'warn' | 'danger'
  to?: string
}) {
  const inner = (
    <div
      className={cn(
        'h-full rounded-md border border-border/70 p-3 transition-colors',
        tone === 'ok' && 'border-success/25 bg-success/[0.04]',
        tone === 'warn' && 'border-warning/30 bg-warning/[0.06]',
        tone === 'danger' && 'border-destructive/25 bg-destructive/[0.05]',
        to && 'hover:bg-accent/35',
      )}
    >
      <div className="flex items-start gap-2.5">
        <Icon
          className={cn(
            'mt-0.5 h-4 w-4 shrink-0 text-muted-foreground',
            tone === 'ok' && 'text-success',
            tone === 'warn' && 'text-warning',
            tone === 'danger' && 'text-destructive',
          )}
        />
        <div className="min-w-0">
          <div className="text-[11px] text-muted-foreground">{label}</div>
          <div className="mt-0.5 truncate text-sm font-semibold text-foreground">{value}</div>
          <div className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">{detail}</div>
        </div>
      </div>
    </div>
  )

  return to ? <Link to={to}>{inner}</Link> : inner
}

export default function SetupCenter() {
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [resetOpen, setResetOpen] = useState(false)
  const [resetBusy, setResetBusy] = useState(false)
  const [confirmText, setConfirmText] = useState('')
  const [resetDocCounter, setResetDocCounter] = useState(false)
  const [resetEmailDedup, setResetEmailDedup] = useState(false)

  const load = async (forceSML = false) => {
    setLoading(true)
    try {
      const res = await client.get<SetupStatus>(forceSML ? '/api/setup/status?refresh_sml=1' : '/api/setup/status')
      setStatus(res.data)
    } catch (e: any) {
      toast.error('โหลดสถานะระบบไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const docs = status?.documents
  const imports = status?.imports
  const smlReadiness = status?.sml_readiness
  const workPending = (docs?.pending ?? 0) + (docs?.needs_review ?? 0) + (docs?.failed ?? 0)
  const isProduction = status?.system?.env === 'production'
  const catalogStep = status?.steps.find((step) => step.key === 'catalog')
  const primaryReady = Boolean(
    status &&
      !status.pending_restart &&
      smlReadiness?.ready &&
      (docs?.failed ?? 0) === 0,
  )
  const primaryIssues = useMemo(() => {
    if (!status) return ['กำลังตรวจสถานะระบบ']
    const issues: string[] = []
    if (status.pending_restart) issues.push('มีค่ารอ restart ก่อนใช้งานจริง')
    if (!smlReadiness?.ready) issues.push(smlReadiness?.message || 'ยังเชื่อมต่อ SML ไม่พร้อม')
    if ((docs?.failed ?? 0) > 0) issues.push(`มีเอกสารส่ง SML ไม่สำเร็จ ${n(docs?.failed)} ใบ`)
    if ((docs?.needs_review ?? 0) > 0) issues.push(`มีเอกสารต้องตรวจ ${n(docs?.needs_review)} ใบ`)
    return issues
  }, [docs?.failed, docs?.needs_review, smlReadiness?.message, smlReadiness?.ready, status])
  const primaryAction = (docs?.failed ?? 0) > 0
    ? { to: '/logs?level=error', label: 'ดูปัญหาใน Logs' }
    : (docs?.needs_review ?? 0) > 0
      ? { to: '/sale-invoices?status=needs_review', label: 'ตรวจเอกสาร' }
      : (docs?.pending ?? 0) > 0
        ? { to: '/sale-invoices?status=pending', label: 'ดูคิวพร้อมส่ง' }
        : { to: '/import/shopee', label: 'นำเข้า Shopee ย้อนหลัง' }
  const optionalSteps = (status?.steps ?? []).filter((step) => step.key !== 'instance' && step.key !== 'catalog' && step.key !== 'operations')

  const resetTestData = async () => {
    if (confirmText !== 'RESET') {
      toast.error('พิมพ์ RESET เพื่อยืนยัน')
      return
    }
    setResetBusy(true)
    const id = toast.loading('กำลังรีเซ็ตข้อมูลชั่วคราว...')
    try {
      await client.post('/api/setup/reset-test-data', {
        confirm: confirmText,
        reset_doc_counter: resetDocCounter,
        reset_email_dedup: resetEmailDedup,
      })
      toast.success(
        resetEmailDedup
          ? 'ล้างข้อมูลแล้ว ระบบจะอ่านอีเมลเก่าในรอบ poll ถัดไป'
          : 'รีเซ็ตข้อมูลชั่วคราวแล้ว',
        { id },
      )
      setResetOpen(false)
      setConfirmText('')
      setResetDocCounter(false)
      setResetEmailDedup(false)
      await load()
    } catch (e: any) {
      toast.error('รีเซ็ตข้อมูลไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'), { id })
    } finally {
      setResetBusy(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="เริ่มต้นใช้งาน"
        description="ตรวจความพร้อมร้านและสถานะระบบก่อนเริ่มใช้งานจริง"
        actions={
          <Button variant="outline" size="sm" onClick={() => load(true)} disabled={loading}>
            <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
            ตรวจใหม่
          </Button>
        }
      />

      <Card className={cn('border-border/70 shadow-sm', primaryReady ? 'bg-success/[0.045]' : 'bg-warning/[0.055]')}>
        <CardContent className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              {primaryReady ? (
                <CheckCircle2 className="h-5 w-5 text-success" />
              ) : (
                <CircleAlert className="h-5 w-5 text-warning" />
              )}
              <h2 className="text-base font-semibold">
                {primaryReady ? 'งานขายหลักพร้อมใช้งาน' : 'งานขายหลักยังต้องตรวจ'}
              </h2>
              <Badge variant="outline" className="bg-background text-xs">
                {'Shopee -> ขายสินค้าและบริการ -> SML'}
              </Badge>
              <Badge variant="outline" className="bg-background text-xs">
                {isProduction ? 'Production workspace' : status?.system?.env ?? 'กำลังโหลด'}
              </Badge>
            </div>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
              {primaryReady
                ? `SML พร้อมใช้งาน · ส่งขายสินค้าและบริการสำเร็จแล้ว ${n(docs?.saleinvoice)} ใบ · ไม่มีเอกสารที่ส่งไม่สำเร็จ`
                : primaryIssues.join(' · ')}
            </p>
            {status?.pending_restart && (
              <div className="mt-2 flex flex-wrap gap-1">
                {(status.pending_restart_settings ?? []).slice(0, 4).map((key) => (
                  <Badge key={key} variant="outline" className="h-5 px-1.5 text-[10px] text-warning">
                    รอเริ่มใช้ค่าใหม่: {key}
                  </Badge>
                ))}
              </div>
            )}
          </div>
          <div className="flex flex-wrap gap-2 lg:justify-end">
            <Button asChild size="sm">
              <Link to={primaryAction.to}>
                {primaryAction.label}
                <ArrowRight className="h-4 w-4" />
              </Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link to="/sale-invoices">ดูเอกสาร SI</Link>
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <ReadinessTile
          icon={Database}
          label="SML ERP"
          value={smlReadiness?.ready ? 'พร้อมใช้งาน' : smlReadiness?.configured ? 'เชื่อมต่อไม่ได้' : 'ยังไม่ได้ตั้งค่า'}
          detail={smlReadiness?.message || 'ตรวจฐานข้อมูลและ sml-api-byboss ก่อนส่งเอกสาร'}
          tone={smlReadiness?.ready ? 'ok' : 'danger'}
          to="/settings/instance"
        />
        <ReadinessTile
          icon={ReceiptText}
          label="ขายสินค้าและบริการ"
          value={`${n(docs?.saleinvoice)} ใบ`}
          detail={`ส่งแล้ว ${n(docs?.sent)} · พร้อมส่ง ${n(docs?.pending)} · ต้องตรวจ ${n(docs?.needs_review)}`}
          tone={(docs?.failed ?? 0) > 0 || (docs?.needs_review ?? 0) > 0 ? 'warn' : 'ok'}
          to="/sale-invoices"
        />
        <ReadinessTile
          icon={PackageCheck}
          label="สินค้าใน SML"
          value={catalogStep?.ready ? 'พร้อมจับคู่' : 'ควรซิงก์สินค้า'}
          detail={catalogStep?.status || 'ช่วยลด mapping ผิดและ user error ตอนส่ง'}
          tone={catalogStep?.ready ? 'ok' : 'warn'}
          to="/settings/catalog"
        />
        <ReadinessTile
          icon={ServerCog}
          label="ระบบ"
          value={status ? 'ตอบสนองปกติ' : 'กำลังโหลด'}
          detail={status?.system?.pending_restart ? 'มีค่ารอเริ่มใช้ใหม่' : `${status?.system?.instance_name ?? 'Nexflow'} · DB ${status?.system?.sml_database ?? '-'}`}
          tone={status?.system?.pending_restart ? 'warn' : 'ok'}
          to="/settings/instance"
        />
      </div>

      <Card className="shadow-sm">
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-semibold">งานที่ควรทำต่อ</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-3">
          <StatTile label="งานค้างวันนี้" value={n(workPending)} tone={workPending > 0 ? 'warn' : 'ok'} />
          <StatTile label="ส่งไม่สำเร็จ" value={n(docs?.failed)} tone={(docs?.failed ?? 0) > 0 ? 'danger' : 'ok'} />
          <StatTile label="ประวัติการทำงาน" value={n(imports?.audit_logs)} />
        </CardContent>
      </Card>

      <details className="group rounded-lg border border-border/70 bg-card shadow-sm">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-semibold">
          <span className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-accent-strong" />
            ช่องทางเสริมและรายละเอียดระบบ
          </span>
          <span className="text-xs font-normal text-muted-foreground group-open:hidden">เปิดดูเมื่อต้องตั้งค่าเพิ่ม</span>
          <span className="hidden text-xs font-normal text-muted-foreground group-open:inline">ซ่อนรายละเอียด</span>
        </summary>
        <div className="space-y-4 border-t border-border/70 p-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {optionalSteps.map((step, index) => {
              const Icon = iconByStep[step.key] ?? ClipboardCheck
              return (
                <div key={step.key} className={cn('rounded-md border border-border/70 p-3', step.ready && 'bg-success/[0.03]')}>
                  <div className="flex items-start gap-3">
                    <div
                      className={cn(
                        'flex h-8 w-8 shrink-0 items-center justify-center rounded-md border',
                        step.ready ? 'border-success/30 bg-success/10 text-success' : 'border-warning/30 bg-warning/10 text-warning',
                      )}
                    >
                      {step.ready ? <CheckCircle2 className="h-4 w-4" /> : <Icon className="h-4 w-4" />}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <p className="text-sm font-semibold">{index + 1}. {step.title}</p>
                        <Badge variant={step.ready ? 'default' : 'outline'} className="h-5 px-1.5 text-[10px]">
                          {step.status}
                        </Badge>
                        {step.blocking === false && (
                          <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                            เสริม
                          </Badge>
                        )}
                      </div>
                      <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground">{step.description}</p>
                      {!!step.missing?.length && (
                        <div className="mt-2 flex flex-wrap gap-1">
                          {step.missing.slice(0, 4).map((m) => (
                            <Badge key={m} variant="secondary" className="h-5 px-1.5 text-[10px]">
                              <CircleDot className="h-3 w-3" />
                              {m}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </div>
                    <Button asChild variant={step.ready ? 'outline' : 'default'} size="sm" className="shrink-0">
                      <Link to={step.href}>{step.ready ? 'ตรวจดู' : 'ไปตั้งค่า'}</Link>
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>

          <div className="grid gap-4 xl:grid-cols-2">
            <Card className="shadow-none">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                  <History className="h-4 w-4" />
                  สรุปข้อมูลระบบ
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-xs">
                <div className="grid grid-cols-3 gap-2">
                  <StatTile label="ใบสั่งซื้อ" value={n(docs?.purchase)} />
                  <StatTile label="ใบสั่งขาย" value={n(docs?.saleorder)} />
                  <StatTile label="ขายสินค้าฯ" value={n(docs?.saleinvoice)} />
                </div>
                <Separator />
                <InfoRow label="ร้าน" value={status?.system?.instance_name ?? 'Nexflow'} />
                <InfoRow label="รหัสร้าน" value={status?.system?.instance_slug ?? 'default'} />
                <InfoRow label="Public URL" value={status?.system?.public_base_url ?? '-'} />
                <InfoRow label="AI ที่ใช้งาน" value={status?.system?.openrouter_model ?? '-'} />
              </CardContent>
            </Card>

            <Card className="shadow-none">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                  <Upload className="h-4 w-4" />
                  การทำงานล่าสุด
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-xs">
                <InfoRow label="รอบนำเข้า Shopee" value={n(imports?.shopee_runs)} />
                <InfoRow label="กำลังนำเข้า / ไม่สำเร็จ" value={`${n(imports?.shopee_running)} / ${n(imports?.shopee_failed)}`} />
                <InfoRow label="อีเมลที่เคยอ่านแล้ว" value={n(imports?.email_dedup_keys)} />
                <Separator />
                <InfoRow label="ดึงสินค้า SML" value={fmtDate(status?.system?.last_catalog_sync)} />
                <InfoRow label="อ่านอีเมลล่าสุด" value={fmtDate(status?.system?.last_email_poll)} />
                <InfoRow label="นำเข้า Shopee ล่าสุด" value={fmtDate(status?.system?.last_import_run)} />
              </CardContent>
            </Card>
          </div>
        </div>
      </details>

      {status && !isProduction && (
        <Card className="border-destructive/25 bg-destructive/[0.03]">
          <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex min-w-0 items-start gap-3">
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
              <div className="min-w-0">
                <p className="text-sm font-semibold text-destructive">Maintenance danger zone</p>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                  ใช้เฉพาะ workspace ที่ไม่ใช่ production เมื่อต้องรีเซ็ตเอกสารนำเข้าและประวัติการทำงานเพื่อทดสอบรอบใหม่
                </p>
              </div>
            </div>
            <Button variant="outline" size="sm" className="shrink-0 border-destructive/40 text-destructive hover:bg-destructive/10" onClick={() => setResetOpen(true)}>
              <RotateCcw className="h-4 w-4" />
              เปิดหน้าต่างรีเซ็ตข้อมูล
            </Button>
          </CardContent>
        </Card>
      )}

      <ResetDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        busy={resetBusy}
        confirmText={confirmText}
        setConfirmText={setConfirmText}
        resetDocCounter={resetDocCounter}
        setResetDocCounter={setResetDocCounter}
        resetEmailDedup={resetEmailDedup}
        setResetEmailDedup={setResetEmailDedup}
        onConfirm={resetTestData}
      />
    </div>
  )
}

function InfoRow({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="flex min-w-0 items-center justify-between gap-3">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate text-right font-medium">{value}</span>
    </div>
  )
}

function ResetDialog({
  open,
  onOpenChange,
  busy,
  confirmText,
  setConfirmText,
  resetDocCounter,
  setResetDocCounter,
  resetEmailDedup,
  setResetEmailDedup,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  busy: boolean
  confirmText: string
  setConfirmText: (value: string) => void
  resetDocCounter: boolean
  setResetDocCounter: (value: boolean) => void
  resetEmailDedup: boolean
  setResetEmailDedup: (value: boolean) => void
  onConfirm: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-destructive">
            <ShieldAlert className="h-5 w-5" />
            รีเซ็ตข้อมูลชั่วคราว
          </DialogTitle>
          <DialogDescription className="leading-relaxed">
            ล้างเอกสาร, รายการสินค้าในเอกสาร, ไฟล์แนบ, รอบนำเข้า Shopee และประวัติการทำงาน แต่จะเก็บการตั้งค่า, สินค้าใน SML, ตารางจับคู่สินค้า และประวัติการใช้ AI ไว้
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="rounded-md border border-destructive/20 bg-destructive/[0.04] p-3 text-xs leading-relaxed text-destructive">
            ใช้เฉพาะ workspace ที่ไม่ใช่ production หรือเมื่อต้องเริ่มรอบตรวจสอบใหม่ ถ้าเคยส่งเข้า SML จริงแล้ว ไม่ควรรีเซ็ตเลขรันเอกสารเพราะอาจทำให้เลขเอกสารซ้ำกับ SML
          </div>

          <div className="space-y-3 rounded-md border border-border/70 p-3">
            <label className="flex items-start gap-3 text-xs">
              <Checkbox
                checked={resetDocCounter}
                onCheckedChange={(v) => setResetDocCounter(v === true)}
                className="mt-0.5"
              />
              <span>
                <span className="block font-medium">รีเซ็ตเลขรันเอกสาร</span>
                <span className="text-muted-foreground">ใช้เมื่อต้องการเริ่ม NX-PO/NX-SO/NX-INV ใหม่ใน workspace ที่ไม่ใช่ production เท่านั้น</span>
              </span>
            </label>
            <label className="flex items-start gap-3 text-xs">
              <Checkbox
                checked={resetEmailDedup}
                onCheckedChange={(v) => setResetEmailDedup(v === true)}
                className="mt-0.5"
              />
              <span>
                <span className="block font-medium">ล้างประวัติอีเมลและย้อนกลับไปอ่านเมลเก่า</span>
                <span className="text-muted-foreground">
                  เปิดเมื่อล้างเอกสารชั่วคราวแล้วต้องการให้ระบบดูดอีเมลเดิมกลับมาอีกครั้ง ระบบจะ reset ตำแหน่งอ่านล่าสุดของ inbox ด้วย
                </span>
              </span>
            </label>
          </div>

          <div className="space-y-2">
            <Label htmlFor="reset-confirm">พิมพ์ RESET เพื่อยืนยัน</Label>
            <Input
              id="reset-confirm"
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder="RESET"
              autoComplete="off"
            />
          </div>
        </div>

        <DialogFooter className="gap-2 sm:gap-2">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            ยกเลิก
          </Button>
          <Button type="button" variant="destructive" onClick={onConfirm} disabled={busy || confirmText !== 'RESET'}>
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
            รีเซ็ตข้อมูลชั่วคราว
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
