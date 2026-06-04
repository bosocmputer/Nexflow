import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertOctagon,
  ArrowUpRight,
  Bot,
  CheckCircle2,
  Database,
  FileClock,
  Mail,
  MessageSquare,
  PackageCheck,
  ReceiptText,
  Sparkles,
  Settings2,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import { PAGE_TITLE } from '@/lib/labels'
import { ENABLE_CHAT } from '@/lib/featureFlags'
import type { SMLReadiness } from '@/types'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

// Live multi-account aware status — returned by GET /api/settings/status.
// LINE/IMAP fields are optional because they only exist when those repos
// are wired (always true in production).
type SystemStatus = {
  sml_configured: boolean
  ai_configured: boolean
  auto_confirm_threshold: number
  line_oa_total?: number
  line_oa_enabled?: number
  imap_total?: number
  imap_enabled?: number
  imap_failing?: number
  sml_readiness?: SMLReadiness
}

type SetupStepLite = {
  key: string
  ready: boolean
  status: string
}

type SetupStatusLite = {
  ready: boolean
  ready_count: number
  total_count: number
  blocking_ready_count: number
  blocking_total_count: number
  steps: SetupStepLite[]
  documents: {
    pending: number
    needs_review: number
    failed: number
    sent: number
    saleinvoice: number
  }
}

// SubsystemRow is a single subsystem on the system-health card. Each row is
// a click-through to the manage page so /settings stays read-only — no
// "view a stat then go figure out where to fix it" handoff.
interface SubsystemRowProps {
  icon: LucideIcon
  label: string
  // Right-aligned status: a quick glanceable summary.
  status: string
  // Tone drives the dot + (when urgent) the row tint.
  tone: 'ok' | 'warn' | 'danger' | 'unknown'
  // Multi-line detail under the status (count breakdowns, expiring tokens, etc.)
  detail?: string
  // Where clicking takes you. Omit for read-only rows (e.g. SML/AI from env).
  to?: string
}

function SubsystemRow({ icon: Icon, label, status, tone, detail, to }: SubsystemRowProps) {
  const dotCls =
    tone === 'ok'
      ? 'bg-success'
      : tone === 'warn'
        ? 'bg-warning'
        : tone === 'danger'
          ? 'bg-destructive animate-pulse'
          : 'bg-muted-foreground/40'

  const inner = (
    <div
      className={cn(
        'flex items-center gap-3 rounded-md px-3 py-2.5 transition-colors',
        to && 'group hover:bg-accent/40',
        tone === 'danger' && 'bg-destructive/[0.04]',
      )}
    >
      <span
        className={cn(
          'flex h-7 w-7 shrink-0 items-center justify-center rounded-md',
          tone === 'danger' ? 'bg-destructive/10 text-destructive' : 'bg-muted text-muted-foreground',
        )}
      >
        <Icon className="h-3.5 w-3.5" strokeWidth={2.25} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate text-sm font-medium text-foreground">{label}</span>
          <span className="flex shrink-0 items-center gap-1.5 text-[11px] tabular-nums text-muted-foreground">
            <span className={cn('inline-block h-1.5 w-1.5 rounded-full', dotCls)} />
            {status}
          </span>
        </div>
        {detail && (
          <p className={cn(
            'mt-0.5 truncate text-[11px]',
            tone === 'danger' ? 'text-destructive' : 'text-muted-foreground',
          )}>
            {detail}
          </p>
        )}
      </div>
      {to && (
        <ArrowUpRight
          className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40 transition-all group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground"
        />
      )}
    </div>
  )

  return to ? <Link to={to}>{inner}</Link> : inner
}

