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
  GitMerge,
  Info,
  Loader2,
  MoreHorizontal,
  PackageCheck,
  Play,
  RefreshCw,
  Save,
  Search,
  Settings2,
  Warehouse,
} from 'lucide-react'
import { toast } from 'sonner'
import { Link } from 'react-router-dom'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { SetProductDetailsDialog } from '@/components/catalog/SetProductDetailsDialog'
import { MarketplaceMappingDrawer } from '@/components/marketplace/MarketplaceMappingDrawer'
import { MarketplaceQuantityField, quantityModeFromMultiplier, type MarketplaceQuantityMode } from '@/components/marketplace/MarketplaceQuantityField'
import { StockSchedulePicker, type StockScheduleValue } from '@/components/shopee-stock/StockSchedulePicker'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
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
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { marketplaceImpactFormulaLines } from '@/lib/marketplace-impact'
import { marketplaceStockPauseCopy } from '@/lib/marketplace-stock-policy'
import { buildShopeeStockGroupSummary } from '@/lib/shopee-stock-group-summary'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'
import { useAuthStore } from '@/store/auth'
import type { CatalogMatch, CatalogSetComponent, MarketplaceAliasImpact, MarketplaceStockPolicyJob } from '@/types'

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
type StockPolicy = 'managed' | 'zeroing' | 'disabled_zero' | 'manual_unmanaged' | 'blocked'
type StockSetting = StockScheduleValue & {
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
  next_run_at?: string
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
  marketplace_conversion_status?: string
  marketplace_stock_policy?: 'managed' | 'zeroing' | 'disabled_zero' | 'manual_unmanaged' | 'blocked'
  marketplace_sales_enabled: boolean
  marketplace_quantity_multiplier: number
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
  shared_pool_enabled: boolean
  pool_allocation_pct: number
  excluded: boolean
  warning_codes: string[]
  last_preview_balance?: number
  last_preview_excluded_balance?: number
  last_preview_excluded_locations?: ExcludedLocation[]
  last_preview_min_qty?: number
  last_preview_max_qty?: number
  last_preview_target?: number
  last_preview_pending_qty?: number
  last_preview_pool_base_target?: number
  last_preview_sml_physical_qty?: number
  last_preview_sml_outstanding_so_qty?: number
  last_preview_sml_usable_qty?: number
  last_preview_calculation_usable_qty?: number
  last_preview_availability_version?: string
  last_preview_source_snapshot_at?: string
  last_preview_source_fingerprint?: string
  last_preview_availability_reason?: string
  last_success_target?: number
  updated_at: string
}
type ProductGroup = {
	item_id: number
	item_name: string
	item_sku: string
	variant_count: number
	ready_count: number
	fix_count: number
	excluded_count: number
	summary_count: number
	sml_usable_total?: number | null
	sml_base_unit_code?: string
	sml_base_unit_name?: string
	sml_total_status: string
	shopee_stock_total: number
	target_stock_total?: number | null
	target_count: number
	changed_count: number
	updated_at: string
}
type CursorPage<T> = { data: T[]; has_more: boolean; next_cursor: string }
type StockOperationResult = {
  tone: 'success' | 'warning'
  title: string
  detail: string
}
type SharedPoolMember = {
  item_id: number
  model_id: number
  item_name: string
  model_name: string
  item_sku: string
  model_sku: string
  shopee_available: number
  shopee_reserved: number
  sml_unit_code: string
  sml_unit_name: string
  sml_base_unit_code: string
  sml_base_unit_name: string
  unit_factor: number
  shared_pool_enabled: boolean
  pool_allocation_pct: number
  last_preview_balance?: number
  last_preview_pending_qty?: number
  last_preview_target?: number
  last_preview_pool_base_target?: number
  updated_at: string
}
type SharedPool = {
  shop_id: number
  sml_item_code: string
  sml_item_name: string
  stock_pct: number
  configured: boolean
  allocation_total: number
  members: SharedPoolMember[]
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
  processed_count?: number
  progress_pct?: number
  summary?: Omit<Preview, 'lines'>
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
  excluded_locations?: Array<{
    item_code?: string
    item_name?: string
    warehouse_code: string
    warehouse_name: string
    location_code: string
    location_name: string
    unit_code: string
    balance_qty: number
  }>
  excluded_items?: ExcludedLocation[]
  excluded_items_total?: number
  excluded_negative_items_total?: number
  circuit_breaker?: string
  availability_mode?: string
  availability_version?: string
  source_fingerprint?: string
  source_snapshot_min_at?: string
  source_snapshot_max_at?: string
  lines: Array<{
    item_id: number
    model_id: number
    sml_item_code: string
    scope_balance: number
    excluded_balance: number
    excluded_locations?: ExcludedLocation[]
    min_qty: number
    max_qty: number
    unit_factor: number
    current_stock: number
    reserved_stock: number
    pending_base_qty: number
    sml_physical_qty: number
    sml_outstanding_so_qty: number
    sml_usable_qty: number
    calculation_usable_qty: number
    availability_version?: string
    source_snapshot_at?: string
    availability_reason?: string
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
type ExcludedLocation = NonNullable<Preview['excluded_locations']>[number]
type PreviewRunLine = { line_order: number; detail: Preview['lines'][number] }

const STATUS_TABS = [
  { key: 'ready', label: 'พร้อมซิงก์' },
  { key: 'fix', label: 'ต้องแก้' },
  { key: 'excluded', label: 'ยกเว้น' },
  { key: 'history', label: 'ประวัติ' },
] as const

const STOCK_COLUMN_LABELS = {
  sml: 'SML พร้อมใช้',
  shopee: 'สต๊อก Shopee',
  target: 'สต๊อกที่จะส่งไป Shopee',
} as const

const WARNING_LABEL: Record<string, string> = {
  sku_not_found: 'ไม่พบ SKU ใน SML',
  unit_factor_missing: 'หน่วยนับไม่มีอัตราส่วน',
  unit_factor_below_one: 'อัตราส่วนต่ำกว่า 1',
  unit_ratio_mismatch: 'ratio ไม่ตรงกับ stand/divide',
  unit_not_found: 'ไม่พบหน่วยที่เลือกใน SML แล้ว',
  duplicate_sml_item: 'สินค้า SML เดียวกันผูกกับหลายรายการ Shopee',
  stock_balance_missing: 'ไม่พบยอดคงเหลือ',
  reserved_stock_exceeds_target: 'สต๊อกจอง Shopee สูงกว่าเป้าหมาย',
  master_target_changed: 'สินค้า SML เปลี่ยน กรุณาเลือกหน่วยและตรวจสอบสต๊อกใหม่',
  master_inactive: 'Master ถูกปิดใช้งาน กรุณาจับคู่ใหม่',
  set_stock_invalid: 'โครงสร้างสินค้าชุดยังใช้คำนวณสต๊อกไม่ได้',
  set_stock_feature_disabled: 'ยังไม่เปิดการคำนวณสต๊อกสินค้าชุด',
  set_definition_missing: 'ไม่พบส่วนประกอบสินค้าชุด',
  set_definition_changed: 'ส่วนประกอบใน SML เปลี่ยน กรุณาตรวจสอบสต๊อกใหม่',
  nested_set_not_supported: 'ยังไม่รองรับสินค้าชุดซ้อนกัน',
  set_component_inactive: 'มีส่วนประกอบที่หยุดใช้งาน',
  set_component_not_stock_item: 'มีส่วนประกอบที่ไม่ใช่สินค้าสต๊อก',
  set_component_qty_invalid: 'จำนวนส่วนประกอบไม่ถูกต้อง',
  set_component_unit_invalid: 'หน่วยส่วนประกอบไม่ถูกต้อง',
  set_allocation_invalid: 'สัดส่วนราคาส่วนประกอบไม่ถูกต้อง',
  shared_component_stock: 'ส่วนประกอบถูกใช้ร่วมกับสินค้าชุดอื่น',
  shared_pool_invalid: 'การแบ่งสต๊อกร่วมกันไม่ถูกต้อง',
  pending_orders_exceed_sml_stock: 'ออเดอร์ที่ยังไม่ลง SML มากกว่าสต๊อกคงเหลือ',
  conversion_needs_review: 'หน่วยหรือ conversion ยังไม่ผ่านการยืนยัน',
  conversion_stale: 'หน่วยหรือ conversion อ้างอิง Catalog รุ่นเก่า',
  conversion_blocked: 'conversion ถูกบล็อกเพื่อความปลอดภัย',
  stock_policy_blocked: 'กดตรวจสอบสต๊อกใหม่เพื่อเตรียมรายการนี้',
  stock_policy_zeroing: 'กำลังตั้ง stock เป็น 0 ก่อนปิด',
  stock_policy_disabled_zero: 'ปิดแล้วและยืนยัน stock 0',
  stock_policy_manual_unmanaged: 'ผู้ใช้เลือกจัดการ stock เอง',
  manual_unmanaged_shared_stock: 'ใช้ stock เดียวกับ listing ที่ผู้ใช้จัดการเอง',
  set_product_schema_unsupported: 'ฐานข้อมูล SML นี้ยังไม่รองรับข้อมูลสินค้าชุด',
  sml_availability_blocked: 'ข้อมูลยอดค้างส่ง SML ยังพิสูจน์ไม่ได้',
  sml_availability_exact_invalid: 'จำนวนสต๊อก SML ไม่ผ่านการตรวจสอบ',
  set_net_availability_unverified: 'สินค้าชุดยังรอตรวจรูปแบบยอดค้างส่งใน SML',
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

function formatUnitLabel(code?: string, name?: string) {
  const unitCode = code?.trim() ?? ''
  const unitName = name?.trim() ?? ''
  if (unitName && unitCode && unitName !== unitCode) return `${unitName} (${unitCode})`
  return unitName || unitCode
}

function stockLocationLabel(code: string, name: string, fallback: string) {
  const values = [code.trim(), name.trim()].filter(Boolean)
  return values.length ? values.join(' · ') : fallback
}

function uniqueExcludedLocationCount(locations: ExcludedLocation[]) {
  return new Set(locations.map((item) => `${item.warehouse_code}\u0000${item.location_code}`)).size
}

function ExcludedStockLocations({ locations, items = [], itemTotal = 0, negativeItemTotal }: { locations: ExcludedLocation[]; items?: ExcludedLocation[]; itemTotal?: number; negativeItemTotal?: number }) {
  const rows = items.length ? items : locations
  if (!rows.length) return null
  const negativeCount = negativeItemTotal ?? rows.filter((item) => item.balance_qty < 0).length
  const locationCount = uniqueExcludedLocationCount(locations.length ? locations : rows)
  return (
    <section className="overflow-hidden rounded-md border border-warning/40 bg-warning/5" aria-labelledby="excluded-stock-title">
      <div className="flex flex-col gap-2 border-b border-warning/30 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-start gap-2">
          <Warehouse className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
          <div className="min-w-0">
            <h2 id="excluded-stock-title" className="text-sm font-semibold">พบยอดคงเหลือในคลัง/พื้นที่เก็บอื่น</h2>
            <p className="text-xs text-muted-foreground">ตำแหน่งเหล่านี้ไม่ได้ถูกเลือก ระบบจะแสดงไว้ให้ตรวจสอบ แต่ไม่นำยอดไปคำนวณหรือส่ง Shopee</p>
          </div>
        </div>
        <Badge variant="outline" className="w-fit shrink-0 border-warning/40 bg-background">{formatNumber(locationCount)} ตำแหน่ง</Badge>
      </div>
      <div className="max-h-64 divide-y overflow-y-auto">
        {rows.map((item) => {
          const warehouse = stockLocationLabel(item.warehouse_code, item.warehouse_name, 'ไม่ระบุคลัง')
          const location = stockLocationLabel(item.location_code, item.location_name, 'ไม่ระบุพื้นที่เก็บ')
          const itemCode = item.item_code?.trim() ?? ''
          const itemName = item.item_name?.trim() ?? ''
          const itemTitle = [itemCode, itemName].filter(Boolean).join(' · ')
          return (
            <div key={`${itemCode}:${item.warehouse_code}:${item.location_code}:${item.unit_code}`} className="grid gap-2 px-3 py-2.5 text-xs sm:grid-cols-[minmax(220px,1.2fr)_minmax(220px,1fr)_auto] sm:items-center sm:gap-3">
              <div className="min-w-0">
                {itemTitle ? (
                  <p className="truncate font-medium text-foreground" title={itemTitle}>
                    {itemCode && <span className="font-mono">{itemCode}</span>}{itemCode && itemName && <span className="text-muted-foreground"> · </span>}{itemName}
                  </p>
                ) : (
                  <p className="text-muted-foreground">ผลตรวจรอบเดิมยังไม่มีรหัสและชื่อสินค้า</p>
                )}
                <p className="mt-0.5 text-muted-foreground">หน่วยนับ: <span className="font-medium text-foreground">{item.unit_code || 'ไม่ระบุ'}</span></p>
              </div>
              <div className="min-w-0 space-y-0.5">
                <p className="truncate" title={warehouse}><span className="text-muted-foreground">คลัง:</span> <span className="font-medium text-foreground">{warehouse}</span></p>
                <p className="truncate" title={location}><span className="text-muted-foreground">พื้นที่เก็บ:</span> <span className="font-medium text-foreground">{location}</span></p>
              </div>
              <p className={cn('whitespace-nowrap font-mono text-sm font-semibold tabular-nums sm:text-right', item.balance_qty < 0 ? 'text-destructive' : 'text-foreground')}>
                {formatNumber(item.balance_qty)} <span className="font-sans text-[11px] font-normal">{item.unit_code || 'หน่วยเล็ก'}</span>
              </p>
            </div>
          )
        })}
      </div>
      {itemTotal > rows.length && <p className="border-t border-warning/30 px-3 py-2 text-xs text-muted-foreground">แสดง {formatNumber(rows.length)} จาก {formatNumber(itemTotal)} รายการสินค้า กรุณาค้นหาสินค้าในรายการด้านล่างเพื่อดูรายละเอียดเพิ่มเติม</p>}
      {negativeCount > 0 && <p className="border-t border-warning/30 px-3 py-2 text-xs text-destructive">พบยอดติดลบ {formatNumber(negativeCount)} รายการ ควรตรวจการรับเข้า/ตัดสต๊อกของตำแหน่งดังกล่าวใน SML</p>}
    </section>
  )
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
    left.schedule_mode === right.schedule_mode &&
    left.monthly_interval === right.monthly_interval &&
    left.monthly_day === right.monthly_day &&
    left.monthly_time === right.monthly_time &&
    left.schedule_risk_acknowledged === right.schedule_risk_acknowledged &&
    left.scope_mode === right.scope_mode &&
    sameLocations(left.locations, right.locations)
}

function normalizeStockSetting(setting: StockSetting): StockSetting {
  const locations = setting.scope_mode === 'selected' && setting.locations.length === 1 ? [setting.locations[0]] : []
  return {
    ...setting,
    schedule_mode: setting.schedule_mode || 'interval',
    interval_seconds: setting.interval_seconds || 300,
    monthly_interval: setting.monthly_interval || 1,
    monthly_day: setting.monthly_day || 1,
    monthly_time: setting.monthly_time || '00:00',
    schedule_risk_acknowledged: setting.schedule_risk_acknowledged ?? false,
    enabled: locations.length > 0 ? setting.enabled : false,
    scope_mode: locations.length > 0 ? 'selected' : 'unconfigured',
    locations,
    all_scope_warning_acknowledged: false,
  }
}

export default function ShopeeStock() {
  const canManage = useAuthStore((state) => state.user?.role === 'admin')
  const [data, setData] = useState<Overview | null>(null)
  const [shopID, setShopID] = useState(0)
  const [tab, setTab] = useState<(typeof STATUS_TABS)[number]['key']>('ready')
  const [page, setPage] = useState(1)
	const [productGroups, setProductGroups] = useState<ProductGroup[]>([])
	const [groupedAvailable, setGroupedAvailable] = useState<boolean | null>(null)
	const [groupCursor, setGroupCursor] = useState('')
	const [groupCursorHistory, setGroupCursorHistory] = useState<string[]>([])
	const [nextGroupCursor, setNextGroupCursor] = useState('')
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  const [draft, setDraft] = useState<StockSetting | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewRun, setPreviewRun] = useState<SyncRun | null>(null)
  const [operationResult, setOperationResult] = useState<StockOperationResult | null>(null)
  const [mapping, setMapping] = useState<ProductRow | null>(null)
  const [sharedPoolItemCode, setSharedPoolItemCode] = useState('')
  const [catalogInfoOpen, setCatalogInfoOpen] = useState(false)
  const [locationsOpen, setLocationsOpen] = useState(false)
  const [warehouseCode, setWarehouseCode] = useState('')
  const sequence = useRef(0)
  const previewPollToken = useRef(0)
  const autoSelectedShops = useRef(new Set<number>())

	const load = useCallback(async (preferredShopID = shopID, preferredGroupCursor = groupCursor) => {
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
	  if (selected > 0 && tab !== 'history' && groupedAvailable !== false) {
		try {
		  const grouped = await client.get<CursorPage<ProductGroup>>(`/api/settings/shopee-stock/${selected}/product-groups`, {
			params: { status: tab, q: search || undefined, cursor: preferredGroupCursor || undefined, limit: 30 },
		  })
		  if (sequence.current !== current) return
		  setProductGroups(grouped.data.data ?? [])
		  setNextGroupCursor(grouped.data.next_cursor ?? '')
		  setGroupedAvailable(true)
		} catch (error) {
		  if ((error as { response?: { status?: number } }).response?.status !== 404) throw error
		  setGroupedAvailable(false)
		}
	  }
      const setting = normalizedData.settings.find((item) => item.shop_id === selected) ?? null
      setDraft(setting)
      setWarehouseCode(setting?.locations[0]?.warehouse ?? '')
      setLocationsOpen(false)
    } catch (error) {
      if (sequence.current === current) toast.error(errorText(error))
    } finally {
      if (sequence.current === current) setLoading(false)
    }
	}, [groupCursor, groupedAvailable, page, search, shopID, tab])

  useEffect(() => { void load() }, [load])
  useEffect(() => () => { previewPollToken.current += 1 }, [])
  useEffect(() => {
    previewPollToken.current += 1
    setPreviewRun(null)
    setPreview(null)
    setOperationResult(null)
  }, [shopID])

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
	const groupedProducts = tab !== 'history' && groupedAvailable === true
	const pageCount = groupedProducts ? groupCursorHistory.length + 1 + (nextGroupCursor ? 1 : 0) : Math.max(1, Math.ceil((data?.products_total ?? 0) / (data?.products_size || 50)))
	const currentProductPage = groupedProducts ? groupCursorHistory.length + 1 : page
  const productNames = useMemo(() => new Map((data?.products ?? []).map((product) => [`${product.item_id}:${product.model_id}`, productDisplayName(product)])), [data?.products])
  const productCounts = data?.product_counts ?? { ready: 0, fix: 0, excluded: 0 }
  const stockPctValid = !!draft && Number.isFinite(draft.stock_pct) && draft.stock_pct >= 1 && draft.stock_pct <= 100
  const scopeReady = !!draft && draft.scope_mode === 'selected' && draft.locations.length === 1
  const selectedWarehouse = warehouses.find((warehouse) => warehouse.code === warehouseCode)
  const selectedPair = draft?.locations[0]
  const scopeSummary = scopeReady && selectedPair ? `${selectedPair.warehouse} · ${selectedPair.location}` : ''
  const settingsDirty = !sameSettings(draft, selectedSetting)
  const scheduleDirty = !!draft && !!selectedSetting && (
    draft.schedule_mode !== selectedSetting.schedule_mode ||
    draft.interval_seconds !== selectedSetting.interval_seconds ||
    draft.monthly_interval !== selectedSetting.monthly_interval ||
    draft.monthly_day !== selectedSetting.monthly_day ||
    draft.monthly_time !== selectedSetting.monthly_time ||
    draft.schedule_risk_acknowledged !== selectedSetting.schedule_risk_acknowledged
  )

  useEffect(() => {
    if (!shopID || tab !== 'ready' || search || productCounts.ready > 0 || productCounts.fix === 0 || autoSelectedShops.current.has(shopID)) return
    autoSelectedShops.current.add(shopID)
    setPage(1)
	setGroupCursor('')
	setGroupCursorHistory([])
    setTab('fix')
  }, [productCounts.fix, productCounts.ready, search, shopID, tab])

	const resetProductPagination = () => {
	  setPage(1)
	  setGroupCursor('')
	  setGroupCursorHistory([])
	}

	const previousProductPage = () => {
	  if (!groupedProducts) {
		setPage((value) => Math.max(1, value - 1))
		return
	  }
	  const previous = groupCursorHistory[groupCursorHistory.length - 1] ?? ''
	  setGroupCursorHistory((value) => value.slice(0, -1))
	  setGroupCursor(previous)
	}

	const nextProductPage = () => {
	  if (!groupedProducts) {
		setPage((value) => value + 1)
		return
	  }
	  if (!nextGroupCursor) return
	  setGroupCursorHistory((value) => [...value, groupCursor])
	  setGroupCursor(nextGroupCursor)
	}

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
      schedule_mode: draft.schedule_mode,
      monthly_interval: draft.monthly_interval,
      monthly_day: draft.monthly_day,
      monthly_time: draft.monthly_time,
      schedule_risk_acknowledged: draft.schedule_risk_acknowledged,
      scope_mode: draft.scope_mode,
      locations: draft.locations,
    })
    setDraft(normalizeStockSetting(response.data))
    setLocationsOpen(false)
    toast.success('บันทึกการตั้งค่าสต๊อกแล้ว')
    await load(shopID)
  })

  const syncCatalog = () => runAction('catalog', async () => {
    setOperationResult(null)
    const response = await client.post<SyncRun>(`/api/settings/shopee-stock/${shopID}/catalog-sync`, {}, { timeout: 180000 })
    setPreview(null)
    setOperationResult({
      tone: 'success',
      title: 'อัปเดตรายการสินค้าจาก Shopee แล้ว',
      detail: `พบ ${formatNumber(response.data.total_count)} ตัวเลือก ระบบอัปเดตรายการด้านล่างแล้ว`,
    })
    toast.success(`อัปเดตสินค้า ${formatNumber(response.data.total_count)} ตัวเลือกแล้ว`)
    await load(shopID)
  })

  const previewImpact = () => runAction('preview', async () => {
    if (!draft) return
    setOperationResult(null)
    await client.put(`/api/settings/shopee-stock/${shopID}`, {
      enabled: false,
      stock_pct: draft.stock_pct,
      interval_seconds: draft.interval_seconds,
      schedule_mode: draft.schedule_mode,
      monthly_interval: draft.monthly_interval,
      monthly_day: draft.monthly_day,
      monthly_time: draft.monthly_time,
      schedule_risk_acknowledged: draft.schedule_risk_acknowledged,
      scope_mode: draft.scope_mode,
      locations: draft.locations,
    })
    const queued = await client.post<{ run_id: string; status: string }>(`/api/settings/shopee-stock/${shopID}/preview`, { as_of_date: bangkokDate() })
    const token = previewPollToken.current + 1
    previewPollToken.current = token
    setPreviewRun({ id: queued.data.run_id, run_type: 'preview', status: queued.data.status, total_count: 0, changed_count: 0, blocked_count: 0, error_count: 0, started_at: new Date().toISOString(), progress_pct: 0 })
    let completed: SyncRun | null = null
    for (let attempt = 0; attempt < 300; attempt += 1) {
      if (previewPollToken.current !== token) return
      const runResponse = await client.get<SyncRun>(`/api/settings/shopee-stock/${shopID}/runs/${queued.data.run_id}`)
      setPreviewRun(runResponse.data)
      if (['success', 'warning', 'failed', 'paused', 'cancelled'].includes(runResponse.data.status)) {
        completed = runResponse.data
        break
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1000))
    }
    if (!completed) throw new Error('ตรวจสต๊อกใช้เวลานานเกิน 5 นาที กรุณาเปิดประวัติเพื่อตรวจผล')
    if (completed.status !== 'success' && completed.status !== 'warning') {
      throw new Error(completed.error_message || 'ตรวจสต๊อกไม่สำเร็จ กรุณาลองใหม่')
    }
    const [firstLines, blockedLines] = await Promise.all([
      client.get<CursorPage<PreviewRunLine>>(`/api/settings/shopee-stock/${shopID}/runs/${queued.data.run_id}/lines`, { params: { limit: 100 } }),
      completed.blocked_count > 0
        ? client.get<CursorPage<PreviewRunLine>>(`/api/settings/shopee-stock/${shopID}/runs/${queued.data.run_id}/lines`, { params: { limit: 100, status: 'blocked' } })
        : Promise.resolve({ data: { data: [], has_more: false, next_cursor: '' } }),
    ])
    const detailByKey = new Map<string, Preview['lines'][number]>()
    for (const line of [...(firstLines.data.data ?? []), ...(blockedLines.data.data ?? [])]) {
      detailByKey.set(`${line.detail.item_id}:${line.detail.model_id}`, line.detail)
    }
    const summary = completed.summary ?? { run_id: completed.id, total_count: completed.total_count, changed_count: completed.changed_count, skipped_count: 0, blocked_count: completed.blocked_count, excluded_balance: 0 }
    const result = { ...summary, run_id: completed.id, lines: [...detailByKey.values()] } as Preview
    setPreview(result)
    setPreviewRun(null)
    toast.success(result.blocked_count ? `ตรวจแล้ว พบ ${result.blocked_count} รายการที่ต้องแก้` : 'ตรวจผลกระทบแล้ว พร้อมเปิดซิงก์')
    await load(shopID)
  })

  const syncNow = () => runAction('run', async () => {
    setOperationResult(null)
    const response = await client.post<{ changed_count: number; blocked_count: number; error_count: number; unknown_count: number }>(`/api/settings/shopee-stock/${shopID}/run`, {}, { timeout: 180000 })
    const issueCount = response.data.blocked_count + response.data.error_count + response.data.unknown_count
    const detail = [
      `อัปเดต ${formatNumber(response.data.changed_count)} ตัวเลือก`,
      response.data.blocked_count > 0 ? `ต้องแก้ ${formatNumber(response.data.blocked_count)}` : '',
      response.data.error_count > 0 ? `ไม่สำเร็จ ${formatNumber(response.data.error_count)}` : '',
      response.data.unknown_count > 0 ? `รอยืนยัน ${formatNumber(response.data.unknown_count)}` : '',
    ].filter(Boolean).join(' · ')
    setPreview(null)
    setOperationResult({
      tone: issueCount > 0 ? 'warning' : 'success',
      title: issueCount > 0
        ? 'ซิงก์เสร็จแล้ว แต่มีรายการที่ต้องตรวจ'
        : response.data.changed_count > 0
          ? 'ซิงก์สต๊อกไป Shopee แล้ว'
          : 'สต๊อก Shopee ตรงกับเป้าหมายแล้ว',
      detail,
    })
    toast.success(`อัปเดต ${formatNumber(response.data.changed_count)} ตัวเลือก`)
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
  const pauseCopy = marketplaceStockPauseCopy(draft?.paused_reason)
  const previewPauseCopy = marketplaceStockPauseCopy(preview?.circuit_breaker)
  const syncDisabledReason = !data?.available
    ? (data?.availability_text || 'ระบบซิงก์สต๊อกยังไม่พร้อมใช้งาน')
    : !draft
      ? 'เลือกร้าน Shopee ก่อน'
      : draft.dry_run_required
        ? 'ตรวจสอบสต๊อกใหม่ก่อนซิงก์'
        : draft.paused_reason ? pauseCopy.description : ''
  const scheduleDisabledReason = !data?.available
    ? (data?.availability_text || 'ระบบซิงก์สต๊อกยังไม่พร้อมใช้งาน')
    : !stockPctValid
      ? 'กำหนดสัดส่วนส่ง Shopee ระหว่าง 1-100%'
      : !scopeReady
        ? 'เลือกคลังและพื้นที่เก็บก่อน'
        : draft?.dry_run_required
          ? 'ต้องตรวจสอบสต๊อกก่อนเปิดซิงก์'
          : draft?.paused_reason
            ? pauseCopy.description
      : ''
  const previewProgress = Math.min(100, Math.max(0, Math.round(previewRun?.progress_pct ?? 0)))
  const previewInProgress = previewRun?.status === 'queued' || previewRun?.status === 'running'
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
      label: 'ตรวจสอบสต๊อก',
      detail: !draft?.dry_run_required && selectedSetting?.last_preview_at
        ? `ตรวจล่าสุด ${formatDateTime(selectedSetting.last_preview_at)}`
        : 'ยังไม่ได้ตรวจด้วยค่าปัจจุบัน',
      done: !!draft && !draft.dry_run_required && !!selectedSetting?.last_preview_at,
    },
  ]
  const nextActionMessage = previewDisabledReason || (draft?.dry_run_required
    ? 'กด “บันทึกและตรวจสต๊อก” เพื่อคำนวณยอด SML และเป้าหมายโดยยังไม่ส่งไป Shopee'
    : syncDisabledReason || (!draft?.enabled ? 'พร้อมแล้ว กด “ซิงก์ตอนนี้” หรือเปิดรอบอัตโนมัติได้ตามต้องการ' : ''))
  const emptyState = search
    ? { title: `ไม่พบรายการที่ตรงกับ “${search}”`, description: 'ลองค้นหาด้วย SKU รหัสสินค้า หรือชื่อที่สั้นลง' }
    : tab === 'ready' && productCounts.fix > 0
      ? { title: 'ยังไม่มีสินค้าพร้อมซิงก์', description: `มี ${formatNumber(productCounts.fix)} รายการที่ต้องจับคู่หรือแก้ข้อมูลก่อน` }
      : tab === 'fix'
        ? { title: 'ไม่มีรายการที่ต้องแก้', description: 'สินค้าที่ใช้งานอยู่พร้อมตรวจสอบสต๊อกแล้ว' }
        : tab === 'excluded'
          ? { title: 'ยังไม่มีสินค้าที่ถูกยกเว้น', description: 'สินค้าที่ไม่ต้องการซิงก์จะปรากฏในแท็บนี้' }
          : { title: 'ยังไม่มีสินค้าในแท็บนี้', description: selectedSetting?.last_catalog_sync_at ? 'ลองตรวจแท็บอื่นหรืออัปเดตรายการสินค้าจาก Shopee อีกครั้ง' : 'อัปเดตรายการสินค้าจาก Shopee เพื่อดึงสินค้าและตัวเลือกล่าสุดมาตรวจกับ SML' }

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
              {!canManage && <Badge variant="secondary" className="h-6">ดูอย่างเดียว</Badge>}
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

      {!canManage && (
        <Alert><Info className="h-4 w-4" /><AlertTitle>สิทธิ์ดูอย่างเดียว</AlertTitle><AlertDescription>คุณตรวจสถานะ สินค้า ตัวเลือก และประวัติได้ การแก้การจับคู่ ตั้งค่า ตรวจสอบ และซิงก์สต๊อกต้องใช้สิทธิ์ผู้ดูแลระบบ</AlertDescription></Alert>
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
				resetProductPagination()
                setPreview(null)
                setSharedPoolItemCode('')
                setLocationsOpen(false)
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
          <div className="space-y-1.5"><Label htmlFor="stock-pct">ส่งไป Shopee</Label><div className="relative"><Input id="stock-pct" type="number" min={1} max={100} disabled={!canManage} aria-invalid={!stockPctValid} aria-describedby={!stockPctValid ? 'stock-pct-error' : undefined} value={draft?.stock_pct ?? 80} onChange={(event) => draft && setDraft({ ...draft, stock_pct: Number(event.target.value), enabled: false, dry_run_required: true })} className={cn('pr-8', !stockPctValid && 'border-destructive')} /><span className="absolute right-3 top-2.5 text-sm text-muted-foreground">%</span></div>{!stockPctValid && <p id="stock-pct-error" className="text-xs text-destructive">กรอก 1-100</p>}</div>
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
                  disabled={!canManage || !draft || !data?.available}
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
                    ระบบคำนวณและส่งสต๊อกจากคลังและพื้นที่เก็บคู่นี้เท่านั้น เมื่อเปลี่ยนค่าให้ตรวจสอบสต๊อกใหม่ก่อนส่งจริง
                  </p>
                </div>
              </PopoverContent>
            </Popover>
          </div>
          {draft && (
            <StockSchedulePicker
              value={draft}
              enabled={draft.enabled}
              disabled={!canManage || !data?.available}
              switchDisabled={!canManage || !data?.available || !stockPctValid || !scopeReady || !!draft.dry_run_required || !!draft.paused_reason}
              disabledReason={scheduleDisabledReason}
              nextRunAt={selectedSetting?.next_run_at}
              scheduleDirty={scheduleDirty}
              onChange={(schedule) => setDraft({ ...draft, ...schedule })}
              onEnabledChange={(checked) => setDraft({ ...draft, enabled: checked })}
            />
          )}
          <Button className="w-full sm:col-span-2 sm:w-auto sm:justify-self-end xl:col-span-1" onClick={saveSettings} disabled={!canManage || !draft || !stockPctValid || !scopeReady || !settingsDirty || !!busy}><Save className="h-4 w-4" />{busy === 'save' ? 'กำลังบันทึก...' : 'บันทึก'}</Button>
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
			<Button type="button" variant="link" size="sm" className="ml-auto h-auto px-0 text-xs" onClick={() => { setTab('fix'); resetProductPagination() }}>
              เปิดรายการต้องแก้ ({formatNumber(productCounts.fix)})
            </Button>
          )}
        </div>

        <div className="flex flex-col gap-2 border-t px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center">
              <Button variant="outline" size="sm" onClick={syncCatalog} disabled={!canManage || !data?.available || !!busy}><Boxes className="h-4 w-4" />{busy === 'catalog' ? 'กำลังอัปเดตจาก Shopee...' : 'อัปเดตรายการสินค้าจาก Shopee'}</Button>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button type="button" variant="ghost" size="icon" className="h-8 w-8" onClick={() => setCatalogInfoOpen(true)} aria-label="อัปเดตรายการสินค้าจาก Shopee คืออะไร">
                    <Info className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>อัปเดตรายการสินค้าจาก Shopee คืออะไร</TooltipContent>
              </Tooltip>
            </div>
            <Button variant="outline" size="sm" onClick={previewImpact} disabled={!canManage || !!previewDisabledReason || !!busy}>
              {previewInProgress ? <Loader2 className="h-4 w-4 animate-spin" /> : <PackageCheck className="h-4 w-4" />}
              {previewInProgress ? `กำลังตรวจ ${previewProgress}%` : 'บันทึกและตรวจสต๊อก'}
            </Button>
            <Button size="sm" onClick={syncNow} disabled={!canManage || !!syncDisabledReason || !!busy}><Play className="h-4 w-4" />ซิงก์ตอนนี้</Button>
          </div>
          <span className="min-w-0 text-xs text-muted-foreground sm:max-w-[48%] sm:text-right xl:max-w-none">สินค้า {formatDateTime(selectedSetting?.last_catalog_sync_at)} · ตรวจ {formatDateTime(selectedSetting?.last_preview_at)}{draft?.dry_run_required && selectedSetting?.last_preview_at ? ' (ต้องตรวจใหม่)' : ''} · ซิงก์ {formatDateTime(selectedSetting?.last_success_at)}</span>
          {nextActionMessage && <p className="basis-full text-xs text-amber-800 dark:text-amber-200 sm:hidden"><span className="font-medium">ขั้นถัดไป:</span> {nextActionMessage}</p>}
        </div>
        {nextActionMessage && <p className="hidden border-t px-3 py-1.5 text-xs text-amber-800 dark:text-amber-200 sm:block"><span className="font-medium">ขั้นถัดไป:</span> {nextActionMessage}</p>}
      </section>

      {previewInProgress && (
        <Alert className="border-info/40 bg-info/5">
          <Loader2 className="h-4 w-4 animate-spin" />
          <AlertTitle>{previewRun?.status === 'queued' ? 'กำลังรอคิวตรวจสต๊อก' : 'กำลังตรวจสต๊อกจาก SML'}</AlertTitle>
          <AlertDescription>
            <div className="mt-1 flex items-center gap-3">
              <div
                className="h-2 flex-1 overflow-hidden rounded-full bg-muted"
                role="progressbar"
                aria-label="ความคืบหน้าการตรวจสต๊อก"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={previewProgress}
              >
                <div className="h-full rounded-full bg-info transition-[width] duration-300" style={{ width: `${previewProgress}%` }} />
              </div>
              <span className="w-12 text-right font-mono text-xs">{previewProgress}%</span>
            </div>
            <p className="mt-1 text-xs">ประมวลผลแล้ว {formatNumber(previewRun?.processed_count ?? 0)} รายการ หน้านี้จะอัปเดตผลให้อัตโนมัติ</p>
          </AlertDescription>
        </Alert>
      )}
      {draft?.paused_reason && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>{pauseCopy.title}</AlertTitle><AlertDescription>{pauseCopy.description}</AlertDescription></Alert>}
      {operationResult && (
        <Alert className={operationResult.tone === 'warning' ? 'border-warning/50 bg-warning/10' : 'border-success/40 bg-success/10'}>
          {operationResult.tone === 'warning' ? <AlertTriangle className="h-4 w-4" /> : <CheckCircle2 className="h-4 w-4" />}
          <AlertTitle>{operationResult.title}</AlertTitle>
          <AlertDescription>{operationResult.detail}</AlertDescription>
        </Alert>
      )}
      {preview && <Alert className={preview.circuit_breaker ? 'border-destructive/50 bg-destructive/5' : preview.blocked_count ? 'border-warning/50 bg-warning/10' : 'border-success/40 bg-success/10'}>{preview.circuit_breaker || preview.blocked_count ? <AlertTriangle className="h-4 w-4" /> : <CheckCircle2 className="h-4 w-4" />}<AlertTitle>{preview.circuit_breaker ? previewPauseCopy.title : preview.blocked_count ? `ตรวจสอบแล้ว พบ ${formatNumber(preview.blocked_count)} รายการที่ต้องแก้` : 'ตรวจสอบสต๊อกผ่านแล้ว'}</AlertTitle><AlertDescription>{preview.circuit_breaker ? `${previewPauseCopy.description} · ` : ''}ตรวจ {formatNumber(preview.total_count)} รายการ · จะเปลี่ยน {formatNumber(preview.changed_count)} · ไม่เปลี่ยน {formatNumber(preview.skipped_count)}{(preview.excluded_locations?.length ?? 0) > 0 ? ` · พบสต๊อกในคลัง/พื้นที่อื่น ${formatNumber(uniqueExcludedLocationCount(preview.excluded_locations ?? []))} ตำแหน่ง` : preview.excluded_balance !== 0 ? ` · พบยอดในคลัง/พื้นที่อื่น ${formatNumber(preview.excluded_balance)}` : ''}</AlertDescription></Alert>}
      <ExcludedStockLocations locations={preview?.excluded_locations ?? []} items={preview?.excluded_items ?? []} itemTotal={preview?.excluded_items_total ?? 0} negativeItemTotal={preview?.excluded_negative_items_total} />
      {preview?.lines?.some((line) => line.blocked) && <section className="rounded-md border border-warning/40 bg-warning/5"><div className="border-b px-4 py-3"><h2 className="text-sm font-semibold">รายการที่ยังซิงก์ไม่ได้</h2><p className="text-xs text-muted-foreground">กด “บันทึกและตรวจสต๊อก” อีกครั้ง ระบบจะเตรียมรายการที่จับคู่พร้อมให้อัตโนมัติ หากยังแสดงอยู่จึงค่อยแก้สินค้า หน่วย หรือข้อมูล SML ตามข้อความด้านล่าง</p></div><div className="divide-y">{(preview.lines ?? []).filter((line) => line.blocked).slice(0, 20).map((line) => <div key={`${line.item_id}:${line.model_id}`} className="grid gap-1 px-4 py-2 text-sm sm:grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)]"><span className="truncate">{productNames.get(`${line.item_id}:${line.model_id}`) || `Item ${line.item_id}${line.model_id ? ` / Model ${line.model_id}` : ''}`}</span><span className="text-amber-800 dark:text-amber-200">{line.warning_codes.map((code) => WARNING_LABEL[code] || code).join(' · ')}</span></div>)}{preview.blocked_count > 20 && <p className="px-4 py-2 text-xs text-muted-foreground">แสดง 20 จาก {formatNumber(preview.blocked_count)} รายการ กดตรวจสอบสต๊อกใหม่เพื่ออัปเดตผล</p>}</div></section>}
      {preview?.lines?.some((line) => line.item_type === 3) && <SetStockPreviewList lines={preview.lines.filter((line) => line.item_type === 3)} productNames={productNames} />}

      <section className="overflow-hidden rounded-md border bg-card">
        <div className="flex flex-col gap-3 border-b p-3 sm:flex-row sm:items-center sm:justify-between">
		  <Tabs value={tab} onValueChange={(value) => { setTab(value as typeof tab); resetProductPagination() }}>
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
		  {tab !== 'history' && <form className="relative w-full sm:w-72" onSubmit={(event) => { event.preventDefault(); resetProductPagination(); setSearch(query.trim()) }}><Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" /><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ค้นหา SKU หรือสินค้า" className="pl-9" /></form>}
        </div>

        {tab === 'history' ? <HistoryList runs={data?.runs ?? []} /> : (
          <>
			{!groupedProducts && <div className="hidden grid-cols-[minmax(280px,1.4fr)_minmax(220px,1fr)_minmax(280px,auto)_148px] gap-3 border-b bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground xl:grid">
              <span>สินค้า Shopee</span>
              <span>สินค้าที่จับคู่ใน SML</span>
              <div className="grid grid-cols-3 gap-3 text-right">
				<StockColumnLabels />
              </div>
              <span />
			</div>}
			{groupedProducts ? <GroupedShopeeProducts
			  key={`${shopID}:${tab}:${search}:${groupCursor}`}
			  shopID={shopID}
			  status={tab}
			  query={search}
			  groups={productGroups}
			  previewStale={Boolean(draft?.dry_run_required)}
			  canManage={canManage}
			  onMap={setMapping}
			  onPool={(product) => setSharedPoolItemCode(product.sml_item_code)}
			/> : <div className="divide-y">{(data?.products ?? []).map((product) => <ProductLine key={`${product.item_id}:${product.model_id}`} product={product} canManage={canManage} onMap={() => setMapping(product)} onPool={() => setSharedPoolItemCode(product.sml_item_code)} />)}</div>}
			{!(groupedProducts ? productGroups.length : data?.products.length) && (
              <div className="flex flex-col items-center px-4 py-10 text-center">
                <PackageCheck className="mb-3 h-7 w-7 text-muted-foreground" aria-hidden="true" />
                <p className="text-sm font-medium text-foreground">{emptyState.title}</p>
                <p className="mt-1 max-w-md text-sm text-muted-foreground">{emptyState.description}</p>
                {search ? (
				  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={() => { setQuery(''); setSearch(''); resetProductPagination() }}>ล้างคำค้นหา</Button>
                ) : tab === 'ready' && productCounts.fix > 0 ? (
				  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={() => { setTab('fix'); resetProductPagination() }}>เปิดรายการต้องแก้ ({formatNumber(productCounts.fix)})</Button>
                ) : !selectedSetting?.last_catalog_sync_at ? (
                  <Button type="button" variant="outline" size="sm" className="mt-4" onClick={syncCatalog} disabled={!canManage || !data?.available || !!busy}>อัปเดตรายการสินค้าจาก Shopee</Button>
                ) : null}
              </div>
            )}
			<div className="flex items-center justify-between border-t px-3 py-2"><span className="text-xs text-muted-foreground">{groupedProducts ? `${formatNumber(productGroups.length)} สินค้าหลักในหน้านี้` : `ทั้งหมด ${formatNumber(data?.products_total)} รายการ`}</span><div className="flex items-center gap-2"><Button variant="outline" size="icon" title="หน้าก่อนหน้า" disabled={groupedProducts ? groupCursorHistory.length === 0 : page <= 1} onClick={previousProductPage}><ChevronLeft className="h-4 w-4" /></Button><span className="text-sm">{currentProductPage} / {pageCount}</span><Button variant="outline" size="icon" title="หน้าถัดไป" disabled={groupedProducts ? !nextGroupCursor : page >= pageCount} onClick={nextProductPage}><ChevronRight className="h-4 w-4" /></Button></div></div>
          </>
        )}
      </section>

      {canManage && <MappingDialog product={mapping} shopID={shopID} onClose={() => setMapping(null)} onConfigurePool={(itemCode) => { setMapping(null); setSharedPoolItemCode(itemCode) }} onSaved={async () => { setMapping(null); setPreview(null); await load(shopID) }} />}
      <SharedPoolDialog
        shopID={shopID}
        itemCode={sharedPoolItemCode}
        fallbackStockPct={draft?.stock_pct ?? 80}
        canManage={canManage}
        onClose={() => setSharedPoolItemCode('')}
        onSaved={async () => { setSharedPoolItemCode(''); setPreview(null); await load(shopID) }}
      />
      <CatalogInfoDialog open={catalogInfoOpen} onOpenChange={setCatalogInfoOpen} />
      </div>
    </TooltipProvider>
  )
}

