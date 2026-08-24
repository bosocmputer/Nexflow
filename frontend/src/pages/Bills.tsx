import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  FileSpreadsheet,
  Filter,
  Info,
  Search,
  Send,
  Store,
  UploadCloud,
  X,
} from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import BillTable from '@/components/BillTable'
import { EmptyState } from '@/components/common/EmptyState'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { archiveBill, deleteBill, restoreBill, useBills } from '@/hooks/useBills'
import { useAuth } from '@/hooks/useAuth'
import client from '@/api/client'
import { BulkSendDialog } from './BulkSendDialog'
import {
  BILL_SOURCE_LABEL,
  BILL_STATUS_LABEL,
  BILL_TYPE_LABEL,
  PAGE_TITLE,
} from '@/lib/labels'
import { cn } from '@/lib/utils'
import { WORK_QUEUE_CHANGED_EVENT } from '@/lib/work-queue-events'
import {
  BILL_INPUT_CHANNEL_OPTIONS,
  billInputChannelSource,
  isBillInputChannel,
  type BillInputChannel,
} from '@/lib/billInputChannel'
import {
  ENABLE_LAZADA_EXCEL,
  ENABLE_SHOPEE_EXCEL,
  ENABLE_TIKTOK_EXCEL,
} from '@/lib/featureFlags'
import type { Bill } from '@/types'

const DEFAULT_PER_PAGE = 20
const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const BULK_BATCH_SIZE = 100
const ALL = '__all__'
type InputChannelFilter = typeof ALL | BillInputChannel
const VALID_INPUT_CHANNELS = [ALL, ...BILL_INPUT_CHANNEL_OPTIONS.map((option) => option.value)]

interface ShopeeShopOption {
  id: string
  shop_id: number
  label: string
  shop_name?: string
  disabled_at?: string
}

// Filter options pull labels from lib/labels.ts so Bills, Dashboard, and
// Logs all show identical status names — no more "ล้มเหลว" vs "ส่ง SML
// ล้มเหลว" drift.
const STATUS_OPTIONS = [
  { value: ALL, label: 'ทุกสถานะ' },
  ...['pending', 'needs_review', 'sent', 'failed', 'skipped'].map((s) => ({
    value: s,
    label: BILL_STATUS_LABEL[s],
  })),
]

// Valid filter values used to validate URL query string against typos.
const VALID_STATUSES = STATUS_OPTIONS.map((o) => o.value)

const ARCHIVE_OPTIONS = [
  { value: 'active', label: 'รายการปกติ' },
  { value: 'include', label: 'รวมบิลที่เก็บแล้ว' },
  { value: 'only', label: 'บิลที่เก็บแล้ว' },
] as const
type ArchiveMode = typeof ARCHIVE_OPTIONS[number]['value']
const QUICK_STATUS_VALUES = [ALL, 'pending', 'needs_review', 'failed']
const QUICK_STATUS_OPTIONS = STATUS_OPTIONS.filter((o) => QUICK_STATUS_VALUES.includes(o.value))
const SECONDARY_STATUS_OPTIONS = STATUS_OPTIONS.filter((o) => !QUICK_STATUS_VALUES.includes(o.value))

type BillsMode = 'sales-order' | 'sale-invoice'

