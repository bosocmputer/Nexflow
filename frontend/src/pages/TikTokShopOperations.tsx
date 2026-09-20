import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Eye,
  FilePlus2,
  Info,
  Loader2,
  RadioTower,
  RefreshCw,
  RotateCcw,
  Search,
} from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import {
  TikTokBillShadowButton,
  TikTokBillShadowDialog,
  type TikTokBillShadowItem,
  type TikTokBillShadowPreview,
} from '@/components/tiktok/TikTokBillShadowDialog'
import { TikTokProductMappingDialog } from '@/components/tiktok/TikTokProductMappingDialog'
import { TikTokCancellationDialog, type TikTokCancellationPreview } from '@/components/tiktok/TikTokCancellationDialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  buildTikTokReviewedBillRequest,
  buildTikTokCancellationRequest,
  canCreateTikTokReviewedBill,
  formatTikTokMoney,
  normalizeTikTokStatusGroup,
  tiktokCancellationState,
  tiktokOrderStatusLabel,
  tiktokDocumentState,
  tiktokRowActions,
  tiktokStatusGroupCount,
  tiktokSyncState,
  type TikTokStatusCounts,
  type TikTokStatusGroup,
} from '@/lib/tiktok-shop-operations'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/store/auth'

interface TikTokOrderRow {
  shop_id: string
  shop_name: string
  order_id: string
  order_status: string
  currency: string
  payment_total_amount: string
  product_subtotal_amount: string
  shipping_fee_amount: string
  item_insurance_fee_amount: string
  item_count: number
  sku_count: number
  last_order_update_at?: string
  last_synced_at: string
  bill_id?: string
  bill_status?: string
  sml_doc_no?: string
  document_path?: string
  auto_sml?: {
    status: string
    error_code?: string
    error_message?: string
    updated_at: string
  }
  cancellation?: {
    status: string
    cancel_sml_doc_no?: string
    error_code?: string
    error_message?: string
    stock_recalc_status?: string
    stock_recalc_error?: string
    updated_at: string
  }
}

interface TikTokOrderPage {
  data: TikTokOrderRow[]
  page: number
  page_size: number
  total_items: number
  total_pages: number
  status_counts: TikTokStatusCounts
}

interface TikTokOrderSyncSetting {
  shop_id: string
  shop_name: string
  enabled: boolean
  interval_seconds: number
  last_success_at?: string
  last_error_code?: string
  last_error_message?: string
}

interface TikTokOrderSyncResponse {
  worker_enabled: boolean
  data: TikTokOrderSyncSetting[]
}

interface TikTokDiagnostics {
  overall: 'ready_for_controlled_enablement' | 'needs_attention'
  api: { enabled: boolean; gateway_configured: boolean; connected_shops: number }
  sync: { worker_enabled: boolean; configured: boolean; enabled_shops: number; error_shops: number }
  webhook: { enabled: boolean }
  document: { reviewed_bill_enabled: boolean; sml_send_enabled: boolean; route_ready: boolean }
  cancellation?: { document_create_enabled: boolean; webhook_enabled: boolean }
  coverage: {
    sampled_orders: number
    ready_orders: number
    blocked_orders: number
    mapped_items: number
    total_items: number
    blockers: Record<string, number>
  }
  auto_sml: { global_enabled: boolean; can_enable: boolean; message: string }
  issues: string[]
  checked_at: string
}

interface TikTokAutoSMLSetting {
  shop_id: string
  shop_name: string
  enabled: boolean
  trigger_status: 'AWAITING_COLLECTION'
  config_version: number
  eligible_after?: string
  paused_reason?: string
  queued_count: number
  needs_review_count: number
  failed_count: number
}

interface TikTokAutoSMLSettingsResponse {
  global_enabled: boolean
  trigger_status: 'AWAITING_COLLECTION'
  historical_backfill: false
  settings: TikTokAutoSMLSetting[]
}

interface TikTokReviewedBillResponse {
  data: {
    bill_id: string
    status: string
    reused: boolean
    document_route: string
    review_path: string
    message: string
  }
  sml_created: false
  notification_created: false
}

const ALL = 'all'
const DEFAULT_PER_PAGE = 20
const PAGE_SIZE_OPTIONS = [20, 50] as const
const STATUSES = [
  'UNPAID',
  'ON_HOLD',
  'AWAITING_SHIPMENT',
  'PARTIALLY_SHIPPING',
  'AWAITING_COLLECTION',
  'IN_TRANSIT',
  'DELIVERED',
  'COMPLETED',
  'CANCELLED',
]
const STATUS_GROUP_TABS: Array<{ value: TikTokStatusGroup; label: string }> = [
  { value: 'all', label: 'ทั้งหมด' },
  { value: 'unpaid', label: 'ยังไม่ชำระ' },
  { value: 'to_ship', label: 'ที่ต้องจัดส่ง' },
  { value: 'shipping', label: 'กำลังจัดส่ง' },
  { value: 'completed', label: 'สำเร็จ' },
  { value: 'cancelled', label: 'ยกเลิก' },
]
const EMPTY_COUNTS: TikTokStatusCounts = {
  total: 0,
  unpaid: 0,
  to_ship: 0,
  shipping: 0,
  completed: 0,
  cancelled: 0,
}
const TIKTOK_ORDER_STATUS_HELP = [
  { value: 'รอจัดส่ง', detail: 'TikTok Shop รับออเดอร์แล้ว แต่ยังไม่พร้อมสร้างเอกสารขายใน Nexflow' },
  { value: 'รอรับพัสดุ', detail: 'เตรียมการจัดส่งแล้ว สามารถตรวจข้อมูลและสร้างเอกสารได้' },
  { value: 'กำลังขนส่ง', detail: 'ขนส่งรับพัสดุแล้ว และยังสร้างเอกสารแบบตรวจทีละใบได้' },
  { value: 'สำเร็จ', detail: 'ออเดอร์เสร็จสมบูรณ์แล้ว และยังตรวจหรือสร้างเอกสารย้อนหลังได้' },
  { value: 'ยกเลิก', detail: 'ไม่สร้างเอกสารขายใหม่จากออเดอร์นี้' },
] as const
const TIKTOK_DOCUMENT_STATUS_HELP = [
  { value: 'รอสร้างเอกสาร', detail: 'ยังไม่มีเอกสารขายใน Nexflow กดสร้างเอกสารเพื่อตรวจข้อมูลก่อนยืนยัน' },
  { value: 'สร้างเอกสารแล้ว', detail: 'มีเอกสารใน Nexflow แล้ว แต่ยังไม่ได้ส่งเข้า SML' },
  { value: 'ส่ง SML แล้ว', detail: 'เอกสารถูกบันทึกเข้า SML สำเร็จ' },
  { value: 'ส่ง SML ไม่สำเร็จ', detail: 'เปิดเอกสารเพื่อตรวจสาเหตุและลองส่งใหม่' },
] as const
const TIKTOK_CANCELLATION_STATUS_HELP = [
  { value: 'ไม่ต้องสร้าง', detail: 'ออเดอร์ไม่มีใบขายใน Nexflow หรือใบขายยังไม่เคยส่งเข้า SML' },
  { value: 'รอตรวจเอกสารยกเลิก', detail: 'พบใบขายเดิมใน SML ต้องตรวจหลักฐานก่อนเปิดการสร้างเอกสารยกเลิกแบบ canary' },
  { value: 'ต้องตรวจเอกสารเดิม', detail: 'สถานะเอกสารไม่ครบหรือขัดกัน ระบบจึงหยุดไว้ก่อนเพื่อป้องกันเอกสารซ้ำ' },
] as const
const TIKTOK_PAYMENT_DETAIL_HELP = [
  { value: 'สินค้า', detail: 'ยอดสินค้าที่ใช้สร้างเอกสารขาย' },
  { value: 'จัดส่ง', detail: 'ค่าจัดส่งที่รองรับและนำเข้าเอกสารตามเส้นทาง SML' },
  { value: 'คุ้มครอง', detail: 'ค่าใช้จ่ายฝั่งผู้ซื้อหรือแพลตฟอร์ม ใช้ตรวจยอดแต่ไม่นำเข้าเอกสารขาย' },
] as const