function ProductExcludedLocations({
  locations,
  fallbackBalance,
  fallbackUnit,
}: {
  locations: ExcludedLocation[]
  fallbackBalance?: number
  fallbackUnit?: string
}) {
  const primary = locations[0]
  const remaining = locations.slice(1)
  const locationDetail = (location: ExcludedLocation) => {
    const warehouse = stockLocationLabel(location.warehouse_code, location.warehouse_name, 'ไม่ระบุคลัง')
    const shelf = stockLocationLabel(location.location_code, location.location_name, 'ไม่ระบุพื้นที่เก็บ')
    return { warehouse, shelf, text: `${formatNumber(location.balance_qty)}${location.unit_code ? ` ${location.unit_code}` : ''} · คลัง: ${warehouse} · พื้นที่เก็บ: ${shelf}` }
  }
  const primaryDetail = primary ? locationDetail(primary) : null
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 border-t border-warning/25 pt-1.5 text-[11px] text-amber-900 dark:text-amber-200 xl:col-span-4">
      <Warehouse className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      <span className="font-medium">ยอดคงเหลืออยู่ในคลัง/พื้นที่เก็บอื่น</span>
      {primary && primaryDetail ? (
        <span className="min-w-0 truncate" title={primaryDetail.text}><span className={cn('font-mono font-semibold tabular-nums', primary.balance_qty < 0 && 'text-destructive')}>{formatNumber(primary.balance_qty)}{primary.unit_code ? ` ${primary.unit_code}` : ''}</span><span> · คลัง: {primaryDetail.warehouse} · พื้นที่เก็บ: {primaryDetail.shelf}</span></span>
      ) : (
        <span>
          <span className={cn('font-mono font-semibold tabular-nums', (fallbackBalance ?? 0) < 0 && 'text-destructive')}>{formatNumber(fallbackBalance)}{fallbackUnit ? ` ${fallbackUnit}` : ''}</span>
          <span> · กด “บันทึกและตรวจสต๊อก” เพื่อดูชื่อคลังและพื้นที่เก็บ</span>
        </span>
      )}
      {remaining.length > 0 && (
        <Popover>
          <PopoverTrigger asChild><Button type="button" variant="ghost" size="sm" className="h-6 shrink-0 px-1.5 text-[11px] text-amber-900 dark:text-amber-200">ดูอีก {formatNumber(remaining.length)} ตำแหน่ง</Button></PopoverTrigger>
          <PopoverContent align="start" className="w-[min(26rem,calc(100vw-2rem))] p-0">
            <div className="border-b px-3 py-2"><p className="text-sm font-medium">คลังและพื้นที่เก็บอื่นของสินค้านี้</p><p className="text-xs text-muted-foreground">ยอดเหล่านี้ไม่นำไปคำนวณหรือส่ง Shopee</p></div>
            <div className="max-h-64 divide-y overflow-y-auto">
              {remaining.map((location) => {
                const detail = locationDetail(location)
                return <div key={`${location.item_code ?? ''}:${location.warehouse_code}:${location.location_code}:${location.unit_code}`} className="px-3 py-2 text-xs"><p className={cn('font-mono font-semibold tabular-nums', location.balance_qty < 0 && 'text-destructive')}>{formatNumber(location.balance_qty)}{location.unit_code ? ` ${location.unit_code}` : ''}</p><p className="mt-0.5 text-foreground">คลัง: {detail.warehouse}</p><p className="text-muted-foreground">พื้นที่เก็บ: {detail.shelf}</p></div>
              })}
            </div>
          </PopoverContent>
        </Popover>
      )}
    </div>
  )
}

