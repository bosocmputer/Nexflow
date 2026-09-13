import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Loader2,
  RadioTower,
  RefreshCw,
  Search,
} from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

import client from '@/api/client'
import {
  TikTokBillShadowButton,
  TikTokBillShadowDialog,
  type TikTokBillShadowItem,
  type TikTokBillShadowPreview,
} from '@/components/tiktok/TikTokBillShadowDialog'
import { TikTokProductMappingDialog } from '@/components/tiktok/TikTokProductMappingDialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  formatTikTokMoney,
  normalizeTikTokStatusGroup,
  tiktokOrderStatusLabel,
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

export default function TikTokShopOperations() {
  const canManage = useAuthStore((state) => state.user?.role === 'admin')
  const [params, setParams] = useSearchParams()
  const [orders, setOrders] = useState<TikTokOrderPage | null>(null)
  const [sync, setSync] = useState<TikTokOrderSyncResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshTick, setRefreshTick] = useState(0)
  const [previewOrder, setPreviewOrder] = useState<TikTokOrderRow | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [billPreview, setBillPreview] = useState<TikTokBillShadowPreview | null>(null)
  const [billPreviewLoading, setBillPreviewLoading] = useState(false)
  const [billPreviewError, setBillPreviewError] = useState('')
  const [mappingItem, setMappingItem] = useState<TikTokBillShadowItem | null>(null)
  const [mappingOpen, setMappingOpen] = useState(false)
  const page = readPage(params)
  const perPage = readPerPage(params)
  const statusGroup = normalizeTikTokStatusGroup(params.get('status_group'))
  const shopID = params.get('shop_id') ?? ALL
  const status = params.get('status') ?? ALL
  const orderID = params.get('order_id') ?? ''
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
      if (active) setBillPreviewError(apiErrorMessage(cause, 'ตรวจตัวอย่าง Bill TikTok Shop ไม่สำเร็จ'))
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
    setPreviewOpen(true)
  }
  const setBillPreviewOpen = (open: boolean) => {
    setPreviewOpen(open)
    if (!open) setBillPreviewLoading(false)
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
              <h1 id="tiktok-operations-title" className="text-lg font-semibold tracking-normal">คำสั่งซื้อ TikTok Shop</h1>
              <Badge className="h-6 border-foreground bg-foreground px-2 text-[11px] text-background hover:bg-foreground">Snapshot</Badge>
              <span className="inline-flex h-6 items-center rounded-full border border-border bg-background px-2 text-xs text-muted-foreground">
                อ่านอย่างเดียว · ยังไม่สร้าง Bill/SML
              </span>
            </div>
            <p className="max-w-3xl text-xs leading-5 text-muted-foreground">
              ติดตามข้อมูลคำสั่งซื้อล่าสุดที่ Nexflow ซิงก์จาก TikTok Shop สำหรับตรวจสอบสถานะและยอดชำระ
            </p>
            <TikTokOperationsHealthLine state={syncState} setting={selectedSetting} />
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
          </div>
        </div>
      </section>

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>โหลดข้อมูลไม่สำเร็จ</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
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
                <th className="px-3 py-2 text-left">สถานะ TikTok Shop</th>
                <th className="px-3 py-2 text-left">เอกสาร Nexflow / SML</th>
                <th className="px-3 py-2 text-left">รายละเอียดยอด</th>
                <th className="px-3 py-2 text-left">ซิงก์ล่าสุด</th>
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
              {!loading && (orders?.data ?? []).length === 0 && <EmptyRow />}
              {!loading && orders?.data.map((row) => (
                <DesktopRow
                  key={`${row.shop_id}:${row.order_id}`}
                  row={row}
                  previewLoading={billPreviewLoading && previewOrder?.shop_id === row.shop_id && previewOrder?.order_id === row.order_id}
                  onPreview={() => openBillPreview(row)}
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

      <TikTokBillShadowDialog
        open={previewOpen}
        orderID={previewOrder?.order_id ?? ''}
        shopName={previewOrder?.shop_name ?? ''}
        loading={billPreviewLoading}
        error={billPreviewError}
        preview={billPreview}
        canManage={canManage}
        onMapItem={openProductMapping}
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
    </div>
  )
}

function TikTokOperationsHealthLine({ state, setting }: { state: ReturnType<typeof tiktokSyncState>; setting?: TikTokOrderSyncSetting }) {
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
    </div>
  )
}

function DesktopRow({ row, previewLoading, onPreview }: { row: TikTokOrderRow; previewLoading: boolean; onPreview: () => void }) {
  return (
    <tr className="border-t border-border hover:bg-muted/30">
      <td className="px-3 py-2 align-top">
        <div className="font-mono text-xs font-medium text-foreground">{row.order_id}</div>
        <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
          <span>{row.shop_name || row.shop_id}</span>
          <span aria-hidden="true">·</span>
          <span>{formatDateTime(row.last_order_update_at)}</span>
        </div>
      </td>
      <td className="px-3 py-2 text-right align-top tabular-nums">
        <div className="font-medium">{formatTikTokMoney(row.payment_total_amount, row.currency)}</div>
        <div className="mt-0.5 text-xs text-muted-foreground">
          {row.item_count.toLocaleString()} รายการ · {row.sku_count.toLocaleString()} SKU
        </div>
      </td>
      <td className="px-3 py-2 align-top"><OrderStatusBadge status={row.order_status} /></td>
      <td className="px-3 py-2 align-top">
        <TikTokBillShadowButton loading={previewLoading} onClick={onPreview} />
        <div className="mt-1 text-[11px] text-muted-foreground">Shadow เท่านั้น · ยังไม่สร้างเอกสาร</div>
      </td>
      <td className="px-3 py-2 align-top text-xs text-muted-foreground">
        <div>สินค้า {formatTikTokMoney(row.product_subtotal_amount, row.currency)}</div>
        <div className="mt-0.5">
          จัดส่ง {formatTikTokMoney(row.shipping_fee_amount, row.currency)} · คุ้มครอง {formatTikTokMoney(row.item_insurance_fee_amount, row.currency)}
        </div>
      </td>
      <td className="px-3 py-2 align-top text-xs text-muted-foreground">{formatDateTime(row.last_synced_at)}</td>
    </tr>
  )
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

function EmptyRow() {
  return (
    <tr>
      <td colSpan={6} className="px-3 py-8">
        <div className="mx-auto max-w-lg text-center">
          <RadioTower className="mx-auto mb-2 h-8 w-8 text-muted-foreground" />
          <div className="font-medium">ยังไม่มี order ในคิวนี้</div>
          <div className="mt-1 text-sm text-muted-foreground">รอรอบซิงก์อัตโนมัติ หรือปรับตัวกรองเพื่อดู Snapshot ที่มีอยู่</div>
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
