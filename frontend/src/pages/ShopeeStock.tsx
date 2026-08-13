import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import axios from 'axios'
import {
  AlertTriangle,
  Boxes,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleOff,
  Info,
  Loader2,
  PackageCheck,
  Play,
  RefreshCw,
  Save,
  Search,
  Settings2,
} from 'lucide-react'
import { toast } from 'sonner'
import { Link } from 'react-router-dom'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { SetProductDetailsDialog } from '@/components/catalog/SetProductDetailsDialog'
import { MarketplaceMappingDrawer } from '@/components/marketplace/MarketplaceMappingDrawer'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'
import type { CatalogMatch, CatalogSetComponent, MarketplaceAliasImpact } from '@/types'

type LocationPair = { warehouse: string; location: string }
type StockLocation = { warehouse_code: string; warehouse_name: string; location_code: string; location_name: string }
type Diagnostic = { warehouse: string; location: string; balance_qty: number; code: string }
type StockCatalogUnit = { code: string; name: string; stand_value: number; divide_value: number; ratio: number; row_order: number; line_number: number }
type StockCatalogOption = {
  item_code: string
  item_name: string
  item_type?: number
  standard_unit: string
  set_component_count?: number
  set_definition_hash?: string
  set_document_valid?: boolean
  set_stock_valid?: boolean
  set_warning_codes?: string[]
  set_components?: CatalogSetComponent[]
  units: StockCatalogUnit[]
}
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
  marketplace_alias_id?: string | null
  marketplace_alias_updated_at?: string | null
  shopee_available: number
  shopee_reserved: number
  sml_item_code: string
  sml_item_name: string
  sml_unit_code: string
  sml_unit_name: string
  sml_base_unit_code: string
  sml_base_unit_name: string
  sml_item_type: number
  set_component_count: number
  set_definition_hash?: string
  mapping_set_definition_hash?: string
  set_document_valid: boolean
  set_stock_valid: boolean
  set_components?: CatalogSetComponent[]
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
    item_type: number
    set_definition_hash?: string
    set_components?: Array<{
      item_code: string
      item_name: string
      component_qty: number
      unit_code: string
      balance_unit_code: string
      balance_qty: number
      required_base: number
      possible_sets: number
      bottleneck: boolean
    }>
    bottleneck_item_code?: string
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
  master_target_changed: 'Master เปลี่ยนสินค้า ต้องเลือกหน่วยและทำ Dry-run ใหม่',
  master_inactive: 'Master ถูกปิดใช้งาน กรุณาจับคู่ใหม่',
  set_stock_invalid: 'โครงสร้างสินค้าชุดยังใช้คำนวณสต๊อกไม่ได้',
  set_stock_feature_disabled: 'ยังไม่เปิดการคำนวณสต๊อกสินค้าชุด',
  set_definition_missing: 'ไม่พบส่วนประกอบสินค้าชุด',
  set_definition_changed: 'ส่วนประกอบใน SML เปลี่ยน ต้องทำ Dry-run ใหม่',
  nested_set_not_supported: 'ยังไม่รองรับสินค้าชุดซ้อนกัน',
  set_component_inactive: 'มีส่วนประกอบที่หยุดใช้งาน',
  set_component_not_stock_item: 'มีส่วนประกอบที่ไม่ใช่สินค้าสต๊อก',
  set_component_qty_invalid: 'จำนวนส่วนประกอบไม่ถูกต้อง',
  set_component_unit_invalid: 'หน่วยส่วนประกอบไม่ถูกต้อง',
  set_allocation_invalid: 'สัดส่วนราคาส่วนประกอบไม่ถูกต้อง',
  shared_component_stock: 'ส่วนประกอบถูกใช้ร่วมกับสินค้าชุดอื่น',
  set_product_schema_unsupported: 'ฐานข้อมูล SML นี้ยังไม่รองรับข้อมูลสินค้าชุด',
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

