import { useEffect, useRef, useState } from 'react'
import axios from 'axios'
import { AlertCircle, AlertTriangle, Check, CheckCircle2, Edit, Info, Trash2, X } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { TableRow, TableCell } from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { UnitSelect } from '@/components/common/UnitSelect'
import { Checkbox } from '@/components/ui/checkbox'
import { money } from '@/lib/shopeeBill'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'
import { useAuthStore } from '@/store/auth'
import api from '@/api/client'
import type { BillItem, CatalogMatch, MarketplaceAliasImpact } from '@/types'
import { useMatchInfo } from '../hooks/useMatchInfo'
import { rowIssueReason } from '../utils/validation'
import { MapItemModal } from './MapItemModal'

export interface DiscountInfo {
  effectiveDiscount: number
  couponDiscount: number
  coinAmount: number
  grossTotal: number
}

interface Props {
  item: BillItem
  billId: string
  source: string
  sourceAccountKey: string
  editable: boolean
  onUpdated: (updated: BillItem) => void
  onDeleted: (itemId: string) => void
  onRefresh: () => Promise<unknown>
  // When true, briefly flash this row (1.5s) so the admin's eye lands on
  // it. Triggered by the BillTotal warning card's "ดู →" link.
  highlighted?: boolean
  rawNameLabel?: string
  showDiscountColumn?: boolean
  discountInfo?: DiscountInfo
  tableColumnCount?: number
}

function IssueBadge({ reason }: { reason: string }) {
  if (!reason) return null
  return (
    <span className="mt-2 inline-flex max-w-full items-center gap-1.5 rounded-md border border-warning/30 bg-warning/10 px-2 py-1 text-xs font-medium text-warning">
      <AlertCircle className="h-3.5 w-3.5 shrink-0" />
      <span className="break-words">{reason}</span>
    </span>
  )
}

