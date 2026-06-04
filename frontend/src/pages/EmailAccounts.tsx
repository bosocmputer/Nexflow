import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import axios from 'axios'
import {
  AlertCircle,
  ExternalLink,
  FileText,
  Info,
  ListFilter,
  Loader2,
  Mail,
  Pencil,
  PlayCircle,
  Plus,
  RotateCcw,
  Search,
  ShoppingBag,
  Trash2,
} from 'lucide-react'
import dayjs from 'dayjs'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import {
  TooltipProvider,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import {
  createIMAPPollJob,
  isActiveIMAPPollJob,
  listActiveIMAPPollJobs,
  type IMAPPollJob,
} from '@/hooks/useIMAPPollJobs'
import type { IMAPAccount } from '@/pages/EmailAccounts/AccountDialog'
import { AccountDialog } from '@/pages/EmailAccounts/AccountDialog'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)
const GMAIL_SECURITY_URL = 'https://myaccount.google.com/security'
const GMAIL_APP_PASSWORDS_URL = 'https://myaccount.google.com/apppasswords'
const GMAIL_IMAP_SETTINGS_URL = 'https://mail.google.com/mail/u/0/#settings/fwdandpop'
const PURCHASE_BILLS_URL = '/bills?status=needs_review&source=shopee_shipped&bill_type=purchase'

interface IMAPAccountFull extends IMAPAccount {
  last_polled_at?: string | null
  last_poll_status?: string | null
  last_poll_error?: string | null
  last_poll_messages?: number | null
  last_poll_found?: number | null
  last_poll_processed?: number | null
  last_poll_skipped?: number | null
  last_poll_details?: IMAPPollDetail[]
  last_poll_summary?: IMAPPollSummary | null
  last_seen_uid?: number
  last_poll_limited?: boolean
  last_poll_backlog?: number | null
  consecutive_failures?: number
  poll_running?: boolean
}

interface IMAPPollSummary {
  scanned?: number
  created?: number
  already_processed?: number
  skipped_user?: number
  failed?: number
  interrupted?: boolean
}

type SetupStatusLite = {
  system?: {
    instance_name?: string
    instance_slug?: string
    sml_database?: string
  }
}

interface IMAPPollDetail {
  uid?: number
  message_id?: string
  subject?: string
  from?: string
  email_date?: string
  status: 'processed' | 'skipped' | string
  reason_code?: string
  reason_label?: string
}

const CHANNEL_META: Record<string, { label: string; cls: string }> = {
  general: { label: 'ไฟล์แนบทั่วไป', cls: 'bg-secondary text-secondary-foreground' },
  shopee:  { label: 'Shopee',  cls: 'bg-warning/15 text-warning hover:bg-warning/20' },
  lazada:  { label: 'Lazada',  cls: 'bg-info/15 text-info hover:bg-info/20' },
}

