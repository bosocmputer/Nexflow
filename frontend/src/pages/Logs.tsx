import { useEffect, useMemo, useState, type ComponentType } from 'react'
import { Link } from 'react-router-dom'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/th'
import {
  Activity,
  AlertTriangle,
  Bug,
  ChevronDown,
  CheckCircle2,
  Code2,
  Copy,
  Database,
  FileText,
  Filter,
  Layers3,
  RotateCw,
  ScrollText,
  ShieldAlert,
  Sparkles,
  UserRound,
} from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { EmptyState } from '@/components/common/EmptyState'
import { DateRangePicker } from '@/components/common/DateRangePicker'
import { JsonViewer } from '@/components/common/JsonViewer'
import { PageHeader } from '@/components/common/PageHeader'
import api from '@/api/client'
import { cn } from '@/lib/utils'
import {
  ACTION_META,
  SOURCE_LABELS,
  SOURCE_TONE,
  TONE_DOT,
  humanizeAuditError,
  smlRouteLabel,
  type ActionMeta,
  type AuditLog,
  type Tone,
  summarize,
} from '@/lib/audit-log-meta'

dayjs.extend(relativeTime)
dayjs.locale('th')

interface LogsResponse {
  data: AuditLog[]
  total?: number
  page?: number
  page_size?: number
  limit?: number
  has_more?: boolean
  next_cursor?: string
}

const ALL = '__all__'
const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)
type QuickView = 'all' | 'actionable' | 'sml_failed' | 'imports' | 'mapping' | 'data_quality'
  | 'shopee_settlement'

const LEVEL_LABELS: Record<string, string> = {
  info: 'ข้อมูล',
  warn: 'คำเตือน',
  error: 'ผิดพลาด',
}

const ROLE_LABELS: Record<string, string> = {
  admin: 'ผู้ดูแล',
  staff: 'พนักงาน',
  viewer: 'ดูอย่างเดียว',
  user: 'ผู้ใช้',
  worker: 'งานอัตโนมัติ',
  system: 'ระบบ',
}

function viaLabel(value: unknown): string {
  const text = String(value ?? '')
  const map: Record<string, string> = {
    retry: 'ส่งจากหน้าบิล',
    bulk_job: 'ส่งแบบกลุ่ม',
    import: 'ส่งตอนนำเข้า',
  }
  return map[text] ?? text
}

function isShopeeSettlementLog(log: AuditLog): boolean {
  return log.action.startsWith('shopee_settlement_')
}

function displaySourceKey(log: AuditLog): string {
  if (isShopeeSettlementLog(log)) return 'shopee_settlement'
  return log.source ?? ''
}

// Action keys that belong to Phase 2+ (LINE chat, chat tags, etc.)
const PHASE2_ACTIONS = new Set([
  'line_admin_reply', 'line_admin_send_media', 'line_conversation_status',
  'line_message_received', 'line_oa_created', 'line_oa_updated', 'line_oa_deleted',
  'chat_phone_saved', 'chat_note_created', 'chat_note_updated', 'chat_note_deleted',
  'chat_tag_created', 'chat_tag_updated', 'chat_tag_deleted', 'chat_conv_tags_set',
  'chat_quick_reply_created', 'chat_quick_reply_updated', 'chat_quick_reply_deleted',
])


function relTime(iso: string): string {
  const d = dayjs(iso)
  const diffMin = dayjs().diff(d, 'minute')
  if (diffMin < 60) return d.fromNow()
  if (dayjs().isSame(d, 'day')) return `วันนี้ ${d.format('HH:mm')}`
  if (dayjs().subtract(1, 'day').isSame(d, 'day')) return `เมื่อวาน ${d.format('HH:mm')}`
  return d.format('DD/MM/YY HH:mm')
}

function CopyChip({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      className="group inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground hover:bg-muted/70"
      onClick={(e) => {
        e.stopPropagation()
        navigator.clipboard?.writeText(value)
        setCopied(true)
        setTimeout(() => setCopied(false), 1200)
      }}
      title={`คัดลอก ${label}: ${value}`}
    >
      <span className="text-[9px] uppercase opacity-60">{label}</span>
      <span>{copied ? 'คัดลอกแล้ว' : value.length > 16 ? value.slice(0, 12) + '…' : value}</span>
      <Copy className="h-2.5 w-2.5 opacity-0 transition-opacity group-hover:opacity-100" />
    </button>
  )
}

interface LogFact {
  label: string
  value?: React.ReactNode
  mono?: boolean
  copyValue?: string
  tone?: 'normal' | 'danger' | 'muted'
}

interface LogGuidance {
  title: string
  description: string
  tone: 'warning' | 'danger' | 'info'
}

