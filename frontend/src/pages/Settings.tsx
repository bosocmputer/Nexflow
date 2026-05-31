import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertOctagon,
  ArrowUpRight,
  Bot,
  CheckCircle2,
  Database,
  Mail,
  MessageSquare,
  Sparkles,
  Settings2,
  type LucideIcon,
} from 'lucide-react'

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

export default function Settings() {
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    client
      .get<SystemStatus>('/api/settings/status')
      .then((r) => setStatus(r.data))
      .catch(() => setStatus(null))
      .finally(() => setLoading(false))
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

  return (
    <div className="space-y-5">
      <PageHeader
        title={PAGE_TITLE.settings}
        description="สถานะการเชื่อมต่อระบบภายนอก · กดที่แต่ละแถวเพื่อจัดการ"
      />

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
              <Sparkles className="h-4 w-4 text-primary" strokeWidth={2.25} />
              <div>
                <p className="text-sm font-medium">เกณฑ์ส่งอัตโนมัติ</p>
                <p className="text-[11px] text-muted-foreground">
                  ความมั่นใจของระบบมากกว่าค่านี้จึงจะผ่านอัตโนมัติ · แก้ได้ที่หน้าการเชื่อมต่อระบบ
                </p>
              </div>
            </div>
            <span className="font-mono text-xl font-semibold tabular-nums text-primary">
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
