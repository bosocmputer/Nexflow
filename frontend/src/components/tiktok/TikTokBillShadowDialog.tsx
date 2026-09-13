import { AlertTriangle, CheckCircle2, FileSearch, Loader2, ShieldCheck } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  formatTikTokMoney,
  tiktokBillShadowMappingLabel,
  tiktokBillShadowReadinessLabel,
  tiktokBillShadowRouteLabel,
  tiktokOrderStatusLabel,
} from '@/lib/tiktok-shop-operations'
import { cn } from '@/lib/utils'

export interface TikTokBillShadowItem {
  product_id: string
  sku_id: string
  seller_sku?: string
  product_name: string
  variant_name: string
  quantity: number
  unit_sale_price: string
  line_total: string
  mapping: {
    status: string
    item_code?: string
    unit_code?: string
    marketplace_quantity: string
    sml_quantity?: string
    base_quantity?: string
  }
}

export interface TikTokBillShadowPreview {
  shadow_mode: boolean
  can_create_bill: boolean
  ready_for_reviewed_bill: boolean
  shop_id: string
  shop_name: string
  order_id: string
  order_status: string
  currency: string
  last_synced_at: string
  amounts: {
    product_subtotal: string
    shipping: string
    proposed_document_total: string
    buyer_payment: string
    excluded_buyer_platform_charges: string
    item_insurance: string
    grouped_line_total: string
  }
  route: {
    ready: boolean
    semantic_route?: string
    doc_format_code?: string
    shipping_ready: boolean
  }
  items: TikTokBillShadowItem[]
  blockers: Array<{
    code: string
    message: string
    product_id?: string
    sku_id?: string
  }>
  existing_bill?: {
    id: string
    status: string
    sml_doc_no?: string
    source_account_key: string
  }
}

interface Props {
  open: boolean
  orderID: string
  shopName: string
  loading: boolean
  error: string
  preview: TikTokBillShadowPreview | null
  canManage: boolean
  onMapItem: (item: TikTokBillShadowItem) => void
  onOpenChange: (open: boolean) => void
}

