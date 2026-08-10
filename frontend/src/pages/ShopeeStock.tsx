import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import axios from 'axios'
import {
  AlertTriangle,
  Boxes,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleOff,
  Loader2,
  PackageCheck,
  Play,
  RefreshCw,
  Save,
  Search,
  Settings2,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { PageHeader } from '@/components/common/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

type LocationPair = { warehouse: string; location: string }
type StockLocation = { warehouse_code: string; warehouse_name: string; location_code: string; location_name: string }
type Diagnostic = { warehouse: string; location: string; balance_qty: number; code: string }
type StockCatalogUnit = { code: string; name: string; stand_value: number; divide_value: number; ratio: number; row_order: number; line_number: number }
type StockCatalogOption = { item_code: string; item_name: string; standard_unit: string; units: StockCatalogUnit[] }
type StockSetting = {
  shop_id: number
  shop_name: string
  credential_mode: string
  enabled: boolean
  stock_pct: number
  interval_seconds: number
  scope_mode: 'unconfigured' | 'all' | 'selected'
  locations: LocationPair[]
  all_scope_warning_acknowledged: boolean
  dry_run_required: boolean
  paused_reason?: string
  last_catalog_sync_at?: string
  last_preview_at?: string
  last_sync_at?: string
  last_success_at?: string
  last_error?: string
}
type ProductRow = {
  shop_id: number
  item_id: number
  model_id: number
  item_name: string
  model_name: string
  item_sku: string
  model_sku: string
  shopee_available: number
  shopee_reserved: number
  sml_item_code: string
  sml_item_name: string
  sml_unit_code: string
  unit_factor: number
  manual_unit_factor?: number
  match_source: string
  excluded: boolean
  warning_codes: string[]
  last_preview_balance?: number
  last_preview_excluded_balance?: number
  last_preview_min_qty?: number
  last_preview_max_qty?: number
  last_preview_target?: number
  last_success_target?: number
  updated_at: string
}
type SyncRun = {
  id: string
  run_type: string
  status: string
  total_count: number
  changed_count: number
  blocked_count: number
  error_count: number
  started_at: string
  error_message?: string
}
type Overview = {
  available: boolean
  availability_code?: string
  availability_text?: string
  gateway_mode: boolean
  settings: StockSetting[]
  locations: StockLocation[]
  diagnostics: Diagnostic[]
  products: ProductRow[]
  products_total: number
  products_page: number
  products_size: number
  runs: SyncRun[]
  checked_at: string
}
type Preview = {
  run_id: string
  total_count: number
  changed_count: number
  skipped_count: number
  blocked_count: number
  excluded_balance: number
  circuit_breaker?: string
  lines: Array<{
    item_id: number
    model_id: number
    sml_item_code: string
    scope_balance: number
    excluded_balance: number
    min_qty: number
    max_qty: number
    unit_factor: number
    current_stock: number
    reserved_stock: number
    target_stock: number
    blocked: boolean
    warning_codes: string[]
  }>
}

const STATUS_TABS = [
  { key: 'ready', label: 'พร้อมซิงก์' },
  { key: 'fix', label: 'ต้องแก้' },
  { key: 'excluded', label: 'ยกเว้น' },
  { key: 'history', label: 'ประวัติ' },
] as const

const WARNING_LABEL: Record<string, string> = {
  sku_not_found: 'ไม่พบ SKU ใน SML',
  unit_factor_missing: 'หน่วยนับไม่มีอัตราส่วน',
  unit_factor_below_one: 'อัตราส่วนต่ำกว่า 1',
  unit_ratio_mismatch: 'ratio ไม่ตรงกับ stand/divide',
  unit_not_found: 'ไม่พบหน่วยที่เลือกใน SML แล้ว',
  duplicate_sml_item: 'สินค้า SML ถูกใช้ซ้ำ',
  stock_balance_missing: 'ไม่พบยอดคงเหลือ',
  reserved_stock_exceeds_target: 'สต๊อกจอง Shopee สูงกว่าเป้าหมาย',
}

function errorText(error: unknown) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message
  return 'เกิดข้อผิดพลาด กรุณาลองอีกครั้ง'
}