export function BillItemRow({
  item,
  billId,
  source,
  sourceAccountKey,
  editable,
  onUpdated,
  onDeleted,
  onRefresh,
  highlighted,
  rawNameLabel = 'ชื่อสินค้าจากต้นทาง',
  showDiscountColumn = false,
  discountInfo,
  tableColumnCount = 9,
}: Props) {
  // When the parent flips `highlighted` true (admin clicked "ดู →" in the
  // BillTotal warning card) we scroll this row into view + add a brief tint
  // ring so the admin's eye lands on the right place. Self-clearing flag.
  const rowRef = useRef<HTMLTableRowElement>(null)
  const [flashing, setFlashing] = useState(false)
  useEffect(() => {
    if (!highlighted) return
    rowRef.current?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setFlashing(true)
    const t = setTimeout(() => setFlashing(false), 1500)
    return () => clearTimeout(t)
  }, [highlighted])

  // Per-row validation reason — concatenates each rule the row violates.
  // Empty string when the row is fine; the indicator cell stays empty.
  const issueReason = rowIssueReason(item)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [showMapModal, setShowMapModal] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [pickedMatch, setPickedMatch] = useState<CatalogMatch | null>(null)
  const [saveImpact, setSaveImpact] = useState<MarketplaceAliasImpact | null>(null)
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const canRememberMapping = Boolean(
    isAdmin &&
    ['shopee', 'lazada', 'tiktok'].includes(source) &&
    !(source === 'shopee' && sourceAccountKey === 'default' && !item.source_item_id),
  )
  const [rememberMapping, setRememberMapping] = useState(false)
  const [draft, setDraft] = useState({
    item_code: item.item_code ?? '',
    unit_code: item.unit_code ?? '',
    qty: String(item.qty ?? 0),
    price: String(item.price ?? 0),
    discount_amount: String(item.discount_amount ?? 0),
  })

  const reset = () => {
    setPickedMatch(null)
    setRememberMapping(false)
    setDraft({
      item_code: item.item_code ?? '',
      unit_code: item.unit_code ?? '',
      qty: String(item.qty ?? 0),
      price: String(item.price ?? 0),
      discount_amount: String(item.discount_amount ?? 0),
    })
  }

  const performSave = async (confirmedImpact: MarketplaceAliasImpact | null = null) => {
    setSaving(true)
    try {
      const nextQty = Number(draft.qty)
      const nextPrice = Number(draft.price)
      const nextDiscount = Number(draft.discount_amount)
      const amountChanged = nextQty !== item.qty || nextPrice !== (item.price ?? 0)
      await api.put(`/api/bills/${billId}/items/${item.id}`, {
        item_code: draft.item_code,
        unit_code: draft.unit_code,
        ...(nextQty !== item.qty ? { qty: nextQty } : {}),
        ...(nextPrice !== (item.price ?? 0) ? { price: nextPrice } : {}),
        ...(nextDiscount !== (item.discount_amount ?? 0) ? { discount_amount: nextDiscount } : {}),
        remember_mapping: canRememberMapping && rememberMapping,
        ...(canRememberMapping && rememberMapping && confirmedImpact ? {
          expected_master_mapping_revision: confirmedImpact.current_mapping_revision,
          remember_mapping_impact_digest: confirmedImpact.impact_digest,
        } : {}),
      })

      const prevCode = item.item_code ?? ''
      if (draft.item_code && draft.item_code !== prevCode && rememberMapping) {
        toast.success('บันทึก Product Master แล้ว ครั้งถัดไประบบจะจับคู่ให้อัตโนมัติ', {
          duration: 3500,
        })
      }

      onUpdated({
        ...item,
        item_code: draft.item_code,
        unit_code: draft.unit_code,
        qty: nextQty,
        price: nextPrice,
        gross_amount: amountChanged ? null : item.gross_amount,
        discount_amount: nextDiscount,
        mapped: draft.item_code !== '',
      })
      setEditing(false)
      setPickedMatch(null)
      setRememberMapping(false)
      notifyWorkQueueChanged()
    } catch (err) {
      console.error('update item failed', err)
      const message = axios.isAxiosError(err)
        ? String(err.response?.data?.error || err.message)
        : 'บันทึกสินค้าไม่สำเร็จ'
      toast.error('บันทึกสินค้าไม่สำเร็จ', { description: message })
    } finally {
      setSaving(false)
    }
  }

  const handleSave = async () => {
    if (!canRememberMapping || !rememberMapping) {
      await performSave()
      return
    }
    setSaving(true)
    try {
      const response = await api.post<MarketplaceAliasImpact>('/api/marketplace-aliases/impact-preview', {
        alias_id: item.marketplace_alias_id ?? '',
        source,
        account_key: sourceAccountKey,
        external_item_id: item.source_item_id ?? '',
        external_variant_id: item.source_variant_id ?? '',
        source_sku: item.source_sku ?? '',
        raw_name: item.raw_name,
        item_code: draft.item_code,
      })
      setSaveImpact(response.data)
    } catch {
      toast.error('ตรวจสอบผลกระทบของ Product Master ไม่สำเร็จ กรุณาลองใหม่')
    } finally {
      setSaving(false)
    }
  }

  const handleQuickConfirm = async () => {
    if (!item.item_code) return
    setConfirming(true)
    try {
      await api.put(`/api/bills/${billId}/items/${item.id}`, {
        item_code: item.item_code,
        unit_code: item.unit_code ?? undefined,
      })
      await onRefresh()
      notifyWorkQueueChanged()
      toast.success('ยืนยันการจับคู่สินค้าแล้ว', {
        description: 'บิลนี้จะพร้อมส่ง SML เมื่อทุกรายการยืนยันครบ',
      })
    } catch (err) {
      console.error('confirm item match failed', err)
      toast.error('ยืนยันสินค้าไม่สำเร็จ')
    } finally {
      setConfirming(false)
    }
  }

  const handleDelete = async () => {
    await api.delete(`/api/bills/${billId}/items/${item.id}`)
    onDeleted(item.id)
    notifyWorkQueueChanged()
  }

  const matchInfo = useMatchInfo(item)
  const needsConfirm = Boolean(item.item_code && item.mapped !== true)
  const shippingPlatform = item.source_sku === '__shopee_shipping__'
    ? 'Shopee'
    : item.source_sku === '__lazada_shipping__'
      ? 'Lazada'
      : item.source_sku === '__tiktok_shipping__'
        ? 'TikTok'
        : ''
  const isMarketplaceShippingLine = Boolean(shippingPlatform)
  const editMatchInfo =
    pickedMatch && pickedMatch.item_code === draft.item_code
      ? {
          itemName: pickedMatch.item_name,
        }
      : matchInfo
  const billPrice = item.price ?? 0
  const discountAmount = item.discount_amount ?? 0
  const grossAmount = item.gross_amount ?? (item.qty ?? 0) * billPrice
  const netAmount = Math.max(grossAmount - discountAmount, 0)
  const canExplainDiscount =
    Boolean(discountInfo) &&
    !isMarketplaceShippingLine &&
    discountAmount > 0 &&
    grossAmount > 0 &&
    (discountInfo?.grossTotal ?? 0) > 0
  const discountRate = discountInfo ? (discountInfo.effectiveDiscount / discountInfo.grossTotal) * 100 : 0

  if (!editing) {
    return (
      <>
        <TableRow
          ref={rowRef}
          className={cn(
            'transition-colors',
            flashing && 'bg-warning/15 ring-2 ring-warning/40',
          )}
        >
          <TableCell className="max-w-[360px] align-top">
            <div className="flex items-start gap-2">
            {item.source_image_url && (
              <div className="h-12 w-12 shrink-0 overflow-hidden rounded border border-border bg-muted">
                <img
                  src={item.source_image_url}
                  alt=""
                  className="h-full w-full object-cover"
                  loading="lazy"
                  referrerPolicy="no-referrer"
                />
              </div>
            )}
            <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2 break-words text-sm leading-6 text-foreground">
              <span>{item.raw_name}</span>
              {isMarketplaceShippingLine && (
                <span className="inline-flex rounded-md border border-info/30 bg-info/10 px-2 py-0.5 text-[11px] font-medium text-info">
                  ค่าส่งจาก {shippingPlatform}
                </span>
              )}
            </div>
            {item.source_sku && !isMarketplaceShippingLine && (
              <div className="mt-1 text-[11px] text-muted-foreground">
                SKU ต้นทาง: <code className="font-mono">{item.source_sku}</code>
                {!item.item_code && <span className="text-warning"> · ยังไม่พบในสินค้า SML</span>}
              </div>
            )}
            <IssueBadge reason={issueReason} />
            </div>
            </div>
          </TableCell>
          <TableCell>
            {item.item_code ? (
              <div className="space-y-1">
                <code className="font-mono text-xs text-foreground">{item.item_code}</code>
                {item.has_hidden_chars && (
                  <div
                    className="inline-flex max-w-full items-center gap-1 rounded-md border border-warning/30 bg-warning/10 px-2 py-0.5 text-[11px] font-medium text-warning"
                    title={`รหัสนี้มาจาก SML และมีอักขระมองไม่เห็น${item.clean_item_code ? ` ควรเป็น ${item.clean_item_code}` : ''}`}
                  >
                    <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">รหัสมีอักขระซ่อน</span>
                  </div>
                )}
                {needsConfirm && (
                  <div className="inline-flex rounded-md border border-warning/30 bg-warning/10 px-2 py-0.5 text-[11px] font-medium text-warning">
                    ต้องยืนยัน
                  </div>
                )}
              </div>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </TableCell>
          <TableCell className="max-w-[300px] break-words text-sm">
            <span className={matchInfo.itemName ? 'text-foreground' : 'text-muted-foreground'}>
              {matchInfo.itemName ?? '—'}
            </span>
          </TableCell>
          <TableCell className="text-right tabular-nums">{item.qty}</TableCell>
          <TableCell>{item.unit_code || '—'}</TableCell>
          <TableCell className="text-right tabular-nums font-medium">
            ฿{(item.price ?? 0).toLocaleString()}
          </TableCell>
          {showDiscountColumn && (
            <TableCell className="text-right tabular-nums">
              {discountAmount > 0 ? (
                <span className="inline-flex items-center justify-end gap-1 font-medium text-success">
                  -฿{discountAmount.toLocaleString()}
                  {canExplainDiscount && discountInfo && (
                    <TooltipProvider delayDuration={120}>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            className="inline-flex h-4 w-4 items-center justify-center rounded-full text-info hover:bg-info/10"
                            aria-label="ดูสูตรส่วนลดรายการนี้"
                          >
                            <Info className="h-3.5 w-3.5" />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent side="left" className="max-w-xs text-xs leading-5">
                          <div className="space-y-1">
                            <p className="font-medium text-popover-foreground">
                              ส่วนลดรวม {money(discountInfo.effectiveDiscount)}
                            </p>
                            <p>
                              = โค้ด {money(discountInfo.couponDiscount)} + Coin {money(discountInfo.coinAmount)}
                            </p>
                            <p>อัตรา = {discountRate.toFixed(3)}%</p>
                            <p>
                              ส่วนลด row = {money(discountInfo.effectiveDiscount)} × ({money(grossAmount)} / {money(discountInfo.grossTotal)})
                            </p>
                            <p>= {money(discountAmount)}</p>
                          </div>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                </span>
              ) : (
                <span className="text-muted-foreground">—</span>
              )}
            </TableCell>
          )}
          <TableCell className="text-right tabular-nums font-medium">
            ฿{netAmount.toLocaleString()}
          </TableCell>
          {editable && (
            <TableCell className="text-center whitespace-nowrap">
              {needsConfirm && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 px-2 text-success"
                  onClick={handleQuickConfirm}
                  disabled={confirming || !item.unit_code}
                  title={item.unit_code ? 'ยืนยันสินค้านี้โดยไม่ต้องเข้าโหมดแก้ไข' : 'ตั้งหน่วยก่อนยืนยันสินค้า'}
                >
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  {confirming ? 'ยืนยัน...' : 'ยืนยัน'}
                </Button>
              )}
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 px-2"
                onClick={() => {
                  reset()
                  setEditing(true)
                }}
              >
                <Edit className="h-3.5 w-3.5" />
                {item.item_code ? 'แก้ไข' : 'จับคู่'}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-destructive hover:text-destructive"
                onClick={() => setDeleteOpen(true)}
                title="ลบรายการ"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </TableCell>
          )}
        </TableRow>

      <ConfirmDialog
        open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title="ลบรายการสินค้า"
          description={`ยืนยันลบ "${item.raw_name.slice(0, 50)}${item.raw_name.length > 50 ? '...' : ''}" ?`}
          confirmLabel="ลบรายการ"
          variant="destructive"
        onConfirm={handleDelete}
      />
      <ConfirmDialog
        open={saveImpact !== null}
        onOpenChange={(open) => !open && setSaveImpact(null)}
        title="ยืนยันบันทึกเป็น Product Master?"
        description={saveImpact ? `สินค้า SML: ${draft.item_code}\nเอกสารเปิด: ${saveImpact.open_items.toLocaleString()} รายการ ใน ${saveImpact.open_bills.toLocaleString()} บิล\nStock mapping: ${saveImpact.stock_mappings.toLocaleString()} รายการ${saveImpact.stock_conflicts ? ` · พบ conflict ${saveImpact.stock_conflicts.toLocaleString()}` : ''}${saveImpact.dry_run_required ? '\nหลังบันทึกต้องทำ Dry-run ใหม่ก่อนซิงก์สต๊อก' : ''}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน` : undefined}
        confirmLabel="บันทึก Master"
        onConfirm={async () => { const impact = saveImpact; setSaveImpact(null); await performSave(impact) }}
      />
      </>
    )
  }

  // ── Edit mode ────────────────────────────────────────────────────────────────
  return (
    <>
      {showMapModal && (
        <MapItemModal
          open={showMapModal}
          rawName={item.raw_name}
          currentCode={draft.item_code}
          currentUnit={draft.unit_code}
          currentPrice={Number(draft.price) || 0}
          sourceImageUrl={item.source_image_url}
          rawNameLabel={rawNameLabel}
          onPick={(code, unit, picked) => {
            setDraft((d) => ({ ...d, item_code: code, unit_code: unit || '' }))
            setPickedMatch(picked ?? null)
            if (canRememberMapping) setRememberMapping(true)
          }}
          onClose={() => setShowMapModal(false)}
        />
      )}
      <TableRow className="bg-muted/20 hover:bg-muted/20">
        <TableCell colSpan={tableColumnCount} className="p-3">
          <div className="rounded-lg border border-border bg-card p-4 shadow-sm">
            <div className="grid gap-4 xl:grid-cols-[minmax(260px,1fr)_minmax(360px,1.15fr)_420px]">
              <div className="space-y-2">
                <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {rawNameLabel}
                </p>
                <p className="break-words text-sm font-medium leading-6 text-foreground">
                  {item.raw_name}
                </p>
                <IssueBadge reason={issueReason} />
              </div>

              <div className="space-y-3">
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">
                    สินค้าใน SML
                  </label>
                  <Button
                    type="button"
                    variant="outline"
                    className="h-10 w-full justify-start gap-2 px-3 text-left"
                    onClick={() => setShowMapModal(true)}
                    title="ค้นหาและเลือกสินค้าที่มีอยู่ใน SML"
                  >
                    <span className="font-mono text-xs">
                      {draft.item_code || 'เลือกสินค้า'}
                    </span>
                    {editMatchInfo.itemName && (
                      <span className="truncate text-sm font-normal text-muted-foreground">
                        {editMatchInfo.itemName}
                      </span>
                    )}
                  </Button>
                </div>
                {canRememberMapping ? (
                  <label className="flex items-start gap-2 text-xs text-muted-foreground">
                    <Checkbox checked={rememberMapping} onCheckedChange={(checked) => setRememberMapping(checked === true)} />
                    <span>บันทึกเป็น Product Master และใช้กับเอกสารเปิดรายการอื่น</span>
                  </label>
                ) : (
                  <p className="text-xs text-muted-foreground">แก้เฉพาะบิลนี้ ไม่สร้าง Product Master</p>
                )}
              </div>

              <div className={cn('grid gap-3', showDiscountColumn ? 'grid-cols-4' : 'grid-cols-3')}>
                <label className="space-y-1.5">
                  <span className="text-xs font-medium text-muted-foreground">จำนวน</span>
                  <Input
                    type="number"
                    step="any"
                    value={draft.qty}
                    onChange={(e) => setDraft((d) => ({ ...d, qty: e.target.value }))}
                    className="h-10 text-right"
                  />
                </label>
                <div className="space-y-1.5">
                  <span className="text-xs font-medium text-muted-foreground">หน่วย</span>
                  <UnitSelect
                    value={draft.unit_code}
                    productCode={draft.item_code}
                    onValueChange={(unit_code) => setDraft((d) => ({ ...d, unit_code }))}
                    disabled={!draft.item_code}
                    autoSelectSingle
                  />
                </div>
                <label className="space-y-1.5">
                  <span className="text-xs font-medium text-muted-foreground">ราคา</span>
                  <Input
                    type="number"
                    step="any"
                    value={draft.price}
                    onChange={(e) => setDraft((d) => ({ ...d, price: e.target.value }))}
                    className="h-10 text-right"
                  />
                </label>
                {showDiscountColumn && (
                  <div className="space-y-1.5">
                    <span className="text-xs font-medium text-muted-foreground">ส่วนลด</span>
                    <Input
                      type="number"
                      min="0"
                      step="0.01"
                      value={draft.discount_amount}
                      onChange={(e) => setDraft((d) => ({ ...d, discount_amount: e.target.value }))}
                      className="h-10 text-right"
                    />
                  </div>
                )}
                <div className={cn('flex items-center justify-between rounded-md bg-muted/50 px-3 py-2', showDiscountColumn ? 'col-span-4' : 'col-span-3')}>
                  <span className="text-xs font-medium text-muted-foreground">รวมรายการนี้</span>
                  <span className="tabular-nums text-sm font-semibold text-foreground">
                    ฿{Math.max(
                      (Number(draft.qty) === item.qty && Number(draft.price) === (item.price ?? 0)
                        ? item.gross_amount ?? Number(draft.qty || 0) * Number(draft.price || 0)
                        : Number(draft.qty || 0) * Number(draft.price || 0)) - Number(draft.discount_amount || 0),
                      0,
                    ).toLocaleString()}
                  </span>
                </div>
                <div className={cn('flex justify-end gap-2', showDiscountColumn ? 'col-span-4' : 'col-span-3')}>
                  <Button
                    type="button"
                    variant="ghost"
                    disabled={saving}
                    onClick={() => setEditing(false)}
                  >
                    <X className="h-4 w-4" />
                    ยกเลิก
                  </Button>
                  <Button
                    type="button"
                    disabled={saving}
                    onClick={handleSave}
                  >
                    <Check className="h-4 w-4" />
                    {saving ? 'กำลังบันทึก...' : 'บันทึก'}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </TableCell>
      </TableRow>
      <ConfirmDialog
        open={saveImpact !== null}
        onOpenChange={(open) => !open && setSaveImpact(null)}
        title="ยืนยันบันทึกเป็น Product Master?"
        description={saveImpact ? `สินค้า SML: ${draft.item_code}\nเอกสารเปิด: ${saveImpact.open_items.toLocaleString()} รายการ ใน ${saveImpact.open_bills.toLocaleString()} บิล\nStock mapping: ${saveImpact.stock_mappings.toLocaleString()} รายการ${saveImpact.stock_conflicts ? ` · พบ conflict ${saveImpact.stock_conflicts.toLocaleString()}` : ''}${saveImpact.dry_run_required ? '\nหลังบันทึกต้องทำ Dry-run ใหม่ก่อนซิงก์สต๊อก' : ''}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน` : undefined}
        confirmLabel="บันทึก Master"
        onConfirm={async () => { const impact = saveImpact; setSaveImpact(null); await performSave(impact) }}
      />
    </>
  )
}
