import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import axios from 'axios'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Copy,
  ExternalLink,
  Eye,
  FilePlus2,
  Loader2,
  RadioTower,
  RefreshCw,
  Search,
  Truck,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { type ServerEventType, useEventsStore } from '@/lib/events-store'
import { cn } from '@/lib/utils'
import { OrderTimelineDrawer, type ShopeeOrderPaymentBreakdown } from './ShopeeOperationsTimelineDrawer'

type Connection = {
  id: string
  shop_id: number
  label: string
  token_state: string
  can_fetch: boolean
}

type Readiness = {
  enabled: boolean
  api: {
    enabled: boolean
    configured: boolean
    connected: boolean
    blocking_reason?: string
  }
  connections: Connection[]
  push: {
    configured: boolean
    url: string
    message: string
    console_status?: string
    deployment_service_area_hint?: string
    last_event_name?: string
    last_event_at?: string
  }
  sml: {
    mode?: string
    channel?: string
    bill_type?: string
    route: string
    message: string
    can_create_document?: boolean
    ready_to_send_sml?: boolean
    document_route?: string
    endpoint?: string
    doc_format_code?: string
    doc_prefix?: string
    doc_running_format?: string
  }
}

type Counts = {
  total: number
  new_orders: number
  pending_erp: number
  needs_review: number
  erp_saved: number
  waiting_ship: number
  shipped: number
  cancelled: number
  failed: number
  tabs?: Record<string, number>
}

type OrderSnapshot = {
  id: string
  connection_id?: string
  shop_id: number
  shop_label: string
  order_sn: string
  order_status: string
  erp_status: string
  bill_id?: string
  sml_doc_no?: string
  sml_cancel_doc_no?: string
  sml_cancel_status?: string
  sml_cancel_error?: string
  document_route?: string
  bill_source_flow?: string
  buyer_username?: string
  total_amount: number
  currency?: string
  item_count: number
  package_number?: string
  logistics_status?: string
  tracking_number?: string
  shipping_carrier?: string
  checkout_shipping_carrier?: string
  shipping_tracking?: ShippingTrackingEvent[]
  ship_action_status?: string
  last_update_source?: string
  payment_method?: string
  payment_breakdown_status?: string
  last_order_update_at?: string
  last_synced_at: string
}

type ShippingTrackingEvent = {
  update_time?: number
  description?: string
  logistics_status?: string
  return_code?: string
}

type TrackingData = {
  order_sn?: string
  order_status?: string
  erp_status?: string
  package_number?: string
  logistics_status?: string
  tracking_number?: string
  shipping_carrier?: string
  checkout_carrier?: string
  ship_action_status?: string
  external_shipment?: boolean
  timeline?: ShippingTrackingEvent[]
}

type TimelineEvent = {
  id: string
  source: string
  kind: string
  title: string
  detail?: string
  status?: string
  created_at: string
}

type TimelineStep = {
  key: string
  status: string
  label: string
  detail?: string
  state: 'done' | 'current' | 'upcoming' | 'skipped' | string
  source: 'push' | 'sync' | 'snapshot' | 'nexflow' | 'shipping' | string
  confidence: 'confirmed' | 'inferred' | 'missing' | string
  occurred_at?: string
  current?: boolean
  terminal?: boolean
}

type ERPMilestone = {
  key: string
  label: string
  detail?: string
  state: 'done' | 'current' | 'upcoming' | 'failed' | string
  source: string
  confidence: 'confirmed' | 'inferred' | 'missing' | string
  occurred_at?: string
}

type TimelineResponse = {
  snapshot: OrderSnapshot
  status_timeline?: TimelineStep[]
  erp_milestones?: ERPMilestone[]
  payment_breakdown?: ShopeeOrderPaymentBreakdown | null
  events: TimelineEvent[]
}

type BulkCreateOrderRef = {
  shop_id: number
  order_sn: string
}

type BulkCreateRow = {
  shop_id: number
  order_sn: string
  buyer_username?: string
  order_status?: string
  erp_status?: string
  total_amount?: number
  item_count?: number
  bill_id?: string
  bill_url?: string
  document_route?: string
  doc_no?: string
  status: string
  reason?: string
  message?: string
}

type BulkCreatePreview = {
  route?: {
    ready?: boolean
    message?: string
    channel?: string
    bill_type?: string
    document_route?: string
    endpoint?: string
    doc_format_code?: string
    destination?: string
  }
  route_signature: string
  ready: BulkCreateRow[]
  skipped: BulkCreateRow[]
  ready_count: number
  skipped_count: number
  max_batch: number
}

type BulkCreateResult = {
  created: BulkCreateRow[]
  reused: BulkCreateRow[]
  skipped: BulkCreateRow[]
  failed: BulkCreateRow[]
  created_count: number
  reused_count: number
  skipped_count: number
  failed_count: number
}

type ListResponse = {
  data: OrderSnapshot[]
  total: number
  page: number
  per_page: number
}

type PushEvent = {
  id: string
  shop_id: number
  shop_label?: string
  order_sn: string
  push_code: number
  push_name: string
  source?: string
  event_status: string
  processing_status: string
  reconcile_status?: string
  reconcile_error?: string
  error?: string
  is_verification_event?: boolean
  received_at: string
}

type DiagnosticsFilter = 'all' | 'order' | 'shop' | 'verify' | 'failed'
type StatusGroup = 'all' | 'unpaid' | 'to_ship' | 'shipping' | 'completed' | 'cancelled'

type ShippingDocumentCreateResponse = {
  status?: string
  message?: string
  document_type?: string
  tracking?: TrackingData
}

type CancelSMLDocumentPreview = {
  status: string
  message?: string
  shop_id: number
  order_sn: string
  bill_id: string
  sale_sml_doc_no: string
  cancel_sml_doc_no?: string
  create_enabled: boolean
  can_create: boolean
  route?: {
    destination?: string
    doc_format_code?: string
    endpoint?: string
    message?: string
    ready?: boolean
  }
  total_amount: number
  item_count: number
  rollback_reality?: string
  error?: string
}

type LogisticsID = string | number

type ShippingParameterData = {
  info_needed?: {
    pickup?: string[]
    dropoff?: string[]
    non_integrated?: string[]
  }
  pickup?: {
    address_list?: Array<{
      address_id: LogisticsID
      address?: string
      time_slot_list?: Array<{
        pickup_time_id: LogisticsID
        date?: number
      }>
    }>
  }
  dropoff?: {
    branch_list?: Array<{
      branch_id: LogisticsID
      name?: string
      address?: string
    }>
  }
}

type ShippingMethod = '' | 'pickup' | 'dropoff' | 'non_integrated'

const ALL = 'all'
const DEFAULT_PER_PAGE = 20
const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const ENABLE_NON_INTEGRATED_SHIPPING_UI = false
const SELLER_CENTER_TOSHIP_URL = 'https://seller.shopee.co.th/portal/sale/shipment?type=toship'
const DROPOFF_LIMITATION_MESSAGE = 'Shopee Open API ส่งข้อมูลสาขา Dropoff ไม่พอสำหรับเลือกใน Nexflow กรุณาจัดส่งจาก Seller Center แล้ว Nexflow จะติดตามสถานะกลับมา'
const SHOP_STATUS_OPTIONS = [
  { value: ALL, label: 'ทุกสถานะ Shopee' },
  { value: 'UNPAID', label: 'UNPAID' },
  { value: 'READY_TO_SHIP', label: 'READY_TO_SHIP' },
  { value: 'PROCESSED', label: 'PROCESSED' },
  { value: 'SHIPPED', label: 'SHIPPED' },
  { value: 'COMPLETED', label: 'COMPLETED' },
  { value: 'CANCELLED', label: 'CANCELLED' },
]
const ERP_STATUS_OPTIONS = [
  { value: ALL, label: 'ทุกสถานะ ERP' },
  { value: 'pending', label: 'รอสร้างเอกสาร' },
  { value: 'pending_erp', label: 'สร้างเอกสารแล้ว รอส่ง SML' },
  { value: 'needs_review', label: 'ต้องตรวจ' },
  { value: 'sent', label: 'ส่ง SML แล้ว' },
  { value: 'failed', label: 'Failed - ส่งไม่สำเร็จ' },
  { value: 'blocked', label: 'Pending - บล็อก' },
  { value: 'cancelled', label: 'Cancelled' },
]
const DIAGNOSTIC_FILTERS: Array<{ value: DiagnosticsFilter; label: string }> = [
  { value: 'all', label: 'ทั้งหมด' },
  { value: 'order', label: 'Order events' },
  { value: 'shop', label: 'Shop auth' },
  { value: 'verify', label: 'Verify/unknown' },
  { value: 'failed', label: 'Failed' },
]
const STATUS_GROUP_TABS: Array<{ value: StatusGroup; label: string }> = [
  { value: 'all', label: 'ทั้งหมด' },
  { value: 'unpaid', label: 'ยังไม่ชำระ' },
  { value: 'to_ship', label: 'ที่ต้องจัดส่ง' },
  { value: 'shipping', label: 'กำลังจัดส่ง' },
  { value: 'completed', label: 'สำเร็จ' },
  { value: 'cancelled', label: 'ยกเลิก/คืนเงิน/คืนสินค้า' },
]

const emptyCounts: Counts = {
  total: 0,
  new_orders: 0,
  pending_erp: 0,
  needs_review: 0,
  erp_saved: 0,
  waiting_ship: 0,
  shipped: 0,
  cancelled: 0,
  failed: 0,
}

function money(v: number | undefined) {
  return new Intl.NumberFormat('th-TH', { style: 'currency', currency: 'THB' }).format(Number(v ?? 0))
}

function apiError(e: unknown) {
  if (axios.isAxiosError(e)) return e.response?.data?.error || e.response?.data?.message || e.message
  return e instanceof Error ? e.message : 'unknown error'
}

function readPage(params: URLSearchParams) {
  const n = Number(params.get('page'))
  return Number.isInteger(n) && n > 0 ? n : 1
}

function readPerPage(params: URLSearchParams): typeof PAGE_SIZE_OPTIONS[number] {
  const n = Number(params.get('per_page'))
  return PAGE_SIZE_OPTIONS.includes(n as typeof PAGE_SIZE_OPTIONS[number])
    ? n as typeof PAGE_SIZE_OPTIONS[number]
    : DEFAULT_PER_PAGE
}

function readStatusGroup(params: URLSearchParams): StatusGroup {
  const raw = params.get('status_group') ?? 'all'
  return STATUS_GROUP_TABS.some((tab) => tab.value === raw) ? raw as StatusGroup : 'all'
}