function formatNumber(value?: number) {
  return new Intl.NumberFormat('th-TH', { maximumFractionDigits: 2 }).format(value ?? 0)
}

function formatDateTime(value?: string) {
  if (!value) return 'ยังไม่มีข้อมูล'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'ยังไม่มีข้อมูล'
  return new Intl.DateTimeFormat('th-TH', { dateStyle: 'short', timeStyle: 'short' }).format(date)
}

function bangkokDate() {
  const parts = new Intl.DateTimeFormat('en', {
    timeZone: 'Asia/Bangkok',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date())
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}`
}

function locationKey(location: LocationPair) {
  return `${location.warehouse}\u0000${location.location}`
}

export default function ShopeeStock() {
  const [data, setData] = useState<Overview | null>(null)
  const [shopID, setShopID] = useState(0)
  const [tab, setTab] = useState<(typeof STATUS_TABS)[number]['key']>('ready')
  const [page, setPage] = useState(1)
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  const [draft, setDraft] = useState<StockSetting | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [mapping, setMapping] = useState<ProductRow | null>(null)
  const sequence = useRef(0)

  const load = useCallback(async (preferredShopID = shopID) => {
    const current = sequence.current + 1
    sequence.current = current
    setLoading(true)
    try {
      const response = await client.get<Overview>('/api/settings/shopee-stock', {
        params: { shop_id: preferredShopID || undefined, status: tab === 'history' ? undefined : tab, page, size: 50, q: search || undefined },
      })
      if (sequence.current !== current) return
      setData(response.data)
      const selected = preferredShopID || response.data.settings[0]?.shop_id || 0
      setShopID(selected)
      const setting = response.data.settings.find((item) => item.shop_id === selected) ?? null
      setDraft(setting ? { ...setting, locations: [...setting.locations] } : null)
    } catch (error) {
      if (sequence.current === current) toast.error(errorText(error))
    } finally {
      if (sequence.current === current) setLoading(false)
    }
  }, [page, search, shopID, tab])

  useEffect(() => { void load() }, [page, search, tab])

  const selectedSetting = data?.settings.find((item) => item.shop_id === shopID)
  const warehouses = useMemo(() => {
    const rows = new Map<string, { code: string; name: string; locations: StockLocation[] }>()
    for (const item of data?.locations ?? []) {
      const current = rows.get(item.warehouse_code) ?? { code: item.warehouse_code, name: item.warehouse_name, locations: [] }
      current.locations.push(item)
      rows.set(item.warehouse_code, current)
    }
    return [...rows.values()]
  }, [data?.locations])
  const pageCount = Math.max(1, Math.ceil((data?.products_total ?? 0) / (data?.products_size || 50)))
  const productNames = useMemo(() => new Map((data?.products ?? []).map((product) => [`${product.item_id}:${product.model_id}`, product.model_name || product.item_name])), [data?.products])

  const runAction = async (name: string, action: () => Promise<void>) => {
    if (busy) return
    setBusy(name)
    try { await action() } catch (error) { toast.error(errorText(error)) } finally { setBusy('') }
  }

  const saveSettings = () => runAction('save', async () => {
    if (!draft) return
    const response = await client.put<StockSetting>(`/api/settings/shopee-stock/${shopID}`, {
      enabled: draft.enabled,
      stock_pct: draft.stock_pct,
      interval_seconds: draft.interval_seconds,
      scope_mode: draft.scope_mode,
      locations: draft.locations,
      acknowledge_all_scope_warnings: draft.all_scope_warning_acknowledged,
    })
    setDraft(response.data)
    toast.success('บันทึกการตั้งค่าสต๊อกแล้ว')
    await load(shopID)
  })

  const syncCatalog = () => runAction('catalog', async () => {
    await client.post(`/api/settings/shopee-stock/${shopID}/catalog-sync`, {}, { timeout: 180000 })
    setPreview(null)
    toast.success('อัปเดตสินค้า Shopee และ SML แล้ว')
    await load(shopID)
  })

  const previewImpact = () => runAction('preview', async () => {
    if (!draft) return
    await client.put(`/api/settings/shopee-stock/${shopID}`, {
      enabled: false,
      stock_pct: draft.stock_pct,
      interval_seconds: draft.interval_seconds,
      scope_mode: draft.scope_mode,
      locations: draft.locations,
      acknowledge_all_scope_warnings: draft.all_scope_warning_acknowledged,
    })
    const response = await client.post<Preview>(`/api/settings/shopee-stock/${shopID}/preview`, { as_of_date: bangkokDate() }, { timeout: 60000 })
    setPreview(response.data)
    toast.success(response.data.blocked_count ? `ตรวจแล้ว พบ ${response.data.blocked_count} รายการที่ต้องแก้` : 'ตรวจผลกระทบแล้ว พร้อมเปิดซิงก์')
    await load(shopID)
  })

  const syncNow = () => runAction('run', async () => {
    const response = await client.post<{ changed_count: number; error_count: number; unknown_count: number }>(`/api/settings/shopee-stock/${shopID}/run`, {}, { timeout: 180000 })
    toast.success(`อัปเดต ${response.data.changed_count} รายการ`)
    await load(shopID)
  })

  const toggleLocation = (location: StockLocation, checked: boolean) => {
    if (!draft) return
    const pair = { warehouse: location.warehouse_code, location: location.location_code }
    const key = locationKey(pair)
    const next = checked ? [...draft.locations.filter((item) => locationKey(item) !== key), pair] : draft.locations.filter((item) => locationKey(item) !== key)
    setDraft({ ...draft, locations: next, enabled: false, dry_run_required: true })
  }

  if (loading && !data) {
    return <div className="flex min-h-[320px] items-center justify-center"><Loader2 className="h-6 w-6 animate-spin text-primary" /></div>
  }

  return (
    <div className="space-y-4 p-4 sm:p-6">
      <PageHeader
        title="ซิงก์สต๊อก Shopee"
        description="คุมสต๊อก Shopee จากยอดพร้อมขายใน SML พร้อมกันของสำหรับหน้าร้าน"
        actions={<Button variant="outline" onClick={() => load(shopID)} disabled={loading || !!busy}><RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />รีเฟรช</Button>}
      />

      {!data?.available && (
        <Alert><CircleOff className="h-4 w-4" /><AlertTitle>ยังไม่เปิดใช้งานสำหรับร้านนี้</AlertTitle><AlertDescription>{data?.availability_text || 'ติดต่อผู้ดูแลระบบเพื่อเปิด Shopee Open API ผ่าน Central Gateway'}</AlertDescription></Alert>
      )}

      {data?.diagnostics?.length ? (
        <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>พบยอดในพื้นที่ว่างหรือไม่อยู่ใน master SML</AlertTitle><AlertDescription>รวม {data.diagnostics.length} ตำแหน่ง หากเลือก “รวมทุกคลัง” ต้องรับทราบก่อน dry-run เพื่อให้ผู้ใช้กลับไปแก้ master ใน SML ได้</AlertDescription></Alert>
      ) : null}

      <Card>
        <CardContent className="grid gap-4 p-4 lg:grid-cols-[220px_160px_180px_minmax(260px,1fr)_auto] lg:items-end">
          <div className="space-y-1.5"><Label>ร้าน Shopee</Label><select className="h-10 w-full rounded-md border bg-background px-3 text-sm" value={shopID} onChange={(event) => { const id = Number(event.target.value); setShopID(id); setPage(1); setPreview(null); void load(id) }}>{data?.settings.map((item) => <option key={item.shop_id} value={item.shop_id}>{item.shop_name}</option>)}</select></div>
          <div className="space-y-1.5"><Label htmlFor="stock-pct">สัดส่วนส่ง Shopee</Label><div className="relative"><Input id="stock-pct" type="number" min={1} max={100} value={draft?.stock_pct ?? 80} onChange={(event) => draft && setDraft({ ...draft, stock_pct: Number(event.target.value), enabled: false, dry_run_required: true })} className="pr-8" /><span className="absolute right-3 top-2.5 text-sm text-muted-foreground">%</span></div></div>
          <div className="space-y-1.5"><Label>ขอบเขตสต๊อก SML</Label><select className="h-10 w-full rounded-md border bg-background px-3 text-sm" value={draft?.scope_mode ?? 'unconfigured'} onChange={(event) => draft && setDraft({ ...draft, scope_mode: event.target.value as StockSetting['scope_mode'], locations: [], enabled: false, dry_run_required: true })}><option value="unconfigured">ยังไม่ได้เลือก</option><option value="all">รวมทุกคลัง</option><option value="selected">เลือกคลัง/พื้นที่</option></select></div>
          <div className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"><div><p className="text-sm font-medium">ซิงก์อัตโนมัติทุก 5 นาที</p><p className="text-xs text-muted-foreground">หยุดระบบไม่เปลี่ยนยอดที่อยู่ใน Shopee</p></div><Switch checked={draft?.enabled ?? false} disabled={!data?.available || !!draft?.dry_run_required || !!draft?.paused_reason} onCheckedChange={(checked) => draft && setDraft({ ...draft, enabled: checked })} /></div>
          <Button onClick={saveSettings} disabled={!draft || !!busy}><Save className="h-4 w-4" />บันทึก</Button>
        </CardContent>
      </Card>

      {draft?.scope_mode === 'selected' && (
        <section className="rounded-md border bg-card p-4">
          <div className="mb-3"><h2 className="text-sm font-semibold">คลังและพื้นที่ที่นำมาคำนวณ</h2><p className="text-xs text-muted-foreground">เลือกอย่างน้อย 1 พื้นที่ ระบบจะแสดงยอดที่ถูกตัดออกใน dry-run</p></div>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{warehouses.map((warehouse) => <div key={warehouse.code} className="rounded-md border p-3"><p className="mb-2 text-sm font-medium">{warehouse.code} · {warehouse.name}</p><div className="space-y-2">{warehouse.locations.map((location) => { const checked = draft.locations.some((item) => locationKey(item) === locationKey({ warehouse: location.warehouse_code, location: location.location_code })); return <label key={`${location.warehouse_code}:${location.location_code}`} className="flex cursor-pointer items-center gap-2 text-sm"><Checkbox checked={checked} onCheckedChange={(value) => toggleLocation(location, value === true)} /><span>{location.location_code} · {location.location_name || 'ไม่ระบุชื่อ'}</span></label> })}</div></div>)}</div>
        </section>
      )}

      {draft?.scope_mode === 'all' && data?.diagnostics?.length ? (
        <label className="flex items-start gap-3 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm"><Checkbox checked={draft.all_scope_warning_acknowledged} onCheckedChange={(value) => setDraft({ ...draft, all_scope_warning_acknowledged: value === true, enabled: false, dry_run_required: true })} /><span>รับทราบว่ารอบนี้จะรวมยอดในพื้นที่ว่าง/orphan ด้วย และจะตรวจ master คลังใน SML ให้ถูกต้อง</span></label>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" onClick={syncCatalog} disabled={!data?.available || !!busy}><Boxes className="h-4 w-4" />{busy === 'catalog' ? 'กำลังดึงสินค้า...' : 'อัปเดต Catalog'}</Button>
        <Button variant="outline" onClick={previewImpact} disabled={!data?.available || !draft || draft.scope_mode === 'unconfigured' || !!busy}><PackageCheck className="h-4 w-4" />{busy === 'preview' ? 'กำลังคำนวณ...' : 'ตรวจผลกระทบ Dry-run'}</Button>
        <Button onClick={syncNow} disabled={!draft?.enabled || !!draft?.dry_run_required || !!busy}><Play className="h-4 w-4" />ซิงก์ตอนนี้</Button>
        <span className="text-xs text-muted-foreground">Catalog ล่าสุด {formatDateTime(selectedSetting?.last_catalog_sync_at)} · ซิงก์สำเร็จล่าสุด {formatDateTime(selectedSetting?.last_success_at)}</span>
      </div>

      {draft?.paused_reason && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>ระบบหยุดร้านนี้เพื่อความปลอดภัย</AlertTitle><AlertDescription>{draft.paused_reason}{draft.last_error ? ` · ${draft.last_error}` : ''}</AlertDescription></Alert>}
      {preview && <Alert className={preview.circuit_breaker ? 'border-destructive/50 bg-destructive/5' : preview.blocked_count ? 'border-warning/50 bg-warning/10' : 'border-success/40 bg-success/10'}>{preview.circuit_breaker || preview.blocked_count ? <AlertTriangle className="h-4 w-4" /> : <CheckCircle2 className="h-4 w-4" />}<AlertTitle>{preview.circuit_breaker ? 'ระบบหยุดทั้งร้านเพื่อความปลอดภัย' : preview.blocked_count ? `พร้อมเปิดซิงก์ โดยเว้น ${formatNumber(preview.blocked_count)} รายการที่ต้องแก้` : 'Dry-run ผ่านแล้ว'}</AlertTitle><AlertDescription>ตรวจ {formatNumber(preview.total_count)} รายการ · จะเปลี่ยน {formatNumber(preview.changed_count)} · ไม่เปลี่ยน {formatNumber(preview.skipped_count)} · ยอดนอกขอบเขต {formatNumber(preview.excluded_balance)}{preview.circuit_breaker && ` · ${preview.circuit_breaker}`}</AlertDescription></Alert>}
      {preview?.lines?.some((line) => line.blocked) && <section className="rounded-md border border-warning/40 bg-warning/5"><div className="border-b px-4 py-3"><h2 className="text-sm font-semibold">รายการที่ระบบจะไม่ส่งไป Shopee</h2><p className="text-xs text-muted-foreground">แก้ข้อมูลใน SML หรือกดจับคู่สินค้าแต่ละรายการ แล้วทำ Dry-run ใหม่</p></div><div className="divide-y">{(preview.lines ?? []).filter((line) => line.blocked).slice(0, 20).map((line) => <div key={`${line.item_id}:${line.model_id}`} className="grid gap-1 px-4 py-2 text-sm sm:grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)]"><span className="truncate">{productNames.get(`${line.item_id}:${line.model_id}`) || `Item ${line.item_id}${line.model_id ? ` / Model ${line.model_id}` : ''}`}</span><span className="text-warning-foreground">{line.warning_codes.map((code) => WARNING_LABEL[code] || code).join(' · ')}</span></div>)}{preview.blocked_count > 20 && <p className="px-4 py-2 text-xs text-muted-foreground">แสดง 20 จาก {formatNumber(preview.blocked_count)} รายการ ใช้แท็บ “ต้องแก้” เพื่อจัดการต่อ</p>}</div></section>}

      <section className="overflow-hidden rounded-md border bg-card">
        <div className="flex flex-col gap-3 border-b p-3 sm:flex-row sm:items-center sm:justify-between">
          <Tabs value={tab} onValueChange={(value) => { setTab(value as typeof tab); setPage(1) }}><TabsList>{STATUS_TABS.map((item) => <TabsTrigger key={item.key} value={item.key}>{item.label}</TabsTrigger>)}</TabsList></Tabs>
          {tab !== 'history' && <form className="relative w-full sm:w-72" onSubmit={(event) => { event.preventDefault(); setPage(1); setSearch(query.trim()) }}><Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" /><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ค้นหา SKU หรือสินค้า" className="pl-9" /></form>}
        </div>

        {tab === 'history' ? <HistoryList runs={data?.runs ?? []} /> : (
          <>
            <div className="hidden grid-cols-[minmax(240px,1.4fr)_minmax(220px,1.2fr)_90px_90px_120px] gap-3 border-b bg-muted/40 px-4 py-2 text-xs font-medium text-muted-foreground lg:grid"><span>สินค้า Shopee</span><span>สินค้าและยอด SML</span><span className="text-right">Shopee ตั้งไว้</span><span className="text-right">เป้าหมาย</span><span /></div>
            <div className="divide-y">{(data?.products ?? []).map((product) => <ProductLine key={`${product.item_id}:${product.model_id}`} product={product} onMap={() => setMapping(product)} />)}</div>
            {!data?.products.length && <div className="px-4 py-12 text-center text-sm text-muted-foreground">{selectedSetting?.last_catalog_sync_at ? 'ไม่พบรายการในแท็บนี้' : 'กด “อัปเดต Catalog” เพื่อดึงสินค้า Shopee และ SML ก่อน'}</div>}
            <div className="flex items-center justify-between border-t px-3 py-2"><span className="text-xs text-muted-foreground">ทั้งหมด {formatNumber(data?.products_total)} รายการ</span><div className="flex items-center gap-2"><Button variant="outline" size="icon" title="หน้าก่อนหน้า" disabled={page <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}><ChevronLeft className="h-4 w-4" /></Button><span className="text-sm">{page} / {pageCount}</span><Button variant="outline" size="icon" title="หน้าถัดไป" disabled={page >= pageCount} onClick={() => setPage((value) => value + 1)}><ChevronRight className="h-4 w-4" /></Button></div></div>
          </>
        )}
      </section>

      <MappingDialog product={mapping} shopID={shopID} onClose={() => setMapping(null)} onSaved={async () => { setMapping(null); setPreview(null); await load(shopID) }} />
    </div>
  )
}

function ProductLine({ product, onMap }: { product: ProductRow; onMap: () => void }) {
  const sku = product.model_sku || product.item_sku || 'ไม่มี SKU'
  return <div className="grid gap-3 px-4 py-3 lg:grid-cols-[minmax(240px,1.4fr)_minmax(220px,1.2fr)_90px_90px_120px] lg:items-center"><div className="min-w-0"><p className="truncate text-sm font-medium">{product.model_name || product.item_name}</p><p className="mt-0.5 truncate text-xs text-muted-foreground">SKU {sku} · Item {product.item_id}{product.model_id ? ` / Model ${product.model_id}` : ''}</p>{product.warning_codes.length > 0 && <div className="mt-2 flex flex-wrap gap-1">{product.warning_codes.map((code) => <Badge key={code} variant="outline" className="border-warning/40 bg-warning/10 text-warning-foreground">{WARNING_LABEL[code] || code}</Badge>)}</div>}</div><div className="min-w-0"><p className="truncate text-sm">{product.sml_item_code ? `${product.sml_item_code} · ${product.sml_item_name}` : 'ยังไม่จับคู่'}</p><p className="text-xs text-muted-foreground">{product.sml_unit_code ? `${product.sml_unit_code} · 1 หน่วยขาย = ${formatNumber(product.unit_factor)} หน่วยเล็ก${product.manual_unit_factor ? ' (กำหนดเอง)' : ''}` : 'เลือกสินค้าและหน่วย SML'}</p>{product.last_preview_balance != null && <p className="mt-1 text-xs text-muted-foreground">คงเหลือ {formatNumber(product.last_preview_balance)} · ขั้นต่ำ {formatNumber(product.last_preview_min_qty)} · สูงสุด {formatNumber(product.last_preview_max_qty)}{product.last_preview_excluded_balance ? ` · นอกขอบเขต ${formatNumber(product.last_preview_excluded_balance)}` : ''}</p>}</div><div className="text-left lg:text-right"><span className="text-xs text-muted-foreground lg:hidden">Shopee ตั้งไว้ </span><span className="font-mono text-sm">{formatNumber(product.shopee_available)}</span>{product.shopee_reserved > 0 && <p className="text-xs text-warning">จอง {formatNumber(product.shopee_reserved)}</p>}</div><div className="text-left lg:text-right"><span className="text-xs text-muted-foreground lg:hidden">เป้าหมาย </span><span className="font-mono text-sm">{product.last_preview_target == null ? '-' : formatNumber(product.last_preview_target)}</span></div><Button variant="outline" size="sm" onClick={onMap}><Settings2 className="h-4 w-4" />{product.excluded ? 'แก้ไข' : product.sml_item_code ? 'เปลี่ยน' : 'จับคู่'}</Button></div>
}

function HistoryList({ runs }: { runs: SyncRun[] }) {
  if (!runs.length) return <div className="px-4 py-12 text-center text-sm text-muted-foreground">ยังไม่มีประวัติ</div>
  return <div className="divide-y">{runs.map((run) => <div key={run.id} className="grid gap-2 px-4 py-3 sm:grid-cols-[140px_1fr_auto] sm:items-center"><div><Badge variant="outline">{run.run_type === 'catalog' ? 'Catalog' : run.run_type === 'preview' ? 'Dry-run' : 'Sync'}</Badge><p className="mt-1 text-xs text-muted-foreground">{formatDateTime(run.started_at)}</p></div><div className="text-sm">ตรวจ {formatNumber(run.total_count)} · เปลี่ยน {formatNumber(run.changed_count)} · block {formatNumber(run.blocked_count)} · error {formatNumber(run.error_count)}{run.error_message && <p className="mt-1 text-xs text-destructive">{run.error_message}</p>}</div><Badge variant={run.status === 'success' ? 'default' : 'outline'}>{run.status}</Badge></div>)}</div>
}

function MappingDialog({ product, shopID, onClose, onSaved }: { product: ProductRow | null; shopID: number; onClose: () => void; onSaved: () => Promise<void> }) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<StockCatalogOption[]>([])
  const [selected, setSelected] = useState<StockCatalogOption | null>(null)
  const [units, setUnits] = useState<StockCatalogUnit[]>([])
  const [unitCode, setUnitCode] = useState('')
  const [useManualFactor, setUseManualFactor] = useState(false)
  const [manualFactor, setManualFactor] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let active = true
    setQuery(product?.sml_item_code || '')
    setResults([])
    setSelected(product?.sml_item_code ? { item_code: product.sml_item_code, item_name: product.sml_item_name, standard_unit: product.sml_unit_code, units: [] } : null)
    setUnits([])
    setUnitCode(product?.sml_unit_code || '')
    setUseManualFactor(product?.manual_unit_factor != null)
    setManualFactor(product?.manual_unit_factor?.toString() || '')
    if (product?.sml_item_code) {
      setBusy(true)
      void client.get<{ items: StockCatalogOption[] }>('/api/settings/shopee-stock/catalog-search', { params: { q: product.sml_item_code } })
        .then((response) => {
          if (!active) return
          const exact = (response.data.items ?? []).find((item) => item.item_code === product.sml_item_code)
          if (exact) { setSelected(exact); setUnits(exact.units ?? []) }
        })
        .catch((error) => { if (active) toast.error(errorText(error)) })
        .finally(() => { if (active) setBusy(false) })
    }
    return () => { active = false }
  }, [product?.item_id, product?.model_id, product?.sml_item_code, product?.sml_item_name, product?.sml_unit_code, product?.manual_unit_factor])
  const searchCatalog = async () => {
    if (query.trim().length < 2) { toast.error('กรอกอย่างน้อย 2 ตัวอักษร'); return }
    setBusy(true)
    try { const response = await client.get<{ items: StockCatalogOption[] }>('/api/settings/shopee-stock/catalog-search', { params: { q: query.trim() } }); setResults(response.data.items ?? []) } catch (error) { toast.error(errorText(error)) } finally { setBusy(false) }
  }
  const selectProduct = (item: StockCatalogOption) => {
    setSelected(item)
    const next = item.units ?? []
    setUnits(next)
    setUnitCode(next.find((unit) => unit.code === item.standard_unit)?.code || next[0]?.code || '')
  }
  const save = async (excluded = false) => {
    if (!product || (!excluded && (!selected || !unitCode))) return
    const parsedManualFactor = Number(manualFactor)
    if (!excluded && useManualFactor && (!Number.isFinite(parsedManualFactor) || parsedManualFactor < 1)) { toast.error('อัตราส่วนที่กำหนดเองต้องไม่น้อยกว่า 1'); return }
    setBusy(true)
    try { await client.put(`/api/settings/shopee-stock/${shopID}/mappings/${product.item_id}/${product.model_id}`, { sml_item_code: excluded ? '' : selected?.item_code, sml_unit_code: excluded ? '' : unitCode, manual_unit_factor: !excluded && useManualFactor ? parsedManualFactor : null, excluded, updated_at: product.updated_at }); toast.success(excluded ? 'ยกเว้นสินค้านี้แล้ว' : 'บันทึกการจับคู่แล้ว ต้อง dry-run ใหม่'); await onSaved() } catch (error) { toast.error(errorText(error)) } finally { setBusy(false) }
  }
  return <Dialog open={!!product} onOpenChange={(open) => !open && onClose()}><DialogContent className="max-w-2xl"><DialogHeader><DialogTitle>จับคู่สินค้า SML</DialogTitle><DialogDescription>{product?.model_name || product?.item_name} · SKU {product?.model_sku || product?.item_sku || 'ไม่มี'}</DialogDescription></DialogHeader><div className="space-y-4"><form className="flex gap-2" onSubmit={(event) => { event.preventDefault(); void searchCatalog() }}><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ค้นหารหัสหรือชื่อสินค้า SML" /><Button type="submit" variant="outline" disabled={busy}><Search className="h-4 w-4" />ค้นหา</Button></form><div className="max-h-52 divide-y overflow-y-auto rounded-md border">{results.map((item) => <button key={item.item_code} type="button" onClick={() => selectProduct(item)} className={cn('block w-full px-3 py-2 text-left hover:bg-muted', selected?.item_code === item.item_code && 'bg-primary/10')}><p className="text-sm font-medium">{item.item_code}</p><p className="text-xs text-muted-foreground">{item.item_name}</p></button>)}{!results.length && <p className="p-4 text-center text-sm text-muted-foreground">ค้นหาและเลือกสินค้าจาก SML stock catalog</p>}</div>{selected && <div className="space-y-3"><div className="space-y-1.5"><Label>หน่วยขายบน Shopee</Label><select value={unitCode} onChange={(event) => setUnitCode(event.target.value)} className="h-10 w-full rounded-md border bg-background px-3 text-sm"><option value="">เลือกหน่วย</option>{units.map((unit) => <option key={unit.code} value={unit.code}>{unit.code}{unit.name ? ` · ${unit.name}` : ''} ({formatNumber((unit.stand_value ?? 0) / (unit.divide_value || 1))} หน่วยเล็ก)</option>)}</select></div><div className="rounded-md border p-3"><div className="flex items-center justify-between gap-3"><div><Label htmlFor="manual-factor">ยืนยันอัตราส่วนเอง</Label><p className="text-xs text-muted-foreground">ใช้เฉพาะเมื่อข้อมูลหน่วยใน SML ยังไม่ถูกต้อง และระบบจะบันทึก audit</p></div><Switch id="manual-factor" checked={useManualFactor} onCheckedChange={setUseManualFactor} /></div>{useManualFactor && <Input className="mt-3" inputMode="decimal" type="number" min="1" step="any" value={manualFactor} onChange={(event) => setManualFactor(event.target.value)} placeholder="เช่น 6" />}</div></div>}</div><DialogFooter className="gap-2 sm:justify-between"><Button type="button" variant="destructive" onClick={() => save(true)} disabled={busy}>ยกเว้นจากการซิงก์</Button><div className="flex gap-2"><Button variant="outline" onClick={onClose}>ยกเลิก</Button><Button onClick={() => save(false)} disabled={busy || !selected || !unitCode || (useManualFactor && !manualFactor)}>{busy && <Loader2 className="h-4 w-4 animate-spin" />}บันทึกการจับคู่</Button></div></DialogFooter></DialogContent></Dialog>
}
