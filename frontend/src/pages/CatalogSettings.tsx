import { useCallback, useEffect, useState } from 'react'
import { Boxes, Database, ImageIcon, Link2, Plus, RefreshCw, Search } from 'lucide-react'
import { toast } from 'sonner'

import api from '@/api/client'
import { AuthImage } from '@/components/common/AuthImage'
import { EmptyState } from '@/components/common/EmptyState'
import { ProductImagePreviewDialog } from '@/components/common/ProductImagePreviewDialog'
import { CatalogMarketplaceLinksDialog } from '@/components/catalog/CatalogMarketplaceLinksDialog'
import { MarketplaceSourceChannelBadges } from '@/components/marketplace/InputChannelBadge'
import { SetProductDetailsDialog } from '@/components/catalog/SetProductDetailsDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useAuth } from '@/hooks/useAuth'
import { marketplaceDisplayInputChannels } from '@/lib/billInputChannel'
import { cn } from '@/lib/utils'
import type { CatalogItem } from '@/types'

interface CatalogResponse { data: CatalogItem[]; total: number; page: number; per_page: number }
interface CatalogStats {
  total: number
  hidden_code_count?: number
  sync_running?: boolean
  sync_status?: { running: boolean; count: number; error?: string; finished_at?: string }
}

const PER_PAGE = 50

function messageFrom(error: unknown, fallback: string) {
  const candidate = error as { response?: { data?: { error?: string } } }
  return candidate.response?.data?.error ?? fallback
}