function HelpBanner() {
  return (
    <details className="group rounded-md border border-info/20 bg-info/[0.035] text-sm">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-3 py-2">
        <span className="inline-flex min-w-0 items-center gap-2">
          <Info className="h-4 w-4 shrink-0 text-info" />
          <span className="font-medium text-foreground">รายละเอียดสำหรับแอดมิน: Gmail / IMAP</span>
        </span>
        <span className="text-xs text-link group-open:hidden">เปิด</span>
        <span className="hidden text-xs text-muted-foreground group-open:inline">ย่อ</span>
      </summary>
      <div className="border-t border-info/15 px-3 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="text-xs leading-5 text-muted-foreground">
            เปิด 2-Step Verification, สร้าง App Password, เปิด IMAP แล้วค่อยเพิ่มกล่องเมล
          </div>
          <Sheet>
            <SheetTrigger asChild>
              <Button variant="outline" size="sm" className="h-8">
                เปิดคู่มือเต็ม
              </Button>
            </SheetTrigger>
            <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
              <SheetHeader>
                <SheetTitle>Nexflow ดึงอีเมลแบบไหน</SheetTitle>
                <SheetDescription>
                  ระบบจะอ่านอีเมลจากกล่องที่เพิ่มไว้ แล้วสร้างบิลให้อัตโนมัติ
                </SheetDescription>
              </SheetHeader>
              <div className="mt-5 space-y-4 text-sm">
                <div className={cn('grid grid-cols-1 gap-3', PHASE >= 2 && 'sm:grid-cols-2')}>
                  <div className="rounded-md border border-border bg-card p-3">
                    <div className="mb-1 flex items-center gap-2 text-sm font-semibold">
                      <ShoppingBag className="h-4 w-4 text-warning" />
                      กล่องเมล Shopee
                    </div>
                    <p className="text-xs text-muted-foreground">
                      สำหรับ Gmail/Outlook ที่มีอีเมลจาก Shopee ใช้สร้างบิลซื้อจากอีเมล
                    </p>
                    <ul className="mt-1.5 space-y-0.5 pl-4 text-xs">
                      <li className="list-disc">Subject “ถูกจัดส่งแล้ว” หรือ “ยืนยันการชำระเงิน” → บิลซื้อ</li>
                      <li className="list-disc">บิลที่สร้างแล้วจะไปตรวจต่อที่หน้าใบสั่งซื้อ</li>
                    </ul>
                  </div>
                  {PHASE >= 2 && (
                    <div className="rounded-md border border-border bg-card p-3">
                      <div className="mb-1 flex items-center gap-2 text-sm font-semibold">
                        <FileText className="h-4 w-4 text-info" />
                        กล่องเมลไฟล์แนบทั่วไป
                      </div>
                      <p className="text-xs text-muted-foreground">
                        สำหรับ PDF / Excel แนบจากผู้ขายทั่วไป ใช้ในช่องทางเสริม
                      </p>
                    </div>
                  )}
                </div>
                <div className="rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
                  Gmail ต้องเตรียม 3 อย่างก่อนเชื่อม: เปิด <b>2-Step Verification</b>, สร้าง{' '}
                  <b>App Password 16 ตัวอักษร</b>, และเปิด <b>IMAP</b> ใน Gmail. วาง App Password
                  แบบมีช่องว่างได้ ระบบจะลบช่องว่างให้ก่อนส่งต่อ
                </div>
                <div className="flex flex-wrap gap-2">
                  {[
                    ['เปิด 2-Step Verification', GMAIL_SECURITY_URL],
                    ['สร้าง App Password', GMAIL_APP_PASSWORDS_URL],
                    ['เปิด Gmail IMAP', GMAIL_IMAP_SETTINGS_URL],
                  ].map(([label, href]) => (
                    <a
                      key={href}
                      href={href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex h-8 items-center gap-1 rounded-md border border-border bg-background px-2.5 text-xs font-medium text-foreground hover:bg-accent"
                    >
                      {label}
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                    </a>
                  ))}
                </div>
              </div>
            </SheetContent>
          </Sheet>
        </div>
      </div>
    </details>
  )
}

function statusLabel(s?: string | null): string {
  if (!s) return 'ยังไม่ poll'
  switch (s) {
    case 'ok': return 'พร้อมใช้งาน'
    case 'no_new_mail': return 'ไม่มีอีเมลใหม่'
    case 'backlog': return 'กำลังทยอยอ่าน'
    case 'partial': return 'อ่านได้บางส่วน'
    case 'warning': return 'มีคำเตือน'
    case 'interrupted': return 'ถูกตัดระหว่างรีสตาร์ท'
    case 'error': return 'ผิดพลาด'
    case 'connect_failed': return 'เชื่อมต่อไม่ได้'
    case 'auth_failed': return 'รหัสผ่านผิด'
    case 'select_failed': return 'โฟลเดอร์ไม่มี'
    case 'search_failed': return 'ค้นหาเมลผิดพลาด'
    case 'fetch_failed': return 'อ่านอีเมลผิดพลาด'
    default: return s
  }
}

function pollProgressHint(account: IMAPAccountFull): string {
  if (account.last_poll_status === 'partial') {
    return 'รอบล่าสุดอ่านได้บางส่วนและบันทึกความคืบหน้าไว้แล้ว ระบบจะอ่านต่อเองในพื้นหลัง'
  }
  if (account.last_poll_status === 'interrupted') {
    return 'รอบล่าสุดถูกตัดระหว่างรีสตาร์ท ยังไม่ถือเป็นความผิดพลาดของกล่องเมล'
  }
  const backlog = account.last_poll_backlog ?? 0
  if (account.last_poll_status === 'backlog' || account.last_poll_limited || backlog > 0) {
    return `กล่องนี้มีเมลรออ่านเยอะ ระบบจะทยอยอ่านต่อเองเป็นชุดเพื่อไม่ให้ Gmail และ backend หนัก${backlog > 0 ? ` ยังเหลือประมาณ ${backlog.toLocaleString('th-TH')} เมล` : ''}`
  }
  return ''
}

function friendlyPollError(error?: string | null): string {
  if (!error) return ''
  const lower = error.toLowerCase()
  if (lower.includes('poll already running') || lower.includes('กำลังดึงอีเมลอยู่แล้ว')) {
    return 'กล่องนี้กำลังดึงอีเมลอยู่แล้ว บิลอาจกำลังทยอยเข้า ให้ดูหน้าใบสั่งซื้อหรือรอสถานะอัปเดต'
  }
  if (lower.includes('context canceled')) {
    return 'รอบดึงอีเมลถูกตัดระหว่างรีสตาร์ท ระบบจะดึงใหม่ในรอบถัดไป'
  }
  if (lower.includes('ถูกข้าม') && lower.includes('ผู้ส่งที่ยอมรับ')) {
    return error
  }
  if (lower.includes('shopee_channel_non_shopee_from')) {
    return 'มีอีเมลเข้ามา แต่ผู้ส่งไม่อยู่ในรายการที่ยอมรับ ให้กดแก้ไขแล้วเพิ่มอีเมลหรือโดเมนผู้ส่ง หรือเว้นว่างถ้าต้องการรับทุกผู้ส่งที่ผ่านคำกรองหัวข้อ'
  }
  if (lower.includes('duplicate_or_empty') || error.includes('ไม่มีบิลใหม่จากเมลนี้')) {
    return 'เมลที่อ่านได้ซ้ำหรือไม่มีรายการใหม่ให้สร้างบิล ไม่ใช่ปัญหาการเชื่อมต่อ'
  }
  if (lower.includes('empty items')) {
    return 'มีอีเมลที่ผ่านคำกรองหัวข้อเข้ามา แต่ไม่ใช่รูปแบบบิลซื้อ Shopee ที่ระบบอ่านได้ แนะนำให้กดแก้ไขกล่องเมล แล้วเหลือคำกรองเฉพาะ "ถูกจัดส่งแล้ว" และ "ยืนยันการชำระเงินคำสั่งซื้อหมายเลข"'
  }
  if (lower.includes('empty orders')) {
    return 'มีอีเมลที่ผ่านคำกรองหัวข้อเข้ามา แต่ไม่พบเลขคำสั่งซื้อ Shopee แนะนำให้ตรวจคำกรองหัวข้อในกล่องเมลนี้'
  }
  if (lower.includes('openrouter') || lower.includes('credit') || lower.includes('quota')) {
    return 'ระบบ AI ยังประมวลผลไม่ได้ กรุณาตรวจเครดิตหรือการเชื่อมต่อ OpenRouter'
  }
  if (
    lower.includes('authenticationfailed') ||
    lower.includes('authenticate') ||
    lower.includes('invalid credentials') ||
    lower.includes('password')
  ) {
    return 'Gmail/IMAP ยืนยันตัวตนไม่ผ่าน: ให้ใช้ App Password 16 ตัวจาก Google ไม่ใช่รหัสผ่าน Gmail ปกติ, ตรวจว่าเปิด 2-Step Verification แล้ว, เปิด IMAP แล้ว, และวางรหัสได้แม้มีช่องว่างเพราะระบบจะลบให้'
  }
  return error
}

function compactList(value?: string | null, maxItems = 2): string {
  const items = (value ?? '')
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)
  if (items.length === 0) return 'ยังไม่กำหนด'
  if (items.length <= maxItems) return items.join(', ')
  return `${items.slice(0, maxItems).join(', ')} +${items.length - maxItems}`
}

function pollDetailStatusLabel(status: string): string {
  switch (status) {
    case 'processed':
      return 'สร้างบิลใหม่'
    case 'skipped':
      return 'ไม่สร้างบิลใหม่'
    default:
      return status || 'ไม่ทราบสถานะ'
  }
}

function friendlyReasonLabel(detail: Pick<IMAPPollDetail, 'reason_code' | 'reason_label' | 'status'>): string {
  if (detail.reason_code === 'duplicate') return 'เคยประมวลผลแล้ว'
  if (detail.reason_code === 'duplicate_or_empty') return 'ซ้ำหรือไม่มีรายการใหม่'
  if (detail.reason_code === 'accepted') return 'ส่งเข้ากระบวนการสร้างบิลแล้ว'
  return detail.reason_label || detail.reason_code || pollDetailStatusLabel(detail.status)
}

