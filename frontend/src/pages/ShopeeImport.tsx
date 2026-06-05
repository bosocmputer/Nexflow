import { useState, useRef, useEffect, Fragment } from 'react'
import { Link } from 'react-router-dom'
import dayjs from 'dayjs'
import {
  AlertCircle,
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock3,
  Database,
  FileSpreadsheet,
  Info,
  Loader2,
  Pencil,
  PlugZap,
  Power,
  RefreshCw,
  Save,
  ShieldCheck,
  Store,
  X,
} from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PageHeader } from '@/components/common/PageHeader'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ActionSafetyPanel } from '@/components/common/ActionSafetyPanel'
import { DateRangePicker, type DateRangePreset } from '@/components/common/DateRangePicker'
import client from '@/api/client'
import { cn } from '@/lib/utils'

interface ShopeeConfig {
  server_url: string
  guid: string
  provider: string
  config_file_name: string
  database_name: string
  doc_format_code: string
  endpoint?: string
  cust_code: string
  sale_code: string
  branch_code: string
  wh_code: string
  shelf_code: string
  unit_code: string
  vat_type: number
  vat_rate: number
  doc_time: string
}

interface ShopeeOrderItem {
  sku: string
  product_name: string
  option_name?: string
  raw_name: string
  price: number
  qty: number
  no_sku?: boolean
}
interface ShopeeOrder {
  order_id: string
  doc_date: string
  order_datetime?: string
  payment_time?: string
  payment_channel?: string
  buyer_username?: string
  tracking_no?: string
  package_number?: string
  shipping_carrier?: string
  cod?: boolean
  status: string
  items: ShopeeOrderItem[]
  item_count: number
  total_qty: number
  paid_amount?: number
  order_total_amount?: number
  item_gross_amount?: number
  line_paid_amount?: number
  shipping_amount?: number
  discount_amount?: number
  no_sku_item_count?: number
  has_no_sku?: boolean
  multi_line?: boolean
  amount_mismatch?: boolean
  existing_bill_id?: string
  shopee_shop_id?: string
  shopee_connection_id?: string
  shopee_shop_label?: string
  duplicate: boolean
}
interface ImportPreflight {
  new_orders: number
  duplicate_orders: number
  skipped_rows: number
  no_sku_orders: number
  no_sku_items: number
  multi_item_orders: number
  amount_mismatch_orders: number
}
interface PreviewResponse {
  orders: ShopeeOrder[]
  warnings: string[]
  total_orders: number
  new_count: number
  duplicate_count: number
  skipped_count: number
  import_run_id?: string
  preflight: ImportPreflight
  file_token?: string
  more?: boolean
  next_cursor?: string
}
interface ConfirmResult {
  order_id: string
  success: boolean
  bill_id?: string
  doc_no?: string
  message?: string
}
interface ImportRunSummary {
  id: string
  filename: string
  period_start?: string
  period_end?: string
  total_orders: number
  new_orders: number
  duplicate_orders: number
  skipped_orders: number
  warning_count: number
  created_count: number
  failed_count: number
  status: 'preview' | 'confirmed' | 'failed'
  created_at: string
  confirmed_at?: string
}

type ShopeeAPIReadinessCheckStatus = 'ok' | 'warning' | 'blocked'

interface ShopeeAPIReadinessCheck {
  key: string
  label: string
  status: ShopeeAPIReadinessCheckStatus
  detail?: string
}

interface ShopeeAPIStatus {
  enabled: boolean
  configured: boolean
  environment: string
  base_url?: string
  partner_id?: number
  redirect_url?: string
  connected: boolean
  shop_id?: number
  shop_name?: string
  access_expires_at?: string
  refresh_expires_at?: string
  last_sync_at?: string
  last_sync_status?: string
  last_sync_error?: string
  token_state?: string
  can_connect?: boolean
  can_fetch?: boolean
  blocking_reason?: string
  checks?: ShopeeAPIReadinessCheck[]
}

interface ShopeeAPIConnection {
  id: string
  shop_id: number
  merchant_id?: number
  shop_name?: string
  label: string
  environment: string
  access_expires_at: string
  refresh_expires_at: string
  disabled_at?: string
  last_sync_at?: string
  last_sync_status?: string
  last_sync_error?: string
  last_error_code?: string
  token_state: string
  can_fetch: boolean
  connected_at?: string
  updated_at?: string
}

function fmt(n: number) {
  return n.toLocaleString('th-TH', { minimumFractionDigits: 2 })
}

