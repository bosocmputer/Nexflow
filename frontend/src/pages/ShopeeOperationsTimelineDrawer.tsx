import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Clock,
  Copy,
  Eye,
  FilePlus2,
  Info,
  Loader2,
  RefreshCw,
} from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

export type ShopeeTimelineOrder = {
  id: string
  shop_id: number
  shop_label: string
  order_sn: string
  order_status: string
  erp_status: string
  bill_id?: string
  sml_doc_no?: string
  document_route?: string
  buyer_username?: string
  total_amount: number
  currency?: string
  item_count: number
  package_number?: string
  logistics_status?: string
  tracking_number?: string
  shipping_carrier?: string
  checkout_shipping_carrier?: string
  last_update_source?: string
  last_order_update_at?: string
  last_synced_at: string
}

export type ShopeeTimelineStep = {
  key: string
  status: string
  label: string
  detail?: string
  state: 'done' | 'current' | 'upcoming' | 'skipped' | string
  source: 'push' | 'sync' | 'snapshot' | 'nexflow' | 'shipping' | string
  confidence: 'confirmed' | 'inferred' | 'missing' | string
  occurred_at?: string
  current?: boolean
  terminal?: boolean
}

export type ShopeeERPMilestone = {
  key: string
  label: string
  detail?: string
  state: 'done' | 'current' | 'upcoming' | 'failed' | string
  source: string
  confidence: 'confirmed' | 'inferred' | 'missing' | string
  occurred_at?: string
}

export type ShopeeTimelineEvent = {
  id: string
  source: string
  kind: string
  title: string
  detail?: string
  status?: string
  created_at: string
}

type OrderTimelineDrawerProps = {
  open: boolean
  order: ShopeeTimelineOrder | null
  steps: ShopeeTimelineStep[]
  milestones: ShopeeERPMilestone[]
  events: ShopeeTimelineEvent[]
  loading: boolean
  refreshing: boolean
  error: string
  canCreateDocument: boolean
  createDocumentDisabledReason?: string
  savingDocument: boolean
  onOpenChange: (open: boolean) => void
  onCreateDocument: () => void
  onCopyOrder: () => void
  onRefresh: () => void
  documentPath: (order: ShopeeTimelineOrder) => string
}

