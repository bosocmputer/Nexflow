import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  Boxes,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleOff,
  Info,
  Loader2,
  PackageCheck,
  RefreshCw,
  Search,
  Warehouse,
} from 'lucide-react'

import client from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/store/auth'

interface TikTokShopConnection {
  gateway_connection_id: string
  shop_id: string
  shop_name: string
  shop_code: string
  granted_scopes: string[]
  disabled: boolean
}

interface TikTokCatalogInventory {
  warehouse_id: string
  available_quantity: number
  committed_quantity: number
}

interface TikTokCatalogItem {
  shop_id: string
  shop_name: string
  product_id: string
  product_title: string
  product_status: string
  sku_id: string
  seller_sku: string
  price: {
    currency?: string
    tax_exclusive_price?: string
    sale_price?: string
  }
  total_available_quantity: number
  total_committed_quantity: number
  inventory: TikTokCatalogInventory[]
  last_seen_at: string
}

interface TikTokCatalogRun {
  id: string
  shop_id: string
  status: 'running' | 'succeeded' | 'failed'
  page_count: number
  product_count: number
  sku_count: number
  warehouse_count: number
  error_code?: string
  started_at?: string
  finished_at?: string
}

interface TikTokCatalogPage {
  data: TikTokCatalogItem[]
  page: number
  page_size: number
  total_items: number
  total_pages: number
  latest_run?: TikTokCatalogRun
}

const PAGE_SIZE = 50