export default function ShopeeOperations() {
  const [params, setParams] = useSearchParams()
  const [readiness, setReadiness] = useState<Readiness | null>(null)
  const [counts, setCounts] = useState<Counts>(emptyCounts)
  const [orders, setOrders] = useState<OrderSnapshot[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [diagnosticsOpen, setDiagnosticsOpen] = useState(false)
  const [diagnosticsFilter, setDiagnosticsFilter] = useState<DiagnosticsFilter>('all')
  const [pushEvents, setPushEvents] = useState<PushEvent[]>([])
  const [selected, setSelected] = useState<OrderSnapshot | null>(null)
  const [erpDialogOpen, setERPDialogOpen] = useState(false)
  const [shippingDialogOpen, setShippingDialogOpen] = useState(false)
  const [shippingParams, setShippingParams] = useState<ShippingParameterData | null>(null)
  const [shippingError, setShippingError] = useState('')
  const [shippingLoading, setShippingLoading] = useState(false)
  const [shippingSubmitting, setShippingSubmitting] = useState(false)
  const [shippingMethod, setShippingMethod] = useState<ShippingMethod>('')
  const [selectedPickupAddressID, setSelectedPickupAddressID] = useState('')
  const [selectedPickupTimeID, setSelectedPickupTimeID] = useState('')
  const [shippingExtraValues, setShippingExtraValues] = useState<Record<string, string>>({})
  const [savingERP, setSavingERP] = useState(false)
  const [trackingDialogOpen, setTrackingDialogOpen] = useState(false)
  const [trackingRefreshing, setTrackingRefreshing] = useState(false)
  const [trackingError, setTrackingError] = useState('')
  const [trackingData, setTrackingData] = useState<TrackingData | null>(null)
  const [shippingReconciling, setShippingReconciling] = useState(false)
  const [labelLoadingOrderSN, setLabelLoadingOrderSN] = useState('')
  const [timelineOpen, setTimelineOpen] = useState(false)
  const [timelineLoading, setTimelineLoading] = useState(false)
  const [timelineRefreshing, setTimelineRefreshing] = useState(false)
  const [timelineError, setTimelineError] = useState('')
  const [timelineEvents, setTimelineEvents] = useState<TimelineEvent[]>([])
  const [timelineSteps, setTimelineSteps] = useState<TimelineStep[]>([])
  const [erpMilestones, setERPMilestones] = useState<ERPMilestone[]>([])
  const [paymentBreakdown, setPaymentBreakdown] = useState<ShopeeOrderPaymentBreakdown | null>(null)
  const [paymentRefreshing, setPaymentRefreshing] = useState(false)
  const [pendingListRefresh, setPendingListRefresh] = useState(false)
  const [selectedOrderKeys, setSelectedOrderKeys] = useState<Set<string>>(new Set())
  const [bulkDialogOpen, setBulkDialogOpen] = useState(false)
  const [bulkPreviewLoading, setBulkPreviewLoading] = useState(false)
  const [bulkCreating, setBulkCreating] = useState(false)
  const [bulkPreview, setBulkPreview] = useState<BulkCreatePreview | null>(null)
  const [bulkResult, setBulkResult] = useState<BulkCreateResult | null>(null)
  const [cancelSMLDialogOpen, setCancelSMLDialogOpen] = useState(false)
  const [cancelSMLPreviewLoading, setCancelSMLPreviewLoading] = useState(false)
  const [cancelSMLCreating, setCancelSMLCreating] = useState(false)
  const [cancelSMLPreview, setCancelSMLPreview] = useState<CancelSMLDocumentPreview | null>(null)
  const [cancelSMLConfirmed, setCancelSMLConfirmed] = useState(false)
  const subscribeEvents = useEventsStore((s) => s.subscribe)
  const page = readPage(params)
  const perPage = readPerPage(params)
  const statusGroup = readStatusGroup(params)
  const [pageJumpInput, setPageJumpInput] = useState(String(page))
  const totalPages = Math.max(1, Math.ceil(total / perPage))

  const shopID = params.get('shop_id') ?? ALL
  const shopStatus = params.get('status') ?? ALL
  const erpStatus = params.get('erp_status') ?? ALL
  const search = params.get('search') ?? ''
  const focusedOrderSN = params.get('order') ?? ''
  const effectiveSearch = search.trim()

  const queryString = useMemo(() => {
    const q = new URLSearchParams()
    q.set('page', String(page))
    q.set('per_page', String(perPage))
    if (shopID !== ALL) q.set('shop_id', shopID)
    if (shopStatus !== ALL) q.set('status', shopStatus)
    if (statusGroup !== ALL) q.set('status_group', statusGroup)
    if (erpStatus !== ALL) q.set('erp_status', erpStatus)
    if (effectiveSearch) q.set('search', effectiveSearch)
    return q.toString()
  }, [effectiveSearch, erpStatus, page, perPage, shopID, shopStatus, statusGroup])

  const selectedConnectionID = readiness?.connections.find((c) => String(c.shop_id) === shopID)?.id
    || readiness?.connections[0]?.id
    || ''
  const pickupAddresses = shippingParams?.pickup?.address_list ?? []
  const dropoffBranches = shippingParams?.dropoff?.branch_list ?? []
  const selectedPickupAddress = pickupAddresses.find((address) => logisticsIDKey(address.address_id) === selectedPickupAddressID)
  const pickupTimeSlots = selectedPickupAddress?.time_slot_list ?? []
  const selectedPickupTime = pickupTimeSlots.find((slot) => logisticsIDKey(slot.pickup_time_id) === selectedPickupTimeID)
  const canUsePickup = shippingMethodAvailable(shippingParams, 'pickup')
  const canUseDropoff = shippingMethodAvailable(shippingParams, 'dropoff')
  const hasHiddenNonIntegrated = shippingNonIntegratedAvailable(shippingParams) && !ENABLE_NON_INTEGRATED_SHIPPING_UI
  const shippingDisabledReason = shippingSubmitDisabledReason({
    loading: shippingLoading,
    error: shippingError,
    params: shippingParams,
    method: shippingMethod,
    selectedPickupAddress,
    selectedPickupTime,
    extraValues: shippingExtraValues,
  })
  const filteredPushEvents = useMemo(
    () => pushEvents.filter((event) => diagnosticsEventVisible(event, diagnosticsFilter)),
    [diagnosticsFilter, pushEvents],
  )
  const visibleBulkEligibleOrders = useMemo(
    () => orders.filter((order) => !bulkCreateDisabledReason(order, readiness)),
    [orders, readiness],
  )
  const selectedOrders = useMemo(
    () => orders.filter((order) => selectedOrderKeys.has(orderKey(order))),
    [orders, selectedOrderKeys],
  )
  const allVisibleEligibleSelected = visibleBulkEligibleOrders.length > 0
    && visibleBulkEligibleOrders.every((order) => selectedOrderKeys.has(orderKey(order)))
  const selectedCreateCount = selectedOrders.length

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    next.delete('page')
    next.delete('order')
    if (!value || value === ALL) next.delete(key)
    else next.set(key, value)
    setParams(next, { replace: true })
  }

  const setStatusGroup = (value: string) => {
    const next = new URLSearchParams(params)
    next.delete('page')
    next.delete('order')
    next.delete('status')
    if (!value || value === ALL) next.delete('status_group')
    else next.set('status_group', value)
    setParams(next, { replace: true })
  }

  const setPage = (nextPage: number) => {
    const next = new URLSearchParams(params)
    next.delete('order')
    if (nextPage <= 1) next.delete('page')
    else next.set('page', String(nextPage))
    setParams(next, { replace: true })
  }

  const handlePerPageChange = (value: string) => {
    const nextSize = Number(value)
    if (!PAGE_SIZE_OPTIONS.includes(nextSize as typeof PAGE_SIZE_OPTIONS[number])) return
    const next = new URLSearchParams(params)
    next.delete('page')
    next.delete('order')
    if (nextSize === DEFAULT_PER_PAGE) next.delete('per_page')
    else next.set('per_page', String(nextSize))
    setParams(next, { replace: true })
  }

  const handleJump = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const next = Number(pageJumpInput)
    if (!Number.isInteger(next) || next < 1) {
      setPageJumpInput(String(page))
      toast.error('เลขหน้าต้องเป็นจำนวนเต็มตั้งแต่ 1 ขึ้นไป')
      return
    }
    setPage(Math.min(next, totalPages))
  }

  const loadReadiness = async () => {
    const res = await client.get<Readiness>('/api/shopee-operations/readiness')
    setReadiness(res.data)
  }

  const loadOrders = async () => {
    setLoading(true)
    try {
      const [listRes, countRes] = await Promise.all([
        client.get<ListResponse>(`/api/shopee-operations/orders?${queryString}`),
        client.get<Counts>('/api/shopee-operations/counts', {
          params: shopID === ALL ? {} : { shop_id: shopID },
        }),
      ])
      setOrders(listRes.data.data ?? [])
      setTotal(Number(listRes.data.total ?? 0))
      setCounts(countRes.data ?? emptyCounts)
    } catch (e) {
      toast.error('โหลดคำสั่งซื้อ Shopee ไม่สำเร็จ: ' + apiError(e))
      setOrders([])
      setTotal(0)
      setCounts(emptyCounts)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadReadiness().catch((e) => toast.error('โหลดความพร้อมคำสั่งซื้อ Shopee ไม่สำเร็จ: ' + apiError(e)))
  }, [])

  useEffect(() => {
    setPageJumpInput(String(page))
  }, [page])

  useEffect(() => {
    void loadOrders()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryString, shopID])

  useEffect(() => {
    setSelectedOrderKeys(new Set())
  }, [queryString, shopID])

  useEffect(() => {
    const orderSN = focusedOrderSN.trim()
    if (!orderSN || timelineOpen) return
    const matched = orders.find((order) => order.order_sn === orderSN)
    if (matched) void openTimeline(matched, false)
    else void openTimelineFromDeepLink(orderSN)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusedOrderSN, orders, timelineOpen])

  useEffect(() => {
    return subscribeEvents((type: ServerEventType, payload: any) => {
      if (type !== 'shopee_realtime_changed') return
      if (
        timelineOpen &&
        selected &&
        payload?.reason === 'payment_breakdown_updated' &&
        Number(payload?.shop_id) === selected.shop_id &&
        String(payload?.order_sn ?? '') === selected.order_sn
      ) {
        void loadTimeline(selected)
        return
      }
      if (timelineOpen || trackingDialogOpen || shippingDialogOpen) {
        setPendingListRefresh(true)
        return
      }
      void loadOrders()
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subscribeEvents, queryString, shopID, trackingDialogOpen, shippingDialogOpen, timelineOpen, selected])

  const syncNow = async () => {
    if (!selectedConnectionID) {
      toast.error('ยังไม่มีร้าน Shopee ที่พร้อมซิงก์')
      return
    }
    setSyncing(true)
    try {
      const res = await client.post('/api/shopee-operations/sync', {
        connection_id: selectedConnectionID,
        days: 14,
      })
      toast.success(`ซิงก์สำเร็จ ${Number(res.data.synced_orders ?? 0).toLocaleString()} order`)
      try {
        await Promise.all([loadReadiness(), loadOrders()])
      } catch (refreshError) {
        toast.warning('ซิงก์สำเร็จแล้ว แต่รีเฟรชหน้าจอไม่สำเร็จ ให้กดรีเฟรชรายการอีกครั้ง')
        console.warn('shopee operations refresh after sync failed', refreshError)
      }
    } catch (e) {
      toast.error('ซิงก์คำสั่งซื้อ Shopee ไม่สำเร็จ: ' + apiError(e))
    } finally {
      setSyncing(false)
    }
  }

  const refreshVisibleList = async () => {
    setPendingListRefresh(false)
    await loadOrders()
  }

  const createDocument = async () => {
    if (!selected) return
    setSavingERP(true)
    try {
      const res = await client.post(`/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/create-document`, {
        confirm: 'CREATE_DOCUMENT',
      })
      toast.success(res.data.message || 'สร้างเอกสารใน Nexflow แล้ว')
      setERPDialogOpen(false)
      await loadOrders()
    } catch (e) {
      toast.error('สร้างเอกสารไม่สำเร็จ: ' + apiError(e))
    } finally {
      setSavingERP(false)
    }
  }

  const openCancelSMLPreview = async (order: OrderSnapshot) => {
    setSelected(order)
    setCancelSMLDialogOpen(true)
    setCancelSMLPreview(null)
    setCancelSMLConfirmed(false)
    setCancelSMLPreviewLoading(true)
    try {
      const res = await client.get<CancelSMLDocumentPreview>(
        `/api/shopee-operations/${order.shop_id}/${encodeURIComponent(order.order_sn)}/cancel-sml-document/preview`,
      )
      setCancelSMLPreview(res.data)
    } catch (e) {
      toast.error('เปิด preview เอกสารยกเลิก SML ไม่สำเร็จ: ' + apiError(e))
      setCancelSMLDialogOpen(false)
    } finally {
      setCancelSMLPreviewLoading(false)
    }
  }

  const createCancelSMLDocument = async () => {
    if (!selected || !cancelSMLPreview) return
    if (!cancelSMLConfirmed) {
      toast.error('กรุณาติ๊กยืนยันก่อนสร้างเอกสารยกเลิก SML')
      return
    }
    setCancelSMLCreating(true)
    try {
      const res = await client.post<CancelSMLDocumentPreview>(
        `/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/cancel-sml-document`,
        { confirm: 'CREATE_SML_CANCEL_DOCUMENT' },
      )
      setCancelSMLPreview(res.data)
      setCancelSMLConfirmed(false)
      toast.success(res.data.message || 'สร้างเอกสารยกเลิก SML แล้ว')
      await loadOrders()
      if (timelineOpen) {
        await loadTimeline(selected)
      }
    } catch (e) {
      toast.error('สร้างเอกสารยกเลิก SML ไม่สำเร็จ: ' + apiError(e))
    } finally {
      setCancelSMLCreating(false)
    }
  }

  const toggleOrderSelection = (order: OrderSnapshot, checked: boolean) => {
    const disabledReason = bulkCreateDisabledReason(order, readiness)
    if (disabledReason) {
      toast.error(disabledReason)
      return
    }
    setSelectedOrderKeys((current) => {
      const next = new Set(current)
      const key = orderKey(order)
      if (checked) next.add(key)
      else next.delete(key)
      return next
    })
  }

  const toggleAllVisibleEligible = (checked: boolean) => {
    setSelectedOrderKeys((current) => {
      const next = new Set(current)
      for (const order of visibleBulkEligibleOrders) {
        const key = orderKey(order)
        if (checked) next.add(key)
        else next.delete(key)
      }
      return next
    })
  }

  const selectedOrderRefs = (): BulkCreateOrderRef[] => selectedOrders.map((order) => ({
    shop_id: order.shop_id,
    order_sn: order.order_sn,
  }))

  const openBulkCreatePreview = async () => {
    const refs = selectedOrderRefs()
    if (refs.length === 0) {
      toast.error('กรุณาเลือก order ที่ต้องการสร้างเอกสาร')
      return
    }
    setBulkDialogOpen(true)
    setBulkPreview(null)
    setBulkResult(null)
    setBulkPreviewLoading(true)
    try {
      const res = await client.post<BulkCreatePreview>('/api/shopee-operations/create-documents/preview', { orders: refs })
      setBulkPreview(res.data)
    } catch (e) {
      toast.error('เปิด preview สร้างเอกสารไม่สำเร็จ: ' + apiError(e))
      setBulkDialogOpen(false)
    } finally {
      setBulkPreviewLoading(false)
    }
  }

  const submitBulkCreate = async () => {
    if (!bulkPreview || bulkPreview.ready_count === 0) return
    setBulkCreating(true)
    try {
      const res = await client.post<BulkCreateResult>('/api/shopee-operations/create-documents', {
        confirm: 'CREATE_DOCUMENTS',
        route_signature: bulkPreview.route_signature,
        orders: selectedOrderRefs(),
      })
      setBulkResult(res.data)
      setSelectedOrderKeys(new Set())
      await loadOrders()
      const created = Number(res.data.created_count ?? 0)
      const reused = Number(res.data.reused_count ?? 0)
      const failed = Number(res.data.failed_count ?? 0)
      if (failed > 0) toast.warning(`สร้างเอกสารสำเร็จ ${created + reused} รายการ และมีผิดพลาด ${failed} รายการ`)
      else toast.success(`สร้างเอกสารสำเร็จ ${created + reused} รายการ`)
    } catch (e) {
      const code = axios.isAxiosError(e) ? e.response?.data?.code : ''
      if (code === 'route_changed') {
        toast.error('เส้นทางคำสั่งซื้อ Shopee เปลี่ยนไป กรุณาเปิด preview ใหม่')
        setBulkPreview(null)
      } else {
        toast.error('สร้างเอกสารแบบกลุ่มไม่สำเร็จ: ' + apiError(e))
      }
    } finally {
      setBulkCreating(false)
    }
  }

  const resetShippingState = () => {
    setShippingParams(null)
    setShippingError('')
    setShippingMethod('')
    setSelectedPickupAddressID('')
    setSelectedPickupTimeID('')
    setShippingExtraValues({})
  }

  const checkShipping = async (order: OrderSnapshot) => {
    setSelected(order)
    resetShippingState()
    setShippingDialogOpen(true)
    setShippingLoading(true)
    try {
      const res = await client.get<{ data: ShippingParameterData }>(`/api/shopee-operations/${order.shop_id}/${encodeURIComponent(order.order_sn)}/shipping-parameters`)
      const data = res.data?.data ?? {}
      const nextMethod = defaultShippingMethod(data)
      const firstPickupAddress = data.pickup?.address_list?.[0]
      const firstPickupTime = firstPickupAddress?.time_slot_list?.[0]
      setShippingParams(data)
      setShippingMethod(nextMethod)
      setSelectedPickupAddressID(firstPickupAddress ? logisticsIDKey(firstPickupAddress.address_id) : '')
      setSelectedPickupTimeID(firstPickupTime ? logisticsIDKey(firstPickupTime.pickup_time_id) : '')
      setShippingExtraValues(defaultShippingExtraValues(data, nextMethod))
    } catch (e) {
      setShippingError(apiError(e))
    } finally {
      setShippingLoading(false)
    }
  }

  const submitShipping = async () => {
    if (!selected) return
    if (shippingDisabledReason) {
      toast.error(shippingDisabledReason)
      return
    }
    const payload = buildShippingPayload({
      order: selected,
      method: shippingMethod,
      params: shippingParams,
      selectedPickupAddress,
      selectedPickupTime,
      extraValues: shippingExtraValues,
    })
    setShippingSubmitting(true)
    try {
      const res = await client.post(`/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/ship`, payload)
      toast.success(res.data?.message || 'ส่งคำสั่งจัดส่งให้ Shopee แล้ว')
      setShippingDialogOpen(false)
      await loadOrders()
    } catch (e) {
      toast.error('จัดส่ง Shopee ไม่สำเร็จ: ' + apiError(e))
    } finally {
      setShippingSubmitting(false)
    }
  }

  const openTracking = async (order: OrderSnapshot) => {
    setSelected(order)
    setTrackingData(trackingFromOrder(order))
    setTrackingDialogOpen(true)
    setTrackingError('')
    try {
      const res = await client.get<{ data: TrackingData; snapshot?: OrderSnapshot }>(`/api/shopee-operations/${order.shop_id}/${encodeURIComponent(order.order_sn)}/tracking`)
      setTrackingData(res.data.data ?? trackingFromOrder(order))
      if (res.data.snapshot) {
        setSelected(res.data.snapshot)
      }
    } catch (e) {
      const message = apiError(e)
      setTrackingError(message)
      setTrackingData(trackingFromOrder(order))
      toast.error('โหลดรายละเอียดจัดส่งไม่สำเร็จ: ' + message)
    }
  }

  const loadTimeline = async (order: OrderSnapshot) => {
    setTimelineLoading(true)
    setTimelineError('')
    try {
      const res = await client.get<TimelineResponse>(`/api/shopee-operations/${order.shop_id}/${encodeURIComponent(order.order_sn)}/timeline`)
      setSelected(res.data.snapshot ?? order)
      setTimelineSteps(res.data.status_timeline ?? [])
      setERPMilestones(res.data.erp_milestones ?? [])
      setPaymentBreakdown(res.data.payment_breakdown ?? null)
      setTimelineEvents(res.data.events ?? [])
    } catch (e) {
      const message = apiError(e)
      setTimelineError(message)
      setTimelineSteps([])
      setERPMilestones([])
      setPaymentBreakdown(null)
      setTimelineEvents([])
      toast.error('โหลด timeline ไม่สำเร็จ: ' + message)
    } finally {
      setTimelineLoading(false)
    }
  }

  const openTimelineFromDeepLink = async (orderSN: string) => {
    const trimmed = orderSN.trim()
    if (!trimmed) return
    try {
      const q = new URLSearchParams()
      q.set('page', '1')
      q.set('per_page', '1')
      q.set('search', trimmed)
      const res = await client.get<ListResponse>(`/api/shopee-operations/orders?${q.toString()}`)
      const order = (res.data.data ?? []).find((row) => row.order_sn === trimmed) ?? res.data.data?.[0]
      if (order) await openTimeline(order, false)
    } catch (e) {
      toast.error('เปิด Timeline จากลิงก์ไม่สำเร็จ: ' + apiError(e))
    }
  }

  const openTimeline = async (order: OrderSnapshot, updateURL = true) => {
    setSelected(order)
    setTimelineOpen(true)
    if (updateURL) {
      const next = new URLSearchParams(params)
      next.set('order', order.order_sn)
      setParams(next, { replace: true })
    }
    await loadTimeline(order)
  }

  const handleTimelineOpenChange = (open: boolean) => {
    setTimelineOpen(open)
    if (!open) {
      const next = new URLSearchParams(params)
      next.delete('order')
      setParams(next, { replace: true })
      setPaymentBreakdown(null)
    }
  }

  const refreshTimelineFromShopee = async () => {
    if (!selected) return
    setTimelineRefreshing(true)
    setTimelineError('')
    try {
      const res = await client.post<{ data?: OrderSnapshot }>(
        `/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/reconcile-shipping?silent=1`,
      )
      const nextOrder = res.data?.data ?? selected
      setSelected(nextOrder)
      await loadTimeline(nextOrder)
      toast.success('ตรวจสถานะจาก Shopee แล้ว')
    } catch (e) {
      const message = apiError(e)
      setTimelineError(message)
      toast.error('ตรวจจาก Shopee ไม่สำเร็จ: ' + message)
    } finally {
      setTimelineRefreshing(false)
    }
  }

  const refreshPaymentBreakdown = async () => {
    if (!selected) return
    setPaymentRefreshing(true)
    try {
      const res = await client.post<{ data?: ShopeeOrderPaymentBreakdown }>(
        `/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/payment-breakdown/refresh`,
      )
      setPaymentBreakdown(res.data?.data ?? null)
      await loadTimeline(selected)
      toast.success('รีเฟรชข้อมูลชำระเงิน Shopee แล้ว')
    } catch (e) {
      toast.error('รีเฟรชข้อมูลชำระเงินไม่สำเร็จ: ' + apiError(e))
    } finally {
      setPaymentRefreshing(false)
    }
  }

  const refreshTrackingFromShopee = async () => {
    if (!selected) return
    setTrackingRefreshing(true)
    setTrackingError('')
    try {
      const res = await client.post<{ data?: OrderSnapshot; tracking?: TrackingData }>(
        `/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/reconcile-shipping?silent=1`,
      )
      const nextOrder = res.data?.data
      if (nextOrder) setSelected(nextOrder)
      setTrackingData(res.data?.tracking ?? trackingFromOrder(nextOrder ?? selected))
      toast.success('ตรวจสถานะจาก Shopee แล้ว')
    } catch (e) {
      const message = apiError(e)
      setTrackingError(message)
      toast.error('ตรวจจาก Shopee ไม่สำเร็จ: ' + message)
    } finally {
      setTrackingRefreshing(false)
    }
  }

  const refreshSelectedShippingStatus = async () => {
    if (!selected) return
    setShippingReconciling(true)
    setShippingError('')
    try {
      const res = await client.post<{ data?: OrderSnapshot; tracking?: TrackingData }>(
        `/api/shopee-operations/${selected.shop_id}/${encodeURIComponent(selected.order_sn)}/reconcile-shipping?silent=1`,
      )
      const nextOrder = res.data?.data
      if (nextOrder) {
        setSelected(nextOrder)
        setTrackingData(res.data?.tracking ?? trackingFromOrder(nextOrder))
      }
      setPendingListRefresh(true)
      toast.success('ตรวจสถานะจาก Shopee แล้ว')
    } catch (e) {
      const message = apiError(e)
      setShippingError(message)
      toast.error('ตรวจจาก Shopee ไม่สำเร็จ: ' + message)
    } finally {
      setShippingReconciling(false)
    }
  }

  const createShippingDocument = async (order: OrderSnapshot) => {
    setLabelLoadingOrderSN(order.order_sn)
    try {
      const res = await client.post<ShippingDocumentCreateResponse>(`/api/shopee-operations/${order.shop_id}/${encodeURIComponent(order.order_sn)}/shipping-document/create`)
      const message = res.data.message || 'ตรวจสิทธิ์ใบปะหน้าพัสดุแล้ว'
      if (res.data.status === 'ready') {
        toast.success(message)
        await downloadShippingDocument(order)
      } else {
        toast.info(message)
      }
      if (res.data.tracking) {
        setTrackingData(res.data.tracking)
      }
    } catch (e) {
      toast.error('ตรวจใบปะหน้าพัสดุไม่สำเร็จ: ' + apiError(e))
    } finally {
      setLabelLoadingOrderSN('')
    }
  }

  const downloadShippingDocument = async (order: OrderSnapshot) => {
    try {
      const res = await client.get<Blob>(
        `/api/shopee-operations/${order.shop_id}/${encodeURIComponent(order.order_sn)}/shipping-document/download`,
        { responseType: 'blob' },
      )
      const contentType = String(res.headers?.['content-type'] ?? '')
      if (contentType.includes('application/json')) {
        const text = await res.data.text()
        const parsed = JSON.parse(text) as { message?: string; error?: string; status?: string }
        toast.info(parsed.message || parsed.error || 'ยังดาวน์โหลดใบปะหน้าไม่ได้ กรุณาพิมพ์จาก Seller Center')
        return
      }
      const blob = new Blob([res.data], { type: contentType || 'application/pdf' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `shopee-label-${order.order_sn}.pdf`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
      toast.success('ดาวน์โหลดใบปะหน้าพัสดุแล้ว')
    } catch (e) {
      toast.error('ดาวน์โหลดใบปะหน้าพัสดุไม่สำเร็จ: ' + apiError(e))
    }
  }

  const loadDiagnostics = async () => {
    try {
      const res = await client.get<{ push_events: PushEvent[] }>('/api/shopee-operations/diagnostics')
      setPushEvents(res.data.push_events ?? [])
    } catch (e) {
      toast.error('โหลด diagnostics ไม่สำเร็จ: ' + apiError(e))
    }
  }

  const pageStart = total === 0 ? 0 : (page - 1) * perPage + 1
  const pageEnd = total === 0 ? 0 : Math.min(page * perPage, total)

  return (
    <TooltipProvider>
      <div className="space-y-4">
        <div className="rounded-lg border border-border bg-card px-3 py-2">
          <div className="flex flex-col gap-2 xl:flex-row xl:items-center xl:justify-between">
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <h1 className="text-lg font-semibold tracking-normal">คำสั่งซื้อ Shopee</h1>
                <Badge className="h-6 bg-primary px-2 text-[11px] text-primary-foreground">Push/Sync</Badge>
                <span
                  className="inline-flex h-6 items-center rounded-full border border-border bg-background px-2 text-xs text-muted-foreground"
                  title="สร้างเอกสารใน Nexflow แล้วส่ง SML จากหน้าคิวเอกสาร ส่วนจัดส่งและใบปะหน้าทำใน Seller Center"
                >
                  {readiness?.sml.doc_format_code || 'route'} · {readiness?.sml.route || 'ยังไม่ตั้งค่า'}
                </span>
              </div>
              <p className="max-w-3xl text-xs leading-5 text-muted-foreground">
                งานประจำวัน: ติดตาม order สดจาก Shopee, สร้างเอกสารใน Nexflow แล้วส่ง SML จากคิวเอกสารเดิม{' '}
                <Button asChild variant="link" className="h-auto px-0 py-0 text-xs font-medium">
                  <Link to="/import/shopee">ต้องดึงย้อนหลังหรือ order ไม่เข้า? ไปนำเข้า Shopee ย้อนหลัง</Link>
                </Button>
              </p>
              <OperationsHealthLine readiness={readiness} />
            </div>
            <div className="flex flex-col gap-2 sm:flex-row xl:shrink-0">
              <Select value={shopID} onValueChange={(v) => setParam('shop_id', v)}>
                <SelectTrigger className="h-8 min-w-[160px] bg-background">
                  <SelectValue placeholder="ร้าน Shopee" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>ทุกร้าน</SelectItem>
                  {(readiness?.connections ?? []).map((conn) => (
                    <SelectItem key={conn.id} value={String(conn.shop_id)}>{conn.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button variant="outline" size="sm" className="h-8 gap-2 bg-background" onClick={() => { setDiagnosticsOpen((v) => !v); if (!diagnosticsOpen) void loadDiagnostics() }}>
                <Eye className="h-4 w-4" />
                ตรวจระบบ
              </Button>
              <Button size="sm" className="h-8 gap-2" disabled={syncing || !selectedConnectionID} onClick={syncNow} title={!selectedConnectionID ? 'ยังไม่มีร้าน Shopee ที่พร้อมซิงก์' : undefined}>
                {syncing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                ซิงก์
              </Button>
            </div>
          </div>

          {diagnosticsOpen && (
            <div className="mt-3 rounded-md border border-border bg-muted/30 p-3">
              <div className="mb-3 flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <div className="text-sm font-medium">Push diagnostics ล่าสุด</div>
                  <div className="text-xs text-muted-foreground">แยก event จริง, verify ping, สิทธิ์ร้าน และงานที่ reconcile ไม่สำเร็จ</div>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                  {DIAGNOSTIC_FILTERS.map((filter) => (
                    <Button
                      key={filter.value}
                      variant={diagnosticsFilter === filter.value ? 'default' : 'outline'}
                      size="sm"
                      className="h-8"
                      onClick={() => setDiagnosticsFilter(filter.value)}
                    >
                      {filter.label}
                    </Button>
                  ))}
                  <Button variant="outline" size="sm" className="h-8 bg-background" onClick={loadDiagnostics}>รีเฟรช</Button>
                </div>
              </div>
              <div className="space-y-1 text-xs text-muted-foreground">
                {pushEvents.length === 0 ? (
                  <div className="rounded-md border border-border bg-background p-3">ยังไม่มี push event ที่บันทึกไว้</div>
                ) : filteredPushEvents.length === 0 ? (
                  <div className="rounded-md border border-border bg-background p-3">ไม่มี event ในตัวกรองนี้</div>
                ) : filteredPushEvents.map((event) => (
                  <div key={event.id} className="grid gap-2 rounded-md border border-border bg-background p-2 lg:grid-cols-[120px_minmax(0,1fr)_150px_130px] lg:items-center">
                    <div className="space-y-1">
                      <SourceBadge source={event.source} verify={event.is_verification_event} />
                      <div>{dayjs(event.received_at).format('DD/MM/YY HH:mm')}</div>
                    </div>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium text-foreground">{event.push_name || 'unknown'}</span>
                        <span className="font-mono text-muted-foreground">code {event.push_code}</span>
                        {event.is_verification_event && <Badge variant="outline" className="bg-warning/10 text-warning">ไม่ใช่ออเดอร์จริง</Badge>}
                      </div>
                      <div className="mt-0.5 truncate">
                        {event.order_sn ? <span className="font-mono text-foreground">{event.order_sn}</span> : 'ไม่มี Order SN'}
                        {event.shop_label ? <span> · {event.shop_label}</span> : event.shop_id ? <span> · shop {event.shop_id}</span> : null}
                      </div>
                      {(event.reconcile_error || event.error) && (
                        <div className="mt-1 text-destructive">{event.reconcile_error || event.error}</div>
                      )}
                    </div>
                    <div>
                      <div className="text-[11px] text-muted-foreground">Event</div>
                      <Badge variant="outline" className="mt-0.5 h-5 bg-background text-[10px]">{event.processing_status || '-'}</Badge>
                    </div>
                    <div>
                      <div className="text-[11px] text-muted-foreground">Reconcile</div>
                      <Badge variant="outline" className={cn('mt-0.5 h-5 bg-background text-[10px]', diagnosticStatusTone(event.reconcile_status))}>
                        {event.reconcile_status || '-'}
                      </Badge>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        <div className="rounded-lg border border-border bg-card px-3 pt-2">
          <Tabs value={statusGroup} onValueChange={setStatusGroup}>
            <TabsList className="h-auto w-full justify-start overflow-x-auto rounded-none border-b border-border bg-transparent p-0">
              {STATUS_GROUP_TABS.map((tab) => (
                <TabsTrigger
                  key={tab.value}
                  value={tab.value}
                  className="h-10 shrink-0 rounded-none border-b-2 border-transparent bg-transparent px-3 text-sm data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none"
                >
                  <span>{tab.label}</span>
                  <Badge variant="outline" className="ml-2 h-5 bg-background px-1.5 text-[10px]">
                    {tabCount(counts, tab.value).toLocaleString()}
                  </Badge>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>

        <div className="rounded-lg border border-border bg-card p-3">
          <div className="grid gap-2 lg:grid-cols-[minmax(260px,1fr)_220px] lg:items-center">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setParam('search', e.target.value)}
                className="h-9 pl-8"
                placeholder="ค้นหา Order SN, ลูกค้า, tracking, SML doc"
              />
            </div>
            <Select value={erpStatus} onValueChange={(v) => setParam('erp_status', v)}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ERP_STATUS_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
        </div>

        {pendingListRefresh && (
          <div className="flex flex-col gap-2 rounded-lg border border-info/30 bg-info/10 px-3 py-2 text-sm sm:flex-row sm:items-center sm:justify-between">
            <div className="text-info">มีข้อมูล Shopee ใหม่ กดรีเฟรชรายการเมื่อพร้อมเพื่อไม่ให้หน้ากระโดดระหว่างทำงาน</div>
            <Button variant="outline" size="sm" className="h-8 bg-background" onClick={() => void refreshVisibleList()}>
              <RefreshCw className="mr-2 h-3.5 w-3.5" />
              รีเฟรชรายการ
            </Button>
          </div>
        )}

        {selectedCreateCount > 0 && (
          <div className="sticky top-2 z-20 flex flex-col gap-2 rounded-lg border border-primary/30 bg-background/95 px-3 py-2 text-sm shadow-sm backdrop-blur sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="font-medium">เลือกแล้ว {selectedCreateCount.toLocaleString()} order</div>
              <div className="text-xs text-muted-foreground">สร้างเอกสารใน Nexflow เท่านั้น ยังไม่ส่งเข้า SML</div>
            </div>
            <div className="flex flex-wrap gap-2 sm:justify-end">
              <Button variant="outline" size="sm" className="h-8 bg-background" onClick={() => setSelectedOrderKeys(new Set())}>
                ล้างที่เลือก
              </Button>
              <Button size="sm" className="h-8 gap-2" onClick={() => void openBulkCreatePreview()}>
                <FilePlus2 className="h-3.5 w-3.5" />
                สร้างเอกสาร {selectedCreateCount.toLocaleString()} รายการ
              </Button>
            </div>
          </div>
        )}

        <div className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[1040px] text-sm">
              <thead className="bg-muted/50 text-xs text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 text-left">
                    <div className="flex items-center gap-3">
                      <Checkbox
                        checked={allVisibleEligibleSelected ? true : selectedCreateCount > 0 ? 'indeterminate' : false}
                        disabled={visibleBulkEligibleOrders.length === 0}
                        onClick={(event) => event.stopPropagation()}
                        onCheckedChange={(checked) => toggleAllVisibleEligible(checked === true)}
                        aria-label="เลือก order ที่สร้างเอกสารได้ทั้งหมดในหน้านี้"
                      />
                      <span>Order</span>
                    </div>
                  </th>
                  <th className="px-3 py-2 text-left">ลูกค้า / ร้าน</th>
                  <th className="px-3 py-2 text-right">ยอดเงิน</th>
                  <th className="px-3 py-2 text-left">Shopee</th>
                  <th className="px-3 py-2 text-left">ERP</th>
                  <th className="px-3 py-2 text-left">Logistics</th>
                  <th className="px-3 py-2 text-right">Action</th>
                </tr>
              </thead>
              <tbody>
                {loading && (
                  <tr>
                    <td colSpan={7} className="px-3 py-8 text-center text-muted-foreground">กำลังโหลด...</td>
                  </tr>
                )}
                {!loading && orders.length === 0 && (
                  <tr>
                    <td colSpan={7} className="px-3 py-8">
                      <div className="mx-auto max-w-lg text-center">
                        <RadioTower className="mx-auto mb-2 h-8 w-8 text-muted-foreground" />
                        <div className="font-medium">ยังไม่มี order ในคิวนี้</div>
                        <div className="mt-1 text-sm text-muted-foreground">กดซิงก์เพื่อดึง snapshot จาก Shopee หรือปรับ filter</div>
                        <Button asChild variant="link" size="sm" className="mt-1 h-auto px-0 text-xs">
                          <Link to="/import/shopee">ถ้าเป็นข้อมูลย้อนหลังหรือรายการตกหล่น ให้ไปนำเข้า Shopee ย้อนหลัง</Link>
                        </Button>
                      </div>
                    </td>
                  </tr>
                )}
                {!loading && orders.map((order) => {
                  const selectableReason = bulkCreateDisabledReason(order, readiness)
                  const checked = selectedOrderKeys.has(orderKey(order))
                  return (
                    <tr
                      key={order.id}
                      className={cn('border-t border-border hover:bg-muted/30', focusedOrderSN === order.order_sn && 'bg-primary/5', checked && 'bg-primary/5')}
                    >
                    <td className="px-3 py-2">
                      <div className="flex items-start gap-3">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span
                              className="mt-0.5 inline-flex"
                              onClick={(event) => event.stopPropagation()}
                            >
                              <Checkbox
                                checked={checked}
                                disabled={Boolean(selectableReason)}
                                onCheckedChange={(value) => toggleOrderSelection(order, value === true)}
                                aria-label={`เลือก order ${order.order_sn}`}
                              />
                            </span>
                          </TooltipTrigger>
                          {selectableReason && <TooltipContent>{selectableReason}</TooltipContent>}
                        </Tooltip>
                        <div className="min-w-0">
                          <div className="font-mono text-xs font-medium text-foreground">{order.order_sn}</div>
                          <div className="text-xs text-muted-foreground">
                            {order.last_order_update_at ? dayjs(order.last_order_update_at).format('DD/MM/YY HH:mm') : 'ยังไม่มี update_time'}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      <div className="font-medium">{order.buyer_username || '-'}</div>
                      <div className="text-xs text-muted-foreground">{order.shop_label || order.shop_id}</div>
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      <div>{money(order.total_amount)}</div>
                      <div className="text-xs text-muted-foreground">{order.item_count.toLocaleString()} รายการ</div>
                      <PaymentBreakdownBadge status={order.payment_breakdown_status} />
                    </td>
                    <td className="px-3 py-2"><OrderStatusBadge status={order.order_status} /></td>
                    <td className="px-3 py-2">
                      <ERPStatusBadge status={order.erp_status} />
                      <div className="mt-1 text-xs text-muted-foreground">
                        {order.sml_doc_no ? <code>{order.sml_doc_no}</code> : order.bill_id ? 'สร้างเอกสารแล้ว' : 'รอสร้างเอกสาร'}
                      </div>
                      {cancelSMLBadge(order)}
                      {isImportFallbackBill(order) && (
                        <Badge variant="outline" className="mt-1 h-5 border-info/40 bg-info/10 px-1.5 text-[10px] text-info">
                          สร้างจากนำเข้าย้อนหลัง
                        </Badge>
                      )}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-xs font-medium">{shippingStateLabel(order)}</span>
                        {externalShipment(order) && (
                          <Badge variant="outline" className="h-5 border-info/40 bg-info/10 px-1.5 text-[10px] text-info">Seller Center</Badge>
                        )}
                      </div>
                      <div className="mt-0.5 text-xs text-muted-foreground">{carrierLabel(order) || '-'}</div>
                      <div className="mt-0.5 flex flex-wrap items-center gap-1.5">
                        <span className="font-mono text-[11px] text-muted-foreground">{order.tracking_number || order.package_number || '-'}</span>
                        <UpdateSourceBadge source={order.last_update_source} />
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap justify-end gap-1.5">
                        {order.bill_id && (
                          <Button asChild variant="outline" size="sm" className="h-8 gap-1.5">
                            <Link to={documentPath(order)} onClick={(event) => event.stopPropagation()}>
                              <Eye className="h-3.5 w-3.5" />
                              เอกสาร
                            </Link>
                          </Button>
                        )}
                        {canChangeDocumentRoute(order) && (
                          <Button asChild variant="outline" size="sm" className="h-8 gap-1.5">
                            <Link to={documentPath(order)} onClick={(event) => event.stopPropagation()} title="เปิดเอกสารเดิมเพื่อเก็บไว้และสร้างใหม่ตามเส้นทาง SML ล่าสุด">
                              <RefreshCw className="h-3.5 w-3.5" />
                              เปลี่ยนเส้นทาง
                            </Link>
                          </Button>
                        )}
                        <GuardedButton
                          icon={<FilePlus2 className="h-3.5 w-3.5" />}
                          label="สร้างเอกสาร"
                          disabledReason={erpDisabledReason(order)}
                          onClick={() => { setSelected(order); setERPDialogOpen(true) }}
                        />
                        {shouldShowCancelSMLAction(order) && (
                          <GuardedButton
                            icon={<AlertTriangle className="h-3.5 w-3.5" />}
                            label="สร้างเอกสารยกเลิก"
                            disabledReason={cancelSMLDisabledReason(order)}
                            onClick={() => void openCancelSMLPreview(order)}
                          />
                        )}
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-8 gap-1.5"
                          onClick={(event) => {
                            event.stopPropagation()
                            void openTimeline(order)
                          }}
                        >
                          <Truck className="h-3.5 w-3.5" />
                          Timeline
                        </Button>
                      </div>
                    </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          <div className="flex flex-col gap-2 border-t border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground lg:flex-row lg:items-center lg:justify-between">
            <span>
              {total > 0
                ? `แสดง ${pageStart.toLocaleString()}-${pageEnd.toLocaleString()} จาก ${total.toLocaleString()} order`
                : `แสดง ${orders.length.toLocaleString()} order`}
            </span>
            <div className="flex flex-wrap items-center gap-2 lg:justify-end">
              <label className="inline-flex items-center gap-1.5">
                <span>ต่อหน้า</span>
                <Select value={String(perPage)} onValueChange={handlePerPageChange}>
                  <SelectTrigger className="h-8 w-[82px] text-xs" aria-label="จำนวน order ต่อหน้า">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PAGE_SIZE_OPTIONS.map((size) => <SelectItem key={size} value={String(size)}>{size}</SelectItem>)}
                  </SelectContent>
                </Select>
              </label>
              <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage(1)}>หน้าแรก</Button>
              <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="h-3.5 w-3.5" />
                ก่อนหน้า
              </Button>
              <span className="min-w-[92px] text-center tabular-nums">หน้า {page.toLocaleString()} / {totalPages.toLocaleString()}</span>
              <form className="inline-flex items-center gap-1.5" onSubmit={handleJump}>
                <span>ไปหน้า</span>
                <Input
                  type="number"
                  min={1}
                  max={totalPages}
                  value={pageJumpInput}
                  onChange={(e) => setPageJumpInput(e.target.value)}
                  className="h-8 w-20 px-2 text-center text-xs tabular-nums"
                  aria-label="ไปหน้าที่"
                />
                <Button type="submit" variant="outline" size="sm" disabled={totalPages <= 1 || loading}>ไป</Button>
              </form>
              <Button variant="outline" size="sm" disabled={page >= totalPages || loading} onClick={() => setPage(page + 1)}>
                ถัดไป
                <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </div>

        <Dialog open={bulkDialogOpen} onOpenChange={setBulkDialogOpen}>
          <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>สร้างเอกสารหลายรายการ</DialogTitle>
              <DialogDescription>
                สร้างเอกสารใน Nexflow เท่านั้น ยังไม่ส่งเข้า SML และไม่จัดส่ง Shopee
              </DialogDescription>
            </DialogHeader>

            {bulkPreviewLoading ? (
              <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                <Loader2 className="mr-2 inline h-4 w-4 animate-spin" />
                กำลังตรวจรายการที่เลือก...
              </div>
            ) : bulkResult ? (
              <div className="space-y-3">
                <div className="grid gap-2 sm:grid-cols-4">
                  <BulkMetric label="สร้างใหม่" value={bulkResult.created_count} tone="success" />
                  <BulkMetric label="มีอยู่แล้ว" value={bulkResult.reused_count} tone="info" />
                  <BulkMetric label="ถูกข้าม" value={bulkResult.skipped_count} tone="warning" />
                  <BulkMetric label="ผิดพลาด" value={bulkResult.failed_count} tone="danger" />
                </div>
                <BulkResultSection title="เอกสารที่สร้างใหม่" rows={bulkResult.created} />
                <BulkResultSection title="เอกสารที่มีอยู่แล้ว" rows={bulkResult.reused} />
                <BulkResultSection title="รายการที่ถูกข้าม" rows={bulkResult.skipped} />
                <BulkResultSection title="รายการที่ผิดพลาด" rows={bulkResult.failed} danger />
              </div>
            ) : bulkPreview ? (
              <div className="space-y-3">
                <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
                  <div className="font-medium">ปลายทางเอกสาร</div>
                  <div className="mt-1 text-muted-foreground">
                    {bulkPreview.route?.destination || 'ยังไม่ตั้งค่า'} · {bulkPreview.route?.doc_format_code || '-'} · {bulkPreview.route?.document_route || '-'}
                  </div>
                  {bulkPreview.route?.message && (
                    <div className="mt-2 text-warning">{bulkPreview.route.message}</div>
                  )}
                </div>
                <div className="grid gap-2 sm:grid-cols-2">
                  <BulkMetric label="พร้อมสร้าง" value={bulkPreview.ready_count} tone="success" />
                  <BulkMetric label="ถูกข้าม" value={bulkPreview.skipped_count} tone={bulkPreview.skipped_count > 0 ? 'warning' : 'info'} />
                </div>
                <Alert>
                  <AlertTriangle className="h-4 w-4" />
                  <AlertTitle>ยังไม่ส่งเข้า SML</AlertTitle>
                  <AlertDescription>
                    หลังสร้างเอกสารแล้ว ให้ผู้ใช้ไปตรวจและส่งเข้า SML จาก `/sales-orders` หรือ `/sale-invoices` ตาม route ที่ตั้งไว้
                  </AlertDescription>
                </Alert>
                <BulkPreviewSection title="พร้อมสร้างเอกสาร" rows={bulkPreview.ready} />
                <BulkPreviewSection title="รายการที่จะถูกข้าม" rows={bulkPreview.skipped} muted />
              </div>
            ) : (
              <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                ยังไม่มี preview
              </div>
            )}

            <DialogFooter>
              <Button variant="outline" onClick={() => setBulkDialogOpen(false)}>ปิด</Button>
              {!bulkResult && (
                <Button
                  onClick={() => void submitBulkCreate()}
                  disabled={bulkPreviewLoading || bulkCreating || !bulkPreview || bulkPreview.ready_count === 0}
                >
                  {bulkCreating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                  สร้างเอกสาร {bulkPreview?.ready_count.toLocaleString() ?? 0} รายการ
                </Button>
              )}
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={erpDialogOpen} onOpenChange={setERPDialogOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>สร้างเอกสารใน Nexflow</DialogTitle>
              <DialogDescription>
                ระบบจะดึงข้อมูลล่าสุดจาก Shopee แล้วสร้างหรือ reuse เอกสารใน Nexflow เท่านั้น ยังไม่ส่งเข้า SML
              </DialogDescription>
            </DialogHeader>
            {selected && (
              <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
                <div className="font-mono text-xs">{selected.order_sn}</div>
                <div className="mt-1">ยอด {money(selected.total_amount)} · {selected.item_count} รายการ</div>
                <div className="mt-1 text-muted-foreground">Shopee {selected.order_status} · ERP {erpStatusLabel(selected.erp_status)}</div>
                <div className="mt-1 text-muted-foreground">
                  ปลายทาง: {readiness?.sml.route || 'ยังไม่ตั้งค่า'} {readiness?.sml.doc_format_code ? `(${readiness.sml.doc_format_code})` : ''}
                </div>
              </div>
            )}
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>กันเอกสารซ้ำ</AlertTitle>
              <AlertDescription>
                Action นี้มี idempotency guard: ถ้ามีเอกสารเดิมจะ reuse ทันที ถ้ายังไม่มีจะสร้างจาก snapshot ล่าสุดของ Shopee และให้ผู้ใช้ไปส่ง SML จากคิวเอกสารเอง
              </AlertDescription>
            </Alert>
            <DialogFooter>
              <Button variant="outline" onClick={() => setERPDialogOpen(false)}>ยกเลิก</Button>
              <Button onClick={createDocument} disabled={savingERP || !readiness?.sml.can_create_document} title={!readiness?.sml.can_create_document ? 'ยังไม่ได้ตั้งค่า route คำสั่งซื้อ Shopee' : undefined}>
                {savingERP && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                สร้างเอกสาร
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={cancelSMLDialogOpen} onOpenChange={(open) => {
          setCancelSMLDialogOpen(open)
          if (!open) setCancelSMLConfirmed(false)
        }}>
          <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>สร้างเอกสารยกเลิก SML</DialogTitle>
              <DialogDescription>
                สำหรับ Shopee order ที่ถูกยกเลิกหลังส่งใบขายเข้า SML แล้ว
              </DialogDescription>
            </DialogHeader>

            {cancelSMLPreviewLoading ? (
              <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                <Loader2 className="mr-2 inline h-4 w-4 animate-spin" />
                กำลังตรวจ preview จาก SML...
              </div>
            ) : cancelSMLPreview ? (
              <div className="space-y-3">
                {cancelSMLPreview.error && (
                  <Alert variant="destructive">
                    <AlertTriangle className="h-4 w-4" />
                    <AlertTitle>ยังสร้างไม่ได้</AlertTitle>
                    <AlertDescription>{cancelSMLPreview.error}</AlertDescription>
                  </Alert>
                )}
                {!cancelSMLPreview.create_enabled && !cancelSMLStatusDone(cancelSMLPreview.status) && (
                  <Alert className="border-warning/40 bg-warning/10">
                    <AlertTriangle className="h-4 w-4 text-warning" />
                    <AlertTitle>ปิดการสร้าง CN อยู่</AlertTitle>
                    <AlertDescription>
                      เปิด notification และ preview ได้แล้ว แต่ปุ่มสร้างจริงยังถูกปิดด้วย feature flag จนกว่า SML domain จะพร้อม
                    </AlertDescription>
                  </Alert>
                )}
                {cancelSMLStatusDone(cancelSMLPreview.status) && (
                  <Alert className="border-success/30 bg-success/10">
                    <CheckCircle2 className="h-4 w-4 text-success" />
                    <AlertTitle>มีเอกสารยกเลิก SML แล้ว</AlertTitle>
                    <AlertDescription>
                      {cancelSMLPreview.cancel_sml_doc_no ? `เลขเอกสาร ${cancelSMLPreview.cancel_sml_doc_no}` : cancelSMLPreview.message || 'ตรวจพบเอกสารเดิมใน SML'}
                    </AlertDescription>
                  </Alert>
                )}
                <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
                  <div className="grid gap-3 sm:grid-cols-2">
                    <PreviewKV label="ใบขายเดิม" value={cancelSMLPreview.sale_sml_doc_no || '-'} mono />
                    <PreviewKV label="เลขเอกสารยกเลิก" value={cancelSMLPreview.cancel_sml_doc_no || 'รอ SML preview'} mono />
                    <PreviewKV label="ยอด" value={money(cancelSMLPreview.total_amount)} />
                    <PreviewKV label="จำนวนรายการ" value={`${Number(cancelSMLPreview.item_count ?? 0).toLocaleString()} รายการ`} />
                  </div>
                  <div className="mt-3 border-t border-border pt-3">
                    <PreviewKV
                      label="เส้นทางเอกสาร"
                      value={`${cancelSMLPreview.route?.destination || 'ขาย -> ยกเลิกขายสินค้าและบริการ'}${cancelSMLPreview.route?.doc_format_code ? ` (${cancelSMLPreview.route.doc_format_code})` : ''}`}
                    />
                  </div>
                </div>
                <Alert className="border-destructive/30 bg-destructive/10">
                  <AlertTriangle className="h-4 w-4 text-destructive" />
                  <AlertTitle>Rollback reality</AlertTitle>
                  <AlertDescription>
                    {cancelSMLPreview.rollback_reality || 'หลังสร้าง CN แล้ว การย้อนกลับต้องตรวจใน SML ด้วยคนทำงาน'}
                  </AlertDescription>
                </Alert>
                {!cancelSMLStatusDone(cancelSMLPreview.status) && (
                  <label className="flex items-start gap-3 rounded-md border border-border bg-background p-3 text-sm">
                    <Checkbox
                      className="mt-0.5"
                      checked={cancelSMLConfirmed}
                      onCheckedChange={(value) => setCancelSMLConfirmed(value === true)}
                    />
                    <span className="leading-5">
                      ยืนยันว่า Shopee order นี้ยกเลิกแล้ว และต้องสร้างเอกสาร “ขาย - ยกเลิกขายสินค้าและบริการ” เพื่ออ้างใบขายเดิมใน SML
                    </span>
                  </label>
                )}
              </div>
            ) : (
              <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                ยังไม่มี preview
              </div>
            )}

            <DialogFooter>
              <Button variant="outline" onClick={() => setCancelSMLDialogOpen(false)}>ปิด</Button>
              {cancelSMLPreview && !cancelSMLStatusDone(cancelSMLPreview.status) && (
                <Button
                  variant="destructive"
                  onClick={() => void createCancelSMLDocument()}
                  disabled={cancelSMLCreating || !cancelSMLPreview.can_create || !cancelSMLConfirmed}
                  title={cancelSMLCreateDisabledTitle(cancelSMLPreview, cancelSMLConfirmed)}
                >
                  {cancelSMLCreating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                  สร้างเอกสารยกเลิก SML
                </Button>
              )}
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <OrderTimelineDrawer
          open={timelineOpen}
          order={selected}
          steps={timelineSteps}
          milestones={erpMilestones}
          events={timelineEvents}
          loading={timelineLoading}
          refreshing={timelineRefreshing}
          paymentBreakdown={paymentBreakdown}
          paymentRefreshing={paymentRefreshing}
          error={timelineError}
          canCreateDocument={Boolean(readiness?.sml.can_create_document)}
          createDocumentDisabledReason={selected ? erpDisabledReason(selected) : ''}
          savingDocument={savingERP}
          onOpenChange={handleTimelineOpenChange}
          onCreateDocument={() => setERPDialogOpen(true)}
          onCopyOrder={() => selected && void copyText(selected.order_sn, 'คัดลอก Order SN แล้ว')}
          onRefresh={() => void refreshTimelineFromShopee()}
          onRefreshPayment={() => void refreshPaymentBreakdown()}
          documentPath={documentPath}
        />
      </div>
    </TooltipProvider>
  )
}

function BulkMetric({ label, value, tone }: { label: string; value: number; tone?: 'success' | 'info' | 'warning' | 'danger' }) {
  return (
    <div className={cn(
      'rounded-md border bg-background p-3',
      tone === 'success' && 'border-success/30 bg-success/10',
      tone === 'info' && 'border-info/30 bg-info/10',
      tone === 'warning' && 'border-warning/30 bg-warning/10',
      tone === 'danger' && 'border-destructive/30 bg-destructive/10',
    )}>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold tabular-nums">{Number(value ?? 0).toLocaleString()}</div>
    </div>
  )
}

function PreviewKV({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn('mt-0.5 truncate text-sm font-medium text-foreground', mono && 'font-mono text-xs')}>
        {value || '-'}
      </div>
    </div>
  )
}

function BulkPreviewSection({ title, rows, muted }: { title: string; rows: BulkCreateRow[]; muted?: boolean }) {
  if (!rows.length) return null
  return (
    <div className="rounded-md border border-border bg-background">
      <div className="border-b border-border px-3 py-2 text-sm font-medium">{title}</div>
      <div className="max-h-56 overflow-y-auto">
        {rows.map((row) => (
          <div key={`${row.shop_id}:${row.order_sn}`} className="grid gap-2 border-b border-border px-3 py-2 text-sm last:border-0 sm:grid-cols-[minmax(0,1fr)_130px]">
            <div className="min-w-0">
              <div className="truncate font-mono text-xs font-medium">{row.order_sn}</div>
              <div className="mt-0.5 truncate text-xs text-muted-foreground">
                {row.buyer_username || '-'} · {money(row.total_amount)} · {row.item_count ?? 0} รายการ
              </div>
              {row.reason && <div className={cn('mt-1 text-xs', muted ? 'text-warning' : 'text-muted-foreground')}>{row.reason}</div>}
            </div>
            <div className="flex flex-wrap items-center gap-1.5 sm:justify-end">
              {row.order_status && <OrderStatusBadge status={row.order_status} />}
              {row.erp_status && <ERPStatusBadge status={row.erp_status} />}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function BulkResultSection({ title, rows, danger }: { title: string; rows: BulkCreateRow[]; danger?: boolean }) {
  if (!rows.length) return null
  return (
    <div className="rounded-md border border-border bg-background">
      <div className="border-b border-border px-3 py-2 text-sm font-medium">{title}</div>
      <div className="max-h-56 overflow-y-auto">
        {rows.map((row) => (
          <div key={`${row.shop_id}:${row.order_sn}`} className="grid gap-2 border-b border-border px-3 py-2 text-sm last:border-0 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
            <div className="min-w-0">
              <div className="truncate font-mono text-xs font-medium">{row.order_sn}</div>
              {(row.message || row.reason) && (
                <div className={cn('mt-0.5 text-xs', danger ? 'text-destructive' : 'text-muted-foreground')}>
                  {row.reason || row.message}
                </div>
              )}
            </div>
            {row.bill_url ? (
              <Button asChild variant="outline" size="sm" className="h-8">
                <Link to={row.bill_url}>เปิดเอกสาร</Link>
              </Button>
            ) : (
              <span className="text-xs text-muted-foreground">{row.status}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function OperationsHealthLine({ readiness }: { readiness: Readiness | null }) {
  if (!readiness) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        กำลังตรวจสถานะคำสั่งซื้อ Shopee...
      </div>
    )
  }
  const apiOk = Boolean(readiness.api.enabled && readiness.api.configured && readiness.api.connected)
  const pushOk = Boolean(readiness.push.configured && readiness.push.console_status === 'receiving')
  const documentOk = Boolean(readiness.sml.can_create_document)
  const tokenOk = Boolean(readiness.connections.some((c) => c.token_state === 'valid' || c.can_fetch))
  const issues = [
    !apiOk ? readiness.api.blocking_reason || 'Shopee API ยังไม่พร้อมใช้งาน' : '',
    !pushOk ? readiness.push.message || `Push callback ยังไม่พร้อม, ตรวจ Deployment Service Area ${readiness.push.deployment_service_area_hint || 'Singapore'}` : '',
    !documentOk ? readiness.sml.message || 'ยังไม่ได้ตั้งค่าเส้นทางคำสั่งซื้อ Shopee สำหรับสร้างเอกสาร' : '',
    !tokenOk ? 'Shopee token ใช้งานไม่ได้หรือหมดอายุ' : '',
  ].filter(Boolean)
  const pushLabel = readiness.push.last_event_name
    ? `Push ${readiness.push.last_event_at ? dayjs(readiness.push.last_event_at).format('HH:mm') : readiness.push.last_event_name}`
    : pushOk ? 'Push พร้อมรับ' : 'Push ต้องตรวจ'
  const pushTitle = readiness.push.last_event_name
    ? `Push ล่าสุด ${readiness.push.last_event_name}${readiness.push.last_event_at ? ` เมื่อ ${dayjs(readiness.push.last_event_at).format('DD/MM/YY HH:mm')}` : ''}`
    : readiness.push.message || ''

  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
        <HealthChip ok={issues.length === 0} label={issues.length === 0 ? 'พร้อมใช้งาน' : `ต้องตรวจ ${issues.length} จุด`} />
        <span className="inline-flex min-h-6 items-center text-xs text-muted-foreground" title={pushTitle || undefined}>
          {pushLabel}
        </span>
        {!documentOk && <span className="text-warning">route ยังไม่พร้อม</span>}
      </div>
      {issues.length > 0 && (
        <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-foreground">
          <div className="flex items-start gap-2">
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            <div className="min-w-0">
              <div className="font-medium">มีสถานะที่ต้องตรวจ</div>
              <div className="mt-1 text-muted-foreground">{issues.join(' · ')}</div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function HealthChip({ ok, label }: { ok: boolean; label: string }) {
  return (
    <span className={cn(
      'inline-flex min-h-6 items-center gap-1.5 rounded-full px-2 py-0.5',
      ok ? 'bg-primary/10 text-accentStrong' : 'bg-warning/10 text-warning',
    )}>
      {ok ? <CheckCircle2 className="h-3.5 w-3.5 text-accentStrong" /> : <AlertTriangle className="h-3.5 w-3.5 text-warning" />}
      <span className="whitespace-nowrap">{label}</span>
    </span>
  )
}

function ReadinessTile({ ok, title, detail }: { ok: boolean; title: string; detail: string }) {
  return (
    <div className="rounded-md border border-border bg-background px-3 py-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        {ok ? <CheckCircle2 className="h-4 w-4 text-accentStrong" /> : <AlertTriangle className="h-4 w-4 text-warning" />}
        {title}
      </div>
      <div className="mt-1 line-clamp-2 text-xs text-muted-foreground">{detail}</div>
    </div>
  )
}

function Metric({ label, value, tone }: { label: string; value: number; tone?: 'success' | 'warning' | 'danger' }) {
  return (
    <div className="rounded-lg border border-border bg-card px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn(
        'mt-1 text-lg font-semibold tabular-nums',
        tone === 'success' && 'text-accentStrong',
        tone === 'warning' && 'text-warning',
        tone === 'danger' && 'text-destructive',
      )}>
        {value.toLocaleString()}
      </div>
    </div>
  )
}

function tabCount(counts: Counts, tab: StatusGroup) {
  if (counts.tabs && typeof counts.tabs[tab] === 'number') return counts.tabs[tab]
  if (tab === 'all') return counts.total
  if (tab === 'unpaid') return 0
  if (tab === 'to_ship') return counts.waiting_ship
  if (tab === 'shipping') return counts.shipped
  if (tab === 'completed') return counts.shipped
  if (tab === 'cancelled') return counts.cancelled
  return 0
}

function OrderStatusBadge({ status }: { status: string }) {
  const s = status.toUpperCase()
  return (
    <Badge variant="outline" className={cn(
      'bg-background',
      s === 'CANCELLED' && 'border-destructive/40 bg-destructive/10 text-destructive',
      s === 'SHIPPED' || s === 'COMPLETED' ? 'border-accentStrong/40 bg-primary/10 text-accentStrong' : '',
      s === 'READY_TO_SHIP' || s === 'PROCESSED' ? 'border-info/40 bg-info/10 text-info' : '',
    )}>
      {s || '-'}
    </Badge>
  )
}

function ERPStatusBadge({ status }: { status: string }) {
  return (
    <Badge variant="outline" className={cn(
      'bg-background',
      status === 'sent' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
      status === 'needs_review' && 'border-warning/40 bg-warning/10 text-warning',
      status === 'failed' || status === 'cancelled' ? 'border-destructive/40 bg-destructive/10 text-destructive' : '',
    )}>
      {erpStatusLabel(status)}
    </Badge>
  )
}

function SourceBadge({ source, verify }: { source?: string; verify?: boolean }) {
  const label = verify ? 'Console Verify' : sourceLabel(source)
  return (
    <Badge variant="outline" className={cn(
      'h-5 bg-background text-[10px]',
      source === 'shopee_push' && 'border-info/40 bg-info/10 text-info',
      source === 'shop_auth' && 'border-warning/40 bg-warning/10 text-warning',
      verify && 'border-warning/40 bg-warning/10 text-warning',
    )}>
      {label}
    </Badge>
  )
}

function UpdateSourceBadge({ source }: { source?: string }) {
  const normalized = String(source ?? '').trim()
  if (!normalized || normalized === 'unknown') return null
  return (
    <Badge variant="outline" className={cn(
      'h-5 bg-background px-1.5 text-[10px]',
      normalized === 'push' && 'border-info/40 bg-info/10 text-info',
      normalized === 'sync' && 'border-muted-foreground/30 text-muted-foreground',
      normalized === 'shipping' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
    )}>
      {sourceLabel(normalized)}
    </Badge>
  )
}

function PaymentBreakdownBadge({ status }: { status?: string }) {
  const normalized = String(status ?? '').trim()
  if (!normalized) return null
  const label = normalized === 'ready'
    ? 'Payment ready'
    : normalized === 'failed'
      ? 'Payment error'
      : normalized === 'unavailable'
        ? 'ยังไม่มี escrow'
        : 'รอข้อมูลชำระเงิน'
  return (
    <Badge variant="outline" className={cn(
      'mt-1 h-5 px-1.5 text-[10px]',
      normalized === 'ready' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
      normalized === 'failed' && 'border-destructive/40 bg-destructive/10 text-destructive',
      normalized === 'unavailable' && 'border-warning/40 bg-warning/10 text-warning',
      (normalized === 'queued' || normalized === 'running') && 'border-info/40 bg-info/10 text-info',
    )}>
      {label}
    </Badge>
  )
}

function sourceLabel(source?: string) {
  switch (String(source ?? '').trim()) {
    case 'push':
    case 'shopee_push':
    case 'Push':
      return 'Push'
    case 'sync':
    case 'Sync':
      return 'Sync'
    case 'Shopee':
      return 'Sync'
    case 'shipping':
      return 'Shipping check'
    case 'shop_auth':
      return 'Shop Auth'
    case 'snapshot':
      return 'Snapshot'
    case 'nexflow':
    case 'Nexflow':
      return 'Nexflow'
    case 'Seller Center':
      return 'Seller Center'
    case 'console_verify':
    case 'Shopee Console':
      return 'Console Verify'
    default:
      return 'Unknown'
  }
}

function diagnosticStatusTone(status?: string) {
  switch (String(status ?? '').trim()) {
    case 'done':
    case 'processed':
      return 'border-accentStrong/40 bg-primary/10 text-accentStrong'
    case 'failed':
      return 'border-destructive/40 bg-destructive/10 text-destructive'
    case 'queued':
    case 'running':
      return 'border-info/40 bg-info/10 text-info'
    case 'not_applicable':
      return 'text-muted-foreground'
    default:
      return ''
  }
}

function diagnosticsEventVisible(event: PushEvent, filter: DiagnosticsFilter) {
  if (filter === 'all') return true
  if (filter === 'failed') {
    return event.processing_status === 'failed'
      || event.reconcile_status === 'failed'
      || Boolean(event.error || event.reconcile_error)
  }
  if (filter === 'verify') return Boolean(event.is_verification_event) || event.source === 'console_verify'
  if (filter === 'shop') return event.source === 'shop_auth'
  if (filter === 'order') return event.source === 'shopee_push' && !event.is_verification_event
  return true
}

function erpStatusLabel(status: string) {
  switch (status) {
    case 'blocked': return 'Pending - บล็อก'
    case 'pending': return 'รอสร้างเอกสาร'
    case 'pending_erp': return 'สร้างเอกสารแล้ว รอส่ง SML'
    case 'needs_review': return 'ต้องตรวจ'
    case 'sent': return 'ส่ง SML แล้ว'
    case 'failed': return 'Failed - ไม่สำเร็จ'
    case 'cancelled': return 'Cancelled'
    case 'waiting_shopee': return 'Completed - รอ Shopee'
    default: return status || '-'
  }
}

function documentPath(order: OrderSnapshot) {
  const billID = encodeURIComponent(order.bill_id ?? '')
  switch ((order.document_route || '').toLowerCase()) {
    case 'saleinvoice':
      return `/sale-invoices/${billID}`
    case 'saleorder':
      return `/sales-orders/${billID}`
    default:
      return `/bills/${billID}`
  }
}

function isImportFallbackBill(order: OrderSnapshot) {
  const flow = (order.bill_source_flow || '').toLowerCase()
  return flow === 'shopee_api' || flow === 'shopee_excel'
}

function canChangeDocumentRoute(order: OrderSnapshot) {
  return Boolean(order.bill_id && !order.sml_doc_no && order.erp_status !== 'sent')
}

function cancelSMLStatusDone(status?: string) {
  return status === 'created' || status === 'already_exists'
}

function orderCancelledAfterSML(order: OrderSnapshot) {
  return Boolean(
    order.bill_id
    && order.sml_doc_no
    && order.erp_status === 'sent'
    && (order.order_status === 'CANCELLED' || order.order_status === 'IN_CANCEL'),
  )
}

function shouldShowCancelSMLAction(order: OrderSnapshot) {
  return orderCancelledAfterSML(order)
}

function cancelSMLDisabledReason(order: OrderSnapshot) {
  if (!orderCancelledAfterSML(order)) return 'ใช้ได้เฉพาะ order ที่ยกเลิกหลังส่ง SML แล้ว'
  if (cancelSMLStatusDone(order.sml_cancel_status)) return 'มีเอกสารยกเลิก SML แล้ว'
  if (order.sml_cancel_status === 'creating') return 'กำลังสร้างเอกสารยกเลิก SML อยู่'
  return ''
}

function cancelSMLBadge(order: OrderSnapshot) {
  if (!orderCancelledAfterSML(order) && !order.sml_cancel_status) return null
  if (cancelSMLStatusDone(order.sml_cancel_status)) {
    return (
      <Badge variant="outline" className="mt-1 h-5 border-success/30 bg-success/10 px-1.5 text-[10px] text-success">
        CN {order.sml_cancel_doc_no || 'สร้างแล้ว'}
      </Badge>
    )
  }
  if (order.sml_cancel_status === 'failed') {
    return (
      <Badge variant="outline" className="mt-1 h-5 border-destructive/40 bg-destructive/10 px-1.5 text-[10px] text-destructive" title={order.sml_cancel_error || undefined}>
        CN ล้มเหลว
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="mt-1 h-5 border-destructive/40 bg-destructive/10 px-1.5 text-[10px] text-destructive">
      ต้องสร้างเอกสารยกเลิก SML
    </Badge>
  )
}

function cancelSMLCreateDisabledTitle(preview: CancelSMLDocumentPreview, confirmed: boolean) {
  if (!preview.create_enabled) return 'ยังปิดด้วย ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS'
  if (!preview.can_create) return preview.message || preview.error || 'ยังสร้างไม่ได้'
  if (!confirmed) return 'กรุณาติ๊กยืนยันก่อนสร้าง'
  return undefined
}

function orderKey(order: OrderSnapshot) {
  return `${order.shop_id}:${order.order_sn}`
}

function erpDisabledReason(order: OrderSnapshot) {
  if (order.order_status === 'UNPAID') return 'order ยังไม่ชำระเงิน'
  if (order.order_status === 'CANCELLED' || order.order_status === 'IN_CANCEL') return 'order ถูกยกเลิกแล้ว'
  if (order.bill_id || order.erp_status === 'pending_erp' || order.erp_status === 'sent') {
    return canChangeDocumentRoute(order)
      ? 'สร้างเอกสารแล้ว ถ้า route ผิดให้กดเปลี่ยนเส้นทาง'
      : 'สร้างเอกสารแล้ว เปิดเอกสารเพื่อส่ง SML หรือแก้ไข'
  }
  return ''
}

function bulkCreateDisabledReason(order: OrderSnapshot, readiness: Readiness | null) {
  if (!readiness?.sml.can_create_document) return readiness?.sml.message || 'ยังไม่ได้ตั้งค่า route คำสั่งซื้อ Shopee'
  if (order.bill_id) return 'สร้างเอกสารแล้ว'
  if (order.order_status === 'UNPAID') return 'order ยังไม่ชำระเงิน'
  if (order.order_status === 'CANCELLED' || order.order_status === 'IN_CANCEL') return 'order ถูกยกเลิกแล้ว'
  if (!['', 'pending', 'failed'].includes(order.erp_status || '')) return 'สถานะ ERP ไม่พร้อมสร้างเอกสาร'
  return ''
}

function shipDisabledReason(order: OrderSnapshot) {
  if (shipmentStarted(order)) return 'Shopee มีข้อมูล shipment/tracking แล้ว กดรายละเอียดจัดส่งหรือพิมพ์ใบปะหน้าแทน'
  if (order.erp_status !== 'sent' || !order.sml_doc_no) return 'ต้องส่งเอกสารเข้า SML ให้สำเร็จก่อนจัดส่ง'
  if (!['READY_TO_SHIP', 'PROCESSED'].includes(order.order_status)) return 'Shopee ยังไม่อยู่ในสถานะพร้อมจัดส่ง'
  return ''
}

function shipmentStarted(order: OrderSnapshot | null | undefined) {
  if (!order) return false
  if (String(order.tracking_number ?? '').trim()) return true
  switch (String(order.logistics_status ?? '').trim().toUpperCase()) {
    case 'LOGISTICS_REQUEST_CREATED':
    case 'LOGISTICS_PICKUP_DONE':
    case 'LOGISTICS_DELIVERY_DONE':
    case 'LOGISTICS_DELIVERY_FAILED':
    case 'LOGISTICS_REQUEST_CANCELED':
      return true
    default:
      return false
  }
}

function externalShipment(order: OrderSnapshot) {
  return shipmentStarted(order) && order.ship_action_status !== 'done'
}

function shippingStateLabel(order: OrderSnapshot) {
  if (externalShipment(order)) return 'จัดส่งแล้วจาก Seller Center'
  if (shipmentStarted(order)) return 'จัดส่งแล้ว'
  return order.logistics_status || 'ยังไม่จัดส่ง'
}

function carrierLabel(order: OrderSnapshot | null | undefined) {
  if (!order) return ''
  const checkout = String(order.checkout_shipping_carrier ?? '').trim()
  const carrier = String(order.shipping_carrier ?? '').trim()
  if (checkout && carrier && checkout !== carrier) return `${checkout} / ${carrier}`
  return checkout || carrier
}

function trackingFromOrder(order: OrderSnapshot): TrackingData {
  return {
    order_sn: order.order_sn,
    order_status: order.order_status,
    erp_status: order.erp_status,
    package_number: order.package_number,
    logistics_status: order.logistics_status,
    tracking_number: order.tracking_number,
    shipping_carrier: order.shipping_carrier,
    checkout_carrier: order.checkout_shipping_carrier,
    ship_action_status: order.ship_action_status,
    external_shipment: externalShipment(order),
    timeline: order.shipping_tracking ?? [],
  }
}

function GuardedButton({
  icon,
  label,
  disabledReason,
  onClick,
}: {
  icon: ReactNode
  label: string
  disabledReason: string
  onClick: () => void
}) {
  const disabled = Boolean(disabledReason)
  const button = (
    <Button
      variant="outline"
      size="sm"
      className="h-8 gap-1.5"
      disabled={disabled}
      onClick={(event) => {
        event.stopPropagation()
        onClick()
      }}
      title={disabledReason || undefined}
    >
      {icon}
      {label}
    </Button>
  )
  if (!disabled) return button
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex" onClick={(event) => event.stopPropagation()}>{button}</span>
      </TooltipTrigger>
      <TooltipContent>{disabledReason}</TooltipContent>
    </Tooltip>
  )
}

function ShippingMethodButton({
  active,
  disabled,
  title,
  detail,
  onClick,
}: {
  active: boolean
  disabled: boolean
  title: string
  detail: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        'rounded-md border border-border bg-background px-3 py-2 text-left transition-colors hover:bg-muted/50 disabled:cursor-not-allowed disabled:opacity-50',
        active && 'border-primary bg-primary/10 text-foreground ring-1 ring-primary/50',
      )}
    >
      <div className="text-sm font-medium">{title}</div>
      <div className="mt-1 text-xs text-muted-foreground">{disabled ? 'ไม่พร้อมใช้งาน' : detail}</div>
    </button>
  )
}

function ShippingExtraFields({
  fields,
  values,
  onChange,
}: {
  fields: string[]
  values: Record<string, string>
  onChange: (field: string, value: string) => void
}) {
  if (fields.length === 0) {
    return null
  }
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {fields.map((field) => (
        <label key={field} className="space-y-1 text-sm">
          <span className="text-xs text-muted-foreground">{shippingFieldLabel(field)}</span>
          <Input
            value={values[field] ?? ''}
            onChange={(event) => onChange(field, event.target.value)}
            className="h-9"
            placeholder={shippingFieldLabel(field)}
          />
        </label>
      ))}
    </div>
  )
}

function DropoffSellerCenterPanel({
  orderSN,
  branchCount,
  refreshing,
  onCopyOrder,
  onRefresh,
}: {
  orderSN: string
  branchCount: number
  refreshing: boolean
  onCopyOrder: () => void
  onRefresh: () => void
}) {
  return (
    <div className="space-y-3 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
      <div className="flex items-start gap-2">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="font-medium text-foreground">Dropoff ต้องจัดส่งใน Seller Center</div>
          <p className="text-sm text-muted-foreground">{DROPOFF_LIMITATION_MESSAGE}</p>
          {branchCount > 0 && (
            <p className="text-xs text-muted-foreground">
              Shopee ส่ง branch id กลับมา {branchCount.toLocaleString()} รายการ แต่ไม่มีข้อมูลพอให้เลือกสาขาอย่างปลอดภัย
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            ไปที่ Seller Center &gt; ที่ต้องจัดส่ง &gt; ค้นหา Order SN แล้วจัดส่งหรือพิมพ์ใบปะหน้าจาก Shopee
          </p>
        </div>
      </div>
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
        <Button asChild size="sm" className="h-8 justify-center gap-1.5">
          <a href={SELLER_CENTER_TOSHIP_URL} target="_blank" rel="noreferrer">
            <ExternalLink className="h-3.5 w-3.5" />
            จัดส่งใน Seller Center
          </a>
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 justify-center gap-1.5 bg-background"
          onClick={onCopyOrder}
          disabled={!orderSN}
        >
          <Copy className="h-3.5 w-3.5" />
          คัดลอก Order SN
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 justify-center gap-1.5 bg-background"
          onClick={onRefresh}
          disabled={refreshing}
        >
          {refreshing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
          ตรวจสถานะจาก Shopee อีกครั้ง
        </Button>
      </div>
    </div>
  )
}

function logisticsIDKey(id: LogisticsID | undefined) {
  return id == null ? '' : String(id)
}

async function copyText(value: string, successMessage: string) {
  const text = value.trim()
  if (!text) {
    toast.error('ไม่มีข้อมูลให้คัดลอก')
    return
  }
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.setAttribute('readonly', '')
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      document.execCommand('copy')
      document.body.removeChild(textarea)
    }
    toast.success(successMessage)
  } catch (error) {
    toast.error('คัดลอกไม่สำเร็จ: ' + apiError(error))
  }
}

function shippingMethodAvailable(data: ShippingParameterData | null, method: Exclude<ShippingMethod, ''>) {
  if (!data) return false
  if (method === 'pickup') {
    return (data.info_needed?.pickup?.length ?? 0) > 0 || (data.pickup?.address_list?.length ?? 0) > 0
  }
  if (method === 'dropoff') {
    return (data.info_needed?.dropoff?.length ?? 0) > 0 || (data.dropoff?.branch_list?.length ?? 0) > 0
  }
  return ENABLE_NON_INTEGRATED_SHIPPING_UI && shippingNonIntegratedAvailable(data)
}

function shippingNonIntegratedAvailable(data: ShippingParameterData | null) {
  return (data?.info_needed?.non_integrated?.length ?? 0) > 0
}

function defaultShippingMethod(data: ShippingParameterData): ShippingMethod {
  if (shippingMethodAvailable(data, 'pickup')) return 'pickup'
  if (shippingMethodAvailable(data, 'non_integrated')) return 'non_integrated'
  return ''
}

function defaultShippingExtraValues(data: ShippingParameterData | null, method: ShippingMethod) {
  const fields = method === 'pickup'
    ? extraShippingFields(data?.info_needed?.pickup, ['address_id', 'pickup_time_id'])
    : method === 'dropoff'
      ? extraShippingFields(data?.info_needed?.dropoff, ['branch_id'])
      : method === 'non_integrated'
        ? data?.info_needed?.non_integrated ?? []
        : []
  return fields.reduce<Record<string, string>>((acc, field) => {
    acc[field] = ''
    return acc
  }, {})
}

function extraShippingFields(fields: string[] | undefined, knownFields: string[]) {
  const known = new Set(knownFields)
  return (fields ?? []).map((field) => field.trim()).filter((field) => field && !known.has(field))
}

function shippingSubmitDisabledReason({
  loading,
  error,
  params,
  method,
  selectedPickupAddress,
  selectedPickupTime,
  extraValues,
}: {
  loading: boolean
  error: string
  params: ShippingParameterData | null
  method: ShippingMethod
  selectedPickupAddress?: { address_id: LogisticsID; time_slot_list?: Array<{ pickup_time_id: LogisticsID; date?: number }> }
  selectedPickupTime?: { pickup_time_id: LogisticsID; date?: number }
  extraValues: Record<string, string>
}) {
  if (loading) return 'กำลังตรวจเงื่อนไขจัดส่งจาก Shopee'
  if (error) return 'ต้องตรวจ shipping parameter ให้สำเร็จก่อน'
  if (!params) return 'ยังไม่มีข้อมูล shipping parameter'
  if (!method) {
    if (shippingMethodAvailable(params, 'dropoff')) return DROPOFF_LIMITATION_MESSAGE
    return 'Shopee ยังไม่ส่งวิธีจัดส่งที่พร้อมใช้สำหรับ order นี้'
  }
  if (method === 'pickup') {
    const required = params.info_needed?.pickup ?? []
    if (required.includes('address_id') && !selectedPickupAddress) return 'กรุณาเลือกที่อยู่รับสินค้า'
    if (required.includes('pickup_time_id') && !selectedPickupTime) return 'กรุณาเลือกช่วงเวลารับสินค้า'
    const missing = missingExtraFields(extraShippingFields(required, ['address_id', 'pickup_time_id']), extraValues)
    if (missing) return `กรุณากรอกข้อมูล pickup ให้ครบ: ${missing}`
  }
  if (method === 'dropoff') {
    return DROPOFF_LIMITATION_MESSAGE
  }
  if (method === 'non_integrated') {
    const missing = missingExtraFields(params.info_needed?.non_integrated ?? [], extraValues)
    if (missing) return `กรุณากรอกข้อมูลจัดส่งให้ครบ: ${missing}`
  }
  return ''
}

function missingExtraFields(fields: string[], values: Record<string, string>) {
  return fields.filter((field) => !String(values[field] ?? '').trim()).join(', ')
}

function buildShippingPayload({
  order,
  method,
  params,
  selectedPickupAddress,
  selectedPickupTime,
  extraValues,
}: {
  order: OrderSnapshot
  method: ShippingMethod
  params: ShippingParameterData | null
  selectedPickupAddress?: { address_id: LogisticsID }
  selectedPickupTime?: { pickup_time_id: LogisticsID }
  extraValues: Record<string, string>
}) {
  const payload: Record<string, unknown> = {
    confirm: 'SHIP_ORDER',
    package_number: order.package_number || undefined,
  }
  if (method === 'pickup') {
    payload.pickup = {
      ...pickShippingFields(extraShippingFields(params?.info_needed?.pickup, ['address_id', 'pickup_time_id']), extraValues),
      ...(selectedPickupAddress ? { address_id: selectedPickupAddress.address_id } : {}),
      ...(selectedPickupTime ? { pickup_time_id: selectedPickupTime.pickup_time_id } : {}),
    }
  }
  if (method === 'non_integrated') {
    payload.non_integrated = pickShippingFields(params?.info_needed?.non_integrated ?? [], extraValues)
  }
  return payload
}

function pickShippingFields(fields: string[], values: Record<string, string>) {
  return fields.reduce<Record<string, string>>((acc, field) => {
    const value = String(values[field] ?? '').trim()
    if (value) acc[field] = value
    return acc
  }, {})
}

function shippingFieldLabel(field: string) {
  switch (field) {
    case 'address_id': return 'ที่อยู่รับสินค้า'
    case 'pickup_time_id': return 'ช่วงเวลารับสินค้า'
    case 'branch_id': return 'สาขาที่นำส่ง'
    case 'tracking_number': return 'Tracking number'
    default: return field
  }
}

function formatPickupDate(value?: number) {
  if (!value) return 'ไม่ระบุวัน'
  return dayjs.unix(value).format('DD/MM/YY HH:mm')
}
