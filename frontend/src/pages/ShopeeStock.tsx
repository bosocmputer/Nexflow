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
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { PageHeader } from '@/components/common/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
  product_counts: { ready: number; fix: number; excluded: number }
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

function formatInterval(seconds?: number) {
  const value = seconds ?? 300
  if (value < 3600) return `${Math.max(1, Math.round(value / 60))} นาที`
  if (value % 3600 === 0) return `${value / 3600} ชั่วโมง`
  return `${Math.round(value / 60)} นาที`
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

function productItemName(product: ProductRow) {
  return product.item_name.trim() || product.model_name.trim() || 'ไม่ระบุชื่อสินค้า'
}

function productOptionName(product: ProductRow) {
  const itemName = product.item_name.trim()
  const modelName = product.model_name.trim()
  return modelName && modelName !== itemName ? modelName : ''
}

function productDisplayName(product: ProductRow) {
  const optionName = productOptionName(product)
  return optionName ? `${productItemName(product)} · ${optionName}` : productItemName(product)
}

function sameLocations(left: LocationPair[], right: LocationPair[]) {
  if (left.length !== right.length) return false
  const leftKeys = left.map(locationKey).sort()
  const rightKeys = right.map(locationKey).sort()
  return leftKeys.every((key, index) => key === rightKeys[index])
}

function sameSettings(left: StockSetting | null, right?: StockSetting) {
  if (!left || !right) return false
  return left.enabled === right.enabled &&
    left.stock_pct === right.stock_pct &&
    left.interval_seconds === right.interval_seconds &&
    left.scope_mode === right.scope_mode &&
    sameLocations(left.locations, right.locations)
}

function normalizeStockSetting(setting: StockSetting): StockSetting {
  const locations = setting.scope_mode === 'selected' ? [...setting.locations] : []
  return {
    ...setting,
    enabled: locations.length > 0 ? setting.enabled : false,
    scope_mode: locations.length > 0 ? 'selected' : 'unconfigured',
    locations,
    all_scope_warning_acknowledged: false,
  }
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
  const autoSelectedShops = useRef(new Set<number>())

  const load = useCallback(async (preferredShopID = shopID) => {
    const current = sequence.current + 1
    sequence.current = current
    setLoading(true)
    try {
      const response = await client.get<Overview>('/api/settings/shopee-stock', {
        params: { shop_id: preferredShopID || undefined, status: tab === 'history' ? undefined : tab, page, size: 50, q: search || undefined },
      })
      if (sequence.current !== current) return
      const normalizedData = { ...response.data, settings: response.data.settings.map(normalizeStockSetting) }
      setData(normalizedData)
      const selected = preferredShopID || normalizedData.settings[0]?.shop_id || 0
      setShopID(selected)
      const setting = normalizedData.settings.find((item) => item.shop_id === selected) ?? null
      setDraft(setting)
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
  const productNames = useMemo(() => new Map((data?.products ?? []).map((product) => [`${product.item_id}:${product.model_id}`, productDisplayName(product)])), [data?.products])
  const productCounts = data?.product_counts ?? { ready: 0, fix: 0, excluded: 0 }
  const stockPctValid = !!draft && Number.isFinite(draft.stock_pct) && draft.stock_pct >= 1 && draft.stock_pct <= 100
  const scopeReady = !!draft && draft.scope_mode === 'selected' && draft.locations.length > 0
  const settingsDirty = !sameSettings(draft, selectedSetting)

  useEffect(() => {
    if (!shopID || tab !== 'ready' || search || productCounts.ready > 0 || productCounts.fix === 0 || autoSelectedShops.current.has(shopID)) return
    autoSelectedShops.current.add(shopID)
    setPage(1)
    setTab('fix')
  }, [productCounts.fix, productCounts.ready, search, shopID, tab])

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
    setDraft({
      ...draft,
      scope_mode: next.length > 0 ? 'selected' : 'unconfigured',
      locations: next,
      all_scope_warning_acknowledged: false,
      enabled: false,
      dry_run_required: true,
    })
  }

  const previewDisabledReason = !data?.available
    ? 'ระบบซิงก์สต๊อกยังไม่พร้อมใช้งาน'
    : !draft
      ? 'เลือกร้าน Shopee ก่อน'
      : !stockPctValid
        ? 'กำหนดสัดส่วนส่ง Shopee ระหว่าง 1-100%'
        : !scopeReady
          ? 'เลือกคลังหรือพื้นที่อย่างน้อย 1 รายการ'
          : ''
  const syncDisabledReason = !draft?.enabled
    ? 'เปิดซิงก์อัตโนมัติและบันทึกก่อนใช้ปุ่มนี้'
    : draft.dry_run_required
      ? 'ตรวจผลกระทบ Dry-run ใหม่ก่อนซิงก์'
      : draft.paused_reason || ''
  const setupSteps = [
    {
      label: 'เลือกขอบเขตสต๊อก',
      detail: draft?.scope_mode === 'selected' && draft.locations.length
          ? `เลือกแล้ว ${formatNumber(draft.locations.length)} พื้นที่`
          : 'ยังไม่ได้เลือกคลังหรือพื้นที่',
      done: scopeReady,
    },
    {
      label: 'ตรวจและจับคู่สินค้า',
      detail: productCounts.fix > 0
        ? `ต้องแก้ ${formatNumber(productCounts.fix)} รายการ`
        : productCounts.ready > 0
          ? `พร้อมซิงก์ ${formatNumber(productCounts.ready)} รายการ`
          : 'ยังไม่มีสินค้าพร้อมซิงก์',
      done: productCounts.fix === 0 && productCounts.ready > 0,
    },
    {
      label: 'ตรวจผลกระทบ Dry-run',
      detail: !draft?.dry_run_required && selectedSetting?.last_preview_at
        ? `ตรวจล่าสุด ${formatDateTime(selectedSetting.last_preview_at)}`
        : 'ยังไม่ได้ตรวจด้วยค่าปัจจุบัน',
      done: !!draft && !draft.dry_run_required && !!selectedSetting?.last_preview_at,
    },
    {
      label: 'เปิดซิงก์สต๊อก',
      detail: draft?.enabled ? `ทำงานทุก ${formatInterval(draft.interval_seconds)}` : 'ยังปิดเพื่อความปลอดภัย',
      done: !!draft?.enabled,
    },
  ]
  const nextActionMessage = previewDisabledReason || (draft?.dry_run_required
    ? 'กด “บันทึกและตรวจสต๊อก” เพื่อคำนวณยอด SML และเป้าหมายโดยยังไม่ส่งไป Shopee'
    : syncDisabledReason)
  const emptyState = search
    ? { title: `ไม่พบรายการที่ตรงกับ “${search}”`, description: 'ลองค้นหาด้วย SKU รหัสสินค้า หรือชื่อที่สั้นลง' }
    : tab === 'ready' && productCounts.fix > 0
      ? { title: 'ยังไม่มีสินค้าพร้อมซิงก์', description: `มี ${formatNumber(productCounts.fix)} รายการที่ต้องจับคู่หรือแก้ข้อมูลก่อน` }
      : tab === 'fix'
        ? { title: 'ไม่มีรายการที่ต้องแก้', description: 'สินค้าที่ใช้งานอยู่พร้อมสำหรับขั้นตอน Dry-run แล้ว' }
        : tab === 'excluded'
          ? { title: 'ยังไม่มีสินค้าที่ถูกยกเว้น', description: 'สินค้าที่ไม่ต้องการซิงก์จะปรากฏในแท็บนี้' }
          : { title: 'ยังไม่มีสินค้าในแท็บนี้', description: selectedSetting?.last_catalog_sync_at ? 'ลองตรวจแท็บอื่นหรืออัปเดต Catalog อีกครั้ง' : 'อัปเดต Catalog เพื่อดึงสินค้า Shopee และสินค้า SML' }

  if (loading && !data) {
    return <div className="flex min-h-[320px] items-center justify-center"><Loader2 className="h-6 w-6 animate-spin text-primary" /></div>
  }

  return (
    <div className="space-y-4 p-4 sm:p-6">
      <PageHeader
        title="ซิงก์สต๊อก Shopee"
        description="คุมสต๊อก Shopee จากยอดพร้อมขายใน SML โดยกันสต๊อกส่วนหนึ่งไว้สำหรับหน้าร้าน"
        actions={<Button variant="outline" onClick={() => load(shopID)} disabled={loading || !!busy}><RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />รีเฟรช</Button>}
      />

      {!data?.available && (
        <Alert><CircleOff className="h-4 w-4" /><AlertTitle>ยังไม่เปิดใช้งานสำหรับร้านนี้</AlertTitle><AlertDescription>{data?.availability_text || 'ติดต่อผู้ดูแลระบบเพื่อเปิด Shopee Open API ผ่าน Central Gateway'}</AlertDescription></Alert>
      )}

      {data?.diagnostics?.length ? (
        <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>พบยอดในพื้นที่ว่างหรือไม่อยู่ใน master SML</AlertTitle><AlertDescription>รวม {formatNumber(data.diagnostics.length)} ตำแหน่ง ระบบจะไม่นำยอดเหล่านี้มาคำนวณ กรุณาแก้ master คลัง/พื้นที่ใน SML หากต้องการนำมาใช้</AlertDescription></Alert>
      ) : null}

      <section className="rounded-md border bg-card px-4 py-3" aria-labelledby="stock-setup-steps">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 id="stock-setup-steps" className="text-sm font-semibold">เริ่มใช้งานตามลำดับ</h2>
            <p className="text-xs text-muted-foreground">ระบบจะยังไม่ส่งยอดไป Shopee จนกว่าจะผ่าน Dry-run และเปิดซิงก์</p>
          </div>
          {productCounts.fix > 0 && (
            <Button type="button" variant="link" size="sm" className="h-auto px-0" onClick={() => { setTab('fix'); setPage(1) }}>
              เปิดรายการต้องแก้ ({formatNumber(productCounts.fix)})
            </Button>
          )}
        </div>
        <ol className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {setupSteps.map((step, index) => (
            <li key={step.label} className={cn('flex min-w-0 gap-3 sm:border-l sm:pl-3 first:sm:border-l-0 first:sm:pl-0', step.done ? 'text-foreground' : 'text-muted-foreground')}>
              <span className={cn('flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold', step.done && 'border-success bg-success/15 text-emerald-800 dark:text-emerald-200')}>
                {step.done ? <CheckCircle2 className="h-4 w-4" aria-hidden="true" /> : index + 1}
              </span>
              <div className="min-w-0">
                <p className="text-sm font-medium text-foreground">{step.label}</p>
                <p className="mt-0.5 text-xs">{step.detail}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>

      <Card>
        <CardContent className="grid gap-4 p-4 md:grid-cols-2 xl:grid-cols-12 xl:items-end">
          <div className="space-y-1.5 xl:col-span-3">
            <Label htmlFor="shopee-stock-shop">ร้าน Shopee</Label>
            <Select
              value={shopID ? String(shopID) : undefined}
              onValueChange={(value) => {
                const id = Number(value)
                setShopID(id)
                setPage(1)
                setPreview(null)
                void load(id)
              }}
            >
              <SelectTrigger id="shopee-stock-shop" className="h-10">
                <SelectValue placeholder="เลือกร้าน Shopee" />
              </SelectTrigger>
              <SelectContent>
                {(data?.settings ?? []).map((item) => (
                  <SelectItem key={item.shop_id} value={String(item.shop_id)}>
                    {item.shop_name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5 xl:col-span-2"><Label htmlFor="stock-pct">สัดส่วนส่ง Shopee</Label><div className="relative"><Input id="stock-pct" type="number" min={1} max={100} aria-invalid={!stockPctValid} aria-describedby={!stockPctValid ? 'stock-pct-error' : undefined} value={draft?.stock_pct ?? 80} onChange={(event) => draft && setDraft({ ...draft, stock_pct: Number(event.target.value), enabled: false, dry_run_required: true })} className={cn('pr-8', !stockPctValid && 'border-destructive')} /><span className="absolute right-3 top-2.5 text-sm text-muted-foreground">%</span></div>{!stockPctValid && <p id="stock-pct-error" className="text-xs text-destructive">กรอกตัวเลขระหว่าง 1-100</p>}</div>
          <div className="space-y-1.5 xl:col-span-2">
            <Label>ขอบเขตสต๊อก SML</Label>
            <div className={cn('flex h-10 items-center rounded-md border px-3 text-sm', scopeReady ? 'border-success/50 bg-success/10 text-foreground' : 'text-muted-foreground')}>
              {scopeReady ? `เลือกแล้ว ${formatNumber(draft?.locations.length ?? 0)} พื้นที่` : 'เลือกคลัง / พื้นที่ด้านล่าง'}
            </div>
          </div>
          <div className="flex items-center justify-between gap-3 rounded-md border px-3 py-2 md:col-span-2 xl:col-span-3"><div><p className="text-sm font-medium">ซิงก์อัตโนมัติทุก {formatInterval(draft?.interval_seconds)}</p><p className="text-xs text-muted-foreground">{draft?.dry_run_required ? 'ต้องผ่าน Dry-run ก่อนจึงจะเปิดได้' : 'ปิดแล้วไม่เปลี่ยนยอดที่อยู่ใน Shopee'}</p></div><Switch aria-label="เปิดซิงก์สต๊อกอัตโนมัติ" checked={draft?.enabled ?? false} disabled={!data?.available || !stockPctValid || !scopeReady || !!draft?.dry_run_required || !!draft?.paused_reason} onCheckedChange={(checked) => draft && setDraft({ ...draft, enabled: checked })} /></div>
          <Button className="w-full md:justify-self-end xl:col-span-2" onClick={saveSettings} disabled={!draft || !stockPctValid || !scopeReady || !settingsDirty || !!busy}><Save className="h-4 w-4" />{busy === 'save' ? 'กำลังบันทึก...' : 'บันทึก'}</Button>
        </CardContent>
      </Card>

      {draft && (
        <section className="rounded-md border bg-card p-4">
          <div className="mb-3"><h2 className="text-sm font-semibold">คลังและพื้นที่ที่นำมาคำนวณ</h2><p className="text-xs text-muted-foreground">เลือกอย่างน้อย 1 พื้นที่ ระบบจะแสดงยอดที่ถูกตัดออกใน dry-run</p></div>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{warehouses.map((warehouse) => <div key={warehouse.code} className="rounded-md border p-3"><p className="mb-2 text-sm font-medium">{warehouse.code} · {warehouse.name}</p><div className="space-y-2">{warehouse.locations.map((location) => { const checked = draft.locations.some((item) => locationKey(item) === locationKey({ warehouse: location.warehouse_code, location: location.location_code })); return <label key={`${location.warehouse_code}:${location.location_code}`} className="flex cursor-pointer items-center gap-2 text-sm"><Checkbox checked={checked} onCheckedChange={(value) => toggleLocation(location, value === true)} /><span>{location.location_code} · {location.location_name || 'ไม่ระบุชื่อ'}</span></label> })}</div></div>)}</div>
        </section>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" onClick={syncCatalog} disabled={!data?.available || !!busy}><Boxes className="h-4 w-4" />{busy === 'catalog' ? 'กำลังดึงสินค้า...' : 'อัปเดต Catalog'}</Button>
        <Button variant="outline" onClick={previewImpact} disabled={!!previewDisabledReason || !!busy}><PackageCheck className="h-4 w-4" />{busy === 'preview' ? 'กำลังคำนวณ...' : 'บันทึกและตรวจสต๊อก'}</Button>
        <Button onClick={syncNow} disabled={!!syncDisabledReason || !!busy}><Play className="h-4 w-4" />ซิงก์ตอนนี้</Button>
        <span className="text-xs text-muted-foreground">Catalog ล่าสุด {formatDateTime(selectedSetting?.last_catalog_sync_at)} · ตรวจสต๊อกล่าสุด {formatDateTime(selectedSetting?.last_preview_at)}{draft?.dry_run_required && selectedSetting?.last_preview_at ? ' (ต้องตรวจใหม่)' : ''} · ซิงก์สำเร็จล่าสุด {formatDateTime(selectedSetting?.last_success_at)}</span>
        {nextActionMessage && <p className="basis-full text-xs text-amber-800 dark:text-amber-200"><span className="font-medium">ขั้นถัดไป:</span> {nextActionMessage}</p>}
      </div>

      {draft?.paused_reason && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>ระบบหยุดร้านนี้เพื่อความปลอดภัย</AlertTitle><AlertDescription>{draft.paused_reason}{draft.last_error ? ` · ${draft.last_error}` : ''}</AlertDescription></Alert>}
      {preview && <Alert className={preview.circuit_breaker ? 'border-destructive/50 bg-destructive/5' : preview.blocked_count ? 'border-warning/50 bg-warning/10' : 'border-success/40 bg-success/10'}>{preview.circuit_breaker || preview.blocked_count ? <AlertTriangle className="h-4 w-4" /> : <CheckCircle2 className="h-4 w-4" />}<AlertTitle>{preview.circuit_breaker ? 'ระบบหยุดทั้งร้านเพื่อความปลอดภัย' : preview.blocked_count ? `พร้อมเปิดซิงก์ โดยเว้น ${formatNumber(preview.blocked_count)} รายการที่ต้องแก้` : 'Dry-run ผ่านแล้ว'}</AlertTitle><AlertDescription>ตรวจ {formatNumber(preview.total_count)} รายการ · จะเปลี่ยน {formatNumber(preview.changed_count)} · ไม่เปลี่ยน {formatNumber(preview.skipped_count)} · ยอดนอกขอบเขต {formatNumber(preview.excluded_balance)}{preview.circuit_breaker && ` · ${preview.circuit_breaker}`}</AlertDescription></Alert>}
      {preview?.lines?.some((line) => line.blocked) && <section className="rounded-md border border-warning/40 bg-warning/5"><div className="border-b px-4 py-3"><h2 className="text-sm font-semibold">รายการที่ระบบจะไม่ส่งไป Shopee</h2><p className="text-xs text-muted-foreground">แก้ข้อมูลใน SML หรือกดจับคู่สินค้าแต่ละรายการ แล้วทำ Dry-run ใหม่</p></div><div className="divide-y">{(preview.lines ?? []).filter((line) => line.blocked).slice(0, 20).map((line) => <div key={`${line.item_id}:${line.model_id}`} className="grid gap-1 px-4 py-2 text-sm sm:grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)]"><span className="truncate">{productNames.get(`${line.item_id}:${line.model_id}`) || `Item ${line.item_id}${line.model_id ? ` / Model ${line.model_id}` : ''}`}</span><span className="text-amber-800 dark:text-amber-200">{line.warning_codes.map((code) => WARNING_LABEL[code] || code).join(' · ')}</span></div>)}{preview.blocked_count > 20 && <p className="px-4 py-2 text-xs text-muted-foreground">แสดง 20 จาก {formatNumber(preview.blocked_count)} รายการ ใช้แท็บ “ต้องแก้” เพื่อจัดการต่อ</p>}</div></section>}

      <section className="overflow-hidden rounded-md border bg-card">
        <div className="flex flex-col gap-3 border-b p-3 sm:flex-row sm:items-center sm:justify-between">
          <Tabs value={tab} onValueChange={(value) => { setTab(value as typeof tab); setPage(1) }}>
            <TabsList className="h-auto max-w-full justify-start overflow-x-auto">
              {STATUS_TABS.map((item) => {
                const count = item.key === 'history' ? null : productCounts[item.key]
                return (
                  <TabsTrigger key={item.key} value={item.key} className="shrink-0 gap-2">
                    <span>{item.label}</span>
                    {count != null && <Badge variant="outline" className="h-5 bg-background px-1.5 text-[10px]">{formatNumber(count)}</Badge>}
                  </TabsTrigger>
                )
              })}
            </TabsList>
          </Tabs>
          {tab !== 'history' && <form className="relative w-full sm:w-72" onSubmit={(event) => { event.preventDefault(); setPage(1); setSearch(query.trim()) }}><Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" /><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ค้นหา SKU หรือสินค้า" className="pl-9" /></form>}
        </div>

        {tab === 'history' ? <HistoryList runs={data?.runs ?? []} /> : (
          <>
            <div className="hidden grid-cols-[minmax(220px,1.35fr)_minmax(170px,1fr)_minmax(270px,auto)_110px] gap-3 border-b bg-muted/40 px-4 py-2 text-xs font-medium text-muted-foreground lg:grid">
              <span>สินค้า Shopee</span>
              <span>สินค้าที่จับคู่ใน SML</span>
              <div className="grid grid-cols-3 gap-3 text-right">
                <span>คงเหลือ SML</span>
                <span>Shopee ตอนนี้</span>
                <span>จะส่ง Shopee</span>
              </div>
              <span />
            </div>
            <div className="divide-y">{(data?.products ?? []).map((product) => <ProductLine key={`${product.item_id}:${product.model_id}`} product={product} onMap={() => setMapping(product)} />)}</div>
            {!data?.products.length && (
              <div className="flex flex-col items-center px-4 py-10 text-center">
                <PackageCheck className="mb-3 h-7 w-7 text-muted-foreground" aria-hidden="true" />
                <p className="text-sm font-medium text-foreground">{emptyState.title}</p>
                <p className="mt-1 max-w-md text-sm text-muted-foreground">{emptyState.description}</p>
                {search ? (
                  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={() => { setQuery(''); setSearch(''); setPage(1) }}>ล้างคำค้นหา</Button>
                ) : tab === 'ready' && productCounts.fix > 0 ? (
                  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={() => { setTab('fix'); setPage(1) }}>เปิดรายการต้องแก้ ({formatNumber(productCounts.fix)})</Button>
                ) : !selectedSetting?.last_catalog_sync_at ? (
                  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={syncCatalog} disabled={!data?.available || !!busy}>อัปเดต Catalog</Button>
                ) : null}
              </div>
            )}
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
  const itemName = productItemName(product)
  const optionName = productOptionName(product)
  return (
    <div className="grid gap-3 px-4 py-3 lg:grid-cols-[minmax(220px,1.35fr)_minmax(170px,1fr)_minmax(270px,auto)_110px] lg:items-center">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium" title={itemName}>{itemName}</p>
        {optionName && <p className="mt-0.5 truncate text-xs text-foreground/80" title={optionName}><span className="text-muted-foreground">ตัวเลือก:</span> {optionName}</p>}
        <p className="mt-0.5 truncate text-xs text-muted-foreground">SKU {sku} · Item {product.item_id}{product.model_id ? ` / Model ${product.model_id}` : ''}</p>
        {product.warning_codes.length > 0 && <div className="mt-2 flex flex-wrap gap-1">{product.warning_codes.map((code) => <Badge key={code} variant="outline" className="border-warning/40 bg-warning/10 text-amber-800 dark:text-amber-200">{WARNING_LABEL[code] || code}</Badge>)}</div>}
      </div>
      <div className="min-w-0">
        <p className="truncate text-sm">{product.sml_item_code ? `${product.sml_item_code} · ${product.sml_item_name}` : 'ยังไม่จับคู่'}</p>
        <p className="text-xs text-muted-foreground">{product.sml_unit_code ? `${product.sml_unit_code} · 1 หน่วยขาย = ${formatNumber(product.unit_factor)} หน่วยเล็ก${product.manual_unit_factor ? ' (กำหนดเอง)' : ''}` : 'เลือกสินค้าและหน่วย SML'}</p>
      </div>
      <div className="grid grid-cols-3 gap-3 rounded-md bg-muted/30 p-2 lg:bg-transparent lg:p-0">
        <div className="min-w-0 text-left lg:text-right">
          <p className="text-[11px] text-muted-foreground lg:hidden">คงเหลือ SML</p>
          <p className="font-mono text-sm font-medium">{product.last_preview_balance == null ? 'รอตรวจ' : formatNumber(product.last_preview_balance)}</p>
          {product.last_preview_balance != null && <p className="text-[11px] text-muted-foreground">หน่วยเล็ก</p>}
          {!!product.last_preview_excluded_balance && <p className="text-[11px] text-warning">นอกขอบเขต {formatNumber(product.last_preview_excluded_balance)}</p>}
        </div>
        <div className="min-w-0 text-left lg:text-right">
          <p className="text-[11px] text-muted-foreground lg:hidden">Shopee ตอนนี้</p>
          <p className="font-mono text-sm font-medium">{formatNumber(product.shopee_available)}</p>
          {product.shopee_reserved > 0 && <p className="text-[11px] text-warning">จอง {formatNumber(product.shopee_reserved)}</p>}
        </div>
        <div className="min-w-0 text-left lg:text-right">
          <p className="text-[11px] text-muted-foreground lg:hidden">จะส่ง Shopee</p>
          <p className="font-mono text-sm font-medium">{product.last_preview_target == null ? 'รอตรวจ' : formatNumber(product.last_preview_target)}</p>
          {product.last_preview_target != null && <p className="text-[11px] text-muted-foreground">หน่วยขาย</p>}
        </div>
      </div>
      <Button variant="outline" size="sm" onClick={onMap}><Settings2 className="h-4 w-4" />{product.excluded ? 'แก้ไข' : product.sml_item_code ? 'เปลี่ยน' : 'จับคู่'}</Button>
    </div>
  )
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
  const [confirmExclude, setConfirmExclude] = useState(false)

  useEffect(() => {
    let active = true
    setConfirmExclude(false)
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
  const selectedUnit = units.find((unit) => unit.code === unitCode)
  return (
    <>
      <Dialog open={!!product} onOpenChange={(open) => !open && !busy && onClose()}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>จับคู่สินค้า SML</DialogTitle>
            <DialogDescription>{product ? `${productDisplayName(product)} · SKU ${product.model_sku || product.item_sku || 'ไม่มี'}` : ''}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <form className="flex gap-2" onSubmit={(event) => { event.preventDefault(); void searchCatalog() }}>
              <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ค้นหารหัสหรือชื่อสินค้า SML" />
              <Button type="submit" variant="outline" disabled={busy}><Search className="h-4 w-4" />ค้นหา</Button>
            </form>
            <div className="max-h-52 divide-y overflow-y-auto rounded-md border">
              {results.map((item) => (
                <button key={item.item_code} type="button" onClick={() => selectProduct(item)} className={cn('block w-full px-3 py-2 text-left hover:bg-muted', selected?.item_code === item.item_code && 'bg-primary/10')}>
                  <p className="text-sm font-medium">{item.item_code}</p>
                  <p className="text-xs text-muted-foreground">{item.item_name}</p>
                </button>
              ))}
              {!results.length && <p className="p-4 text-center text-sm text-muted-foreground">{query.trim().length >= 2 ? 'ไม่พบสินค้าที่ตรงกับคำค้นหา' : 'ค้นหาด้วยรหัสหรือชื่อสินค้า แล้วเลือกสินค้าจาก SML'}</p>}
            </div>
            {selected && (
              <div className="space-y-3">
                <div className="rounded-md border bg-muted/30 p-3">
                  <p className="text-xs text-muted-foreground">สินค้าที่เลือก</p>
                  <p className="mt-0.5 text-sm font-medium">{selected.item_code} · {selected.item_name}</p>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="shopee-stock-unit">หน่วยขายบน Shopee</Label>
                  <Select value={unitCode || undefined} onValueChange={setUnitCode}>
                    <SelectTrigger id="shopee-stock-unit" className="h-10"><SelectValue placeholder={units.length ? 'เลือกหน่วย' : 'ไม่พบหน่วยใน SML'} /></SelectTrigger>
                    <SelectContent>
                      {units.length ? units.map((unit) => <SelectItem key={unit.code} value={unit.code}>{unit.code}{unit.name ? ` · ${unit.name}` : ''} ({formatNumber((unit.stand_value ?? 0) / (unit.divide_value || 1))} หน่วยเล็ก)</SelectItem>) : <SelectItem value="__empty" disabled>ไม่พบหน่วยใน SML</SelectItem>}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">ระบบเลือกหน่วยมาตรฐานให้ก่อน กรุณาตรวจสอบว่าตรงกับหน่วยที่ขายบน Shopee</p>
                  {selectedUnit && <p className="text-xs font-medium text-foreground">1 {selectedUnit.code} = {formatNumber((selectedUnit.stand_value ?? 0) / (selectedUnit.divide_value || 1))} หน่วยเล็กใน SML</p>}
                </div>
                <div className="rounded-md border p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div><Label htmlFor="manual-factor">กำหนดอัตราส่วนเอง</Label><p className="text-xs text-muted-foreground">ใช้เฉพาะเมื่อข้อมูลหน่วยใน SML ยังไม่ถูกต้อง ระบบจะบันทึกไว้ในประวัติ</p></div>
                    <Switch id="manual-factor" checked={useManualFactor} onCheckedChange={setUseManualFactor} />
                  </div>
                  {useManualFactor && <Input className="mt-3" inputMode="decimal" type="number" min="1" step="any" value={manualFactor} onChange={(event) => setManualFactor(event.target.value)} placeholder="เช่น 6" />}
                </div>
              </div>
            )}
          </div>
          <DialogFooter className="gap-2 sm:justify-between">
            <Button type="button" variant="outline" className="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive" onClick={() => setConfirmExclude(true)} disabled={busy}>ยกเว้นสินค้านี้</Button>
            <div className="flex gap-2"><Button variant="outline" onClick={onClose} disabled={busy}>ยกเลิก</Button><Button onClick={() => save(false)} disabled={busy || !selected || !unitCode || (useManualFactor && !manualFactor)}>{busy && <Loader2 className="h-4 w-4 animate-spin" />}บันทึกการจับคู่</Button></div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirmExclude}
        onOpenChange={setConfirmExclude}
        title="ยกเว้นสินค้านี้จากการซิงก์?"
        description={product ? `${productDisplayName(product)}\nสินค้านี้จะไม่ถูกส่งยอดสต๊อกไป Shopee จนกว่าจะกลับมาแก้ไขการจับคู่` : undefined}
        confirmLabel="ยืนยันการยกเว้น"
        variant="destructive"
        onConfirm={() => save(true)}
      />
    </>
  )
}
