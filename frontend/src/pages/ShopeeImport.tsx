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
  ShieldCheck,
  Store,
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
import { DateRangePicker, type DateRangePreset } from '@/components/common/DateRangePicker'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'

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
  gross_amount?: number
  discount_amount?: number
  platform_discount_amount?: number
  seller_discount_amount?: number
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
  platform_discount_amount?: number
  seller_discount_amount?: number
  net_product_amount?: number
  amount_difference?: number
  amount_review_reason?: string
  no_sku_item_count?: number
  has_no_sku?: boolean
  multi_line?: boolean
  amount_mismatch?: boolean
  existing_bill_id?: string
  shopee_shop_id?: string
  shopee_connection_id?: string
  shopee_shop_label?: string
  duplicate: boolean
  blocked_reason?: 'realtime_managed' | string
  realtime_status?: string
  realtime_bill_id?: string
  action_url?: string
}
interface ImportPreflight {
  new_orders: number
  duplicate_orders: number
  skipped_rows: number
  no_sku_orders: number
  no_sku_items: number
  multi_item_orders: number
  amount_mismatch_orders: number
  realtime_managed_orders?: number
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
  preview_token?: string
  more?: boolean
  next_cursor?: string
}
interface ConfirmResult {
  order_id: string
  success: boolean
  bill_id?: string
  doc_no?: string
  message?: string
  blocked_reason?: 'realtime_managed' | string
  action_url?: string
  realtime_status?: string
  realtime_bill_id?: string
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
  token_state?: string
  can_fetch?: boolean
  connected_at?: string
  updated_at?: string
}

type APIStatusTone = 'success' | 'warning' | 'danger' | 'muted'

interface ShopeeAPIStatus {
  enabled: boolean
  configured: boolean
  environment: string
  connected: boolean
  shop_id?: number
  shop_name?: string
  last_sync_at?: string
  last_sync_status?: string
  last_sync_error?: string
  token_state?: string
  can_fetch?: boolean
  blocking_reason?: string
}

type PreviewSource = 'excel' | 'api'