function ReadinessTile({
  icon: Icon,
  label,
  status,
  detail,
  tone,
  to,
}: SubsystemRowProps) {
  const toneClass =
    tone === 'ok'
      ? 'border-success/25 bg-success/[0.04]'
      : tone === 'warn'
        ? 'border-warning/30 bg-warning/[0.06]'
        : tone === 'danger'
          ? 'border-destructive/25 bg-destructive/[0.05]'
          : 'border-border bg-card'

  const iconClass =
    tone === 'ok'
      ? 'text-success'
      : tone === 'warn'
        ? 'text-warning'
        : tone === 'danger'
          ? 'text-destructive'
          : 'text-muted-foreground'

  const content = (
    <div className={cn('h-full rounded-md border p-3 transition-colors', toneClass, to && 'hover:bg-accent/35')}>
      <div className="flex items-start gap-2.5">
        <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', iconClass)} />
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="mt-0.5 truncate text-sm font-semibold text-foreground">{status}</div>
          {detail && <div className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">{detail}</div>}
        </div>
      </div>
    </div>
  )

  return to ? <Link to={to}>{content}</Link> : content
}

export default function Settings() {
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [setupStatus, setSetupStatus] = useState<SetupStatusLite | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    client
      .get<SystemStatus>('/api/settings/status')
      .then((r) => setStatus(r.data))
      .catch(() => setStatus(null))
      .finally(() => setLoading(false))
    client
      .get<SetupStatusLite>('/api/setup/status')
      .then((r) => setSetupStatus(r.data))
      .catch(() => setSetupStatus(null))
  }, [])

  // Derive each subsystem's tone from its live state. Falls back to 'unknown'
  // when the API didn't return that field (e.g. repo not wired in dev).
  const lineOA = (() => {
    if (status?.line_oa_total == null) return null
    const total = status.line_oa_total
    const enabled = status.line_oa_enabled ?? 0
    if (total === 0) {
      return { status: 'ยังไม่มี OA', tone: 'warn' as const, detail: 'เพิ่ม LINE OA เพื่อรับข้อความจากลูกค้า' }
    }
    return {
      status: `${enabled} / ${total} เปิดใช้งาน`,
      tone: enabled > 0 ? ('ok' as const) : ('warn' as const),
      detail: enabled === total ? undefined : `${total - enabled} OA ถูกปิด`,
    }
  })()

  const imap = (() => {
    if (status?.imap_total == null) return null
    const total = status.imap_total
    const enabled = status.imap_enabled ?? 0
    const failing = status.imap_failing ?? 0
    if (total === 0) {
      return { status: 'ยังไม่มีกล่องเมล', tone: 'warn' as const, detail: 'เพิ่มกล่องเมลเพื่อรับบิลทางอีเมล' }
    }
    if (failing > 0) {
      return {
        status: `${failing} มีปัญหา`,
        tone: 'danger' as const,
        detail: `จาก ${enabled} กล่องเมลที่เปิดใช้งาน — ตรวจรหัสผ่าน / 2FA`,
      }
    }
    return {
      status: `${enabled} / ${total} เปิดใช้งาน`,
      tone: enabled > 0 ? ('ok' as const) : ('warn' as const),
    }
  })()

  const sml = (() => {
    const readiness = status?.sml_readiness
    if (readiness) {
      if (!readiness.configured) {
        return {
          status: 'ยังไม่ได้ตั้งค่า',
          tone: 'danger' as const,
          detail: readiness.message || 'ตั้งค่า SML ในหน้าการเชื่อมต่อระบบ',
        }
      }
      if (!readiness.ready) {
        return {
          status: 'เชื่อมต่อไม่ได้',
          tone: 'danger' as const,
          detail: readiness.message || 'เครื่อง SML/Postgres ของร้านนี้อาจยังไม่เปิด',
        }
      }
      return {
        status: 'พร้อมใช้งาน',
        tone: 'ok' as const,
        detail: readiness.tenant ? `ฐานข้อมูล ${readiness.tenant} พร้อมใช้งาน` : 'เชื่อมต่อฐานข้อมูล SML ได้',
      }
    }
    return {
      status: status?.sml_configured ? 'พร้อมใช้งาน' : 'ยังไม่ได้ตั้งค่า',
      tone: status?.sml_configured ? ('ok' as const) : ('danger' as const),
      detail: status?.sml_configured ? 'แก้ URL/database/header ได้ที่การเชื่อมต่อระบบ' : 'ตั้งค่า SML ในหน้าการเชื่อมต่อระบบ',
    }
  })()

  const setupStep = (key: string) => setupStatus?.steps?.find((step) => step.key === key)
  const channelsStep = setupStep('channels')
  const catalogStep = setupStep('catalog')
  const docs = setupStatus?.documents
  const pendingFailures = docs?.failed ?? 0
  const pendingWork = (docs?.pending ?? 0) + (docs?.needs_review ?? 0)
  const readinessCount = setupStatus
    ? `${setupStatus.blocking_ready_count}/${setupStatus.blocking_total_count}`
    : 'กำลังโหลด'

  return (
    <div className="space-y-5">
      <PageHeader
        title={PAGE_TITLE.settings}
        description="สถานะการเชื่อมต่อระบบภายนอก · กดที่แต่ละแถวเพื่อจัดการ"
        actions={
          <Button asChild variant="outline" size="sm">
            <Link to="/setup">
              <ShieldCheck className="h-4 w-4" />
              ตรวจความพร้อมทั้งหมด
            </Link>
          </Button>
        }
      />

      <Card className="border-primary/20 bg-primary/[0.03]">
        <CardContent className="space-y-3 p-4">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <p className="text-sm font-semibold text-foreground">Production readiness</p>
              <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                สรุปจุดที่ต้องพร้อมก่อนรับงานจริง: SML, เส้นทางเอกสาร, สินค้า, Shopee/SI และงานค้างที่ต้องแก้
              </p>
            </div>
            <span className="rounded-md border border-border/70 bg-background px-2 py-1 text-xs font-medium text-foreground">
              ขั้นตอนสำคัญ {readinessCount}
            </span>
          </div>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
            <ReadinessTile
              icon={Database}
              label="SML ERP"
              status={sml.status}
              tone={sml.tone}
              detail={sml.detail}
              to="/settings/instance"
            />
            <ReadinessTile
              icon={FileClock}
              label="เส้นทางเอกสาร"
              status={channelsStep?.ready ? 'พร้อมใช้งาน' : 'ต้องตั้งค่า'}
              tone={channelsStep?.ready ? 'ok' : 'danger'}
              detail={channelsStep?.status || 'Shopee/Marketplace ต้องชี้ไปขายสินค้าและบริการ'}
              to="/settings/channels"
            />
            <ReadinessTile
              icon={PackageCheck}
              label="สินค้าใน SML"
              status={catalogStep?.ready ? 'พร้อมจับคู่' : 'ควรซิงก์สินค้า'}
              tone={catalogStep?.ready ? 'ok' : 'warn'}
              detail={catalogStep?.status || 'ใช้ลดงานเลือกสินค้าและป้องกันส่งรหัสผิด'}
              to="/settings/catalog"
            />
            <ReadinessTile
              icon={ReceiptText}
              label="Shopee / SI"
              status={(docs?.saleinvoice ?? 0) > 0 ? `${docs?.saleinvoice.toLocaleString('th-TH')} เอกสาร` : 'ยังไม่มีเอกสาร'}
              tone={(docs?.saleinvoice ?? 0) > 0 ? 'ok' : 'warn'}
              detail={`ส่งแล้ว ${(docs?.sent ?? 0).toLocaleString('th-TH')} · ค้าง ${pendingWork.toLocaleString('th-TH')}`}
              to="/sale-invoices"
            />
            <ReadinessTile
              icon={ShieldCheck}
              label="งานที่ต้องระวัง"
              status={pendingFailures > 0 ? `${pendingFailures.toLocaleString('th-TH')} ส่งไม่สำเร็จ` : 'ไม่มี failure ค้าง'}
              tone={pendingFailures > 0 ? 'danger' : pendingWork > 0 ? 'warn' : 'ok'}
              detail={pendingWork > 0 ? `มีงานรอตรวจ/พร้อมส่ง ${pendingWork.toLocaleString('th-TH')} รายการ` : 'คิวรายวันไม่มีรายการเสี่ยง'}
              to={pendingFailures > 0 ? '/logs?level=error' : '/dashboard'}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-semibold">การเชื่อมต่อภายนอก</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 px-2 pb-3 pt-0">
          {/* LINE OA — hidden in Phase 1 (VITE_PHASE < 2) */}
          {ENABLE_CHAT && PHASE >= 2 && (lineOA ? (
            <SubsystemRow
              icon={MessageSquare}
              label="LINE OA"
              status={lineOA.status}
              tone={lineOA.tone}
              detail={lineOA.detail}
              to="/settings/line-oa"
            />
          ) : loading ? (
            <SubsystemRowSkeleton icon={MessageSquare} label="LINE OA" />
          ) : (
            <SubsystemRow icon={MessageSquare} label="LINE OA" status="—" tone="unknown" />
          ))}

          {/* Email inboxes — multi-account aware. Failing count surfaces here. */}
          {imap ? (
            <SubsystemRow
              icon={Mail}
              label="กล่องอีเมลรับบิล"
              status={imap.status}
              tone={imap.tone}
              detail={imap.detail}
              to="/settings/email"
            />
          ) : loading ? (
            <SubsystemRowSkeleton icon={Mail} label="กล่องอีเมลรับบิล" />
          ) : (
            <SubsystemRow icon={Mail} label="กล่องอีเมลรับบิล" status="—" tone="unknown" />
          )}

          {/* SML — env config only; not multi-account, no click-through. */}
          <SubsystemRow
            icon={Database}
            label="SML ERP"
            status={sml.status}
            tone={sml.tone}
            detail={sml.detail}
            to="/settings/instance"
          />

          <SubsystemRow
            icon={Bot}
            label="OpenRouter AI"
            status={status?.ai_configured ? 'พร้อมใช้งาน' : 'ยังไม่ได้ตั้งค่า'}
            tone={status?.ai_configured ? 'ok' : 'danger'}
            detail={status?.ai_configured ? 'แก้ API key/model ได้ที่การเชื่อมต่อระบบ' : 'ตั้งค่า OpenRouter ในหน้าการเชื่อมต่อระบบ'}
            to="/settings/instance"
          />

          <SubsystemRow
            icon={Settings2}
            label="การเชื่อมต่อระบบ"
            status="SML / OpenRouter"
            tone="ok"
            detail="ข้อมูลร้าน, SML, OpenRouter และระบบอัตโนมัติ"
            to="/settings/instance"
          />
        </CardContent>
      </Card>

      {/* Auto-confirm threshold — small "config snapshot" card for transparency.
          Lives in env, not editable here; surfacing the value avoids
          "what's our current threshold?" trips into the codebase. */}
      {status && (
        <Card>
          <CardContent className="flex items-center justify-between p-4">
            <div className="flex items-center gap-2.5">
              <Sparkles className="h-4 w-4 text-accent-strong" strokeWidth={2.25} />
              <div>
                <p className="text-sm font-medium">เกณฑ์ส่งอัตโนมัติ</p>
                <p className="text-[11px] text-muted-foreground">
                  ความมั่นใจของระบบมากกว่าค่านี้จึงจะผ่านอัตโนมัติ · แก้ได้ที่หน้าการเชื่อมต่อระบบ
                </p>
              </div>
            </div>
            <span className="font-mono text-xl font-semibold tabular-nums text-accent-strong">
              {(status.auto_confirm_threshold * 100).toFixed(0)}%
            </span>
          </CardContent>
        </Card>
      )}

      {/* Pre-deploy notice — let admin know /settings shows live state, not config */}
      <p className="flex items-center justify-center gap-1.5 text-center text-xs text-muted-foreground">
        <CheckCircle2 className="h-3 w-3" />
        Nexflow Main · สถานะสด · ตั้งค่าจริงในแต่ละหน้าย่อย
      </p>
    </div>
  )
}

// Loading placeholder that mirrors the SubsystemRow layout so the page
// doesn't "jump" when the API returns. Reuses the icon prop so the row
// already feels recognizable while loading.
function SubsystemRowSkeleton({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return (
    <div className="flex items-center gap-3 rounded-md px-3 py-2.5">
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground/40">
        <Icon className="h-3.5 w-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className="text-sm text-muted-foreground/40">{label}</span>
          <span className="text-[11px] text-muted-foreground/40">กำลังโหลด…</span>
        </div>
      </div>
      {/* Use AlertOctagon as a hidden anchor so layout matches non-skeleton row */}
      <AlertOctagon className="h-3.5 w-3.5 opacity-0" />
    </div>
  )
}