function parseDetailError(log: AuditLog): Record<string, any> {
  const err = log.detail?.error
  if (err && typeof err === 'object' && !Array.isArray(err)) return err
  if (typeof err !== 'string') return {}
  try {
    const parsed = JSON.parse(err)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function guidanceFor(log: AuditLog): LogGuidance | null {
  const d = log.detail ?? {}
  const parsedError = parseDetailError(log)
  const rawError = parsedError.error ?? d.error ?? ''
  const errorText = String(rawError).toLowerCase()

  if (log.action === 'demo_test_data_reset') {
    return {
      title: 'ไม่ใช่ error: มีการล้างข้อมูลทดสอบ',
      description:
        'รายการนี้เกิดจากผู้ดูแลกดล้างข้อมูลทดสอบในหน้าเริ่มต้นใช้งาน ระบบลบเฉพาะบิล/import/log เดิม และเก็บการตั้งค่า สินค้า SML ตารางจับคู่ และประวัติ AI ไว้ตามค่าเริ่มต้น',
      tone: 'info',
    }
  }

  if (isShopeeSettlementLog(log)) {
    if (log.action === 'shopee_settlement_preview_failed') {
      return {
        title: 'ดึงรอบถอนเงิน Shopee ไม่สำเร็จ',
        description: 'ตรวจช่วงวันที่ release เงิน, token Shopee, และการเชื่อมต่อ SML แล้วลองดึงใหม่อีกครั้ง',
        tone: 'danger',
      }
    }
    if (log.action === 'shopee_settlement_send_blocked') {
      return {
        title: 'ระบบบล็อกการส่งรับชำระ',
        description: 'ระบบยังไม่สร้าง RC เพราะรายการหรือค่าตั้งต้นไม่พร้อม ให้เปิดหน้า รับชำระ Shopee เพื่อตรวจ run แล้วแก้ตามเหตุผลที่แสดง',
        tone: 'warning',
      }
    }
    if (log.action === 'shopee_settlement_sent') {
      return {
        title: 'รับชำระ Shopee ถูกส่งเข้า SML แล้ว',
        description: 'ตรวจเลข RC, จำนวนรายการ, บัญชีรับเงิน และค่าใช้จ่ายส่วนต่างได้จากรายละเอียดด้านล่างหรือเปิดหน้า รับชำระ Shopee เพื่อตรวจ run',
        tone: 'info',
      }
    }
    if (log.action === 'shopee_settlement_defaults_updated') {
      return {
        title: 'มีการแก้ไขค่าตั้งต้นรับชำระ Shopee',
        description: 'ค่าตั้งต้นนี้จะถูกใช้ตอนส่งรับชำระ Shopee รอบถัดไป หากมีส่วนต่างต้องตรวจ expense code ก่อนส่งจริง',
        tone: 'info',
      }
    }
    return {
      title: 'งานรับชำระ Shopee',
      description: 'เปิดหน้า รับชำระ Shopee เพื่อตรวจรายการ, สถานะ run, และเหตุผลที่พร้อมส่งหรือถูกข้าม',
      tone: 'info',
    }
  }

  if (log.action === 'bill_doc_no_regenerate_failed') {
    return {
      title: 'ออกเลขเอกสารใหม่ไม่สำเร็จ',
      description:
        'ให้ตรวจเส้นทางเอกสาร SML, prefix/running format และการเชื่อมต่อ SML API แล้วลองกดออกเลขใหม่อีกครั้ง',
      tone: 'danger',
    }
  }

  if (log.action === 'sml_failed') {
    if (errorText.includes('timeout') || errorText.includes('deadline') || errorText.includes('eof') || errorText.includes('connection refused')) {
      return {
        title: 'ส่งให้ทีมระบบ/SML API ตรวจ: เชื่อมต่อหรือรอคำตอบไม่สำเร็จ',
        description: 'คัดลอก error พร้อม Trace และเวลาที่เกิดเหตุให้ทีมตรวจ network, service SML, หรือ timeout ของ API ก่อน retry ซ้ำ',
        tone: 'danger',
      }
    }
    if (errorText.includes('duplicate key') || errorText.includes('already exists')) {
      return {
        title: 'ผู้ใช้แก้ได้: เลขเอกสาร SML ซ้ำ',
        description: 'เปิดบิลนี้ กดส่งใหม่ แล้วเปลี่ยนเลขเอกสาร SML (doc_no) ก่อนส่งอีกครั้ง',
        tone: 'warning',
      }
    }
    if (errorText.includes('doc_format') || errorText.includes('format_code') || errorText.includes('doc format')) {
      return {
        title: 'ผู้ดูแลตั้งค่าได้: ตรวจรูปแบบเลขเอกสาร',
        description: 'ไปที่เส้นทางเอกสาร SML แล้วตรวจ doc format / prefix / running format ของปลายทางนี้ ก่อนส่งใหม่',
        tone: 'warning',
      }
    }
    if (errorText.includes('customer') || errorText.includes('cust_code') || errorText.includes('supplier') || errorText.includes('party')) {
      return {
        title: 'ผู้ใช้แก้ได้: ตรวจลูกค้าหรือผู้ขาย',
        description: 'เปิดบิลนี้แล้วเลือกลูกค้า/ผู้ขายจาก SML ให้ถูกต้อง ถ้าไม่พบให้ sync รายชื่อคู่ค้าหรือตรวจรหัสใน SML',
        tone: 'warning',
      }
    }
    if (errorText.includes('warehouse') || errorText.includes('wh_code') || errorText.includes('shelf')) {
      return {
        title: 'ผู้ใช้แก้ได้: ตรวจคลังหรือพื้นที่เก็บ',
        description: 'เปิดบิลนี้แล้วกรอกรหัสคลังและพื้นที่เก็บให้ตรงกับ SML ก่อนส่งใหม่',
        tone: 'warning',
      }
    }
    if (errorText.includes('vat') || errorText.includes('tax')) {
      return {
        title: 'ผู้ใช้แก้ได้: ตรวจประเภทภาษีและอัตราภาษี',
        description: 'เปิดบิลนี้แล้วเลือก VAT type และ VAT rate ให้ตรงกับเอกสาร/นโยบายร้านก่อนส่งใหม่',
        tone: 'warning',
      }
    }
    if (errorText.includes('item') || errorText.includes('สินค้า') || errorText.includes('unit')) {
      return {
        title: 'ผู้ใช้แก้ได้: ตรวจสินค้าและหน่วย',
        description: 'ตรวจรหัสสินค้าและหน่วยในหน้ารายละเอียดบิล ถ้ารหัสไม่มีใน SML ให้ซิงก์หรือสร้างสินค้าให้เรียบร้อยก่อน',
        tone: 'warning',
      }
    }
    return {
      title: 'ส่งให้ทีมดูแลระบบตรวจ',
      description: 'คัดลอก error พร้อมเลขเอกสาร SML และ Trace ให้ทีมดูแลระบบหรือทีม SML API ตรวจต่อ',
      tone: 'danger',
    }
  }

  if (log.level === 'error') {
    if (errorText.includes('authenticationfailed') || errorText.includes('invalid credentials') || errorText.includes('app password')) {
      return {
        title: 'ผู้ดูแลแก้ได้: รหัสผ่านอีเมลหรือ App Password ไม่ถูกต้อง',
        description: 'ไปที่กล่องอีเมลรับบิล ตรวจ 2-Step Verification, Gmail App Password 16 ตัว และสถานะ IMAP ก่อนทดสอบเชื่อมต่อใหม่',
        tone: 'warning',
      }
    }
    if (errorText.includes('openrouter') || errorText.includes('ai') || errorText.includes('quota') || errorText.includes('credit')) {
      return {
        title: 'ผู้ดูแลแก้ได้: ตรวจเครดิตหรือการเชื่อมต่อ AI',
        description: 'ไปที่การเชื่อมต่อระบบหรือการใช้งาน AI แล้วตรวจ API key, model, quota/credit และ retry งานที่อ่านไม่สำเร็จ',
        tone: 'warning',
      }
    }
    return {
      title: 'ต้องให้ผู้ดูแลระบบตรวจ',
      description: 'เหตุการณ์นี้เป็น error ของระบบ ให้แนบ Trace หรือข้อมูลดิบในรายการนี้เวลาส่งต่อทีมดูแล',
      tone: 'danger',
    }
  }

  if (log.level === 'warn' || String(d.error ?? '').includes('ถูกข้าม')) {
    return {
      title: 'ควรตรวจการตั้งค่า',
      description: 'ระบบยังทำงานได้ แต่มีข้อมูลบางส่วนถูกข้ามหรืออ่านไม่ครบ ให้ตรวจ filter, ผู้ส่งอีเมล หรือไฟล์ต้นทาง',
      tone: 'warning',
    }
  }

  return null
}

function compact(value: unknown, max = 90): string {
  if (value == null || value === '') return ''
  const text = String(value)
  return text.length > max ? `${text.slice(0, max)}…` : text
}

function cleanDocNo(value: unknown): string {
  return String(value ?? '')
    .normalize('NFKC')
    .replace(/[\u0300-\u036f\u0E31-\u0E4E\u200B-\u200D\uFEFF]/g, '')
    .trim()
}

function docNoCandidates(log: AuditLog): string[] {
  const d = log.detail ?? {}
  const parsedError = parseDetailError(log)
  const payload = (d.sml_payload && typeof d.sml_payload === 'object') ? d.sml_payload : {}
  return [
    d.doc_no,
    d.rc_doc_no,
    d.receipt_doc_no,
    parsedError.doc_no_attempted,
    parsedError.doc_no,
    payload.doc_no,
  ]
    .filter((v) => v != null && String(v) !== '')
    .map(String)
}

function hasDocNoQualityIssue(log: AuditLog): boolean {
  return docNoCandidates(log).some((docNo) => cleanDocNo(docNo) !== docNo.trim())
}

function primaryDocNo(log: AuditLog): string {
  return docNoCandidates(log)[0] ?? ''
}

function isImportLog(log: AuditLog): boolean {
  return log.action.includes('import') || log.action === 'bill_created'
}

function platformLabel(logs: AuditLog[]): string {
  const source = logs.find((l) => l.source)?.source
  if (source?.includes('tiktok')) return 'TikTok'
  if (source?.includes('lazada')) return 'Lazada'
  return 'Shopee'
}

function orderIdOf(log: AuditLog): string {
  const d = log.detail ?? {}
  return String(d.order_id ?? d.shopee_order_id ?? d.tiktok_order_id ?? d.lazada_order_id ?? '')
}

function actorName(log: AuditLog): string {
  if (log.actor?.name) {
    const normalized = log.actor.name.toLowerCase()
    if (normalized === 'system') return 'ระบบ'
    if (normalized === 'email worker') return 'ระบบอ่านอีเมล'
    if (normalized === 'unknown user') return 'ผู้ใช้ไม่ทราบชื่อ'
    return log.actor.name
  }
  if (log.user_id) return 'ผู้ใช้ไม่ทราบชื่อ'
  if (log.source === 'email' || log.source === 'shopee_email' || log.source === 'shopee_shipped') return 'ระบบอ่านอีเมล'
  return 'ระบบ'
}

function actorRoleLabel(log: AuditLog): string {
  if (log.actor?.type === 'user') return ROLE_LABELS[log.actor.role ?? 'user'] ?? log.actor.role ?? 'ผู้ใช้'
  if (log.actor?.type === 'worker') return 'งานอัตโนมัติ'
  return 'ระบบ'
}

function makeFacts(log: AuditLog): LogFact[] {
  const d = log.detail ?? {}
  const parsedError = parseDetailError(log)
  const facts: LogFact[] = []

  if (log.target_id && isShopeeSettlementLog(log)) {
    facts.push({
      label: 'Settlement run',
      value: (
        <Link
          to="/shopee-settlements"
          className="font-mono text-primary hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          {log.target_id.slice(0, 8)}…
        </Link>
      ),
      copyValue: log.target_id,
    })
  } else if (log.target_id) {
    facts.push({
      label: 'บิล',
      value: (
        <Link
          to={`/bills/${log.target_id}`}
          className="font-mono text-primary hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          {log.target_id.slice(0, 8)}…
        </Link>
      ),
      copyValue: log.target_id,
    })
  }

  facts.push({
    label: 'ผู้ทำรายการ',
    value: `${actorName(log)}${log.actor?.email ? ` · ${log.actor.email}` : ''}`,
    tone: log.actor?.type === 'user' ? 'normal' : 'muted',
  })

  const docNo = d.doc_no ?? d.rc_doc_no ?? d.receipt_doc_no ?? parsedError.doc_no_attempted
  if (docNo) facts.push({ label: 'เลขเอกสาร SML', value: docNo, mono: true, copyValue: String(docNo) })
  if (d.route ?? parsedError.route) {
    const route = d.route ?? parsedError.route
    facts.push({ label: 'ปลายทาง SML', value: smlRouteLabel(route), mono: false })
  }
  if (d.via) facts.push({ label: 'วิธีส่ง', value: viaLabel(d.via), mono: false })
  if (d.subject) facts.push({ label: 'หัวข้ออีเมล', value: compact(d.subject, 140) })
  if (d.message_id) facts.push({ label: 'Message ID', value: compact(d.message_id, 64), mono: true, copyValue: String(d.message_id) })
  if (d.raw_name) facts.push({ label: 'ชื่อจากบิล', value: compact(d.raw_name, 140) })
  if (d.item_code ?? d.code) facts.push({ label: 'รหัสสินค้า', value: d.item_code ?? d.code, mono: true, copyValue: String(d.item_code ?? d.code) })
  if (d.unit_code) facts.push({ label: 'หน่วย', value: d.unit_code })
  if (d.name) facts.push({ label: 'ชื่อสินค้า', value: compact(d.name, 140) })
  if (d.channel) facts.push({ label: 'ช่องทาง', value: d.channel })
  if (d.bill_type) facts.push({ label: 'ประเภทบิล', value: d.bill_type })
  if (d.party_code) facts.push({ label: 'คู่ค้า', value: d.party_code, mono: true })
  if (log.duration_ms != null) facts.push({ label: 'เวลาใช้', value: `${log.duration_ms.toLocaleString()}ms`, mono: true })
  if (log.trace_id) facts.push({ label: 'Trace', value: <CopyChip value={log.trace_id} label="trace" /> })

  if (log.action === 'demo_test_data_reset') {
    const beforeDocuments = d.before_documents && typeof d.before_documents === 'object' ? d.before_documents : {}
    const beforeImports = d.before_imports && typeof d.before_imports === 'object' ? d.before_imports : {}
    const beforeTotal = Number(beforeDocuments.total ?? 0)
    const beforePurchase = Number(beforeDocuments.purchase ?? 0)
    const beforeSaleOrder = Number(beforeDocuments.saleorder ?? 0)
    const beforeSaleInvoice = Number(beforeDocuments.saleinvoice ?? 0)
    const beforeLogs = Number(d.before_logs ?? beforeImports.audit_logs ?? 0)
    const preserved: string[] = []
    if (d.preserved_settings) preserved.push('การตั้งค่า')
    if (d.preserved_catalog) preserved.push('สินค้า SML')
    if (d.preserved_mappings) preserved.push('ตารางจับคู่')
    if (d.preserved_ai_usage_log) preserved.push('ประวัติ AI')

    facts.push({ label: 'บิลก่อนล้าง', value: `${beforeTotal.toLocaleString()} ใบ` })
    facts.push({
      label: 'แยกตามงาน',
      value: `ซื้อ ${beforePurchase.toLocaleString()} · ใบสั่งขาย ${beforeSaleOrder.toLocaleString()} · ขายสินค้า ${beforeSaleInvoice.toLocaleString()}`,
    })
    facts.push({ label: 'ประวัติเดิม', value: `${beforeLogs.toLocaleString()} รายการ` })
    facts.push({ label: 'เก็บข้อมูลไว้', value: preserved.length ? preserved.join(', ') : 'ไม่มีข้อมูลที่ระบุ' })
    facts.push({ label: 'เลขรันเอกสาร', value: d.reset_doc_counter ? 'รีเซ็ตแล้ว' : 'ไม่ได้รีเซ็ต' })
    facts.push({ label: 'ประวัติอีเมลซ้ำ', value: d.reset_email_dedup ? 'ล้างแล้ว' : 'ไม่ได้ล้าง' })
    facts.push({ label: 'ตำแหน่งอ่านอีเมล', value: d.reset_email_cursor ? 'ย้อนกลับไปอ่านเมลเก่าได้' : 'ไม่ได้รีเซ็ต' })
  }

  if (isShopeeSettlementLog(log)) {
    if (d.run_id && !log.target_id) facts.push({ label: 'Settlement run', value: String(d.run_id).slice(0, 8) + '…', mono: true, copyValue: String(d.run_id) })
    if (d.shop_label || d.shop_id) facts.push({ label: 'ร้าน Shopee', value: [d.shop_label, d.shop_id ? `ร้าน ${d.shop_id}` : ''].filter(Boolean).join(' · ') })
    if (d.release_date_from || d.release_date_to) {
      facts.push({
        label: 'ช่วง release เงิน',
        value: [d.release_date_from, d.release_date_to].filter(Boolean).join(' - '),
        mono: true,
      })
    }
    if (d.total_count != null) facts.push({ label: 'รายการทั้งหมด', value: `${Number(d.total_count).toLocaleString('th-TH')} รายการ` })
    if (d.ready_count != null) facts.push({ label: 'พร้อมส่ง', value: `${Number(d.ready_count).toLocaleString('th-TH')} รายการ` })
    if (d.blocked_count != null) facts.push({ label: 'ต้องตรวจ/ถูกข้าม', value: `${Number(d.blocked_count).toLocaleString('th-TH')} รายการ`, tone: Number(d.blocked_count) > 0 ? 'muted' : 'normal' })
    if (d.sent_count != null) facts.push({ label: 'ส่งรับชำระ', value: `${Number(d.sent_count).toLocaleString('th-TH')} รายการ` })
    if (d.newly_blocked != null) facts.push({ label: 'Block เพิ่ม', value: `${Number(d.newly_blocked).toLocaleString('th-TH')} รายการ`, tone: Number(d.newly_blocked) > 0 ? 'muted' : 'normal' })
    if (d.blocked_after_reconcile_count != null) {
      facts.push({
        label: 'ถูก block หลังตรวจซ้ำ',
        value: `${Number(d.blocked_after_reconcile_count).toLocaleString('th-TH')} รายการ`,
        tone: Number(d.blocked_after_reconcile_count) > 0 ? 'muted' : 'normal',
      })
    }
    if (d.doc_format_code) facts.push({ label: 'รูปแบบเอกสาร', value: d.doc_format_code, mono: true })
    if (d.passbook_code) facts.push({ label: 'บัญชีรับเงิน', value: [d.passbook_code, d.passbook_name].filter(Boolean).join(' · ') })
    if (d.expense_code) facts.push({ label: 'ค่าใช้จ่ายส่วนต่าง', value: [d.expense_code, d.expense_name].filter(Boolean).join(' · ') })
    if (d.message) facts.push({ label: 'ข้อความ', value: compact(humanizeAuditError(d.message), 180) })
    if (d.error) facts.push({ label: 'ข้อผิดพลาด', value: compact(humanizeAuditError(d.error), 180), tone: 'danger' })
  }

  return facts.filter((fact) => fact.value != null && fact.value !== '')
}

function LogExpandedSummary({
  log,
  onRetry,
  retrying,
  canRetry,
  devMode,
}: {
  log: AuditLog
  onRetry: (e: React.MouseEvent) => void
  retrying: boolean
  canRetry: boolean
  devMode: boolean
}) {
  const d = log.detail ?? {}
  const parsedError = parseDetailError(log)
  const errorMessage = humanizeAuditError(parsedError.error ?? d.error)
  const facts = makeFacts(log)
  const guidance = guidanceFor(log)
  const isSmlSent = log.action === 'sml_sent'
  const isSmlFailed = log.action === 'sml_failed'
  const docNoIssue = hasDocNoQualityIssue(log)
  const originalDocNo = primaryDocNo(log)
  const fixedDocNo = cleanDocNo(originalDocNo)
  const devPayload = {
    id: log.id,
    action: log.action,
    source: log.source,
    level: log.level,
    actor: log.actor,
    target_id: log.target_id,
    trace_id: log.trace_id,
    created_at: log.created_at,
    duration_ms: log.duration_ms,
    detail: log.detail ?? {},
  }

  const copyDevInfo = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard?.writeText(JSON.stringify(devPayload, null, 2))
    toast.success('คัดลอกข้อมูลสำหรับ DEV แล้ว')
  }

  return (
    <div className="space-y-2">
      {docNoIssue && (
        <div className="flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2">
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
          <div className="min-w-0">
            <div className="text-sm font-semibold text-warning">พบเลขเอกสารมีอักขระแปลก</div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              เลขที่เห็นอาจดูเหมือนถูกต้อง แต่มีอักขระซ่อนหรือวรรณยุกต์ติดหน้าเลขเอกสาร ควรแก้เป็น
              <span className="mx-1 font-mono font-semibold text-foreground">{fixedDocNo}</span>
              ก่อนส่งซ้ำหรือเทียบกับ SML
            </p>
          </div>
        </div>
      )}

      {isSmlSent && (
        <div className="flex items-start gap-2 rounded-md border border-success/25 bg-success/10 px-3 py-2">
          <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
          <div className="min-w-0">
            <div className="text-sm font-semibold text-foreground">ส่งเข้า SML สำเร็จ</div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              ระบบบันทึกเลขเอกสารและ response ไว้แล้ว ตรวจต่อได้จากหน้ารายละเอียดบิล
            </p>
          </div>
        </div>
      )}

      {isSmlFailed && (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2">
          <div className="flex items-start justify-between gap-3">
            <div className="flex min-w-0 gap-2">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
              <div className="min-w-0">
                <div className="text-sm font-semibold text-destructive">ส่งเข้า SML ไม่สำเร็จ</div>
                {errorMessage && (
                  <p className="mt-1 whitespace-pre-wrap break-words text-xs text-destructive">
                    {compact(errorMessage, 260)}
                  </p>
                )}
              </div>
            </div>
            <div className="flex shrink-0 flex-col gap-1.5">
              {canRetry && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={onRetry}
                  disabled={retrying}
                  className="h-7 shrink-0 gap-1.5 px-2 text-[11px]"
                >
                  <RotateCw className={cn('h-3 w-3', retrying && 'animate-spin')} />
                  {retrying ? 'กำลังส่งซ้ำ…' : 'ส่งซ้ำ'}
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                onClick={copyDevInfo}
                className="h-7 gap-1.5 px-2 text-[11px]"
              >
                <Copy className="h-3 w-3" />
                ส่งให้ DEV
              </Button>
            </div>
          </div>
        </div>
      )}

      {!isSmlSent && !isSmlFailed && (log.action.includes('email') || log.action.includes('shopee')) && (
        <div className="flex items-start gap-2 rounded-md border bg-background px-3 py-2">
          <FileText className="mt-0.5 h-4 w-4 shrink-0 text-info" />
          <div className="min-w-0">
            <div className="text-sm font-semibold text-foreground">ข้อมูลจากช่องทางต้นทาง</div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              ใช้ดูว่าอีเมลหรือไฟล์ใดเป็นต้นทางของบิล และใช้ trace กลับตอนตรวจซ้ำ
            </p>
          </div>
        </div>
      )}

      {guidance && (
        <div
          className={cn(
            'flex items-start gap-2 rounded-md border px-3 py-2',
            guidance.tone === 'danger'
              ? 'border-destructive/30 bg-destructive/10'
              : guidance.tone === 'warning'
                ? 'border-warning/30 bg-warning/10'
                : 'border-info/25 bg-info/10',
          )}
        >
          <AlertTriangle
            className={cn(
              'mt-0.5 h-4 w-4 shrink-0',
              guidance.tone === 'danger'
                ? 'text-destructive'
                : guidance.tone === 'warning'
                  ? 'text-warning'
                  : 'text-info',
            )}
          />
          <div className="min-w-0">
            <div
              className={cn(
                'text-sm font-semibold',
                guidance.tone === 'danger'
                  ? 'text-destructive'
                  : guidance.tone === 'warning'
                    ? 'text-warning'
                    : 'text-foreground',
              )}
            >
              {guidance.title}
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">{guidance.description}</p>
          </div>
        </div>
      )}

      <div className="grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3">
        {facts.map((fact, idx) => (
          <div key={`${fact.label}-${idx}`} className="rounded-md border bg-background px-2.5 py-1.5">
            <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
              {fact.label}
            </div>
            <div
              className={cn(
                'mt-0.5 min-w-0 break-words text-sm leading-5 text-foreground',
                fact.mono && 'font-mono text-xs',
                fact.tone === 'danger' && 'text-destructive',
                fact.tone === 'muted' && 'text-muted-foreground',
              )}
            >
              {fact.value}
            </div>
            {fact.copyValue && fact.label !== 'Trace' && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  navigator.clipboard?.writeText(fact.copyValue ?? '')
                }}
                className="mt-1 text-[10px] text-muted-foreground hover:text-foreground"
              >
                คัดลอก
              </button>
            )}
          </div>
        ))}
      </div>

      {log.action === 'sml_sent' && (
        <div className="flex items-center gap-2 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          <Database className="h-3.5 w-3.5" />
          <span>ดูข้อมูลที่ส่งและผลตอบกลับฉบับเต็มได้ในหน้ารายละเอียดบิล</span>
        </div>
      )}

      {devMode && (
        <div className="flex flex-wrap items-center gap-2 rounded-md border border-dashed bg-background px-3 py-2">
          <Bug className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-xs text-muted-foreground">ข้อมูลเทคนิค:</span>
          {log.trace_id && <CopyChip value={log.trace_id} label="trace" />}
          {log.target_id && <CopyChip value={log.target_id} label={isShopeeSettlementLog(log) ? 'run' : 'bill'} />}
          {originalDocNo && <CopyChip value={originalDocNo} label="doc" />}
          <Button variant="ghost" size="sm" onClick={copyDevInfo} className="h-6 gap-1 px-2 text-[11px]">
            <Copy className="h-3 w-3" />
            คัดลอก JSON
          </Button>
        </div>
      )}
    </div>
  )
}