export default function TikTokShopOperations() {
  const navigate = useNavigate()
  const userRole = useAuthStore((state) => state.user?.role)
  const canManageMapping = userRole === 'admin'
  const canCreateDocument = canCreateTikTokReviewedBill(userRole)
  const [params, setParams] = useSearchParams()
  const [orders, setOrders] = useState<TikTokOrderPage | null>(null)
  const [sync, setSync] = useState<TikTokOrderSyncResponse | null>(null)
  const [diagnostics, setDiagnostics] = useState<TikTokDiagnostics | null>(null)
  const [autoSML, setAutoSML] = useState<TikTokAutoSMLSettingsResponse | null>(null)
  const [diagnosticsOpen, setDiagnosticsOpen] = useState(false)
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false)
  const [autoSMLSaving, setAutoSMLSaving] = useState(false)
  const [autoSMLConfirmSetting, setAutoSMLConfirmSetting] = useState<TikTokAutoSMLSetting | null>(null)
  const [autoSMLRetryingOrder, setAutoSMLRetryingOrder] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshTick, setRefreshTick] = useState(0)
  const [previewOrder, setPreviewOrder] = useState<TikTokOrderRow | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [billPreview, setBillPreview] = useState<TikTokBillShadowPreview | null>(null)
  const [billPreviewLoading, setBillPreviewLoading] = useState(false)
  const [billPreviewError, setBillPreviewError] = useState('')
  const [billCreating, setBillCreating] = useState(false)
  const [billCreateError, setBillCreateError] = useState('')
  const [mappingItem, setMappingItem] = useState<TikTokBillShadowItem | null>(null)
  const [mappingOpen, setMappingOpen] = useState(false)
  const [cancellationOrder, setCancellationOrder] = useState<TikTokOrderRow | null>(null)
  const [cancellationOpen, setCancellationOpen] = useState(false)
  const [cancellationPreview, setCancellationPreview] = useState<TikTokCancellationPreview | null>(null)
  const [cancellationLoading, setCancellationLoading] = useState(false)
  const [cancellationCreating, setCancellationCreating] = useState(false)
  const [cancellationConfirmed, setCancellationConfirmed] = useState(false)
  const [cancellationError, setCancellationError] = useState('')
  const page = readPage(params)
  const perPage = readPerPage(params)
  const statusGroup = normalizeTikTokStatusGroup(params.get('status_group'))
  const cancellationQueue = statusGroup === 'cancelled'
  const shopID = params.get('shop_id') ?? ALL
  const status = params.get('status') ?? ALL
  const orderID = params.get('order_id') ?? ''
  const reviewOrderID = params.get('review_order_id') ?? ''
  const [pageJumpInput, setPageJumpInput] = useState(String(page))
  const total = orders?.total_items ?? 0
  const totalPages = Math.max(1, orders?.total_pages ?? 1)
  const pageStart = total === 0 ? 0 : (page - 1) * perPage + 1
  const pageEnd = total === 0 ? 0 : Math.min(page * perPage, total)

  const setQuery = useCallback((next: Record<string, string | number | null>) => {
    setParams((current) => {
      const updated = new URLSearchParams(current)
      Object.entries(next).forEach(([key, value]) => value === null || value === '' || value === ALL ? updated.delete(key) : updated.set(key, String(value)))
      return updated
    }, { replace: true })
  }, [setParams])

  useEffect(() => setPageJumpInput(String(page)), [page])

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    Promise.all([
      client.get<TikTokOrderPage>('/api/tiktok-shop-api/orders', {
        params: {
          shop_id: shopID === ALL ? undefined : shopID,
          status: status === ALL ? undefined : status,
          status_group: statusGroup === ALL ? undefined : statusGroup,
          order_id: orderID || undefined,
          page,
          page_size: perPage,
        },
      }),
      client.get<TikTokOrderSyncResponse>('/api/tiktok-shop-api/order-sync-settings'),
    ]).then(([orderResponse, syncResponse]) => {
      if (!active) return
      setOrders(orderResponse.data)
      setSync(syncResponse.data)
    }).catch((cause: unknown) => {
      if (!active) return
      setOrders(null)
      setError(apiErrorMessage(cause, 'โหลดคำสั่งซื้อ TikTok Shop ไม่สำเร็จ'))
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [orderID, page, perPage, refreshTick, shopID, status, statusGroup])

  useEffect(() => {
    if (!reviewOrderID || loading || previewOpen) return
    const row = orders?.data.find((item) => item.order_id === reviewOrderID)
    if (!row) return
    setPreviewOrder(row)
    setBillCreateError('')
    setPreviewOpen(true)
    setQuery({ review_order_id: null })
  }, [loading, orders?.data, previewOpen, reviewOrderID, setQuery])

  useEffect(() => {
    if (!previewOpen || !previewOrder) return
    let active = true
    setBillPreview(null)
    setBillPreviewError('')
    setBillPreviewLoading(true)
    client.get<TikTokBillShadowPreview>(
      `/api/tiktok-shop-api/orders/${encodeURIComponent(previewOrder.shop_id)}/${encodeURIComponent(previewOrder.order_id)}/bill-shadow-preview`,
    ).then((response) => {
      if (active) setBillPreview(response.data)
    }).catch((cause: unknown) => {
      if (active) setBillPreviewError(apiErrorMessage(cause, 'ตรวจข้อมูลเอกสาร TikTok Shop ไม่สำเร็จ'))
    }).finally(() => {
      if (active) setBillPreviewLoading(false)
    })
    return () => { active = false }
  }, [previewOpen, previewOrder])

  const selectedSetting = useMemo(() => {
    const settings = sync?.data ?? []
    return shopID !== ALL ? settings.find((item) => item.shop_id === shopID) : settings[0]
  }, [shopID, sync?.data])
  const syncState = selectedSetting ? tiktokSyncState(Boolean(sync?.worker_enabled), selectedSetting.enabled, selectedSetting.last_error_code) : 'shop_disabled'

  const loadDiagnostics = useCallback(async () => {
    setDiagnosticsLoading(true)
    try {
      const [diagnosticResponse, autoSMLResponse] = await Promise.all([
        client.get<TikTokDiagnostics>('/api/tiktok-shop-api/diagnostics', {
          params: { shop_id: shopID === ALL ? undefined : shopID },
        }),
        client.get<TikTokAutoSMLSettingsResponse>('/api/tiktok-shop-api/auto-sml/settings'),
      ])
      setDiagnostics(diagnosticResponse.data)
      setAutoSML(autoSMLResponse.data)
    } catch (cause: unknown) {
      toast.error(apiErrorMessage(cause, 'ตรวจสอบระบบ TikTok Shop ไม่สำเร็จ'))
    } finally {
      setDiagnosticsLoading(false)
    }
  }, [shopID])

  const updateAutoSML = useCallback(async (setting: TikTokAutoSMLSetting, enabled: boolean) => {
    setAutoSMLSaving(true)
    try {
      await client.put(`/api/tiktok-shop-api/auto-sml/settings/${encodeURIComponent(setting.shop_id)}`, {
        enabled,
        expected_config_version: setting.config_version,
        confirm: enabled ? 'ENABLE_TIKTOK_AUTO_SML' : '',
      })
      toast.success(enabled ? 'เปิด Auto SML สำหรับออเดอร์ใหม่แล้ว' : 'ปิด Auto SML แล้ว')
      await loadDiagnostics()
    } catch (cause: unknown) {
      toast.error(apiErrorMessage(cause, 'บันทึก Auto SML ไม่สำเร็จ'))
    } finally {
      setAutoSMLSaving(false)
    }
  }, [loadDiagnostics])

  const requestAutoSMLUpdate = useCallback(async (setting: TikTokAutoSMLSetting, enabled: boolean) => {
    if (enabled) {
      setAutoSMLConfirmSetting(setting)
      return
    }
    await updateAutoSML(setting, false)
  }, [updateAutoSML])

  const retryAutoSML = useCallback(async (row: TikTokOrderRow) => {
    setAutoSMLRetryingOrder(row.order_id)
    try {
      await client.post(`/api/tiktok-shop-api/orders/${encodeURIComponent(row.shop_id)}/${encodeURIComponent(row.order_id)}/auto-sml/retry`)
      toast.success('นำออเดอร์กลับเข้าคิว Auto SML แล้ว')
      setRefreshTick((value) => value + 1)
      if (diagnosticsOpen) await loadDiagnostics()
    } catch (cause: unknown) {
      toast.error(apiErrorMessage(cause, 'นำงาน Auto SML กลับเข้าคิวไม่สำเร็จ'))
    } finally {
      setAutoSMLRetryingOrder('')
    }
  }, [diagnosticsOpen, loadDiagnostics])

  const setStatusGroup = (value: string) => setQuery({ status_group: normalizeTikTokStatusGroup(value), status: null, page: null })
  const setPage = (nextPage: number) => setQuery({ page: nextPage <= 1 ? null : nextPage })
  const handleSearch = (value: string) => {
    if (!/^\d{0,32}$/.test(value)) return
    setQuery({ order_id: value || null, page: null })
  }
  const handlePerPageChange = (value: string) => setQuery({ per_page: Number(value) === DEFAULT_PER_PAGE ? null : value, page: null })
  const handlePageJump = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const next = Number(pageJumpInput)
    if (!Number.isInteger(next) || next < 1) {
      setPageJumpInput(String(page))
      return
    }
    setPage(Math.min(next, totalPages))
  }
  const openBillPreview = (row: TikTokOrderRow) => {
    setPreviewOrder(row)
    setBillCreateError('')
    setPreviewOpen(true)
  }
  const setBillPreviewOpen = (open: boolean) => {
    setPreviewOpen(open)
    if (!open) {
      setBillPreviewLoading(false)
      setBillCreateError('')
    }
  }
  const createReviewedBill = async () => {
    if (!previewOrder || !billPreview?.can_create_bill) return
    setBillCreating(true)
    setBillCreateError('')
    try {
      const response = await client.post<TikTokReviewedBillResponse>(
        `/api/tiktok-shop-api/orders/${encodeURIComponent(previewOrder.shop_id)}/${encodeURIComponent(previewOrder.order_id)}/reviewed-bill`,
        buildTikTokReviewedBillRequest(billPreview.review_digest),
      )
      const result = response.data.data
      toast.success(result.reused ? 'พบเอกสารที่สร้างจาก Order นี้แล้ว' : 'สร้างเอกสาร TikTok Shop ใน Nexflow แล้ว', {
        description: 'ยังไม่ได้ส่งเข้า SML, แจ้ง LINE หรือเขียนสต๊อก',
      })
      setPreviewOpen(false)
      setRefreshTick((value) => value + 1)
      navigate(result.review_path)
    } catch (cause: unknown) {
      setBillCreateError(apiErrorMessage(cause, 'สร้างเอกสาร TikTok Shop ไม่สำเร็จ กรุณาเปิดรายละเอียดใหม่แล้วตรวจอีกครั้ง'))
    } finally {
      setBillCreating(false)
    }
  }
  const openCancellationPreview = async (row: TikTokOrderRow) => {
    setCancellationOrder(row)
    setCancellationOpen(true)
    setCancellationPreview(null)
    setCancellationConfirmed(false)
    setCancellationError('')
    setCancellationLoading(true)
    try {
      const response = await client.post<TikTokCancellationPreview>(
        `/api/tiktok-shop-api/orders/${encodeURIComponent(row.shop_id)}/${encodeURIComponent(row.order_id)}/cancellation/preview`,
      )
      setCancellationPreview(response.data)
    } catch (cause: unknown) {
      setCancellationError(apiErrorMessage(cause, 'ตรวจ Preview เอกสารยกเลิก TikTok Shop ไม่สำเร็จ'))
    } finally {
      setCancellationLoading(false)
    }
  }
  const createCancellation = async () => {
    if (!cancellationOrder || !cancellationPreview?.create_enabled || !cancellationConfirmed) return
    setCancellationCreating(true)
    setCancellationError('')
    try {
      await client.post(
        `/api/tiktok-shop-api/orders/${encodeURIComponent(cancellationOrder.shop_id)}/${encodeURIComponent(cancellationOrder.order_id)}/cancellation`,
        buildTikTokCancellationRequest(cancellationPreview.review_digest),
      )
      toast.success('สร้างเอกสารยกเลิก TikTok Shop ใน SML แล้ว', {
        description: 'ระบบจัดคิวคำนวณสต๊อก SML ต่อโดยไม่ส่งเอกสารซ้ำ',
      })
      setCancellationOpen(false)
      setCancellationConfirmed(false)
      setRefreshTick((value) => value + 1)
    } catch (cause: unknown) {
      setCancellationError(apiErrorMessage(cause, 'สร้างเอกสารยกเลิก TikTok Shop ไม่สำเร็จ'))
    } finally {
      setCancellationCreating(false)
    }
  }
  const openProductMapping = (item: TikTokBillShadowItem) => {
    setPreviewOpen(false)
    setBillPreviewLoading(false)
    setMappingItem(item)
    setMappingOpen(true)
  }
  const setProductMappingOpen = (open: boolean) => {
    setMappingOpen(open)
    if (!open) {
      setMappingItem(null)
      if (previewOrder) setPreviewOpen(true)
    }
  }

  return (
    <div className="space-y-4">
      <section className="rounded-lg border border-border bg-card px-3 py-2" aria-labelledby="tiktok-operations-title">
        <div className="flex flex-col gap-2 xl:flex-row xl:items-center xl:justify-between">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <h1 id="tiktok-operations-title" className="text-lg font-semibold tracking-normal">
                {cancellationQueue ? 'เอกสารยกเลิก TikTok Shop' : 'คำสั่งซื้อ TikTok Shop'}
              </h1>
              <Badge className="h-6 border-foreground bg-foreground px-2 text-[11px] text-background hover:bg-foreground">
                {cancellationQueue ? 'Cancellation Review' : 'Snapshot'}
              </Badge>
              <span className="inline-flex h-6 items-center rounded-full border border-border bg-background px-2 text-xs text-muted-foreground">
                {cancellationQueue ? 'ตรวจหลักฐานใบขายเดิม · ไม่สร้างเอกสารซ้ำ' : 'ตรวจทานก่อนสร้าง · ส่ง SML ทีละใบ'}
              </span>
            </div>
            <p className="max-w-3xl text-xs leading-5 text-muted-foreground">
              {cancellationQueue
                ? 'แสดงออเดอร์ที่ TikTok ยืนยันว่ายกเลิกแล้ว พร้อมตรวจว่าใบขายเดิมเคยส่งเข้า SML หรือไม่'
                : 'ติดตาม order จาก TikTok Shop ตรวจข้อมูลก่อนสร้างเอกสารใน Nexflow แล้วเปิดเอกสารเพื่อส่ง SML ทีละใบ'}
            </p>
            <TikTokOperationsHealthLine state={syncState} setting={selectedSetting} diagnostics={diagnostics} />
          </div>
          <div className="flex flex-col gap-2 sm:flex-row xl:shrink-0">
            <Select value={shopID} onValueChange={(value) => setQuery({ shop_id: value, page: null })}>
              <SelectTrigger className="h-8 min-w-[160px] bg-background">
                <SelectValue placeholder="ร้าน TikTok Shop" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>ทุกร้าน</SelectItem>
                {(sync?.data ?? []).map((item) => (
                  <SelectItem key={item.shop_id} value={item.shop_id}>{item.shop_name || item.shop_id}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="button" size="sm" className="h-8 gap-2" disabled={loading} onClick={() => setRefreshTick((value) => value + 1)}>
              <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
              รีเฟรชรายการ
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 gap-2 bg-background"
              disabled={diagnosticsLoading}
              onClick={() => {
                setDiagnosticsOpen((value) => !value)
                if (!diagnosticsOpen) void loadDiagnostics()
              }}
            >
              {diagnosticsLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RadioTower className="h-4 w-4" />}
              ตรวจสอบระบบ
            </Button>
          </div>
        </div>
      </section>

      {diagnosticsOpen && (
        <TikTokDiagnosticsPanel
          diagnostics={diagnostics}
          autoSML={autoSML}
          selectedShopID={shopID}
          loading={diagnosticsLoading}
          saving={autoSMLSaving}
          onRefresh={loadDiagnostics}
          onUpdateAutoSML={requestAutoSMLUpdate}
        />
      )}

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>โหลดข้อมูลไม่สำเร็จ</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {cancellationQueue && (
        <Alert>
          <Info className="h-4 w-4" />
          <AlertTitle>คิวยกเลิก TikTok แยกจากงานคืนสินค้า/คืนเงิน</AlertTitle>
          <AlertDescription>
            รายการที่ไม่มีใบขาย SML ไม่ต้องออกเอกสารยกเลิก ส่วนรายการที่เคยส่ง SML จะถูกพักไว้เพื่อตรวจทานก่อนเปิดการสร้างเอกสารยกเลิกแบบ canary
          </AlertDescription>
        </Alert>
      )}

      <div className="rounded-lg border border-border bg-card px-3 pt-2">
        <Tabs value={statusGroup} onValueChange={setStatusGroup}>
          <TabsList className="h-auto w-full justify-start overflow-x-auto rounded-none border-b border-border bg-transparent p-0">
            {STATUS_GROUP_TABS.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value} className="h-10 shrink-0 rounded-none border-b-2 border-transparent bg-transparent px-3 text-sm data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none">
                <span>{tab.label}</span>
                <Badge variant="outline" className="ml-2 h-5 bg-background px-1.5 text-[10px]">
                  {tiktokStatusGroupCount(orders?.status_counts ?? EMPTY_COUNTS, tab.value).toLocaleString()}
                </Badge>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <div className="rounded-lg border border-border bg-card p-3">
        <div className="grid gap-2 lg:grid-cols-[minmax(260px,1fr)_220px_auto] lg:items-center">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={orderID} onChange={(event) => handleSearch(event.target.value)} placeholder="ค้นหา Order ID" inputMode="numeric" className="h-9 pl-8" aria-label="ค้นหา Order ID" />
          </div>
          <Select value={status} onValueChange={(value) => setQuery({ status: value, status_group: null, page: null })}>
            <SelectTrigger className="h-9">
              <SelectValue placeholder="ทุกสถานะ TikTok Shop" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>ทุกสถานะ TikTok Shop</SelectItem>
              {STATUSES.map((item) => <SelectItem key={item} value={item}>{tiktokOrderStatusLabel(item)}</SelectItem>)}
            </SelectContent>
          </Select>
          {(statusGroup !== ALL || status !== ALL || orderID) && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9"
              onClick={() => setParams(shopID === ALL ? {} : { shop_id: shopID }, { replace: true })}
            >
              ล้างตัวกรอง
            </Button>
          )}
        </div>
      </div>

      <div className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[960px] text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left">คำสั่งซื้อ / ร้าน</th>
                <th className="px-3 py-2 text-right">ยอดเงิน</th>
                <th className="px-3 py-2 text-left">
                  <StatusColumnHelp
                    label="สถานะ TikTok Shop"
                    title="ความหมายสถานะคำสั่งซื้อ TikTok Shop"
                    description="สถานะการชำระเงินและจัดส่งล่าสุดที่ Nexflow อ่านจาก TikTok Shop"
                    items={TIKTOK_ORDER_STATUS_HELP}
                  />
                </th>
                <th className="px-3 py-2 text-left">
                  <StatusColumnHelp
                    label={cancellationQueue ? 'ใบขายเดิม / หลังยกเลิก' : 'เอกสาร Nexflow / SML'}
                    title={cancellationQueue ? 'หลักฐานเอกสารก่อนยกเลิก' : 'ความหมายสถานะเอกสาร'}
                    description={cancellationQueue
                      ? 'ระบบจะสร้างเอกสารยกเลิกได้เฉพาะเมื่อพบใบขายเดิมที่ส่งเข้า SML สำเร็จแล้ว'
                      : 'เป็นสถานะการสร้างเอกสารใน Nexflow และการบันทึกเข้า SML ไม่ใช่สถานะจัดส่งของ TikTok Shop'}
                    items={cancellationQueue ? TIKTOK_CANCELLATION_STATUS_HELP : TIKTOK_DOCUMENT_STATUS_HELP}
                  />
                </th>
                <th className="px-3 py-2 text-left">
                  <StatusColumnHelp
                    label="รายละเอียดยอด"
                    title="ยอดที่ใช้สร้างเอกสาร"
                    description="แยกยอดสินค้า ค่าจัดส่ง และค่าใช้จ่ายฝั่งผู้ซื้อ เพื่อไม่บันทึกยอดที่ไม่ใช่รายได้เข้า SML"
                    items={TIKTOK_PAYMENT_DETAIL_HELP}
                  />
                </th>
                <th className="px-3 py-2 text-right">จัดการ</th>
              </tr>
            </thead>
            <tbody>
              {loading && (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-muted-foreground">
                    <Loader2 className="mr-2 inline h-4 w-4 animate-spin" />
                    กำลังโหลด...
                  </td>
                </tr>
              )}
              {!loading && (orders?.data ?? []).length === 0 && <EmptyRow cancellationQueue={cancellationQueue} />}
              {!loading && orders?.data.map((row) => (
                <DesktopRow
                  key={`${row.shop_id}:${row.order_id}`}
                  row={row}
                  previewLoading={billPreviewLoading && previewOrder?.shop_id === row.shop_id && previewOrder?.order_id === row.order_id}
                  onPreview={() => openBillPreview(row)}
                  retryingAutoSML={autoSMLRetryingOrder === row.order_id}
                  canRetryAutoSML={Boolean(canManageMapping && autoSML?.global_enabled)}
                  onRetryAutoSML={() => void retryAutoSML(row)}
                  cancellationQueue={cancellationQueue}
                  cancellationLoading={cancellationLoading && cancellationOrder?.shop_id === row.shop_id && cancellationOrder?.order_id === row.order_id}
                  onReviewCancellation={() => void openCancellationPreview(row)}
                />
              ))}
            </tbody>
          </table>
        </div>
        <div className="flex flex-col gap-2 border-t border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground lg:flex-row lg:items-center lg:justify-between">
          <span>{total > 0 ? `แสดง ${pageStart.toLocaleString()}-${pageEnd.toLocaleString()} จาก ${total.toLocaleString()} order` : 'แสดง 0 order'}</span>
          <div className="flex flex-wrap items-center gap-2 lg:justify-end">
            <label className="inline-flex items-center gap-1.5">
              <span>ต่อหน้า</span>
              <Select value={String(perPage)} onValueChange={handlePerPageChange}>
                <SelectTrigger className="h-8 w-[82px] text-xs" aria-label="จำนวน order ต่อหน้า"><SelectValue /></SelectTrigger>
                <SelectContent>{PAGE_SIZE_OPTIONS.map((size) => <SelectItem key={size} value={String(size)}>{size}</SelectItem>)}</SelectContent>
              </Select>
            </label>
            <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage(1)}>หน้าแรก</Button>
            <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}><ChevronLeft className="h-3.5 w-3.5" />ก่อนหน้า</Button>
            <span className="min-w-[92px] text-center tabular-nums">หน้า {page.toLocaleString()} / {totalPages.toLocaleString()}</span>
            <form className="inline-flex items-center gap-1.5" onSubmit={handlePageJump}>
              <span>ไปหน้า</span>
              <Input type="number" min={1} max={totalPages} value={pageJumpInput} onChange={(event) => setPageJumpInput(event.target.value)} className="h-8 w-20 px-2 text-center text-xs tabular-nums" aria-label="ไปหน้าที่" />
              <Button type="submit" variant="outline" size="sm" disabled={totalPages <= 1 || loading}>ไป</Button>
            </form>
            <Button variant="outline" size="sm" disabled={page >= totalPages || loading} onClick={() => setPage(page + 1)}>ถัดไป<ChevronRight className="h-3.5 w-3.5" /></Button>
          </div>
        </div>
      </div>

      <TikTokCancellationDialog
        open={cancellationOpen}
        loading={cancellationLoading}
        error={cancellationError}
        preview={cancellationPreview}
        confirmed={cancellationConfirmed}
        creating={cancellationCreating}
        onOpenChange={(open) => {
          setCancellationOpen(open)
          if (!open) {
            setCancellationConfirmed(false)
            setCancellationError('')
          }
        }}
        onConfirmedChange={setCancellationConfirmed}
        onCreate={() => void createCancellation()}
      />

      <TikTokBillShadowDialog
        open={previewOpen}
        orderID={previewOrder?.order_id ?? ''}
        shopName={previewOrder?.shop_name ?? ''}
        loading={billPreviewLoading}
        error={billPreviewError}
        preview={billPreview}
        canCreateDocument={canCreateDocument}
        canManageMapping={canManageMapping}
        creatingBill={billCreating}
        createError={billCreateError}
        onMapItem={openProductMapping}
        onCreateBill={createReviewedBill}
        onOpenChange={setBillPreviewOpen}
      />
      <TikTokProductMappingDialog
        open={mappingOpen}
        shopID={previewOrder?.shop_id ?? ''}
        shopName={previewOrder?.shop_name ?? ''}
        orderID={previewOrder?.order_id ?? ''}
        item={mappingItem}
        onOpenChange={setProductMappingOpen}
      />
      <ConfirmDialog
        open={Boolean(autoSMLConfirmSetting)}
        onOpenChange={(open) => {
          if (!open) setAutoSMLConfirmSetting(null)
        }}
        title="เปิดส่ง SML อัตโนมัติสำหรับร้านนี้?"
        description={`ร้าน ${autoSMLConfirmSetting?.shop_name || autoSMLConfirmSetting?.shop_id || 'TikTok Shop'} จะส่งเฉพาะออเดอร์ใหม่ที่เปลี่ยนเข้า AWAITING_COLLECTION หลังเวลาที่เปิดใช้งาน\n\nระบบจะไม่ประมวลผลออเดอร์ย้อนหลัง และจะหยุดอัตโนมัติเมื่อเส้นทาง SML เปลี่ยนหรือพบข้อผิดพลาดต่อเนื่อง`}
        confirmLabel="เปิด Auto SML"
        onConfirm={async () => {
          if (autoSMLConfirmSetting) await updateAutoSML(autoSMLConfirmSetting, true)
        }}
      />
    </div>
  )
}