export default function CatalogSettings() {
  const { user } = useAuth()
  const [items, setItems] = useState<CatalogItem[]>([])
  const [stats, setStats] = useState<CatalogStats | null>(null)
  const [page, setPage] = useState(1)
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState('')
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [refreshingCode, setRefreshingCode] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [preview, setPreview] = useState<CatalogItem | null>(null)
  const [setDetailsItem, setSetDetailsItem] = useState<CatalogItem | null>(null)
  const [marketplaceDetailsItem, setMarketplaceDetailsItem] = useState<CatalogItem | null>(null)
  const canManageCatalog = user?.role === 'admin'
  const totalPages = Math.max(1, Math.ceil((stats?.total ?? 0) / PER_PAGE))

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [listResponse, statsResponse] = await Promise.all([
        api.get<CatalogResponse>('/api/catalog', { params: { page, per_page: PER_PAGE, q: query || undefined } }),
        api.get<CatalogStats>('/api/catalog/stats'),
      ])
      setItems(listResponse.data.data ?? [])
      setStats({ ...statsResponse.data, total: listResponse.data.total ?? statsResponse.data.total ?? 0 })
    } catch (error) {
      toast.error(messageFrom(error, 'โหลดสินค้า SML ไม่สำเร็จ'))
    } finally {
      setLoading(false)
    }
  }, [page, query])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (!stats?.sync_running) return
    const timer = window.setInterval(() => void load(), 3000)
    return () => window.clearInterval(timer)
  }, [load, stats?.sync_running])

  const syncCatalog = async () => {
    setSyncing(true)
    try {
      await api.post('/api/catalog/sync')
      toast.success('เริ่มซิงก์สินค้า SML แล้ว')
      await load()
    } catch (error) {
      toast.error(messageFrom(error, 'เริ่มซิงก์สินค้าไม่สำเร็จ'))
    } finally {
      setSyncing(false)
    }
  }

  const refreshOne = async (code: string) => {
    setRefreshingCode(code)
    try {
      await api.post(`/api/catalog/${encodeURIComponent(code)}/refresh`)
      toast.success(`อัปเดต ${code} แล้ว`)
      await load()
    } catch (error) {
      toast.error(messageFrom(error, `อัปเดต ${code} ไม่สำเร็จ`))
    } finally {
      setRefreshingCode('')
    }
  }

  const visibleCount = items.length

  return (
    <div className="space-y-4 p-0 sm:p-0">
      <header className="flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
            <h1 className="text-xl font-semibold tracking-tight text-foreground">รายการสินค้า SML</h1>
            <span className="text-sm tabular-nums text-muted-foreground">{(stats?.total ?? 0).toLocaleString()} รายการ</span>
          </div>
          <p className="mt-1 text-sm text-muted-foreground">ดูสินค้า SML และกลุ่มรายการ Marketplace ที่ใช้สินค้านี้ร่วมกัน</p>
        </div>
        {canManageCatalog && (
          <div className="flex shrink-0 flex-wrap gap-2">
            <Button variant="outline" size="sm" onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" />เพิ่มสินค้า</Button>
            <Button size="sm" onClick={syncCatalog} disabled={syncing || stats?.sync_running}>
              <RefreshCw className={cn('h-4 w-4', (syncing || stats?.sync_running) && 'animate-spin')} />
              {stats?.sync_running ? 'กำลังซิงก์' : 'ซิงก์จาก SML'}
            </Button>
          </div>
        )}
      </header>

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-live="polite">
        <span>ค้นหาได้ด้วยรหัสหรือชื่อสินค้าจากฐานข้อมูล SML</span>
        {stats?.sync_running
          ? <span>กำลังซิงก์แล้ว {(stats.sync_status?.count ?? 0).toLocaleString()} รายการ</span>
          : stats?.sync_status?.error
            ? <span className="text-destructive">ซิงก์ล่าสุดไม่สำเร็จ: {stats.sync_status.error}</span>
            : <span>แสดง {visibleCount.toLocaleString()} จาก {(stats?.total ?? 0).toLocaleString()} รายการ</span>}
      </div>

      {items.some((item) => item.item_type === 3) && (
        <div className="flex gap-2 border-l-2 border-primary px-3 py-1.5 text-xs text-muted-foreground">
          <Boxes className="h-4 w-4 shrink-0 text-primary" />
          <span>สินค้าชุดจับคู่ด้วยรหัสสินค้าแม่เพียงรายการเดียว ราคาบิลมาจาก Marketplace และสัดส่วน SML ใช้แบ่งรายการส่วนประกอบ</span>
        </div>
      )}

      <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="catalog-results-heading">
        <div className="flex flex-col gap-2 border-b p-3 sm:flex-row sm:items-center">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter') { setQuery(draft.trim()); setPage(1) } }}
              placeholder="ค้นหารหัสหรือชื่อสินค้า"
              className="pl-8"
            />
          </div>
          <Button variant="outline" className="sm:shrink-0" onClick={() => { setQuery(draft.trim()); setPage(1) }}>ค้นหา</Button>
        </div>

        {!loading && items.length === 0 ? (
          <EmptyState icon={Database} title="ยังไม่มีสินค้า SML" description="กดซิงก์จาก SML ก่อนเริ่มนำเข้าและสร้างเอกสารขาย" />
        ) : (
          <>
            <h2 id="catalog-results-heading" className="sr-only">รายการสินค้าและการจับคู่ Marketplace</h2>
            <div className="hidden overflow-x-auto md:block">
            <Table>
              <TableHeader><TableRow><TableHead className="min-w-[360px]">สินค้า SML</TableHead><TableHead className="min-w-[360px]">กลุ่มการจับคู่ Marketplace</TableHead>{canManageCatalog && <TableHead className="w-[112px] text-right">จัดการ</TableHead>}</TableRow></TableHeader>
              <TableBody>
                {loading ? Array.from({ length: 8 }).map((_, index) => <TableRow key={index}><TableCell colSpan={canManageCatalog ? 3 : 2}><Skeleton className="h-11 w-full" /></TableCell></TableRow>) : items.map((item) => {
                  return <CatalogTableRow key={item.item_code} item={item} canManageCatalog={canManageCatalog} refreshing={refreshingCode === item.item_code} onRefresh={refreshOne} onPreview={setPreview} onSetDetails={setSetDetailsItem} onMarketplaceDetails={setMarketplaceDetailsItem} />
                })}
              </TableBody>
            </Table>
            </div>
            <div className="divide-y md:hidden">
              {loading ? Array.from({ length: 6 }).map((_, index) => <div key={index} className="p-4"><Skeleton className="h-28 w-full" /></div>) : items.map((item) => (
                <CatalogMobileCard key={item.item_code} item={item} canManageCatalog={canManageCatalog} refreshing={refreshingCode === item.item_code} onRefresh={refreshOne} onPreview={setPreview} onSetDetails={setSetDetailsItem} onMarketplaceDetails={setMarketplaceDetailsItem} />
              ))}
            </div>
          </>
        )}

        <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
          <span>หน้า {page}/{totalPages}</span>
          <div className="flex gap-2"><Button size="sm" variant="outline" disabled={page <= 1 || loading} onClick={() => setPage((value) => value - 1)}>ก่อนหน้า</Button><Button size="sm" variant="outline" disabled={page >= totalPages || loading} onClick={() => setPage((value) => value + 1)}>ถัดไป</Button></div>
        </div>
      </section>

      {canManageCatalog && <CreateProductDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={load} />}
      <ProductImagePreviewDialog open={preview !== null} onOpenChange={(open) => !open && setPreview(null)} imageUrl={preview?.image_url} itemCode={preview?.item_code} itemName={preview?.item_name} imageCount={preview?.image_count ?? 0} />
      <SetProductDetailsDialog
        open={setDetailsItem !== null}
        onOpenChange={(open) => !open && setSetDetailsItem(null)}
        itemCode={setDetailsItem?.item_code ?? ''}
        itemName={setDetailsItem?.item_name ?? ''}
        components={setDetailsItem?.set_components}
        documentValid={setDetailsItem?.set_document_valid}
        stockValid={setDetailsItem?.set_stock_valid}
        warningCodes={setDetailsItem?.set_warning_codes}
        showStockStatus
      />
      <CatalogMarketplaceLinksDialog
        open={marketplaceDetailsItem !== null}
        onOpenChange={(open) => !open && setMarketplaceDetailsItem(null)}
        itemCode={marketplaceDetailsItem?.item_code ?? ''}
        itemName={marketplaceDetailsItem?.item_name ?? ''}
      />
    </div>
  )
}