function pollDetailStatusClass(status: string): string {
  switch (status) {
    case 'processed':
      return 'bg-success/10 text-success'
    case 'skipped':
      return 'bg-muted text-muted-foreground'
    default:
      return 'bg-muted text-muted-foreground'
  }
}

function pollSummaryFor(account: IMAPAccountFull): Required<IMAPPollSummary> {
  const duplicateDetails = (account.last_poll_details ?? []).filter((d) => d.reason_code === 'duplicate').length
  const summary = account.last_poll_summary ?? {}
  const scanned = summary.scanned ?? account.last_poll_found ?? 0
  const created = summary.created ?? account.last_poll_processed ?? account.last_poll_messages ?? 0
  const alreadyProcessed = summary.already_processed ?? duplicateDetails
  const skippedUser = summary.skipped_user ?? Math.max((account.last_poll_skipped ?? 0) - alreadyProcessed, 0)
  const failed =
    summary.failed ??
    (account.last_poll_status && !['ok', 'no_new_mail', 'backlog', 'partial', 'warning', 'interrupted'].includes(account.last_poll_status)
      ? 1
      : 0)
  return {
    scanned,
    created,
    already_processed: alreadyProcessed,
    skipped_user: skippedUser,
    failed,
    interrupted: summary.interrupted ?? account.last_poll_status === 'interrupted',
  }
}

function formatPollSummary(summary: Required<IMAPPollSummary>): string {
  return `สแกน ${summary.scanned} / สร้างใหม่ ${summary.created} / เคยประมวลผลแล้ว ${summary.already_processed}`
}

function withPurchaseBillsAction(id: string | number, enabled = true) {
  const opts: {
    id: string | number
    action?: { label: string; onClick: () => void }
  } = { id }
  if (enabled) {
    opts.action = {
      label: 'ดูใบสั่งซื้อ',
      onClick: () => {
        window.location.href = PURCHASE_BILLS_URL
      },
    }
  }
  return opts
}

function isPollRunningError(error?: string | null): boolean {
  const lower = (error ?? '').toLowerCase()
  return lower.includes('poll already running') || lower.includes('กำลังดึงอีเมลอยู่แล้ว')
}

function accountState(account: IMAPAccountFull): 'ready' | 'backlog' | 'attention' | 'disabled' | 'unknown' {
  if (!account.enabled) return 'disabled'
  if (account.last_poll_status === 'backlog' || account.last_poll_status === 'partial' || account.last_poll_limited || (account.last_poll_backlog ?? 0) > 0) {
    return 'backlog'
  }
  if (
    account.last_poll_status === 'warning' ||
    account.last_poll_status === 'error' ||
    account.last_poll_status?.endsWith('_failed') ||
    (account.consecutive_failures ?? 0) > 0
  ) {
    return 'attention'
  }
  if (account.last_poll_status === 'ok' || account.last_poll_status === 'no_new_mail') return 'ready'
  return 'unknown'
}

function jobDoneCount(job?: IMAPPollJob | null) {
  if (!job) return 0
  return Math.min(job.scanned_count || 0, Math.max(job.total_count || job.scanned_count || 0, 0))
}

function jobPercent(job?: IMAPPollJob | null) {
  if (!job) return 0
  return Math.min(100, Math.round((jobDoneCount(job) / Math.max(job.total_count || 0, 1)) * 100))
}

function stateLabel(state: ReturnType<typeof accountState>): string {
  switch (state) {
    case 'ready':
      return 'พร้อมใช้งาน'
    case 'backlog':
      return 'กำลังทยอยอ่าน'
    case 'attention':
      return 'ต้องตรวจ'
    case 'disabled':
      return 'ปิดใช้งาน'
    default:
      return 'ยังไม่เคยดึง'
  }
}

function stateTone(state: ReturnType<typeof accountState>): string {
  switch (state) {
    case 'ready':
      return 'border-success/20 bg-success/10 text-success'
    case 'backlog':
      return 'border-warning/20 bg-warning/10 text-warning'
    case 'attention':
      return 'border-destructive/20 bg-destructive/10 text-destructive'
    case 'disabled':
      return 'border-border bg-muted text-muted-foreground'
    default:
      return 'border-border bg-background text-muted-foreground'
  }
}

function pollSummaryText(account: IMAPAccountFull): string {
  const summary = pollSummaryFor(account)
  const backlog = account.last_poll_backlog ?? 0
  const skippedOther = Math.max(summary.skipped_user - summary.already_processed, 0)
  const parts = [
    `สแกน ${summary.scanned.toLocaleString('th-TH')}`,
    `ใหม่ ${summary.created.toLocaleString('th-TH')}`,
    `เคยอ่าน ${summary.already_processed.toLocaleString('th-TH')}`,
    `ต้องตรวจ ${summary.failed.toLocaleString('th-TH')}`,
  ]
  if (skippedOther > 0) parts.push(`ข้าม ${skippedOther.toLocaleString('th-TH')}`)
  if (backlog > 0) parts.push(`เหลือ ${backlog.toLocaleString('th-TH')}`)
  return parts.join(' · ')
}

function PollSummaryView({
  account,
  onOpenDetails,
}: {
  account: IMAPAccountFull
  onOpenDetails: (account: IMAPAccountFull) => void
}) {
  const hasLegacyCounts =
    account.last_poll_found != null ||
    account.last_poll_processed != null ||
    account.last_poll_messages != null ||
    account.last_poll_skipped != null
  if (!account.last_poll_summary && !hasLegacyCounts) {
    return <span className="text-xs text-muted-foreground">—</span>
  }
  const summary = pollSummaryFor(account)
  const backlog = account.last_poll_backlog ?? 0
  const skippedOther = Math.max(summary.skipped_user - summary.already_processed, 0)
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
      <span className="rounded-full border border-border bg-background px-2 py-0.5 tabular-nums text-muted-foreground">
        สแกน {summary.scanned.toLocaleString('th-TH')}
      </span>
      <span className={cn('rounded-full border px-2 py-0.5 tabular-nums', summary.created > 0 ? 'border-success/20 bg-success/10 text-success' : 'border-border bg-muted/40 text-muted-foreground')}>
        สร้างบิลใหม่ {summary.created.toLocaleString('th-TH')}
      </span>
      <span className="rounded-full border border-border bg-background px-2 py-0.5 tabular-nums text-muted-foreground">
        เคยอ่าน {summary.already_processed.toLocaleString('th-TH')}
      </span>
      {skippedOther > 0 && (
        <span className="rounded-full border border-border bg-muted/40 px-2 py-0.5 tabular-nums text-muted-foreground">
          ข้าม {skippedOther.toLocaleString('th-TH')}
        </span>
      )}
      <span className={cn('rounded-full border px-2 py-0.5 tabular-nums', summary.failed > 0 ? 'border-destructive/20 bg-destructive/10 text-destructive' : 'border-border bg-muted/40 text-muted-foreground')}>
        ต้องตรวจ {summary.failed.toLocaleString('th-TH')}
      </span>
      {backlog > 0 && (
        <span className="rounded-full border border-warning/20 bg-warning/10 px-2 py-0.5 tabular-nums text-warning">
          เหลือ {backlog.toLocaleString('th-TH')}
        </span>
      )}
      {account.channel === 'shopee' && summary.created > 0 && (
        <Button asChild variant="outline" size="sm" className="h-7 w-fit gap-1.5 px-2 text-[11px]">
          <Link to={PURCHASE_BILLS_URL}>
            ดูใบสั่งซื้อ
            <ExternalLink className="h-3 w-3" />
          </Link>
        </Button>
      )}
      <LatestPollDetailsButton account={account} onOpen={onOpenDetails} />
    </div>
  )
}