function TikTokOperationsHealthLine({
  state,
  setting,
  diagnostics,
}: {
  state: ReturnType<typeof tiktokSyncState>
  setting?: TikTokOrderSyncSetting
  diagnostics: TikTokDiagnostics | null
}) {
  const Icon = state === 'active' ? CheckCircle2 : Clock3
  return (
    <div className={cn('flex flex-wrap items-center gap-x-2 gap-y-1 text-xs', state === 'active' ? 'text-accentStrong' : state === 'error' ? 'text-destructive' : 'text-warning')}>
      <span className="inline-flex items-center gap-1 font-medium">
        <Icon className="h-3.5 w-3.5" />
        {syncStateLabel(state)}
      </span>
      {setting && (
        <span className="text-muted-foreground">
          {setting.shop_name || setting.shop_id} · ทุก {formatInterval(setting.interval_seconds)} · สำเร็จล่าสุด {formatDateTime(setting.last_success_at)}
        </span>
      )}
      {setting?.last_error_message && <span className="text-destructive">{setting.last_error_message}</span>}
      {diagnostics && (
        <span className={diagnostics.overall === 'ready_for_controlled_enablement' ? 'text-accentStrong' : 'text-warning'}>
          · {diagnostics.overall === 'ready_for_controlled_enablement' ? 'พื้นฐานพร้อมสำหรับ canary' : `ต้องตรวจ ${diagnostics.issues.length} จุด`}
        </span>
      )}
    </div>
  )
}

