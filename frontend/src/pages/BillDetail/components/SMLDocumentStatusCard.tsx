import { AlertTriangle, CheckCircle2, Clock3, Database, RefreshCw } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/store/auth'
import type { Bill } from '@/types'

type Tone = 'success' | 'warning' | 'danger' | 'muted'

type StatusPresentation = {
  label: string
  detail: string
  tone: Tone
}

const toneClass: Record<Tone, string> = {
  success: 'border-success/30 bg-success/10 text-success',
  warning: 'border-warning/30 bg-warning/10 text-warning-foreground',
  danger: 'border-destructive/30 bg-destructive/10 text-destructive',
  muted: 'border-border bg-muted/50 text-muted-foreground',
}

function coreStatus(bill: Bill): StatusPresentation {
  if (bill.status === 'sent' || ['created', 'already_exists', 'complete'].includes(bill.sml_core_status ?? '')) {
    return { label: 'สร้างแล้ว', detail: 'SML รับ header และรายการหลักแล้ว ห้ามส่งบิลใหม่ซ้ำ', tone: 'success' }
  }
  if (bill.sml_attempt_state === 'sending') {
    return { label: 'กำลังส่ง', detail: 'กำลังยืนยันผลการสร้างเอกสารหลัก', tone: 'warning' }
  }
  return { label: 'ยังไม่สร้าง', detail: 'ยังไม่มีหลักฐานว่า SML สร้างเอกสารหลักสำเร็จ', tone: 'muted' }
}

function profileStatus(bill: Bill): StatusPresentation {
  switch (bill.sml_profile_status) {
    case 'complete':
      return { label: 'สมบูรณ์', detail: 'VAT, ขนส่ง และ audit log ผ่านรายการตรวจที่กำหนดแล้ว', tone: 'success' }
    case 'terminal_failure':
      return { label: 'ต้องให้ผู้ดูแลซ่อม', detail: 'ครบจำนวน retry อัตโนมัติแล้ว เอกสารหลักยังคงอยู่และจะไม่ถูกส่งซ้ำ', tone: 'danger' }
    case 'needs_reconciliation':
      return { label: 'กำลังรอซ่อม', detail: 'เอกสารหลักสร้างแล้ว แต่ข้อมูลประกอบหรือ audit log ยังไม่ครบ', tone: 'warning' }
    case 'pending':
      return { label: 'กำลังตรวจ', detail: 'กำลังตรวจความครบถ้วนของ Document Profile', tone: 'warning' }
    default:
      return { label: 'ไม่เปิดใช้', detail: 'เอกสารนี้ใช้รูปแบบเดิมและไม่มี Profile V1', tone: 'muted' }
  }
}

function stockStatus(bill: Bill): StatusPresentation {
  switch (bill.sml_stock_job_status) {
    case 'completed':
      return { label: 'สำเร็จ', detail: 'คำนวณต้นทุน/สต๊อกจากเอกสารหลักสำเร็จแล้ว', tone: 'success' }
    case 'queued':
    case 'running':
      return { label: bill.sml_stock_job_status === 'running' ? 'กำลังคำนวณ' : 'รอคำนวณ', detail: 'งานสต๊อกแยกจากการเติม Document Profile', tone: 'warning' }
    case 'failed':
    case 'manual_reconciliation':
      return { label: 'ต้องตรวจสอบ', detail: 'เอกสารหลักยังอยู่ แต่การคำนวณสต๊อกยังไม่สมบูรณ์', tone: 'danger' }
    default:
      return { label: 'ไม่มีงาน', detail: 'เอกสารนี้ไม่มี durable stock job หรือไม่เข้าเงื่อนไขคำนวณ', tone: 'muted' }
  }
}

