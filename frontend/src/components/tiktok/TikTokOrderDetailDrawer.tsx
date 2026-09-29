import { AlertTriangle, CheckCircle2, Clock, Copy, Eye, FilePlus2, Info, Loader2, PackageSearch, RefreshCw, Truck } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
  tiktokCancellationState,
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

export type TikTokTrackingEvent = {
  description: string
  update_time_millis: number
  action_code: number
}

type Props = {
  open: boolean
  order: TikTokOrderDetail | null
  createDocumentDisabledReason?: string
  tracking: TikTokTrackingEvent[]
  trackingLoading: boolean
  trackingError?: string
  onOpenChange: (open: boolean) => void
  onReviewBill: () => void
  onCopyOrder: () => void
  onRefreshTracking: () => void
}

export function TikTokOrderDetailDrawer({
  open,
  order,
  createDocumentDisabledReason = '',
  tracking,
  trackingLoading,
  trackingError = '',
  onOpenChange,
  onReviewBill,
  onCopyOrder,
  onRefreshTracking,
}: Props) {
  const isCancelled = order?.order_status === 'CANCELLED'
  const documentInput = order ? {
    billID: order.bill_id,
    billStatus: order.bill_status,
    smlDocNo: order.sml_doc_no,
    documentPath: order.document_path,
    cancellation: order.cancellation ? {
      status: order.cancellation.status,
      cancelSMLDocNo: order.cancellation.cancel_sml_doc_no,
      errorMessage: order.cancellation.error_message,
    } : undefined,
  } : null
  const cancellationDocument = isCancelled && documentInput ? tiktokCancellationState(documentInput) : null
  const document = order && documentInput
    ? cancellationDocument ?? tiktokCompactDocumentState(documentInput, order.auto_sml)
    : null
  const rawDocument = order && documentInput
    ? tiktokDocumentState(documentInput)
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
            <div className="space-y-3 px-4 py-3 sm:px-6">
              <OrderSummaryCard order={order} />

              <TikTokLifecycleTimeline order={order} />

              <DocumentMilestones
                hasDocument={Boolean(rawDocument.path)}
                sentToSML={order.bill_status === 'sent' && Boolean(order.sml_doc_no)}
                cancelled={isCancelled}
                cancellationStatus={cancellationDocument?.status}
                documentDetail={document.detail}
              />

              <TrackingCard
                tracking={tracking}
                loading={trackingLoading}
                error={trackingError}
                onRefresh={onRefreshTracking}
              />

              <Alert className="border-info/30 bg-info/10">
                <Info className="h-4 w-4" />
                <AlertTitle>จัดส่งและใบปะหน้า</AlertTitle>
                <AlertDescription>ทำใน TikTok Seller Center แล้ว Nexflow จะติดตามสถานะกลับมาใน timeline นี้</AlertDescription>
              </Alert>

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
          <Button type="button" variant="outline" className="gap-2" onClick={onRefreshTracking} disabled={trackingLoading || !order}>
            {trackingLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            ตรวจสถานะล่าสุด
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function OrderSummaryCard({ order }: { order: TikTokOrderDetail }) {
  return (
    <Card className="shadow-none">
      <CardContent className="p-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <div className="font-mono text-xs font-semibold text-foreground">{order.order_id}</div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <OrderStatusBadge status={order.order_status} />
              <Badge variant="outline" className="border-[#111817] bg-[#111817] text-white hover:bg-[#111817]">TikTok Shop</Badge>
            </div>
          </div>
          <div className="sm:text-right">
            <div className="font-semibold tabular-nums">{formatTikTokMoney(order.payment_total_amount, order.currency)}</div>
            <div className="text-xs text-muted-foreground">{order.item_count.toLocaleString()} รายการ · {order.sku_count.toLocaleString()} SKU</div>
          </div>
        </div>
        <div className="mt-3 grid gap-2 sm:grid-cols-2">
          <SummaryLine label="ร้าน" value={order.shop_name || order.shop_id} />
          <SummaryLine label="สถานะล่าสุดจาก TikTok" value={formatDateTime(order.last_order_update_at)} />
          <SummaryLine label="ยอดสินค้า" value={formatTikTokMoney(order.product_subtotal_amount, order.currency)} />
          <SummaryLine label="ค่าจัดส่ง" value={formatTikTokMoney(order.shipping_fee_amount, order.currency)} />
          <SummaryLine label="SML" value={order.sml_doc_no || 'ยังไม่ส่ง SML'} mono />
          <SummaryLine label="ซิงก์เข้า Nexflow ล่าสุด" value={formatDateTime(order.last_synced_at)} />
        </div>
      </CardContent>
    </Card>
  )
}

function TikTokLifecycleTimeline({ order }: { order: TikTokOrderDetail }) {
  const steps = lifecycleSteps(order)
  return (
    <Card className="shadow-none">
      <CardHeader className="flex-row items-start justify-between gap-3 p-3 pb-0">
        <div>
          <CardTitle className="text-sm font-semibold tracking-normal">Timeline สถานะคำสั่งซื้อ</CardTitle>
          <p className="mt-0.5 text-xs text-muted-foreground">TikTok snapshot เป็นสถานะจริงปัจจุบัน; ขั้นก่อนหน้าเป็นการอนุมานจากสถานะปัจจุบัน</p>
        </div>
        <OrderStatusBadge status={order.order_status} />
      </CardHeader>
      <CardContent className="p-3">
        <ol className="space-y-0">
          {steps.map((step, index) => <LifecycleRow key={step.key} step={step} index={index} isLast={index === steps.length - 1} />)}
        </ol>
      </CardContent>
    </Card>
  )
}

function lifecycleSteps(order: TikTokOrderDetail) {
  if (order.order_status === 'CANCELLED') return [{ key: 'cancelled', label: 'ยกเลิกแล้ว', state: 'current' as const, detail: 'TikTok Shop แจ้งสถานะยกเลิก', occurredAt: order.last_order_update_at, tone: 'danger' as const }]
  const steps = [
    ['AWAITING_SHIPMENT', 'รอจัดส่ง'],
    ['AWAITING_COLLECTION', 'รอรับพัสดุ'],
    ['IN_TRANSIT', 'กำลังขนส่ง'],
    ['DELIVERED', 'นำส่งสำเร็จ'],
    ['COMPLETED', 'สำเร็จ'],
  ] as const
  const aliases: Record<string, number> = { PARTIALLY_SHIPPING: 2, ON_HOLD: 0, UNPAID: -1 }
  const currentIndex = aliases[order.order_status] ?? steps.findIndex(([status]) => status === order.order_status)
  if (currentIndex < 0) return [{ key: 'current', label: tiktokOrderStatusLabel(order.order_status), state: 'current' as const, detail: 'สถานะล่าสุดจาก TikTok Shop', occurredAt: order.last_order_update_at, tone: 'muted' as const }]
  return steps.slice(0, Math.max(currentIndex + 1, 1)).map(([status, label], index) => ({
    key: status,
    label,
    state: index === currentIndex ? 'current' as const : 'done' as const,
    detail: index === currentIndex ? 'สถานะล่าสุดจาก TikTok Shop' : 'สถานะปัจจุบันยืนยันว่าเลยขั้นนี้แล้ว',
    occurredAt: index === currentIndex ? order.last_order_update_at : undefined,
    tone: 'normal' as const,
  }))
}

function LifecycleRow({ step, index, isLast }: { step: ReturnType<typeof lifecycleSteps>[number]; index: number; isLast: boolean }) {
  const done = step.state === 'done'
  const current = step.state === 'current'
  const danger = step.tone === 'danger'
  return <li className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
    <div className="relative flex justify-center">
      {!isLast && <span className="absolute top-8 h-[calc(100%-1.5rem)] w-px bg-border" />}
      <span className={cn('relative z-10 flex h-7 w-7 items-center justify-center rounded-full border text-xs font-semibold', danger ? 'border-destructive/40 bg-destructive/10 text-destructive' : done ? 'border-accentStrong/40 bg-primary/10 text-accentStrong' : 'border-info/30 bg-info/10 text-info')}>
        {done ? <CheckCircle2 className="h-4 w-4" /> : index + 1}
      </span>
    </div>
    <div className={cn('mb-2 rounded-md border px-3 py-2.5', danger ? 'border-destructive/40 bg-destructive/10' : current ? 'border-info/30 bg-info/10' : 'border-border bg-muted/20')}>
      <div className="flex flex-wrap items-center gap-2"><span className="font-medium text-foreground">{step.label}</span>{current && <Badge className={cn('h-5 px-1.5 text-[10px]', danger ? 'bg-destructive text-destructive-foreground' : 'bg-info text-info-foreground')}>ตอนนี้</Badge>}</div>
      <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground"><Clock className="h-3.5 w-3.5" />{step.occurredAt ? formatDateTime(step.occurredAt) : 'เวลาไม่ได้รับจาก TikTok'}</div>
      <div className="mt-1 text-xs text-muted-foreground">{step.detail}</div>
    </div>
  </li>
}

function DocumentMilestones({ hasDocument, sentToSML, cancelled, cancellationStatus, documentDetail }: { hasDocument: boolean; sentToSML: boolean; cancelled: boolean; cancellationStatus?: string; documentDetail: string }) {
  const items = [
    { label: 'เอกสาร Nexflow', detail: hasDocument ? documentDetail : cancelled ? 'ไม่ต้องสร้างใบขาย' : 'รอตรวจและยืนยันสร้าง', state: hasDocument ? 'done' : 'pending' },
    { label: 'ส่งเข้า SML', detail: sentToSML ? 'ส่ง SML แล้ว' : cancelled ? 'ไม่ต้องส่ง — ออเดอร์ยกเลิก' : hasDocument ? 'รอส่งตามการตั้งค่าร้าน หรือส่งด้วยมือจากหน้าเอกสาร' : 'ทำหลังสร้างเอกสาร', state: sentToSML ? 'done' : 'pending' },
    ...(cancelled ? [{ label: 'เอกสารหลังยกเลิก', detail: cancellationStatus === 'completed' ? 'สร้างแล้ว' : cancellationStatus === 'not_required' ? 'ไม่ต้องสร้าง' : 'ตรวจจากคิวยกเลิก', state: cancellationStatus === 'completed' ? 'done' : 'pending' }] : []),
  ]
  return <Card className="shadow-none"><CardHeader className="p-3 pb-0"><CardTitle className="text-sm font-semibold tracking-normal">เอกสารใน Nexflow</CardTitle></CardHeader><CardContent className="grid gap-2 p-3 sm:grid-cols-2">{items.map((item) => <div key={item.label} className={cn('rounded-md border px-3 py-2 text-sm', item.state === 'done' ? 'border-accentStrong/30 bg-primary/10' : 'border-border bg-muted/20')}><div className="flex items-center gap-2">{item.state === 'done' ? <CheckCircle2 className="h-4 w-4 text-accentStrong" /> : <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/50" />}<span className="font-medium text-foreground">{item.label}</span></div><p className="mt-1 text-xs text-muted-foreground">{item.detail}</p></div>)}</CardContent></Card>
}

function TrackingCard({ tracking, loading, error, onRefresh }: { tracking: TikTokTrackingEvent[]; loading: boolean; error: string; onRefresh: () => void }) {
  return <Card className="shadow-none"><CardHeader className="flex-row items-start justify-between gap-3 p-3 pb-0"><div><CardTitle className="text-sm font-semibold tracking-normal">ข้อมูลติดตามพัสดุจาก TikTok Shop</CardTitle><p className="mt-0.5 text-xs text-muted-foreground">อ่านข้อมูลจริงเมื่อกดตรวจสถานะล่าสุดเท่านั้น</p></div><Truck className="h-4 w-4 text-muted-foreground" /></CardHeader><CardContent className="space-y-2 p-3">{loading ? <div className="flex items-center gap-2 rounded-md bg-muted/30 px-3 py-3 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังตรวจสถานะจาก TikTok Shop…</div> : error ? <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>ตรวจสถานะขนส่งไม่สำเร็จ</AlertTitle><AlertDescription>{error}</AlertDescription></Alert> : tracking.length ? <ol className="space-y-2">{tracking.map((event, index) => <li key={`${event.update_time_millis}-${index}`} className="rounded-md border border-border bg-muted/20 px-3 py-2"><p className="break-words text-sm font-medium text-foreground">{event.description || 'TikTok Shop อัปเดตสถานะขนส่ง'}</p><p className="mt-1 text-xs text-muted-foreground">{formatMillis(event.update_time_millis)}</p></li>)}</ol> : <div className="rounded-md border border-dashed border-border bg-muted/20 px-3 py-3 text-sm text-muted-foreground">ยังไม่ได้ตรวจข้อมูล Tracking จาก TikTok Shop กดตรวจสถานะล่าสุดเพื่ออ่านข้อมูลจริง</div>}<div className="flex justify-end"><Button type="button" variant="outline" size="sm" className="h-8 gap-2" onClick={onRefresh} disabled={loading}>{loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}ตรวจสถานะล่าสุด</Button></div></CardContent></Card>
}

function SummaryLine({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) { return <div className="min-w-0"><p className="text-xs text-muted-foreground">{label}</p><p className={cn('mt-0.5 break-words font-medium text-foreground', mono && 'font-mono text-xs')}>{value || '-'}</p></div> }

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

function formatDateTime(value?: string) {
  if (!value) return 'ไม่ทราบเวลา'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? 'ไม่ทราบเวลา'
    : date.toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}

function formatMillis(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 'TikTok Shop ไม่ได้ระบุเวลา'
  return new Date(value).toLocaleString('th-TH-u-ca-gregory', { timeZone: 'Asia/Bangkok', dateStyle: 'medium', timeStyle: 'short' })
}