type GroupProductState = { rows: ProductRow[]; nextCursor: string; loading: boolean; error: string }

function GroupedShopeeProducts({ shopID, status, query, groups, previewStale, canManage, onMap, onPool }: {
	shopID: number
	status: string
	query: string
	groups: ProductGroup[]
	previewStale: boolean
	canManage: boolean
	onMap: (product: ProductRow) => void
	onPool: (product: ProductRow) => void
}) {
	const [expanded, setExpanded] = useState<Set<number>>(new Set())
	const [children, setChildren] = useState<Record<number, GroupProductState>>({})

	useEffect(() => {
		setExpanded(new Set())
		setChildren({})
	}, [groups])

	const loadChildren = async (group: ProductGroup, append = false) => {
		const current = children[group.item_id]
		setChildren((value) => ({ ...value, [group.item_id]: { rows: append ? current?.rows ?? [] : [], nextCursor: current?.nextCursor ?? '', loading: true, error: '' } }))
		try {
			const response = await client.get<CursorPage<ProductRow>>(`/api/settings/shopee-stock/${shopID}/product-groups/${group.item_id}/variants`, {
				params: { status, q: query || undefined, cursor: append ? current?.nextCursor || undefined : undefined, limit: 100 },
			})
			setChildren((value) => ({ ...value, [group.item_id]: {
				rows: append ? [...(current?.rows ?? []), ...(response.data.data ?? [])] : response.data.data ?? [],
				nextCursor: response.data.next_cursor ?? '',
				loading: false,
				error: '',
			} }))
		} catch (error) {
			setChildren((value) => ({ ...value, [group.item_id]: { rows: current?.rows ?? [], nextCursor: current?.nextCursor ?? '', loading: false, error: errorText(error) } }))
		}
	}

	const toggle = (group: ProductGroup) => {
		setExpanded((value) => {
			const next = new Set(value)
			if (next.has(group.item_id)) next.delete(group.item_id)
			else next.add(group.item_id)
			return next
		})
		if (!children[group.item_id]) void loadChildren(group)
	}

	return <div>{groups.map((group) => {
		const open = expanded.has(group.item_id)
		const state = children[group.item_id]
		return <div key={group.item_id} className="border-b last:border-b-0">
			<div className={cn('grid min-h-16 grid-cols-[minmax(0,1fr)_40px] items-center gap-x-3 gap-y-2 px-3 py-2 xl:grid-cols-[minmax(260px,1fr)_minmax(180px,auto)_minmax(360px,0.9fr)_40px]', open && 'bg-muted/25')}>
				<button type="button" className="min-w-0 text-left" onClick={() => toggle(group)} aria-expanded={open}>
					<p className="line-clamp-2 text-sm font-semibold">{group.item_name || 'ไม่ระบุชื่อสินค้า'}</p>
					<p className="mt-1 truncate text-xs text-muted-foreground">SKU: <span className="font-mono text-foreground">{group.item_sku || '-'}</span> · Item <span className="font-mono">{group.item_id}</span></p>
				</button>
				<div className="col-span-2 row-start-2 flex flex-wrap justify-start gap-1 xl:col-span-1 xl:col-start-2 xl:row-start-1 xl:justify-end">
					<Badge variant="outline">{formatNumber(group.variant_count)} ตัวเลือก</Badge>
					{group.ready_count > 0 && <Badge variant="outline" className="border-success/30 bg-success/10 text-success">พร้อม {formatNumber(group.ready_count)}</Badge>}
					{group.fix_count > 0 && <Badge variant="outline" className="border-warning/30 bg-warning/10 text-amber-800 dark:text-amber-200">ต้องแก้ {formatNumber(group.fix_count)}</Badge>}
					{group.excluded_count > 0 && <Badge variant="secondary">ไม่นับ {formatNumber(group.excluded_count)}</Badge>}
				</div>
				<GroupStockTotals group={group} previewStale={previewStale} />
				<Button type="button" variant="ghost" size="icon" className="col-start-2 row-start-1 h-8 w-8 justify-self-end xl:col-start-4" onClick={() => toggle(group)} aria-label={open ? 'ซ่อนตัวเลือก' : 'แสดงตัวเลือก'} aria-expanded={open}><ChevronDown className={cn('h-4 w-4 transition-transform', open && 'rotate-180')} /></Button>
			</div>
			{open && <div className="border-t bg-muted/10">
				{state?.loading && state.rows.length === 0 && <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังโหลดตัวเลือก</div>}
				{state?.error && <div className="flex items-center justify-between gap-3 px-4 py-4 text-sm text-destructive"><span>{state.error}</span><Button variant="outline" size="sm" onClick={() => void loadChildren(group)}>ลองใหม่</Button></div>}
				{state && !state.loading && !state.error && state.rows.length === 0 && <div className="py-6 text-center text-sm text-muted-foreground">ไม่พบตัวเลือกที่ตรงกับตัวกรอง</div>}
				{state && state.rows.length > 0 && <div className="hidden grid-cols-[minmax(280px,1.4fr)_minmax(220px,1fr)_minmax(280px,auto)_148px] gap-3 border-b bg-muted/30 py-1.5 pl-8 pr-3 text-[11px] font-medium text-muted-foreground xl:grid">
					<span>ตัวเลือก Shopee</span>
					<span>สินค้าที่จับคู่ใน SML</span>
					<div className="grid grid-cols-3 gap-3 text-right"><StockColumnLabels /></div>
					<span />
				</div>}
				<div className="divide-y">{state?.rows.map((product) => <ProductLine key={`${product.item_id}:${product.model_id}`} product={product} canManage={canManage} nested onMap={() => onMap(product)} onPool={() => onPool(product)} />)}</div>
				{state?.nextCursor && <div className="flex justify-center border-t py-2"><Button variant="outline" size="sm" disabled={state.loading} onClick={() => void loadChildren(group, true)}>{state.loading && <Loader2 className="h-4 w-4 animate-spin" />}โหลดตัวเลือกเพิ่ม</Button></div>}
			</div>}
		</div>
	})}</div>
}