function StatusRow({ title, status }: { title: string; status: StatusPresentation }) {
  const Icon = status.tone === 'success' ? CheckCircle2 : status.tone === 'danger' ? AlertTriangle : Clock3
  return (
    <div className="flex min-w-0 items-start gap-2.5 rounded-md border border-border/70 bg-background/70 p-3">
      <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', status.tone === 'success' ? 'text-success' : status.tone === 'danger' ? 'text-destructive' : 'text-muted-foreground')} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-semibold text-foreground">{title}</span>
          <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px]', toneClass[status.tone])}>{status.label}</Badge>
        </div>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">{status.detail}</p>
      </div>
    </div>
  )
}

export function SMLDocumentStatusCard({ bill, retrying, onRetryProfile }: { bill: Bill; retrying: boolean; onRetryProfile: () => Promise<void> }) {
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const hasProfile = Boolean(bill.sml_profile_version || bill.sml_profile_status)
  if (!bill.current_sml_attempt_id && !hasProfile) return null

  const profile = profileStatus(bill)
  const recoverable = bill.sml_profile_reconciliation_required && ['needs_reconciliation', 'terminal_failure'].includes(bill.sml_profile_status ?? '')
  const jobBusy = ['queued', 'running'].includes(bill.sml_profile_job_status ?? '')

  return (
    <Card className="border-border/80">
      <CardContent className="space-y-3 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 items-start gap-2.5">
            <Database className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
            <div>
              <h3 className="text-sm font-semibold text-foreground">สถานะเอกสาร SML</h3>
              <p className="mt-0.5 text-xs text-muted-foreground">Core, Document Profile และสต๊อกทำงานแยกกัน เพื่อป้องกันการสร้างบิลซ้ำ</p>
            </div>
          </div>
          {isAdmin && recoverable && (
            <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" disabled={retrying || jobBusy} onClick={() => void onRetryProfile()}>
              <RefreshCw className={cn('h-3.5 w-3.5', retrying && 'animate-spin')} />
              {jobBusy ? 'อยู่ในคิวซ่อม' : 'Retry เฉพาะ Profile'}
            </Button>
          )}
        </div>

        <div className="grid gap-2 lg:grid-cols-3">
          <StatusRow title="SML Core" status={coreStatus(bill)} />
          <StatusRow title="Document Profile" status={profile} />
          <StatusRow title="Stock recalculation" status={stockStatus(bill)} />
        </div>

        {recoverable && (
          <div className={cn('rounded-md border px-3 py-2.5 text-xs leading-5', profile.tone === 'danger' ? toneClass.danger : toneClass.warning)}>
            <div><span className="font-semibold">สาเหตุ:</span> {bill.sml_profile_error_message || bill.sml_profile_error_code || 'ข้อมูลประกอบเอกสารยังผ่านรายการตรวจไม่ครบ'}</div>
            <div><span className="font-semibold">ผลกระทบ:</span> บิล SML สร้างแล้ว แต่ Profile ยังไม่สมบูรณ์; ระบบจะไม่ส่ง Core ซ้ำ</div>
            <div><span className="font-semibold">ขั้นตอนแก้:</span> ตรวจระบบ logs/Gateway แล้วให้ Admin กด Retry เฉพาะ Profile</div>
          </div>
        )}

        {(bill.sml_doc_no || bill.sml_profile_correlation_id || hasProfile) && (
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
            {bill.sml_doc_no && <span>เอกสาร <code className="font-mono text-foreground">{bill.sml_doc_no}</code></span>}
            {hasProfile && <span>Profile <code className="font-mono text-foreground">{bill.sml_profile_version}</code></span>}
            {bill.sml_profile_correlation_id && <span>Correlation <code className="font-mono text-foreground">{bill.sml_profile_correlation_id}</code></span>}
            {bill.sml_profile_max_retries ? <span>Retry {bill.sml_profile_retry_count ?? 0}/{bill.sml_profile_max_retries}{bill.sml_profile_manual_retries ? ` · ผู้ดูแล ${bill.sml_profile_manual_retries} ครั้ง` : ''}</span> : null}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
