import { AlertTriangle, CheckCircle2, Clock3, RefreshCw } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { documentSendPresentation } from '@/lib/smlPayloadSummary.js'
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
    return { label: 'สร้างแล้ว', detail: 'เอกสารและรายการสินค้าถูกสร้างใน SML แล้ว', tone: 'success' }
  }
  if (bill.sml_attempt_state === 'sending') {
    return { label: 'กำลังส่ง', detail: 'กำลังยืนยันผลการสร้างเอกสารหลัก', tone: 'warning' }
  }
  return { label: 'ยังไม่สร้าง', detail: 'ยังไม่มีหลักฐานว่า SML สร้างเอกสารหลักสำเร็จ', tone: 'muted' }
}

function profileStatus(bill: Bill): StatusPresentation {
  switch (bill.sml_profile_status) {
    case 'complete':
      return { label: 'ครบถ้วน', detail: 'ข้อมูลภาษี การจัดส่ง และประวัติเอกสารครบแล้ว', tone: 'success' }
    case 'terminal_failure':
      return { label: 'ต้องให้ผู้ดูแลแก้ไข', detail: 'ระบบลองซ่อมอัตโนมัติครบแล้ว แต่เอกสารใน SML ยังคงอยู่', tone: 'danger' }
    case 'needs_reconciliation':
      return { label: 'กำลังรอแก้ไข', detail: 'เอกสารสร้างแล้ว แต่ข้อมูลประกอบหรือประวัติยังไม่ครบ', tone: 'warning' }
    case 'pending':
      return { label: 'กำลังตรวจ', detail: 'กำลังตรวจความครบถ้วนของข้อมูลประกอบเอกสาร', tone: 'warning' }
    default:
      return { label: 'ไม่ต้องดำเนินการ', detail: 'เอกสารนี้ไม่มีงานข้อมูลประกอบที่ต้องรอ', tone: 'muted' }
  }
}

function stockStatus(bill: Bill): StatusPresentation {
  switch (bill.sml_stock_job_status) {
    case 'completed':
      return { label: 'สำเร็จ', detail: 'คำนวณต้นทุน/สต๊อกจากเอกสารหลักสำเร็จแล้ว', tone: 'success' }
    case 'queued':
    case 'running':
      return { label: bill.sml_stock_job_status === 'running' ? 'กำลังคำนวณ' : 'รอคำนวณ', detail: 'ระบบกำลังคำนวณต้นทุนและสต๊อกหลังส่งบิล', tone: 'warning' }
    case 'failed':
    case 'manual_reconciliation':
      return { label: 'ต้องตรวจสอบ', detail: 'เอกสารหลักยังอยู่ แต่การคำนวณสต๊อกยังไม่สมบูรณ์', tone: 'danger' }
    default:
      return { label: 'ไม่ต้องดำเนินการ', detail: 'เอกสารนี้ไม่มีงานคำนวณสต๊อกที่ต้องรอ', tone: 'muted' }
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
  const presentation = documentSendPresentation(bill as unknown as Record<string, unknown>)
  const recoverable = bill.sml_profile_reconciliation_required && ['needs_reconciliation', 'terminal_failure'].includes(bill.sml_profile_status ?? '')
  const jobBusy = ['queued', 'running'].includes(bill.sml_profile_job_status ?? '')

  if (presentation.complete) return null

  return (
    <Card className="border-border/80">
      <CardContent className="space-y-3 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 items-start gap-2.5">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
            <div>
              <h3 className="text-sm font-semibold text-foreground">
                {bill.status === 'sent' ? 'มีงานหลังส่ง SML ที่ต้องตรวจสอบ' : 'กำลังตรวจสอบผลการส่ง SML'}
              </h3>
              <p className="mt-0.5 text-xs text-muted-foreground">หากเอกสารถูกสร้างแล้ว ระบบจะซ่อมเฉพาะส่วนที่ขาดและจะไม่สร้างบิลซ้ำ</p>
            </div>
          </div>
          {isAdmin && recoverable && (
            <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" disabled={retrying || jobBusy} onClick={() => void onRetryProfile()}>
              <RefreshCw className={cn('h-3.5 w-3.5', retrying && 'animate-spin')} />
              {jobBusy ? 'อยู่ในคิวแก้ไข' : 'ลองแก้ไขข้อมูลประกอบอีกครั้ง'}
            </Button>
          )}
        </div>

        <div className="grid gap-2 lg:grid-cols-3">
          <StatusRow title="เอกสารใน SML" status={coreStatus(bill)} />
          <StatusRow title="ข้อมูลประกอบเอกสาร" status={profile} />
          <StatusRow title="ต้นทุนและสต๊อก" status={stockStatus(bill)} />
        </div>

        {recoverable && (
          <div className={cn('rounded-md border px-3 py-2.5 text-xs leading-5', profile.tone === 'danger' ? toneClass.danger : toneClass.warning)}>
            <div><span className="font-semibold">สาเหตุ:</span> {bill.sml_profile_error_message || bill.sml_profile_error_code || 'ข้อมูลประกอบเอกสารยังผ่านรายการตรวจไม่ครบ'}</div>
            <div><span className="font-semibold">ผลกระทบ:</span> บิล SML สร้างแล้ว แต่ข้อมูลประกอบยังไม่ครบ ระบบจะไม่ส่งเอกสารซ้ำ</div>
            <div><span className="font-semibold">ขั้นตอนแก้:</span> ให้ผู้ดูแลตรวจระบบเชื่อมต่อ แล้วกดลองแก้ไขข้อมูลประกอบอีกครั้ง</div>
          </div>
        )}

        {['failed', 'manual_reconciliation'].includes(bill.sml_stock_job_status ?? '') && (
          <div className={cn('rounded-md border px-3 py-2.5 text-xs leading-5', toneClass.danger)}>
            <div><span className="font-semibold">ต้นทุนและสต๊อก:</span> {bill.sml_stock_error_message || 'คำนวณหลังส่งบิลไม่สำเร็จ'}</div>
            <div><span className="font-semibold">ผลกระทบ:</span> เอกสารใน SML ยังอยู่และจะไม่ถูกส่งซ้ำ กรุณาให้ผู้ดูแลตรวจงานคำนวณสต๊อก</div>
          </div>
        )}

        {(bill.sml_doc_no || recoverable) && (
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
            {bill.sml_doc_no && <span>เอกสาร <code className="font-mono text-foreground">{bill.sml_doc_no}</code></span>}
            {recoverable && bill.sml_profile_max_retries ? <span>ลองแก้ไขแล้ว {bill.sml_profile_retry_count ?? 0}/{bill.sml_profile_max_retries} ครั้ง</span> : null}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
