import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, CheckCircle2, ChevronDown, ExternalLink, FileText, Loader2, RefreshCw, Search, Settings2 } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'

import client from '@/api/client'
import { DateRangePicker } from '@/components/common/DateRangePicker'
import { MarketplaceOperationsHeader } from '@/components/marketplace/MarketplaceOperationsHeader'
import { MarketplaceOperationsHelp } from '@/components/marketplace/MarketplaceOperationsHelp'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

type Channel = 'all' | 'shopee' | 'tiktok'
type View = 'work' | 'all' | 'cancelled'
type WorkState = 'needs_mapping' | 'needs_review' | 'ready_to_create' | 'ready_to_send' | 'send_failed' | 'cancel_document_needed' | 'cancel_failed' | 'complete'

type MarketplaceRow = {
  source: Exclude<Channel, 'all'>
  shop_id: string
  shop_name: string
  order_id: string
  marketplace_status: string
  bill_id?: string
  bill_status?: string
  sml_doc_no?: string
  cancel_sml_doc_no?: string
  cancellation_status?: string
  auto_sml_status?: string
  currency?: string
  total_amount: string
  item_count: number
  sku_count: number
  last_updated_at: string
  last_synced_at: string
  work_state: WorkState
  available_actions: string[]
  document_path?: string
}

type OperationsResponse = {
  data: MarketplaceRow[]
  next_cursor?: string
  has_more: boolean
  partial_errors?: Array<{ source: string; message: string }>
  channels: Channel[]
}

type SummarySource = {
  source: Exclude<Channel, 'all'>
  enabled: boolean
  total: number
  last_synced_at?: string
  error?: string
}

type Shop = { source: Exclude<Channel, 'all'>; shop_id: string; name: string }
type SummaryResponse = { data: SummarySource[]; shops: Shop[] }

const PAGE_SIZE = 20

const viewTabs: Array<{ value: View; label: string }> = [
  { value: 'work', label: 'งานที่ต้องทำ' },
  { value: 'all', label: 'คำสั่งซื้อทั้งหมด' },
  { value: 'cancelled', label: 'ยกเลิก / คืนสินค้า' },
]

const statusOptions = [
  { value: 'all', label: 'ทุกสถานะ Marketplace' },
  { value: 'UNPAID', label: 'ยังไม่ชำระ' },
  { value: 'COMPLETED', label: 'สำเร็จ' },
  { value: 'CANCELLED', label: 'ยกเลิก' },
]

function text(value: string | undefined, fallback = '—') {
  const normalized = value?.trim()
  return normalized || fallback
}

function money(value: string, currency?: string) {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '—'
  return new Intl.NumberFormat('th-TH', { style: 'currency', currency: currency || 'THB', minimumFractionDigits: 2 }).format(amount)
}

function timestamp(value?: string) {
  if (!value) return 'ยังไม่มีข้อมูล'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'ยังไม่มีข้อมูล'
  return new Intl.DateTimeFormat('th-TH', { dateStyle: 'short', timeStyle: 'short', timeZone: 'Asia/Bangkok' }).format(date)
}

function sourceLabel(source: Channel) {
  return source === 'shopee' ? 'Shopee' : source === 'tiktok' ? 'TikTok' : 'ทุกช่องทาง'
}

function orderStatusLabel(status: string) {
  const labels: Record<string, string> = {
    UNPAID: 'ยังไม่ชำระ', READY_TO_SHIP: 'ต้องจัดส่ง', PROCESSED: 'เตรียมจัดส่งแล้ว', SHIPPED: 'กำลังขนส่ง', COMPLETED: 'สำเร็จ', CANCELLED: 'ยกเลิก', IN_CANCEL: 'กำลังยกเลิก',
    ON_HOLD: 'พักรายการ', AWAITING_SHIPMENT: 'รอจัดส่ง', PARTIALLY_SHIPPING: 'จัดส่งบางส่วน', AWAITING_COLLECTION: 'รอรับพัสดุ', IN_TRANSIT: 'กำลังขนส่ง', DELIVERED: 'จัดส่งแล้ว',
  }
  return labels[status] ?? text(status, 'ไม่ระบุสถานะ')
}