function PollMetric({
  label,
  value,
  tone = 'neutral',
}: {
  label: string
  value: number
  tone?: 'neutral' | 'success' | 'danger' | 'muted'
}) {
  const toneClass =
    tone === 'success'
      ? 'border-success/20 bg-success/10 text-success'
      : tone === 'danger'
        ? 'border-destructive/20 bg-destructive/10 text-destructive'
        : tone === 'muted'
          ? 'border-border bg-muted/40 text-muted-foreground'
          : 'border-border bg-background text-foreground'
  return (
    <div className={cn('rounded-md border px-2 py-1', toneClass)}>
      <div className="text-[10px] leading-tight text-muted-foreground">{label}</div>
      <div className="font-mono text-xs font-semibold tabular-nums">{value}</div>
    </div>
  )
}

function groupPollDetails(details: IMAPPollDetail[]) {
  const groups = new Map<string, { label: string; status: string; count: number; details: IMAPPollDetail[] }>()
  for (const detail of details) {
    const key = detail.reason_code || detail.status || 'unknown'
    const label = friendlyReasonLabel(detail)
    const existing = groups.get(key)
    if (existing) {
      existing.count += 1
      existing.details.push(detail)
    } else {
      groups.set(key, { label, status: detail.status, count: 1, details: [detail] })
    }
  }
  return Array.from(groups.entries()).map(([key, value]) => ({ key, ...value }))
}

function LatestPollDetailsButton({
  account,
  onOpen,
}: {
  account: IMAPAccountFull
  onOpen: (account: IMAPAccountFull) => void
}) {
  const details = account.last_poll_details ?? []
  if (details.length === 0) {
    return null
  }
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className="h-7 w-fit gap-1.5 px-2 text-[11px]"
      onClick={() => onOpen(account)}
    >
      <ListFilter className="h-3.5 w-3.5" />
      ดูรายละเอียดรอบล่าสุด
    </Button>
  )
}