interface CatalogRowProps {
  item: CatalogItem
  canManageCatalog: boolean
  refreshing: boolean
  onRefresh: (code: string) => Promise<void>
  onPreview: (item: CatalogItem) => void
  onSetDetails: (item: CatalogItem) => void
  onMarketplaceDetails: (item: CatalogItem) => void
}

function CatalogTableRow(props: CatalogRowProps) {
  const { item, canManageCatalog, refreshing, onRefresh, onPreview, onSetDetails, onMarketplaceDetails } = props
  return (
    <TableRow>
      <TableCell><CatalogProductIdentity item={item} onPreview={onPreview} onSetDetails={onSetDetails} /></TableCell>
      <TableCell><MarketplaceMappingSummary item={item} onShowDetails={onMarketplaceDetails} /></TableCell>
      {canManageCatalog && <TableCell className="text-right"><RefreshCatalogProductButton itemCode={item.item_code} refreshing={refreshing} onRefresh={onRefresh} /></TableCell>}
    </TableRow>
  )
}

function CatalogMobileCard(props: CatalogRowProps) {
  const { item, canManageCatalog, refreshing, onRefresh, onPreview, onSetDetails, onMarketplaceDetails } = props
  return (
    <article className="space-y-3 p-4">
      <CatalogProductIdentity item={item} onPreview={onPreview} onSetDetails={onSetDetails} />
      <div className="border-t pt-3">
        <p className="mb-2 text-xs font-medium text-muted-foreground">กลุ่มการจับคู่ Marketplace</p>
        <MarketplaceMappingSummary item={item} onShowDetails={onMarketplaceDetails} />
      </div>
      {canManageCatalog && <div className="flex justify-end"><RefreshCatalogProductButton itemCode={item.item_code} refreshing={refreshing} onRefresh={onRefresh} /></div>}
    </article>
  )
}

function CatalogProductIdentity({ item, onPreview, onSetDetails }: Pick<CatalogRowProps, 'item' | 'onPreview' | 'onSetDetails'>) {
  const hasImage = Boolean(item.image_url && (item.image_count ?? 0) > 0)
  return (
    <div className="flex min-w-0 items-start gap-2.5">
      <button type="button" className="h-8 w-8 shrink-0 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2" disabled={!hasImage} onClick={() => hasImage && onPreview(item)} aria-label={hasImage ? `ดูรูปสินค้า ${item.item_name}` : undefined}>
        <AuthImage src={hasImage ? item.image_url : undefined} className="h-full w-full rounded-md border bg-muted/30" imgClassName="object-cover" fallback={<div className="flex h-full items-center justify-center"><ImageIcon className="h-4 w-4 text-muted-foreground" /></div>} />
      </button>
      <div className="min-w-0">
        <p className="line-clamp-1 font-medium leading-snug" title={`${item.item_code} · ${item.item_name}`}><code className="font-semibold">{item.item_code}</code><span> · {item.item_name}</span></p>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span className="shrink-0">หน่วย SML: <span className="text-foreground">{item.unit_code || '-'}</span></span>
          {item.item_type === 3 && <button type="button" className="shrink-0 text-link hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2" onClick={() => onSetDetails(item)}>สินค้าชุด · ดูส่วนประกอบ {item.set_component_count ?? 0}</button>}
          {item.has_hidden_chars && <Badge variant="destructive" className="h-5 shrink-0">รหัสผิดรูปแบบ</Badge>}
          {item.item_name2 && <span className="truncate" title={item.item_name2}>{item.item_name2}</span>}
        </div>
      </div>
    </div>
  )
}