function LogRow({ log, onRetried, devMode }: { log: AuditLog; onRetried: () => void; devMode: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const [showRaw, setShowRaw] = useState(false)
  const [retrying, setRetrying] = useState(false)

  const meta = ACTION_META[log.action] ?? {
    label: log.action,
    emoji: '•',
    tone: 'muted' as Tone,
  }
  const summary = summarize(log)
  const isError = log.level === 'error'
  const source = displaySourceKey(log)
  const docNo = primaryDocNo(log)
  const docNoIssue = hasDocNoQualityIssue(log)
  // Inline retry available only on sml_failed rows that have a bill target.
  const canRetry = log.action === 'sml_failed' && !!log.target_id

  const handleRetry = async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!log.target_id || retrying) return
    setRetrying(true)
    try {
      await api.post(`/api/bills/${log.target_id}/retry`)
      toast.success('ส่งใหม่สำเร็จ — โหลด log ใหม่')
      onRetried()
    } catch (err: any) {
      toast.error(
        'ส่งซ้ำไม่สำเร็จ: ' +
          (err?.response?.data?.error ?? err?.message ?? 'unknown'),
      )
    } finally {
      setRetrying(false)
    }
  }

  return (
    <div
      className={cn(
        'rounded-md border bg-card transition-colors',
        isError
          ? 'border-destructive/25 border-l-4 bg-card'
          : expanded
            ? 'border-primary/25 bg-primary/[0.025]'
            : 'border-border hover:bg-accent/30',
      )}
    >
      {/* Row is a div not a button so we can nest a Retry <button> inside
          (HTML doesn't allow button-in-button). Keyboard a11y: Enter/Space
          toggle expanded, role=button + tabIndex for screen readers. */}
      <div
        role="button"
        tabIndex={0}
        onClick={() => setExpanded((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setExpanded((v) => !v)
          }
        }}
        className="flex w-full cursor-pointer items-start gap-2.5 px-3 py-2 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        <span
          className={cn(
            'mt-1 inline-block h-2 w-2 shrink-0 rounded-full',
            TONE_DOT[meta.tone],
          )}
        />

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-sm font-medium text-foreground">{meta.label}</span>
            <ActorBadge log={log} />
            {source && SOURCE_LABELS[source] && (
              <Badge
                variant="secondary"
                className={cn(
                  'h-5 px-1.5 text-[10px] font-medium',
                  SOURCE_TONE[source] ?? 'bg-muted text-muted-foreground',
                )}
              >
                {SOURCE_LABELS[source]}
              </Badge>
            )}
            {docNo && (
              <span className="font-mono text-[11px] font-medium text-foreground">
                {docNo}
              </span>
            )}
            {docNoIssue && (
              <Badge
                variant="secondary"
                className="h-5 bg-warning/15 px-1.5 text-[10px] font-medium text-warning"
              >
                doc_no ผิดรูปแบบ
              </Badge>
            )}
            {/* Delivery-method chip for LINE outgoing — tells admin at a glance
                whether the message used the free Reply API or paid Push quota. */}
            {(log.action === 'line_admin_reply' || log.action === 'line_admin_send_media') &&
              log.detail?.delivery_method === 'reply' && (
                <Badge
                  variant="secondary"
                  className="h-5 px-1.5 text-[10px] font-medium bg-success/15 text-success"
                  title="ส่งผ่าน Reply API — ไม่นับ quota"
                >
                  ฟรี
                </Badge>
              )}
            {(log.action === 'line_admin_reply' || log.action === 'line_admin_send_media') &&
              log.detail?.delivery_method === 'push' && (
                <Badge
                  variant="secondary"
                  className="h-5 px-1.5 text-[10px] font-medium"
                  title="ส่งผ่าน Push API — นับ quota เดือนนี้"
                >
                  Push
                </Badge>
              )}
            {log.level && log.level !== 'info' && (
              <Badge
                variant={isError ? 'destructive' : 'secondary'}
                className="h-5 px-1.5 text-[10px] font-medium uppercase"
              >
                {LEVEL_LABELS[log.level] ?? log.level}
              </Badge>
            )}
          </div>
          {summary && (
            <p
              className={cn(
                'mt-0.5 line-clamp-1 text-xs',
                isError ? 'text-destructive' : 'text-muted-foreground',
              )}
            >
              {summary}
            </p>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {/* Inline retry — visible at row level (not just expanded) on
              sml_failed rows. Saves the click to expand + the trip to
              /bills/:id. Stop click bubbling so the row doesn't toggle. */}
          {canRetry && !expanded && (
            <Tooltip delayDuration={300}>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleRetry}
                  disabled={retrying}
                  className="h-7 w-7 shrink-0 p-0 text-destructive hover:bg-destructive/10 hover:text-destructive"
                >
                  <RotateCw className={cn('h-3.5 w-3.5', retrying && 'animate-spin')} />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="left">
                ส่งซ้ำบิลนี้
              </TooltipContent>
            </Tooltip>
          )}
          <div className="flex flex-col items-end gap-0.5 text-right">
            <Tooltip delayDuration={300}>
              <TooltipTrigger asChild>
                <span className="text-[11px] tabular-nums text-muted-foreground">
                  {relTime(log.created_at)}
                </span>
              </TooltipTrigger>
              <TooltipContent side="left">
                {dayjs(log.created_at).format('DD/MM/YYYY HH:mm:ss')}
              </TooltipContent>
            </Tooltip>
            {log.duration_ms != null && (
              <span
                className={cn(
                  'font-mono text-[10px] tabular-nums',
                  log.duration_ms > 30000
                    ? 'text-destructive'
                    : log.duration_ms > 10000
                      ? 'text-warning'
                      : 'text-muted-foreground/70',
                )}
              >
                {log.duration_ms}ms
              </span>
            )}
            <ChevronDown
              className={cn(
                'h-3.5 w-3.5 text-muted-foreground transition-transform',
                expanded && 'rotate-180',
              )}
            />
          </div>
        </div>
      </div>

      {expanded && (
        <div className="space-y-2 border-t border-border bg-muted/20 px-3 py-2.5">
          <LogExpandedSummary
            log={log}
            onRetry={handleRetry}
            retrying={retrying}
            canRetry={canRetry}
            devMode={devMode}
          />

          {devMode && (
          <div>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                setShowRaw((v) => !v)
              }}
              className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
            >
              <Code2 className="h-3 w-3" />
              {showRaw ? 'ซ่อนข้อมูลดิบ' : 'ดูข้อมูลดิบ JSON'}
            </button>
            {showRaw && (
              <div className="mt-2">
                <JsonViewer title="detail" data={log.detail ?? {}} />
              </div>
            )}
          </div>
          )}
        </div>
      )}
    </div>
  )
}