const MODE_CONFIG: Record<BillsMode, {
  title: string
  description: string
  source: string
  billType: 'purchase' | 'sale'
  documentRoute?: string
  destination: string
  docCode: string
  emptyTitle: string
  emptyDescription: string
  emptyActionLabel: string
  emptyActionTo: string
  emptySecondaryLabel?: string
  emptySecondaryTo?: string
  searchPlaceholder: string
}> = {
  'sales-order': {
    title: PAGE_TITLE.salesOrders,
    description: 'คิวเอกสารขายที่ปลายทางเป็นใบสั่งขาย ยังใช้งานได้ครบสำหรับช่องทางที่ตั้งค่าไว้',
    source: '',
    billType: 'sale',
    documentRoute: 'saleorder',
    destination: 'ขาย -> ใบสั่งขาย',
    docCode: 'SR',
    emptyTitle: 'ยังไม่มีใบสั่งขาย',
    emptyDescription: 'นำเข้าไฟล์ Shopee, Lazada หรือ TikTok แล้วเอกสารที่ตั้งปลายทางเป็นใบสั่งขายจะมาอยู่หน้านี้',
    emptyActionLabel: 'นำเข้าไฟล์ Marketplace',
    emptyActionTo: '/import/shopee',
    emptySecondaryLabel: 'ตั้งค่าเส้นทาง SML',
    emptySecondaryTo: '/settings/channels',
    searchPlaceholder: 'ค้นหาเลขบิล / เลขคำสั่งซื้อ / ลูกค้า…',
  },
  'sale-invoice': {
    title: PAGE_TITLE.saleInvoices,
    description: 'เส้นทางใช้งานหลักสำหรับงานขาย Marketplace ตรวจรายการจาก Shopee, Lazada หรือ TikTok แล้วส่งเป็นขายสินค้าและบริการ / SI เข้า SML',
    source: '',
    billType: 'sale',
    documentRoute: 'saleinvoice',
    destination: 'ขาย -> ขายสินค้าและบริการ',
    docCode: 'SI',
    emptyTitle: 'ยังไม่มีเอกสารขายสินค้าและบริการ',
    emptyDescription: 'นำเข้าไฟล์ Shopee, Lazada หรือ TikTok แล้วเลือกปลายทาง SML เป็นขายสินค้าและบริการ เอกสารจะมาอยู่หน้านี้',
    emptyActionLabel: 'นำเข้าไฟล์ Marketplace',
    emptyActionTo: '/import/shopee',
    emptySecondaryLabel: 'ตั้งค่าเส้นทาง SML',
    emptySecondaryTo: '/settings/channels',
    searchPlaceholder: 'ค้นหาเลขบิล / เลขคำสั่งซื้อ / ลูกค้า…',
  },
}

function readURLFilter(params: URLSearchParams, key: string, valid: string[]): string {
  const v = params.get(key) ?? ''
  return v && valid.includes(v) ? v : ALL
}

function readURLArchive(params: URLSearchParams): ArchiveMode {
  const v = params.get('archived')
  return v === 'include' || v === 'only' ? v : 'active'
}

function readURLPage(params: URLSearchParams): number {
  const n = Number(params.get('page'))
  return Number.isInteger(n) && n > 0 ? n : 1
}

function readURLPerPage(params: URLSearchParams): typeof PAGE_SIZE_OPTIONS[number] {
  const n = Number(params.get('per_page'))
  return PAGE_SIZE_OPTIONS.includes(n as typeof PAGE_SIZE_OPTIONS[number])
    ? n as typeof PAGE_SIZE_OPTIONS[number]
    : DEFAULT_PER_PAGE
}