function workStateMeta(row: MarketplaceRow) {
  const states: Record<WorkState, { label: string; detail: string; tone: string }> = {
    needs_mapping: { label: 'รอจับคู่สินค้า', detail: 'ตรวจสินค้า SML ก่อนสร้างเอกสาร', tone: 'border-amber-300 bg-amber-50 text-amber-900' },
    needs_review: { label: 'ต้องตรวจข้อมูล', detail: 'ตรวจความพร้อมก่อนทำรายการ', tone: 'border-amber-300 bg-amber-50 text-amber-900' },
    ready_to_create: { label: 'พร้อมสร้างเอกสาร', detail: 'ยังไม่มี Bill ใน Nexflow', tone: 'border-sky-300 bg-sky-50 text-sky-900' },
    ready_to_send: { label: 'รอส่ง SML', detail: 'มี Bill แล้ว แต่ยังไม่มีเลข SML', tone: 'border-violet-300 bg-violet-50 text-violet-900' },
    send_failed: { label: 'ส่ง SML ไม่สำเร็จ', detail: 'เปิดเอกสารเพื่อตรวจและลองใหม่', tone: 'border-rose-300 bg-rose-50 text-rose-900' },
    cancel_document_needed: { label: 'รอเอกสารหลังยกเลิก', detail: 'มีใบขายเดิมใน SML แล้ว', tone: 'border-rose-300 bg-rose-50 text-rose-900' },
    cancel_failed: { label: 'เอกสารยกเลิกมีปัญหา', detail: 'ตรวจหลักฐานและสถานะ SML', tone: 'border-rose-300 bg-rose-50 text-rose-900' },
    complete: { label: 'ดำเนินการแล้ว', detail: 'ไม่มีงานที่ต้องทำตอนนี้', tone: 'border-emerald-300 bg-emerald-50 text-emerald-900' },
  }
  if (row.work_state === 'cancel_document_needed' || row.work_state === 'cancel_failed') return states[row.work_state]
  if (row.sml_doc_no) return { label: 'ส่ง SML แล้ว', detail: row.sml_doc_no, tone: 'border-emerald-300 bg-emerald-50 text-emerald-900' }
  return states[row.work_state]
}

function legacyActionPath(row: MarketplaceRow) {
  const query = new URLSearchParams({ legacy_action: '1', shop_id: row.shop_id })
  if (row.source === 'shopee') {
    query.set('order', row.order_id)
    if (row.marketplace_status === 'CANCELLED' || row.marketplace_status === 'IN_CANCEL') query.set('status_group', 'cancelled')
    return `/shopee-operations?${query.toString()}`
  }
  query.set('order_id', row.order_id)
  if (row.marketplace_status === 'CANCELLED') query.set('status_group', 'cancelled')
  return `/tiktok-shop-operations?${query.toString()}`
}

