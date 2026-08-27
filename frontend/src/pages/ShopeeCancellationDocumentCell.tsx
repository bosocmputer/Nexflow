import { Info } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  cancellationDocumentSummary,
  cancellationDocumentTypeLabel,
  cancellationStockRecalcLabel,
  cancellationTriggerLabel,
} from '@/lib/shopee-operations-status'
import { cn } from '@/lib/utils'

type CancellationOrder = {
  order_sn: string
  sml_doc_no?: string
  sml_cancel_doc_no?: string
  sml_cancel_status?: string
  sml_cancel_error?: string
  sml_cancel_stock_recalc_status?: string
  sml_cancel_stock_recalc_error?: string
  sml_cancel_trigger_source?: string
  sml_cancel_document_type?: string
}

export function ShopeeCancellationDocumentCell({ order }: { order: CancellationOrder }) {
  const documentType = cancellationDocumentTypeLabel(order.sml_cancel_document_type)
  const trigger = cancellationTriggerLabel(order.sml_cancel_trigger_source)
  const stockRecalc = cancellationStockRecalcLabel(order.sml_cancel_stock_recalc_status)
  const summary = cancellationDocumentSummary({
    status: order.sml_cancel_status,
    documentType: order.sml_cancel_document_type,
    triggerSource: order.sml_cancel_trigger_source,
    saleDocNo: order.sml_doc_no,
    cancelDocNo: order.sml_cancel_doc_no,
  })

  return (
    <div className="flex max-w-[300px] flex-col items-start gap-1 text-xs">
      <div className="flex min-w-0 items-center gap-1">
        <Badge
          variant="outline"
          className={cn(
            'h-5 max-w-[260px] truncate whitespace-nowrap px-1.5 text-[10px]',
            summary.tone === 'info'
              ? 'border-info/40 bg-info/10 text-info'
              : 'border-destructive/40 bg-destructive/10 text-destructive',
          )}
          title={summary.headline}
        >
          {summary.headline}
        </Badge>
        <Popover>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-6 w-6 shrink-0 text-muted-foreground hover:text-foreground"
              aria-label={`ดูรายละเอียด ${documentType} ของ order ${order.order_sn}`}
              title="ดูรายละเอียดเอกสารหลังยกเลิก"
              onClick={(event) => event.stopPropagation()}
            >
              <Info className="h-3.5 w-3.5" />
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 p-3" onClick={(event) => event.stopPropagation()}>
            <div className="text-sm font-semibold text-foreground">รายละเอียดเอกสารหลังยกเลิก</div>
            <div className="mt-0.5 font-mono text-[11px] text-muted-foreground">Order {order.order_sn}</div>
            <div className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2">
              <DetailField label="ประเภทเอกสาร" value={documentType} />
              <DetailField label="วิธีสร้าง" value={trigger || 'ยังไม่มีข้อมูล'} />
              <DetailField label="ใบขายเดิม" value={order.sml_doc_no || '-'} mono />
              <DetailField label="เอกสารหลังยกเลิก" value={order.sml_cancel_doc_no || 'รอเลขเอกสาร'} mono />
              <DetailField label="ผลสร้างเอกสาร" value={cancellationCreateStatusLabel(order.sml_cancel_status)} />
              <DetailField label="สต๊อก SML" value={stockRecalc?.label || 'ยังไม่มีผลคำนวณ'} />
            </div>
            {(order.sml_cancel_error || order.sml_cancel_stock_recalc_error) && (
              <div className="mt-3 rounded-md bg-destructive/10 p-2 text-xs text-destructive">
                {order.sml_cancel_error || order.sml_cancel_stock_recalc_error}
              </div>
            )}
          </PopoverContent>
        </Popover>
      </div>
      <code className="whitespace-nowrap text-[11px] text-muted-foreground" title={summary.documentLine}>
        {summary.documentLine}
      </code>
    </div>
  )
}

function DetailField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn('mt-0.5 truncate text-sm font-medium text-foreground', mono && 'font-mono text-xs')} title={value}>
        {value || '-'}
      </div>
    </div>
  )
}

function cancellationCreateStatusLabel(status?: string) {
  switch (status?.trim().toLowerCase()) {
    case 'created': return 'สร้างแล้ว'
    case 'already_exists': return 'พบเอกสารเดิมแล้ว'
    case 'creating': return 'กำลังสร้าง'
    case 'pending': return 'รอสร้าง'
    case 'previewed': return 'ตรวจรูปแบบแล้ว'
    case 'failed': return 'สร้างไม่สำเร็จ'
    case 'blocked': return 'ต้องตรวจสอบ'
    case 'superseded': return 'มีรายการใหม่แทนที่แล้ว'
    default: return 'ยังไม่ได้สร้าง'
  }
}