export default function Bills({ mode = 'sales-order' }: { mode?: BillsMode }) {
  const config = MODE_CONFIG[mode]
  const { user } = useAuth()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  // Seed filters from the URL so deep-links/shared links keep the exact queue
  // view, including page and page size.
  const [page, setPage] = useState(() => readURLPage(searchParams))
  const [perPage, setPerPage] = useState<typeof PAGE_SIZE_OPTIONS[number]>(() => readURLPerPage(searchParams))
  const [pageJumpInput, setPageJumpInput] = useState(() => String(readURLPage(searchParams)))
  const [counts, setCounts] = useState({
    needs_review: 0,
    pending: 0,
    sent: 0,
    failed: 0,
    skipped: 0,
    total: 0,
  })
  const [status, setStatus] = useState<string>(() =>
    readURLFilter(searchParams, 'status', VALID_STATUSES),
  )
  const [shopeeShopId, setShopeeShopId] = useState(() => searchParams.get('shopee_shop_id') || ALL)
  const [shopeeShops, setShopeeShops] = useState<ShopeeShopOption[]>([])
  const [search, setSearch] = useState(() => searchParams.get('search') ?? '')
  const [debouncedSearch, setDebouncedSearch] = useState(search)
  const [inputChannel, setInputChannel] = useState<InputChannelFilter>(() =>
    readURLFilter(searchParams, 'input_channel', VALID_INPUT_CHANNELS) as InputChannelFilter,
  )
  const [archiveMode, setArchiveMode] = useState<ArchiveMode>(() => readURLArchive(searchParams))
  const [bulkOpen, setBulkOpen] = useState(false)
  const [confirmAction, setConfirmAction] = useState<{
    kind: 'archive' | 'restore' | 'delete' | 'permanent'
    bill: Bill
  } | null>(null)
  const legacySource = config.source || (searchParams.get('source') ?? '')
  const effectiveSource = inputChannel === ALL ? legacySource : ''
  const showShopeeShopFilter =
    inputChannel === 'shopee' ||
    inputChannel === 'shopee_excel' ||
    (inputChannel === ALL && (!effectiveSource || effectiveSource === 'shopee'))
  const canManageBills = user?.role === 'admin' || user?.role === 'staff'
  const canPermanentDelete = user?.role === 'admin'
  const countsRequestRef = useRef(0)
  const selectedInputChannel = inputChannel === ALL ? '' : inputChannel
  const bulkSource = effectiveSource || billInputChannelSource(selectedInputChannel)

  const { data, loading, refetch } = useBills({
    page,
    per_page: perPage,
    include_total: true,
    status: status === ALL ? '' : status,
    source: effectiveSource,
    input_channel: selectedInputChannel,
    bill_type: config.billType,
    document_route: config.documentRoute,
    sort: mode === 'sale-invoice' ? 'latest_desc' : undefined,
    shopee_shop_id: showShopeeShopFilter && shopeeShopId !== ALL ? shopeeShopId : '',
    search: debouncedSearch,
    archived: archiveMode === 'active' ? '' : archiveMode,
  })
  const bills = data?.data ?? []
  const total = typeof data?.total === 'number' ? data.total : counts.total
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const pageStart = total === 0 ? 0 : (page - 1) * perPage + 1
  const pageEnd = total === 0 ? 0 : Math.min(page * perPage, total)
  const hasPreviousPage = page > 1
  const hasNextPage = page < totalPages
  const bulkCandidateCount = counts.pending
  const bulkStatusAllowed = status === ALL || status === 'pending'
  const bulkDisabled = bulkCandidateCount === 0 || archiveMode !== 'active' || !bulkStatusAllowed
  const bulkButtonLabel =
    archiveMode !== 'active'
      ? 'ส่ง SML ใช้ได้เฉพาะรายการปกติ'
      : !bulkStatusAllowed
        ? 'ส่ง SML ใช้ได้เมื่อดูทุกสถานะ/เอกสารสถานะพร้อมส่ง'
        : bulkCandidateCount > BULK_BATCH_SIZE
          ? `ส่ง SML เอกสารสถานะพร้อมส่งชุดแรก ${BULK_BATCH_SIZE}/${bulkCandidateCount.toLocaleString()} รายการ`
          : `ส่ง SML เอกสารสถานะพร้อมส่ง ${bulkCandidateCount.toLocaleString()} รายการ`
  const bulkCompactLabel = `ส่ง SML ${Math.min(bulkCandidateCount, BULK_BATCH_SIZE).toLocaleString()} ใบ`
  const detailBasePath =
    mode === 'sale-invoice' ? '/sale-invoices' : mode === 'sales-order' ? '/sales-orders' : '/bills'
  const selectedStatusLabel = STATUS_OPTIONS.find((o) => o.value === status)?.label ?? 'สถานะอื่น'
  const selectedArchiveLabel = ARCHIVE_OPTIONS.find((o) => o.value === archiveMode)?.label ?? 'รายการปกติ'
  const secondaryStatusActive = SECONDARY_STATUS_OPTIONS.some((o) => o.value === status)
  const hasActiveFilters =
    status !== ALL ||
    inputChannel !== ALL ||
    shopeeShopId !== ALL ||
    archiveMode !== 'active' ||
    search.trim() !== '' ||
    legacySource !== ''

  const resetPage = (cb: () => void) => {
    cb()
    setPage(1)
  }

  const handleInputChannelChange = (value: string) => {
    if (value !== ALL && !isBillInputChannel(value)) return
    resetPage(() => {
      setInputChannel(value as InputChannelFilter)
      if (value !== ALL && value !== 'shopee' && value !== 'shopee_excel') {
        setShopeeShopId(ALL)
      }
    })
  }

  const clearFilters = () => {
    setStatus(ALL)
    setInputChannel(ALL)
    setShopeeShopId(ALL)
    setSearch('')
    setDebouncedSearch('')
    setArchiveMode('active')
    setPage(1)
    if (legacySource) {
      const next = new URLSearchParams(searchParams)
      next.delete('source')
      setSearchParams(next, { replace: true })
    }
  }

  const refreshAll = () => {
    setPage(1)
    refetch()
    fetchCounts()
  }

  const handlePerPageChange = (value: string) => {
    const next = Number(value)
    if (!PAGE_SIZE_OPTIONS.includes(next as typeof PAGE_SIZE_OPTIONS[number])) return
    setPerPage(next as typeof PAGE_SIZE_OPTIONS[number])
    setPage(1)
  }

  const handleJumpToPage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const next = Number(pageJumpInput)
    if (!Number.isInteger(next) || next < 1) {
      setPageJumpInput(String(page))
      toast.error('เลขหน้าต้องเป็นจำนวนเต็มตั้งแต่ 1 ขึ้นไป')
      return
    }
    setPage(Math.min(next, totalPages))
  }

  const fetchCounts = useCallback(async () => {
    const requestID = ++countsRequestRef.current
    const params = new URLSearchParams()
    if (effectiveSource) params.set('source', effectiveSource)
    if (selectedInputChannel) params.set('input_channel', selectedInputChannel)
    params.set('bill_type', config.billType)
    if (config.documentRoute) params.set('document_route', config.documentRoute)
    if (archiveMode !== 'active') params.set('archived', archiveMode)
    if (showShopeeShopFilter && shopeeShopId !== ALL) params.set('shopee_shop_id', shopeeShopId)
    if (debouncedSearch) params.set('search', debouncedSearch)
    const res = await client.get<typeof counts>(`/api/bills/counts?${params}`)
    if (requestID === countsRequestRef.current) setCounts(res.data)
  }, [effectiveSource, selectedInputChannel, config.billType, config.documentRoute, archiveMode, showShopeeShopFilter, shopeeShopId, debouncedSearch])

  const handleConfirmedAction = async () => {
    if (!confirmAction) return
    const { kind, bill } = confirmAction
    try {
      if (kind === 'archive') {
        await archiveBill(bill.id, 'ผู้ใช้เก็บบิลจากหน้ารายการ')
        toast.success('เก็บบิลแล้ว')
      } else if (kind === 'restore') {
        await restoreBill(bill.id)
        toast.success('กู้คืนบิลแล้ว')
      } else {
        await deleteBill(bill.id)
        toast.success(kind === 'permanent' ? 'ลบถาวรแล้ว' : 'ลบบิลแล้ว')
      }
      setConfirmAction(null)
      refreshAll()
    } catch (err: unknown) {
      const e = err as { response?: { data?: { error?: string } }; message?: string }
      toast.error(e?.response?.data?.error || e?.message || 'ทำรายการไม่สำเร็จ')
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [search])

  useEffect(() => {
    let alive = true
    client.get<{ data: ShopeeShopOption[] }>('/api/shopee-api/connections')
      .then((res) => {
        if (alive) setShopeeShops((res.data.data ?? []).filter((shop) => !shop.disabled_at))
      })
      .catch(() => {
        if (alive) setShopeeShops([])
      })
    return () => { alive = false }
  }, [])

  useEffect(() => {
    fetchCounts().catch(() => {
      setCounts({ needs_review: 0, pending: 0, sent: 0, failed: 0, skipped: 0, total: 0 })
    })
  }, [fetchCounts])

  useEffect(() => {
    const onWorkQueueChanged = () => {
      void refetch()
      fetchCounts().catch(() => {
        setCounts({ needs_review: 0, pending: 0, sent: 0, failed: 0, skipped: 0, total: 0 })
      })
    }
    window.addEventListener(WORK_QUEUE_CHANGED_EVENT, onWorkQueueChanged)
    return () => window.removeEventListener(WORK_QUEUE_CHANGED_EVENT, onWorkQueueChanged)
  }, [refetch, fetchCounts])

  useEffect(() => {
    if (!loading && data && page > totalPages) {
      setPage(totalPages)
    }
  }, [data, loading, page, totalPages])

  useEffect(() => {
    setPageJumpInput(String(page))
  }, [page])

  useEffect(() => {
    const next = new URLSearchParams(searchParams)
    if (status === ALL) next.delete('status')
    else next.set('status', status)
    if (inputChannel === ALL) next.delete('input_channel')
    else {
      next.set('input_channel', inputChannel)
      next.delete('source')
    }
    next.delete('shopee_status')
    if (showShopeeShopFilter && shopeeShopId !== ALL) next.set('shopee_shop_id', shopeeShopId)
    else next.delete('shopee_shop_id')
    if (archiveMode === 'active') next.delete('archived')
    else next.set('archived', archiveMode)
    next.delete('email_account_id')
    if (debouncedSearch) next.set('search', debouncedSearch)
    else next.delete('search')
    if (page > 1) next.set('page', String(page))
    else next.delete('page')
    if (perPage !== DEFAULT_PER_PAGE) next.set('per_page', String(perPage))
    else next.delete('per_page')
    const nextString = next.toString()
    if (nextString !== searchParams.toString()) {
      setSearchParams(next, { replace: true })
    }
  }, [
    status,
    inputChannel,
    archiveMode,
    debouncedSearch,
    page,
    perPage,
    showShopeeShopFilter,
    shopeeShopId,
    searchParams,
    setSearchParams,
  ])

  return (
    <div className="space-y-5">
      <div className="rounded-lg border border-border/70 bg-card p-2.5 shadow-sm">
        <div className="flex flex-col gap-2 xl:flex-row xl:items-start xl:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-lg font-semibold tracking-tight text-foreground">
                {config.title}
              </h1>
              <code className="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-accent-strong">
                {config.docCode}
              </code>
              <p className="sr-only">{config.description}</p>
              <span className="hidden text-xs text-muted-foreground sm:inline">·</span>
              <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
                <Info className="h-3.5 w-3.5 shrink-0 text-accent-strong" />
                <span>Shopee และไฟล์ Marketplace</span>
                <span aria-hidden="true">→</span>
                <span className="font-medium text-foreground">ปลายทาง SML: {config.destination}</span>
              </span>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-1.5 xl:justify-end">
            <QueueMetricChip label="ต้องตรวจ" value={counts.needs_review} tone="warning" />
            <QueueMetricChip label="พร้อมส่ง" value={counts.pending} tone="primary" />
            <QueueMetricChip label="ส่งแล้ว" value={counts.sent} tone="success" />
            <QueueMetricChip label="ไม่สำเร็จ" value={counts.failed} tone="danger" />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="sm"
                  variant={mode === 'sale-invoice' ? 'default' : 'outline'}
                  className="h-8 w-full justify-center gap-1.5 sm:w-auto"
                >
                  <UploadCloud className="h-4 w-4" />
                  นำเข้าไฟล์
                  <ChevronDown className="h-3.5 w-3.5 opacity-70" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-52">
                <DropdownMenuLabel className="text-xs">เลือกไฟล์จากแพลตฟอร์ม</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {ENABLE_SHOPEE_EXCEL && (
                  <DropdownMenuItem asChild>
                    <Link to="/import/shopee">
                      <FileSpreadsheet className="text-[#ee4d2d]" />
                      Shopee Excel
                    </Link>
                  </DropdownMenuItem>
                )}
                {ENABLE_LAZADA_EXCEL && (
                  <DropdownMenuItem asChild>
                    <Link to="/import/lazada">
                      <FileSpreadsheet className="text-[#1d3491]" />
                      Lazada Excel
                    </Link>
                  </DropdownMenuItem>
                )}
                {ENABLE_TIKTOK_EXCEL && (
                  <DropdownMenuItem asChild>
                    <Link to="/import/tiktok">
                      <FileSpreadsheet className="text-foreground" />
                      TikTok Excel
                    </Link>
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button asChild size="sm" variant="outline" className="h-8 w-full justify-center sm:w-auto">
              <Link to="/settings/channels">ตั้งค่าเส้นทาง</Link>
            </Button>
          </div>
        </div>

        <div className="mt-2 space-y-2 border-t border-border/60 pt-2">
          <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
            <div className="relative w-full lg:max-w-[360px] lg:flex-1">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                placeholder={config.searchPlaceholder}
                value={search}
                onChange={(e) => resetPage(() => setSearch(e.target.value))}
                className="h-8 pl-8 text-sm"
              />
            </div>

            <Select value={inputChannel} onValueChange={handleInputChannelChange}>
              <SelectTrigger className="h-8 w-full text-xs lg:w-[205px]" aria-label="กรองตามช่องทางรับข้อมูล">
                <SelectValue placeholder="ทุกช่องทางรับข้อมูล" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>ทุกช่องทางรับข้อมูล</SelectItem>
                {BILL_INPUT_CHANNEL_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    <span className="inline-flex items-center gap-2">
                      {option.excel ? (
                        <FileSpreadsheet className="h-3.5 w-3.5 text-muted-foreground" />
                      ) : (
                        <span className="h-2 w-2 rounded-full bg-[#ee4d2d]" aria-hidden="true" />
                      )}
                      {option.label}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {showShopeeShopFilter && shopeeShops.length > 0 && (
              <Select value={shopeeShopId} onValueChange={(value) => resetPage(() => setShopeeShopId(value))}>
                <SelectTrigger className="h-8 w-full text-xs lg:w-[230px]" aria-label="กรองตามร้าน Shopee">
                  <Store className="mr-2 h-3.5 w-3.5 shrink-0 text-[#9f2f16]" />
                  <SelectValue placeholder="ร้าน Shopee" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>ทุกร้าน Shopee</SelectItem>
                  {shopeeShops.map((shop) => (
                    <SelectItem key={shop.id} value={String(shop.shop_id)}>
                      {shop.label || shop.shop_name || 'Shopee shop'} · {shop.shop_id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant={archiveMode === 'active' ? 'outline' : 'default'}
                  size="sm"
                  className="h-8 w-full justify-between gap-1.5 px-2.5 text-xs lg:w-auto"
                >
                  {selectedArchiveLabel}
                  <ChevronDown className="h-3.5 w-3.5 opacity-60" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-52">
                <DropdownMenuLabel className="text-xs">มุมมองรายการ</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuRadioGroup
                  value={archiveMode}
                  onValueChange={(value) => resetPage(() => setArchiveMode(value as ArchiveMode))}
                >
                  {ARCHIVE_OPTIONS.map((o) => (
                    <DropdownMenuRadioItem key={o.value} value={o.value}>
                      {o.label}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>

            {hasActiveFilters && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8 w-full justify-center gap-1.5 px-2.5 text-xs text-muted-foreground lg:w-auto"
                onClick={clearFilters}
              >
                <X className="h-3.5 w-3.5" />
                ล้างตัวกรอง
              </Button>
            )}
          </div>

          <div className="flex flex-col gap-2 border-t border-border/50 pt-2 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
              {QUICK_STATUS_OPTIONS.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  onClick={() => resetPage(() => setStatus(o.value))}
                  className={cn(
                    'h-7 rounded-full border px-2.5 text-xs font-medium transition-colors',
                    status === o.value
                      ? 'border-primary bg-primary text-primary-foreground'
                      : 'border-border bg-background text-muted-foreground hover:bg-accent/70 hover:text-foreground',
                  )}
                >
                  {o.label}
                </button>
              ))}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    type="button"
                    variant={secondaryStatusActive ? 'default' : 'outline'}
                    size="sm"
                    className="h-7 justify-between gap-1.5 px-2.5 text-xs"
                  >
                    <Filter className="h-3.5 w-3.5" />
                    {secondaryStatusActive ? selectedStatusLabel : 'สถานะอื่น'}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-48">
                  <DropdownMenuLabel className="text-xs">สถานะเอกสาร</DropdownMenuLabel>
                  <DropdownMenuRadioGroup
                    value={status}
                    onValueChange={(value) => resetPage(() => setStatus(value))}
                  >
                    {SECONDARY_STATUS_OPTIONS.map((o) => (
                      <DropdownMenuRadioItem key={o.value} value={o.value}>
                        {o.label}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>

            <Button
              type="button"
              size="sm"
              className="h-8 w-full min-w-0 justify-center gap-1.5 lg:w-auto"
              disabled={bulkDisabled}
              onClick={() => setBulkOpen(true)}
              title={
                archiveMode !== 'active'
                  ? 'ส่ง SML แบบกลุ่มปิดไว้เมื่อดูบิลที่เก็บแล้ว เพื่อไม่ส่งเอกสารย้อนหลังโดยไม่ตั้งใจ'
                  : !bulkStatusAllowed
                    ? 'ส่ง SML แบบกลุ่มส่งเฉพาะเอกสารสถานะพร้อมส่ง จึงเปิดได้เมื่อเลือกทุกสถานะหรือสถานะพร้อมส่ง'
                    : counts.needs_review > 0
                      ? `มีรายการต้องตรวจสินค้า ${counts.needs_review.toLocaleString()} รายการ ปุ่มนี้ส่งเฉพาะเอกสารสถานะพร้อมส่ง`
                      : bulkButtonLabel
              }
            >
              <Send className="h-3.5 w-3.5" />
              <span className="truncate">{bulkCompactLabel}</span>
            </Button>
          </div>

          {counts.needs_review > 0 && archiveMode === 'active' && (
            <div className="text-[11px] text-muted-foreground">
              รายการต้องตรวจสินค้า {counts.needs_review.toLocaleString()} รายการจะไม่ถูกรวมในปุ่มส่ง SML เอกสารสถานะพร้อมส่ง
            </div>
          )}
        </div>
      </div>

      {!loading && bills.length === 0 && !hasActiveFilters && !effectiveSource ? (
        <EmptyState
          icon={UploadCloud}
          title={config.emptyTitle}
          description={config.emptyDescription}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button asChild>
                <Link to={config.emptyActionTo}>
                  <UploadCloud className="h-4 w-4" />
                  {config.emptyActionLabel}
                </Link>
              </Button>
              {config.emptySecondaryLabel && config.emptySecondaryTo && (
                <Button asChild variant="outline">
                  <Link to={config.emptySecondaryTo}>{config.emptySecondaryLabel}</Link>
                </Button>
              )}
            </div>
          }
        />
      ) : (
        <BillTable
          bills={bills}
          loading={loading}
          showShopeeStatusColumn={false}
          canManage={canManageBills}
          canPermanentDelete={canPermanentDelete}
          virtualize={perPage >= 100}
          onArchive={(bill: Bill) => setConfirmAction({ kind: 'archive', bill })}
          onRestore={(bill: Bill) => setConfirmAction({ kind: 'restore', bill })}
          onDelete={(bill: Bill) => setConfirmAction({ kind: 'delete', bill })}
          onPermanentDelete={(bill: Bill) => setConfirmAction({ kind: 'permanent', bill })}
          onRowClick={(id) => navigate(`${detailBasePath}/${id}`)}
        />
      )}

      <div className="flex flex-col gap-2 text-xs text-muted-foreground lg:flex-row lg:items-center lg:justify-between">
        <span>
          {total > 0
            ? `แสดง ${pageStart.toLocaleString()}-${pageEnd.toLocaleString()} จาก ${total.toLocaleString()} รายการ`
            : `แสดง ${bills.length.toLocaleString()} รายการ`}
        </span>
        <div className="flex flex-wrap items-center gap-2 lg:justify-end">
          <label className="inline-flex items-center gap-1.5">
            <span>ต่อหน้า</span>
            <Select
              value={String(perPage)}
              onValueChange={handlePerPageChange}
            >
              <SelectTrigger className="h-8 w-[82px] text-xs" aria-label="จำนวนรายการต่อหน้า">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PAGE_SIZE_OPTIONS.map((size) => (
                  <SelectItem key={size} value={String(size)}>
                    {size}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <Button
            variant="outline"
            size="sm"
            disabled={!hasPreviousPage}
            onClick={() => setPage(1)}
          >
            หน้าแรก
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!hasPreviousPage}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
          >
            <ChevronLeft className="h-3.5 w-3.5" />
            ก่อนหน้า
          </Button>
          <span className="min-w-[92px] text-center tabular-nums">
            หน้า {page.toLocaleString()} / {totalPages.toLocaleString()}
          </span>
          <form className="inline-flex items-center gap-1.5" onSubmit={handleJumpToPage}>
            <span>ไปหน้า</span>
            <Input
              type="number"
              inputMode="numeric"
              min={1}
              max={totalPages}
              value={pageJumpInput}
              onChange={(e) => setPageJumpInput(e.target.value)}
              className="h-8 w-20 px-2 text-center text-xs tabular-nums"
              aria-label="ไปหน้าที่"
            />
            <Button type="submit" variant="outline" size="sm" disabled={totalPages <= 1}>
              ไป
            </Button>
          </form>
          <Button
            variant="outline"
            size="sm"
            disabled={!hasNextPage}
            onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
          >
            ถัดไป
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <BulkSendDialog
        open={bulkOpen}
        onOpenChange={setBulkOpen}
        title={config.title}
        billType={config.billType}
        filters={{
          source: bulkSource,
          input_channel: selectedInputChannel,
          bill_type: config.billType,
          document_route: config.documentRoute,
          shopee_shop_id: showShopeeShopFilter && shopeeShopId !== ALL ? shopeeShopId : '',
          search: debouncedSearch,
        }}
        onDone={() => {
          setPage(1)
          refetch()
          fetchCounts()
        }}
      />

      <ConfirmDialog
        open={confirmAction !== null}
        onOpenChange={(open) => !open && setConfirmAction(null)}
        title={confirmActionTitle(confirmAction)}
        description={confirmActionDescription(confirmAction)}
        confirmLabel={confirmAction?.kind === 'permanent' ? 'ลบถาวร' : confirmAction?.kind === 'delete' ? 'ลบบิล' : confirmAction?.kind === 'restore' ? 'กู้คืน' : 'เก็บบิล'}
        variant={confirmAction?.kind === 'delete' || confirmAction?.kind === 'permanent' ? 'destructive' : 'default'}
        onConfirm={handleConfirmedAction}
      />
    </div>
  )
}

function confirmActionTitle(action: { kind: 'archive' | 'restore' | 'delete' | 'permanent'; bill: Bill } | null) {
  if (!action) return ''
  if (action.kind === 'archive') return 'เก็บบิลออกจากคิวงานประจำ?'
  if (action.kind === 'restore') return 'กู้คืนบิลกลับเข้าคิวงาน?'
  if (action.kind === 'permanent') return 'ลบบิลถาวรจาก Nexflow?'
  return 'ลบบิลที่ยังไม่ได้ส่ง?'
}

function confirmActionDescription(action: { kind: 'archive' | 'restore' | 'delete' | 'permanent'; bill: Bill } | null) {
  if (!action) return ''
  const doc = action.bill.sml_doc_no || action.bill.id.slice(0, 8)
  const order = action.bill.sml_order_id ? `\nOrder อ้างอิง: ${action.bill.sml_order_id}` : ''
  if (action.kind === 'archive') {
    return [
      `เอกสาร: ${doc}${order}`,
      'ผลกระทบ: เอกสารจะถูกซ่อนจากคิวงานประจำและ bulk send จะไม่หยิบไปส่ง',
      'Rollback: ยังค้นย้อนหลังในมุมมองบิลที่เก็บแล้วและกู้คืนกลับมาได้',
    ].join('\n')
  }
  if (action.kind === 'restore') {
    return [
      `เอกสาร: ${doc}${order}`,
      'ผลกระทบ: เอกสารจะกลับมาแสดงในรายการปกติและเข้าข่าย workflow เดิมอีกครั้ง',
      'Rollback: ถ้ากู้คืนผิด สามารถเก็บออกจากคิวงานประจำใหม่ได้',
    ].join('\n')
  }
  if (action.kind === 'permanent') {
    return [
      `เอกสาร: ${doc}${order}`,
      'ผลกระทบ: ลบบิล รายการสินค้า และไฟล์แนบออกจาก Nexflow ถาวร',
      'Rollback: ทำกลับจากหน้าจอนี้ไม่ได้ ต้องอาศัย backup ฐานข้อมูลเท่านั้น',
      'หมายเหตุ: การลบใน Nexflow ไม่ได้ลบเอกสารที่เคยส่งสำเร็จใน SML',
    ].join('\n')
  }
  return [
    `เอกสาร: ${doc}${order}`,
    'ผลกระทบ: ลบบิลที่ยังไม่ได้ส่งเข้า SML พร้อมรายการสินค้าและไฟล์แนบ',
    'Rollback: ทำกลับจากหน้าจอนี้ไม่ได้ หากลบผิดต้องนำเข้าหรือสร้างเอกสารใหม่',
  ].join('\n')
}

function QueueMetricChip({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: 'primary' | 'warning' | 'success' | 'danger'
}) {
  const toneCls = {
    primary: 'border-primary/25 bg-primary/10 text-accent-strong',
    warning: 'border-warning/30 bg-warning/10 text-warning',
    success: 'border-success/25 bg-success/10 text-success',
    danger: 'border-destructive/25 bg-destructive/10 text-destructive',
  }[tone]
  return (
    <span className={cn('inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[11px]', toneCls)}>
      <span className="font-semibold tabular-nums">{value.toLocaleString()}</span>
      <span className="text-foreground/75">{label}</span>
    </span>
  )
}