function fmtDateTime(s: string) {
  if (!s) return '—'
  return new Date(s).toLocaleString('th-TH', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function shopeeDestination(config?: ShopeeConfig | null) {
  const docFormat = (config?.doc_format_code ?? '').trim().toUpperCase()
  const endpoint = (config?.endpoint ?? '').toLowerCase()
  const isSaleInvoice = endpoint.includes('saleinvoice') || docFormat === 'SI'
  return isSaleInvoice
    ? {
        documentName: 'เอกสารขายสินค้าและบริการ',
        shortName: 'ขายสินค้าและบริการ',
        smlPath: 'ขาย -> ขายสินค้าและบริการ',
        action: 'สร้างเอกสารขายสินค้าและบริการ',
        done: 'สร้างเอกสารขายสินค้าและบริการแล้ว',
        listPath: '/sale-invoices',
        listName: 'ขายสินค้าและบริการ',
      }
    : {
        documentName: 'ใบสั่งขาย',
        shortName: 'ใบสั่งขาย',
        smlPath: 'ขาย -> ใบสั่งขาย',
        action: 'สร้างใบสั่งขาย',
        done: 'สร้างใบสั่งขายแล้ว',
        listPath: '/sales-orders',
        listName: 'ใบสั่งขาย',
      }
}

type APIReadinessTone = 'success' | 'warning' | 'danger' | 'muted'

interface APIReadinessStep {
  label: string
  done: boolean
  status?: ShopeeAPIReadinessCheckStatus
  detail?: string
}

interface APIReadiness {
  title: string
  description: string
  tone: APIReadinessTone
  steps: APIReadinessStep[]
}

function isLiveAPI(status: ShopeeAPIStatus) {
  return (status.environment || '').toLowerCase() === 'live'
}

function shopeeAPIReadiness(status: ShopeeAPIStatus): APIReadiness {
  const live = isLiveAPI(status)
  const steps: APIReadinessStep[] =
    status.checks && status.checks.length > 0
      ? status.checks.map((check) => ({
          label: check.label,
          done: check.status === 'ok',
          status: check.status,
          detail: check.detail,
        }))
      : [
          { label: 'เปิด Shopee Open API บน server', done: status.enabled },
          { label: 'ตั้งค่า Partner ID / Key', done: status.configured },
          { label: 'ใช้ Live key หลัง Shopee approve', done: live },
          { label: 'เชื่อมร้านผ่าน OAuth', done: status.connected },
        ]
  const hasBlocked = steps.some((s) => s.status === 'blocked')
  const hasWarning = steps.some((s) => s.status === 'warning' || !s.done)

  if (!status.enabled) {
    return {
      title: 'Shopee Open API ยังปิดอยู่',
      description: 'เปิด SHOPEE_OPEN_API_ENABLED=true ก่อนเริ่มเชื่อมร้าน',
      tone: 'danger',
      steps,
    }
  }
  if (hasBlocked) {
    return {
      title: 'Shopee API ยังไม่พร้อมใช้งาน',
      description: status.blocking_reason || 'มีรายการ preflight ที่ต้องแก้ก่อนเชื่อมต่อหรือดึง order',
      tone: 'danger',
      steps,
    }
  }
  if (!status.configured) {
    return {
      title: 'ยังไม่ได้ตั้งค่า key บน server',
      description: 'ต้องใส่ Partner ID และ Partner Key ให้ครบก่อนสร้างลิงก์ OAuth',
      tone: 'warning',
      steps,
    }
  }
  if (!live && !status.connected) {
    return {
      title: 'พร้อมระดับ sandbox แต่ยังไม่พร้อมร้านจริง',
      description: 'ตอนนี้ใช้ test key อยู่ ร้านจริงต้องรอ Shopee approve Go-Live แล้วเปลี่ยนเป็น live key ก่อนเชื่อม',
      tone: 'warning',
      steps,
    }
  }
  if (live && !status.connected) {
    return {
      title: 'พร้อมเชื่อมร้านจริง',
      description: 'ตรวจว่า Redirect URL Domain ใน Shopee Console ตรงกับ public URL ปัจจุบัน แล้วกดเชื่อมต่อ Shopee API',
      tone: 'success',
      steps,
    }
  }
  if (hasWarning) {
    return {
      title: live ? 'พร้อมใช้งานแต่มีข้อควรตรวจ' : 'พร้อมระดับ sandbox แต่ยังไม่ใช่ live',
      description: status.blocking_reason || 'ตรวจ warning ก่อนใช้งานจริง โดยเฉพาะ token, redirect และ sync ล่าสุด',
      tone: 'warning',
      steps,
    }
  }
  return {
    title: live ? 'เชื่อมร้านจริงแล้ว' : 'เชื่อม sandbox แล้ว',
    description: 'ดึง order เพื่อตรวจรายการก่อนสร้างบิล และตรวจข้อมูลก่อนส่งเข้า SML',
    tone: live ? 'success' : 'warning',
    steps,
  }
}

function readinessToneClass(tone: APIReadinessTone) {
  if (tone === 'success') return 'border-success/30 bg-success/5 text-success'
  if (tone === 'danger') return 'border-destructive/30 bg-destructive/5 text-destructive'
  if (tone === 'warning') return 'border-warning/35 bg-warning/10 text-warning'
  return 'border-border bg-muted/30 text-muted-foreground'
}

function readinessStepIcon(step: APIReadinessStep) {
  if (step.status === 'blocked') return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
  if (step.status === 'warning' || !step.done) return <Clock3 className="h-3.5 w-3.5 shrink-0 text-warning" />
  return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
}

function tokenStateLabel(v?: string) {
  switch (v) {
    case 'access_valid':
      return 'พร้อมใช้'
    case 'access_expiring':
      return 'ใกล้ refresh'
    case 'refresh_required':
      return 'ต้อง refresh'
    case 'refresh_expired':
      return 'หมดอายุ'
    default:
      return '—'
  }
}

function apiRangeError(from: string, to: string) {
  if (!from || !to) return 'เลือกวันที่เริ่มต้นและสิ้นสุดให้ครบ'
  const fromDate = new Date(`${from}T00:00:00`)
  const toDate = new Date(`${to}T00:00:00`)
  if (Number.isNaN(fromDate.getTime()) || Number.isNaN(toDate.getTime())) {
    return 'รูปแบบวันที่ไม่ถูกต้อง'
  }
  if (toDate < fromDate) return 'วันที่สิ้นสุดต้องไม่ก่อนวันที่เริ่มต้น'
  const days = Math.floor((toDate.getTime() - fromDate.getTime()) / 86400000) + 1
  if (days > 15) return 'Shopee API จำกัดการดึงข้อมูลไม่เกิน 15 วันต่อครั้ง'
  return ''
}

const shopeeOrderStatusOptions = [
  { value: 'ready_to_bill', label: 'พร้อมออกบิล (จัดส่งแล้ว/รอรับสินค้า/สำเร็จ)' },
  { value: 'all', label: 'ทั้งหมด' },
  { value: 'SHIPPED', label: 'จัดส่งแล้ว (SHIPPED)' },
  { value: 'TO_CONFIRM_RECEIVE', label: 'รอลูกค้ายืนยันรับสินค้า (TO_CONFIRM_RECEIVE)' },
  { value: 'COMPLETED', label: 'สำเร็จแล้ว (COMPLETED)' },
  { value: 'READY_TO_SHIP', label: 'พร้อมจัดส่ง (READY_TO_SHIP)' },
  { value: 'PROCESSED', label: 'กำลังเตรียมพัสดุ (PROCESSED)' },
]

const NO_SHOP_SELECTED = '__no_shop_selected__'
const shopeeImportPresets: DateRangePreset[] = [
  {
    label: 'วันนี้',
    getRange: () => {
      const today = dayjs().format('YYYY-MM-DD')
      return { from: today, to: today }
    },
  },
  {
    label: '7 วัน',
    getRange: () => ({
      from: dayjs().subtract(6, 'day').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
  },
  {
    label: '15 วัน',
    getRange: () => ({
      from: dayjs().subtract(14, 'day').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
  },
]

const CANONICAL_PUBLIC_URL = 'https://animal-galvanize-tameness.ngrok-free.dev'

function hostFromURL(value?: string) {
  if (!value) return ''
  try {
    return new URL(value).host
  } catch {
    return ''
  }
}

function apiErrorMessage(err: unknown, fallback: string) {
  const data = (err as { response?: { data?: { error?: string; error_code?: string } } })?.response?.data
  const raw = data?.error ?? ''
  const lower = raw.toLowerCase()
  switch (data?.error_code) {
    case 'not_configured':
      return 'Shopee Open API ยังไม่ได้ตั้งค่า Partner ID/Key บน server'
    case 'redirect_not_ready':
      return 'Redirect URL ยังไม่พร้อม ให้ตรวจ PUBLIC_BASE_URL และ Shopee Console ว่าตรงกัน'
    case 'not_connected':
      return 'ยังไม่ได้เชื่อมต่อร้าน Shopee ให้รอ Go-Live approve แล้วกดเชื่อมต่อ API'
    case 'bad_signature':
      return 'Shopee ปฏิเสธ signature ให้ตรวจ Partner ID/Key และ sandbox/live base URL'
    case 'token_error':
      return 'Shopee token ใช้งานไม่ได้หรือหมดอายุ ให้กดเชื่อมต่อร้านใหม่'
    case 'permission_denied':
      return 'Shopee ยังไม่อนุญาตสิทธิ์นี้ ให้ตรวจสถานะ Go-Live และ permission ของแอป'
    case 'rate_limited':
      return 'Shopee rate limit ให้รอสักครู่แล้วลองใหม่'
    case 'network_timeout':
      return 'เชื่อมต่อ Shopee ชั่วคราวไม่สำเร็จ ให้ลองใหม่อีกครั้ง'
  }
  if (lower.includes('wrong sign') || lower.includes('error_sign')) {
    return 'Shopee ปฏิเสธ signature ให้ตรวจ Partner ID/Key และ sandbox/live base URL'
  }
  if (lower.includes('redirect') && lower.includes('domain')) {
    return 'Shopee ปฏิเสธ Redirect URL Domain ให้ตรวจ public URL ใน Console ให้ตรงกับ server'
  }
  if (lower.includes('ยังไม่ได้เชื่อมต่อร้าน')) {
    return 'ยังไม่ได้เชื่อมต่อร้าน Shopee ให้กดเชื่อมต่อ API ก่อนดึง order'
  }
  if (lower.includes('token')) {
    return 'Shopee token ใช้งานไม่ได้หรือหมดอายุ ให้กดเชื่อมต่อร้านใหม่'
  }
  if (lower.includes('permission') || lower.includes('forbidden')) {
    return 'Shopee ยังไม่อนุญาตสิทธิ์นี้ ให้ตรวจสถานะ Go-Live และ permission ของแอป'
  }
  if (lower.includes('rate') || lower.includes('429')) {
    return 'Shopee rate limit ให้รอสักครู่แล้วลองใหม่'
  }
  return raw || fallback
}

function SummaryCard({
  label,
  value,
  variant = 'muted',
}: {
  label: string
  value: number
  variant?: 'success' | 'danger' | 'primary' | 'muted'
}) {
  const tone: Record<typeof variant, string> = {
    success: 'border-success/30 bg-success/5 text-success',
    danger: 'border-destructive/30 bg-destructive/5 text-destructive',
    primary: 'border-primary/30 bg-primary/5 text-accent-strong',
    muted: 'border-border bg-muted/30 text-foreground',
  }
  return (
    <Card className={cn('text-center', tone[variant])}>
      <CardContent className="p-4">
        <p className="text-3xl font-semibold tabular-nums">{value}</p>
        <p className="mt-1 text-xs font-medium text-muted-foreground">{label}</p>
      </CardContent>
    </Card>
  )
}

type Step = 'idle' | 'uploading' | 'preview' | 'confirming' | 'done'

export default function ShopeeImport() {
  const fileRef = useRef<HTMLInputElement>(null)
  const apiAuthPollRef = useRef<number | null>(null)
  const [step, setStep] = useState<Step>('idle')
  const [config, setConfig] = useState<ShopeeConfig | null>(null)
  const [preview, setPreview] = useState<PreviewResponse | null>(null)
  const [selectedIDs, setSelectedIDs] = useState<Set<string>>(new Set())
  const [results, setResults] = useState<{
    success_count: number
    fail_count: number
    results: ConfirmResult[]
  } | null>(null)
  const [error, setError] = useState('')
  const [expandedOrders, setExpandedOrders] = useState<Set<string>>(new Set())
  const [recentRuns, setRecentRuns] = useState<ImportRunSummary[]>([])
  const [confirmElapsed, setConfirmElapsed] = useState(0)
  const [previewSource, setPreviewSource] = useState<'excel' | 'api'>('excel')
  const [apiStatus, setAPIStatus] = useState<ShopeeAPIStatus | null>(null)
  const [apiConnections, setAPIConnections] = useState<ShopeeAPIConnection[]>([])
  const [apiStatusLoadError, setAPIStatusLoadError] = useState('')
  const [apiConnectionsLoadError, setAPIConnectionsLoadError] = useState('')
  const [selectedConnectionID, setSelectedConnectionID] = useState('')
  const [editingConnectionID, setEditingConnectionID] = useState('')
  const [editingLabel, setEditingLabel] = useState('')
  const [confirmConnectOpen, setConfirmConnectOpen] = useState(false)
  const [disableConnection, setDisableConnection] = useState<ShopeeAPIConnection | null>(null)
  const [apiBusy, setAPIBusy] = useState(false)
  const [apiFrom, setAPIFrom] = useState(() => {
    const d = new Date()
    d.setDate(d.getDate() - 7)
    return d.toISOString().slice(0, 10)
  })
  const [apiTo, setAPITo] = useState(() => new Date().toISOString().slice(0, 10))
  const [apiTimeRangeField, setAPITimeRangeField] = useState<'create_time' | 'update_time'>('create_time')
  const [apiOrderStatus, setAPIOrderStatus] = useState('ready_to_bill')

  // Track config load + ready states separately so preflight UI can render
  // a missing-config banner BEFORE admin uploads a file. Without this, file
  // upload silently succeeds → preview works → confirm fails late with a
  // confusing "config missing" error.
  const [configLoading, setConfigLoading] = useState(true)
  const configReady = !configLoading
  const smlCustomerReady = configReady && Boolean(config?.cust_code?.trim())
  const destination = shopeeDestination(config)

  const fallbackConfig: ShopeeConfig = {
    server_url: '',
    guid: '',
    provider: '',
    config_file_name: '',
    database_name: '',
    doc_format_code: 'SR',
    endpoint: '',
    cust_code: '',
    sale_code: '',
    branch_code: '',
    wh_code: '',
    shelf_code: '',
    unit_code: '',
    vat_type: -1,
    vat_rate: -1,
    doc_time: '',
  }

  useEffect(() => {
    let alive = true
    client
      .get<ShopeeConfig>('/api/settings/shopee-config')
      .then((res) => {
        if (alive) setConfig(res.data)
      })
      .catch(() => {
        if (alive) setError('โหลด config ไม่ได้')
      })
      .finally(() => {
        if (alive) setConfigLoading(false)
      })
    client
      .get<{ runs: ImportRunSummary[] }>('/api/import/shopee/runs?limit=5')
      .then((res) => {
        if (alive) setRecentRuns(res.data.runs ?? [])
      })
      .catch(() => undefined)
    client
      .get<ShopeeAPIStatus>('/api/settings/shopee-api/status')
      .then((res) => {
        if (!alive) return
        setAPIStatus(res.data)
        setAPIStatusLoadError('')
      })
      .catch((err: unknown) => {
        if (!alive) return
        setAPIStatus(null)
        setAPIStatusLoadError(apiErrorMessage(err, 'โหลดสถานะ Shopee API ไม่ได้'))
      })
    client
      .get<{ data: ShopeeAPIConnection[] }>('/api/shopee-api/connections')
      .then((res) => {
        if (!alive) return
        const rows = res.data.data ?? []
        setAPIConnections(rows)
        setAPIConnectionsLoadError('')
        const firstActive = rows.find((c) => !c.disabled_at)
        if (firstActive) setSelectedConnectionID((current) => current || firstActive.id)
      })
      .catch((err: unknown) => {
        if (!alive) return
        setAPIConnections([])
        setAPIConnectionsLoadError(apiErrorMessage(err, 'โหลดรายการร้าน Shopee ไม่ได้'))
      })
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    return () => {
      if (apiAuthPollRef.current !== null) {
        window.clearInterval(apiAuthPollRef.current)
      }
    }
  }, [])

  useEffect(() => {
    if (step !== 'confirming') {
      setConfirmElapsed(0)
      return
    }
    const startedAt = Date.now()
    const timer = window.setInterval(() => {
      setConfirmElapsed(Math.floor((Date.now() - startedAt) / 1000))
    }, 1000)
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('beforeunload', onBeforeUnload)
    }
  }, [step])

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    e.target.value = ''
    setStep('uploading')
    setError('')
    setPreview(null)
    setResults(null)
    const form = new FormData()
    form.append('file', file)
    if (selectedConnectionID) form.append('connection_id', selectedConnectionID)
    try {
      const res = await client.post<PreviewResponse>(
        '/api/import/shopee/preview',
        form,
        { headers: { 'Content-Type': 'multipart/form-data' } },
      )
      setPreviewSource('excel')
      setPreview(res.data)
      setSelectedIDs(
        new Set(res.data.orders.filter((o) => !o.duplicate).map((o) => o.order_id)),
      )
      setStep('preview')
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          'อัปโหลดไฟล์ไม่ได้',
      )
      setStep('idle')
    }
  }

  const refreshAPIStatus = async () => {
    try {
      const res = await client.get<ShopeeAPIStatus>('/api/settings/shopee-api/status')
      setAPIStatus(res.data)
      setAPIStatusLoadError('')
    } catch (err: unknown) {
      setAPIStatus(null)
      setAPIConnections([])
      setAPIStatusLoadError(apiErrorMessage(err, 'โหลดสถานะ Shopee API ไม่ได้'))
    }
  }

  const refreshAPIConnections = async () => {
    try {
      const res = await client.get<{ data: ShopeeAPIConnection[] }>('/api/shopee-api/connections')
      const rows = res.data.data ?? []
      setAPIConnections(rows)
      const active = rows.filter((c) => !c.disabled_at)
      setSelectedConnectionID((current) => {
        if (current && active.some((c) => c.id === current)) return current
        return active[0]?.id ?? ''
      })
      setAPIConnectionsLoadError('')
    } catch (err: unknown) {
      setAPIConnections([])
      setAPIConnectionsLoadError(apiErrorMessage(err, 'โหลดรายการร้าน Shopee ไม่ได้'))
    }
  }

  const handleRefreshAPISection = async () => {
    setError('')
    setAPIStatusLoadError('')
    setAPIConnectionsLoadError('')
    await Promise.all([refreshAPIStatus(), refreshAPIConnections()])
  }

  const handleConnectAPI = async () => {
    setError('')
    if (apiAuthPollRef.current !== null) {
      window.clearInterval(apiAuthPollRef.current)
      apiAuthPollRef.current = null
    }

    const authWindow = window.open('', '_blank', 'popup=yes,width=1120,height=820')
    if (!authWindow) {
      setError('Browser บล็อกหน้าต่าง Shopee ให้เปิด pop-up สำหรับ Nexflow แล้วลองกดเชื่อมต่ออีกครั้ง')
      return
    }

    authWindow.document.title = 'กำลังเปิด Shopee Open API'
    authWindow.document.body.style.cssText =
      'margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#F8FBFF;color:#0F172A;display:grid;place-items:center;min-height:100vh;'
    authWindow.document.body.textContent = 'กำลังเปิดหน้า Shopee เพื่อเชื่อมต่อร้าน...'

    setAPIBusy(true)
    try {
      const res = await client.post<{ auth_url: string }>('/api/shopee-api/auth-url')
      authWindow.opener = null
      authWindow.location.href = res.data.auth_url
      let pollCount = 0
      apiAuthPollRef.current = window.setInterval(() => {
        pollCount += 1
        void Promise.all([refreshAPIStatus(), refreshAPIConnections()])
        if (authWindow.closed || pollCount >= 60) {
          if (apiAuthPollRef.current !== null) {
            window.clearInterval(apiAuthPollRef.current)
            apiAuthPollRef.current = null
          }
          void Promise.all([refreshAPIStatus(), refreshAPIConnections()])
        }
      }, 2000)
    } catch (err: unknown) {
      authWindow.close()
      setError(apiErrorMessage(err, 'สร้างลิงก์เชื่อมต่อ Shopee API ไม่ได้'))
    } finally {
      setAPIBusy(false)
    }
  }

  const startEditConnection = (conn: ShopeeAPIConnection) => {
    setEditingConnectionID(conn.id)
    setEditingLabel(conn.label || String(conn.shop_id))
  }

  const saveConnectionLabel = async (conn: ShopeeAPIConnection) => {
    const next = editingLabel.trim()
    if (!next) {
      setError('ชื่อร้านต้องไม่ว่าง')
      return
    }
    setAPIBusy(true)
    setError('')
    try {
      await client.patch(`/api/shopee-api/connections/${conn.id}`, { label: next })
      setEditingConnectionID('')
      setEditingLabel('')
      await refreshAPIConnections()
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'แก้ไขชื่อร้าน Shopee ไม่ได้'))
    } finally {
      setAPIBusy(false)
    }
  }

  const toggleConnectionDisabled = async (conn: ShopeeAPIConnection) => {
    setAPIBusy(true)
    setError('')
    try {
      await client.patch(`/api/shopee-api/connections/${conn.id}`, { disabled: !conn.disabled_at })
      await refreshAPIConnections()
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'อัปเดตร้าน Shopee ไม่ได้'))
    } finally {
      setAPIBusy(false)
    }
  }

  const handleFetchAPI = async () => {
    setStep('uploading')
    setError('')
    setPreview(null)
    setResults(null)
    setAPIBusy(true)
    try {
      const res = await client.post<PreviewResponse>('/api/import/shopee/api/preview', {
        connection_id: selectedConnectionID,
        time_from: apiFrom,
        time_to: apiTo,
        time_range_field: apiTimeRangeField,
        order_status: apiOrderStatus,
        page_size: 50,
      })
      setPreviewSource('api')
      setPreview(res.data)
      setSelectedIDs(
        new Set(res.data.orders.filter((o) => !o.duplicate).map((o) => o.order_id)),
      )
      setStep('preview')
      void refreshAPIStatus()
      void refreshAPIConnections()
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'ดึง order จาก Shopee API ไม่ได้'))
      setStep('idle')
      void refreshAPIStatus()
      void refreshAPIConnections()
    } finally {
      setAPIBusy(false)
    }
  }

  const handleConfirm = async () => {
    if (!preview || selectedIDs.size === 0) return
    setStep('confirming')
    setError('')
    try {
      const res = await client.post('/api/import/shopee/confirm', {
        config: config ?? fallbackConfig,
        order_ids: Array.from(selectedIDs),
        orders: preview.orders,
        file_token: preview.file_token,
        import_run_id: preview.import_run_id,
        source_flow: previewSource === 'api' ? 'shopee_api' : 'shopee_excel',
        connection_id: selectedConnectionID,
      }, { timeout: 120000 })
      setResults(res.data)
      setStep('done')
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          'ส่งข้อมูลไม่ได้',
      )
      setStep('preview')
    }
  }

  const toggleOrder = (id: string) =>
    setSelectedIDs((p) => {
      const s = new Set(p)
      if (s.has(id)) s.delete(id)
      else s.add(id)
      return s
    })
  const toggleAll = () => {
    if (!preview) return
    const nonDup = preview.orders.filter((o) => !o.duplicate).map((o) => o.order_id)
    setSelectedIDs(selectedIDs.size === nonDup.length ? new Set() : new Set(nonDup))
  }
  const toggleExpand = (id: string) =>
    setExpandedOrders((p) => {
      const s = new Set(p)
      if (s.has(id)) s.delete(id)
      else s.add(id)
      return s
    })
  const apiLoadError = apiStatusLoadError || apiConnectionsLoadError
  const apiReadiness = apiStatus ? shopeeAPIReadiness(apiStatus) : null
  const apiLive = apiStatus ? isLiveAPI(apiStatus) : false
  const apiDateError = apiRangeError(apiFrom, apiTo)
  const apiEnabled = apiStatus?.enabled !== false
  const activeConnections = apiEnabled ? apiConnections.filter((c) => !c.disabled_at) : []
  const selectedConnection = activeConnections.find((c) => c.id === selectedConnectionID) ?? null
  const needsShopSelection = activeConnections.length > 1 && !selectedConnection
  const selectedShopHint = selectedConnection
    ? `${selectedConnection.label || selectedConnection.shop_name || 'Shopee shop'} · ${selectedConnection.shop_id}`
    : activeConnections.length === 0
      ? 'ยังไม่มีร้านที่เชื่อมต่อ'
      : 'เลือกร้าน Shopee ก่อนดึง order'
  const apiWaitingForLive =
    !!apiStatus && apiStatus.enabled && apiStatus.configured && !apiLive && !apiStatus.connected
  const apiCanConnect = apiStatus?.can_connect ?? (!!apiStatus?.configured && !apiWaitingForLive)
  const apiCanFetch = (apiStatus?.can_fetch ?? false) && Boolean(selectedConnection?.can_fetch)
  const apiConnectDisabled = apiBusy || !apiCanConnect
  const apiFetchDisabled = apiBusy || !apiCanFetch || !!apiDateError || needsShopSelection
  const currentHost = typeof window !== 'undefined' ? window.location.host : ''
  const canonicalHost = hostFromURL(CANONICAL_PUBLIC_URL)
  const redirectHost = hostFromURL(apiStatus?.redirect_url)
  const connectHostWarning = Boolean(currentHost && canonicalHost && currentHost !== canonicalHost)
  const redirectHostWarning = Boolean(redirectHost && canonicalHost && redirectHost !== canonicalHost)
  const confirmDisabled = selectedIDs.size === 0 || !!preview?.more
  const confirmTitle = preview?.more
    ? 'ลดช่วงวันที่หรือเลือกสถานะแยกก่อนยืนยันนำเข้า'
    : selectedIDs.size === 0
      ? 'เลือกรายการที่ต้องการสร้างเอกสาร'
      : undefined
  const apiConnectLabel = apiWaitingForLive
    ? 'รอ Shopee approve'
    : activeConnections.length > 0
      ? 'เชื่อมร้านเพิ่ม'
      : 'เชื่อมต่อร้าน Shopee'
  const apiLastSyncError = apiStatus?.last_sync_error
    ? apiErrorMessage({ response: { data: { error: apiStatus.last_sync_error } } }, apiStatus.last_sync_error)
    : ''
  const sourceSelectionVisible = step === 'idle' || step === 'uploading'
  const apiSummary = (() => {
    if (apiLoadError) {
      return {
        title: 'Shopee API ยังไม่พร้อม',
        description: 'ใช้การนำเข้าจาก Excel ได้ และแจ้งแอดมินตรวจการเชื่อมต่อ',
        tone: 'warning' as const,
      }
    }
    if (!apiStatus) {
      return {
        title: 'กำลังตรวจสถานะ Shopee',
        description: 'กำลังโหลดสถานะร้านและการเชื่อมต่อ',
        tone: 'muted' as const,
      }
    }
    if (!apiStatus.enabled || !apiStatus.configured) {
      return {
        title: !apiStatus.enabled ? 'Shopee API ปิดใช้งาน' : 'ต้องให้แอดมินตั้งค่า',
        description: apiStatus.blocking_reason || (!apiStatus.enabled ? 'เปิด SHOPEE_OPEN_API_ENABLED=true ก่อนใช้งาน Shopee API' : 'ยังไม่พร้อมเชื่อมต่อ Shopee API'),
        tone: 'warning' as const,
      }
    }
    if (apiStatus.token_state === 'refresh_expired') {
      return {
        title: 'Token หมดอายุ',
        description: 'กดเชื่อมต่อร้าน Shopee ใหม่ก่อนดึงออเดอร์',
        tone: 'warning' as const,
      }
    }
    if (activeConnections.length === 0) {
      return {
        title: 'ยังไม่เชื่อมร้าน',
        description: 'กดเชื่อมต่อร้าน Shopee ก่อนดึงออเดอร์ครั้งแรก',
        tone: 'warning' as const,
      }
    }
    if (apiCanFetch) {
      return {
        title: 'พร้อมใช้งาน',
        description: selectedShopHint,
        tone: 'success' as const,
      }
    }
    return {
      title: 'ยังดึงออเดอร์ไม่ได้',
      description: apiStatus.blocking_reason || 'เลือกร้านหรือตรวจ token ก่อนดึงออเดอร์',
      tone: 'warning' as const,
    }
  })()
  const apiSummaryClass = {
    success: 'border-success/30 bg-success/5 text-success',
    warning: 'border-warning/35 bg-warning/10 text-warning',
    muted: 'border-border bg-muted/30 text-muted-foreground',
  }[apiSummary.tone]
  const previewIssueCount = preview
    ? (preview.more ? 1 : 0) +
      (preview.preflight?.no_sku_orders ?? 0) +
      (preview.preflight?.amount_mismatch_orders ?? 0) +
      (preview.warnings?.length ?? 0)
    : 0
  const previewHasNoOrders = !!preview && preview.orders.length === 0
  const resetPreviewLabel = previewSource === 'api' ? 'กลับไปเลือกช่วงวันที่ใหม่' : 'เลือกไฟล์ใหม่'
  const resetDoneLabel = previewSource === 'api' ? 'ดึงออเดอร์ใหม่' : 'นำเข้าไฟล์ใหม่'
  const connectConfirmDescription = [
    `Domain สำหรับ Shopee: ${CANONICAL_PUBLIC_URL}`,
    `หน้าที่เปิดอยู่: ${currentHost || 'ไม่ทราบ host'}`,
    `Redirect ใน server: ${apiStatus?.redirect_url || 'ยังไม่ได้ตั้งค่า'}`,
    '',
    connectHostWarning
      ? 'คำเตือน: ตอนนี้ไม่ได้เปิดผ่าน ngrok canonical domain ถ้าเชื่อมร้านจาก LAN/IP อาจทำให้ Shopee redirect/callback ไม่ตรง domain'
      : 'Domain ปัจจุบันตรงกับ ngrok canonical domain สำหรับ Shopee callback',
    redirectHostWarning
      ? 'คำเตือน: redirect host ใน server ไม่ตรงกับ ngrok canonical domain ให้ตรวจ PUBLIC_BASE_URL / SHOPEE_OPEN_API_REDIRECT_URL ก่อนเชื่อมร้านจริง'
      : 'หลัง Shopee approve ระบบจะเปิด popup และ callback จะปิดหน้าต่างเองเมื่อเชื่อมสำเร็จ',
  ].join('\n')
  const disableConnectionDescription = disableConnection
    ? [
        `ร้าน: ${disableConnection.label || disableConnection.shop_name || 'Shopee shop'} · ${disableConnection.shop_id}`,
        'ผลกระทบ: ร้านนี้จะไม่ถูกใช้ดึง order ผ่าน API และจะไม่ปรากฏเป็นตัวเลือก import ปกติ',
        'ข้อมูลเดิม: เอกสารที่สร้างไว้แล้วใน Nexflow และเอกสารที่ส่งเข้า SML แล้วจะไม่ถูกลบ',
        'Rollback: เปิดใช้งานร้านนี้กลับมาได้จากหน้ารายละเอียดสำหรับแอดมิน',
      ].join('\n')
    : ''

  return (
    <div className="space-y-5">
      <PageHeader
        title="นำเข้า Shopee ย้อนหลัง"
        description={`ใช้เมื่อ order ไม่เข้า realtime, ต้องดึงช่วงวันที่ย้อนหลัง, หรือใช้ไฟล์ Excel จาก Seller Center เพื่อสร้าง${destination.documentName} (${destination.shortName}) ไว้ตรวจก่อนส่งเข้า SML`}
      />

      <div className="rounded-lg border border-info/30 bg-info/5 px-3 py-2">
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 text-sm font-medium text-foreground">
              <Info className="h-4 w-4 text-info" />
              <span>เมนูนี้ใช้สำหรับงานย้อนหลังและซ่อมข้อมูล</span>
              <Badge variant="outline" className="h-5 bg-background text-[11px]">ดึงย้อนหลัง</Badge>
              <Badge variant="outline" className="h-5 bg-background text-[11px]">ซ่อมรายการตกหล่น</Badge>
              <Badge variant="outline" className="h-5 bg-background text-[11px]">Excel fallback</Badge>
            </div>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              ถ้าเป็น order ใหม่ที่เข้าตามปกติ ให้เริ่มจากคำสั่งซื้อ Shopee เพื่อดูสถานะ, Timeline และสร้างเอกสารจากคิวประจำวัน
            </p>
          </div>
          <Button asChild variant="outline" size="sm" className="h-8 shrink-0 bg-background">
            <Link to="/shopee-operations">
              ไปคำสั่งซื้อ Shopee
              <ArrowRight className="h-3.5 w-3.5" />
            </Link>
          </Button>
        </div>
      </div>

      <Card className="border-border/80 bg-card/95 shadow-sm">
        <CardContent className="grid gap-3 p-4 sm:grid-cols-3">
          <SummaryCard label="พร้อมสร้างใหม่" value={preview?.new_count ?? 0} variant="primary" />
          <SummaryCard label="ซ้ำ/ข้าม" value={(preview?.duplicate_count ?? 0) + (preview?.skipped_count ?? 0)} variant="muted" />
          <SummaryCard label="ต้องตรวจ" value={previewIssueCount} variant={previewIssueCount > 0 ? 'danger' : 'success'} />
        </CardContent>
      </Card>

      <input
        ref={fileRef}
        type="file"
        accept=".xlsx"
        className="sr-only"
        onChange={handleFileChange}
      />

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {sourceSelectionVisible && (
        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Store className="h-5 w-5 text-accent-strong" />
                  ดึงออเดอร์ย้อนหลังจาก Shopee
                </CardTitle>
                <Badge variant="outline">{destination.shortName}</Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-4 pt-0">
              <div className={cn('rounded-md border px-3 py-2 text-sm', apiSummaryClass)}>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="font-medium text-foreground">{apiSummary.title}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">{apiSummary.description}</p>
                  </div>
                  {apiStatus && (
                    <Button
                      variant={activeConnections.length > 0 ? 'outline' : 'default'}
                      size="sm"
                      onClick={() => setConfirmConnectOpen(true)}
                      disabled={apiConnectDisabled}
                      title={
                        apiStatus.blocking_reason ||
                        (connectHostWarning ? 'ควรเปิดผ่าน ngrok canonical domain ก่อนเชื่อมร้าน' : undefined)
                      }
                    >
                      {apiBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <PlugZap className="h-4 w-4" />}
                      {apiConnectLabel}
                    </Button>
                  )}
                </div>
              </div>

              {apiStatus ? (
                <>
                  <div className="rounded-md border border-border bg-muted/15 p-3">
                    <div className="grid gap-3 lg:grid-cols-[minmax(220px,0.9fr)_minmax(260px,1fr)_minmax(170px,0.72fr)_minmax(220px,0.9fr)_auto] lg:items-end">
                    <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                      ร้านค้า
                      {activeConnections.length > 1 ? (
                        <Select
                          value={selectedConnectionID || NO_SHOP_SELECTED}
                          onValueChange={(value) => setSelectedConnectionID(value === NO_SHOP_SELECTED ? '' : value)}
                        >
                          <SelectTrigger className="h-10 bg-background text-sm">
                            <SelectValue placeholder="เลือกร้าน Shopee" />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value={NO_SHOP_SELECTED}>เลือกร้าน Shopee</SelectItem>
                            {activeConnections.map((conn) => (
                              <SelectItem key={conn.id} value={conn.id}>
                                {conn.label || conn.shop_name || 'Shopee shop'} · {conn.shop_id}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : (
                        <div className="flex h-10 items-center rounded-md border border-border bg-background px-3 text-sm text-foreground">
                          <span className="truncate">{selectedConnection?.label || selectedConnection?.shop_name || 'ยังไม่มีร้านที่เชื่อมต่อ'}</span>
                        </div>
                      )}
                    </label>
                    <div className="space-y-1.5">
                      <div className="text-xs font-medium text-muted-foreground">ช่วงวันที่</div>
                      <DateRangePicker
                        from={apiFrom}
                        to={apiTo}
                        onFromChange={setAPIFrom}
                        onToChange={setAPITo}
                        presets={shopeeImportPresets}
                        title="ช่วงวันที่ Shopee"
                        description="ดึง order ได้ครั้งละไม่เกิน 15 วัน"
                        className="h-10 w-full min-w-0 bg-background text-sm"
                      />
                    </div>
                    <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                      ค้นหาจาก
                      <Select
                        value={apiTimeRangeField}
                        onValueChange={(value) => setAPITimeRangeField(value as 'create_time' | 'update_time')}
                      >
                        <SelectTrigger className="h-10 bg-background text-sm">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="create_time">วันที่สร้าง order</SelectItem>
                          <SelectItem value="update_time">วันที่อัปเดต order</SelectItem>
                        </SelectContent>
                      </Select>
                    </label>
                    <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                      สถานะ
                      <Select value={apiOrderStatus} onValueChange={setAPIOrderStatus}>
                        <SelectTrigger className="h-10 bg-background text-sm">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {shopeeOrderStatusOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </label>
                    <Button
                      className="h-10 self-end"
                      onClick={handleFetchAPI}
                      disabled={apiFetchDisabled}
                      title={apiDateError || (needsShopSelection ? 'เลือกร้าน Shopee ก่อนดึง order' : apiStatus.blocking_reason) || undefined}
                    >
                      {apiBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Database className="h-4 w-4" />}
                      ดึงออเดอร์
                    </Button>
                  </div>
                    {apiOrderStatus === 'ready_to_bill' && (
                      <p className="mt-2 text-[11px] leading-4 text-muted-foreground">
                        สถานะพร้อมออกบิลรวม order ที่จัดส่งแล้ว รอลูกค้ายืนยันรับสินค้า และสำเร็จแล้ว
                      </p>
                    )}
                  </div>

                  <ActionSafetyPanel
                    title="ตรวจขอบเขตการนำเข้า Shopee ย้อนหลัง"
                    description="ขั้นตอนนี้ใช้สำหรับดึงย้อนหลังหรือซ่อมรายการตกหล่น ยังไม่สร้างบิลจนกว่าจะกดยืนยันในขั้นตอนถัดไป"
                    tone="info"
                    items={[
                      {
                        label: 'ร้าน',
                        value: selectedConnection
                          ? `${selectedConnection.label || selectedConnection.shop_name || 'Shopee shop'} · ${selectedConnection.shop_id}`
                          : 'ยังไม่ได้เลือกร้าน',
                        detail: needsShopSelection ? 'ต้องเลือกร้านก่อนดึง order' : 'กันการดึงผิดร้านก่อนสร้างเอกสาร',
                      },
                      {
                        label: 'ช่วงวันที่',
                        value: `${apiFrom || '—'} ถึง ${apiTo || '—'}`,
                        detail: apiTimeRangeField === 'create_time' ? 'ค้นจากวันที่สร้าง order' : 'ค้นจากวันที่อัปเดต order',
                      },
                      {
                        label: 'สถานะ',
                        value: shopeeOrderStatusOptions.find((option) => option.value === apiOrderStatus)?.label || apiOrderStatus,
                        detail: 'ระบบจะตรวจซ้ำด้วย Order ID + ร้าน ก่อนสร้างเอกสาร',
                      },
                      {
                        label: 'หลังดึงข้อมูล',
                        value: 'ตรวจรายการก่อนสร้างเอกสาร',
                        detail: 'ยังไม่ส่ง SML และยังไม่แก้เอกสารเดิม',
                      },
                    ]}
                  />

                  {(apiDateError || apiLastSyncError || needsShopSelection) && (
                    <div className="space-y-1 text-xs">
                      {apiDateError && <p className="text-warning">{apiDateError}</p>}
                      {needsShopSelection && <p className="text-warning">เลือกร้าน Shopee ก่อนดึง order เพื่อกัน import ผิดร้าน</p>}
                      {apiLastSyncError && <p className="text-destructive">{apiLastSyncError}</p>}
                    </div>
                  )}
                  {configReady && !smlCustomerReady && (
                    <Alert>
                      <AlertTriangle className="h-4 w-4" />
                      <AlertTitle>ยังไม่ได้ตั้งค่าลูกค้า Shopee สำหรับส่ง SML</AlertTitle>
                      <AlertDescription>
                        ยังสร้างเอกสารไว้ตรวจใน Nexflow ได้ แต่ก่อนกดส่งเข้า SML ต้องตั้งค่าลูกค้า คลัง ชั้น และ VAT ให้ครบ
                        <Button asChild variant="link" className="h-auto px-1 py-0 text-xs">
                          <Link to="/settings/channels">ไปตั้งค่าตอนนี้</Link>
                        </Button>
                      </AlertDescription>
                    </Alert>
                  )}

                  <details className="group rounded-md border border-border bg-muted/20 text-sm">
                    <summary className="flex cursor-pointer list-none items-center justify-between gap-2 px-3 py-2 font-medium text-foreground">
                      รายละเอียดสำหรับแอดมิน
                      <ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
                    </summary>
                    <div className="space-y-3 border-t border-border p-3">
                      {apiReadiness && (
                        <div className={cn('rounded-md border px-3 py-2 text-xs', readinessToneClass(apiReadiness.tone))}>
                          <div className="flex items-start gap-2">
                            {apiReadiness.tone === 'success' ? (
                              <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" />
                            ) : (
                              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                            )}
                            <div className="min-w-0 flex-1">
                              <p className="font-medium">{apiReadiness.title}</p>
                              <p className="mt-0.5 text-muted-foreground">{apiReadiness.description}</p>
                              <div className="mt-2 grid gap-1.5 text-muted-foreground sm:grid-cols-2">
                                {apiReadiness.steps.map((s) => (
                                  <div key={s.label} className="flex min-w-0 items-start gap-1.5">
                                    {readinessStepIcon(s)}
                                    <span className="min-w-0">
                                      <span>{s.label}</span>
                                      {s.detail && (
                                        <span className="block truncate text-[11px]" title={s.detail}>
                                          {s.detail}
                                        </span>
                                      )}
                                    </span>
                                  </div>
                                ))}
                              </div>
                            </div>
                          </div>
                        </div>
                      )}

                      <div className="rounded-md border border-border bg-background p-3">
                        <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                          <Store className="h-4 w-4 text-accent-strong" />
                          ร้าน Shopee ที่เชื่อมต่อ
                        </div>
                        {apiStatus && !apiStatus.enabled ? (
                          <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs text-warning">
                            Shopee API ปิดใช้งานใน instance นี้ จึงซ่อนร้านที่เคยเชื่อมต่อไว้ก่อน
                            เพื่อไม่ให้เข้าใจผิดว่าสามารถดึง order ได้
                          </div>
                        ) : apiConnections.length === 0 ? (
                          <p className="text-xs text-muted-foreground">ยังไม่มีร้านที่เชื่อมต่อ</p>
                        ) : (
                          <div className="space-y-2">
                            {apiConnections.map((conn) => {
                              const editing = editingConnectionID === conn.id
                              const disabled = Boolean(conn.disabled_at)
                              return (
                                <div
                                  key={conn.id}
                                  className={cn(
                                    'grid gap-2 rounded-md border border-border bg-background p-2 text-xs md:grid-cols-[minmax(0,1fr)_auto]',
                                    disabled && 'opacity-60',
                                  )}
                                >
                                  <div className="min-w-0">
                                    {editing ? (
                                      <input
                                        value={editingLabel}
                                        onChange={(e) => setEditingLabel(e.target.value)}
                                        className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs text-foreground"
                                        maxLength={120}
                                      />
                                    ) : (
                                      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                        <span className="truncate font-medium text-foreground">
                                          {conn.label || conn.shop_name || 'Shopee shop'}
                                        </span>
                                        <Badge variant="outline" className="font-mono text-[10px]">
                                          {conn.shop_id}
                                        </Badge>
                                        {selectedConnectionID === conn.id && !disabled && (
                                          <Badge className="text-[10px]">กำลังใช้</Badge>
                                        )}
                                        {disabled && (
                                          <Badge variant="secondary" className="text-[10px]">
                                            ปิดใช้งาน
                                          </Badge>
                                        )}
                                      </div>
                                    )}
                                    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
                                      <span>Token: {tokenStateLabel(conn.token_state)}</span>
                                      <span>Last sync: {conn.last_sync_at ? fmtDateTime(conn.last_sync_at) : '—'}</span>
                                      {conn.last_sync_error && (
                                        <span className="text-destructive" title={conn.last_sync_error}>
                                          sync error
                                        </span>
                                      )}
                                    </div>
                                  </div>
                                  <div className="flex items-center justify-end gap-1">
                                    {editing ? (
                                      <>
                                        <Button type="button" size="sm" variant="outline" className="h-8 px-2" onClick={() => saveConnectionLabel(conn)} disabled={apiBusy}>
                                          <Save className="h-3.5 w-3.5" />
                                          บันทึก
                                        </Button>
                                        <Button type="button" size="sm" variant="ghost" className="h-8 px-2" onClick={() => setEditingConnectionID('')} disabled={apiBusy}>
                                          <X className="h-3.5 w-3.5" />
                                        </Button>
                                      </>
                                    ) : (
                                      <>
                                        <Button type="button" size="sm" variant="ghost" className="h-8 px-2" onClick={() => startEditConnection(conn)} disabled={apiBusy}>
                                          <Pencil className="h-3.5 w-3.5" />
                                          ชื่อ
                                        </Button>
                                        <Button
                                          type="button"
                                          size="sm"
                                          variant={disabled ? 'outline' : 'ghost'}
                                          className="h-8 px-2"
                                          onClick={() => {
                                            if (disabled) void toggleConnectionDisabled(conn)
                                            else setDisableConnection(conn)
                                          }}
                                          disabled={apiBusy}
                                        >
                                          <Power className="h-3.5 w-3.5" />
                                          {disabled ? 'เปิดใช้' : 'ปิดใช้'}
                                        </Button>
                                      </>
                                    )}
                                  </div>
                                </div>
                              )
                            })}
                          </div>
                        )}
                      </div>

                      <div className="grid gap-2 text-xs text-muted-foreground md:grid-cols-3 xl:grid-cols-6">
                        <div className="rounded-md border border-border bg-background p-3">
                          <p className="font-medium text-foreground">Partner</p>
                          <p className="mt-1 font-mono">{apiStatus.partner_id || 'ยังไม่ได้ตั้งค่า'}</p>
                        </div>
                        <div className="rounded-md border border-border bg-background p-3">
                          <p className="font-medium text-foreground">Selected shop</p>
                          <p className="mt-1 truncate font-mono" title={apiEnabled ? selectedShopHint : 'Shopee API ปิดใช้งาน'}>
                            {apiEnabled ? selectedConnection?.shop_id || '—' : 'ปิดใช้งาน'}
                          </p>
                        </div>
                        <div className="rounded-md border border-border bg-background p-3">
                          <p className="font-medium text-foreground">Base URL</p>
                          <p className="mt-1 truncate font-mono" title={apiStatus.base_url || '—'}>
                            {apiStatus.base_url || '—'}
                          </p>
                        </div>
                        <div className="rounded-md border border-border bg-background p-3">
                          <p className="font-medium text-foreground">Redirect</p>
                          <p className="mt-1 truncate font-mono" title={apiStatus.redirect_url || '—'}>
                            {apiStatus.redirect_url || '—'}
                          </p>
                        </div>
                        <div className="rounded-md border border-border bg-background p-3">
                          <p className="font-medium text-foreground">Token</p>
                          <p className="mt-1">{tokenStateLabel(apiStatus.token_state)}</p>
                        </div>
                        <div className="rounded-md border border-border bg-background p-3">
                          <p className="font-medium text-foreground">Last sync</p>
                          <p className="mt-1">
                            {apiStatus.last_sync_at ? fmtDateTime(apiStatus.last_sync_at) : '—'}
                          </p>
                        </div>
                      </div>
                    </div>
                  </details>
                </>
              ) : (
                <Alert>
                  <AlertTriangle className="h-4 w-4" />
                  <AlertTitle>{apiLoadError ? 'ยังโหลดสถานะ Shopee API ไม่ได้' : 'กำลังตรวจสถานะ Shopee API'}</AlertTitle>
                  <AlertDescription>
                    {apiLoadError || 'กำลังโหลดสถานะร้านและการเชื่อมต่อ ระหว่างนี้ยังใช้ Excel สำรองได้'}
                  </AlertDescription>
                </Alert>
              )}
            </CardContent>
          </Card>

          <details className="group rounded-lg border border-border bg-card">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-2 px-4 py-3 text-sm font-medium text-foreground">
              <span className="flex items-center gap-2">
                <FileSpreadsheet className="h-4 w-4 text-muted-foreground" />
                นำเข้าจากไฟล์ Excel กรณี API ใช้งานไม่ได้
              </span>
              <ChevronDown className="h-4 w-4 text-muted-foreground transition-transform group-open:rotate-180" />
            </summary>
            <div className="border-t border-border p-4">
              <div
                className={cn(
                  'flex min-h-[112px] flex-col items-center justify-center rounded-md border border-dashed border-border bg-muted/20 p-4 text-center',
                  step === 'uploading' && !apiBusy && 'opacity-60',
                )}
              >
                {step === 'uploading' && !apiBusy ? (
                  <p className="text-sm text-muted-foreground">กำลังวิเคราะห์ไฟล์…</p>
                ) : (
                  <>
                    <p className="text-sm font-medium text-foreground">ไฟล์ Excel (.xlsx) จาก Shopee Seller Center</p>
                    <p className="mt-1 text-xs leading-5 text-muted-foreground">ใช้เมื่อ API มีปัญหา หรือยังต้องนำเข้าจากไฟล์เดิม</p>
                    <Button
                      variant="outline"
                      size="sm"
                      className="mt-3"
                      onClick={() => fileRef.current?.click()}
                      disabled={!configReady}
                      title={!configReady ? 'กำลังเตรียมหน้า import' : undefined}
                    >
                      {configLoading ? 'กำลังโหลด config…' : 'เลือกไฟล์ Shopee'}
                    </Button>
                  </>
                )}
              </div>
            </div>
          </details>

          {recentRuns.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-sm">
                  <Clock3 className="h-4 w-4 text-muted-foreground" />
                  ประวัติการนำเข้าล่าสุด
                </CardTitle>
              </CardHeader>
              <CardContent className="grid gap-2 pt-0 md:grid-cols-2">
                {recentRuns.map((run) => (
                  <div
                    key={run.id}
                    className="grid gap-2 rounded-md border border-border px-3 py-2 text-xs"
                  >
                    <div className="min-w-0">
                      <div className="truncate font-medium text-foreground">
                        {run.filename || 'Shopee Import'}
                      </div>
                      <div className="mt-0.5 text-muted-foreground">
                        {fmtDateTime(run.created_at)}
                        {run.period_start && run.period_end
                          ? ` · ${run.period_start} ถึง ${run.period_end}`
                          : ''}
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-1">
                      <Badge variant={run.status === 'confirmed' ? 'default' : 'secondary'}>
                        {run.status === 'confirmed' ? 'สร้างแล้ว' : 'ตรวจรายการ'}
                      </Badge>
                      <Badge variant="outline">ใหม่ {run.new_orders}</Badge>
                      <Badge variant="outline">ซ้ำ {run.duplicate_orders}</Badge>
                      <Badge variant="outline">ข้าม {run.skipped_orders}</Badge>
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
          )}
        </div>
      )}

      {step === 'preview' && preview && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <SummaryCard
              label="ทั้งหมด"
              value={preview.total_orders}
              variant="primary"
            />
            <SummaryCard
              label="ใหม่"
              value={preview.preflight?.new_orders ?? preview.new_count}
              variant="success"
            />
            <SummaryCard
              label="เลือกแล้ว"
              value={selectedIDs.size}
              variant="success"
            />
            <SummaryCard
              label="มีปัญหาต้องตรวจ"
              value={previewIssueCount}
              variant={previewIssueCount > 0 ? 'danger' : 'muted'}
            />
          </div>

          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>นโยบายการนำเข้าซ้ำ</AlertTitle>
            <AlertDescription>
              ถ้าไฟล์หรือ API ครอบคลุมช่วงวันที่เดิม ระบบจะสร้างเฉพาะ Order ID ที่ยังไม่มีในร้าน Shopee เดียวกัน และจะข้ามรายการซ้ำโดยไม่เขียนทับบิลเดิม
            </AlertDescription>
          </Alert>

          {(preview.orders.some((o) => o.shopee_shop_id) || selectedConnection) && (
            <Alert>
              <Store className="h-4 w-4" />
              <AlertTitle>ร้านที่ใช้กับ preview นี้</AlertTitle>
              <AlertDescription>
                {preview.orders.find((o) => o.shopee_shop_label)?.shopee_shop_label || selectedConnection?.label || 'Shopee shop'}
                {' · '}
                {preview.orders.find((o) => o.shopee_shop_id)?.shopee_shop_id || selectedConnection?.shop_id}
              </AlertDescription>
            </Alert>
          )}

          {configReady && !smlCustomerReady && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>สร้างเอกสารเพื่อตรวจได้ แต่ยังส่ง SML ไม่ได้</AlertTitle>
              <AlertDescription>
                ยังไม่ได้ตั้งค่าลูกค้า Shopee สำหรับปลายทาง SML ระบบจะสร้างเอกสารใน Nexflow ให้ตรวจสินค้าและ SKU ก่อน
                แล้วค่อยตั้งค่าลูกค้าในเมนูช่องทางรับข้อมูลก่อนกดส่ง SML
                <Button asChild variant="link" className="h-auto px-1 py-0 text-xs">
                  <Link to="/settings/channels">ไปตั้งค่าตอนนี้</Link>
                </Button>
              </AlertDescription>
            </Alert>
          )}

          {previewHasNoOrders && (
            <Card className="border-dashed">
              <CardContent className="flex flex-col items-center gap-2 px-6 py-10 text-center">
                <Database className="h-8 w-8 text-muted-foreground" />
                <div>
                  <p className="text-sm font-semibold text-foreground">
                    ไม่พบ order ในช่วงที่เลือก
                  </p>
                  <p className="mt-1 max-w-xl text-sm leading-6 text-muted-foreground">
                    ลองขยายช่วงวันที่ เปลี่ยนสถานะ order หรือสลับจากวันที่สร้างเป็นวันที่อัปเดต แล้วกดดึงออเดอร์ใหม่อีกครั้ง
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setStep('idle')
                    setPreview(null)
                  }}
                >
                  {resetPreviewLabel}
                </Button>
              </CardContent>
            </Card>
          )}

          {preview.more && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ยังมี order เพิ่มเติมใน Shopee</AlertTitle>
              <AlertDescription>
                ช่วงวันที่นี้มีข้อมูลมากกว่าหนึ่งหน้า ลดช่วงวันที่หรือเลือกสถานะแยกก่อนยืนยันนำเข้า เพื่อไม่ให้ order ตกหล่น
              </AlertDescription>
            </Alert>
          )}

          {(preview.preflight?.no_sku_items ?? 0) > 0 && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ไฟล์นี้ไม่มี SKU บางรายการ</AlertTitle>
              <AlertDescription>
                พบ {preview.preflight.no_sku_items} รายการสินค้าใน {preview.preflight.no_sku_orders} order ที่ไม่มี SKU ระบบจะใช้ชื่อสินค้า + ตัวเลือกสินค้าเป็นข้อมูลจับคู่แทน
              </AlertDescription>
            </Alert>
          )}

          {(preview.warnings ?? []).length > 0 && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>คำเตือน ({preview.warnings.length} รายการ)</AlertTitle>
              <AlertDescription>
                <ul className="mt-1 list-disc pl-5 text-xs">
                  {preview.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          )}

          {!previewHasNoOrders && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" size="sm" onClick={toggleAll}>
                  {selectedIDs.size === preview.orders.filter((o) => !o.duplicate).length
                    ? 'ยกเลิกทั้งหมด'
                    : 'เลือกทั้งหมด'}
                </Button>
                <Button
                  size="sm"
                  disabled={confirmDisabled}
                  onClick={handleConfirm}
                  title={confirmTitle}
                >
                  {destination.action} {selectedIDs.size} รายการ
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setStep('idle')
                    setPreview(null)
                  }}
                >
                  {resetPreviewLabel}
                </Button>
              </div>

              <div className="overflow-hidden rounded-lg border border-border bg-card">
                <Table>
                  <TableHeader>
                    <TableRow className="bg-muted/40">
                      <TableHead className="w-10">
                        <Checkbox
                          checked={
                            selectedIDs.size ===
                            preview.orders.filter((o) => !o.duplicate).length
                          }
                          onCheckedChange={toggleAll}
                          aria-label="เลือกทั้งหมด"
                        />
                      </TableHead>
                      <TableHead>Order ID</TableHead>
                      <TableHead>วันที่</TableHead>
                      <TableHead>ผู้ซื้อ</TableHead>
                      <TableHead>สถานะ</TableHead>
                      <TableHead>สินค้า</TableHead>
                      <TableHead className="text-right">Qty รวม</TableHead>
                      <TableHead className="text-right">ยอดชำระ</TableHead>
                      <TableHead>ตรวจเบื้องต้น</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.orders.map((order) => {
                  const expanded = expandedOrders.has(order.order_id)
                  return (
                    <Fragment key={order.order_id}>
                      <TableRow
                        className={cn(
                          order.duplicate && 'bg-muted/30 text-muted-foreground',
                        )}
                      >
                        <TableCell>
                          <Checkbox
                            checked={selectedIDs.has(order.order_id)}
                            disabled={order.duplicate}
                            onCheckedChange={() => toggleOrder(order.order_id)}
                            aria-label={`เลือก order ${order.order_id}`}
                          />
                        </TableCell>
                        <TableCell>
                          <div className="space-y-1">
                            <button
                              type="button"
                              className="inline-flex items-center gap-1 font-mono text-xs font-medium text-foreground hover:text-link"
                              onClick={() => toggleExpand(order.order_id)}
                            >
                              {expanded ? (
                                <ChevronDown className="h-3 w-3" />
                              ) : (
                                <ChevronRight className="h-3 w-3" />
                              )}
                              {order.order_id}
                            </button>
                            {order.shopee_shop_id && (
                              <div className="flex max-w-[220px] items-center gap-1 text-[11px] text-muted-foreground">
                                <Store className="h-3 w-3 shrink-0" />
                                <span className="truncate">
                                  {order.shopee_shop_label || 'Shopee shop'} · {order.shopee_shop_id}
                                </span>
                              </div>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-xs tabular-nums text-muted-foreground">
                          {order.order_datetime || order.doc_date}
                        </TableCell>
                        <TableCell className="max-w-[160px] truncate text-xs text-muted-foreground">
                          {order.buyer_username || '—'}
                        </TableCell>
                        <TableCell>
                          <Badge variant="secondary" className="text-xs font-normal">
                            {order.status}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-xs">
                          {order.item_count} รายการ
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {order.total_qty}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          <div>{order.paid_amount != null ? `฿${fmt(order.paid_amount)}` : '—'}</div>
                          {!!order.shipping_amount && (
                            <div className="text-[11px] text-muted-foreground">
                              ส่ง ฿{fmt(order.shipping_amount)}
                            </div>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                          {order.duplicate && (
                            <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/20">
                              มีในระบบแล้ว
                            </Badge>
                          )}
                          {order.has_no_sku && (
                            <Badge variant="outline" className="border-warning/40 text-warning">
                              ไม่มี SKU {order.no_sku_item_count}
                            </Badge>
                          )}
                          {order.multi_line && (
                            <Badge variant="outline">หลายรายการ</Badge>
                          )}
                          {order.amount_mismatch && (
                            <Badge variant="outline">
                              มีส่วนต่างยอด
                            </Badge>
                          )}
                          </div>
                        </TableCell>
                      </TableRow>
                      {expanded && (
                        <TableRow>
                          <TableCell colSpan={9} className="bg-muted/20 p-0">
                            <div className="overflow-hidden border-l-2 border-primary/40">
                              <div className="grid gap-2 border-b border-border bg-background/60 p-3 text-xs text-muted-foreground sm:grid-cols-4">
                                <div>
                                  <span className="font-medium text-foreground">ขนส่ง</span>
                                  <div>{order.shipping_carrier || '—'}</div>
                                </div>
                                <div>
                                  <span className="font-medium text-foreground">Package</span>
                                  <div className="font-mono">{order.package_number || order.tracking_no || '—'}</div>
                                </div>
                                <div>
                                  <span className="font-medium text-foreground">ค่าส่ง</span>
                                  <div>{order.shipping_amount != null ? `฿${fmt(order.shipping_amount)}` : '—'}</div>
                                </div>
                                <div>
                                  <span className="font-medium text-foreground">ชำระเงิน</span>
                                  <div>{order.cod ? 'COD' : order.payment_channel || '—'}</div>
                                </div>
                              </div>
                              <Table>
                                <TableHeader>
                                  <TableRow className="bg-muted/30">
                                    <TableHead className="text-[10px] uppercase">SKU</TableHead>
                                    <TableHead className="text-[10px] uppercase">ชื่อสินค้า</TableHead>
                                    <TableHead className="text-[10px] uppercase">ตัวเลือก</TableHead>
                                    <TableHead className="text-right text-[10px] uppercase">ราคา</TableHead>
                                    <TableHead className="text-right text-[10px] uppercase">จำนวน</TableHead>
                                  </TableRow>
                                </TableHeader>
                                <TableBody>
                                  {order.items.map((item, i) => (
                                    <TableRow key={i}>
                                      <TableCell className="font-mono text-xs">
                                        {item.sku || (
                                          <Badge variant="outline" className="border-warning/40 text-warning">
                                            ไม่มี SKU
                                          </Badge>
                                        )}
                                      </TableCell>
                                      <TableCell className="text-sm">
                                        <div>{item.raw_name || item.product_name}</div>
                                        {item.raw_name && item.raw_name !== item.product_name && (
                                          <div className="mt-1 text-[11px] text-muted-foreground">
                                            ต้นทาง: {item.product_name}
                                          </div>
                                        )}
                                      </TableCell>
                                      <TableCell className="text-xs text-muted-foreground">
                                        {item.option_name || '—'}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {fmt(item.price)}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {item.qty}
                                      </TableCell>
                                    </TableRow>
                                  ))}
                                </TableBody>
                              </Table>
                            </div>
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  )
                    })}
                  </TableBody>
                </Table>
              </div>
            </>
          )}
        </>
      )}

      {step === 'confirming' && (
        <Card className="overflow-hidden">
          <CardContent className="p-0">
            <div className="border-b border-border bg-muted/30 px-5 py-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-semibold text-foreground">
                    กำลัง{destination.action}จาก {previewSource === 'api' ? 'Shopee API' : 'Shopee Excel'}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    ระบบกำลังจับคู่สินค้าและสร้างเอกสารไว้รอตรวจ ยังไม่ส่งเข้า SML อัตโนมัติ
                  </p>
                </div>
                <Badge variant="secondary" className="gap-1.5">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  {confirmElapsed}s
                </Badge>
              </div>
              <div className="mt-4 h-2 overflow-hidden rounded-full bg-background">
                <div
                  className="h-full rounded-full bg-primary transition-all duration-500"
                  style={{ width: `${Math.min(92, 18 + confirmElapsed * 2)}%` }}
                />
              </div>
            </div>
            <div className="grid gap-3 p-5 sm:grid-cols-3">
              <div className="rounded-md border border-border p-3">
                <Database className="h-4 w-4 text-accent-strong" />
                <p className="mt-2 text-xs font-medium">Order ที่เลือก</p>
                <p className="mt-1 text-2xl font-semibold tabular-nums">
                  {selectedIDs.size}
                </p>
              </div>
              <div className="rounded-md border border-border p-3">
                <ShieldCheck className="h-4 w-4 text-success" />
                <p className="mt-2 text-xs font-medium">กันนำเข้าซ้ำ</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Order ID ที่มีแล้วจะถูกข้าม ไม่เขียนทับบิลเดิม
                </p>
              </div>
              <div className="rounded-md border border-border p-3">
                <Clock3 className="h-4 w-4 text-warning" />
                <p className="mt-2 text-xs font-medium">กรุณารอหน้านี้</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  ถ้าเปลี่ยนเมนู งานบน server อาจยังทำต่อ ให้ดูผลย้อนหลังในประวัติการนำเข้า
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {step === 'done' && results && (
        <>
          <Alert>
            <CheckCircle2 className="h-4 w-4 text-success" />
            <AlertTitle>{destination.done} {results.success_count} รายการ</AlertTitle>
            <AlertDescription>
              ระบบ map สินค้าให้เบื้องต้น แต่ <b>ยังไม่ส่ง SML</b> — กรุณาไปที่เมนู{destination.listName}เพื่อตรวจสินค้า
              หน่วย จำนวน ราคา และส่งเข้า SML ปลายทาง {destination.smlPath}
            </AlertDescription>
          </Alert>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <SummaryCard
              label="สร้างเอกสารสำเร็จ"
              value={results.success_count}
              variant="success"
            />
            <SummaryCard
              label="ข้าม / ล้มเหลว"
              value={results.fail_count}
              variant="danger"
            />
            <SummaryCard
              label="ทั้งหมด"
              value={results.results.length}
              variant="primary"
            />
          </div>

          <div className="flex gap-2">
            <Button asChild>
              <Link to={destination.listPath}>
                ไปตรวจ{destination.listName}
                <ArrowRight className="h-4 w-4" />
              </Link>
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                setStep('idle')
                setPreview(null)
                setResults(null)
              }}
            >
              {resetDoneLabel}
            </Button>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-sm">รายละเอียดผลลัพธ์</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow className="bg-muted/40">
                    <TableHead>Order ID</TableHead>
                    <TableHead>ผล</TableHead>
                    <TableHead>หมายเหตุ</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {results.results.map((r) => (
                    <TableRow key={r.order_id}>
                      <TableCell className="font-mono text-xs">{r.order_id}</TableCell>
                      <TableCell>
                        {r.success ? (
                          r.bill_id ? (
                            <Link
                              to={`${destination.listPath}/${r.bill_id}`}
                              className="inline-flex items-center gap-1 font-medium text-success hover:underline"
                            >
                              เปิดรายละเอียด
                              <ArrowRight className="h-3 w-3" />
                            </Link>
                          ) : (
                            <span className="font-medium text-success">สำเร็จ</span>
                          )
                        ) : (
                          r.bill_id ? (
                            <Link
                              to={`${destination.listPath}/${r.bill_id}`}
                              className="inline-flex items-center gap-1 font-medium text-warning hover:underline"
                            >
                              เปิดรายการเดิม
                              <ArrowRight className="h-3 w-3" />
                            </Link>
                          ) : (
                            <span className="font-medium text-destructive">
                              ข้าม / ล้มเหลว
                            </span>
                          )
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {r.message}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
      <ConfirmDialog
        open={confirmConnectOpen}
        onOpenChange={setConfirmConnectOpen}
        title="เปิด Shopee OAuth ผ่าน ngrok domain?"
        description={connectConfirmDescription}
        confirmLabel={activeConnections.length > 0 ? 'เชื่อมร้านเพิ่ม' : 'เชื่อมต่อร้าน Shopee'}
        onConfirm={handleConnectAPI}
      />
      <ConfirmDialog
        open={disableConnection !== null}
        onOpenChange={(open) => !open && setDisableConnection(null)}
        title="ปิดใช้งานร้าน Shopee นี้?"
        description={disableConnectionDescription}
        confirmLabel="ปิดใช้งานร้าน"
        variant="destructive"
        onConfirm={async () => {
          if (!disableConnection) return
          await toggleConnectionDisabled(disableConnection)
          setDisableConnection(null)
        }}
      />
    </div>
  )
}
