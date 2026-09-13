import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, ArrowRight, CheckCircle2, Loader2, PackageSearch } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { MarketplaceQuantityField, type MarketplaceQuantityMode } from '@/components/marketplace/MarketplaceQuantityField'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  buildTikTokShadowMappingPayload,
  tiktokShadowMappingValidation,
} from '@/lib/tiktok-shop-operations'
import { MapItemModal } from '@/pages/BillDetail/components/MapItemModal'
import type { CatalogMatch, MarketplaceAliasImpact, UnitOption } from '@/types'

import type { TikTokBillShadowItem } from './TikTokBillShadowDialog'

interface Props {
  open: boolean
  shopID: string
  shopName: string
  orderID: string
  item: TikTokBillShadowItem | null
  onOpenChange: (open: boolean) => void
}

export function TikTokProductMappingDialog({ open, shopID, shopName, orderID, item, onOpenChange }: Props) {
  const [pickerOpen, setPickerOpen] = useState(false)
  const [product, setProduct] = useState<CatalogMatch | null>(null)
  const [units, setUnits] = useState<UnitOption[]>([])
  const [unitCode, setUnitCode] = useState('')
  const [quantityMode, setQuantityMode] = useState<MarketplaceQuantityMode>('marketplace_qty')
  const [multiplier, setMultiplier] = useState('1')
  const [loadingUnits, setLoadingUnits] = useState(false)
  const [impact, setImpact] = useState<MarketplaceAliasImpact | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open || !item) return
    setPickerOpen(false)
    setProduct(null)
    setUnits([])
    setUnitCode('')
    setQuantityMode('marketplace_qty')
    setMultiplier('1')
    setImpact(null)
    setError('')
  }, [item, open])

  useEffect(() => {
    if (!product) return
    let active = true
    setUnits([])
    setUnitCode('')
    setImpact(null)
    setError('')
    setLoadingUnits(true)
    client.get<{ units: UnitOption[] }>(`/api/catalog/${encodeURIComponent(product.item_code)}/units`)
      .then((response) => {
        if (!active) return
        const next = response.data.units ?? []
        setUnits(next)
        const preferred = next.find((unit) => unit.code === product.unit_code)
          ?? next.find((unit) => unit.is_default)
          ?? next[0]
        setUnitCode(preferred?.code ?? '')
        if (next.length === 0) setError('สินค้านี้ไม่มีหน่วยที่พร้อมใช้ใน Catalog กรุณาอัปเดต Catalog แล้วลองใหม่')
      })
      .catch(() => {
        if (active) setError('โหลดหน่วย SML ไม่สำเร็จ กรุณาลองใหม่')
      })
      .finally(() => {
        if (active) setLoadingUnits(false)
      })
    return () => { active = false }
  }, [product])

  const multiplierValue = Number(multiplier)
  const validationError = tiktokShadowMappingValidation(product?.item_code ?? '', unitCode, multiplierValue)
  const selectedUnit = units.find((unit) => unit.code === unitCode)
  const baseQuantity = useMemo(() => {
    if (!selectedUnit || !Number.isInteger(multiplierValue)) return null
    const stand = Number(selectedUnit.stand_value_exact ?? selectedUnit.stand_value)
    const divide = Number(selectedUnit.divide_value_exact ?? selectedUnit.divide_value)
    return Number.isFinite(stand) && Number.isFinite(divide) && divide > 0 ? multiplierValue * stand / divide : null
  }, [multiplierValue, selectedUnit])

  if (!item) return null

  const payload = () => buildTikTokShadowMappingPayload({
    productID: item.product_id,
    skuID: item.sku_id,
    itemCode: product?.item_code ?? '',
    unitCode,
    quantityMultiplier: multiplierValue,
  })
  const endpoint = `/api/tiktok-shop-api/orders/${encodeURIComponent(shopID)}/${encodeURIComponent(orderID)}/bill-shadow-mapping`

  const previewImpact = async () => {
    if (validationError) return
    setPreviewing(true)
    setError('')
    try {
      const response = await client.post<MarketplaceAliasImpact>(`${endpoint}/impact-preview`, payload())
      setImpact(response.data)
    } catch (cause: unknown) {
      setImpact(null)
      setError(apiErrorMessage(cause, 'ตรวจผลกระทบการจับคู่ไม่สำเร็จ'))
    } finally {
      setPreviewing(false)
    }
  }

  const confirmMapping = async () => {
    if (!impact || impact.conversion_status !== 'ready' || validationError) return
    setConfirming(true)
    setError('')
    try {
      await client.post(`${endpoint}/confirm`, {
        ...payload(),
        expected_mapping_revision: impact.current_mapping_revision,
        impact_digest: impact.impact_digest,
      })
      toast.success('บันทึก Product Master สำหรับร้าน TikTok Shop นี้แล้ว')
      onOpenChange(false)
    } catch (cause: unknown) {
      const status = (cause as { response?: { status?: number } }).response?.status
      if (status === 409) setImpact(null)
      setError(apiErrorMessage(cause, 'บันทึกการจับคู่ไม่สำเร็จ'))
    } finally {
      setConfirming(false)
    }
  }

  const changeSelection = () => {
    setImpact(null)
    setError('')
  }

  const sourceName = [item.product_name, item.variant_name].filter(Boolean).join(' / ')
  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="grid max-h-[92dvh] max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0">
          <DialogHeader className="border-b border-border px-4 py-4 pr-12 sm:px-6">
            <DialogTitle>จับคู่สินค้า TikTok Shop กับ SML</DialogTitle>
            <DialogDescription>
              ร้าน {shopName || shopID} · Order <span className="font-mono text-foreground">{orderID}</span>
            </DialogDescription>
          </DialogHeader>

          <div className="min-h-0 space-y-4 overflow-y-auto px-4 py-4 sm:px-6">
            <TikTokMappingSelection
              item={item}
              product={product}
              units={units}
              unitCode={unitCode}
              quantityMode={quantityMode}
              multiplier={multiplier}
              multiplierValue={multiplierValue}
              baseQuantity={baseQuantity}
              loadingUnits={loadingUnits}
              validationError={validationError}
              onPickProduct={() => setPickerOpen(true)}
              onUnitChange={(value) => { setUnitCode(value); changeSelection() }}
              onModeChange={(value) => { setQuantityMode(value); changeSelection() }}
              onMultiplierChange={(value) => { setMultiplier(value); changeSelection() }}
            />

            {error && (
              <Alert variant="destructive">
                <AlertTriangle className="h-4 w-4" />
                <AlertTitle>ดำเนินการไม่สำเร็จ</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            {impact && <TikTokMappingImpact impact={impact} />}

            <p className="text-xs leading-5 text-muted-foreground">
              การบันทึกนี้สร้างเฉพาะ Product Master mapping สำหรับร้าน TikTok Shop นี้ ยังไม่สร้าง Bill ไม่ส่ง SML และไม่เปลี่ยนสถานะออเดอร์
            </p>
          </div>

          <DialogFooter className="border-t border-border bg-muted/30 px-4 py-3 sm:px-6">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={previewing || confirming}>ยกเลิก</Button>
            {impact ? (
              <Button type="button" onClick={() => void confirmMapping()} disabled={confirming || impact.conversion_status !== 'ready'}>
                {confirming && <Loader2 className="h-4 w-4 animate-spin" />}
                ยืนยันการจับคู่
              </Button>
            ) : (
              <Button type="button" onClick={() => void previewImpact()} disabled={previewing || Boolean(validationError) || loadingUnits}>
                {previewing && <Loader2 className="h-4 w-4 animate-spin" />}
                ตรวจผลกระทบ
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <MapItemModal
        open={open && pickerOpen}
        rawName={sourceName}
        rawNameLabel="สินค้า TikTok Shop ที่ต้องจับคู่"
        currentCode={product?.item_code ?? item.mapping.item_code ?? ''}
        currentUnit={unitCode || item.mapping.unit_code || ''}
        currentPrice={Number(item.unit_sale_price)}
        onPick={(_, __, picked) => {
          if (picked) setProduct(picked)
          setPickerOpen(false)
        }}
        onClose={() => setPickerOpen(false)}
      />
    </>
  )
}

function TikTokMappingSelection({
  item, product, units, unitCode, quantityMode, multiplier, multiplierValue, baseQuantity,
  loadingUnits, validationError, onPickProduct, onUnitChange, onModeChange, onMultiplierChange,
}: {
  item: TikTokBillShadowItem
  product: CatalogMatch | null
  units: UnitOption[]
  unitCode: string
  quantityMode: MarketplaceQuantityMode
  multiplier: string
  multiplierValue: number
  baseQuantity: number | null
  loadingUnits: boolean
  validationError: string
  onPickProduct: () => void
  onUnitChange: (value: string) => void
  onModeChange: (value: MarketplaceQuantityMode) => void
  onMultiplierChange: (value: string) => void
}) {
  return (
    <>
      <section aria-labelledby="tiktok-mapping-source">
        <h2 id="tiktok-mapping-source" className="mb-2 text-sm font-semibold">สินค้าจาก TikTok Shop</h2>
        <div className="rounded-lg border border-border bg-muted/30 p-3">
          <div className="text-sm font-medium">{item.product_name}</div>
          <div className="mt-0.5 text-xs text-muted-foreground">{item.variant_name || 'ไม่มีตัวเลือก'}{item.seller_sku ? ` · SKU ${item.seller_sku}` : ''}</div>
          <div className="mt-1 break-all font-mono text-[11px] text-muted-foreground">Product {item.product_id} · SKU {item.sku_id}</div>
        </div>
      </section>

      <section aria-labelledby="tiktok-mapping-target" className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id="tiktok-mapping-target" className="text-sm font-semibold">สินค้าและหน่วยใน SML</h2>
          <Badge variant="outline">ผูกเฉพาะร้านนี้</Badge>
        </div>
        {product ? (
          <div className="flex flex-col gap-3 rounded-lg border border-border p-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <div className="font-mono text-sm font-semibold">{product.item_code}</div>
              <div className="mt-0.5 text-sm text-muted-foreground">{product.item_name}</div>
            </div>
            <Button type="button" variant="outline" size="sm" onClick={onPickProduct}>เปลี่ยนสินค้า SML</Button>
          </div>
        ) : (
          <Button type="button" variant="outline" className="w-full justify-start gap-2" onClick={onPickProduct}>
            <PackageSearch className="h-4 w-4" />เลือกสินค้า SML
          </Button>
        )}
        {product && (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="tiktok-shadow-unit">หน่วย SML</Label>
              <Select value={unitCode} onValueChange={onUnitChange} disabled={loadingUnits || units.length === 0}>
                <SelectTrigger id="tiktok-shadow-unit" className="h-10"><SelectValue placeholder={loadingUnits ? 'กำลังโหลดหน่วย...' : 'เลือกหน่วย SML'} /></SelectTrigger>
                <SelectContent>{units.map((unit) => <SelectItem key={unit.code} value={unit.code}>{unit.code}{unit.name_1 && unit.name_1 !== unit.code ? ` · ${unit.name_1}` : ''}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <MarketplaceQuantityField idPrefix="tiktok-shadow" mode={quantityMode} multiplier={multiplier} onModeChange={onModeChange} onMultiplierChange={onMultiplierChange} />
            {!loadingUnits && validationError && <p className="text-xs text-destructive" role="alert">{validationError}</p>}
            {!validationError && (
              <div className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
                TikTok 1 รายการ <ArrowRight className="mx-1 inline h-3.5 w-3.5" /> {multiplierValue.toLocaleString()} {unitCode}
                {baseQuantity != null ? ` · ${baseQuantity.toLocaleString('th-TH', { maximumFractionDigits: 6 })} หน่วยฐาน` : ''}
              </div>
            )}
          </div>
        )}
      </section>
    </>
  )
}

function TikTokMappingImpact({ impact }: { impact: MarketplaceAliasImpact }) {
  return (
    <section aria-labelledby="tiktok-mapping-impact" className="space-y-2">
      <h2 id="tiktok-mapping-impact" className="text-sm font-semibold">ผลกระทบก่อนบันทึก</h2>
      <div className="rounded-lg border border-border p-3 text-sm">
        <div className="flex items-center gap-2"><CheckCircle2 className="h-4 w-4 text-accentStrong" /><span className="font-medium">สูตรพร้อมใช้: {impact.after_formula}</span></div>
        <div className="mt-2 grid gap-1 text-xs text-muted-foreground sm:grid-cols-2">
          <span>Bill ที่ยังเปิดอยู่ {impact.open_bills.toLocaleString()} ใบ</span>
          <span>Reservation ที่เกี่ยวข้อง {impact.reservations.toLocaleString()} รายการ</span>
        </div>
      </div>
      {impact.dry_run_required && (
        <Alert className="border-warning/40 bg-warning/10">
          <AlertTriangle className="h-4 w-4 text-warning" />
          <AlertTitle>ต้องตรวจสต๊อก Shopee ใหม่</AlertTitle>
          <AlertDescription>SML item นี้เกี่ยวข้องกับร้าน Shopee {impact.affected_shop_ids.length.toLocaleString()} ร้าน หลังยืนยันระบบจะพักซิงก์อัตโนมัติของร้านที่ได้รับผลกระทบจนกว่าจะผ่าน dry-run ใหม่</AlertDescription>
        </Alert>
      )}
      {impact.conversion_status !== 'ready' && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>หน่วยยังไม่พร้อม</AlertTitle>
          <AlertDescription>Catalog ยังพิสูจน์ conversion ของหน่วยนี้ไม่ได้ กรุณาเลือกหน่วยอื่นหรืออัปเดต Catalog</AlertDescription>
        </Alert>
      )}
    </section>
  )
}

function apiErrorMessage(cause: unknown, fallback: string) {
  const data = (cause as { response?: { data?: { error?: { message?: string } | string } } })?.response?.data
  return typeof data?.error === 'string' ? data.error : data?.error?.message || fallback
}