export function OrderTimelineDrawer({
  open,
  order,
  steps,
  milestones,
  events,
  loading,
  refreshing,
  error,
  canCreateDocument,
  createDocumentDisabledReason,
  savingDocument,
  onOpenChange,
  onCreateDocument,
  onCopyOrder,
  onRefresh,
  documentPath,
}: OrderTimelineDrawerProps) {
  const [systemOpen, setSystemOpen] = useState(false)
  const createDisabledReason = createDocumentDisabledReason || (!canCreateDocument ? 'ยังไม่ได้ตั้งค่า route คำสั่งซื้อ Shopee' : '')

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl lg:max-w-2xl">
        <SheetHeader className="border-b border-border px-4 py-3 text-left">
          <SheetTitle>Timeline คำสั่งซื้อ</SheetTitle>
          <SheetDescription>
            สถานะคำสั่งซื้อจาก Shopee และ milestone เอกสารใน Nexflow
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-3 px-4 py-3">
            {order && <OrderSummaryCard order={order} />}

            <LifecycleTimeline
              steps={steps}
              loading={loading && steps.length === 0}
              currentStatus={order?.order_status}
            />

            <DocumentMilestones
              milestones={milestones}
              loading={loading && milestones.length === 0}
            />

            <Alert className="border-info/30 bg-info/10">
              <Info className="h-4 w-4" />
              <AlertTitle>จัดส่งและใบปะหน้า</AlertTitle>
              <AlertDescription>
                ทำใน Seller Center แล้ว Nexflow จะติดตามสถานะกลับมาใน timeline นี้
              </AlertDescription>
            </Alert>

            {(loading || refreshing) && (
              <Card className="shadow-none">
                <CardContent className="flex items-center gap-2 p-3 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {refreshing ? 'กำลังตรวจสถานะล่าสุดจาก Shopee โดยไม่รีเฟรชรายการด้านหลัง' : 'กำลังโหลด timeline...'}
                </CardContent>
              </Card>
            )}

            {error && (
              <Alert variant="destructive">
                <AlertTriangle className="h-4 w-4" />
                <AlertTitle>โหลด timeline ไม่สำเร็จ</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            <Collapsible open={systemOpen} onOpenChange={setSystemOpen}>
              <Card className="shadow-none">
                <CollapsibleTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    className="flex h-auto w-full justify-between rounded-lg px-3 py-3 text-left"
                  >
                    <span>
                      <span className="block text-sm font-medium text-foreground">รายละเอียดระบบและ Push events</span>
                      <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
                        ใช้สำหรับ admin/support เมื่ออยากตรวจ raw event
                      </span>
                    </span>
                    <ChevronDown className={cn(
                      'mt-0.5 h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200 motion-reduce:transition-none',
                      systemOpen && 'rotate-180',
                    )} />
                  </Button>
                </CollapsibleTrigger>
                <CollapsibleContent className="data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0 motion-reduce:animate-none">
                  <CardContent className="space-y-2 border-t border-border p-3">
                    {!loading && events.length === 0 ? (
                      <div className="rounded-md bg-muted/30 px-3 py-4 text-center text-sm text-muted-foreground">
                        ยังไม่มี event log จาก Shopee แสดงสถานะปัจจุบันจาก snapshot ด้านบนแทน
                      </div>
                    ) : events.map((event) => (
                      <TimelineEventRow key={event.id} event={event} />
                    ))}
                  </CardContent>
                </CollapsibleContent>
              </Card>
            </Collapsible>
          </div>
        </ScrollArea>

        <div className="flex flex-col gap-2 border-t border-border p-3 sm:flex-row sm:justify-between">
          <div className="flex flex-col gap-2 sm:flex-row">
            {order?.bill_id ? (
              <Button asChild variant="outline" className="gap-2">
                <Link to={documentPath(order)}>
                  <Eye className="h-4 w-4" />
                  เปิดเอกสาร
                </Link>
              </Button>
            ) : order ? (
              <Button
                variant="outline"
                className="gap-2"
                disabled={savingDocument || Boolean(createDisabledReason)}
                onClick={onCreateDocument}
                title={createDisabledReason || undefined}
              >
                {savingDocument ? <Loader2 className="h-4 w-4 animate-spin" /> : <FilePlus2 className="h-4 w-4" />}
                สร้างเอกสาร
              </Button>
            ) : null}

            {order && (
              <Button
                variant="outline"
                className="gap-2"
                onClick={onCopyOrder}
              >
                <Copy className="h-4 w-4" />
                คัดลอก Order SN
              </Button>
            )}
          </div>
          <Button
            variant="outline"
            className="gap-2"
            onClick={onRefresh}
            disabled={refreshing || !order}
          >
            {refreshing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            ตรวจสถานะล่าสุด
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function OrderSummaryCard({ order }: { order: ShopeeTimelineOrder }) {
  return (
    <Card className="shadow-none">
      <CardContent className="p-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <div className="font-mono text-xs font-semibold text-foreground">{order.order_sn}</div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <OrderStatusBadge status={order.order_status} />
              <ERPStatusBadge status={order.erp_status} />
              <TimelineSourceBadge source={order.last_update_source} />
            </div>
          </div>
          <div className="sm:text-right">
            <div className="font-semibold">{money(order.total_amount)}</div>
            <div className="text-xs text-muted-foreground">{order.item_count.toLocaleString()} รายการ</div>
          </div>
        </div>
        <div className="mt-3 grid gap-2 sm:grid-cols-2">
          <SummaryLine label="ร้าน" value={order.shop_label || String(order.shop_id)} />
          <SummaryLine label="ลูกค้า" value={order.buyer_username || '-'} />
          <SummaryLine label="ขนส่ง" value={carrierLabel(order) || '-'} />
          <SummaryLine label="Tracking / Package" value={order.tracking_number || order.package_number || '-'} mono />
          <SummaryLine label="SML" value={order.sml_doc_no || 'ยังไม่ส่ง SML'} mono />
          <SummaryLine label="อัปเดตล่าสุด" value={formatOrderUpdate(order)} />
        </div>
      </CardContent>
    </Card>
  )
}

function LifecycleTimeline({
  steps,
  loading,
  currentStatus,
}: {
  steps: ShopeeTimelineStep[]
  loading: boolean
  currentStatus?: string
}) {
  return (
    <Card className="shadow-none">
      <CardHeader className="flex-row items-start justify-between gap-3 p-3 pb-0">
        <div>
          <CardTitle className="text-sm font-semibold tracking-normal">Timeline สถานะคำสั่งซื้อ</CardTitle>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Shopee snapshot เป็นสถานะจริงปัจจุบัน, Push/Sync เป็นหลักฐานเวลา
          </p>
        </div>
        {currentStatus && <OrderStatusBadge status={currentStatus} />}
      </CardHeader>
      <CardContent className="p-3">
        {loading ? (
          <TimelineSkeleton />
        ) : steps.length === 0 ? (
          <div className="rounded-md bg-muted/30 px-3 py-4 text-center text-sm text-muted-foreground">
            ยังไม่มี timeline สถานะจาก Shopee
          </div>
        ) : (
          <ol className="space-y-0">
            {steps.map((step, index) => (
              <LifecycleTimelineRow
                key={step.key}
                step={step}
                index={index}
                isLast={index === steps.length - 1}
              />
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}

function LifecycleTimelineRow({
  step,
  index,
  isLast,
}: {
  step: ShopeeTimelineStep
  index: number
  isLast: boolean
}) {
  const tone = lifecycleTone(step)
  const active = step.state === 'current'
  const done = step.state === 'done'
  return (
    <li className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
      <div className="relative flex justify-center">
        {!isLast && <span className="absolute top-8 h-[calc(100%-1.5rem)] w-px bg-border" />}
        <span className={cn(
          'relative z-10 flex h-7 w-7 items-center justify-center rounded-full border text-xs font-semibold tabular-nums transition-[background-color,border-color,transform] duration-200 ease-out motion-reduce:transition-none',
          markerTone(tone, active, done),
          active && 'scale-105 motion-reduce:scale-100',
        )}>
          {done ? <CheckCircle2 className="h-4 w-4" /> : index + 1}
        </span>
      </div>
      <div className={cn(
        'mb-2 rounded-md border px-3 py-2.5 transition-[background-color,border-color] duration-200 ease-out motion-reduce:transition-none',
        rowTone(tone, active, done),
      )}>
        <div className="flex flex-wrap items-center gap-2">
          <div className="font-medium text-foreground">{step.label}</div>
          {active && <Badge className={cn('h-5 px-1.5 text-[10px]', tone === 'danger' ? 'bg-destructive text-destructive-foreground' : 'bg-info text-info-foreground')}>ตอนนี้</Badge>}
          <TimelineSourceBadge source={step.source} />
          <ConfidenceBadge confidence={step.confidence} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <Clock className="h-3.5 w-3.5" />
            {timelineTimeText(step)}
          </span>
          {step.status && <span className="font-mono">{step.status}</span>}
        </div>
        {step.detail && <div className="mt-1 text-xs text-muted-foreground">{step.detail}</div>}
      </div>
    </li>
  )
}

function DocumentMilestones({ milestones, loading }: { milestones: ShopeeERPMilestone[]; loading: boolean }) {
  const visibleMilestones = milestones.length ? milestones : defaultERPMilestones()
  return (
    <Card className="shadow-none">
      <CardHeader className="p-3 pb-0">
        <CardTitle className="text-sm font-semibold tracking-normal">เอกสารใน Nexflow</CardTitle>
      </CardHeader>
      <CardContent className="p-3">
        {loading ? (
          <div className="grid gap-2 sm:grid-cols-2">
            <Skeleton className="h-16" />
            <Skeleton className="h-16" />
          </div>
        ) : (
          <div className="grid gap-2 sm:grid-cols-2">
            {visibleMilestones.map((milestone) => (
              <DocumentMilestoneItem key={milestone.key} milestone={milestone} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function DocumentMilestoneItem({ milestone }: { milestone: ShopeeERPMilestone }) {
  const done = milestone.state === 'done'
  const failed = milestone.state === 'failed'
  const current = milestone.state === 'current'
  return (
    <div className={cn(
      'rounded-md border px-3 py-2 text-sm transition-colors duration-200 motion-reduce:transition-none',
      done && 'border-accentStrong/30 bg-primary/10',
      failed && 'border-destructive/40 bg-destructive/10',
      current && 'border-warning/40 bg-warning/10',
      !done && !failed && !current && 'border-border bg-muted/20',
    )}>
      <div className="flex items-center gap-2">
        {done ? (
          <CheckCircle2 className="h-4 w-4 text-accentStrong" />
        ) : failed ? (
          <AlertTriangle className="h-4 w-4 text-destructive" />
        ) : current ? (
          <AlertTriangle className="h-4 w-4 text-warning" />
        ) : (
          <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/50" />
        )}
        <div className="font-medium text-foreground">{milestone.label}</div>
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{milestone.detail || '-'}</div>
      {milestone.occurred_at && (
        <div className="mt-1 text-xs text-muted-foreground">{formatBangkokDateTime(milestone.occurred_at)} เวลาไทย</div>
      )}
    </div>
  )
}

function TimelineEventRow({ event }: { event: ShopeeTimelineEvent }) {
  return (
    <div className="flex gap-3 rounded-md bg-muted/30 px-3 py-2 text-sm">
      <div className={cn('mt-1 h-2.5 w-2.5 shrink-0 rounded-full', timelineDotTone(event.source, event.status))} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <div className="font-medium text-foreground">{event.title || 'Shopee update'}</div>
          <TimelineSourceBadge source={event.source} />
        </div>
        {event.detail && <div className="mt-0.5 break-words text-xs text-muted-foreground">{event.detail}</div>}
        <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          <span>{event.created_at ? formatBangkokDateTime(event.created_at) : 'ไม่ระบุเวลา'}</span>
          {event.status && <span>· {event.status}</span>}
        </div>
      </div>
    </div>
  )
}

function TimelineSkeleton() {
  return (
    <div className="space-y-2">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
          <Skeleton className="mx-auto h-7 w-7 rounded-full" />
          <Skeleton className="h-16" />
        </div>
      ))}
    </div>
  )
}

function SummaryLine({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn('mt-0.5 break-words font-medium text-foreground', mono && 'font-mono text-xs')}>
        {value || '-'}
      </div>
    </div>
  )
}

function OrderStatusBadge({ status }: { status: string }) {
  const s = status.toUpperCase()
  return (
    <Badge variant="outline" className={cn(
      'bg-background',
      s === 'CANCELLED' && 'border-destructive/40 bg-destructive/10 text-destructive',
      s === 'SHIPPED' || s === 'COMPLETED' ? 'border-accentStrong/40 bg-primary/10 text-accentStrong' : '',
      s === 'READY_TO_SHIP' || s === 'PROCESSED' ? 'border-info/40 bg-info/10 text-info' : '',
    )}>
      {s || '-'}
    </Badge>
  )
}

function ERPStatusBadge({ status }: { status: string }) {
  return (
    <Badge variant="outline" className={cn(
      'bg-background',
      status === 'sent' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
      status === 'needs_review' && 'border-warning/40 bg-warning/10 text-warning',
      status === 'failed' || status === 'cancelled' ? 'border-destructive/40 bg-destructive/10 text-destructive' : '',
    )}>
      {erpStatusLabel(status)}
    </Badge>
  )
}

function TimelineSourceBadge({ source }: { source?: string }) {
  const label = sourceLabel(source)
  return (
    <Badge variant="outline" className={cn(
      'h-5 bg-background px-1.5 text-[10px]',
      label === 'Push' && 'border-info/40 bg-info/10 text-info',
      label === 'Sync' && 'text-muted-foreground',
      label === 'Nexflow' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
      label === 'Seller Center' && 'border-warning/40 bg-warning/10 text-warning',
    )}>
      {label}
    </Badge>
  )
}

function ConfidenceBadge({ confidence }: { confidence?: string }) {
  const normalized = String(confidence ?? '').trim()
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant="outline" className={cn(
          'h-5 cursor-help bg-background px-1.5 text-[10px]',
          normalized === 'confirmed' && 'border-accentStrong/40 bg-primary/10 text-accentStrong',
          normalized === 'inferred' && 'border-warning/40 bg-warning/10 text-warning',
          (!normalized || normalized === 'missing') && 'text-muted-foreground',
        )}>
          {confidenceLabel(normalized)}
        </Badge>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        {confidenceHelp(normalized)}
      </TooltipContent>
    </Tooltip>
  )
}

function confidenceLabel(confidence?: string) {
  switch (confidence) {
    case 'confirmed':
      return 'ยืนยันแล้ว'
    case 'inferred':
      return 'ประมาณ'
    case 'missing':
      return 'ไม่มีเวลา'
    default:
      return 'ไม่ระบุ'
  }
}

function confidenceHelp(confidence?: string) {
  switch (confidence) {
    case 'confirmed':
      return 'มีหลักฐานเวลาจาก Push หรือข้อมูลที่ Shopee/Nexflow ยืนยันแล้ว'
    case 'inferred':
      return 'Nexflow รู้ว่าสถานะนี้เกิดขึ้นจาก snapshot ล่าสุด แต่ Shopee ไม่ส่งเวลาของสถานะนั้นมาโดยตรง'
    case 'missing':
      return 'ยังไม่มีเวลาหรือหลักฐานเฉพาะสำหรับสถานะนี้'
    default:
      return 'ยังไม่ระบุความมั่นใจของสถานะนี้'
  }
}

function defaultERPMilestones(): ShopeeERPMilestone[] {
  return [
    { key: 'document', label: 'สร้างเอกสาร', detail: 'ยังไม่สร้างเอกสารใน Nexflow', state: 'upcoming', source: 'nexflow', confidence: 'missing' },
    { key: 'sml', label: 'ส่ง SML', detail: 'ยังไม่ส่ง SML', state: 'upcoming', source: 'nexflow', confidence: 'missing' },
  ]
}

function markerTone(tone: 'success' | 'info' | 'danger' | 'muted', active: boolean, done: boolean) {
  if (tone === 'danger') return 'border-destructive bg-destructive text-destructive-foreground'
  if (done) return 'border-accentStrong bg-accentStrong text-white'
  if (active) return 'border-info bg-info text-info-foreground'
  if (tone === 'success') return 'border-accentStrong/40 bg-primary/10 text-accentStrong'
  return 'border-border bg-background text-muted-foreground'
}

function rowTone(tone: 'success' | 'info' | 'danger' | 'muted', active: boolean, done: boolean) {
  if (tone === 'danger') return 'border-destructive/40 bg-destructive/10'
  if (active) return 'border-info/40 bg-info/10'
  if (done) return 'border-accentStrong/30 bg-primary/10'
  return 'border-border bg-muted/20 text-muted-foreground'
}

function lifecycleTone(step: ShopeeTimelineStep): 'success' | 'info' | 'danger' | 'muted' {
  if (step.key === 'cancelled' && step.state === 'current') return 'danger'
  if (step.state === 'done') return 'success'
  if (step.state === 'current') return 'info'
  return 'muted'
}

function timelineTimeText(step: ShopeeTimelineStep) {
  if (step.occurred_at) return `${formatBangkokDateTime(step.occurred_at)} เวลาไทย`
  if (step.confidence === 'inferred') return 'ผ่านสถานะนี้แล้ว แต่ Shopee ไม่ส่งเวลาใน Push'
  if (step.source === 'sync') return 'อัปเดตจาก Shopee Sync'
  return 'ยังไม่มีเวลาจาก Shopee'
}

function timelineDotTone(source?: string, status?: string) {
  const normalizedStatus = String(status ?? '').toLowerCase()
  if (normalizedStatus.includes('fail') || normalizedStatus.includes('error')) return 'bg-destructive'
  const label = sourceLabel(source)
  if (label === 'Push') return 'bg-info'
  if (label === 'Seller Center') return 'bg-warning'
  if (label === 'Nexflow') return 'bg-accentStrong'
  return 'bg-muted-foreground'
}

function sourceLabel(source?: string) {
  switch (String(source ?? '').trim()) {
    case 'push':
    case 'shopee_push':
    case 'Push':
      return 'Push'
    case 'sync':
    case 'Sync':
      return 'Sync'
    case 'Shopee':
      return 'Sync'
    case 'shipping':
      return 'Shipping check'
    case 'shop_auth':
      return 'Shop Auth'
    case 'snapshot':
      return 'Snapshot'
    case 'nexflow':
    case 'Nexflow':
      return 'Nexflow'
    case 'Seller Center':
      return 'Seller Center'
    case 'console_verify':
    case 'Shopee Console':
      return 'Console Verify'
    default:
      return 'Unknown'
  }
}

function erpStatusLabel(status: string) {
  switch (status) {
    case 'blocked': return 'Pending - บล็อก'
    case 'pending': return 'รอสร้างเอกสาร'
    case 'pending_erp': return 'สร้างเอกสารแล้ว รอส่ง SML'
    case 'needs_review': return 'ต้องตรวจ'
    case 'sent': return 'ส่ง SML แล้ว'
    case 'failed': return 'Failed - ไม่สำเร็จ'
    case 'cancelled': return 'Cancelled'
    case 'waiting_shopee': return 'Completed - รอ Shopee'
    default: return status || '-'
  }
}

function formatOrderUpdate(order: ShopeeTimelineOrder) {
  const value = order.last_order_update_at || order.last_synced_at
  return value ? formatBangkokDateTime(value) : '-'
}

function formatBangkokDateTime(value?: string | Date) {
  if (!value) return ''
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Bangkok',
    day: '2-digit',
    month: '2-digit',
    year: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date).replace(',', '')
}

function carrierLabel(order: ShopeeTimelineOrder | null | undefined) {
  if (!order) return ''
  const checkout = String(order.checkout_shipping_carrier ?? '').trim()
  const carrier = String(order.shipping_carrier ?? '').trim()
  if (checkout && carrier && checkout !== carrier) return `${checkout} / ${carrier}`
  return checkout || carrier
}

function money(v: number | undefined) {
  return new Intl.NumberFormat('th-TH', { style: 'currency', currency: 'THB' }).format(Number(v ?? 0))
}