function TikTokDiagnosticsPanel({
  diagnostics,
  autoSML,
  selectedShopID,
  loading,
  saving,
  onRefresh,
  onUpdateAutoSML,
}: {
  diagnostics: TikTokDiagnostics | null
  autoSML: TikTokAutoSMLSettingsResponse | null
  selectedShopID: string
  loading: boolean
  saving: boolean
  onRefresh: () => Promise<void>
  onUpdateAutoSML: (setting: TikTokAutoSMLSetting, enabled: boolean) => Promise<void>
}) {
  if (!diagnostics) {
    return (
      <div className="rounded-lg border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        <span className="inline-flex items-center gap-2"><Loader2 className="h-4 w-4 animate-spin" />กำลังตรวจสอบระบบ TikTok Shop...</span>
      </div>
    )
  }
  const checks = [
    { label: 'Open API / Gateway', ok: diagnostics.api.enabled && diagnostics.api.gateway_configured && diagnostics.api.connected_shops > 0 },
    { label: 'ซิงก์ออเดอร์', ok: diagnostics.sync.worker_enabled && diagnostics.sync.enabled_shops > 0 && diagnostics.sync.error_shops === 0 },
    { label: 'Webhook', ok: diagnostics.webhook.enabled },
    { label: 'เส้นทาง SML', ok: diagnostics.document.route_ready && diagnostics.document.sml_send_enabled },
    { label: 'Webhook ยกเลิก', ok: diagnostics.cancellation?.webhook_enabled === true },
    { label: 'เอกสารยกเลิก (Canary)', ok: diagnostics.cancellation?.document_create_enabled === true },
    {
      label: `Mapping ${diagnostics.coverage.mapped_items}/${diagnostics.coverage.total_items}`,
      ok: diagnostics.coverage.blocked_orders === 0 && diagnostics.coverage.sampled_orders > 0,
      href: `/marketplace-aliases?tab=pending&source=tiktok`,
    },
  ]
  const selectedAutoSML = selectedShopID === ALL ? undefined : autoSML?.settings.find((setting) => setting.shop_id === selectedShopID)
  const autoSMLRunning = Boolean(selectedAutoSML?.enabled && !selectedAutoSML.paused_reason)
  return (
    <section className="rounded-lg border border-border bg-card px-4 py-3" aria-label="ผลตรวจสอบระบบ TikTok Shop">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold">ความพร้อม TikTok Shop → Nexflow → SML</h2>
            <Badge variant="outline" className={diagnostics.overall === 'ready_for_controlled_enablement' ? 'border-accentStrong/40 bg-primary/10 text-accentStrong' : 'border-warning/40 bg-warning/10 text-warning'}>
              {diagnostics.overall === 'ready_for_controlled_enablement' ? 'พื้นฐานพร้อม' : 'ต้องตรวจสอบ'}
            </Badge>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">ตรวจจากร้านที่เชื่อมอยู่ งานซิงก์ route และตัวอย่างออเดอร์ล่าสุด โดยไม่อ่านข้อมูลผู้รับ</p>
        </div>
        <Button type="button" variant="outline" size="sm" className="h-8 gap-2" disabled={loading} onClick={() => void onRefresh()}>
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />รีเฟรชผลตรวจ
        </Button>
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        {checks.map((check) => {
          const className = cn('inline-flex min-h-7 items-center gap-1.5 rounded-full px-2.5 text-xs', check.ok ? 'bg-primary/10 text-accentStrong' : 'bg-warning/10 text-warning', check.href && 'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring')
          const content = <>{check.ok ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}{check.label}</>
          return check.href ? (
            <Link key={check.label} to={check.href} className={className} title="เปิดรายการสินค้าที่ต้องจับคู่">{content}</Link>
          ) : (
            <span key={check.label} className={className}>{content}</span>
          )
        })}
      </div>
      <div className="mt-3 rounded-md border border-border bg-muted/30 px-3 py-2 text-xs">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium">ส่ง SML อัตโนมัติ</div>
            <div className="mt-1 text-muted-foreground">{diagnostics.auto_sml.message}</div>
            <div className="mt-1 text-muted-foreground">Trigger: AWAITING_COLLECTION · เฉพาะออเดอร์ใหม่ · ไม่ย้อนหลัง</div>
            {selectedAutoSML?.eligible_after && <div className="mt-1 text-muted-foreground">Cutoff: {formatDateTime(selectedAutoSML.eligible_after)}</div>}
            {selectedAutoSML?.paused_reason && <div className="mt-1 text-warning">หยุดชั่วคราว: {selectedAutoSML.paused_reason === 'route_changed' ? 'เส้นทาง SML เปลี่ยน' : 'ระบบเชื่อมต่อล้มเหลวต่อเนื่อง'}</div>}
          </div>
          {selectedAutoSML ? (
            <Button
              type="button"
              size="sm"
              variant={autoSMLRunning ? 'outline' : 'default'}
              disabled={saving || (!autoSMLRunning && (!autoSML?.global_enabled || !diagnostics.auto_sml.can_enable))}
              onClick={() => void onUpdateAutoSML(selectedAutoSML, !autoSMLRunning)}
            >
              {saving && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {autoSMLRunning ? 'ปิด Auto SML' : selectedAutoSML.paused_reason ? 'ตรวจแล้วเปิดต่อ' : 'เปิด Auto SML'}
            </Button>
          ) : (
            <span className="text-muted-foreground">เลือกร้านเพื่อจัดการ</span>
          )}
        </div>
        {!diagnostics.auto_sml.global_enabled && <div className="mt-2 text-warning">ยังไม่มีการส่งอัตโนมัติ และจะไม่ประมวลผลออเดอร์ย้อนหลัง</div>}
      </div>
      {diagnostics.issues.length > 0 && (
        <div className="mt-3 text-xs text-muted-foreground">จุดที่ต้องตรวจ: {diagnostics.issues.map(tikTokDiagnosticIssueLabel).join(' · ')}</div>
      )}
    </section>
  )
}

function tikTokDiagnosticIssueLabel(code: string) {
  const labels: Record<string, string> = {
    open_api_disabled: 'Open API ยังปิด',
    gateway_not_configured: 'Gateway ยังไม่พร้อม',
    gateway_unavailable: 'ติดต่อ Gateway ไม่สำเร็จ',
    shop_not_connected: 'ไม่พบร้านที่เชื่อมต่อ',
    order_sync_not_configured: 'ยังไม่มีระบบซิงก์ออเดอร์',
    order_sync_unavailable: 'อ่านสถานะซิงก์ไม่สำเร็จ',
    order_sync_disabled: 'ซิงก์ออเดอร์ยังปิด',
    order_sync_error: 'ซิงก์ออเดอร์มีข้อผิดพลาด',
    review_pipeline_not_configured: 'ระบบตรวจบิลยังไม่พร้อม',
    order_evidence_unavailable: 'อ่านตัวอย่างออเดอร์ไม่ได้',
    no_unsent_order_sample: 'ยังไม่มีออเดอร์ที่ยังไม่สร้างบิลสำหรับตรวจ',
    order_review_blocked: 'มีออเดอร์ที่ mapping หรือยอดยังไม่พร้อม',
    sale_route_not_ready: 'เส้นทางขาย SML ยังไม่พร้อม',
    webhook_disabled: 'Webhook ยังปิด',
    sml_send_disabled: 'การส่ง SML ยังปิด',
  }
  return labels[code] ?? code
}

function DesktopRow({
  row,
  previewLoading,
  retryingAutoSML,
  canRetryAutoSML,
  onPreview,
  onRetryAutoSML,
  cancellationQueue,
  cancellationLoading,
  onReviewCancellation,
}: {
  row: TikTokOrderRow
  previewLoading: boolean
  retryingAutoSML: boolean
  canRetryAutoSML: boolean
  onPreview: () => void
  onRetryAutoSML: () => void
  cancellationQueue: boolean
  cancellationLoading: boolean
  onReviewCancellation: () => void
}) {
  const documentInput = {
    billID: row.bill_id,
    billStatus: row.bill_status,
    smlDocNo: row.sml_doc_no,
    documentPath: row.document_path,
    cancellation: row.cancellation ? {
      status: row.cancellation.status,
      cancelSMLDocNo: row.cancellation.cancel_sml_doc_no,
      errorCode: row.cancellation.error_code,
      errorMessage: row.cancellation.error_message,
      stockRecalcStatus: row.cancellation.stock_recalc_status,
      stockRecalcError: row.cancellation.stock_recalc_error,
    } : undefined,
  }
  const cancellationDocument = cancellationQueue ? tiktokCancellationState(documentInput) : null
  const document = cancellationDocument ?? tiktokDocumentState(documentInput)
  const actions = tiktokRowActions({ billID: row.bill_id, documentPath: row.document_path })
  return (
    <tr className="border-t border-border hover:bg-muted/30">
      <td className="px-3 py-2 align-top">
        <div className="font-mono text-xs font-medium text-foreground">{row.order_id}</div>
        <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
          <span>{row.shop_name || row.shop_id}</span>
          <span aria-hidden="true">·</span>
          <span>{formatDateTime(row.last_order_update_at)}</span>
        </div>
        <div className="mt-0.5 text-[11px] text-muted-foreground">ซิงก์ล่าสุด {formatDateTime(row.last_synced_at)}</div>
      </td>
      <td className="px-3 py-2 text-right align-top tabular-nums">
        <div className="font-medium">{formatTikTokMoney(row.payment_total_amount, row.currency)}</div>
        <div className="mt-0.5 text-xs text-muted-foreground">
          {row.item_count.toLocaleString()} รายการ · {row.sku_count.toLocaleString()} SKU
        </div>
      </td>
      <td className="px-3 py-2 align-top"><OrderStatusBadge status={row.order_status} /></td>
      <td className="px-3 py-2 align-top">
        <div className="flex max-w-[300px] flex-col items-start gap-1">
          <Badge variant="outline" className={documentBadgeClass(document.tone)}>{document.label}</Badge>
          <div className="text-[11px] text-muted-foreground">{document.detail}</div>
          {!cancellationQueue && row.auto_sml && <TikTokAutoSMLBadge state={row.auto_sml} retrying={retryingAutoSML} canRetry={canRetryAutoSML} onRetry={onRetryAutoSML} />}
        </div>
      </td>
      <td className="px-3 py-2 align-top text-xs text-muted-foreground">
        <div className="inline-flex items-center gap-1.5 font-medium text-accentStrong">
          <span className="h-1.5 w-1.5 rounded-full bg-info" aria-hidden="true" />
          ข้อมูลพร้อม
        </div>
        <div className="mt-1">สินค้า {formatTikTokMoney(row.product_subtotal_amount, row.currency)}</div>
        <div className="mt-0.5">
          จัดส่ง {formatTikTokMoney(row.shipping_fee_amount, row.currency)} · คุ้มครอง {formatTikTokMoney(row.item_insurance_fee_amount, row.currency)}
        </div>
      </td>
      <td className="px-3 py-2 align-top">
        <div className="flex flex-wrap justify-end gap-1.5">
          {cancellationDocument?.canReviewCancellation && (
            <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" disabled={cancellationLoading} onClick={onReviewCancellation}>
              {cancellationLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
              ตรวจเอกสารยกเลิก
            </Button>
          )}
          {cancellationQueue && document.path ? (
            <Button asChild variant="outline" size="sm" className="h-8 gap-1.5">
              <Link to={document.path}><Eye className="h-3.5 w-3.5" />ใบขายเดิม</Link>
            </Button>
          ) : cancellationQueue && !cancellationDocument?.canReviewCancellation ? (
            <Badge variant="outline" className="h-8 border-border bg-muted/40 px-2 text-muted-foreground">
              ไม่ต้องดำเนินการ
            </Badge>
          ) : actions.primary === 'open_document' && document.path ? (
            <Button asChild variant="outline" size="sm" className="h-8 gap-1.5">
              <Link to={document.path}>
                <Eye className="h-3.5 w-3.5" />
                {actions.primaryLabel}
              </Link>
            </Button>
          ) : (
            <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" disabled={previewLoading} onClick={onPreview}>
              {previewLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FilePlus2 className="h-3.5 w-3.5" />}
              {actions.primaryLabel}
            </Button>
          )}
          {!cancellationQueue && document.path && (
            <TikTokBillShadowButton
              loading={previewLoading}
              label={actions.detailsLabel}
              onClick={onPreview}
            />
          )}
        </div>
      </td>
    </tr>
  )
}

function TikTokAutoSMLBadge({
  state,
  retrying,
  canRetry,
  onRetry,
}: {
  state: NonNullable<TikTokOrderRow['auto_sml']>
  retrying: boolean
  canRetry: boolean
  onRetry: () => void
}) {
  const labels: Record<string, string> = {
    queued: 'Auto SML: รอคิว',
    running: 'Auto SML: กำลังส่ง',
    retry_wait: 'Auto SML: รอลองใหม่',
    needs_review: 'Auto SML: ต้องตรวจสอบ',
    succeeded: 'ส่ง SML แล้ว (AUTO)',
    failed: 'Auto SML: ไม่สำเร็จ',
    cancelled: 'Auto SML: ยกเลิกงาน',
  }
  const success = state.status === 'succeeded'
  const danger = state.status === 'failed'
  const warning = ['retry_wait', 'needs_review'].includes(state.status)
  return (
    <div className="max-w-full">
      <Badge variant="outline" className={cn(
        success && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
        danger && 'border-destructive/30 bg-destructive/10 text-destructive',
        warning && 'border-warning/40 bg-warning/10 text-warning',
      )}>
        {labels[state.status] ?? `Auto SML: ${state.status}`}
      </Badge>
      {state.error_message && <div className="mt-1 text-[11px] text-warning">{state.error_message}</div>}
      {canRetry && ['needs_review', 'failed'].includes(state.status) && (
        <Button type="button" variant="outline" size="sm" className="mt-1 h-7" disabled={retrying} onClick={onRetry}>
          {retrying && <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />}
          ลอง Auto SML ใหม่
        </Button>
      )}
    </div>
  )
}

function documentBadgeClass(tone: ReturnType<typeof tiktokDocumentState>['tone']) {
  if (tone === 'success') return 'border-accentStrong/40 bg-primary/10 text-accentStrong'
  if (tone === 'danger') return 'border-destructive/30 bg-destructive/10 text-destructive'
  if (tone === 'warning') return 'border-warning/40 bg-warning/10 text-warning'
  return 'border-border bg-muted/40 text-muted-foreground'
}

function OrderStatusBadge({ status }: { status: string }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        'font-medium',
        status === 'COMPLETED' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
        ['AWAITING_SHIPMENT', 'ON_HOLD'].includes(status) && 'border-warning/40 bg-warning/10 text-warning',
        ['PARTIALLY_SHIPPING', 'AWAITING_COLLECTION', 'IN_TRANSIT', 'DELIVERED'].includes(status) && 'border-info/30 bg-info/10 text-info',
        status === 'CANCELLED' && 'border-destructive/30 bg-destructive/10 text-destructive',
      )}
    >
      {tiktokOrderStatusLabel(status)}
    </Badge>
  )
}