function GroupStockTotals({ group, previewStale }: { group: ProductGroup; previewStale: boolean }) {
	const summary = buildShopeeStockGroupSummary(group, previewStale)
	const smlHint = summary.smlStatusText === 'สต๊อกร่วม'
		? 'มีตัวเลือกที่ใช้สต๊อก SML ก้อนเดียวกัน จึงไม่รวมยอดซ้ำ กดแสดงตัวเลือกเพื่อดูรายละเอียด'
		: summary.smlStatusText === 'หลายหน่วย'
			? 'ตัวเลือกใช้หน่วยฐานต่างกัน จึงไม่รวมเป็นตัวเลขเดียว กดแสดงตัวเลือกเพื่อดูรายละเอียด'
			: summary.smlStatusText === 'ตรวจใหม่'
				? 'ข้อมูลเปลี่ยนแล้ว กดบันทึกและตรวจสต๊อกเพื่อคำนวณยอดใหม่'
				: 'ยังไม่มียอดจากการตรวจสต๊อก'
	return (
		<div className="col-span-2 row-start-3 grid grid-cols-3 gap-2 rounded-md bg-muted/30 p-2 xl:col-span-1 xl:col-start-3 xl:row-start-1 xl:bg-transparent xl:p-0" aria-label={`ยอดรวม ${formatNumber(group.summary_count)} ตัวเลือก`}>
			<div className="min-w-0" title={summary.smlStatusText ? smlHint : `ยอด SML พร้อมใช้รวม ${formatNumber(summary.smlValue ?? 0)} ${summary.smlUnit}`}>
				<p className="truncate text-[10px] text-muted-foreground">SML พร้อมใช้รวม</p>
				<p className={cn('truncate text-sm font-semibold tabular-nums', summary.smlStatusText && 'text-muted-foreground')}>{summary.smlStatusText || formatNumber(summary.smlValue ?? 0)}{summary.smlValue != null && summary.smlUnit && <span className="ml-1 text-[10px] font-normal text-muted-foreground">{summary.smlUnit}</span>}</p>
			</div>
			<div className="min-w-0" title={`สต๊อก Shopee ปัจจุบันรวม ${formatNumber(summary.shopeeValue)}`}>
				<p className="truncate text-[10px] text-muted-foreground">Shopee ปัจจุบันรวม</p>
				<p className="truncate text-sm font-semibold tabular-nums">{formatNumber(summary.shopeeValue)}</p>
			</div>
			<div className="min-w-0" title={summary.targetStatusText || `ยอดที่ Nexflow จะตั้งใน Shopee รวม ${formatNumber(summary.targetValue ?? 0)}`}>
				<p className="truncate text-[10px] text-muted-foreground">Shopee เป้าหมายรวม</p>
				<p className={cn('truncate text-sm font-semibold tabular-nums', summary.targetStatusText && 'text-muted-foreground')}>{summary.targetStatusText || formatNumber(summary.targetValue ?? 0)}</p>
				{summary.changeText && <p className={cn('truncate text-[10px]', group.changed_count > 0 ? 'text-warning' : 'text-success')}>{summary.changeText}</p>}
			</div>
		</div>
	)
}