function formatUnitLabel(code?: string, name?: string) {
  const unitCode = code?.trim() ?? ''
  const unitName = name?.trim() ?? ''
  if (unitName && unitCode && unitName !== unitCode) return `${unitName} (${unitCode})`
  return unitName || unitCode
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
  const locations = setting.scope_mode === 'selected' && setting.locations.length === 1 ? [setting.locations[0]] : []
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
  const [catalogInfoOpen, setCatalogInfoOpen] = useState(false)
  const [locationsOpen, setLocationsOpen] = useState(false)
  const [warehouseCode, setWarehouseCode] = useState('')
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
      setWarehouseCode(setting?.locations[0]?.warehouse ?? '')
      setLocationsOpen(false)
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
  const scopeReady = !!draft && draft.scope_mode === 'selected' && draft.locations.length === 1
  const selectedWarehouse = warehouses.find((warehouse) => warehouse.code === warehouseCode)
  const selectedPair = draft?.locations[0]
  const scopeSummary = scopeReady && selectedPair ? `${selectedPair.warehouse} · ${selectedPair.location}` : ''
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
    setDraft(normalizeStockSetting(response.data))
    setLocationsOpen(false)
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

  const selectWarehouse = (value: string) => {
    if (!draft) return
    setWarehouseCode(value)
    const current = draft.locations[0]
    const next = current?.warehouse === value ? [current] : []
    setDraft({
      ...draft,
      scope_mode: next.length > 0 ? 'selected' : 'unconfigured',
      locations: next,
      all_scope_warning_acknowledged: false,
      enabled: false,
      dry_run_required: true,
    })
  }

  const selectLocation = (locationCode: string) => {
    if (!draft || !warehouseCode) return
    setDraft({
      ...draft,
      scope_mode: 'selected',
      locations: [{ warehouse: warehouseCode, location: locationCode }],
      all_scope_warning_acknowledged: false,
      enabled: false,
      dry_run_required: true,
    })
    setLocationsOpen(false)
  }

  const previewDisabledReason = !data?.available
    ? 'ระบบซิงก์สต๊อกยังไม่พร้อมใช้งาน'
    : !draft
      ? 'เลือกร้าน Shopee ก่อน'
      : !stockPctValid
        ? 'กำหนดสัดส่วนส่ง Shopee ระหว่าง 1-100%'
        : !scopeReady
          ? 'เลือก 1 คลังและ 1 พื้นที่เก็บ'
          : ''
  const syncDisabledReason = !draft?.enabled
    ? 'เปิดซิงก์อัตโนมัติและบันทึกก่อนใช้ปุ่มนี้'
    : draft.dry_run_required
      ? 'ตรวจผลกระทบ Dry-run ใหม่ก่อนซิงก์'
      : draft.paused_reason || ''
  const setupSteps = [
    {
      label: 'เลือกขอบเขตสต๊อก',
      detail: scopeReady ? scopeSummary : 'ยังไม่ได้เลือกคลังและพื้นที่เก็บ',
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
          : { title: 'ยังไม่มีสินค้าในแท็บนี้', description: selectedSetting?.last_catalog_sync_at ? 'ลองตรวจแท็บอื่นหรืออัปเดตรายการสินค้าอีกครั้ง' : 'อัปเดตรายการสินค้าเพื่อดึงข้อมูลจาก Shopee และ SML' }

  if (loading && !data) {
    return <div className="flex min-h-[320px] items-center justify-center"><Loader2 className="h-6 w-6 animate-spin text-primary" /></div>
  }

  return (
    <TooltipProvider delayDuration={150}>
      <div className="space-y-3 p-3 sm:p-4">
        <header className="flex items-start justify-between gap-3 sm:items-center">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold text-foreground">ซิงก์สต๊อก Shopee</h1>
              <Badge variant="outline" className={cn('h-6', selectedSetting?.enabled ? 'border-success/50 bg-success/10 text-emerald-800 dark:text-emerald-200' : 'text-muted-foreground')}>
                {selectedSetting?.enabled ? 'กำลังซิงก์อัตโนมัติ' : 'ยังไม่เปิดซิงก์'}
              </Badge>
            </div>
            <p className="mt-0.5 text-sm text-muted-foreground">ส่งสต๊อกตามคลังที่เลือก โดยกันสินค้าไว้ขายหน้าร้านตามสัดส่วนที่กำหนด</p>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="icon" onClick={() => load(shopID)} disabled={loading || !!busy} aria-label="รีเฟรชข้อมูลหน้านี้">
                <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>รีเฟรชข้อมูลหน้านี้</TooltipContent>
          </Tooltip>
        </header>

      {!data?.available && (
        <Alert><CircleOff className="h-4 w-4" /><AlertTitle>ยังไม่เปิดใช้งานสำหรับร้านนี้</AlertTitle><AlertDescription>{data?.availability_text || 'ติดต่อผู้ดูแลระบบเพื่อเปิด Shopee Open API ผ่าน Central Gateway'}</AlertDescription></Alert>
      )}

      {data?.diagnostics?.length ? (
        <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>พบยอดในพื้นที่ว่างหรือไม่อยู่ใน master SML</AlertTitle><AlertDescription>รวม {formatNumber(data.diagnostics.length)} ตำแหน่ง ระบบจะไม่นำยอดเหล่านี้มาคำนวณ กรุณาแก้ master คลัง/พื้นที่ใน SML หากต้องการนำมาใช้</AlertDescription></Alert>
      ) : null}

      <section className="overflow-hidden rounded-md border bg-card" aria-label="ตั้งค่าและควบคุมการซิงก์สต๊อก">
        <div className="grid gap-3 px-3 py-3 sm:grid-cols-2 xl:grid-cols-[repeat(3,minmax(0,1fr))_minmax(220px,1fr)_auto] xl:items-end">
          <div className="space-y-1.5">
            <Label htmlFor="shopee-stock-shop">ร้าน Shopee</Label>
            <Select
              value={shopID ? String(shopID) : undefined}
              onValueChange={(value) => {
                const id = Number(value)
                setShopID(id)
                setPage(1)
                setPreview(null)
                setLocationsOpen(false)
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
          <div className="space-y-1.5"><Label htmlFor="stock-pct">ส่งไป Shopee</Label><div className="relative"><Input id="stock-pct" type="number" min={1} max={100} aria-invalid={!stockPctValid} aria-describedby={!stockPctValid ? 'stock-pct-error' : undefined} value={draft?.stock_pct ?? 80} onChange={(event) => draft && setDraft({ ...draft, stock_pct: Number(event.target.value), enabled: false, dry_run_required: true })} className={cn('pr-8', !stockPctValid && 'border-destructive')} /><span className="absolute right-3 top-2.5 text-sm text-muted-foreground">%</span></div>{!stockPctValid && <p id="stock-pct-error" className="text-xs text-destructive">กรอก 1-100</p>}</div>
          <div className="space-y-1.5">
            <Label>ขอบเขตสต๊อก SML</Label>
            <Popover open={locationsOpen} onOpenChange={setLocationsOpen}>
              <PopoverTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  className={cn(
                    'h-10 w-full justify-between px-3 font-normal',
                    scopeReady ? 'border-success/50 bg-success/10 text-foreground hover:bg-success/10' : 'text-muted-foreground',
                  )}
                  disabled={!draft || !data?.available}
                  aria-label={scopeReady ? `ขอบเขตสต๊อก SML ${scopeSummary}` : 'เลือกขอบเขตสต๊อก SML'}
                >
                  <span className="truncate">{scopeReady ? scopeSummary : 'เลือก 1 คลัง / 1 พื้นที่เก็บ'}</span>
                  <ChevronDown className={cn('h-4 w-4 shrink-0 transition-transform motion-reduce:transition-none', locationsOpen && 'rotate-180')} aria-hidden="true" />
                </Button>
              </PopoverTrigger>
              <PopoverContent align="start" className="w-[26rem] max-w-[calc(100vw-2rem)] p-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="shopee-stock-warehouse">คลัง SML</Label>
                    <Select value={warehouseCode || undefined} onValueChange={selectWarehouse}>
                      <SelectTrigger id="shopee-stock-warehouse" className="h-10">
                        <SelectValue placeholder="เลือกคลัง" />
                      </SelectTrigger>
                      <SelectContent>
                        {warehouses.map((warehouse) => (
                          <SelectItem key={warehouse.code} value={warehouse.code}>
                            {warehouse.code} · {warehouse.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="shopee-stock-location">พื้นที่เก็บ</Label>
                    <Select value={selectedPair?.location || undefined} onValueChange={selectLocation} disabled={!selectedWarehouse}>
                      <SelectTrigger id="shopee-stock-location" className="h-10">
                        <SelectValue placeholder={selectedWarehouse ? 'เลือกพื้นที่เก็บ' : 'เลือกคลังก่อน'} />
                      </SelectTrigger>
                      <SelectContent>
                        {(selectedWarehouse?.locations ?? []).map((location) => (
                          <SelectItem key={location.location_code} value={location.location_code}>
                            {location.location_code} · {location.location_name || 'ไม่ระบุชื่อ'}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <p className="text-xs text-muted-foreground sm:col-span-2">
                    ระบบคำนวณและส่งสต๊อกจากคลังและพื้นที่เก็บคู่นี้เท่านั้น การเปลี่ยนค่าจะปิดซิงก์และต้องตรวจ Dry-run ใหม่
                  </p>
                </div>
              </PopoverContent>
            </Popover>
          </div>
          <div className="flex h-10 items-center justify-between gap-3 rounded-md border px-3"><div className="min-w-0"><p className="truncate text-sm font-medium">ซิงก์ทุก {formatInterval(draft?.interval_seconds)}</p><p className="truncate text-[11px] text-muted-foreground">{draft?.dry_run_required ? 'ต้องตรวจสต๊อกก่อนเปิด' : 'ปิดแล้วไม่เปลี่ยนยอด Shopee'}</p></div><Switch aria-label="เปิดซิงก์สต๊อกอัตโนมัติ" checked={draft?.enabled ?? false} disabled={!data?.available || !stockPctValid || !scopeReady || !!draft?.dry_run_required || !!draft?.paused_reason} onCheckedChange={(checked) => draft && setDraft({ ...draft, enabled: checked })} /></div>
          <Button className="w-full sm:col-span-2 sm:w-auto sm:justify-self-end xl:col-span-1" onClick={saveSettings} disabled={!draft || !stockPctValid || !scopeReady || !settingsDirty || !!busy}><Save className="h-4 w-4" />{busy === 'save' ? 'กำลังบันทึก...' : 'บันทึก'}</Button>
        </div>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t px-3 py-2" aria-labelledby="stock-setup-steps">
          <span id="stock-setup-steps" className="text-xs font-medium text-muted-foreground">สถานะ:</span>
          {setupSteps.map((step) => (
            <span key={step.label} className={cn('inline-flex min-w-0 items-center gap-1.5 text-xs', step.done ? 'text-foreground' : 'text-muted-foreground')} title={step.detail}>
              {step.done ? <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" aria-hidden="true" /> : <span className="h-2 w-2 shrink-0 rounded-full border" aria-hidden="true" />}
              <span className="font-medium">{step.label}</span>
              <span className="hidden text-muted-foreground xl:inline">{step.detail}</span>
            </span>
          ))}
          {productCounts.fix > 0 && (
            <Button type="button" variant="link" size="sm" className="ml-auto h-auto px-0 text-xs" onClick={() => { setTab('fix'); setPage(1) }}>
              เปิดรายการต้องแก้ ({formatNumber(productCounts.fix)})
            </Button>
          )}
        </div>

        <div className="flex flex-col gap-2 border-t px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center">
              <Button variant="outline" size="sm" onClick={syncCatalog} disabled={!data?.available || !!busy}><Boxes className="h-4 w-4" />{busy === 'catalog' ? 'กำลังดึงข้อมูล...' : 'อัปเดตรายการสินค้า'}</Button>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button type="button" variant="ghost" size="icon" className="h-8 w-8" onClick={() => setCatalogInfoOpen(true)} aria-label="อัปเดตรายการสินค้าคืออะไร">
                    <Info className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>อัปเดตรายการสินค้าคืออะไร</TooltipContent>
              </Tooltip>
            </div>
            <Button variant="outline" size="sm" onClick={previewImpact} disabled={!!previewDisabledReason || !!busy}><PackageCheck className="h-4 w-4" />{busy === 'preview' ? 'กำลังคำนวณ...' : 'บันทึกและตรวจสต๊อก'}</Button>
            <Button size="sm" onClick={syncNow} disabled={!!syncDisabledReason || !!busy}><Play className="h-4 w-4" />ซิงก์ตอนนี้</Button>
          </div>
          <span className="min-w-0 text-xs text-muted-foreground sm:max-w-[48%] sm:text-right xl:max-w-none">สินค้า {formatDateTime(selectedSetting?.last_catalog_sync_at)} · ตรวจ {formatDateTime(selectedSetting?.last_preview_at)}{draft?.dry_run_required && selectedSetting?.last_preview_at ? ' (ต้องตรวจใหม่)' : ''} · ซิงก์ {formatDateTime(selectedSetting?.last_success_at)}</span>
          {nextActionMessage && <p className="basis-full text-xs text-amber-800 dark:text-amber-200 sm:hidden"><span className="font-medium">ขั้นถัดไป:</span> {nextActionMessage}</p>}
        </div>
        {nextActionMessage && <p className="hidden border-t px-3 py-1.5 text-xs text-amber-800 dark:text-amber-200 sm:block"><span className="font-medium">ขั้นถัดไป:</span> {nextActionMessage}</p>}
      </section>

      {draft?.paused_reason && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>ระบบหยุดร้านนี้เพื่อความปลอดภัย</AlertTitle><AlertDescription>{draft.paused_reason}{draft.last_error ? ` · ${draft.last_error}` : ''}</AlertDescription></Alert>}
      {preview && <Alert className={preview.circuit_breaker ? 'border-destructive/50 bg-destructive/5' : preview.blocked_count ? 'border-warning/50 bg-warning/10' : 'border-success/40 bg-success/10'}>{preview.circuit_breaker || preview.blocked_count ? <AlertTriangle className="h-4 w-4" /> : <CheckCircle2 className="h-4 w-4" />}<AlertTitle>{preview.circuit_breaker ? 'ระบบหยุดทั้งร้านเพื่อความปลอดภัย' : preview.blocked_count ? `พร้อมเปิดซิงก์ โดยเว้น ${formatNumber(preview.blocked_count)} รายการที่ต้องแก้` : 'Dry-run ผ่านแล้ว'}</AlertTitle><AlertDescription>ตรวจ {formatNumber(preview.total_count)} รายการ · จะเปลี่ยน {formatNumber(preview.changed_count)} · ไม่เปลี่ยน {formatNumber(preview.skipped_count)} · ยอดนอกขอบเขต {formatNumber(preview.excluded_balance)}{preview.circuit_breaker && ` · ${preview.circuit_breaker}`}</AlertDescription></Alert>}
      {preview?.lines?.some((line) => line.blocked) && <section className="rounded-md border border-warning/40 bg-warning/5"><div className="border-b px-4 py-3"><h2 className="text-sm font-semibold">รายการที่ระบบจะไม่ส่งไป Shopee</h2><p className="text-xs text-muted-foreground">แก้ข้อมูลใน SML หรือกดจับคู่สินค้าแต่ละรายการ แล้วทำ Dry-run ใหม่</p></div><div className="divide-y">{(preview.lines ?? []).filter((line) => line.blocked).slice(0, 20).map((line) => <div key={`${line.item_id}:${line.model_id}`} className="grid gap-1 px-4 py-2 text-sm sm:grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)]"><span className="truncate">{productNames.get(`${line.item_id}:${line.model_id}`) || `Item ${line.item_id}${line.model_id ? ` / Model ${line.model_id}` : ''}`}</span><span className="text-amber-800 dark:text-amber-200">{line.warning_codes.map((code) => WARNING_LABEL[code] || code).join(' · ')}</span></div>)}{preview.blocked_count > 20 && <p className="px-4 py-2 text-xs text-muted-foreground">แสดง 20 จาก {formatNumber(preview.blocked_count)} รายการ ใช้แท็บ “ต้องแก้” เพื่อจัดการต่อ</p>}</div></section>}
      {preview?.lines?.some((line) => line.item_type === 3) && <SetStockPreviewList lines={preview.lines.filter((line) => line.item_type === 3)} productNames={productNames} />}

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
            <div className="hidden grid-cols-[minmax(260px,1.45fr)_minmax(190px,1fr)_minmax(260px,auto)_96px] gap-3 border-b bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground lg:grid">
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
                  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={syncCatalog} disabled={!data?.available || !!busy}>อัปเดตรายการสินค้า</Button>
                ) : null}
              </div>
            )}
            <div className="flex items-center justify-between border-t px-3 py-2"><span className="text-xs text-muted-foreground">ทั้งหมด {formatNumber(data?.products_total)} รายการ</span><div className="flex items-center gap-2"><Button variant="outline" size="icon" title="หน้าก่อนหน้า" disabled={page <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}><ChevronLeft className="h-4 w-4" /></Button><span className="text-sm">{page} / {pageCount}</span><Button variant="outline" size="icon" title="หน้าถัดไป" disabled={page >= pageCount} onClick={() => setPage((value) => value + 1)}><ChevronRight className="h-4 w-4" /></Button></div></div>
          </>
        )}
      </section>

      <MappingDialog product={mapping} shopID={shopID} onClose={() => setMapping(null)} onSaved={async () => { setMapping(null); setPreview(null); await load(shopID) }} />
      <CatalogInfoDialog open={catalogInfoOpen} onOpenChange={setCatalogInfoOpen} />
      </div>
    </TooltipProvider>
  )
}

function ProductLine({ product, onMap }: { product: ProductRow; onMap: () => void }) {
  const [setDetailsOpen, setSetDetailsOpen] = useState(false)
  const sku = product.model_sku || product.item_sku || 'ไม่มี SKU'
  const itemName = productItemName(product)
  const optionName = productOptionName(product)
  const sellingUnit = formatUnitLabel(product.sml_unit_code, product.sml_unit_name)
  const baseUnit = formatUnitLabel(product.sml_base_unit_code, product.sml_base_unit_name)
  const conversionText = sellingUnit
    ? baseUnit && !(product.unit_factor === 1 && sellingUnit === baseUnit)
      ? `1 ${sellingUnit} = ${formatNumber(product.unit_factor)} ${baseUnit}`
      : `หน่วยที่ใช้คำนวณ: ${sellingUnit}`
    : ''
  const shopeeIdentity = `SKU ${sku} · Item ${product.item_id}${product.model_id ? ` / Model ${product.model_id}` : ''}`
  return (
    <>
    <div className="grid gap-3 px-3 py-2 lg:grid-cols-[minmax(260px,1.45fr)_minmax(190px,1fr)_minmax(260px,auto)_96px] lg:items-center">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium leading-5" title={itemName}>{itemName}</p>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs">
          {optionName && (
            <span className="max-w-[45%] shrink-0 truncate rounded-sm border border-primary/25 bg-primary/10 px-1.5 py-0.5 font-semibold text-primary" title={`ตัวเลือกสินค้า: ${optionName}`}>
              ตัวเลือก: {optionName}
            </span>
          )}
          <span className="min-w-0 truncate text-muted-foreground" title={shopeeIdentity}>{shopeeIdentity}</span>
        </div>
        {product.warning_codes.length > 0 && <div className="mt-1 flex flex-wrap gap-1">{product.warning_codes.map((code) => <Badge key={code} variant="outline" className="border-warning/40 bg-warning/10 text-amber-800 dark:text-amber-200">{WARNING_LABEL[code] || code}</Badge>)}</div>}
      </div>
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-1.5">
          <p className="min-w-0 truncate text-sm">{product.sml_item_code ? `${product.sml_item_code} · ${product.sml_item_name}` : 'ยังไม่จับคู่'}</p>
          {product.sml_item_type === 3 && (
            <button type="button" className="shrink-0" onClick={() => setSetDetailsOpen(true)} aria-label={`ดูส่วนประกอบสินค้าชุด ${product.sml_item_code}`}>
              <Badge variant="outline" className="border-primary/30 bg-primary/10 text-primary"><Boxes className="mr-1 h-3 w-3" />ชุด {formatNumber(product.set_component_count)}</Badge>
            </button>
          )}
        </div>
        <p className="text-xs text-muted-foreground">{product.sml_unit_code ? `${conversionText}${product.manual_unit_factor ? ' (กำหนดเอง)' : ''}` : 'เลือกสินค้าและหน่วย SML'}</p>
        {product.marketplace_alias_id && <Link to={`/marketplace-aliases?q=${encodeURIComponent(product.model_sku || product.item_sku || String(product.item_id))}`} className="mt-0.5 inline-flex text-[11px] font-medium text-primary hover:underline">ดูใน Product Master</Link>}
      </div>
      <div className="grid grid-cols-3 gap-3 rounded-md bg-muted/30 p-2 lg:bg-transparent lg:p-0">
        <div className="min-w-0 text-left lg:text-right">
          <p className="text-[11px] text-muted-foreground lg:hidden">คงเหลือ SML</p>
          <p className="whitespace-nowrap text-sm font-medium"><span className="font-mono">{product.last_preview_balance == null ? 'รอตรวจ' : formatNumber(product.last_preview_balance)}</span>{product.last_preview_balance != null && baseUnit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{baseUnit}</span>}</p>
          {!!product.last_preview_excluded_balance && <p className="text-[11px] text-warning">นอกขอบเขต {formatNumber(product.last_preview_excluded_balance)}</p>}
        </div>
        <div className="min-w-0 text-left lg:text-right">
          <p className="text-[11px] text-muted-foreground lg:hidden">Shopee ตอนนี้</p>
          <p className="whitespace-nowrap text-sm font-medium"><span className="font-mono">{formatNumber(product.shopee_available)}</span>{sellingUnit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{sellingUnit}</span>}</p>
          {product.shopee_reserved > 0 && <p className="text-[11px] text-warning">จอง {formatNumber(product.shopee_reserved)}</p>}
        </div>
        <div className="min-w-0 text-left lg:text-right">
          <p className="text-[11px] text-muted-foreground lg:hidden">จะส่ง Shopee</p>
          <p className="whitespace-nowrap text-sm font-medium"><span className="font-mono">{product.last_preview_target == null ? 'รอตรวจ' : formatNumber(product.last_preview_target)}</span>{product.last_preview_target != null && sellingUnit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{sellingUnit}</span>}</p>
        </div>
      </div>
      <Button variant="outline" size="sm" className="px-2" onClick={onMap}><Settings2 className="h-4 w-4" />{product.excluded ? 'แก้ไข' : product.sml_item_code ? 'เปลี่ยน' : 'จับคู่'}</Button>
    </div>
    <SetProductDetailsDialog
      open={setDetailsOpen}
      onOpenChange={setSetDetailsOpen}
      itemCode={product.sml_item_code}
      itemName={product.sml_item_name}
      components={product.set_components}
      documentValid={product.set_document_valid}
      stockValid={product.set_stock_valid}
      warningCodes={product.warning_codes.filter((code) => code.startsWith('set_') || code === 'nested_set_not_supported')}
      showStockStatus
    />
    </>
  )
}

function availableSetCount(line: Preview['lines'][number]) {
  const components = line.set_components ?? []
  return components.length > 0 ? Math.min(...components.map((item) => item.possible_sets)) : 0
}

function SetStockPreviewList({ lines, productNames }: { lines: Preview['lines']; productNames: Map<string, string> }) {
  return (
    <section className="overflow-hidden rounded-md border bg-card">
      <div className="border-b px-3 py-2">
        <h2 className="text-sm font-semibold">ผลคำนวณสินค้าชุด</h2>
        <p className="text-xs text-muted-foreground">จำนวนที่ส่ง Shopee จำกัดด้วยส่วนประกอบที่ประกอบสินค้าได้น้อยที่สุด</p>
      </div>
      <div className="divide-y">
        {lines.slice(0, 20).map((line) => (
          <details key={`${line.item_id}:${line.model_id}`} className="group px-3 py-2">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm">
              <span className="min-w-0 truncate font-medium">{productNames.get(`${line.item_id}:${line.model_id}`) || line.sml_item_code}</span>
              <span className="shrink-0 text-xs text-muted-foreground">ประกอบได้ {formatNumber(availableSetCount(line))} ชุด · จะส่ง {formatNumber(line.target_stock)} · คอขวด {line.bottleneck_item_code || '-'}</span>
            </summary>
            <div className="mt-2 overflow-hidden rounded-md border">
              <div className="hidden grid-cols-[90px_minmax(160px,1fr)_100px_110px] gap-3 border-b bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground sm:grid">
                <span>รหัส</span><span>ส่วนประกอบ</span><span className="text-right">คงเหลือ SML</span><span className="text-right">ประกอบได้</span>
              </div>
              {(line.set_components ?? []).map((component) => (
                <div key={component.item_code} className={cn('grid gap-1 px-3 py-1.5 text-xs sm:grid-cols-[90px_minmax(160px,1fr)_100px_110px] sm:items-center sm:gap-3', component.bottleneck && 'bg-warning/10')}>
                  <span className="font-mono font-medium">{component.item_code}</span>
                  <span className="min-w-0" title={component.item_name}>
                    <span className="block truncate">{component.item_name || '-'}</span>
                    <span className="block truncate text-[11px] text-muted-foreground">ใช้ต่อชุด {formatNumber(component.component_qty)} {component.unit_code}{component.required_base !== component.component_qty ? ` = ${formatNumber(component.required_base)} ${component.balance_unit_code || 'หน่วยเล็ก'}` : ''}</span>
                  </span>
                  <span className="tabular-nums sm:text-right">{formatNumber(component.balance_qty)} {component.balance_unit_code || 'หน่วยเล็ก'}</span>
                  <span className="tabular-nums sm:text-right">{formatNumber(component.possible_sets)} ชุด{component.bottleneck ? ' · คอขวด' : ''}</span>
                </div>
              ))}
            </div>
          </details>
        ))}
      </div>
      {lines.length > 20 && <p className="border-t px-3 py-2 text-xs text-muted-foreground">แสดง 20 จาก {formatNumber(lines.length)} สินค้าชุด</p>}
    </section>
  )
}

function HistoryList({ runs }: { runs: SyncRun[] }) {
  if (!runs.length) return <div className="px-4 py-12 text-center text-sm text-muted-foreground">ยังไม่มีประวัติ</div>
  return <div className="divide-y">{runs.map((run) => <div key={run.id} className="grid gap-2 px-4 py-3 sm:grid-cols-[140px_1fr_auto] sm:items-center"><div><Badge variant="outline">{run.run_type === 'catalog' ? 'รายการสินค้า' : run.run_type === 'preview' ? 'Dry-run' : 'Sync'}</Badge><p className="mt-1 text-xs text-muted-foreground">{formatDateTime(run.started_at)}</p></div><div className="text-sm">ตรวจ {formatNumber(run.total_count)} · เปลี่ยน {formatNumber(run.changed_count)} · block {formatNumber(run.blocked_count)} · error {formatNumber(run.error_count)}{run.error_message && <p className="mt-1 text-xs text-destructive">{run.error_message}</p>}</div><Badge variant={run.status === 'success' ? 'default' : 'outline'}>{run.status}</Badge></div>)}</div>
}

function CatalogInfoDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>อัปเดตรายการสินค้าคืออะไร</DialogTitle>
          <DialogDescription>ดึงข้อมูลล่าสุดมาไว้ใน Nexflow เพื่อเตรียมตรวจและจับคู่สินค้า</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 text-sm">
          <div>
            <p className="font-medium text-foreground">ข้อมูลที่ระบบอัปเดต</p>
            <ul className="mt-1 list-disc space-y-1 pl-5 text-muted-foreground">
              <li>สินค้า ตัวเลือก SKU และสต๊อกปัจจุบันจาก Shopee</li>
              <li>รหัสสินค้า บาร์โค้ด และหน่วยนับจาก SML</li>
              <li>สถานะการจับคู่ เพื่อแยกรายการพร้อมซิงก์และรายการที่ต้องแก้</li>
            </ul>
          </div>
          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>ขั้นตอนนี้ยังไม่แก้สต๊อก</AlertTitle>
            <AlertDescription>ระบบจะไม่สร้างสินค้า ไม่แก้ข้อมูลใน SML และไม่ส่งยอดไป Shopee จนกว่าจะตรวจสต๊อกและกดซิงก์</AlertDescription>
          </Alert>
        </div>
        <DialogFooter>
          <Button type="button" onClick={() => onOpenChange(false)}>เข้าใจแล้ว</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MappingDialog({ product, shopID, onClose, onSaved }: { product: ProductRow | null; shopID: number; onClose: () => void; onSaved: () => Promise<void> }) {
  const [selected, setSelected] = useState<StockCatalogOption | null>(null)
  const [units, setUnits] = useState<StockCatalogUnit[]>([])
  const [unitCode, setUnitCode] = useState('')
  const [useManualFactor, setUseManualFactor] = useState(false)
  const [manualFactor, setManualFactor] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmExclude, setConfirmExclude] = useState(false)
  const [saveImpact, setSaveImpact] = useState<MarketplaceAliasImpact | null>(null)

  useEffect(() => {
    let active = true
    setConfirmExclude(false)
    setSaveImpact(null)
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

  const selectProduct = async (picked: CatalogMatch) => {
    setBusy(true)
    try {
      const response = await client.get<{ items: StockCatalogOption[] }>('/api/settings/shopee-stock/catalog-search', { params: { q: picked.item_code } })
      const item = (response.data.items ?? []).find((candidate) => candidate.item_code === picked.item_code)
      if (!item) {
        toast.error('ไม่พบข้อมูลหน่วยของสินค้านี้ กรุณาอัปเดตรายการสินค้าแล้วลองใหม่')
        return
      }
      setSelected(item)
      const next = item.units ?? []
      setUnits(next)
      setUnitCode(next.find((unit) => unit.code === item.standard_unit)?.code || next[0]?.code || '')
    } catch (error) {
      toast.error(errorText(error))
    } finally {
      setBusy(false)
    }
  }
  const validateSelection = () => {
    const parsedManualFactor = Number(manualFactor)
    if (!selected || !unitCode) return null
    if (useManualFactor && (!Number.isFinite(parsedManualFactor) || parsedManualFactor < 1)) {
      toast.error('อัตราส่วนที่กำหนดเองต้องไม่น้อยกว่า 1')
      return null
    }
    return parsedManualFactor
  }
  const previewSave = async () => {
    if (!product || !selected || validateSelection() == null) return
    setBusy(true)
    try {
      const response = await client.post<MarketplaceAliasImpact>('/api/marketplace-aliases/impact-preview', {
        alias_id: product.marketplace_alias_id || '',
        source: 'shopee',
        account_key: `shop:${shopID}`,
        external_item_id: String(product.item_id),
        external_variant_id: String(product.model_id),
        source_sku: product.model_sku || product.item_sku || '',
        raw_name: productDisplayName(product),
        item_code: selected.item_code,
      })
      setSaveImpact(response.data)
    } catch (error) {
      toast.error(errorText(error))
    } finally {
      setBusy(false)
    }
  }
  const save = async (excluded = false) => {
    if (!product || (!excluded && (!selected || !unitCode))) return
    const parsedManualFactor = excluded ? 0 : validateSelection()
    if (!excluded && parsedManualFactor == null) return
    setBusy(true)
    try {
      await client.put(`/api/settings/shopee-stock/${shopID}/mappings/${product.item_id}/${product.model_id}`, {
        sml_item_code: excluded ? '' : selected?.item_code,
        sml_unit_code: excluded ? '' : unitCode,
        manual_unit_factor: !excluded && useManualFactor ? parsedManualFactor : null,
        excluded,
        updated_at: product.updated_at,
        marketplace_alias_id: product.marketplace_alias_id || '',
        marketplace_alias_updated_at: product.marketplace_alias_updated_at || null,
      })
      toast.success(excluded ? 'ยกเว้นสินค้านี้แล้ว' : 'บันทึก Product Master แล้ว ต้อง Dry-run ใหม่')
      notifyWorkQueueChanged()
      await onSaved()
    } catch (error) {
      toast.error(errorText(error))
    } finally {
      setBusy(false)
    }
  }
  const selectedUnit = units.find((unit) => unit.code === unitCode)
  const baseUnit = useMemo(() => [...units].sort((left, right) =>
    left.row_order - right.row_order || left.line_number - right.line_number || left.code.localeCompare(right.code),
  )[0], [units])
  const baseUnitLabel = formatUnitLabel(baseUnit?.code, baseUnit?.name)
  const selectedUnitLabel = formatUnitLabel(selectedUnit?.code, selectedUnit?.name)
  const selectedFactor = selectedUnit ? (selectedUnit.stand_value ?? 0) / (selectedUnit.divide_value || 1) : 0
  return (
    <>
      <MarketplaceMappingDrawer
        open={!!product}
        rawName={product ? productDisplayName(product) : ''}
        currentCode={product?.sml_item_code || ''}
        currentUnit={product?.sml_unit_code || ''}
        rawNameLabel={product ? `สินค้า Shopee · SKU ${product.model_sku || product.item_sku || 'ไม่มี'}` : 'สินค้า Shopee'}
        closeOnPick={false}
        onPick={(_, __, picked) => { if (picked) void selectProduct(picked) }}
        onOpenChange={(open) => !open && !busy && onClose()}
        footer={(
          <div className="flex flex-wrap justify-between gap-2">
            <Button type="button" variant="outline" className="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive" onClick={() => setConfirmExclude(true)} disabled={busy}>ยกเว้นสินค้านี้</Button>
            <div className="flex gap-2"><Button variant="outline" onClick={onClose} disabled={busy}>ยกเลิก</Button><Button onClick={previewSave} disabled={busy || !selected || !unitCode || (useManualFactor && !manualFactor)}>{busy && <Loader2 className="h-4 w-4 animate-spin" />}ตรวจผลกระทบ</Button></div>
          </div>
        )}
      >
        {selected && (
              <div className="mt-4 space-y-3 border-t pt-4">
                <div className="rounded-md border bg-muted/30 p-3">
                  <p className="text-xs text-muted-foreground">สินค้าที่เลือก</p>
                  <p className="mt-0.5 text-sm font-medium">{selected.item_code} · {selected.item_name}</p>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="shopee-stock-unit">หน่วย SML ต่อสินค้า Shopee 1 ชิ้น</Label>
                  <Select value={unitCode || undefined} onValueChange={setUnitCode}>
                    <SelectTrigger id="shopee-stock-unit" className="h-10"><SelectValue placeholder={units.length ? 'เลือกหน่วย' : 'ไม่พบหน่วยใน SML'} /></SelectTrigger>
                    <SelectContent>
                      {units.length ? units.map((unit) => <SelectItem key={unit.code} value={unit.code}>{formatUnitLabel(unit.code, unit.name)}{baseUnitLabel ? ` · เท่ากับ ${formatNumber((unit.stand_value ?? 0) / (unit.divide_value || 1))} ${baseUnitLabel}` : ''}</SelectItem>) : <SelectItem value="__empty" disabled>ไม่พบหน่วยใน SML</SelectItem>}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">เลือกหน่วย SML ที่ต้องตัด เมื่อสินค้า Shopee รุ่นนี้ขายได้ 1 ชิ้น</p>
                  {selectedUnit && (
                    <p className="text-xs font-medium text-foreground">
                      Shopee 1 ชิ้น = 1 {selectedUnitLabel}{baseUnitLabel && (selectedFactor !== 1 || selectedUnitLabel !== baseUnitLabel) ? ` = ${formatNumber(selectedFactor)} ${baseUnitLabel}` : ''}
                    </p>
                  )}
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
      </MarketplaceMappingDrawer>
      <ConfirmDialog
        open={confirmExclude}
        onOpenChange={setConfirmExclude}
        title="ยกเว้นสินค้านี้จากการซิงก์?"
        description={product ? `${productDisplayName(product)}\nสินค้านี้จะไม่ถูกส่งยอดสต๊อกไป Shopee จนกว่าจะกลับมาแก้ไขการจับคู่` : undefined}
        confirmLabel="ยืนยันการยกเว้น"
        variant="destructive"
        onConfirm={() => save(true)}
      />
      <ConfirmDialog
        open={saveImpact !== null}
        onOpenChange={(open) => !open && setSaveImpact(null)}
        title="ยืนยัน Product Master และหน่วยสต๊อก?"
        description={saveImpact && product && selected ? `${productDisplayName(product)}\nสินค้า SML: ${selected.item_code} · ${selected.item_name}\nบิลเปิดที่จะอัปเดต: ${saveImpact.open_items.toLocaleString()} รายการ ใน ${saveImpact.open_bills.toLocaleString()} บิล\nStock mapping ที่เกี่ยวข้อง: ${saveImpact.stock_mappings.toLocaleString()} รายการ${saveImpact.stock_conflicts ? `\nพบสินค้าซ้ำใน Stock ${saveImpact.stock_conflicts.toLocaleString()} รายการ` : ''}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน และต้องทำ Dry-run ก่อนซิงก์` : undefined}
        confirmLabel="บันทึกและบังคับ Dry-run"
        onConfirm={() => save(false)}
      />
    </>
  )
}
