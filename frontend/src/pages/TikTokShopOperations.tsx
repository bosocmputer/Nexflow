import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { AlertTriangle, CheckCircle2, Clock3, Loader2, RefreshCw, Search, Store } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

import client from '@/api/client'
import { PageHeader } from '@/components/common/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { formatTikTokMoney, tiktokOrderStatusLabel, tiktokSyncState } from '@/lib/tiktok-shop-operations'
import { cn } from '@/lib/utils'

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
}

interface TikTokOrderSyncSetting {
  shop_id: string
  shop_name: string
  enabled: boolean
  interval_seconds: number
  watermark_update_at?: string
  next_run_at: string
  last_success_at?: string
  last_error_code: string
  last_error_message: string
}

interface TikTokOrderSyncResponse {
  worker_enabled: boolean
  data: TikTokOrderSyncSetting[]
}

const PAGE_SIZE = 20
const STATUSES = ['UNPAID', 'ON_HOLD', 'AWAITING_SHIPMENT', 'PARTIALLY_SHIPPING', 'AWAITING_COLLECTION', 'IN_TRANSIT', 'DELIVERED', 'COMPLETED', 'CANCELLED']

export default function TikTokShopOperations() {
  const [params, setParams] = useSearchParams()
  const [draft, setDraft] = useState(params.get('order_id') ?? '')
  const [orders, setOrders] = useState<TikTokOrderPage | null>(null)
  const [sync, setSync] = useState<TikTokOrderSyncResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshTick, setRefreshTick] = useState(0)
  const page = Math.max(1, Number(params.get('page') || '1') || 1)
  const shopID = params.get('shop_id') ?? ''
  const status = params.get('status') ?? ''
  const orderID = params.get('order_id') ?? ''

  const setQuery = useCallback((next: Record<string, string | number | null>) => {
    setParams((current) => {
      const updated = new URLSearchParams(current)
      Object.entries(next).forEach(([key, value]) => value === null || value === '' ? updated.delete(key) : updated.set(key, String(value)))
      return updated
    })
  }, [setParams])

  useEffect(() => setDraft(orderID), [orderID])

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    Promise.all([
      client.get<TikTokOrderPage>('/api/tiktok-shop-api/orders', { params: { shop_id: shopID || undefined, status: status || undefined, order_id: orderID || undefined, page, page_size: PAGE_SIZE } }),
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
  }, [orderID, page, refreshTick, shopID, status])

  const selectedSetting = useMemo(() => {
    const settings = sync?.data ?? []
    return shopID ? settings.find((item) => item.shop_id === shopID) : settings[0]
  }, [shopID, sync?.data])
  const syncState = selectedSetting ? tiktokSyncState(Boolean(sync?.worker_enabled), selectedSetting.enabled, selectedSetting.last_error_code) : 'shop_disabled'

  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    const value = draft.trim()
    if (value && !/^\d{1,32}$/.test(value)) {
      setError('Order ID ต้องเป็นตัวเลขไม่เกิน 32 หลัก')
      return
    }
    setQuery({ order_id: value || null, page: 1 })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="คำสั่งซื้อ TikTok Shop"
        description="รายการ Snapshot จากการซิงก์อัตโนมัติ ใช้ตรวจข้อมูลจริงแบบ read-only และยังไม่สร้าง Bill หรือเอกสาร SML"
        actions={<Button type="button" variant="outline" size="sm" className="gap-2" disabled={loading} onClick={() => setRefreshTick((value) => value + 1)}><RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />รีเฟรช</Button>}
      />

      {error && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>โหลดข้อมูลไม่สำเร็จ</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}

      <Card className="border-border/70 shadow-none">
        <CardContent className="flex flex-col gap-3 p-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            {syncState === 'active' ? <CheckCircle2 className="mt-0.5 h-5 w-5 shrink-0 text-success" /> : <Clock3 className="mt-0.5 h-5 w-5 shrink-0 text-warning" />}
            <div className="min-w-0">
              <p className="text-sm font-semibold">{syncStateLabel(syncState)}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                {selectedSetting ? `${selectedSetting.shop_name || selectedSetting.shop_id} · ทุก ${formatInterval(selectedSetting.interval_seconds)} · สำเร็จล่าสุด ${formatDateTime(selectedSetting.last_success_at)}` : 'ยังไม่พบร้านที่เชื่อมต่อ'}
              </p>
              {selectedSetting?.last_error_message && <p className="mt-1 text-xs text-destructive">{selectedSetting.last_error_message}</p>}
            </div>
          </div>
          <Badge variant="outline" className="w-fit">Snapshot เท่านั้น · ไม่สร้าง Bill/SML</Badge>
        </CardContent>
      </Card>

      <div className="grid gap-2 lg:grid-cols-[minmax(220px,1fr)_220px_220px_auto]">
        <form className="relative" onSubmit={submitSearch}>
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={draft} onChange={(event) => setDraft(event.target.value)} placeholder="ค้นหา Order ID" inputMode="numeric" className="h-9 pl-9 pr-20" />
          <Button type="submit" size="sm" variant="ghost" className="absolute right-1 top-1 h-7">ค้นหา</Button>
        </form>
        <Select value={shopID || 'all'} onValueChange={(value) => setQuery({ shop_id: value === 'all' ? null : value, page: 1 })}>
          <SelectTrigger className="h-9"><SelectValue placeholder="ทุกร้าน" /></SelectTrigger>
          <SelectContent><SelectItem value="all">ทุกร้าน</SelectItem>{(sync?.data ?? []).map((item) => <SelectItem key={item.shop_id} value={item.shop_id}>{item.shop_name || item.shop_id}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={status || 'all'} onValueChange={(value) => setQuery({ status: value === 'all' ? null : value, page: 1 })}>
          <SelectTrigger className="h-9"><SelectValue placeholder="ทุกสถานะ" /></SelectTrigger>
          <SelectContent><SelectItem value="all">ทุกสถานะ</SelectItem>{STATUSES.map((item) => <SelectItem key={item} value={item}>{tiktokOrderStatusLabel(item)}</SelectItem>)}</SelectContent>
        </Select>
        {(shopID || status || orderID) && <Button type="button" variant="outline" size="sm" onClick={() => { setDraft(''); setParams({}) }}>ล้างตัวกรอง</Button>}
      </div>

      <div className="hidden overflow-x-auto rounded-lg border border-border/80 bg-card md:block">
        <table className="w-full min-w-[920px] text-sm">
          <thead><tr className="border-b bg-muted/55 text-left text-[11px] font-semibold text-muted-foreground"><th className="px-4 py-3">Order / ร้าน</th><th className="px-4 py-3">สถานะ</th><th className="px-4 py-3 text-right">สินค้า</th><th className="px-4 py-3 text-right">ยอดที่ผู้ซื้อชำระ</th><th className="px-4 py-3">รายละเอียดยอด</th><th className="px-4 py-3">อัปเดต / ซิงก์</th></tr></thead>
          <tbody>{loading ? <LoadingRows /> : (orders?.data ?? []).length === 0 ? <tr><td colSpan={6} className="px-4 py-12 text-center text-muted-foreground">ยังไม่พบ Snapshot ตามตัวกรองนี้</td></tr> : orders?.data.map((row) => <DesktopRow key={`${row.shop_id}:${row.order_id}`} row={row} />)}</tbody>
        </table>
      </div>

      <div className="space-y-2 md:hidden">
        {loading ? <div className="flex items-center justify-center gap-2 rounded-lg border py-10 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังโหลด...</div> : (orders?.data ?? []).length === 0 ? <div className="rounded-lg border py-10 text-center text-sm text-muted-foreground">ยังไม่พบ Snapshot ตามตัวกรองนี้</div> : orders?.data.map((row) => <MobileRow key={`${row.shop_id}:${row.order_id}`} row={row} />)}
      </div>

      <div className="flex flex-col gap-2 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
        <span>ทั้งหมด {orders?.total_items ?? 0} ออเดอร์ · หน้า {orders?.page ?? page} / {Math.max(1, orders?.total_pages ?? 1)}</span>
        <div className="flex gap-2"><Button variant="outline" size="sm" disabled={loading || page <= 1} onClick={() => setQuery({ page: page - 1 })}>ก่อนหน้า</Button><Button variant="outline" size="sm" disabled={loading || page >= Math.max(1, orders?.total_pages ?? 1)} onClick={() => setQuery({ page: page + 1 })}>ถัดไป</Button></div>
      </div>
    </div>
  )
}

function DesktopRow({ row }: { row: TikTokOrderRow }) {
  return <tr className="border-b border-border/60 last:border-0"><td className="px-4 py-3"><p className="font-mono text-xs font-semibold">{row.order_id}</p><p className="mt-1 text-xs text-muted-foreground">{row.shop_name || row.shop_id}</p></td><td className="px-4 py-3"><Badge variant="outline">{tiktokOrderStatusLabel(row.order_status)}</Badge></td><td className="px-4 py-3 text-right tabular-nums">{row.item_count} รายการ<p className="text-xs text-muted-foreground">{row.sku_count} SKU</p></td><td className="px-4 py-3 text-right font-semibold tabular-nums">{formatTikTokMoney(row.payment_total_amount, row.currency)}</td><td className="px-4 py-3 text-xs text-muted-foreground"><p>สินค้า {formatTikTokMoney(row.product_subtotal_amount, row.currency)}</p><p>จัดส่ง {formatTikTokMoney(row.shipping_fee_amount, row.currency)} · คุ้มครอง {formatTikTokMoney(row.item_insurance_fee_amount, row.currency)}</p></td><td className="px-4 py-3 text-xs text-muted-foreground"><p>TikTok {formatDateTime(row.last_order_update_at)}</p><p>Nexflow {formatDateTime(row.last_synced_at)}</p></td></tr>
}

function MobileRow({ row }: { row: TikTokOrderRow }) {
  return <Card className="shadow-none"><CardContent className="space-y-3 p-4"><div className="flex items-start justify-between gap-2"><div className="min-w-0"><p className="break-all font-mono text-xs font-semibold">{row.order_id}</p><p className="mt-1 flex items-center gap-1 text-xs text-muted-foreground"><Store className="h-3 w-3" />{row.shop_name || row.shop_id}</p></div><Badge variant="outline" className="shrink-0">{tiktokOrderStatusLabel(row.order_status)}</Badge></div><div className="flex items-end justify-between gap-3 border-t pt-3"><div className="text-xs text-muted-foreground"><p>{row.item_count} รายการ · {row.sku_count} SKU</p><p className="mt-1">ซิงก์ {formatDateTime(row.last_synced_at)}</p></div><div className="text-right"><p className="text-xs text-muted-foreground">ยอดที่ผู้ซื้อชำระ</p><p className="font-semibold tabular-nums">{formatTikTokMoney(row.payment_total_amount, row.currency)}</p></div></div><p className="text-xs text-muted-foreground">สินค้า {formatTikTokMoney(row.product_subtotal_amount, row.currency)} · จัดส่ง {formatTikTokMoney(row.shipping_fee_amount, row.currency)} · คุ้มครอง {formatTikTokMoney(row.item_insurance_fee_amount, row.currency)}</p></CardContent></Card>
}

function LoadingRows() { return <>{Array.from({ length: 4 }).map((_, index) => <tr key={index} className="border-b"><td colSpan={6} className="px-4 py-4"><div className="h-4 w-2/3 animate-pulse rounded bg-muted" /></td></tr>)}</> }
function formatDateTime(value?: string) { if (!value) return 'ยังไม่มี'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' }) }
function formatInterval(seconds: number) { return seconds % 60 === 0 ? `${seconds / 60} นาที` : `${seconds} วินาที` }
function syncStateLabel(state: ReturnType<typeof tiktokSyncState>) { return state === 'active' ? 'ซิงก์อัตโนมัติทำงาน' : state === 'error' ? 'ซิงก์ล่าสุดมีปัญหา' : state === 'server_disabled' ? 'Worker บน Server ยังปิดอยู่' : 'ร้านนี้ยังไม่ได้เปิดซิงก์อัตโนมัติ' }
function apiErrorMessage(cause: unknown, fallback: string) { const data = (cause as { response?: { data?: { error?: { message?: string } | string } } })?.response?.data; return typeof data?.error === 'string' ? data.error : data?.error?.message || fallback }
