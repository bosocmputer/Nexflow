import { AlertTriangle, CheckCircle2, Copy, Eye, FilePlus2, PackageSearch } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  formatTikTokMoney,
  tiktokCompactDocumentState,
  tiktokDocumentState,
  tiktokOrderStatusLabel,
} from '@/lib/tiktok-shop-operations'
import { cn } from '@/lib/utils'

export type TikTokOrderDetail = {
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
  bill_id?: string
  bill_status?: string
  sml_doc_no?: string
  document_path?: string
  auto_sml?: { status: string; error_message?: string; updated_at: string }
  cancellation?: { status: string; cancel_sml_doc_no?: string; error_message?: string; updated_at: string }
}

type Props = {
  open: boolean
  order: TikTokOrderDetail | null
  canCreateDocument: boolean
  createDocumentDisabledReason?: string
  onOpenChange: (open: boolean) => void
  onReviewBill: () => void
  onCopyOrder: () => void
}

export function TikTokOrderDetailDrawer({
  open,
  order,
  canCreateDocument,
  createDocumentDisabledReason = '',
  onOpenChange,
  onReviewBill,
  onCopyOrder,
}: Props) {
  const document = order
    ? tiktokCompactDocumentState({
      billID: order.bill_id,
      billStatus: order.bill_status,
      smlDocNo: order.sml_doc_no,
      documentPath: order.document_path,
    }, order.auto_sml)
    : null
  const rawDocument = order
    ? tiktokDocumentState({
      billID: order.bill_id,
      billStatus: order.bill_status,
      smlDocNo: order.sml_doc_no,
      documentPath: order.document_path,
    })
    : null

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl lg:max-w-2xl">
        <SheetHeader className="border-b border-border px-4 py-3 text-left sm:px-6">
          <div className="flex flex-wrap items-center gap-2">
            <SheetTitle>Timeline คำสั่งซื้อ</SheetTitle>
            <Badge className="border-[#111817] bg-[#111817] text-white hover:bg-[#111817]">TikTok Shop</Badge>
          </div>
          <SheetDescription>สถานะคำสั่งซื้อจาก TikTok Shop และ milestone เอกสารใน Nexflow</SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {order && document && rawDocument && (
            <div className="space-y-5 px-4 py-4 sm:px-6">
              <section aria-labelledby="tiktok-order-summary">
                <h2 id="tiktok-order-summary" className="mb-2 text-sm font-semibold">สรุปคำสั่งซื้อ</h2>
                <dl className="grid overflow-hidden rounded-lg border border-border sm:grid-cols-2">
                  <DetailValue label="สถานะ TikTok Shop" value={<OrderStatusBadge status={order.order_status} />} />
                  <DetailValue label="ยอดที่ผู้ซื้อชำระ" value={formatTikTokMoney(order.payment_total_amount, order.currency)} emphasized />
                  <DetailValue label="ยอดสินค้า" value={formatTikTokMoney(order.product_subtotal_amount, order.currency)} />
                  <DetailValue label="ค่าจัดส่ง" value={formatTikTokMoney(order.shipping_fee_amount, order.currency)} />
                  <DetailValue label="จำนวนรายการ" value={`${order.item_count.toLocaleString()} รายการ · ${order.sku_count.toLocaleString()} SKU`} />
                  <DetailValue label="สถานะล่าสุดจาก TikTok" value={formatDateTime(order.last_order_update_at)} />
                  <DetailValue label="ซิงก์เข้า Nexflow ล่าสุด" value={formatDateTime(order.last_synced_at)} />
                  <DetailValue label="ค่าคุ้มครองสินค้า" value={formatTikTokMoney(order.item_insurance_fee_amount, order.currency)} />
                </dl>
                {Number(order.item_insurance_fee_amount) > 0 && (
                  <p className="mt-2 text-xs leading-5 text-muted-foreground">
                    ค่าคุ้มครองสินค้าเป็นยอดผู้ซื้อหรือแพลตฟอร์ม จึงไม่ถูกเพิ่มเป็นรายการขายหรือค่าจัดส่งใน SML
                  </p>
                )}
              </section>

              <section aria-labelledby="tiktok-order-document">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                  <h2 id="tiktok-order-document" className="text-sm font-semibold">เอกสาร Nexflow และ SML</h2>
                  <Badge variant="outline" className={documentToneClass(document.tone)}>{document.label}</Badge>
                </div>
                <div className="rounded-lg border border-border bg-muted/20 p-3">
                  <p className="text-sm font-medium text-foreground">{document.detail}</p>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">
                    {rawDocument.path
                      ? 'เปิดเอกสารเพื่อตรวจข้อมูล แล้วกดส่ง SML ทีละใบจากหน้าเอกสารเดียวกับ Shopee'
                      : canCreateDocument
                        ? 'ตรวจสินค้า การจับคู่ และยอดก่อนยืนยันสร้างเอกสารใน Nexflow'
                        : 'ผู้ที่มีสิทธิ์สร้างเอกสารสามารถตรวจข้อมูลและสร้าง Bill ได้'}
                  </p>
                </div>
              </section>

              <ManualSMLFlow hasDocument={Boolean(rawDocument.path)} sentToSML={Boolean(order.sml_doc_no)} />

              {order.cancellation && (
                <Alert className={cn(
                  'border-warning/30 bg-warning/10',
                  ['created', 'already_exists'].includes(order.cancellation.status) && 'border-accentStrong/30 bg-primary/10',
                )}>
                  {['created', 'already_exists'].includes(order.cancellation.status) ? <PackageSearch className="h-4 w-4 text-accentStrong" /> : <AlertTriangle className="h-4 w-4 text-warning" />}
                  <AlertTitle>เอกสารหลังยกเลิก</AlertTitle>
                  <AlertDescription>
                    {order.cancellation.cancel_sml_doc_no
                      ? `สร้างเอกสาร SML แล้ว: ${order.cancellation.cancel_sml_doc_no}`
                      : order.cancellation.error_message || 'มีสถานะเอกสารหลังยกเลิก โปรดตรวจคิวยกเลิก TikTok Shop'}
                  </AlertDescription>
                </Alert>
              )}
            </div>
          )}
        </ScrollArea>

        <div className="flex flex-col gap-2 border-t border-border p-3 sm:flex-row sm:justify-between">
          <div className="flex flex-col gap-2 sm:flex-row">
            {rawDocument?.path ? (
              <Button asChild variant="outline" className="gap-2">
                <Link to={rawDocument.path}>
                  <Eye className="h-4 w-4" />
                  เปิดเอกสาร
                </Link>
              </Button>
            ) : order ? (
              <Button
                type="button"
                variant="outline"
                className="gap-2"
                disabled={Boolean(createDocumentDisabledReason)}
                title={createDocumentDisabledReason || undefined}
                onClick={onReviewBill}
              >
                <FilePlus2 className="h-4 w-4" />
                สร้างเอกสาร
              </Button>
            ) : null}
            {order && (
              <Button type="button" variant="outline" className="gap-2" onClick={onCopyOrder}>
                <Copy className="h-4 w-4" />
                คัดลอก Order ID
              </Button>
            )}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ManualSMLFlow({ hasDocument, sentToSML }: { hasDocument: boolean; sentToSML: boolean }) {
  const steps = [
    {
      label: 'ตรวจคำสั่งซื้อ',
      detail: 'ตรวจยอด สินค้า และสถานะ TikTok Shop',
      complete: true,
    },
    {
      label: 'สร้างเอกสาร Nexflow',
      detail: hasDocument ? 'สร้างเอกสารแล้ว' : 'รอตรวจและยืนยันสร้าง',
      complete: hasDocument,
    },
    {
      label: 'ส่งเข้า SML',
      detail: sentToSML ? 'ส่ง SML แล้ว' : hasDocument ? 'เปิดเอกสารเพื่อส่งทีละใบ' : 'ทำหลังสร้างเอกสาร',
      complete: sentToSML,
    },
  ]

  return (
    <section aria-labelledby="tiktok-manual-sml-flow">
      <h2 id="tiktok-manual-sml-flow" className="mb-2 text-sm font-semibold">Timeline เอกสาร Nexflow และ SML</h2>
      <ol className="overflow-hidden rounded-lg border border-border bg-card sm:grid sm:grid-cols-3 sm:divide-x sm:divide-border">
        {steps.map((step, index) => (
          <li key={step.label} className="flex min-w-0 gap-2.5 border-b border-border px-3 py-3 last:border-b-0 sm:border-b-0">
            <span className={cn(
              'flex h-5 w-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold tabular-nums',
              step.complete ? 'border-accentStrong/40 bg-primary/10 text-accentStrong' : 'border-border bg-muted/40 text-muted-foreground',
            )}>
              {step.complete ? <CheckCircle2 className="h-3.5 w-3.5" aria-hidden="true" /> : index + 1}
            </span>
            <span className="min-w-0">
              <span className="block text-xs font-medium text-foreground">{step.label}</span>
              <span className="mt-0.5 block text-[11px] leading-4 text-muted-foreground">{step.detail}</span>
            </span>
          </li>
        ))}
      </ol>
    </section>
  )
}

function DetailValue({ label, value, emphasized = false }: { label: string; value: React.ReactNode; emphasized?: boolean }) {
  return (
    <div className="border-b border-border px-3 py-2.5 last:border-b-0 sm:[&:nth-child(odd)]:border-r sm:[&:nth-last-child(-n+2)]:border-b-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn('mt-1 min-w-0 text-sm', emphasized && 'font-semibold tabular-nums text-foreground')}>{value}</dd>
    </div>
  )
}

function OrderStatusBadge({ status }: { status: string }) {
  const className = status === 'CANCELLED'
    ? 'border-destructive/30 bg-destructive/10 text-destructive'
    : ['AWAITING_SHIPMENT', 'ON_HOLD'].includes(status)
      ? 'border-warning/40 bg-warning/10 text-warning'
      : ['PARTIALLY_SHIPPING', 'AWAITING_COLLECTION', 'IN_TRANSIT', 'DELIVERED'].includes(status)
        ? 'border-info/30 bg-info/10 text-info'
        : status === 'COMPLETED'
          ? 'border-accentStrong/40 bg-primary/10 text-accentStrong'
          : 'border-border bg-muted/40 text-muted-foreground'
  return <Badge variant="outline" className={className}>{tiktokOrderStatusLabel(status)}</Badge>
}

function documentToneClass(tone: 'muted' | 'warning' | 'danger' | 'success') {
  if (tone === 'success') return 'border-accentStrong/40 bg-primary/10 text-accentStrong'
  if (tone === 'danger') return 'border-destructive/30 bg-destructive/10 text-destructive'
  if (tone === 'warning') return 'border-warning/40 bg-warning/10 text-warning'
  return 'border-border bg-muted/40 text-muted-foreground'
}

function formatDateTime(value?: string) {
  if (!value) return 'ไม่ทราบเวลา'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? 'ไม่ทราบเวลา'
    : date.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}
