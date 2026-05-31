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
  RefreshCw,
  RotateCcw,
  ServerCog,
  ShieldAlert,
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

  const nextStep = useMemo(
    () => status?.steps.find((s) => s.blocking !== false && !s.ready) ?? status?.steps.find((s) => !s.ready) ?? status?.steps[0],
    [status],
  )
  const pct = status ? Math.round((status.blocking_ready_count / Math.max(status.blocking_total_count, 1)) * 100) : 0
  const docs = status?.documents
  const imports = status?.imports
  const smlReadiness = status?.sml_readiness
  const workPending = (docs?.pending ?? 0) + (docs?.needs_review ?? 0) + (docs?.failed ?? 0)
  const hasWorkPending = !!status?.ready && workPending > 0

  const resetTestData = async () => {
    if (confirmText !== 'RESET') {
      toast.error('พิมพ์ RESET เพื่อยืนยัน')
      return
    }
    setResetBusy(true)
    const id = toast.loading('กำลังล้างข้อมูลทดสอบ...')
    try {
      await client.post('/api/setup/reset-test-data', {
        confirm: confirmText,
        reset_doc_counter: resetDocCounter,
        reset_email_dedup: resetEmailDedup,
      })
      toast.success(
        resetEmailDedup
          ? 'ล้างข้อมูลแล้ว ระบบจะอ่านอีเมลเก่าในรอบ poll ถัดไป'
          : 'ล้างข้อมูลทดสอบแล้ว',
        { id },
      )
      setResetOpen(false)
      setConfirmText('')
      setResetDocCounter(false)
      setResetEmailDedup(false)
      await load()
    } catch (e: any) {
      toast.error('ล้างข้อมูลไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'), { id })
    } finally {
      setResetBusy(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="เริ่มต้นใช้งาน"
        description="ตรวจความพร้อมร้านและจัดการข้อมูลทดสอบก่อนเริ่มใช้งานจริง"
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => load(true)} disabled={loading}>
              <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
              ตรวจใหม่
            </Button>
            <Button variant="outline" size="sm" onClick={() => setResetOpen(true)}>
              <RotateCcw className="h-4 w-4" />
              ล้างข้อมูลทดสอบ
            </Button>
          </div>
        }
      />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(320px,0.8fr)]">
        <Card className={cn('border-border/70', status?.ready ? 'bg-success/[0.04]' : 'bg-warning/[0.05]')}>
          <CardContent className="flex flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                {status?.ready ? (
                  <CheckCircle2 className="h-5 w-5 text-success" />
                ) : (
                  <CircleAlert className="h-5 w-5 text-warning" />
                )}
                <p className="text-sm font-semibold">
                  {status?.ready
                    ? hasWorkPending
                      ? 'ระบบพร้อมใช้งาน มีงานค้างให้จัดการ'
                      : 'พร้อมให้ลูกค้าทดลองใช้งาน'
                    : 'ยังมีขั้นตอนที่ต้องตั้งค่า'}
                </p>
              </div>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {status
                  ? hasWorkPending
                    ? `การตั้งค่าหลักพร้อมแล้ว · มีเอกสารต้องตรวจ/ส่ง ${n(workPending)} รายการ`
                    : `ขั้นตอนสำคัญพร้อม ${status.blocking_ready_count}/${status.blocking_total_count} · ทั้งหมด ${status.ready_count}/${status.total_count}`
                  : 'กำลังตรวจสถานะ...'}
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
            <div className="h-2 w-full overflow-hidden rounded-full bg-muted lg:max-w-xs">
              <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${pct}%` }} />
            </div>
            {nextStep && !status?.ready && (
              <Button asChild size="sm" className="shrink-0">
                <Link to={nextStep.href}>
                  ไปขั้นต่อไป
                  <ArrowRight className="h-4 w-4" />
                </Link>
              </Button>
            )}
            {hasWorkPending && (
              <div className="flex shrink-0 flex-wrap gap-2">
                {(docs?.needs_review ?? 0) > 0 && (
                  <Button asChild size="sm">
                    <Link to="/bills?status=needs_review">
                      ไปตรวจเอกสาร
                      <ArrowRight className="h-4 w-4" />
                    </Link>
                  </Button>
                )}
                {(docs?.pending ?? 0) > 0 && (
                  <Button asChild variant="outline" size="sm">
                    <Link to="/bills?status=pending">ดูเอกสารสถานะพร้อมส่ง</Link>
                  </Button>
                )}
                {(docs?.failed ?? 0) > 0 && (
                  <Button asChild variant="outline" size="sm">
                    <Link to="/logs?level=error">ดูรายการส่งไม่สำเร็จ</Link>
                  </Button>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold">ร้านนี้</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-xs">
            <InfoRow label="ร้าน" value={status?.system?.instance_name ?? 'Nexflow'} />
            <InfoRow label="รหัสร้าน" value={status?.system?.instance_slug ?? 'default'} />
            <InfoRow label="ฐานข้อมูล SML" value={status?.system?.sml_database ?? '-'} />
            <InfoRow
              label="สถานะ SML"
              value={
                smlReadiness?.ready
                  ? 'พร้อมใช้งาน'
                  : smlReadiness?.message ?? 'ยังไม่ได้ตรวจ'
              }
            />
            <InfoRow label="AI ที่ใช้งาน" value={status?.system?.openrouter_model ?? '-'} />
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-3 lg:grid-cols-3">
        <StatTile label="เอกสารทั้งหมด" value={n(docs?.total)} />
        <StatTile label="สถานะพร้อมส่ง/รอตรวจ" value={`${n(docs?.pending)} / ${n(docs?.needs_review)}`} tone={docs?.needs_review ? 'warn' : 'ok'} />
        <StatTile label="ส่งไม่สำเร็จ" value={n(docs?.failed)} tone={docs?.failed ? 'danger' : 'ok'} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(360px,0.85fr)]">
        <div className="grid gap-3">
          {(status?.steps ?? []).map((step, index) => {
            const Icon = iconByStep[step.key] ?? ClipboardCheck
            return (
              <Card key={step.key} className={cn('border-border/70', step.ready && 'bg-success/[0.03]')}>
                <CardContent className="flex flex-col gap-3 p-4 md:flex-row md:items-center">
                  <div
                    className={cn(
                      'flex h-9 w-9 shrink-0 items-center justify-center rounded-md border text-sm font-semibold',
                      step.ready ? 'border-success/30 bg-success/10 text-success' : 'border-warning/30 bg-warning/10 text-warning',
                    )}
                  >
                    {step.ready ? <CheckCircle2 className="h-4 w-4" /> : <Icon className="h-4 w-4" />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="text-sm font-semibold">{index + 1}. {step.title}</h2>
                      <Badge variant={step.ready ? 'default' : 'outline'} className="h-5 px-1.5 text-[10px]">
                        {step.status}
                      </Badge>
                      {step.blocking === false && (
                        <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                          เสริม
                        </Badge>
                      )}
                    </div>
                    <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{step.description}</p>
                    {!!step.missing?.length && (
                      <div className="mt-2 flex flex-wrap gap-1">
                        {step.missing.map((m) => (
                          <Badge key={m} variant="secondary" className="h-5 px-1.5 text-[10px]">
                            <CircleDot className="h-3 w-3" />
                            {m}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>
                  <Button asChild variant={step.ready ? 'outline' : 'default'} size="sm" className="md:self-center">
                    <Link to={step.href}>
                      {step.ready ? 'ตรวจดู' : 'ไปตั้งค่า'}
                      <ArrowRight className="h-4 w-4" />
                    </Link>
                  </Button>
                </CardContent>
              </Card>
            )
          })}
        </div>

        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <History className="h-4 w-4" />
                สรุปข้อมูลทดสอบ
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-xs">
              <div className="grid grid-cols-3 gap-2">
                <StatTile label="ใบสั่งซื้อ" value={n(docs?.purchase)} />
                <StatTile label="ใบสั่งขาย" value={n(docs?.saleorder)} />
                <StatTile label="ขายสินค้าฯ" value={n(docs?.saleinvoice)} />
              </div>
              <Separator />
              <InfoRow label="รอบนำเข้า Shopee" value={n(imports?.shopee_runs)} />
              <InfoRow label="กำลังนำเข้า / ไม่สำเร็จ" value={`${n(imports?.shopee_running)} / ${n(imports?.shopee_failed)}`} />
              <InfoRow label="อีเมลที่เคยอ่านแล้ว" value={n(imports?.email_dedup_keys)} />
              <InfoRow label="ประวัติการทำงาน" value={n(imports?.audit_logs)} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Upload className="h-4 w-4" />
                การทำงานล่าสุด
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-xs">
              <InfoRow label="ดึงสินค้า SML" value={fmtDate(status?.system?.last_catalog_sync)} />
              <InfoRow label="อ่านอีเมลล่าสุด" value={fmtDate(status?.system?.last_email_poll)} />
              <InfoRow label="นำเข้า Shopee ล่าสุด" value={fmtDate(status?.system?.last_import_run)} />
            </CardContent>
          </Card>
        </div>
      </div>

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
            ล้างข้อมูลทดสอบ
          </DialogTitle>
          <DialogDescription className="leading-relaxed">
            ล้างเอกสาร, รายการสินค้าในเอกสาร, ไฟล์แนบ, รอบนำเข้า Shopee และประวัติการทำงาน แต่จะเก็บการตั้งค่า, สินค้าใน SML, ตารางจับคู่สินค้า และประวัติการใช้ AI ไว้
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="rounded-md border border-destructive/20 bg-destructive/[0.04] p-3 text-xs leading-relaxed text-destructive">
            ใช้เฉพาะช่วงทดสอบหรือเมื่อต้องการเริ่มทดสอบใหม่ ถ้าเคยส่งเข้า SML จริงแล้ว ไม่ควรรีเซ็ตเลขรันเอกสารเพราะอาจทำให้เลขเอกสารซ้ำกับ SML
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
                <span className="text-muted-foreground">ใช้เมื่อต้องการเริ่ม NX-PO/NX-SO/NX-INV ใหม่ในชุดทดสอบเท่านั้น</span>
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
                  เปิดเมื่อล้างบิลทดสอบแล้วต้องการให้ระบบดูดอีเมลเดิมกลับมาอีกครั้ง ระบบจะ reset ตำแหน่งอ่านล่าสุดของ inbox ด้วย
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
            ล้างข้อมูลทดสอบ
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