function MarketplaceMappingSummary({ item, onShowDetails }: { item: CatalogItem; onShowDetails: (item: CatalogItem) => void }) {
  const summaries = item.marketplace_summaries ?? []
  const mappingCount = summaries.reduce((total, summary) => total + summary.mapping_count, 0)
  if (summaries.length === 0) return <p className="text-sm text-muted-foreground">ยังไม่จับคู่กับ Marketplace</p>
  return (
    <button type="button" className="group block rounded-md text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2" onClick={() => onShowDetails(item)} aria-label={`ดูสินค้า Marketplace ที่จับคู่กับ ${item.item_code}`}>
      <span className="flex flex-wrap gap-1.5">
        {summaries.map((summary) => (
          <MarketplaceSourceChannelBadges
            key={`${summary.source}:${summary.input_channels?.join(',') ?? ''}`}
            source={summary.source}
            count={summary.mapping_count}
            channels={marketplaceDisplayInputChannels(summary.source, { inputChannels: summary.input_channels })}
          />
        ))}
      </span>
      <span className="mt-2 flex items-center gap-1.5 text-xs text-link group-hover:underline">
        <Link2 className="h-3.5 w-3.5" aria-hidden="true" />ดู {mappingCount.toLocaleString()} รายการที่จับคู่และตัวเลือก
      </span>
    </button>
  )
}

function RefreshCatalogProductButton({ itemCode, refreshing, onRefresh }: { itemCode: string; refreshing: boolean; onRefresh: (code: string) => Promise<void> }) {
  return <Button size="sm" variant="outline" disabled={refreshing} onClick={() => void onRefresh(itemCode)}><RefreshCw className={cn('h-3.5 w-3.5', refreshing && 'animate-spin')} />อัปเดต</Button>
}

function CreateProductDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (open: boolean) => void; onCreated: () => Promise<void> }) {
  const [form, setForm] = useState({ code: '', name: '', unit_code: 'ชิ้น' })
  const [saving, setSaving] = useState(false)
  const create = async () => {
    if (!form.code.trim() || !form.name.trim() || !form.unit_code.trim()) { toast.error('กรอกรหัส ชื่อ และหน่วยสินค้าให้ครบ'); return }
    setSaving(true)
    try {
      await api.post('/api/catalog/products', { code: form.code.trim(), name: form.name.trim(), unit_code: form.unit_code.trim() })
      toast.success('เพิ่มสินค้าใน SML แล้ว')
      onOpenChange(false)
      setForm({ code: '', name: '', unit_code: 'ชิ้น' })
      await onCreated()
    } catch (error) {
      toast.error(messageFrom(error, 'เพิ่มสินค้าไม่สำเร็จ'))
    } finally { setSaving(false) }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader><DialogTitle>เพิ่มสินค้าใน SML</DialogTitle></DialogHeader>
        <div className="grid gap-3">
          <div><Label htmlFor="catalog-code">รหัสสินค้า</Label><Input id="catalog-code" value={form.code} onChange={(event) => setForm((value) => ({ ...value, code: event.target.value }))} /></div>
          <div><Label htmlFor="catalog-name">ชื่อสินค้า</Label><Input id="catalog-name" value={form.name} onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))} /></div>
          <div><Label htmlFor="catalog-unit">หน่วยหลัก</Label><Input id="catalog-unit" value={form.unit_code} onChange={(event) => setForm((value) => ({ ...value, unit_code: event.target.value }))} /></div>
          <p className="text-xs text-muted-foreground">ระบบจะสร้างสินค้าใน SML โดยไม่กำหนดราคา ราคาขายใช้จาก Marketplace ของแต่ละช่องทาง</p>
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button><Button onClick={create} disabled={saving}>{saving ? 'กำลังเพิ่ม...' : 'เพิ่มสินค้า'}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