function StockColumnLabels() {
  return (
    <>
      <span title="ยอดคงเหลือ SML หลังหักใบสั่งขายค้างส่ง และก่อนแปลงเป็นหน่วยขาย Shopee">{STOCK_COLUMN_LABELS.sml}</span>
      <span title="ยอดสต๊อกปัจจุบันที่อ่านจาก Shopee">{STOCK_COLUMN_LABELS.shopee}</span>
      <span title="ยอดสต๊อกปลายทางที่ Nexflow จะตั้งใน Shopee เมื่อกดซิงก์">{STOCK_COLUMN_LABELS.target}</span>
    </>
  )
}

function ProductLine({ product, canManage, onMap, onPool, nested = false }: { product: ProductRow; canManage: boolean; onMap: () => void; onPool: () => void; nested?: boolean }) {
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
  const canManagePool = product.sml_item_code && (product.shared_pool_enabled || product.warning_codes.includes('duplicate_sml_item'))
  const excludedLocations = product.last_preview_excluded_locations ?? []
  const smlUsable = product.last_preview_sml_usable_qty ?? product.last_preview_balance
  const hasSMLAvailabilityBreakdown = product.last_preview_sml_physical_qty != null && product.last_preview_sml_outstanding_so_qty != null
  return (
    <>
	<div className={cn('grid gap-x-3 gap-y-2 px-3 py-2 xl:grid-cols-[minmax(280px,1.4fr)_minmax(220px,1fr)_minmax(280px,auto)_148px] xl:items-center', nested && 'pl-8')}>
      <div className="min-w-0">
		<p className="truncate text-sm font-medium leading-5" title={nested ? optionName || itemName : itemName}>{nested ? optionName || 'ตัวเลือกหลัก' : itemName}</p>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-xs">
		  {!nested && optionName && (
            <span className="max-w-[58%] shrink-0 truncate font-semibold text-foreground" title={`ตัวเลือกสินค้า: ${optionName}`}>
              <span className="font-normal text-muted-foreground">ตัวเลือก:</span> {optionName}
            </span>
          )}
          <span className="min-w-0 truncate text-muted-foreground" title={shopeeIdentity}>SKU: {sku}</span>
        </div>
        {(product.shared_pool_enabled || product.warning_codes.length > 0) && <div className="mt-1 flex flex-wrap gap-1">{product.shared_pool_enabled && <Badge variant="outline" className="border-info/30 bg-info/10 text-info">สต๊อกร่วม {formatNumber(product.pool_allocation_pct)}%</Badge>}{product.warning_codes.map((code) => <Badge key={code} variant="outline" className="border-warning/40 bg-warning/10 text-amber-800 dark:text-amber-200">{WARNING_LABEL[code] || code}</Badge>)}</div>}
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
      </div>
      <div className="grid grid-cols-3 gap-3 rounded-md bg-muted/30 p-2 xl:bg-transparent xl:p-0">
        <div className="min-w-0 text-left xl:text-right">
		  <p className="text-[11px] text-muted-foreground xl:hidden">{STOCK_COLUMN_LABELS.sml}</p>
          <p className="whitespace-nowrap text-sm font-medium"><span className="font-mono">{smlUsable == null ? 'รอตรวจ' : formatNumber(smlUsable)}</span>{smlUsable != null && baseUnit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{baseUnit}</span>}</p>
          {hasSMLAvailabilityBreakdown && <p className="mt-0.5 whitespace-nowrap text-[10px] text-muted-foreground" title="ยอดคงเหลือ SML ลบยอดใบสั่งขายที่ยังค้างส่ง">คงเหลือ {formatNumber(product.last_preview_sml_physical_qty)}{baseUnit ? ` ${baseUnit}` : ''} − ค้างส่ง {formatNumber(product.last_preview_sml_outstanding_so_qty)}{baseUnit ? ` ${baseUnit}` : ''}</p>}
          {(product.last_preview_pending_qty ?? 0) > 0 && <p className="whitespace-nowrap text-[10px] text-muted-foreground" title="ออเดอร์ที่ Nexflow สำรองไว้แต่ SML ยังไม่รับภาระ">Nexflow สำรอง {formatNumber(product.last_preview_pending_qty)}{baseUnit ? ` ${baseUnit}` : ''} · ใช้คำนวณ {formatNumber(product.last_preview_calculation_usable_qty)}{baseUnit ? ` ${baseUnit}` : ''}</p>}
        </div>
        <div className="min-w-0 text-left xl:text-right">
		  <p className="text-[11px] text-muted-foreground xl:hidden">{STOCK_COLUMN_LABELS.shopee}</p>
          <p className="whitespace-nowrap text-sm font-medium"><span className="font-mono">{formatNumber(product.shopee_available)}</span>{sellingUnit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{sellingUnit}</span>}</p>
          {product.shopee_reserved > 0 && <p className="text-[11px] text-warning">จอง {formatNumber(product.shopee_reserved)}</p>}
        </div>
        <div className="min-w-0 text-left xl:text-right">
		  <p className="text-[11px] text-muted-foreground xl:hidden">{STOCK_COLUMN_LABELS.target}</p>
          <p className="whitespace-nowrap text-sm font-medium"><span className="font-mono">{product.last_preview_target == null ? 'รอตรวจ' : formatNumber(product.last_preview_target)}</span>{product.last_preview_target != null && sellingUnit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{sellingUnit}</span>}</p>
        </div>
      </div>
      {!canManage ? (canManagePool ? (
        <Button variant="outline" size="sm" className="h-8 justify-self-end whitespace-nowrap px-2.5 xl:w-full" onClick={onPool}><GitMerge className="h-4 w-4" />ดูสัดส่วน</Button>
      ) : <span className="justify-self-end text-xs text-muted-foreground">ดูอย่างเดียว</span>) : canManagePool ? (
        <div className="flex items-center justify-end gap-1">
          <Button variant="outline" size="sm" className="h-8 flex-1 whitespace-nowrap px-2.5" onClick={onPool}><GitMerge className="h-4 w-4" />{product.shared_pool_enabled ? 'ปรับสัดส่วน' : 'แบ่งสต๊อก'}</Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild><Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" aria-label="คำสั่งเพิ่มเติม" title="คำสั่งเพิ่มเติม"><MoreHorizontal className="h-4 w-4" /></Button></DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              <DropdownMenuItem onSelect={onMap}><Settings2 className="h-4 w-4" />เปลี่ยนสินค้าและหน่วย SML</DropdownMenuItem>
              {product.marketplace_alias_id && <DropdownMenuItem asChild><Link to={`/marketplace-aliases?q=${encodeURIComponent(product.model_sku || product.item_sku || String(product.item_id))}`}>ดูใน Product Master</Link></DropdownMenuItem>}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ) : <Button variant="outline" size="sm" className="h-8 justify-self-end whitespace-nowrap px-2.5 xl:w-full" onClick={onMap}><Settings2 className="h-4 w-4" />{product.excluded ? 'แก้ไข' : product.sml_item_code ? 'เปลี่ยนสินค้า' : 'จับคู่สินค้า'}</Button>}
      {(excludedLocations.length > 0 || !!product.last_preview_excluded_balance) && (
        <ProductExcludedLocations
          locations={excludedLocations}
          fallbackBalance={product.last_preview_excluded_balance}
          fallbackUnit={baseUnit}
        />
      )}
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
  return <div className="divide-y">{runs.map((run) => <div key={run.id} className="grid gap-2 px-4 py-3 sm:grid-cols-[140px_1fr_auto] sm:items-center"><div><Badge variant="outline">{run.run_type === 'catalog' ? 'รายการสินค้า' : run.run_type === 'preview' ? 'ตรวจสอบสต๊อก' : 'ซิงก์สต๊อก'}</Badge><p className="mt-1 text-xs text-muted-foreground">{formatDateTime(run.started_at)}</p></div><div className="text-sm">ตรวจ {formatNumber(run.total_count)} · เปลี่ยน {formatNumber(run.changed_count)} · ต้องแก้ {formatNumber(run.blocked_count)} · ไม่สำเร็จ {formatNumber(run.error_count)}{run.error_message && <p className="mt-1 text-xs text-destructive">{run.error_message}</p>}</div><Badge variant={run.status === 'success' ? 'default' : 'outline'}>{run.status}</Badge></div>)}</div>
}

function CatalogInfoDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>อัปเดตรายการสินค้าจาก Shopee</DialogTitle>
          <DialogDescription>ดึงสินค้าและตัวเลือกล่าสุดจาก Shopee มาตรวจเทียบกับสินค้าและหน่วยนับใน SML</DialogDescription>
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

function sharedPoolMemberKey(member: Pick<SharedPoolMember, 'item_id' | 'model_id'>) {
  return `${member.item_id}:${member.model_id}`
}

function equalSharedPoolAllocations(members: SharedPoolMember[]) {
  if (!members.length) return {} as Record<string, string>
  const base = Math.floor(10000 / members.length) / 100
  return Object.fromEntries(members.map((member, index) => [
    sharedPoolMemberKey(member),
    (index === members.length - 1 ? 100 - base * (members.length - 1) : base).toFixed(2),
  ]))
}

function SharedPoolDialog({ shopID, itemCode, fallbackStockPct, canManage, onClose, onSaved }: { shopID: number; itemCode: string; fallbackStockPct: number; canManage: boolean; onClose: () => void; onSaved: () => Promise<void> }) {
  const [pool, setPool] = useState<SharedPool | null>(null)
  const [allocations, setAllocations] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [saving, setSaving] = useState(false)
  const [retryNonce, setRetryNonce] = useState(0)
  const requestSequence = useRef(0)

  useEffect(() => {
    if (!itemCode || !shopID) {
      setPool(null)
      setAllocations({})
      setLoadError('')
      return
    }
    const current = requestSequence.current + 1
    requestSequence.current = current
    setLoading(true)
    setPool(null)
    setAllocations({})
    setLoadError('')
    void client.get<SharedPool>(`/api/settings/shopee-stock/${shopID}/shared-pool`, { params: { sml_item_code: itemCode } })
      .then((response) => {
        if (requestSequence.current !== current) return
        const nextPool = response.data
        setPool(nextPool)
        setAllocations(nextPool.configured
          ? Object.fromEntries(nextPool.members.map((member) => [sharedPoolMemberKey(member), member.pool_allocation_pct.toFixed(2)]))
          : equalSharedPoolAllocations(nextPool.members))
      })
      .catch((error) => {
        if (requestSequence.current !== current) return
        const message = errorText(error)
        setLoadError(message)
        toast.error(message)
      })
      .finally(() => { if (requestSequence.current === current) setLoading(false) })
  }, [itemCode, shopID, retryNonce])

  const allocationValues = pool?.members.map((member) => Number(allocations[sharedPoolMemberKey(member)] ?? 0)) ?? []
  const allocationTotal = allocationValues.reduce((total, value) => total + (Number.isFinite(value) ? value : 0), 0)
  const allocationValid = !!pool && pool.members.length >= 2 && allocationValues.every((value) => Number.isFinite(value) && value > 0 && value <= 100) && Math.abs(allocationTotal - 100) < 0.001
  const dirty = !!pool && (!pool.configured || pool.members.some((member) => Math.abs(Number(allocations[sharedPoolMemberKey(member)] ?? 0) - member.pool_allocation_pct) > 0.001))
  const scopeBalance = pool?.members.find((member) => member.last_preview_balance != null)?.last_preview_balance
  // last_preview_pending_qty is a legacy column name. New previews persist the
  // tenant-wide base demand in it, so every member of the same pool carries the
  // same aggregate and it must be counted once.
  const pendingBase = pool?.members.reduce((total, member) => Math.max(total, member.last_preview_pending_qty ?? 0), 0) ?? 0
  const stockPct = pool?.stock_pct ?? fallbackStockPct
  const persistedPoolBaseTarget = pool?.members.find((member) => member.last_preview_pool_base_target != null)?.last_preview_pool_base_target
  const poolBaseTarget = persistedPoolBaseTarget ?? (scopeBalance == null ? null : Math.floor(Math.max(scopeBalance - pendingBase, 0) * stockPct / 100))
  const baseUnit = pool?.members[0] ? formatUnitLabel(pool.members[0].sml_base_unit_code, pool.members[0].sml_base_unit_name) : ''

  const save = async () => {
    if (!canManage || !pool || !allocationValid || saving) return
    setSaving(true)
    try {
      await client.put(`/api/settings/shopee-stock/${shopID}/shared-pool`, {
        sml_item_code: pool.sml_item_code,
        auto_manage_members: true,
        members: pool.members.map((member) => ({
          item_id: member.item_id,
          model_id: member.model_id,
          allocation_pct: Number(allocations[sharedPoolMemberKey(member)]),
          updated_at: member.updated_at,
        })),
      })
      toast.success('บันทึกสัดส่วนแล้ว ระบบกำลังเตรียมทุกรายการในกลุ่ม เมื่อเสร็จให้ตรวจสอบสต๊อก')
      await onSaved()
    } catch (error) {
      toast.error(errorText(error))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={!!itemCode} onOpenChange={(open) => !open && !saving && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-4xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><GitMerge className="h-5 w-5 text-info" />{canManage ? 'แบ่งสต๊อกที่ใช้ร่วมกัน' : 'รายละเอียดสต๊อกที่ใช้ร่วมกัน'}</DialogTitle>
          <DialogDescription>{canManage ? 'กำหนดว่าสต๊อก SML ก้อนเดียวกันจะแบ่งให้แต่ละรายการ Shopee เท่าไร ยอดรวมต้องเท่ากับ 100%' : 'ตรวจสัดส่วนที่ผู้ดูแลระบบกำหนดไว้สำหรับแต่ละรายการ Shopee'}</DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className="flex min-h-48 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังโหลดรายการในกลุ่ม...</div>
        ) : loadError ? (
          <div className="flex min-h-48 flex-col items-center justify-center gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-6 text-center">
            <AlertTriangle className="h-6 w-6 text-destructive" />
            <div><p className="text-sm font-medium">โหลดรายการสต๊อกร่วมกันไม่สำเร็จ</p><p className="mt-1 text-xs text-muted-foreground">{loadError}</p></div>
            <Button type="button" variant="outline" size="sm" onClick={() => setRetryNonce((value) => value + 1)}><RefreshCw className="h-4 w-4" />ลองใหม่</Button>
          </div>
        ) : pool ? (
          <div className="space-y-4">
            <div className="rounded-md border bg-muted/30 px-3 py-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0"><p className="truncate text-sm font-semibold">{pool.sml_item_code} · {pool.sml_item_name}</p><p className="text-xs text-muted-foreground">ส่งขึ้น Shopee {formatNumber(stockPct)}% ก่อนแบ่งตามสัดส่วนด้านล่าง</p></div>
                {canManage && <Button type="button" variant="outline" size="sm" onClick={() => setAllocations(equalSharedPoolAllocations(pool.members))}>แบ่งเท่ากัน</Button>}
              </div>
              <div className="mt-2 grid grid-cols-3 gap-3 border-t pt-2 text-xs">
                <div><span className="text-muted-foreground">คงเหลือ SML</span><p className="mt-0.5 font-mono text-sm font-semibold">{scopeBalance == null ? 'รอตรวจสอบ' : `${formatNumber(scopeBalance)} ${baseUnit}`}</p></div>
                <div><span className="text-muted-foreground">ออเดอร์รอลง SML</span><p className="mt-0.5 font-mono text-sm font-semibold">{scopeBalance == null ? 'รอตรวจสอบ' : `${formatNumber(pendingBase)} ${baseUnit}`}</p></div>
                <div><span className="text-muted-foreground">ก้อนที่นำไปแบ่ง</span><p className="mt-0.5 font-mono text-sm font-semibold">{poolBaseTarget == null ? 'รอตรวจสอบ' : `${formatNumber(poolBaseTarget)} ${baseUnit}`}</p></div>
              </div>
            </div>

            <div className="overflow-hidden rounded-md border">
              <div className="hidden grid-cols-[minmax(0,1fr)_110px_120px_130px] gap-3 border-b bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground md:grid">
                <span>รายการ Shopee</span><span className="text-right">Shopee ตอนนี้</span><span className="text-right">สัดส่วน</span><span className="text-right">ประมาณการส่ง</span>
              </div>
              <div className="divide-y">
                {pool.members.map((member) => {
                  const key = sharedPoolMemberKey(member)
                  const allocation = Number(allocations[key] ?? 0)
                  const target = poolBaseTarget == null || !Number.isFinite(allocation) || member.unit_factor <= 0
                    ? null
                    : Math.floor(poolBaseTarget * allocation / 100 / member.unit_factor)
                  const itemName = member.item_name.trim() || member.model_name.trim() || `Item ${member.item_id}`
                  const optionName = member.model_name.trim() && member.model_name.trim() !== itemName ? member.model_name.trim() : ''
                  const sellingUnit = formatUnitLabel(member.sml_unit_code, member.sml_unit_name)
                  return (
                    <div key={key} className="grid gap-2 px-3 py-2.5 md:grid-cols-[minmax(0,1fr)_110px_120px_130px] md:items-center md:gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium" title={itemName}>{itemName}</p>
                        <p className="truncate text-xs text-muted-foreground" title={optionName || undefined}>{optionName ? `ตัวเลือก: ${optionName} · ` : ''}Item {member.item_id}{member.model_id ? ` / Model ${member.model_id}` : ''}</p>
                        <p className="text-[11px] text-muted-foreground">Shopee 1 ชิ้น ใช้ {formatNumber(member.unit_factor)} {baseUnit}</p>
                      </div>
                      <div className="flex items-center justify-between text-sm md:block md:text-right"><span className="text-xs text-muted-foreground md:hidden">Shopee ตอนนี้</span><span className="font-mono font-medium">{formatNumber(member.shopee_available)} {sellingUnit}</span></div>
                      <div>
                        <Label htmlFor={`pool-${key}`} className="sr-only">สัดส่วนของ {itemName}</Label>
                        <div className="relative">
                          <Input id={`pool-${key}`} type="number" min="0.01" max="100" step="0.01" inputMode="decimal" disabled={!canManage} className="h-9 pr-8 text-right" value={allocations[key] ?? ''} onChange={(event) => setAllocations((current) => ({ ...current, [key]: event.target.value }))} />
                          <span className="pointer-events-none absolute right-3 top-2 text-xs text-muted-foreground">%</span>
                        </div>
                      </div>
                      <div className="flex items-center justify-between text-sm md:block md:text-right"><span className="text-xs text-muted-foreground md:hidden">ประมาณการส่ง</span><span className="font-mono font-semibold">{target == null ? 'รอตรวจสอบ' : `${formatNumber(target)} ${sellingUnit}`}</span></div>
                    </div>
                  )
                })}
              </div>
            </div>

            <Alert className={allocationValid ? 'border-success/40 bg-success/10' : 'border-warning/50 bg-warning/10'}>
              {allocationValid ? <CheckCircle2 className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
              <AlertTitle>สัดส่วนรวม {formatNumber(allocationTotal)}%</AlertTitle>
              <AlertDescription>{canManage ? (allocationValid ? 'พร้อมบันทึก ระบบจะเตรียมทุกรายการในกลุ่มให้และรอตรวจสอบสต๊อกก่อนส่งจริง' : 'ปรับสัดส่วนของทุกรายการให้รวมกันเท่ากับ 100%') : (allocationValid ? 'สัดส่วนที่บันทึกไว้รวมกันถูกต้อง' : 'สัดส่วนที่บันทึกไว้ยังไม่พร้อมใช้งาน กรุณาแจ้งผู้ดูแลระบบ')}</AlertDescription>
            </Alert>
            <p className="text-xs text-muted-foreground">Nexflow จะส่งสต๊อกตามสัดส่วนนี้แทนการส่งยอดเต็มซ้ำให้ทุกสินค้า และหักออเดอร์ที่ยังไม่ลง SML ก่อนคำนวณ</p>
          </div>
        ) : null}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={saving}>{canManage ? 'ยกเลิก' : 'ปิด'}</Button>
          {canManage && <Button type="button" onClick={save} disabled={!allocationValid || !dirty || saving}>{saving && <Loader2 className="h-4 w-4 animate-spin" />}{saving ? 'กำลังบันทึก...' : 'บันทึกสัดส่วน'}</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MappingDialog({ product, shopID, onClose, onConfigurePool, onSaved }: { product: ProductRow | null; shopID: number; onClose: () => void; onConfigurePool: (itemCode: string) => void; onSaved: () => Promise<void> }) {
  const [selected, setSelected] = useState<StockCatalogOption | null>(null)
  const [units, setUnits] = useState<StockCatalogUnit[]>([])
  const [unitCode, setUnitCode] = useState('')
  const [quantityMultiplier, setQuantityMultiplier] = useState('1')
  const [quantityMode, setQuantityMode] = useState<MarketplaceQuantityMode>('marketplace_qty')
  const [busy, setBusy] = useState(false)
  const [confirmExclude, setConfirmExclude] = useState(false)
  const [saveImpact, setSaveImpact] = useState<MarketplaceAliasImpact | null>(null)
  const [recoveredPolicyJob, setRecoveredPolicyJob] = useState<MarketplaceStockPolicyJob | null>(null)
  const [policyRefreshKey, setPolicyRefreshKey] = useState(0)

  useEffect(() => {
    let active = true
    setConfirmExclude(false)
    setSaveImpact(null)
    setSelected(product?.sml_item_code ? { item_code: product.sml_item_code, item_name: product.sml_item_name, standard_unit: product.sml_unit_code, units: [] } : null)
    setUnits([])
    setUnitCode(product?.sml_unit_code || '')
    const savedMultiplier = product?.marketplace_quantity_multiplier || 1
    setQuantityMultiplier(String(savedMultiplier))
    setQuantityMode(quantityModeFromMultiplier(savedMultiplier))
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
  }, [product?.item_id, product?.model_id, product?.sml_item_code, product?.sml_item_name, product?.sml_unit_code, product?.manual_unit_factor, product?.marketplace_quantity_multiplier, product?.marketplace_stock_policy])

  useEffect(() => {
    const aliasID = product?.marketplace_alias_id
    if (!aliasID || product?.marketplace_stock_policy !== 'zeroing') {
      setRecoveredPolicyJob(null)
      return
    }
    let active = true
    let timer = 0
    const refresh = async () => {
      try {
        const response = await client.get<MarketplaceStockPolicyJob>(`/api/marketplace-aliases/${aliasID}/policy-job`)
        if (!active) return
        setRecoveredPolicyJob(response.data)
        if (response.data.status === 'completed') {
          toast.success('Shopee ยืนยัน stock 0 แล้ว กำลังรีเฟรชรายการ')
          await onSaved()
          return
        }
        if ((response.data.status === 'failed' || response.data.status === 'unknown') && response.data.attempt_count >= 10) return
        timer = window.setTimeout(() => void refresh(), 2000)
      } catch {
        if (active) setRecoveredPolicyJob(null)
      }
    }
    void refresh()
    return () => { active = false; window.clearTimeout(timer) }
  }, [onSaved, policyRefreshKey, product?.marketplace_alias_id, product?.marketplace_stock_policy])

  const retryRecoveredPolicyJob = async () => {
    if (!recoveredPolicyJob) return
    setBusy(true)
    try {
      const response = await client.post<MarketplaceStockPolicyJob>(`/api/marketplace-aliases/policy-jobs/${recoveredPolicyJob.id}/retry`)
      setRecoveredPolicyJob(response.data)
      setPolicyRefreshKey((value) => value + 1)
      toast.success('นำงานตั้ง stock 0 กลับเข้าคิวแล้ว')
    } catch (error) {
      toast.error(errorText(error))
    } finally {
      setBusy(false)
    }
  }

  const selectProduct = async (picked: CatalogMatch) => {
    setBusy(true)
    try {
      const response = await client.get<{ items: StockCatalogOption[] }>('/api/settings/shopee-stock/catalog-search', { params: { q: picked.item_code } })
      const item = (response.data.items ?? []).find((candidate) => candidate.item_code === picked.item_code)
      if (!item) {
        toast.error('ไม่พบข้อมูลหน่วยของสินค้านี้ กรุณาอัปเดตรายการสินค้าจาก Shopee แล้วลองใหม่')
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
    const multiplier = Number(quantityMultiplier)
    if (!selected || !unitCode) return null
    if (!Number.isInteger(multiplier) || multiplier < 1 || multiplier > 1_000_000) {
      toast.error('จำนวนที่ตัดต้องเป็นจำนวนเต็ม 1 ถึง 1,000,000')
      return null
    }
    return multiplier
  }
  const previewSave = async () => {
    if (!product || !selected) return
    const multiplier = validateSelection()
    if (multiplier == null) return
    const currentPolicy = product.marketplace_stock_policy ?? 'blocked'
    const nextPolicy: StockPolicy = currentPolicy === 'zeroing' ? 'zeroing' : 'managed'
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
        unit_code: unitCode,
        quantity_multiplier: multiplier,
        sales_enabled: true,
        stock_policy: nextPolicy,
        acknowledge_manual_unmanaged: false,
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
    const multiplier = excluded ? 1 : validateSelection()
    if (!excluded && multiplier == null) return
    const nextPolicy: StockPolicy = product.marketplace_stock_policy === 'zeroing' ? 'zeroing' : 'managed'
    setBusy(true)
    try {
      const response = await client.put<{ job?: { id: string } }>(`/api/settings/shopee-stock/${shopID}/mappings/${product.item_id}/${product.model_id}`, {
        sml_item_code: excluded ? '' : selected?.item_code,
        sml_unit_code: excluded ? '' : unitCode,
        excluded,
        updated_at: product.updated_at,
        marketplace_alias_id: product.marketplace_alias_id || '',
        marketplace_alias_updated_at: product.marketplace_alias_updated_at || null,
        quantity_multiplier: multiplier,
        sales_enabled: true,
        stock_policy: nextPolicy,
        acknowledge_manual_unmanaged: false,
        expected_mapping_revision: saveImpact?.current_mapping_revision ?? 0,
        impact_digest: saveImpact?.impact_digest ?? '',
      })
      toast.success(excluded ? 'ยกเว้นสินค้านี้แล้ว' : `บันทึก revision แล้ว กำลังปรับข้อมูล${response.data.job?.id ? ` · งาน ${response.data.job.id.slice(0, 8)}` : ''}`)
      notifyWorkQueueChanged()
      await onSaved()
    } catch (error) {
      toast.error(errorText(error))
    } finally {
      setBusy(false)
    }
  }
  const selectedUnit = units.find((unit) => unit.code === unitCode)
  const currentStockPolicy: StockPolicy = product?.marketplace_stock_policy ?? 'blocked'
  const exclusionBlocked = currentStockPolicy === 'managed' || currentStockPolicy === 'zeroing'
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
          <div className={cn('flex flex-wrap gap-2', exclusionBlocked ? 'justify-end' : 'justify-between')}>
            {!exclusionBlocked && <Button type="button" variant="outline" className="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive" onClick={() => setConfirmExclude(true)} disabled={busy}>ยกเว้นสินค้านี้</Button>}
            <div className="flex gap-2"><Button variant="outline" onClick={onClose} disabled={busy}>ยกเลิก</Button><Button onClick={previewSave} disabled={busy || currentStockPolicy === 'zeroing' || !selected || !unitCode || !quantityMultiplier}>{busy && <Loader2 className="h-4 w-4 animate-spin" />}ตรวจสอบและบันทึก</Button></div>
          </div>
        )}
      >
        {product?.warning_codes.includes('duplicate_sml_item') && (
          <Alert className="mt-4 border-warning/50 bg-warning/10">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>รหัส SML นี้ผูกกับหลายรายการ Shopee</AlertTitle>
            <AlertDescription>
              ระบบจะรวมเป็นสต๊อกร่วมและแบ่งเท่ากันอัตโนมัติเมื่อกดตรวจสต๊อก หากต้องการแบ่งไม่เท่ากันสามารถปรับสัดส่วนได้
            </AlertDescription>
            <Button type="button" variant="outline" size="sm" className="mt-3" onClick={() => product.sml_item_code && onConfigurePool(product.sml_item_code)}><GitMerge className="h-4 w-4" />ปรับสัดส่วน</Button>
          </Alert>
        )}
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
                      Shopee 1 ชิ้น = {quantityMultiplier || '-'} {selectedUnitLabel}{baseUnitLabel && Number.isInteger(Number(quantityMultiplier)) ? ` = ${formatNumber(Number(quantityMultiplier) * selectedFactor)} ${baseUnitLabel}` : ''}
                    </p>
                  )}
                </div>
                <MarketplaceQuantityField idPrefix="shopee-stock" mode={quantityMode} multiplier={quantityMultiplier} onModeChange={setQuantityMode} onMultiplierChange={setQuantityMultiplier} />
                <p className="text-xs text-muted-foreground">หลังบันทึก ระบบจะใช้การจับคู่นี้กับออเดอร์และการซิงก์สต๊อกโดยอัตโนมัติ</p>
                {currentStockPolicy === 'zeroing' && (
                  <div className="flex items-start justify-between gap-3 rounded-md border border-warning/40 bg-warning/5 p-3">
                    <div className="text-xs">
                      <p className="font-medium">{recoveredPolicyJob && (recoveredPolicyJob.status === 'failed' || recoveredPolicyJob.status === 'unknown') && recoveredPolicyJob.attempt_count >= 10 ? 'ยืนยันสต๊อก 0 ไม่สำเร็จ' : 'กำลังรอ Shopee ยืนยันสต๊อก 0'}</p>
                      {recoveredPolicyJob?.error_message && <p className="mt-1 text-muted-foreground">{recoveredPolicyJob.error_message}</p>}
                    </div>
                    {recoveredPolicyJob && (recoveredPolicyJob.status === 'failed' || recoveredPolicyJob.status === 'unknown') && recoveredPolicyJob.attempt_count >= 10 && <Button type="button" size="sm" variant="outline" onClick={() => void retryRecoveredPolicyJob()} disabled={busy}><RefreshCw className="h-3.5 w-3.5" />ลองใหม่</Button>}
                  </div>
                )}
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
        title="ยืนยันการจับคู่สินค้า?"
		description={saveImpact && product && selected ? `${productDisplayName(product)}\nสินค้า SML: ${selected.item_code} · ${selected.item_name}\n${marketplaceImpactFormulaLines(saveImpact).join('\n')}\nบิลเปิดที่จะอัปเดต: ${saveImpact.open_items.toLocaleString()} รายการ ใน ${saveImpact.open_bills.toLocaleString()} บิล\nรายการสต๊อกที่เกี่ยวข้อง: ${saveImpact.stock_mappings.toLocaleString()} รายการ${saveImpact.stock_conflicts ? `\nพบสินค้าที่ใช้สต๊อกร่วมกัน ${saveImpact.stock_conflicts.toLocaleString()} รายการ` : ''}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน หลังบันทึกให้ตรวจสอบสต๊อกก่อนส่งจริง` : undefined}
        confirmLabel="บันทึกการจับคู่"
        onConfirm={() => save(false)}
      />
    </>
  )
}