export function TikTokBillShadowDialog({ open, orderID, shopName, loading, error, preview, canManage, onMapItem, onOpenChange }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[92dvh] max-w-4xl grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0">
        <DialogHeader className="border-b border-border px-4 py-4 pr-12 sm:px-6">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle>ตรวจตัวอย่าง Bill TikTok Shop</DialogTitle>
            <Badge variant="outline" className="border-info/30 bg-info/10 text-info">Shadow</Badge>
          </div>
          <DialogDescription>
            Order <span className="font-mono text-foreground">{orderID}</span>{shopName ? ` · ${shopName}` : ''}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 overflow-y-auto px-4 py-4 sm:px-6">
          {loading && (
            <div className="space-y-3" role="status" aria-label="กำลังตรวจตัวอย่าง Bill">
              <div className="h-16 animate-pulse rounded-lg bg-muted" />
              <div className="h-28 animate-pulse rounded-lg bg-muted" />
              <div className="h-36 animate-pulse rounded-lg bg-muted" />
            </div>
          )}

          {!loading && error && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ตรวจตัวอย่างไม่สำเร็จ</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {!loading && preview && (
            <div className="space-y-4">
              <Alert className={cn(
                preview.ready_for_reviewed_bill ? 'border-accentStrong/30 bg-primary/10' : 'border-warning/30 bg-warning/10',
              )}>
                {preview.ready_for_reviewed_bill
                  ? <CheckCircle2 className="h-4 w-4 text-accentStrong" />
                  : <AlertTriangle className="h-4 w-4 text-warning" />}
                <AlertTitle>{tiktokBillShadowReadinessLabel(preview.ready_for_reviewed_bill, preview.blockers.length)}</AlertTitle>
                <AlertDescription>
                  หน้านี้ใช้ตรวจข้อมูลเท่านั้น ยังไม่สร้าง Bill ไม่ส่ง SML และไม่เปลี่ยนข้อมูลสินค้า
                </AlertDescription>
              </Alert>

              <section aria-labelledby="tiktok-shadow-amounts">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                  <h2 id="tiktok-shadow-amounts" className="text-sm font-semibold">ยอดสำหรับเอกสาร</h2>
                  <Badge variant="outline">{tiktokOrderStatusLabel(preview.order_status)}</Badge>
                </div>
                <dl className="grid overflow-hidden rounded-lg border border-border bg-card sm:grid-cols-2 lg:grid-cols-4">
                  <AmountCell label="ยอดสินค้า" value={formatTikTokMoney(preview.amounts.product_subtotal, preview.currency)} />
                  <AmountCell label="ค่าจัดส่งเข้าเอกสาร" value={formatTikTokMoney(preview.amounts.shipping, preview.currency)} />
                  <AmountCell label="ยอด Bill ที่เสนอ" value={formatTikTokMoney(preview.amounts.proposed_document_total, preview.currency)} emphasized />
                  <AmountCell label="ผู้ซื้อชำระ" value={formatTikTokMoney(preview.amounts.buyer_payment, preview.currency)} />
                </dl>
                {Number(preview.amounts.excluded_buyer_platform_charges) > 0 && (
                  <p className="mt-2 text-xs leading-5 text-muted-foreground">
                    ไม่รวมใน Bill: ค่าคุ้มครองสินค้า {formatTikTokMoney(preview.amounts.item_insurance, preview.currency)} ซึ่งเป็นยอดผู้ซื้อ/แพลตฟอร์ม
                  </p>
                )}
              </section>

              <section aria-labelledby="tiktok-shadow-items">
                <h2 id="tiktok-shadow-items" className="mb-2 text-sm font-semibold">สินค้าและ Product Master</h2>
                <div className="divide-y divide-border overflow-hidden rounded-lg border border-border">
                  {preview.items.map((item) => (
                    <div key={`${item.product_id}:${item.sku_id}`} className="p-3">
                      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                        <div className="min-w-0">
                          <div className="break-words text-sm font-medium">{item.product_name}</div>
                          <div className="mt-0.5 break-words text-xs text-muted-foreground">
                            {item.variant_name || 'ไม่มีตัวเลือก'}{item.seller_sku ? ` · SKU ${item.seller_sku}` : ''}
                          </div>
                          <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                            Product {item.product_id} · SKU {item.sku_id}
                          </div>
                        </div>
                        <div className="flex shrink-0 flex-wrap items-center gap-2 lg:justify-end">
                          <Badge variant="outline" className={mappingBadgeClass(item.mapping.status)}>
                            {tiktokBillShadowMappingLabel(item.mapping.status)}
                          </Badge>
                          <span className="text-sm tabular-nums">
                            {item.quantity.toLocaleString()} ชิ้น · {formatTikTokMoney(item.line_total, preview.currency)}
                          </span>
                        </div>
                      </div>
                      {item.mapping.status === 'ready' && (
                        <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 rounded-md bg-muted/60 px-3 py-2 text-xs">
                          <span>SML <strong className="font-mono font-medium">{item.mapping.item_code}</strong></span>
                          <span>หน่วย <strong className="font-medium">{item.mapping.unit_code}</strong></span>
                          <span>จำนวนเข้า SML <strong className="font-medium tabular-nums">{item.mapping.sml_quantity}</strong></span>
                        </div>
                      )}
                      {item.mapping.status !== 'ready' && (
                        <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
                          <span className="text-xs text-muted-foreground">
                            {canManage ? 'เลือกสินค้าและหน่วย SML ที่ใช้กับ SKU นี้' : 'ให้ผู้ดูแลระบบยืนยัน Product Master ของ SKU นี้'}
                          </span>
                          {canManage && (
                            <Button type="button" size="sm" onClick={() => onMapItem(item)}>
                              {item.mapping.status === 'missing' ? 'จับคู่สินค้า SML' : 'ตรวจและแก้การจับคู่'}
                            </Button>
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </section>

              {preview.blockers.length > 0 && (
                <section aria-labelledby="tiktok-shadow-blockers">
                  <h2 id="tiktok-shadow-blockers" className="mb-2 text-sm font-semibold">สิ่งที่ต้องแก้ก่อน</h2>
                  <ul className="space-y-2">
                    {preview.blockers.map((blocker, index) => (
                      <li key={`${blocker.code}:${blocker.product_id ?? ''}:${blocker.sku_id ?? ''}:${index}`} className="flex gap-2 rounded-md bg-warning/10 px-3 py-2 text-sm">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
                        <span>{blocker.message}</span>
                      </li>
                    ))}
                  </ul>
                </section>
              )}

              <div className="flex flex-col gap-2 border-t border-border pt-3 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
                <span className="inline-flex items-center gap-1.5">
                  <ShieldCheck className="h-4 w-4" aria-hidden="true" />
                  เส้นทางเอกสาร: {tiktokBillShadowRouteLabel(preview.route.semantic_route ?? '')}{preview.route.doc_format_code ? ` · ${preview.route.doc_format_code}` : ''}
                </span>
                <span>Snapshot ล่าสุด {formatPreviewTime(preview.last_synced_at)}</span>
              </div>
            </div>
          )}
        </div>

        <DialogFooter className="border-t border-border bg-muted/30 px-4 py-3 sm:px-6">
          <DialogClose asChild>
            <Button type="button" variant="outline">ปิดตัวอย่าง</Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function TikTokBillShadowButton({ loading, onClick }: { loading: boolean; onClick: () => void }) {
  return (
    <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" disabled={loading} onClick={onClick}>
      {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FileSearch className="h-3.5 w-3.5" />}
      ตรวจตัวอย่าง Bill
    </Button>
  )
}

function AmountCell({ label, value, emphasized = false }: { label: string; value: string; emphasized?: boolean }) {
  return (
    <div className="border-b border-border px-3 py-2 last:border-b-0 sm:[&:nth-child(odd)]:border-r lg:border-b-0 lg:border-r lg:last:border-r-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn('mt-0.5 text-sm tabular-nums', emphasized && 'font-semibold text-foreground')}>{value}</dd>
    </div>
  )
}

function mappingBadgeClass(status: string) {
  if (status === 'ready') return 'border-accentStrong/30 bg-primary/10 text-accentStrong'
  if (status === 'legacy_unscoped') return 'border-info/30 bg-info/10 text-info'
  return 'border-warning/30 bg-warning/10 text-warning'
}

function formatPreviewTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? 'ไม่ทราบเวลา'
    : date.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}
