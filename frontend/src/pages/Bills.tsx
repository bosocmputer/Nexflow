import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { AlertTriangle, CheckCircle2, ChevronLeft, ChevronRight, Clock, Info, Mail, Search, Send, Settings, Store, UploadCloud } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
import type { Bill } from '@/types'

const DEFAULT_PER_PAGE = 20
const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const BULK_BATCH_SIZE = 100
const ALL = '__all__'

interface InboxOption {
  id: string
  name: string
  username: string
}

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

const SHOPEE_STATUS_OPTIONS = [
  { value: ALL, label: 'ทุกสถานะคำสั่งซื้อ' },
  { value: 'shipped', label: 'ถูกจัดส่งแล้ว' },
  { value: 'payment_confirmed', label: 'ยืนยันการชำระเงินแล้ว' },
  { value: 'ready_to_ship', label: 'เตรียมจัดส่ง' },
  { value: 'picked_up', label: 'คนขับเข้ารับ' },
  { value: 'delivered', label: 'จัดส่งสำเร็จ' },
  { value: 'cancelled', label: 'ยกเลิก' },
  { value: 'refund', label: 'คืนเงิน' },
  { value: 'return', label: 'คืนสินค้า' },
]

const VALID_SHOPEE_STATUSES = SHOPEE_STATUS_OPTIONS.map((o) => o.value)
const ARCHIVE_OPTIONS = [
  { value: 'active', label: 'รายการปกติ' },
  { value: 'include', label: 'รวมบิลที่เก็บแล้ว' },
  { value: 'only', label: 'บิลที่เก็บแล้ว' },
] as const
type ArchiveMode = typeof ARCHIVE_OPTIONS[number]['value']

type BillsMode = 'purchase-order' | 'sales-order' | 'sale-invoice'