function StatusColumnHelp({
  label,
  title,
  description,
  items,
}: {
  label: string
  title: string
  description: string
  items: ReadonlyArray<{ value: string; detail: string }>
}) {
  return (
    <div className="flex items-center gap-1 whitespace-nowrap">
      <span>{label}</span>
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="inline-flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label={`ดูคำอธิบาย ${label}`}
          >
            <Info className="h-3.5 w-3.5" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="max-h-[min(360px,calc(100vh-2rem))] w-[min(22rem,calc(100vw-2rem))] overflow-y-auto p-3 text-left">
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <p className="mt-1 text-xs font-normal leading-5 text-muted-foreground">{description}</p>
          <div className="mt-3 space-y-2 border-t border-border pt-3">
            {items.map((item) => (
              <div key={item.value} className="grid grid-cols-[minmax(92px,auto)_minmax(0,1fr)] gap-2 text-xs font-normal leading-5">
                <div className="font-medium text-foreground">{item.value}</div>
                <div className="text-muted-foreground">{item.detail}</div>
              </div>
            ))}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  )
}

function EmptyRow({ cancellationQueue }: { cancellationQueue: boolean }) {
  return (
    <tr>
      <td colSpan={6} className="px-3 py-8">
        <div className="mx-auto max-w-lg text-center">
          <RadioTower className="mx-auto mb-2 h-8 w-8 text-muted-foreground" />
          <div className="font-medium">{cancellationQueue ? 'ยังไม่มีออเดอร์ TikTok ที่ยกเลิก' : 'ยังไม่มี order ในคิวนี้'}</div>
          <div className="mt-1 text-sm text-muted-foreground">
            {cancellationQueue
              ? 'เมื่อ TikTok ยืนยันการยกเลิก ระบบจะแสดงรายการนี้พร้อมหลักฐานใบขายเดิม'
              : 'รอรอบซิงก์อัตโนมัติ หรือปรับตัวกรองเพื่อดู Snapshot ที่มีอยู่'}
          </div>
        </div>
      </td>
    </tr>
  )
}