export default function MarketplaceOperations() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const [rows, setRows] = useState<MarketplaceRow[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [hasMore, setHasMore] = useState(false)
  const [summary, setSummary] = useState<SummaryResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [partialErrors, setPartialErrors] = useState<Array<{ source: string; message: string }>>([])
  const sequence = useRef(0)
  const activeRequest = useRef<AbortController | null>(null)

  const view = (params.get('view') as View) || 'work'
  const channel = (params.get('channel') as Channel) || 'all'
  const shopID = params.get('shop_id') || 'all'
  const status = params.get('status') || 'all'
  const search = params.get('q') || ''
  const from = params.get('from') || ''
  const to = params.get('to') || ''

  const updateQuery = useCallback((updates: Record<string, string | null>) => {
    setParams((current) => {
      const next = new URLSearchParams(current)
      for (const [key, value] of Object.entries(updates)) {
        if (!value || value === 'all') next.delete(key)
        else next.set(key, value)
      }
      return next
    }, { replace: true })
  }, [setParams])

  const updateDateRange = useCallback((range: { from: string; to: string }) => {
    updateQuery({ from: range.from, to: range.to, cursor: null })
  }, [updateQuery])

  const load = useCallback(async (cursor = '', append = false) => {
    const request = ++sequence.current
    activeRequest.current?.abort()
    const controller = new AbortController()
    activeRequest.current = controller
    append ? setLoadingMore(true) : setLoading(true)
    if (!append) setError('')
    try {
      const response = await client.get<OperationsResponse>('/api/marketplace-operations', {
        params: {
          view, channel: channel === 'all' ? undefined : channel, shop_id: shopID === 'all' ? undefined : shopID,
          status: status === 'all' ? undefined : status, q: search || undefined, from: from || undefined, to: to || undefined,
          limit: PAGE_SIZE, cursor: cursor || undefined,
        },
        signal: controller.signal,
      })
      if (request !== sequence.current) return
      setRows((current) => append ? [...current, ...response.data.data] : response.data.data)
      setNextCursor(response.data.next_cursor || '')
      setHasMore(response.data.has_more)
      setPartialErrors(response.data.partial_errors || [])
    } catch {
      if (request !== sequence.current) return
      if (controller.signal.aborted) return
      setError('โหลดคิว Marketplace ไม่สำเร็จ กรุณาลองใหม่อีกครั้ง')
      if (!append) setRows([])
    } finally {
      if (request === sequence.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [channel, from, search, shopID, status, to, view])

  useEffect(() => { void load() }, [load])
  useEffect(() => () => activeRequest.current?.abort(), [])
  useEffect(() => {
    let mounted = true
    client.get<SummaryResponse>('/api/marketplace-operations/summary')
      .then((response) => { if (mounted) setSummary(response.data) })
      .catch(() => { if (mounted) setSummary(null) })
    return () => { mounted = false }
  }, [])

  const selectedShop = useMemo(() => summary?.shops.find((shop) => shop.source === channel && shop.shop_id === shopID), [channel, shopID, summary?.shops])
  const shopSelection = channel === 'all' || shopID === 'all' ? 'all' : `${channel}:${shopID}`
  const availableSources = (summary?.data ?? []).filter((source) => source.enabled).map((source) => source.source)
  const canOpenSourceTools = channel !== 'all' && shopID !== 'all'
  const health = summary?.data ?? []

  const openSourceTools = (kind: 'auto' | 'diagnostics') => {
    if (!canOpenSourceTools) {
      toast.error(kind === 'auto' ? 'เลือกร้านเดียวก่อนจัดการ Auto SML' : 'เลือกร้านเดียวก่อนตรวจระบบ')
      return
    }
    const target = channel === 'shopee' ? '/shopee-operations' : '/tiktok-shop-operations'
    const query = new URLSearchParams({ legacy_action: '1', shop_id: shopID })
    if (kind === 'diagnostics') query.set('diagnostics', '1')
    navigate(`${target}?${query.toString()}`)
  }

  return (
    <div className="space-y-4 p-0 sm:p-0">
      <MarketplaceOperationsHeader
        titleID="marketplace-operations-title"
        title="คำสั่งซื้อ Marketplace"
        modeLabel="คิวงานประจำวัน"
        modeClassName="bg-primary"
        routeLabel={view === 'cancelled' ? 'เอกสารหลังยกเลิก / รับคืน' : 'Shopee และ TikTok Shop'}
        description="รวมงานที่ต้องตรวจ สร้างเอกสาร และติดตามการส่ง SML ไว้ในที่เดียว โดยเอกสารและการตั้งค่าของแต่ละร้านยังแยกกัน"
        health={
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground" role="status">
            {health.map((source) => (
              <span key={source.source} className="inline-flex items-center gap-1.5">
                <span className={cn('h-2 w-2 rounded-full', source.error ? 'bg-rose-500' : source.source === 'shopee' ? 'bg-[#EE4D2D]' : 'bg-[#111817]')} />
                <strong className="font-medium text-foreground">{sourceLabel(source.source)}</strong>
                <span>{source.error || `${source.total} ออเดอร์ · ซิงก์ล่าสุด ${timestamp(source.last_synced_at)}`}</span>
              </span>
            ))}
          </div>
        }
        actions={
          <>
            <MarketplaceOperationsHelp channel="Marketplace" signalLabel="Webhook" />
            <Button variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => openSourceTools('diagnostics')}><CheckCircle2 className="h-3.5 w-3.5" />ตรวจระบบ</Button>
            <Button variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => openSourceTools('auto')}><Settings2 className="h-3.5 w-3.5" />Auto SML</Button>
            <Button size="sm" className="h-8 gap-1.5" onClick={() => void load()} disabled={loading}><RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />รีเฟรช</Button>
          </>
        }
      />

      {partialErrors.length > 0 && (
        <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>แสดงข้อมูลไม่ครบ</AlertTitle><AlertDescription>{partialErrors.map((entry) => `${sourceLabel(entry.source as Channel)}: ${entry.message}`).join(' · ')}</AlertDescription></Alert>
      )}
      {error && (
        <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>โหลดข้อมูลไม่สำเร็จ</AlertTitle><AlertDescription className="flex items-center justify-between gap-3">{error}<Button size="sm" variant="outline" onClick={() => void load()}>ลองใหม่</Button></AlertDescription></Alert>
      )}

      <Tabs value={view} onValueChange={(value) => updateQuery({ view: value, cursor: null })}>
        <TabsList className="h-9">{viewTabs.map((tab) => <TabsTrigger key={tab.value} value={tab.value} className="px-3 text-xs sm:text-sm">{tab.label}</TabsTrigger>)}</TabsList>
      </Tabs>

      <section className="rounded-lg border bg-card">
        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-[220px] flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input className="h-9 pl-8" placeholder="ค้นหาเลขออเดอร์ ร้าน หรือเลขเอกสาร" value={search} onChange={(event) => updateQuery({ q: event.target.value, cursor: null })} />
          </div>
          <Select value={channel} onValueChange={(value) => updateQuery({ channel: value, shop_id: null, cursor: null })}>
            <SelectTrigger className="h-9 w-[150px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">ทุกช่องทาง</SelectItem>
              {availableSources.includes('shopee') && <SelectItem value="shopee">Shopee</SelectItem>}
              {availableSources.includes('tiktok') && <SelectItem value="tiktok">TikTok</SelectItem>}
            </SelectContent>
          </Select>
          <Select value={shopSelection} onValueChange={(value) => {
            if (value === 'all') {
              updateQuery({ shop_id: null, cursor: null })
              return
            }
            const [source, selectedID] = value.split(':', 2)
            updateQuery({ channel: source, shop_id: selectedID, cursor: null })
          }}>
            <SelectTrigger className="h-9 w-[190px]"><SelectValue placeholder="ทุกร้าน" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">ทุกร้าน</SelectItem>
              {(summary?.shops ?? []).filter((shop) => channel === 'all' || shop.source === channel).map((shop) => <SelectItem key={`${shop.source}:${shop.shop_id}`} value={`${shop.source}:${shop.shop_id}`}>{sourceLabel(shop.source)} · {text(shop.name, shop.shop_id)}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={status} onValueChange={(value) => updateQuery({ status: value, cursor: null })}>
            <SelectTrigger className="h-9 w-[170px]"><SelectValue /></SelectTrigger>
            <SelectContent>{statusOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
          </Select>
          <DateRangePicker
            from={from}
            to={to}
            onFromChange={(value) => updateDateRange({ from: value, to })}
            onToChange={(value) => updateDateRange({ from, to: value })}
            onRangeChange={updateDateRange}
            className="h-9 w-full sm:w-[250px]"
            title="ช่วงเวลาที่อัปเดตคำสั่งซื้อ"
            description="กรองตามเวลาที่ Marketplace แจ้งสถานะล่าสุดให้ Nexflow"
            clearLabel="ล้างช่วงเวลา"
          />
        </div>

        {loading ? <MarketplaceRowsSkeleton /> : rows.length === 0 ? <EmptyMarketplaceQueue view={view} channel={channel} /> : <MarketplaceRows rows={rows} />}

        {(hasMore || loadingMore) && <div className="flex justify-center border-t p-3"><Button variant="outline" size="sm" className="gap-1.5" disabled={loadingMore} onClick={() => void load(nextCursor, true)}>{loadingMore ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ChevronDown className="h-3.5 w-3.5" />}โหลดรายการเพิ่ม</Button></div>}
      </section>
      {selectedShop && <p className="text-xs text-muted-foreground">กำลังดูร้าน {selectedShop.name}; การตั้งค่า Auto SML และตรวจระบบจะทำกับร้านนี้เท่านั้น</p>}
    </div>
  )
}

function MarketplaceRows({ rows }: { rows: MarketplaceRow[] }) {
  return <>
    <div className="hidden overflow-x-auto md:block"><table className="w-full text-sm"><thead className="border-b text-left text-xs text-muted-foreground"><tr><th className="p-3 font-medium">ช่องทาง / คำสั่งซื้อ</th><th className="p-3 font-medium">ยอดรวม</th><th className="p-3 font-medium">สถานะ Marketplace</th><th className="p-3 font-medium">เอกสาร Nexflow / SML</th><th className="p-3 text-right font-medium">จัดการ</th></tr></thead><tbody>{rows.map((row) => <MarketplaceTableRow key={`${row.source}:${row.shop_id}:${row.order_id}`} row={row} />)}</tbody></table></div>
    <div className="divide-y md:hidden">{rows.map((row) => <MarketplaceMobileRow key={`${row.source}:${row.shop_id}:${row.order_id}`} row={row} />)}</div>
  </>
}

function ChannelBadge({ source }: { source: Exclude<Channel, 'all'> }) {
  return <Badge className={cn('h-6 border px-2 text-[11px] text-white', source === 'shopee' ? 'border-[#EE4D2D] bg-[#EE4D2D]' : 'border-[#111817] bg-[#111817]')}>{sourceLabel(source)}</Badge>
}

function MarketplaceTableRow({ row }: { row: MarketplaceRow }) {
  const state = workStateMeta(row)
  return <tr className="border-b last:border-0"><td className="p-3 align-top"><div className="flex items-start gap-2"><ChannelBadge source={row.source} /><div className="min-w-0"><p className="font-medium">{row.order_id}</p><p className="mt-0.5 max-w-[24rem] truncate text-xs text-muted-foreground">{text(row.shop_name, row.shop_id)} · อัปเดต {timestamp(row.last_updated_at)}</p></div></div></td><td className="p-3 align-top"><p className="font-medium">{money(row.total_amount, row.currency)}</p><p className="mt-0.5 text-xs text-muted-foreground">{row.item_count} รายการ · {row.sku_count} SKU</p></td><td className="p-3 align-top"><p className="font-medium">{orderStatusLabel(row.marketplace_status)}</p><p className="mt-0.5 text-xs text-muted-foreground">{state.label}</p></td><td className="p-3 align-top"><DocumentCell row={row} state={state} /></td><td className="p-3 text-right align-top"><RowAction row={row} /></td></tr>
}

function MarketplaceMobileRow({ row }: { row: MarketplaceRow }) {
  const state = workStateMeta(row)
  return <article className="space-y-3 p-4"><div className="flex items-start gap-2"><ChannelBadge source={row.source} /><div className="min-w-0"><p className="font-medium">{row.order_id}</p><p className="mt-0.5 truncate text-xs text-muted-foreground">{text(row.shop_name, row.shop_id)} · {timestamp(row.last_updated_at)}</p></div></div><div className="grid grid-cols-2 gap-3 text-sm"><div><p className="font-medium">{money(row.total_amount, row.currency)}</p><p className="text-xs text-muted-foreground">{row.item_count} รายการ · {row.sku_count} SKU</p></div><div><p className="font-medium">{orderStatusLabel(row.marketplace_status)}</p><p className="text-xs text-muted-foreground">{state.label}</p></div></div><div className="flex items-end justify-between gap-3"><DocumentCell row={row} state={state} /><RowAction row={row} /></div></article>
}

function DocumentCell({ row, state }: { row: MarketplaceRow; state: ReturnType<typeof workStateMeta> }) {
  const tone = state.tone
  const detail = row.work_state === 'cancel_document_needed' || row.work_state === 'cancel_failed'
    ? [row.sml_doc_no, row.cancel_sml_doc_no].filter(Boolean).join(' → ') || state.detail
    : state.detail
  return <div className="min-w-0"><Badge variant="outline" className={cn('max-w-full truncate font-medium', tone)}>{state.label}</Badge><p className="mt-1 max-w-[18rem] truncate text-xs text-muted-foreground">{detail}</p></div>
}

function RowAction({ row }: { row: MarketplaceRow }) {
  if (row.work_state === 'cancel_document_needed' || row.work_state === 'cancel_failed') {
    return <Button asChild size="sm" className="h-8 gap-1.5"><Link to={legacyActionPath(row)}>ตรวจยกเลิก<ExternalLink className="h-3.5 w-3.5" /></Link></Button>
  }
  if (row.document_path) return <Button asChild size="sm" variant="outline" className="h-8 gap-1.5"><Link to={row.document_path}><FileText className="h-3.5 w-3.5" />เอกสาร</Link></Button>
  if (row.work_state === 'complete') return <Button asChild size="sm" variant="outline" className="h-8 gap-1.5"><Link to={legacyActionPath(row)}>รายละเอียด<ExternalLink className="h-3.5 w-3.5" /></Link></Button>
  return <Button asChild size="sm" className="h-8 gap-1.5"><Link to={legacyActionPath(row)}>ดำเนินการ<ExternalLink className="h-3.5 w-3.5" /></Link></Button>
}

function MarketplaceRowsSkeleton() {
  return <div className="space-y-3 p-4">{Array.from({ length: 6 }, (_, index) => <div key={index} className="h-14 animate-pulse rounded-md bg-muted" />)}</div>
}

function EmptyMarketplaceQueue({ view, channel }: { view: View; channel: Channel }) {
  const title = view === 'work' ? 'ไม่มีงานที่ต้องทำในตอนนี้' : view === 'cancelled' ? 'ยังไม่มีรายการยกเลิกหรือรับคืน' : `ยังไม่มีคำสั่งซื้อ${channel === 'all' ? '' : `จาก ${sourceLabel(channel)}`}`
  const detail = view === 'work' ? 'ออเดอร์ที่ต้องตรวจ สร้างเอกสาร หรือส่ง SML จะแสดงที่นี่' : 'ลองเปลี่ยนตัวกรองหรือกดรีเฟรชเพื่อดูข้อมูลล่าสุด'
  return <div className="px-4 py-12 text-center"><CheckCircle2 className="mx-auto h-8 w-8 text-emerald-600" /><h2 className="mt-3 text-sm font-semibold">{title}</h2><p className="mx-auto mt-1 max-w-md text-xs leading-5 text-muted-foreground">{detail}</p></div>
}