const MODE_CONFIG: Record<BillsMode, {
  title: string
  description: string
  source: string
  sourceLabel?: string
  billType: 'purchase' | 'sale'
  documentRoute?: string
  destination: string
  docCode: string
  routeLabel: string
  routeTo: string
  emptyTitle: string
  emptyDescription: string
  emptyActionLabel: string
  emptyActionTo: string
  emptySecondaryLabel?: string
  emptySecondaryTo?: string
  searchPlaceholder: string
}> = {
  'purchase-order': {
    title: PAGE_TITLE.bills,
    description: 'ตรวจข้อมูลจากกล่องอีเมลรับบิลที่ตั้งค่าไว้ แล้วสร้างเป็นใบสั่งซื้อเพื่อส่งเข้า SML',
    source: 'shopee_shipped',
    billType: 'purchase',
    destination: 'ซื้อ -> ใบสั่งซื้อ',
    docCode: 'PO',
    routeLabel: 'กล่องอีเมลรับบิล',
    routeTo: '/settings/email',
    emptyTitle: 'ยังไม่มีใบสั่งซื้อ',
    emptyDescription: 'เมื่อ Nexflow อ่านอีเมลรับบิลจากกล่องที่ตั้งค่าไว้ เอกสารซื้อจะเข้าคิวที่นี่ให้ตรวจสินค้าและส่งเข้า SML',
    emptyActionLabel: 'ไปตั้งค่ากล่องอีเมล',
    emptyActionTo: '/settings/email',
    emptySecondaryLabel: 'ตรวจหน้าเริ่มต้นใช้งาน',
    emptySecondaryTo: '/setup',
    searchPlaceholder: 'ค้นหาเลขบิล / เลขคำสั่งซื้อ / ผู้ขาย…',
  },
  'sales-order': {
    title: PAGE_TITLE.salesOrders,
    description: 'ตรวจข้อมูลจาก Marketplace Excel ที่นำเข้า แล้วสร้างเป็นใบสั่งขายเพื่อส่งเข้า SML',
    source: '',
    sourceLabel: 'Marketplace Excel',
    billType: 'sale',
    documentRoute: 'saleorder',
    destination: 'ขาย -> ใบสั่งขาย',
    docCode: 'SR',
    routeLabel: 'Marketplace Excel',
    routeTo: '/import/shopee',
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
    description: 'ตรวจข้อมูลจาก Marketplace Excel ที่นำเข้า แล้วสร้างเป็นเอกสารขายสินค้าและบริการเพื่อส่งเข้า SML',
    source: '',
    sourceLabel: 'Marketplace Excel',
    billType: 'sale',
    documentRoute: 'saleinvoice',
    destination: 'ขาย -> ขายสินค้าและบริการ',
    docCode: 'SI',
    routeLabel: 'Marketplace Excel',
    routeTo: '/import/shopee',
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

export default function Bills({ mode = 'purchase-order' }: { mode?: BillsMode }) {
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
  const [shopeeStatus, setShopeeStatus] = useState<string>(() =>
    readURLFilter(searchParams, 'shopee_status', VALID_SHOPEE_STATUSES),
  )
  const [shopeeShopId, setShopeeShopId] = useState(() => searchParams.get('shopee_shop_id') || ALL)
  const [emailAccountId, setEmailAccountId] = useState(() => searchParams.get('email_account_id') || ALL)
  const [inboxes, setInboxes] = useState<InboxOption[]>([])
  const [shopeeShops, setShopeeShops] = useState<ShopeeShopOption[]>([])
  const [search, setSearch] = useState(() => searchParams.get('search') ?? '')
  const [archiveMode, setArchiveMode] = useState<ArchiveMode>(() => readURLArchive(searchParams))
  const [bulkOpen, setBulkOpen] = useState(false)
  const [confirmAction, setConfirmAction] = useState<{
    kind: 'archive' | 'restore' | 'delete' | 'permanent'
    bill: Bill
  } | null>(null)
  const showShopeeStatusFilter = mode === 'purchase-order'
  const showShopeeShopFilter = mode !== 'purchase-order'
  const canManageBills = user?.role === 'admin' || user?.role === 'staff'
  const canPermanentDelete = user?.role === 'admin'

  const { data, loading, refetch } = useBills({
    page,
    per_page: perPage,
    include_total: true,
    status: status === ALL ? '' : status,
    shopee_status: showShopeeStatusFilter && shopeeStatus !== ALL ? shopeeStatus : '',
    source: config.source,
    bill_type: config.billType,
    document_route: config.documentRoute,
    email_account_id: emailAccountId === ALL ? '' : emailAccountId,
    shopee_shop_id: showShopeeShopFilter && shopeeShopId !== ALL ? shopeeShopId : '',
    search,
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
  const detailBasePath =
    mode === 'sale-invoice' ? '/sale-invoices' : mode === 'sales-order' ? '/sales-orders' : '/bills'

  const resetPage = (cb: () => void) => {
    cb()
    setPage(1)
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

  const fetchCounts = async () => {
    const params = new URLSearchParams()
    if (config.source) params.set('source', config.source)
    params.set('bill_type', config.billType)
    if (config.documentRoute) params.set('document_route', config.documentRoute)
    if (emailAccountId !== ALL) params.set('email_account_id', emailAccountId)
    if (archiveMode !== 'active') params.set('archived', archiveMode)
    if (showShopeeStatusFilter && shopeeStatus !== ALL) params.set('shopee_status', shopeeStatus)
    if (showShopeeShopFilter && shopeeShopId !== ALL) params.set('shopee_shop_id', shopeeShopId)
    if (search) params.set('search', search)
    const res = await client.get<typeof counts>(`/api/bills/counts?${params}`)
    setCounts(res.data)
  }

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
    let alive = true
    client.get<{ data: InboxOption[] }>('/api/settings/imap-accounts')
      .then((res) => {
        if (alive) setInboxes(res.data.data ?? [])
      })
      .catch(() => {
        if (alive) setInboxes([])
      })
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config.source, config.billType, config.documentRoute, emailAccountId, archiveMode, shopeeStatus, shopeeShopId, search])

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
    if (showShopeeStatusFilter && shopeeStatus !== ALL) next.set('shopee_status', shopeeStatus)
    else next.delete('shopee_status')
    if (showShopeeShopFilter && shopeeShopId !== ALL) next.set('shopee_shop_id', shopeeShopId)
    else next.delete('shopee_shop_id')
    if (archiveMode === 'active') next.delete('archived')
    else next.set('archived', archiveMode)
    if (emailAccountId === ALL) next.delete('email_account_id')
    else next.set('email_account_id', emailAccountId)
    if (search.trim()) next.set('search', search)
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
    shopeeStatus,
    archiveMode,
    emailAccountId,
    search,
    page,
    perPage,
    showShopeeStatusFilter,
    showShopeeShopFilter,
    shopeeShopId,
    searchParams,
    setSearchParams,
  ])

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
        <QueueMetric label="ต้องตรวจสินค้า" value={counts.needs_review} icon={AlertTriangle} tone="warning" />
        <QueueMetric label="เอกสารสถานะพร้อมส่ง" value={counts.pending} icon={Clock} tone="primary" />
        <QueueMetric label="ส่งแล้ว" value={counts.sent} icon={CheckCircle2} tone="success" />
        <QueueMetric label="ส่งไม่สำเร็จ" value={counts.failed} icon={Send} tone="danger" />
      </div>

      <div className="rounded-xl border border-border/70 bg-card p-3 shadow-sm">
        <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <Info className="h-3.5 w-3.5 shrink-0 text-primary" />
          <Link to={config.routeTo} className="font-medium text-primary hover:underline">
            {config.routeLabel}
          </Link>
          <span>→</span>
          <span className="font-medium text-foreground">{config.destination}</span>
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground">
            {config.docCode}
          </code>
          <Link
            to="/settings/channels"
            className="font-medium text-primary hover:underline sm:ml-auto"
          >
            ตั้งค่าเส้นทาง
          </Link>
        </div>

        <div className="mb-2 flex flex-wrap gap-1.5">
          {STATUS_OPTIONS.map((o) => (
            <button
              key={o.value}
              type="button"
              onClick={() => resetPage(() => setStatus(o.value))}
              className={[
                'rounded-full border px-2.5 py-1 text-xs font-medium transition-colors',
                status === o.value
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border bg-background text-muted-foreground hover:bg-accent/70 hover:text-foreground',
              ].join(' ')}
            >
              {o.label}
            </button>
          ))}
        </div>

        <div className="mb-2 flex flex-wrap gap-1.5">
          {ARCHIVE_OPTIONS.map((o) => (
            <button
              key={o.value}
              type="button"
              onClick={() => resetPage(() => setArchiveMode(o.value))}
              className={[
                'rounded-full border px-2.5 py-1 text-xs font-medium transition-colors',
                archiveMode === o.value
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border bg-background text-muted-foreground hover:bg-accent/70 hover:text-foreground',
              ].join(' ')}
            >
              {o.label}
            </button>
          ))}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative w-full max-w-sm">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder={config.searchPlaceholder}
              value={search}
              onChange={(e) => resetPage(() => setSearch(e.target.value))}
              className="h-9 pl-8"
            />
          </div>

          {mode !== 'purchase-order' && (
            <span className="w-full rounded-md border border-border bg-background px-2.5 py-2 text-xs text-muted-foreground sm:w-auto">
              {(config.sourceLabel ?? BILL_SOURCE_LABEL[config.source])} · {BILL_TYPE_LABEL[config.billType]}
            </span>
          )}
          {showShopeeStatusFilter && (
            <select
              value={shopeeStatus}
              onChange={(e) => resetPage(() => setShopeeStatus(e.target.value))}
              className="h-9 w-full min-w-0 rounded-md border border-border bg-background px-2.5 text-xs text-foreground sm:w-auto"
              aria-label="กรองตามสถานะคำสั่งซื้อ Shopee"
            >
              {SHOPEE_STATUS_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          )}
          {showShopeeShopFilter && shopeeShops.length > 0 && (
            <label className="flex w-full items-center gap-1.5 rounded-md border border-border bg-background px-2 text-xs text-muted-foreground sm:w-auto">
              <Store className="h-3.5 w-3.5 text-primary" />
              <select
                value={shopeeShopId}
                onChange={(e) => resetPage(() => setShopeeShopId(e.target.value))}
                className="h-9 min-w-0 bg-transparent text-xs text-foreground outline-none"
                aria-label="กรองตามร้าน Shopee"
              >
                <option value={ALL}>ทุกร้าน Shopee</option>
                {shopeeShops.map((shop) => (
                  <option key={shop.id} value={String(shop.shop_id)}>
                    {shop.label || shop.shop_name || 'Shopee shop'} · {shop.shop_id}
                  </option>
                ))}
              </select>
            </label>
          )}
          {inboxes.length > 0 && config.routeTo === '/settings/email' && (
            <select
              value={emailAccountId}
              onChange={(e) => resetPage(() => setEmailAccountId(e.target.value))}
              className="h-9 w-full min-w-0 rounded-md border border-border bg-background px-2.5 text-xs text-foreground sm:w-auto"
              aria-label="กรองตามกล่องอีเมล"
            >
              <option value={ALL}>ทุกกล่องอีเมล</option>
              {inboxes.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name} · {a.username}
                </option>
              ))}
            </select>
          )}
          <Button
            type="button"
            size="sm"
            className="w-full min-w-0 justify-center gap-1.5 sm:ml-auto sm:w-auto"
            disabled={bulkDisabled}
            onClick={() => setBulkOpen(true)}
            title={
              archiveMode !== 'active'
                ? 'Bulk send ปิดไว้เมื่อดูบิลที่เก็บแล้ว เพื่อไม่ส่งเอกสารย้อนหลังโดยไม่ตั้งใจ'
                : !bulkStatusAllowed
                  ? 'Bulk send ส่งเฉพาะเอกสารสถานะพร้อมส่ง จึงเปิดได้เมื่อเลือกทุกสถานะหรือสถานะพร้อมส่ง'
                : counts.needs_review > 0
                  ? `มีรายการต้องตรวจสินค้า ${counts.needs_review.toLocaleString()} รายการ ปุ่มนี้ส่งเฉพาะเอกสารสถานะพร้อมส่ง`
                  : undefined
            }
          >
            <Send className="h-3.5 w-3.5" />
            <span className="truncate">{bulkButtonLabel}</span>
          </Button>
        </div>
        {counts.needs_review > 0 && archiveMode === 'active' && (
          <div className="mt-2 text-[11px] text-muted-foreground">
            รายการต้องตรวจสินค้า {counts.needs_review.toLocaleString()} รายการจะไม่ถูกรวมในปุ่มส่ง SML เอกสารสถานะพร้อมส่ง
          </div>
        )}
      </div>

      {!loading && bills.length === 0 && !search && status === ALL && shopeeStatus === ALL && archiveMode === 'active' ? (
        <EmptyState
          icon={mode === 'purchase-order' ? Mail : UploadCloud}
          title={config.emptyTitle}
          description={config.emptyDescription}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button asChild>
                <Link to={config.emptyActionTo}>
                  {mode === 'purchase-order' ? <Settings className="h-4 w-4" /> : <UploadCloud className="h-4 w-4" />}
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
          showShopeeStatusColumn={showShopeeStatusFilter}
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
            <select
              value={String(perPage)}
              onChange={(e) => handlePerPageChange(e.target.value)}
              className="h-8 rounded-md border border-border bg-background px-2 text-xs text-foreground"
              aria-label="จำนวนรายการต่อหน้า"
            >
              {PAGE_SIZE_OPTIONS.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
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
          source: config.source,
          bill_type: config.billType,
          document_route: config.documentRoute,
          email_account_id: emailAccountId === ALL ? '' : emailAccountId,
          shopee_status: showShopeeStatusFilter && shopeeStatus !== ALL ? shopeeStatus : '',
          shopee_shop_id: showShopeeShopFilter && shopeeShopId !== ALL ? shopeeShopId : '',
          search,
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
  if (action.kind === 'archive') return 'เก็บบิลนี้?'
  if (action.kind === 'restore') return 'กู้คืนบิลนี้?'
  if (action.kind === 'permanent') return 'ลบถาวร?'
  return 'ลบบิลนี้?'
}

function confirmActionDescription(action: { kind: 'archive' | 'restore' | 'delete' | 'permanent'; bill: Bill } | null) {
  if (!action) return ''
  const doc = action.bill.sml_doc_no || action.bill.id.slice(0, 8)
  if (action.kind === 'archive') return `เก็บบิล ${doc} ออกจากหน้างานประจำ แต่ยังค้นย้อนหลังและดู logs ได้`
  if (action.kind === 'restore') return `นำบิล ${doc} กลับมาแสดงในรายการปกติ`
  if (action.kind === 'permanent') return `ลบบิล ${doc} และไฟล์แนบถาวร คืนไม่ได้ แต่ logs จะยังเก็บข้อมูลสำคัญไว้`
  return `ลบบิล ${doc} ที่ยังไม่ได้ส่งเข้า SML พร้อมรายการสินค้าและไฟล์แนบ`
}

function QueueMetric({
  label,
  value,
  icon: Icon,
  tone,
}: {
  label: string
  value: number
  icon: typeof AlertTriangle
  tone: 'primary' | 'warning' | 'success' | 'danger'
}) {
  const toneCls = {
    primary: 'bg-primary/10 text-primary',
    warning: 'bg-warning/10 text-warning',
    success: 'bg-success/10 text-success',
    danger: 'bg-destructive/10 text-destructive',
  }[tone]
  return (
    <div className="flex items-center gap-3 rounded-xl border border-border/70 bg-card p-3 shadow-sm">
      <div className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${toneCls}`}>
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        <p className="text-xl font-semibold leading-6 tabular-nums">{value.toLocaleString()}</p>
        <p className="text-xs text-muted-foreground">{label}</p>
      </div>
    </div>
  )
}