const shopeeImportPresets: DateRangePreset[] = [
  {
    label: 'วันนี้',
    getRange: () => {
      const today = dayjs().format('YYYY-MM-DD')
      return { from: today, to: today }
    },
  },
  {
    label: '3 วัน',
    getRange: () => ({
      from: dayjs().subtract(2, 'day').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
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

const shopeeOrderStatusOptions = [
  { value: 'ready_to_bill', label: 'พร้อมออกบิล' },
  { value: 'all', label: 'ทุกสถานะ' },
  { value: 'ready_to_ship', label: 'รอจัดส่ง' },
  { value: 'processed', label: 'จัดเตรียมแล้ว' },
  { value: 'shipped', label: 'จัดส่งแล้ว' },
  { value: 'to_confirm_receive', label: 'รอยืนยันรับสินค้า' },
  { value: 'completed', label: 'สำเร็จแล้ว' },
]

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

function apiErrorMessage(err: unknown, fallback: string) {
  const data = (err as { response?: { data?: { error?: string; error_code?: string } } })?.response?.data
  const raw = data?.error?.trim()
  switch (data?.error_code) {
    case 'not_configured':
      return 'Shopee Open API ยังไม่ได้ตั้งค่า Partner ID/Key บน server ให้ใช้ไฟล์ Excel หรือแจ้งแอดมินตั้งค่าก่อน'
    case 'redirect_not_ready':
      return 'Redirect URL ยังไม่พร้อม ให้ไปจัดการร้าน Shopee และตรวจ domain ก่อนเชื่อมต่อ'
    case 'not_connected':
      return 'ยังไม่ได้เชื่อมต่อร้าน Shopee ให้ไปจัดการร้าน Shopee หรือใช้ไฟล์ Excel'
    case 'token_error':
      return 'Shopee token ใช้งานไม่ได้หรือหมดอายุ ให้ไปจัดการร้าน Shopee แล้วเชื่อมร้านใหม่'
    case 'permission_denied':
      return 'Shopee ยังไม่อนุญาตสิทธิ์นี้ ให้ตรวจสิทธิ์ร้าน หรือใช้ไฟล์ Excel แทน'
    default:
      return raw || fallback
  }
}

function apiRangeError(from: string, to: string) {
  if (!from || !to) return 'เลือกวันที่เริ่มต้นและสิ้นสุดให้ครบ'
  const fromDate = new Date(`${from}T00:00:00`)
  const toDate = new Date(`${to}T00:00:00`)
  if (Number.isNaN(fromDate.getTime()) || Number.isNaN(toDate.getTime())) return 'รูปแบบวันที่ไม่ถูกต้อง'
  if (toDate < fromDate) return 'วันที่สิ้นสุดต้องไม่ก่อนวันที่เริ่มต้น'
  const days = Math.floor((toDate.getTime() - fromDate.getTime()) / 86400000) + 1
  if (days > 15) return 'Shopee API ดึงได้ไม่เกิน 15 วันต่อครั้ง ให้ลดช่วงวันที่'
  return ''
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

function apiStatusToneClass(tone: APIStatusTone) {
  if (tone === 'success') return 'border-success/30 bg-success/5 text-success'
  if (tone === 'danger') return 'border-destructive/30 bg-destructive/5 text-destructive'
  if (tone === 'warning') return 'border-warning/35 bg-warning/10 text-warning'
  return 'border-border bg-muted/30 text-muted-foreground'
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

const NO_SHOP_SELECTED = '__no_shop_selected__'

function isOrderBlocked(order: Pick<ShopeeOrder, 'duplicate' | 'blocked_reason'>) {
  return Boolean(order.duplicate || order.blocked_reason)
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
  const apiStatusRequestSeq = useRef(0)
  const apiPreviewRequestSeq = useRef(0)
  const [step, setStep] = useState<Step>('idle')
  const [config, setConfig] = useState<ShopeeConfig | null>(null)
  const [preview, setPreview] = useState<PreviewResponse | null>(null)
  const [previewSource, setPreviewSource] = useState<PreviewSource>('excel')
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
  const [apiConnections, setAPIConnections] = useState<ShopeeAPIConnection[]>([])
  const [selectedConnectionID, setSelectedConnectionID] = useState('')
  const [apiHelperOpen, setAPIHelperOpen] = useState(false)
  const [apiStatus, setAPIStatus] = useState<ShopeeAPIStatus | null>(null)
  const [apiStatusLoading, setAPIStatusLoading] = useState(false)
  const [apiStatusError, setAPIStatusError] = useState('')
  const [apiBusy, setAPIBusy] = useState(false)
  const [apiFrom, setAPIFrom] = useState(() => dayjs().subtract(2, 'day').format('YYYY-MM-DD'))
  const [apiTo, setAPITo] = useState(() => dayjs().format('YYYY-MM-DD'))
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

  const invalidateAPIPreviewRequest = () => {
    apiPreviewRequestSeq.current += 1
    setAPIBusy(false)
  }

  const loadAPIStatus = async () => {
    const seq = apiStatusRequestSeq.current + 1
    apiStatusRequestSeq.current = seq
    setAPIStatusLoading(true)
    setAPIStatusError('')
    try {
      const res = await client.get<ShopeeAPIStatus>('/api/settings/shopee-api/status')
      if (apiStatusRequestSeq.current !== seq) return
      setAPIStatus(res.data)
    } catch (err: unknown) {
      if (apiStatusRequestSeq.current !== seq) return
      setAPIStatusError(apiErrorMessage(err, 'โหลดสถานะ Shopee API ไม่ได้ ให้ใช้ไฟล์ Excel หรือไปจัดการร้าน Shopee'))
    } finally {
      if (apiStatusRequestSeq.current === seq) setAPIStatusLoading(false)
    }
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
      .get<{ data: ShopeeAPIConnection[] }>('/api/shopee-api/connections')
      .then((res) => {
        if (!alive) return
        const rows = res.data.data ?? []
        setAPIConnections(rows)
        const firstActive = rows.find((c) => !c.disabled_at)
        if (firstActive) setSelectedConnectionID((current) => current || firstActive.id)
      })
      .catch(() => {
        if (!alive) return
        setAPIConnections([])
      })
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    if (!apiHelperOpen || apiStatus || apiStatusLoading) return
    void loadAPIStatus()
  }, [apiHelperOpen, apiStatus, apiStatusLoading])

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
    invalidateAPIPreviewRequest()
    setStep('uploading')
    setError('')
    setPreview(null)
    setPreviewSource('excel')
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
        new Set(res.data.orders.filter((o) => !isOrderBlocked(o)).map((o) => o.order_id)),
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

  const handleFetchAPI = async () => {
    const dateError = apiRangeError(apiFrom, apiTo)
    const selectedConnection = apiConnections.filter((c) => !c.disabled_at).find((c) => c.id === selectedConnectionID)
    const needsShopSelection = apiConnections.filter((c) => !c.disabled_at).length > 1 && !selectedConnection
    if (dateError || needsShopSelection || !selectedConnection) {
      setError(dateError || (needsShopSelection ? 'เลือกร้าน Shopee ก่อนดึง order เพื่อกัน import ผิดร้าน' : 'ยังไม่มีร้าน Shopee ที่พร้อมดึง order ให้ไปจัดการร้าน Shopee หรือใช้ไฟล์ Excel'))
      return
    }
    if (!apiStatus?.enabled || !apiStatus.configured || !selectedConnection.can_fetch) {
      setError('Shopee API หรือ token ร้านนี้ยังไม่พร้อม ให้ไปจัดการร้าน Shopee หรือใช้ไฟล์ Excel จาก Seller Center')
      return
    }

    const seq = apiPreviewRequestSeq.current + 1
    apiPreviewRequestSeq.current = seq
    setStep('uploading')
    setError('')
    setPreview(null)
    setPreviewSource('api')
    setResults(null)
    setAPIBusy(true)
    try {
      const res = await client.post<PreviewResponse>('/api/import/shopee/api/preview', {
        connection_id: selectedConnection.id,
        time_from: apiFrom,
        time_to: apiTo,
        time_range_field: apiTimeRangeField,
        order_status: apiOrderStatus,
        page_size: 50,
      })
      if (apiPreviewRequestSeq.current !== seq) return
      setPreviewSource('api')
      setPreview(res.data)
      setSelectedIDs(
        new Set(res.data.orders.filter((o) => !isOrderBlocked(o)).map((o) => o.order_id)),
      )
      setStep('preview')
      void loadAPIStatus()
    } catch (err: unknown) {
      if (apiPreviewRequestSeq.current !== seq) return
      setError(`${apiErrorMessage(err, 'ดึง order จาก Shopee API ไม่ได้')} ถ้าต้องทำงานต่อทันที ให้ใช้ไฟล์ Excel จาก Seller Center ได้`)
      setStep('idle')
      void loadAPIStatus()
    } finally {
      if (apiPreviewRequestSeq.current === seq) setAPIBusy(false)
    }
  }

  const handleConfirm = async () => {
    if (!preview || selectedIDs.size === 0) return
    setStep('confirming')
    setError('')
    try {
      const res = await client.post('/api/import/shopee/confirm', {
        preview_token: preview.preview_token,
        order_ids: Array.from(selectedIDs),
        import_run_id: preview.import_run_id,
      }, { timeout: 120000 })
      setResults(res.data)
      setStep('done')
      notifyWorkQueueChanged()
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
    const nonDup = preview.orders.filter((o) => !isOrderBlocked(o)).map((o) => o.order_id)
    setSelectedIDs(selectedIDs.size === nonDup.length ? new Set() : new Set(nonDup))
  }
  const toggleExpand = (id: string) =>
    setExpandedOrders((p) => {
      const s = new Set(p)
      if (s.has(id)) s.delete(id)
      else s.add(id)
      return s
    })
  const activeConnections = apiConnections.filter((c) => !c.disabled_at)
  const selectedConnection = activeConnections.find((c) => c.id === selectedConnectionID) ?? null
  const apiDateError = apiRangeError(apiFrom, apiTo)
  const apiNeedsShopSelection = activeConnections.length > 1 && !selectedConnection
  const selectedConnectionReady = Boolean(selectedConnection?.can_fetch)
  const apiCanFetch = Boolean(apiStatus?.enabled && apiStatus.configured && selectedConnectionReady)
  const apiFetchDisabled = apiBusy || apiStatusLoading || !apiCanFetch || !!apiDateError || apiNeedsShopSelection
  const apiLastSyncError = selectedConnection?.last_sync_error || apiStatus?.last_sync_error || ''
  const apiSummary = (() => {
    if (apiStatusLoading) {
      return {
        title: 'กำลังตรวจสถานะ Shopee API',
        description: 'ระหว่างนี้ยังเลือกไฟล์ Excel ได้ตามปกติ',
        tone: 'muted' as APIStatusTone,
      }
    }
    if (apiStatusError) {
      return {
        title: 'ยังโหลดสถานะ API ไม่ได้',
        description: apiStatusError,
        tone: 'warning' as APIStatusTone,
      }
    }
    if (!apiStatus) {
      return {
        title: 'ยังไม่ได้ตรวจสถานะ API',
        description: 'กดแสดงตัวช่วยเพื่อโหลดสถานะร้านและ token เฉพาะเมื่อจำเป็น',
        tone: 'muted' as APIStatusTone,
      }
    }
    if (!apiStatus.enabled || !apiStatus.configured) {
      return {
        title: !apiStatus.enabled ? 'Shopee API ปิดใช้งาน' : 'ต้องให้แอดมินตั้งค่า API',
        description: apiStatus.blocking_reason || 'ใช้ไฟล์ Excel ได้ตามปกติ หรือไปจัดการร้าน Shopee เพื่อตรวจค่าเชื่อมต่อ',
        tone: 'warning' as APIStatusTone,
      }
    }
    if (activeConnections.length === 0) {
      return {
        title: 'ยังไม่มีร้าน Shopee ที่เชื่อมต่อ',
        description: 'ไปจัดการร้าน Shopee เพื่อเชื่อมร้านก่อนดึง order จาก API หรือใช้ไฟล์ Excel',
        tone: 'warning' as APIStatusTone,
      }
    }
    if (apiNeedsShopSelection) {
      return {
        title: 'เลือกร้านก่อนดึง order',
        description: 'มีหลายร้านในระบบ เลือกร้านให้ตรงกับข้อมูลที่ต้องการดึงเพื่อกันสร้างเอกสารผิดร้าน',
        tone: 'warning' as APIStatusTone,
      }
    }
    if (selectedConnection && !selectedConnectionReady) {
      return {
        title: 'ร้านนี้ยังไม่พร้อมดึง order',
        description: `Token: ${tokenStateLabel(selectedConnection.token_state)} ไปจัดการร้าน Shopee หรือใช้ไฟล์ Excel`,
        tone: 'warning' as APIStatusTone,
      }
    }
    if (apiCanFetch) {
      return {
        title: 'พร้อมดึง order จาก Shopee API',
        description: selectedConnection
          ? `${selectedConnection.label || selectedConnection.shop_name || 'Shopee shop'} · ${selectedConnection.shop_id}`
          : 'เลือกร้าน Shopee แล้วดึงช่วงวันที่ที่ต้องการ',
        tone: 'success' as APIStatusTone,
      }
    }
    return {
      title: 'ยังดึง order ไม่ได้',
      description: apiStatus.blocking_reason || 'ตรวจ token หรือเลือกช่วงวันที่ใหม่ ถ้าต้องทำงานต่อให้ใช้ไฟล์ Excel',
      tone: 'warning' as APIStatusTone,
    }
  })()
  const confirmDisabled = selectedIDs.size === 0 || !!preview?.more
  const confirmTitle = preview?.more
    ? 'ลดช่วงวันที่หรือเลือกสถานะแยกก่อนยืนยันนำเข้า'
    : selectedIDs.size === 0
      ? 'เลือกรายการที่ต้องการสร้างเอกสาร'
      : undefined
  const previewIssueCount = preview
    ? (preview.more ? 1 : 0) +
      (preview.preflight?.no_sku_orders ?? 0) +
      (preview.preflight?.amount_mismatch_orders ?? 0) +
      (preview.warnings?.length ?? 0)
    : 0
  const previewHasNoOrders = !!preview && preview.orders.length === 0
  const resetPreviewLabel = previewSource === 'api' ? 'กลับไปเลือกช่วงวันที่ใหม่' : 'เลือกไฟล์ใหม่'
  const resetDoneLabel = previewSource === 'api' ? 'ดึงออเดอร์ใหม่' : 'นำเข้าไฟล์ใหม่'

  return (
    <div className="space-y-5">
      <PageHeader
        title="นำเข้า Shopee"
        description={`อัปโหลดไฟล์จาก Shopee Seller Center แล้วสร้างเป็น${destination.documentName}สำหรับตรวจและส่งเข้า SML`}
      />

      <Alert>
        <Info className="h-4 w-4" />
        <AlertTitle>ช่องทางรับข้อมูลสำหรับ{destination.shortName}</AlertTitle>
        <AlertDescription>
          หน้านี้ทำหน้าที่นำเข้าไฟล์เท่านั้น ระบบจะสร้างรายการไปที่เมนู{' '}
          <Link to={destination.listPath} className="font-medium text-link hover:underline">
            {destination.listName}
          </Link>{' '}
          เพื่อให้ตรวจสินค้า หน่วย จำนวน ราคา และส่งเข้า SML ปลายทาง{' '}
          <span className="font-medium text-foreground">{destination.smlPath}</span>
          {' '}ถ้าเป็น order ใหม่จาก realtime ให้เริ่มจาก{' '}
          <Link to="/shopee-operations" className="font-medium text-link hover:underline">
            คำสั่งซื้อ Shopee
          </Link>
        </AlertDescription>
      </Alert>

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

      {(step === 'idle' || step === 'uploading') && (
        <>
          {recentRuns.length > 0 && step === 'idle' && (
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
                        {run.filename || 'Shopee Excel'}
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

          {activeConnections.length > 1 && (
            <Card>
              <CardContent className="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_minmax(220px,320px)] sm:items-center">
                <div className="min-w-0">
                  <p className="text-sm font-medium text-foreground">ร้านของไฟล์นี้</p>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">
                    เลือกร้านให้ตรงกับไฟล์ Shopee เพื่อให้ระบบกันรายการซ้ำและเก็บที่มาของบิลได้ถูกต้อง
                  </p>
                </div>
                <Select
                  value={selectedConnectionID || NO_SHOP_SELECTED}
                  onValueChange={(value) => {
                    invalidateAPIPreviewRequest()
                    setSelectedConnectionID(value === NO_SHOP_SELECTED ? '' : value)
                  }}
                >
                  <SelectTrigger className="h-10 bg-background text-sm">
                    <SelectValue placeholder="เลือกร้าน Shopee" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NO_SHOP_SELECTED}>ไม่ระบุร้าน</SelectItem>
                    {activeConnections.map((conn) => (
                      <SelectItem key={conn.id} value={conn.id}>
                        {conn.label || conn.shop_name || 'Shopee shop'} · {conn.shop_id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </CardContent>
            </Card>
          )}

          <div
            className={cn(
              'flex flex-col items-center justify-center rounded-lg border-2 border-dashed border-border bg-muted/20 p-10 text-center',
              step === 'uploading' && 'opacity-60',
            )}
          >
            {step === 'uploading' ? (
              <p className="text-sm text-muted-foreground">กำลังวิเคราะห์ไฟล์…</p>
            ) : (
              <>
                <FileSpreadsheet className="mb-3 h-10 w-10 text-muted-foreground" />
                <p className="text-sm font-medium text-foreground">
                  คลิกเพื่อเลือกไฟล์ Excel (.xlsx) จาก Shopee
                </p>
                <p className="mt-1 text-[11px] text-muted-foreground">
                  รองรับไฟล์ที่ export จาก Shopee Seller Center สำหรับนำเข้าย้อนหลังหรือซ่อมรายการตกหล่น
                </p>
                <Button
                  className="mt-4"
                  onClick={() => fileRef.current?.click()}
                  disabled={!configReady}
                  title={!configReady ? 'กำลังเตรียมหน้า import' : undefined}
                >
                  {configLoading ? 'กำลังโหลด config…' : 'เลือกไฟล์ Shopee'}
                </Button>
              </>
            )}
          </div>

          {step === 'idle' && (
            <Card className="border-border bg-card">
              <CardContent className="space-y-4 p-4">
                <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                  <div className="flex min-w-0 items-start gap-3">
                    <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/40">
                      <Database className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-foreground">
                        ไม่มีไฟล์ Excel?
                      </p>
                      <p className="mt-1 text-xs leading-5 text-muted-foreground">
                        ใช้ตัวช่วยดึงจาก Shopee API เฉพาะกรณี order ตกหล่นหรือไม่มีไฟล์ ยังไม่สร้างบิลจนกว่าจะตรวจรายการและกดยืนยัน
                      </p>
                    </div>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0"
                    onClick={() => setAPIHelperOpen((open) => !open)}
                  >
                    <ChevronDown className={cn('h-4 w-4 transition-transform', apiHelperOpen && 'rotate-180')} />
                    {apiHelperOpen ? 'ซ่อนตัวช่วย' : 'แสดงตัวช่วยดึงจาก Shopee API'}
                  </Button>
                </div>

                {apiHelperOpen && (
                  <div className="space-y-4 border-t border-border pt-4">
                    <div className={cn('rounded-md border px-3 py-2 text-sm', apiStatusToneClass(apiSummary.tone))}>
                      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                        <div className="min-w-0">
                          <p className="font-medium">{apiSummary.title}</p>
                          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
                            {apiSummary.description}
                          </p>
                        </div>
                        <Button asChild variant="outline" size="sm" className="shrink-0 bg-background">
                          <Link to="/settings/shopee-connections">
                            <Store className="h-4 w-4" />
                            จัดการร้าน Shopee
                          </Link>
                        </Button>
                      </div>
                    </div>

                    <div className="grid gap-3 lg:grid-cols-[minmax(180px,0.9fr)_minmax(240px,1fr)_minmax(160px,0.7fr)_minmax(180px,0.8fr)_auto] lg:items-end">
                      <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                        ร้าน Shopee
                        {activeConnections.length > 1 ? (
                          <Select
                            value={selectedConnectionID || NO_SHOP_SELECTED}
                            onValueChange={(value) => {
                              invalidateAPIPreviewRequest()
                              setSelectedConnectionID(value === NO_SHOP_SELECTED ? '' : value)
                            }}
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
                            <span className="truncate">
                              {selectedConnection
                                ? `${selectedConnection.label || selectedConnection.shop_name || 'Shopee shop'} · ${selectedConnection.shop_id}`
                                : 'ยังไม่มีร้านที่เชื่อมต่อ'}
                            </span>
                          </div>
                        )}
                      </label>

                      <div className="space-y-1.5">
                        <div className="text-xs font-medium text-muted-foreground">ช่วงวันที่</div>
                        <DateRangePicker
                          from={apiFrom}
                          to={apiTo}
                          onFromChange={(value) => {
                            invalidateAPIPreviewRequest()
                            setAPIFrom(value)
                          }}
                          onToChange={(value) => {
                            invalidateAPIPreviewRequest()
                            setAPITo(value)
                          }}
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
                          onValueChange={(value) => {
                            invalidateAPIPreviewRequest()
                            setAPITimeRangeField(value as 'create_time' | 'update_time')
                          }}
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
                        <Select
                          value={apiOrderStatus}
                          onValueChange={(value) => {
                            invalidateAPIPreviewRequest()
                            setAPIOrderStatus(value)
                          }}
                        >
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
                        type="button"
                        className="h-10"
                        onClick={handleFetchAPI}
                        disabled={apiFetchDisabled}
                        title={apiDateError || (apiNeedsShopSelection ? 'เลือกร้าน Shopee ก่อนดึง order' : undefined)}
                      >
                        {apiBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Database className="h-4 w-4" />}
                        ดึงออเดอร์
                      </Button>
                    </div>

                    {(apiDateError || apiNeedsShopSelection || apiLastSyncError) && (
                      <div className="space-y-1 text-xs leading-5">
                        {apiDateError && <p className="text-warning">{apiDateError}</p>}
                        {apiNeedsShopSelection && <p className="text-warning">เลือกร้าน Shopee ก่อนดึง order เพื่อกัน import ผิดร้าน</p>}
                        {apiLastSyncError && <p className="text-destructive">{apiLastSyncError}</p>}
                      </div>
                    )}

                    <div className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                      <ShieldCheck className="h-4 w-4 text-success" />
                      <span>หลังดึง API ระบบจะแสดง preview ก่อน ยังไม่สร้างบิล ไม่ส่ง SML และยังไม่แก้เอกสารเดิม</span>
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          )}
        </>
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
              ถ้าไฟล์ Shopee ครอบคลุมช่วงวันที่เดิม ระบบจะสร้างเฉพาะ Order ID ที่ยังไม่มีในร้าน Shopee เดียวกัน และจะข้ามรายการซ้ำโดยไม่เขียนทับบิลเดิม
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
                แล้วค่อยตั้งค่าลูกค้าในเมนูเชื่อมต่อแพลตฟอร์มก่อนกดส่ง SML
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
                    {previewSource === 'api' ? 'ไม่พบ order ในช่วงวันที่นี้' : 'ไม่พบ order ในไฟล์นี้'}
                  </p>
                  <p className="mt-1 max-w-xl text-sm leading-6 text-muted-foreground">
                    {previewSource === 'api'
                      ? 'ลองขยายช่วงวันที่ เปลี่ยนสถานะ order หรือใช้ไฟล์ Excel จาก Seller Center แทน'
                      : 'ตรวจว่าไฟล์ export จาก Shopee Seller Center ถูกต้อง แล้วเลือกไฟล์ใหม่อีกครั้ง'}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setStep('idle')
                    setPreview(null)
                    if (previewSource === 'api') setAPIHelperOpen(true)
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

          {(preview.preflight?.realtime_managed_orders ?? 0) > 0 && (
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>มี order ที่อยู่ในคำสั่งซื้อ Shopee แล้ว</AlertTitle>
              <AlertDescription>
                {preview.preflight.realtime_managed_orders} order จะไม่ถูกสร้างจากเมนูนำเข้าย้อนหลังเพื่อกันบิลซ้ำ ให้เปิดจากเมนูคำสั่งซื้อ Shopee แทน
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
                  {selectedIDs.size > 0 && selectedIDs.size === preview.orders.filter((o) => !isOrderBlocked(o)).length
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
                    if (previewSource === 'api') setAPIHelperOpen(true)
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
                            preview.orders.filter((o) => !isOrderBlocked(o)).length > 0 &&
                            selectedIDs.size ===
                            preview.orders.filter((o) => !isOrderBlocked(o)).length
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
                      <TableHead className="text-right">ยอดรวม</TableHead>
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
                          isOrderBlocked(order) && 'bg-muted/30 text-muted-foreground',
                        )}
                      >
                        <TableCell>
                          <Checkbox
                            checked={selectedIDs.has(order.order_id)}
                            disabled={isOrderBlocked(order)}
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
                        <TableCell className="min-w-[180px] text-right tabular-nums">
                          <div className="font-medium">{order.paid_amount != null ? `฿${fmt(order.paid_amount)}` : '—'}</div>
                          <div className="mt-0.5 whitespace-nowrap text-[11px] text-muted-foreground">
                            เต็ม {fmt(order.item_gross_amount ?? 0)} · ลด {fmt(order.discount_amount ?? 0)} · ส่ง {fmt(order.shipping_amount ?? 0)}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                          {order.blocked_reason === 'realtime_managed' ? (
                            <Badge variant="secondary" className="bg-info/10 text-info hover:bg-info/15">
                              อยู่ในคำสั่งซื้อ Shopee
                            </Badge>
                          ) : order.duplicate ? (
                            <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/20">
                              มีในระบบแล้ว
                            </Badge>
                          ) : null}
                          {order.blocked_reason === 'realtime_managed' && order.action_url && (
                            <Button asChild variant="link" className="h-auto px-0 py-0 text-xs text-link">
                              <Link to={order.action_url}>เปิดในคำสั่งซื้อ Shopee</Link>
                            </Button>
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
                            <Badge variant="outline" className="border-warning/40 text-warning" title={order.amount_review_reason}>
                              ต้องยืนยันยอด
                            </Badge>
                          )}
                          {order.blocked_reason && order.blocked_reason !== 'realtime_managed' && (
                            <Badge variant="destructive" title={order.blocked_reason}>นำเข้าไม่ได้</Badge>
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
                                    <TableHead className="text-right text-[10px] uppercase">ยอดเต็ม</TableHead>
                                    <TableHead className="text-right text-[10px] uppercase">ส่วนลด</TableHead>
                                    <TableHead className="text-right text-[10px] uppercase">ยอดสุทธิ</TableHead>
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
                                      <TableCell className="text-right tabular-nums">{fmt(item.gross_amount ?? item.price * item.qty)}</TableCell>
                                      <TableCell className="text-right tabular-nums text-success">{item.discount_amount ? `-${fmt(item.discount_amount)}` : '0.00'}</TableCell>
                                      <TableCell className="text-right tabular-nums font-medium">{fmt((item.gross_amount ?? item.price * item.qty) - (item.discount_amount ?? 0))}</TableCell>
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
                if (previewSource === 'api') setAPIHelperOpen(true)
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
                          ) : r.blocked_reason === 'realtime_managed' && r.action_url ? (
                            <Link
                              to={r.action_url}
                              className="inline-flex items-center gap-1 font-medium text-info hover:underline"
                            >
                              เปิดคำสั่งซื้อ Shopee
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
    </div>
  )
}