export default function TikTokShopStock() {
  const canManage = useAuthStore((state) => state.user?.role === 'admin')
  const [connections, setConnections] = useState<TikTokShopConnection[]>([])
  const [shopID, setShopID] = useState('')
  const [catalog, setCatalog] = useState<TikTokCatalogPage | null>(null)
  const [query, setQuery] = useState('')
  const [appliedQuery, setAppliedQuery] = useState('')
  const [status, setStatus] = useState('ALL')
  const [page, setPage] = useState(1)
  const [connectionsLoading, setConnectionsLoading] = useState(true)
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  const selectedConnection = useMemo(
    () => connections.find((connection) => connection.shop_id === shopID),
    [connections, shopID],
  )
  const productBasicReady = Boolean(selectedConnection?.granted_scopes.includes('seller.product.basic'))
  const productModifyReady = Boolean(selectedConnection?.granted_scopes.includes('seller.product.write'))

  const fetchCatalog = useCallback(async (selectedShopID: string, selectedPage: number, search: string, selectedStatus: string) => {
    if (!selectedShopID) {
      setCatalog(null)
      return
    }
    const response = await client.get<TikTokCatalogPage>('/api/tiktok-shop-api/products', {
      params: {
        shop_id: selectedShopID,
        page: selectedPage,
        page_size: PAGE_SIZE,
        q: search || undefined,
        status: selectedStatus === 'ALL' ? undefined : selectedStatus,
      },
    })
    setCatalog(response.data)
  }, [])

  const fetchConnections = useCallback(async (preferredShopID: string) => {
    const response = await client.get<{ data: TikTokShopConnection[] }>('/api/tiktok-shop-api/connections')
    const active = (response.data.data ?? []).filter((connection) => !connection.disabled)
    setConnections(active)
    const selected = active.some((connection) => connection.shop_id === preferredShopID) ? preferredShopID : active[0]?.shop_id ?? ''
    setShopID(selected)
    return selected
  }, [])

  const loadCatalogView = useCallback(async (selectedShopID: string, selectedPage: number, search: string, selectedStatus: string) => {
    setCatalogLoading(true)
    setError('')
    try {
      await fetchCatalog(selectedShopID, selectedPage, search, selectedStatus)
    } catch (cause: unknown) {
      setError(apiErrorMessage(cause, 'โหลดข้อมูลสต๊อก TikTok Shop ไม่สำเร็จ'))
    } finally {
      setCatalogLoading(false)
    }
  }, [fetchCatalog])

  useEffect(() => {
    let active = true
    setConnectionsLoading(true)
    setError('')
    void fetchConnections('')
      .catch((cause: unknown) => {
        if (active) setError(apiErrorMessage(cause, 'โหลดร้าน TikTok Shop ไม่สำเร็จ'))
      })
      .finally(() => {
        if (active) setConnectionsLoading(false)
      })
    return () => { active = false }
  }, [fetchConnections])

  useEffect(() => {
    void loadCatalogView(shopID, page, appliedQuery, status)
  }, [appliedQuery, loadCatalogView, page, shopID, status])

  const refreshPage = async () => {
    setConnectionsLoading(true)
    setError('')
    try {
      const selected = await fetchConnections(shopID)
      await loadCatalogView(selected, page, appliedQuery, status)
    } catch (cause: unknown) {
      setError(apiErrorMessage(cause, 'รีเฟรชข้อมูลสต๊อก TikTok Shop ไม่สำเร็จ'))
    } finally {
      setConnectionsLoading(false)
    }
  }

  const loading = connectionsLoading || catalogLoading

  const changeShop = (value: string) => {
    setShopID(value)
    setPage(1)
    setCatalog(null)
  }

  const searchCatalog = (event: FormEvent) => {
    event.preventDefault()
    setPage(1)
    setAppliedQuery(query.trim())
  }

  const syncCatalog = async () => {
    if (!shopID || !productBasicReady || !canManage) return
    setSyncing(true)
    setError('')
    setSuccess('')
    try {
      const response = await client.post<{ data: TikTokCatalogRun }>(
        '/api/tiktok-shop-api/products/catalog-sync',
        { shop_id: shopID },
        { timeout: 180000 },
      )
      const run = response.data.data
      setSuccess(`อัปเดตรายการสำเร็จ ${formatNumber(run.product_count)} สินค้า · ${formatNumber(run.sku_count)} SKU`)
      if (page === 1) {
        await fetchCatalog(shopID, 1, appliedQuery, status)
      } else {
        setPage(1)
      }
    } catch (cause: unknown) {
      setError(apiErrorMessage(cause, 'อัปเดตรายการสินค้าจาก TikTok Shop ไม่สำเร็จ'))
    } finally {
      setSyncing(false)
    }
  }

  const latestRun = catalog?.latest_run
  const setupSteps = [
    { label: 'สิทธิ์ Product Basic', done: productBasicReady, detail: productBasicReady ? 'พร้อมอ่านสินค้าและสต๊อก' : 'ต้องอนุมัติใน Partner Center และเชื่อมร้านใหม่' },
    { label: 'Product Catalog', done: latestRun?.status === 'succeeded', detail: latestRun?.status === 'succeeded' ? `ล่าสุด ${formatDateTime(latestRun.finished_at)}` : 'ยังไม่มี snapshot ที่สำเร็จ' },
    { label: 'จับคู่ Product Master', done: false, detail: 'ตรวจและจับคู่ในเมนูจับคู่สินค้า Marketplace' },
    { label: 'Dry-run สต๊อก', done: false, detail: 'ยังไม่เปิดจนกว่า Catalog UAT จะผ่าน' },
  ]

  return (
    <div className="space-y-3 p-3 sm:p-4">
      <header className="flex items-start justify-between gap-3 sm:items-center">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-xl font-semibold text-foreground">ซิงก์สต๊อก TikTok Shop</h1>
            <Badge variant="outline" className="h-6 border-warning/50 bg-warning/10 text-foreground">Catalog UAT · อ่านอย่างเดียว</Badge>
            {!canManage && <Badge variant="secondary" className="h-6">ดูอย่างเดียว</Badge>}
          </div>
          <p className="mt-0.5 text-sm text-muted-foreground">ตรวจสินค้า SKU และสต๊อกที่ TikTok ส่งมาก่อนเปิดการคำนวณและส่งสต๊อกจริง</p>
        </div>
        <Button variant="outline" size="icon" onClick={() => void refreshPage()} disabled={loading || syncing} aria-label="รีเฟรชข้อมูลหน้านี้">
          <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
        </Button>
      </header>

      <Alert>
        <Info className="h-4 w-4" />
        <AlertTitle>ยังไม่มีการเขียนสต๊อกไป TikTok Shop</AlertTitle>
        <AlertDescription>หน้านี้ใช้ตรวจ Product Catalog และ Inventory snapshot เท่านั้น ปุ่มตรวจสต๊อกและซิงก์จริงจะเปิดหลัง Product Basic UAT, การจับคู่สินค้า และ exact read-back ผ่านครบ</AlertDescription>
      </Alert>

      {error && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>ดำเนินการไม่สำเร็จ</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      {success && <Alert><CheckCircle2 className="h-4 w-4 text-success" /><AlertTitle>อัปเดตรายการแล้ว</AlertTitle><AlertDescription>{success}</AlertDescription></Alert>}

      <section className="overflow-hidden rounded-md border bg-card" aria-label="ตั้งค่าและควบคุม Product Catalog TikTok Shop">
        <div className="grid gap-3 px-3 py-3 sm:grid-cols-[minmax(220px,1fr)_auto] sm:items-end">
          <div className="space-y-1.5">
            <Label htmlFor="tiktok-stock-shop">ร้าน TikTok Shop</Label>
            <Select value={shopID || undefined} onValueChange={changeShop}>
              <SelectTrigger id="tiktok-stock-shop"><SelectValue placeholder="เลือกร้าน TikTok Shop" /></SelectTrigger>
              <SelectContent>
                {connections.map((connection) => (
                  <SelectItem key={connection.shop_id} value={connection.shop_id}>{connection.shop_name || connection.shop_code || connection.shop_id}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button variant="outline" onClick={syncCatalog} disabled={!canManage || !shopID || !productBasicReady || syncing}>
            {syncing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Boxes className="h-4 w-4" />}
            {syncing ? 'กำลังอัปเดตจาก TikTok Shop...' : 'อัปเดตรายการสินค้าจาก TikTok Shop'}
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t px-3 py-2" aria-label="สถานะการเตรียมซิงก์สต๊อก">
          <span className="text-xs font-medium text-muted-foreground">สถานะ:</span>
          {setupSteps.map((step) => (
            <span key={step.label} className={cn('inline-flex items-center gap-1.5 text-xs', step.done ? 'text-foreground' : 'text-muted-foreground')} title={step.detail}>
              {step.done ? <CheckCircle2 className="h-3.5 w-3.5 text-success" /> : <span className="h-2 w-2 rounded-full border" />}
              <span className="font-medium">{step.label}</span>
            </span>
          ))}
        </div>
      </section>

      {selectedConnection && !productBasicReady && (
        <Alert variant="destructive">
          <CircleOff className="h-4 w-4" />
          <AlertTitle>ยังไม่มีสิทธิ์ Product Basic</AlertTitle>
          <AlertDescription>ต้องเปิด Product Basic ให้แอปและเชื่อมร้าน AOY ใหม่ก่อนจึงจะอัปเดตรายการสินค้าได้</AlertDescription>
        </Alert>
      )}

      <section className="overflow-hidden rounded-md border bg-card" aria-label="รายการสินค้า TikTok Shop">
        <div className="flex flex-col gap-3 border-b p-3 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <div className="flex items-center gap-2"><PackageCheck className="h-4 w-4 text-accent-strong" /><h2 className="font-semibold">สินค้าและตัวเลือกจาก TikTok Shop</h2></div>
            <p className="mt-1 text-xs text-muted-foreground">
              {latestRun?.status === 'succeeded'
                ? `Snapshot ${formatNumber(latestRun.product_count)} สินค้า · ${formatNumber(latestRun.sku_count)} SKU · ${formatNumber(latestRun.warehouse_count)} คลัง/รายการ`
                : 'ยังไม่มี Product Catalog snapshot ที่สำเร็จ'}
            </p>
          </div>
          <form className="flex flex-col gap-2 sm:flex-row" onSubmit={searchCatalog}>
            <Select value={status} onValueChange={(value) => { setStatus(value); setPage(1) }}>
              <SelectTrigger className="w-full sm:w-[190px]" aria-label="กรองสถานะสินค้า"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="ALL">ทุกสถานะ</SelectItem>
                <SelectItem value="ACTIVATE">กำลังขาย</SelectItem>
                <SelectItem value="DRAFT">แบบร่าง</SelectItem>
                <SelectItem value="PENDING">รอตรวจ</SelectItem>
                <SelectItem value="FAILED">ไม่ผ่าน</SelectItem>
                <SelectItem value="SELLER_DEACTIVATED">ผู้ขายปิด</SelectItem>
                <SelectItem value="PLATFORM_DEACTIVATED">แพลตฟอร์มปิด</SelectItem>
              </SelectContent>
            </Select>
            <div className="flex min-w-0">
              <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ชื่อสินค้า, Seller SKU หรือ ID" className="rounded-r-none sm:w-[280px]" maxLength={100} />
              <Button type="submit" variant="outline" className="rounded-l-none border-l-0" aria-label="ค้นหาสินค้า"><Search className="h-4 w-4" /></Button>
            </div>
          </form>
        </div>

        {loading && !catalog ? (
          <div className="flex min-h-52 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังโหลดสินค้า...</div>
        ) : !catalog?.data.length ? (
          <div className="p-8 text-center text-sm text-muted-foreground">{appliedQuery ? `ไม่พบสินค้าที่ตรงกับ “${appliedQuery}”` : 'ยังไม่มีสินค้า กดอัปเดตรายการสินค้าจาก TikTok Shop เมื่อ Product Basic พร้อม'}</div>
        ) : (
          <>
            <div className="hidden md:block">
              <Table>
                <TableHeader><TableRow><TableHead>สินค้า / SKU</TableHead><TableHead>สถานะ</TableHead><TableHead className="text-right">TikTok พร้อมขาย</TableHead><TableHead className="text-right">จองแล้ว</TableHead><TableHead>คลัง TikTok</TableHead><TableHead>อัปเดตล่าสุด</TableHead></TableRow></TableHeader>
                <TableBody>{catalog.data.map((item) => <CatalogTableRow key={`${item.product_id}:${item.sku_id}`} item={item} />)}</TableBody>
              </Table>
            </div>
            <div className="divide-y md:hidden">
              {catalog.data.map((item) => <CatalogMobileCard key={`${item.product_id}:${item.sku_id}`} item={item} />)}
            </div>
          </>
        )}

        <div className="flex flex-col gap-2 border-t px-3 py-2 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
          <span>ทั้งหมด {formatNumber(catalog?.total_items)} SKU · หน้า {catalog?.page ?? 1} / {Math.max(catalog?.total_pages ?? 0, 1)}</span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={(catalog?.page ?? 1) <= 1 || loading}><ChevronLeft className="h-4 w-4" />ก่อนหน้า</Button>
            <Button variant="outline" size="sm" onClick={() => setPage((value) => value + 1)} disabled={(catalog?.page ?? 1) >= (catalog?.total_pages ?? 0) || loading}>ถัดไป<ChevronRight className="h-4 w-4" /></Button>
          </div>
        </div>
      </section>

      <Alert>
        <Warehouse className="h-4 w-4" />
        <AlertTitle>Product Modify: {productModifyReady ? 'สิทธิ์พร้อม แต่ยังไม่เปิดเขียน' : 'ยังไม่พร้อม'}</AlertTitle>
        <AlertDescription>แม้ Partner Center อนุมัติ Product Modify แล้ว ระบบจะเปิดการเขียนเฉพาะหลังมีหน้า dry-run, mapping ครบ, ยืนยันคลังเดียว และ canary 1 SKU ที่อ่านค่ากลับตรงกัน</AlertDescription>
      </Alert>
    </div>
  )
}

function CatalogTableRow({ item }: { item: TikTokCatalogItem }) {
  return (
    <TableRow>
      <TableCell><ProductIdentity item={item} /></TableCell>
      <TableCell><Badge variant="outline">{productStatusLabel(item.product_status)}</Badge></TableCell>
      <TableCell className="text-right tabular-nums">{formatNumber(item.total_available_quantity)}</TableCell>
      <TableCell className="text-right tabular-nums">{formatNumber(item.total_committed_quantity)}</TableCell>
      <TableCell><InventorySummary inventory={item.inventory} /></TableCell>
      <TableCell className="whitespace-nowrap text-xs text-muted-foreground">{formatDateTime(item.last_seen_at)}</TableCell>
    </TableRow>
  )
}

function CatalogMobileCard({ item }: { item: TikTokCatalogItem }) {
  return (
    <article className="space-y-3 p-3">
      <div className="flex items-start justify-between gap-2"><ProductIdentity item={item} /><Badge variant="outline">{productStatusLabel(item.product_status)}</Badge></div>
      <div className="grid grid-cols-2 gap-2 rounded-md bg-muted/30 p-2 text-xs">
        <div><span className="text-muted-foreground">TikTok พร้อมขาย</span><div className="mt-0.5 font-semibold tabular-nums">{formatNumber(item.total_available_quantity)}</div></div>
        <div><span className="text-muted-foreground">จองแล้ว</span><div className="mt-0.5 font-semibold tabular-nums">{formatNumber(item.total_committed_quantity)}</div></div>
      </div>
      <InventorySummary inventory={item.inventory} />
      <p className="text-xs text-muted-foreground">อัปเดต {formatDateTime(item.last_seen_at)}</p>
    </article>
  )
}

function ProductIdentity({ item }: { item: TikTokCatalogItem }) {
  return (
    <div className="min-w-0">
      <p className="max-w-xl font-medium text-foreground">{item.product_title}</p>
      <p className="mt-1 break-all text-xs text-muted-foreground">Seller SKU {item.seller_sku || '—'} · SKU ID {item.sku_id}</p>
      <p className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">Product ID {item.product_id}</p>
    </div>
  )
}

function InventorySummary({ inventory }: { inventory: TikTokCatalogInventory[] }) {
  if (!inventory.length) return <span className="text-xs text-muted-foreground">ไม่พบคลังใน snapshot</span>
  return (
    <div className="space-y-1 text-xs">
      {inventory.map((item) => <div key={item.warehouse_id}><span className="font-mono">{item.warehouse_id}</span> · พร้อมขาย {formatNumber(item.available_quantity)} · จอง {formatNumber(item.committed_quantity)}</div>)}
    </div>
  )
}

function productStatusLabel(status: string) {
  const labels: Record<string, string> = {
    ACTIVATE: 'กำลังขาย', DRAFT: 'แบบร่าง', PENDING: 'รอตรวจ', FAILED: 'ไม่ผ่าน',
    SELLER_DEACTIVATED: 'ผู้ขายปิด', PLATFORM_DEACTIVATED: 'แพลตฟอร์มปิด', FREEZE: 'ระงับ', DELETED: 'ลบแล้ว',
  }
  return labels[status] ?? status
}

function formatNumber(value?: number) {
  return Number(value ?? 0).toLocaleString('th-TH-u-nu-latn')
}

function formatDateTime(value?: string) {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}

function apiErrorMessage(cause: unknown, fallback: string) {
  const data = (cause as { response?: { data?: { error?: { message?: string } | string } } })?.response?.data
  if (typeof data?.error === 'string') return data.error
  return data?.error?.message || fallback
}