function PollDetailsDialog({
  account,
  onOpenChange,
}: {
  account: IMAPAccountFull | null
  onOpenChange: (open: boolean) => void
}) {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('all')
  const details = account?.last_poll_details ?? []
  const summary = account ? pollSummaryFor(account) : pollSummaryFor({} as IMAPAccountFull)
  const backlog = account?.last_poll_backlog ?? 0
  const reasonGroups = groupPollDetails(details)
  const normalizedQuery = query.trim().toLowerCase()
  const filteredDetails = details.filter((d) => {
    if (status !== 'all' && d.status !== status) return false
    if (!normalizedQuery) return true
    return [d.subject, d.from, d.message_id, d.reason_code, friendlyReasonLabel(d)]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(normalizedQuery))
  })

  return (
    <Dialog open={account !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-5xl overflow-hidden p-0">
        <DialogHeader className="border-b border-border px-6 py-4">
          <DialogTitle>รายละเอียดรอบดึงอีเมลล่าสุด</DialogTitle>
          <DialogDescription>
            {account?.name ?? 'กล่องเมล'} · {account?.last_polled_at ? dayjs(account.last_polled_at).format('DD/MM/YY HH:mm:ss') : 'ยังไม่เคยดึง'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 overflow-y-auto px-6 py-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
            <PollMetric label="สแกนแล้ว" value={summary.scanned} />
            <PollMetric label="สร้างบิลใหม่" value={summary.created} tone={summary.created > 0 ? 'success' : 'muted'} />
            <PollMetric label="เคยประมวลผลแล้ว" value={summary.already_processed} />
            <PollMetric label="ต้องตรวจ" value={summary.failed} tone={summary.failed > 0 ? 'danger' : 'muted'} />
            <PollMetric label="ค้างอ่านต่อ" value={backlog} tone={backlog > 0 ? 'neutral' : 'muted'} />
          </div>

          {account && pollProgressHint(account) && (
            <div className="rounded-md border border-info/20 bg-info/5 px-3 py-2 text-sm text-info">
              {pollProgressHint(account)}
            </div>
          )}

          {summary.scanned > 0 && summary.created === 0 && summary.failed === 0 && summary.already_processed > 0 && (
            <div className="rounded-md border border-success/20 bg-success/5 px-3 py-2 text-sm text-success">
              ไม่มีอีเมลใหม่ — เมลที่พบเคยสร้างบิลแล้ว
            </div>
          )}

          <div className="space-y-2">
            <div className="text-sm font-medium">สรุปตามเหตุผล</div>
            <div className="flex flex-wrap gap-2">
              {reasonGroups.length === 0 ? (
                <span className="text-sm text-muted-foreground">ยังไม่มีรายละเอียดในรอบล่าสุด</span>
              ) : (
                reasonGroups.map((group) => (
                  <span
                    key={group.key}
                    className={cn(
                      'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium',
                      pollDetailStatusClass(group.status),
                    )}
                  >
                    {group.label}
                    <span className="font-mono tabular-nums">{group.count}</span>
                  </span>
                ))
              )}
            </div>
          </div>

          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="pl-8"
                placeholder="ค้นหาเลขคำสั่งซื้อ, ผู้ส่ง, เหตุผล"
              />
            </div>
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger className="w-full sm:w-48">
                <SelectValue placeholder="ทุกสถานะ" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">ทุกสถานะ</SelectItem>
                <SelectItem value="processed">สร้างบิลใหม่</SelectItem>
                <SelectItem value="skipped">ไม่สร้างบิลใหม่</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="overflow-x-auto rounded-md border border-border">
            <div className="min-w-[860px]">
              <div className="grid grid-cols-[120px_1fr_210px_130px_180px] gap-3 border-b border-border bg-muted/50 px-3 py-2 text-xs font-medium text-muted-foreground">
                <div>สถานะ</div>
                <div>หัวข้อ</div>
                <div>ผู้ส่ง</div>
                <div>เวลาอีเมล</div>
                <div>เหตุผล</div>
              </div>
              <div className="max-h-[42vh] overflow-y-auto">
                {filteredDetails.length === 0 ? (
                  <div className="px-3 py-8 text-center text-sm text-muted-foreground">
                    ไม่พบรายการที่ตรงกับตัวกรอง
                  </div>
                ) : (
                  filteredDetails.map((d, idx) => (
                    <div
                      key={`${d.message_id || d.uid || idx}-${idx}`}
                      className="grid grid-cols-[120px_1fr_210px_130px_180px] gap-3 border-b border-border/70 px-3 py-2 text-xs last:border-b-0"
                    >
                      <div>
                        <span className={cn('rounded-full px-2 py-0.5 font-medium', pollDetailStatusClass(d.status))}>
                          {pollDetailStatusLabel(d.status)}
                        </span>
                      </div>
                      <div className="min-w-0">
                        <div className="truncate font-medium text-foreground" title={d.subject || 'ไม่มีหัวข้อ'}>
                          {d.subject || 'ไม่มีหัวข้อ'}
                        </div>
                        {d.message_id && (
                          <div className="truncate font-mono text-[10px] text-muted-foreground" title={d.message_id}>
                            {d.message_id}
                          </div>
                        )}
                      </div>
                      <div className="truncate text-muted-foreground" title={d.from || undefined}>
                        {d.from || 'ไม่พบผู้ส่ง'}
                      </div>
                      <div className="tabular-nums text-muted-foreground">
                        {d.email_date ? dayjs(d.email_date).format('DD/MM/YY HH:mm') : '—'}
                      </div>
                      <div className="text-muted-foreground">
                        {friendlyReasonLabel(d)}
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ResetProgressDialog({
  account,
  onOpenChange,
  onConfirm,
}: {
  account: IMAPAccountFull | null
  onOpenChange: (open: boolean) => void
  onConfirm: (account: IMAPAccountFull, lookbackDays: number, pollNow: boolean) => Promise<void>
}) {
  const [lookbackDays, setLookbackDays] = useState(30)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (account) setLookbackDays(account.lookback_days || 30)
  }, [account])

  const submit = async (pollNow: boolean) => {
    if (!account) return
    const nextLookback = Math.min(90, Math.max(1, Number(lookbackDays) || account.lookback_days || 30))
    setSubmitting(true)
    try {
      await onConfirm(account, nextLookback, pollNow)
      onOpenChange(false)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={account !== null} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>ตั้งช่วงย้อนหลัง / อ่านใหม่</DialogTitle>
          <DialogDescription>
            ใช้เมื่ออยากให้ Nexflow อ่านช่วงอีเมลย้อนหลังใหม่อีกครั้ง ระบบจะรีเซ็ตตำแหน่งอ่านล่าสุดเท่านั้น ไม่ลบประวัติเมลที่เคยสร้างบิลแล้ว
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <label className="space-y-1.5 text-sm font-medium">
            <span>อ่านย้อนหลัง</span>
            <Input
              type="number"
              min={1}
              max={90}
              value={lookbackDays}
              onChange={(e) => setLookbackDays(Number(e.target.value))}
            />
          </label>
          <div className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
            ถ้าเจอเมลเดิม ระบบจะแสดงเป็น “เคยประมวลผลแล้ว” และไม่สร้างบิลซ้ำ
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" disabled={submitting} onClick={() => submit(false)}>
            บันทึกช่วงย้อนหลัง
          </Button>
          <Button disabled={submitting} onClick={() => submit(true)}>
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            บันทึกและดึงตอนนี้
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SummaryPill({
  label,
  value,
  tone = 'neutral',
}: {
  label: string
  value: number
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'muted'
}) {
  const cls =
    tone === 'success'
      ? 'border-success/20 bg-success/10 text-success'
      : tone === 'warning'
        ? 'border-warning/20 bg-warning/10 text-warning'
        : tone === 'danger'
          ? 'border-destructive/20 bg-destructive/10 text-destructive'
          : tone === 'muted'
            ? 'border-border bg-muted/50 text-muted-foreground'
            : 'border-border bg-background text-foreground'
  return (
    <div className={cn('rounded-md border px-2.5 py-2', cls)}>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="font-mono text-lg font-semibold leading-tight tabular-nums">{value.toLocaleString('th-TH')}</div>
    </div>
  )
}

function EmailAccountRow({
  account,
  job,
  polling,
  onPoll,
  onReset,
  onEdit,
  onDelete,
  onOpenDetails,
}: {
  account: IMAPAccountFull
  job?: IMAPPollJob
  polling: boolean
  onPoll: (account: IMAPAccountFull) => void
  onReset: (account: IMAPAccountFull) => void
  onEdit: (account: IMAPAccountFull) => void
  onDelete: (id: string) => void
  onOpenDetails: (account: IMAPAccountFull) => void
}) {
  const state = accountState(account)
  const displayState = polling ? 'backlog' : state
  const meta = CHANNEL_META[account.channel] ?? CHANNEL_META.general
  const hint = pollProgressHint(account)
  const error = friendlyPollError(account.last_poll_error)
  const activeJob = job && isActiveIMAPPollJob(job) ? job : null

  return (
    <div className="rounded-md border border-border bg-card px-4 py-3 shadow-sm">
      <div className="grid gap-3 lg:grid-cols-[minmax(220px,1.2fr)_minmax(150px,0.8fr)_minmax(260px,1fr)] lg:items-center 2xl:grid-cols-[minmax(220px,1.25fr)_150px_170px_minmax(280px,1.4fr)_120px_auto]">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-foreground" title={account.name}>
            {account.name}
          </div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground" title={account.username}>
            {account.username}
          </div>
        </div>

        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Badge variant="secondary" className={cn('w-fit', meta.cls)}>
            {meta.label}
          </Badge>
          {account.channel === 'shopee' && (
            <span
              className="max-w-[220px] truncate text-[11px] text-muted-foreground"
              title={account.shopee_domains || 'เว้นว่าง = รับทุกผู้ส่งที่ผ่านคำกรองหัวข้อ'}
            >
              {account.shopee_domains ? compactList(account.shopee_domains) : 'ทุกผู้ส่ง'}
            </span>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <span className={cn('rounded-full border px-2.5 py-1 text-xs font-medium', stateTone(displayState))}>
            {polling ? 'กำลังดึงอีเมล' : stateLabel(state)}
          </span>
          {account.last_poll_status && (
            <span className="text-[11px] text-muted-foreground">{statusLabel(account.last_poll_status)}</span>
          )}
        </div>

        <div className="min-w-0 space-y-1">
          <PollSummaryView account={account} onOpenDetails={onOpenDetails} />
          {hint && (
            <div className="truncate text-[11px] text-info" title={hint}>
              {hint}
            </div>
          )}
          {polling && (
            <div className="truncate text-[11px] text-warning" title="ระบบกำลังประมวลผลอยู่ บิลอาจทยอยเข้าในหน้าใบสั่งซื้อ">
              ระบบกำลังประมวลผลอยู่ บิลอาจทยอยเข้าในหน้าใบสั่งซื้อ
            </div>
          )}
          {activeJob && (
            <div className="space-y-1 rounded-md border border-info/20 bg-info/5 px-2 py-1.5">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
                <span className="font-medium text-info">
                  ทำแล้ว {jobDoneCount(activeJob).toLocaleString('th-TH')} / {activeJob.total_count.toLocaleString('th-TH')}
                </span>
                <span className="text-success">สร้างบิล {activeJob.created_count.toLocaleString('th-TH')}</span>
                <span>ข้าม {activeJob.skipped_count.toLocaleString('th-TH')}</span>
                <span className={activeJob.failed_count > 0 ? 'text-warning' : ''}>
                  ต้องตรวจ {activeJob.failed_count.toLocaleString('th-TH')}
                </span>
                <span>เหลือ {activeJob.backlog_count.toLocaleString('th-TH')}</span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-background">
                <div className="h-full rounded-full bg-info transition-all" style={{ width: `${jobPercent(activeJob)}%` }} />
              </div>
            </div>
          )}
          {error && state === 'attention' && (
            <div className="truncate text-[11px] text-destructive" title={error}>
              {error}
            </div>
          )}
        </div>

        <div className="text-xs text-muted-foreground">
          <div className="tabular-nums">
            {account.last_polled_at ? dayjs(account.last_polled_at).format('DD/MM/YY HH:mm') : 'ยังไม่เคยดึง'}
          </div>
          <div className="mt-0.5 tabular-nums">ทุก {Math.round(account.poll_interval_seconds / 60)} นาที</div>
        </div>

        <div className="flex flex-wrap justify-start gap-1.5 lg:justify-end">
          <Button
            variant={state === 'backlog' ? 'default' : 'outline'}
            size="sm"
            className="h-8 gap-1.5 px-2.5 text-xs"
            onClick={() => onPoll(account)}
            disabled={!account.enabled || polling}
          >
            {polling ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <PlayCircle className="h-3.5 w-3.5" />}
            {polling ? 'กำลังดึง' : state === 'backlog' ? 'เร่งดึง' : 'ดึงตอนนี้'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-1.5 px-2.5 text-xs"
            onClick={() => onReset(account)}
            disabled={!account.enabled || polling}
          >
            <RotateCcw className="h-3.5 w-3.5" />
            อ่านใหม่
          </Button>
          <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => onEdit(account)}>
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="icon"
            variant="ghost"
            className="h-8 w-8 text-muted-foreground hover:text-destructive"
            onClick={() => onDelete(account.id)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
    </div>
  )
}

export default function EmailAccounts() {
  const [accounts, setAccounts] = useState<IMAPAccountFull[]>([])
  const [instanceContext, setInstanceContext] = useState<SetupStatusLite['system'] | null>(null)
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<IMAPAccount | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [detailAccount, setDetailAccount] = useState<IMAPAccountFull | null>(null)
  const [resetAccount, setResetAccount] = useState<IMAPAccountFull | null>(null)
  const [pollingIds, setPollingIds] = useState<Set<string>>(() => new Set())
  const [activeJobs, setActiveJobs] = useState<IMAPPollJob[]>([])
  const [busyUntilById, setBusyUntilById] = useState<Record<string, number>>({})
  const [pollingBacklogAll, setPollingBacklogAll] = useState(false)
  const [query, setQuery] = useState('')
  const [stateFilter, setStateFilter] = useState('all')
  const [channelFilter, setChannelFilter] = useState('all')

  const fetchAll = async () => {
    try {
      const res = await client.get<{ data: IMAPAccountFull[] }>('/api/settings/imap-accounts')
      setAccounts(res.data.data ?? [])
    } catch {
      toast.error('โหลดรายการอีเมลไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }

  const fetchActiveJobs = async () => {
    try {
      setActiveJobs(await listActiveIMAPPollJobs())
    } catch {
      setActiveJobs([])
    }
  }

  useEffect(() => {
    fetchAll()
    fetchActiveJobs()
    client
      .get<SetupStatusLite>('/api/setup/status')
      .then((res) => setInstanceContext(res.data.system ?? null))
      .catch(() => setInstanceContext(null))
    // Refresh often enough that long IMAP polls do not look frozen to admins.
    const t = setInterval(() => {
      fetchAll()
      fetchActiveJobs()
    }, 4_000)
    return () => clearInterval(t)
  }, [])

  const markBusyHint = (id: string) => {
    setBusyUntilById((prev) => ({ ...prev, [id]: Date.now() + 3 * 60_000 }))
  }

  const handleAdd = () => {
    setEditing(null)
    setDialogOpen(true)
  }

  const handleEdit = (a: IMAPAccountFull) => {
    setEditing(a)
    setDialogOpen(true)
  }

  const handlePollNow = async (a: IMAPAccountFull) => {
    if (pollingIds.has(a.id)) return
    setPollingIds((prev) => new Set(prev).add(a.id))
    const id = toast.loading(`เริ่มดึงอีเมล ${a.name}…`)
    try {
      const job = await createIMAPPollJob(a.id)
      setActiveJobs((prev) => {
        const next = prev.filter((x) => x.id !== job.id && x.account_id !== job.account_id)
        return [job, ...next]
      })
      toast.success('เริ่มงานดึงอีเมลแล้ว สามารถเปลี่ยนไปเมนูอื่นได้', withPurchaseBillsAction(id))
      fetchAll()
      fetchActiveJobs()
    } catch (e) {
      if (axios.isAxiosError(e)) {
        const msg = e.response?.data?.error || e.message
        toast.error(`เริ่มงานดึงอีเมลไม่สำเร็จ${msg ? `: ${friendlyPollError(msg)}` : ''}`, { id })
        return
      }
      toast.error('เริ่มงานดึงอีเมลไม่สำเร็จ', { id })
    } finally {
      setPollingIds((prev) => {
        const next = new Set(prev)
        next.delete(a.id)
        return next
      })
    }
  }

  const handlePollAllBacklog = async () => {
    const backlogAccounts = accounts.filter((account) => accountState(account) === 'backlog' && account.enabled)
    if (backlogAccounts.length === 0) {
      toast.info('ไม่มีกล่องเมลที่ต้องดึงต่อ')
      return
    }
    setPollingBacklogAll(true)
    const id = toast.loading(`กำลังเร่งดึง ${backlogAccounts.length.toLocaleString('th-TH')} กล่อง…`)
    let ok = 0
    let failed = 0
    try {
      for (const account of backlogAccounts) {
        setPollingIds((prev) => new Set(prev).add(account.id))
        try {
          await createIMAPPollJob(account.id)
          ok += 1
        } catch {
          failed += 1
        } finally {
          setPollingIds((prev) => {
            const next = new Set(prev)
            next.delete(account.id)
            return next
          })
        }
      }
      if (failed > 0) {
        toast.warning(`เร่งดึงสำเร็จ ${ok} กล่อง / ไม่สำเร็จ ${failed} กล่อง`, { id })
      } else {
        toast.success(`เร่งดึงครบ ${ok} กล่องแล้ว`, { id })
      }
      fetchAll()
      fetchActiveJobs()
    } finally {
      setPollingBacklogAll(false)
    }
  }

  const handleResetProgress = async (a: IMAPAccountFull, lookbackDays: number, pollNow: boolean) => {
    if (pollNow) {
      setPollingIds((prev) => new Set(prev).add(a.id))
    }
    const id = toast.loading(pollNow ? `กำลังตั้งช่วงย้อนหลังและดึงอีเมล ${a.name}…` : 'กำลังตั้งช่วงย้อนหลัง…')
    try {
      const res = await client.post<{
        job?: IMAPPollJob
        status?: string
        messages_found?: number
        processed?: number
        skipped?: number
        summary?: IMAPPollSummary
        backlog?: number
        error?: string
      }>(`/api/settings/imap-accounts/${a.id}/reset-progress`, {
        lookback_days: lookbackDays,
        poll_now: pollNow,
      }, {
        timeout: 180000,
      })
      if (!pollNow) {
        toast.success(`ตั้งช่วงย้อนหลัง ${lookbackDays} วันแล้ว`, { id })
      } else {
        const job = res.data.job
        if (job) {
          setActiveJobs((prev) => {
            const next = prev.filter((x) => x.id !== job.id && x.account_id !== job.account_id)
            return [job, ...next]
          })
        }
        toast.success('ตั้งช่วงย้อนหลังและเริ่มงานดึงอีเมลแล้ว สามารถเปลี่ยนไปเมนูอื่นได้', withPurchaseBillsAction(id))
      }
      fetchAll()
      fetchActiveJobs()
    } catch (e) {
      if (axios.isAxiosError(e)) {
        if (e.response?.status === 409) {
          markBusyHint(a.id)
          toast.warning(
            'กล่องนี้กำลังดึงอีเมลอยู่แล้ว บิลอาจกำลังทยอยเข้า ให้ดูหน้าใบสั่งซื้อหรือรอสถานะอัปเดต',
            withPurchaseBillsAction(id),
          )
          fetchAll()
          return
        }
        if (e.code === 'ECONNABORTED') {
          markBusyHint(a.id)
          toast.warning(
            'คำสั่งอ่านใหม่ใช้เวลานาน ระบบอาจยังทำงานต่ออยู่ บิลจะทยอยเข้าในหน้าใบสั่งซื้อ',
            withPurchaseBillsAction(id),
          )
          fetchAll()
          return
        }
      }
      const msg = axios.isAxiosError(e)
        ? e.response?.data?.error || e.message
        : ''
      if (isPollRunningError(msg)) {
        markBusyHint(a.id)
        toast.warning(friendlyPollError(msg), withPurchaseBillsAction(id))
        return
      }
      toast.error(`ตั้งช่วงย้อนหลังไม่สำเร็จ${msg ? `: ${msg}` : ''}`, { id })
    } finally {
      if (pollNow) {
        setPollingIds((prev) => {
          const next = new Set(prev)
          next.delete(a.id)
          return next
        })
      }
    }
  }

  const handleDelete = async () => {
    if (!deleteId) return
    try {
      await client.delete(`/api/settings/imap-accounts/${deleteId}`)
      toast.success('ลบสำเร็จ')
      fetchAll()
    } catch {
      toast.error('ลบไม่สำเร็จ')
    }
  }

  const headerActions = (
    <Button size="sm" onClick={handleAdd}>
      <Plus className="h-4 w-4" />
      เพิ่มกล่องเมล
    </Button>
  )

  const warningAccounts = accounts.filter((a) => accountState(a) === 'attention')
  const summaryCounts = accounts.reduce(
    (acc, account) => {
      acc.total += 1
      acc[accountState(account)] += 1
      return acc
    },
    { total: 0, ready: 0, backlog: 0, attention: 0, disabled: 0, unknown: 0 },
  )
  const normalizedQuery = query.trim().toLowerCase()
  const filteredAccounts = accounts.filter((account) => {
    const state = accountState(account)
    if (stateFilter !== 'all' && state !== stateFilter) return false
    if (channelFilter !== 'all' && account.channel !== channelFilter) return false
    if (!normalizedQuery) return true
    return [account.name, account.username, account.host, account.channel, account.shopee_domains, statusLabel(account.last_poll_status), pollSummaryText(account)]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(normalizedQuery))
  })
  const activeJobByAccount = new Map(
    activeJobs.filter(isActiveIMAPPollJob).map((job) => [job.account_id, job]),
  )

  return (
    <div className="space-y-5">
      <PageHeader
        title="กล่องอีเมลรับบิล"
        description="กล่องเมลที่ใช้ดึงอีเมล Shopee และสร้างบิลซื้ออัตโนมัติ"
        actions={headerActions}
      />

      <div className="rounded-lg border border-border bg-card p-3 shadow-none">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <Mail className="h-4 w-4 text-accent-strong" />
              <h2 className="text-sm font-semibold text-foreground">Email Intake Readiness</h2>
              <Badge variant={summaryCounts.attention > 0 ? 'destructive' : 'secondary'} className="h-6">
                {summaryCounts.attention > 0 ? 'ต้องตรวจ' : summaryCounts.ready > 0 ? 'พร้อมใช้งาน' : 'ยังไม่มีกล่องพร้อม'}
              </Badge>
            </div>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              ตรวจว่ากล่องอีเมลรับบิลอ่านได้ล่าสุดหรือไม่ ก่อนปล่อยให้ระบบสร้างใบสั่งซื้อจากอีเมล
            </p>
            {instanceContext && (
              <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
                <span>Instance</span>
                <span className="font-medium text-foreground">{instanceContext.instance_name || 'Nexflow'}</span>
                <span className="font-mono">({instanceContext.instance_slug || 'default'})</span>
                {instanceContext.sml_database && (
                  <>
                    <span>· SML tenant</span>
                    <span className="font-mono text-foreground">{instanceContext.sml_database}</span>
                  </>
                )}
              </div>
            )}
          </div>

          <div className="flex flex-wrap gap-1.5">
            <Button size="sm" onClick={handleAdd}>
              <Plus className="h-3.5 w-3.5" />
              เพิ่มกล่องเมล
            </Button>
            {summaryCounts.backlog > 0 && (
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5"
                onClick={handlePollAllBacklog}
                disabled={pollingBacklogAll}
              >
                {pollingBacklogAll ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <PlayCircle className="h-3.5 w-3.5" />}
                เร่งดึง {summaryCounts.backlog.toLocaleString('th-TH')} กล่อง
              </Button>
            )}
          </div>
        </div>

        <div className="mt-3 grid gap-2 sm:grid-cols-3 lg:grid-cols-6">
          <SummaryPill label="ทั้งหมด" value={summaryCounts.total} />
          <SummaryPill label="พร้อมใช้งาน" value={summaryCounts.ready} tone="success" />
          <SummaryPill label="กำลังทยอยอ่าน" value={summaryCounts.backlog} tone="warning" />
          <SummaryPill label="ต้องตรวจ" value={summaryCounts.attention} tone="danger" />
          <SummaryPill label="ปิดใช้งาน" value={summaryCounts.disabled} tone="muted" />
          <SummaryPill label="ยังไม่เคยดึง" value={summaryCounts.unknown} tone="muted" />
        </div>

        {!loading && warningAccounts.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-warning/35 bg-warning/[0.07] px-3 py-2 text-sm text-warning">
            <div className="flex items-center gap-2 font-medium">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>มีกล่องเมลต้องตรวจ {warningAccounts.length.toLocaleString('th-TH')} กล่อง</span>
            </div>
            <Button variant="outline" size="sm" className="h-7 px-2 text-xs" onClick={() => setStateFilter('attention')}>
              ดูเฉพาะที่ต้องตรวจ
            </Button>
          </div>
        )}

        <div className="mt-3">
          <HelpBanner />
        </div>
      </div>

      {!loading && accounts.length === 0 ? (
        <EmptyState
          icon={Mail}
          title="ยังไม่มีกล่องเมล"
          description="เพิ่มกล่องเมล Shopee เพื่อเริ่มดึงอีเมลและสร้างบิลซื้ออัตโนมัติ"
          action={
            <Button onClick={handleAdd}>
              <Plus className="h-4 w-4" />
              เพิ่มกล่องเมลแรก
            </Button>
          }
        />
      ) : (
        <TooltipProvider delayDuration={0}>
          <div className="space-y-3">
            <div className="flex flex-col gap-2 rounded-md border border-border bg-card p-3 sm:flex-row sm:items-center">
              <div className="relative flex-1">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  className="pl-8"
                  placeholder="ค้นหาชื่อกล่อง, อีเมล, host, สถานะ"
                />
              </div>
              <Select value={stateFilter} onValueChange={setStateFilter}>
                <SelectTrigger className="w-full sm:w-44">
                  <SelectValue placeholder="สถานะ" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">ทุกสถานะ</SelectItem>
                  <SelectItem value="ready">พร้อมใช้งาน</SelectItem>
                  <SelectItem value="backlog">กำลังทยอยอ่าน</SelectItem>
                  <SelectItem value="attention">ต้องตรวจ</SelectItem>
                  <SelectItem value="disabled">ปิดใช้งาน</SelectItem>
                  <SelectItem value="unknown">ยังไม่เคยดึง</SelectItem>
                </SelectContent>
              </Select>
              <Select value={channelFilter} onValueChange={setChannelFilter}>
                <SelectTrigger className="w-full sm:w-44">
                  <SelectValue placeholder="ประเภท" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">ทุกประเภท</SelectItem>
                  <SelectItem value="shopee">Shopee</SelectItem>
                  <SelectItem value="general">ไฟล์แนบทั่วไป</SelectItem>
                  <SelectItem value="lazada">Lazada</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              {loading ? (
                <div className="rounded-md border border-border bg-card p-8 text-center text-sm text-muted-foreground">
                  กำลังโหลดรายการกล่องเมล…
                </div>
              ) : filteredAccounts.length === 0 ? (
                <div className="rounded-md border border-border bg-card p-8 text-center text-sm text-muted-foreground">
                  ไม่พบกล่องเมลที่ตรงกับตัวกรอง
                </div>
              ) : (
                filteredAccounts.map((account) => (
                  <EmailAccountRow
                    key={account.id}
                    account={account}
                    job={activeJobByAccount.get(account.id)}
                    polling={pollingIds.has(account.id) || activeJobByAccount.has(account.id) || account.poll_running === true || (busyUntilById[account.id] ?? 0) > Date.now()}
                    onPoll={handlePollNow}
                    onReset={setResetAccount}
                    onEdit={handleEdit}
                    onDelete={setDeleteId}
                    onOpenDetails={setDetailAccount}
                  />
                ))
              )}
            </div>
          </div>
        </TooltipProvider>
      )}

      {accounts.some((a) => a.consecutive_failures != null && a.consecutive_failures >= 3) && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>
            มีกล่องเมลที่ดึงไม่สำเร็จ 3 ครั้งติด — ผู้ดูแลได้รับ LINE แจ้งเตือนแล้ว
            กรุณาตรวจ host, เปิด IMAP, และใช้ App Password 16 ตัวอักษรแทนรหัสผ่าน Gmail ปกติ
          </span>
        </div>
      )}

      <AccountDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        account={editing}
        onSaved={fetchAll}
      />

      <PollDetailsDialog
        account={detailAccount}
        onOpenChange={(open) => !open && setDetailAccount(null)}
      />

      <ResetProgressDialog
        account={resetAccount}
        onOpenChange={(open) => !open && setResetAccount(null)}
        onConfirm={handleResetProgress}
      />

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(o) => !o && setDeleteId(null)}
        title="ลบ inbox นี้?"
        description="หลังลบ inbox จะไม่ถูก poll อีก แต่บิลที่สร้างไว้แล้วยังอยู่"
        variant="destructive"
        confirmLabel="ลบ"
        onConfirm={handleDelete}
      />
    </div>
  )
}