interface DateGroup {
  key: string
  label: string
  items: AuditLog[]
}

interface ImportGroup {
  kind: 'import'
  key: string
  logs: AuditLog[]
  created: AuditLog[]
  done?: AuditLog
  preview?: AuditLog
}

type ActivityItem =
  | { kind: 'log'; key: string; log: AuditLog }
  | ImportGroup

function buildActivityItems(logs: AuditLog[]): ActivityItem[] {
  const groups = new Map<string, AuditLog[]>()
  for (const log of logs) {
    if (!isImportLog(log) || !log.trace_id) continue
    const list = groups.get(log.trace_id) ?? []
    list.push(log)
    groups.set(log.trace_id, list)
  }

  const used = new Set<string>()
  const items: ActivityItem[] = []
  for (const log of logs) {
    const groupLogs = log.trace_id ? groups.get(log.trace_id) : undefined
    if (groupLogs && groupLogs.length > 1 && !used.has(log.trace_id ?? '')) {
      const trace = log.trace_id ?? log.id
      used.add(trace)
      items.push({
        kind: 'import',
        key: trace,
        logs: groupLogs,
        created: groupLogs.filter((l) => l.action === 'bill_created'),
        done: groupLogs.find((l) => l.action.endsWith('_import_done')),
        preview: groupLogs.find((l) => l.action.endsWith('_import_preview')),
      })
      continue
    }
    if (log.trace_id && used.has(log.trace_id)) continue
    items.push({ kind: 'log', key: log.id, log })
  }
  return items
}