function readPage(params: URLSearchParams) {
  const value = Number(params.get('page'))
  return Number.isInteger(value) && value > 0 ? value : 1
}

function readPerPage(params: URLSearchParams): typeof PAGE_SIZE_OPTIONS[number] {
  const value = Number(params.get('per_page'))
  return PAGE_SIZE_OPTIONS.includes(value as typeof PAGE_SIZE_OPTIONS[number])
    ? value as typeof PAGE_SIZE_OPTIONS[number]
    : DEFAULT_PER_PAGE
}

function formatDateTime(value?: string) {
  if (!value) return 'ยังไม่มี'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '—'
    : date.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}

function formatInterval(seconds: number) {
  return seconds % 60 === 0 ? `${seconds / 60} นาที` : `${seconds} วินาที`
}

function syncStateLabel(state: ReturnType<typeof tiktokSyncState>) {
  return state === 'active'
    ? 'ซิงก์พร้อมใช้งาน'
    : state === 'error'
      ? 'ซิงก์ล่าสุดมีปัญหา'
      : state === 'server_disabled'
        ? 'ระบบซิงก์ยังปิดอยู่'
        : 'ร้านนี้ยังไม่ได้เปิดซิงก์'
}

function apiErrorMessage(cause: unknown, fallback: string) {
  const data = (cause as { response?: { data?: { error?: { message?: string } | string } } })?.response?.data
  return typeof data?.error === 'string' ? data.error : data?.error?.message || fallback
}