function ImportGroupCard({
  group,
  devMode,
  onRetried,
}: {
  group: ImportGroup
  devMode: boolean
  onRetried: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const platform = platformLabel(group.logs)
  const doneDetail = group.done?.detail ?? {}
  const previewDetail = group.preview?.detail ?? {}
  const success = Number(doneDetail.success_count ?? group.created.length ?? 0)
  const failed = Number(doneDetail.fail_count ?? 0)
  const total = Number(doneDetail.total ?? previewDetail.total_orders ?? success + failed)
  const skipped = Number(previewDetail.skipped_count ?? Math.max(total - success - failed, 0))
  const orders = group.created.map(orderIdOf).filter(Boolean)
  const firstAt = group.logs[group.logs.length - 1]?.created_at ?? group.logs[0]?.created_at

  return (
    <div className="rounded-md border border-info/20 bg-info/[0.035]">
      <div
        role="button"
        tabIndex={0}
        onClick={() => setExpanded((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setExpanded((v) => !v)
          }
        }}
        className="flex cursor-pointer items-start gap-3 px-3 py-2.5"
      >
        <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-info/10 text-info">
          <Layers3 className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-foreground">นำเข้า {platform}</span>
            <Badge variant="secondary" className="h-5 bg-success/15 px-1.5 text-[10px] text-success">
              สร้าง {success}
            </Badge>
            {failed > 0 && (
              <Badge variant="secondary" className="h-5 bg-destructive/10 px-1.5 text-[10px] text-destructive">
                ล้มเหลว {failed}
              </Badge>
            )}
            {skipped > 0 && (
              <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                ข้าม {skipped}
              </Badge>
            )}
            {group.logs[0]?.trace_id && <CopyChip value={group.logs[0].trace_id ?? ''} label="trace" />}
          </div>
          <p className="mt-1 line-clamp-1 text-xs text-muted-foreground">
            {previewDetail.filename ? `${previewDetail.filename} · ` : ''}
            พบ {total.toLocaleString()} order
            {orders.length ? ` · ${orders.slice(0, 4).join(', ')}${orders.length > 4 ? '…' : ''}` : ''}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2 text-right">
          <span className="text-[11px] tabular-nums text-muted-foreground">{relTime(firstAt)}</span>
          <ChevronDown className={cn('h-3.5 w-3.5 text-muted-foreground transition-transform', expanded && 'rotate-180')} />
        </div>
      </div>

      {expanded && (
        <div className="space-y-2 border-t bg-muted/20 px-3 py-2.5">
          <div className="grid gap-2 sm:grid-cols-4">
            <MiniMetric label="พบ" value={total} tone="info" />
            <MiniMetric label="สร้างบิล" value={success} tone="success" />
            <MiniMetric label="ข้าม" value={skipped} tone={skipped > 0 ? 'warning' : 'muted'} />
            <MiniMetric label="ล้มเหลว" value={failed} tone={failed > 0 ? 'danger' : 'muted'} />
          </div>
          <div className="space-y-1.5">
            {group.created.map((log) => (
              <LogRow key={log.id} log={log} onRetried={onRetried} devMode={devMode} />
            ))}
            {devMode && group.logs.filter((log) => log.action !== 'bill_created').map((log) => (
              <LogRow key={log.id} log={log} onRetried={onRetried} devMode={devMode} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function MiniMetric({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: Tone
}) {
  const cls =
    tone === 'success'
      ? 'border-success/25 bg-success/10 text-success'
      : tone === 'danger'
        ? 'border-destructive/25 bg-destructive/10 text-destructive'
        : tone === 'warning'
          ? 'border-warning/25 bg-warning/10 text-warning'
          : tone === 'info'
            ? 'border-info/25 bg-info/10 text-info'
            : 'border-border bg-background text-muted-foreground'
  return (
    <div className={cn('rounded-md border px-3 py-2', cls)}>
      <div className="text-[10px] font-semibold uppercase tracking-wide opacity-80">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums">{value.toLocaleString()}</div>
    </div>
  )
}

function groupByDate(logs: AuditLog[]): DateGroup[] {
  const today = dayjs().startOf('day')
  const yesterday = today.subtract(1, 'day')
  const groups: Record<string, DateGroup> = {}

  for (const log of logs) {
    const d = dayjs(log.created_at).startOf('day')
    let key: string
    let label: string
    if (d.isSame(today)) {
      key = 'today'
      label = 'วันนี้'
    } else if (d.isSame(yesterday)) {
      key = 'yesterday'
      label = 'เมื่อวาน'
    } else {
      key = d.format('YYYY-MM-DD')
      label = d.format('D MMM YYYY')
    }
    if (!groups[key]) groups[key] = { key, label, items: [] }
    groups[key].items.push(log)
  }

  return Object.values(groups)
}

function quickViewMatch(log: AuditLog, quickView: QuickView): boolean {
  switch (quickView) {
    case 'actionable':
      return log.level === 'error' || log.level === 'warn' || hasDocNoQualityIssue(log)
    case 'sml_failed':
      return log.action === 'sml_failed'
    case 'imports':
      return isImportLog(log)
    case 'shopee_settlement':
      return isShopeeSettlementLog(log)
    case 'mapping':
      return log.action === 'mapping_feedback'
    case 'data_quality':
      return hasDocNoQualityIssue(log)
    default:
      return true
  }
}

function SummaryButton({
  label,
  value,
  icon: Icon,
  active,
  tone,
  onClick,
}: {
  label: string
  value: number
  icon: ComponentType<{ className?: string }>
  active: boolean
  tone: Tone
  onClick: () => void
}) {
  const toneClass =
    tone === 'danger'
      ? 'text-destructive'
      : tone === 'warning'
        ? 'text-warning'
        : tone === 'success'
          ? 'text-success'
          : tone === 'info'
            ? 'text-info'
            : tone === 'primary'
              ? 'text-primary'
              : 'text-muted-foreground'
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex min-w-[132px] items-center gap-3 rounded-md border bg-card px-3 py-2 text-left transition-colors hover:bg-accent/40',
        active && 'border-primary/40 bg-primary/[0.04]',
      )}
    >
      <span className={cn('flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted', toneClass)}>
        <Icon className="h-4 w-4" />
      </span>
      <span className="min-w-0">
        <span className="block text-[11px] text-muted-foreground">{label}</span>
        <span className="block text-lg font-semibold leading-5 tabular-nums text-foreground">
          {value.toLocaleString()}
        </span>
      </span>
    </button>
  )
}

function ActorBadge({ log }: { log: AuditLog }) {
  return (
    <Badge
      variant="secondary"
      className={cn(
        'h-5 gap-1 px-1.5 text-[10px] font-medium',
        log.actor?.type === 'user' ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground',
      )}
      title={log.actor?.email || actorName(log)}
    >
      <UserRound className="h-3 w-3" />
      {actorName(log)}
      <span className="opacity-60">· {actorRoleLabel(log)}</span>
    </Badge>
  )
}

export default function Logs() {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState<number | null>(null)
  const [nextCursor, setNextCursor] = useState('')
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const [source, setSource] = useState<string>(ALL)
  const [action, setAction] = useState<string>(ALL)
  const [level, setLevel] = useState<string>(ALL)
  const [userID, setUserID] = useState<string>(ALL)
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [quickView, setQuickView] = useState<QuickView>('all')
  const [devMode, setDevMode] = useState(false)
  const pageSize = 50

  const load = async (opts: { cursor?: string; append?: boolean; includeTotal?: boolean } = {}) => {
    setLoading(true)
    try {
      const params: Record<string, string | number | boolean> = { limit: pageSize }
      if (opts.cursor) params.cursor = opts.cursor
      if (opts.includeTotal) params.include_total = true
      if (source !== ALL) params.source = source
      if (action !== ALL) params.action = action
      if (level !== ALL) params.level = level
      if (userID !== ALL) params.user_id = userID
      if (dateFrom) params.date_from = dateFrom
      if (dateTo) params.date_to = dateTo
      const res = await api.get<LogsResponse>('/api/logs', { params })
      const rows = res.data.data || []
      setLogs((prev) => opts.append ? [...prev, ...rows] : rows)
      setTotal(typeof res.data.total === 'number' ? res.data.total : null)
      setNextCursor(res.data.next_cursor || '')
      setHasMore(!!res.data.has_more)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [source, action, level, userID, dateFrom, dateTo])

  const hasFilters =
    source !== ALL || action !== ALL || level !== ALL || userID !== ALL || !!dateFrom || !!dateTo || quickView !== 'all'

  const resetFilters = () => {
    setSource(ALL)
    setAction(ALL)
    setLevel(ALL)
    setUserID(ALL)
    setDateFrom('')
    setDateTo('')
    setQuickView('all')
  }

  const visibleLogs = useMemo(
    () => logs.filter((log) => quickViewMatch(log, quickView)),
    [logs, quickView],
  )

  // Stats: count errors + warnings within current page result for quick scan
  const errorCount = useMemo(
    () => visibleLogs.filter((l) => l.level === 'error').length,
    [visibleLogs],
  )
  const warnCount = useMemo(
    () => visibleLogs.filter((l) => l.level === 'warn' || hasDocNoQualityIssue(l)).length,
    [visibleLogs],
  )
  const pageStats = useMemo(() => ({
    all: logs.length,
    actionable: logs.filter((l) => quickViewMatch(l, 'actionable')).length,
    smlFailed: logs.filter((l) => l.action === 'sml_failed').length,
    shopeeSettlement: logs.filter(isShopeeSettlementLog).length,
    imports: logs.filter(isImportLog).length,
    mapping: logs.filter((l) => l.action === 'mapping_feedback').length,
    dataQuality: logs.filter(hasDocNoQualityIssue).length,
    sent: logs.filter((l) => l.action === 'sml_sent').length,
  }), [logs])

  const actorOptions = useMemo(() => {
    const seen = new Map<string, { id: string; label: string }>()
    for (const log of logs) {
      if (log.actor?.type !== 'user' || !log.actor.id) continue
      seen.set(log.actor.id, {
        id: log.actor.id,
        label: `${log.actor.name}${log.actor.email ? ` · ${log.actor.email}` : ''}`,
      })
    }
    return Array.from(seen.values()).sort((a, b) => a.label.localeCompare(b.label))
  }, [logs])

  const grouped = useMemo(() => groupByDate(visibleLogs), [visibleLogs])

  return (
    <TooltipProvider>
      <div className="space-y-4">
        <PageHeader
          title="ประวัติการทำงาน"
          description="ตรวจย้อนหลังว่าระบบดึงอีเมล สร้างบิล และส่งเข้า SML สำเร็จหรือไม่"
          actions={
            <div className="flex flex-wrap items-center gap-2">
              <div className="flex h-9 items-center gap-2 rounded-md border bg-card px-2.5">
                <Bug className="h-3.5 w-3.5 text-muted-foreground" />
                <Label htmlFor="logs-dev-mode" className="cursor-pointer text-xs text-muted-foreground">
                  DEV
                </Label>
                <Switch id="logs-dev-mode" checked={devMode} onCheckedChange={setDevMode} />
              </div>
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5"
                onClick={() => load()}
                disabled={loading}
              >
                <RotateCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
                รีเฟรช
              </Button>
            </div>
          }
        />

        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-8">
          <SummaryButton
            label="ทั้งหมดในหน้านี้"
            value={pageStats.all}
            icon={Activity}
            tone="muted"
            active={quickView === 'all'}
            onClick={() => setQuickView('all')}
          />
          <SummaryButton
            label="ต้องแก้"
            value={pageStats.actionable}
            icon={AlertTriangle}
            tone={pageStats.actionable > 0 ? 'danger' : 'muted'}
            active={quickView === 'actionable'}
            onClick={() => setQuickView('actionable')}
          />
          <SummaryButton
            label="SML ล้มเหลว"
            value={pageStats.smlFailed}
            icon={ShieldAlert}
            tone={pageStats.smlFailed > 0 ? 'danger' : 'muted'}
            active={quickView === 'sml_failed'}
            onClick={() => setQuickView('sml_failed')}
          />
          <SummaryButton
            label="ส่งสำเร็จ"
            value={pageStats.sent}
            icon={CheckCircle2}
            tone="success"
            active={false}
            onClick={() => {
              setQuickView('all')
              setAction('sml_sent')
            }}
          />
          <SummaryButton
            label="รับชำระ Shopee"
            value={pageStats.shopeeSettlement}
            icon={ScrollText}
            tone="success"
            active={quickView === 'shopee_settlement'}
            onClick={() => {
              setQuickView('shopee_settlement')
              setSource(ALL)
              setAction(ALL)
            }}
          />
          <SummaryButton
            label="นำเข้าไฟล์"
            value={pageStats.imports}
            icon={Layers3}
            tone="info"
            active={quickView === 'imports'}
            onClick={() => setQuickView('imports')}
          />
          <SummaryButton
            label="จับคู่สินค้า"
            value={pageStats.mapping}
            icon={Sparkles}
            tone="primary"
            active={quickView === 'mapping'}
            onClick={() => setQuickView('mapping')}
          />
          <SummaryButton
            label="เลขเอกสารแปลก"
            value={pageStats.dataQuality}
            icon={Bug}
            tone={pageStats.dataQuality > 0 ? 'warning' : 'muted'}
            active={quickView === 'data_quality'}
            onClick={() => setQuickView('data_quality')}
          />
        </div>

        <Card className="shadow-none">
          <CardContent className="grid grid-cols-1 items-end gap-3 p-3 sm:grid-cols-2 xl:grid-cols-[170px_minmax(220px,1fr)_160px_170px_240px_auto]">
            <div className="space-y-1.5">
              <Label className="block text-xs text-muted-foreground">ช่องทาง</Label>
              <Select value={source} onValueChange={setSource}>
                <SelectTrigger className="h-10 w-full text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>ทั้งหมด</SelectItem>
                  {PHASE >= 2 && <SelectItem value="line">LINE</SelectItem>}
                  <SelectItem value="email">อีเมล</SelectItem>
                  <SelectItem value="shopee_email">Shopee Email</SelectItem>
	                  <SelectItem value="shopee_shipped">Shopee Shipped</SelectItem>
	                  {PHASE >= 2 && <SelectItem value="shopee_excel">Shopee Excel</SelectItem>}
                  <SelectItem value="shopee_settlement">รับชำระ Shopee</SelectItem>
	                  {PHASE >= 2 && <SelectItem value="lazada">Lazada</SelectItem>}
	                  {PHASE >= 2 && <SelectItem value="tiktok">TikTok Excel</SelectItem>}
	                  <SelectItem value="sml">SML</SelectItem>
                  <SelectItem value="catalog">สินค้า SML</SelectItem>
                  <SelectItem value="channel_defaults">ตั้งค่าเอกสาร</SelectItem>
                  <SelectItem value="system">ระบบ</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="block text-xs text-muted-foreground">เหตุการณ์</Label>
              <Select value={action} onValueChange={setAction}>
                <SelectTrigger className="h-10 w-full text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>ทั้งหมด</SelectItem>
                  {Object.entries(ACTION_META)
                    .filter(([key]) => PHASE >= 2 || !PHASE2_ACTIONS.has(key))
                    .map(([key, meta]) => (
                      <SelectItem key={key} value={key}>
                        {meta.label}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="block text-xs text-muted-foreground">ระดับ</Label>
              <Select value={level} onValueChange={setLevel}>
                <SelectTrigger className="h-10 w-full text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>ทั้งหมด</SelectItem>
                  <SelectItem value="info">ข้อมูล</SelectItem>
                  <SelectItem value="warn">คำเตือน</SelectItem>
                  <SelectItem value="error">ผิดพลาด</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="block text-xs text-muted-foreground">ผู้ทำรายการ</Label>
              <Select value={userID} onValueChange={setUserID}>
                <SelectTrigger className="h-10 w-full text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>ทั้งหมด</SelectItem>
                  {actorOptions.map((actor) => (
                    <SelectItem key={actor.id} value={actor.id}>
                      {actor.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="block text-xs text-muted-foreground">
                วันที่
              </Label>
              <DateRangePicker
                from={dateFrom}
                to={dateTo}
                onFromChange={setDateFrom}
                onToChange={setDateTo}
                className="h-10 w-full min-w-0 text-sm"
              />
            </div>
            {hasFilters && (
              <Button variant="ghost" size="sm" onClick={resetFilters} className="h-10 justify-self-start text-xs lg:justify-self-end">
                <Filter className="h-3.5 w-3.5" />
                ล้างตัวกรอง
              </Button>
            )}
          </CardContent>
        </Card>

        <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          <span>
            แสดง <span className="font-medium text-foreground">{visibleLogs.length.toLocaleString()}</span>
            {total != null && (
              <>
                {' '}จาก <span className="font-medium text-foreground">{total.toLocaleString()}</span> รายการ
              </>
            )}
          </span>
          {errorCount > 0 && (
            <span className="text-destructive">· ผิดพลาด {errorCount}</span>
          )}
          {warnCount > 0 && <span className="text-warning">· คำเตือน {warnCount}</span>}
        </div>

        <div className="space-y-3">
          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-16 w-full rounded-lg" />
              ))}
            </div>
          ) : visibleLogs.length === 0 ? (
            <EmptyState
              icon={ScrollText}
              title="ยังไม่มีประวัติ"
              description={
                hasFilters
                  ? 'ลองล้างตัวกรองหรือขยายช่วงวันที่'
                  : 'เมื่อระบบทำงานจะมีประวัติแสดงที่นี่'
              }
            />
          ) : (
            grouped.map((g) => (
              <div key={g.key} className="space-y-1">
                <div className="flex items-center gap-2 px-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  <span>{g.label}</span>
                  <span className="text-muted-foreground/60">· {g.items.length}</span>
                  <div className="h-px flex-1 bg-border" />
                </div>
                <div className="space-y-1">
                  {buildActivityItems(g.items).map((item) =>
                    item.kind === 'import' ? (
                      <ImportGroupCard
                        key={item.key}
                        group={item}
                        devMode={devMode}
                        onRetried={() => load()}
                      />
                    ) : (
                      <LogRow
                        key={item.key}
                        log={item.log}
                        onRetried={() => load()}
                        devMode={devMode}
                      />
                    ),
                  )}
                </div>
              </div>
            ))
          )}
        </div>

        {hasMore && (
          <div className="flex items-center justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={loading || !nextCursor}
              onClick={() => load({ cursor: nextCursor, append: true })}
            >
              โหลดเพิ่ม
            </Button>
          </div>
        )}
      </div>
    </TooltipProvider>
  )
}
