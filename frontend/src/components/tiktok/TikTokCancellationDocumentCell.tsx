import { Info } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { type TikTokCancellationState } from '@/lib/tiktok-shop-operations'
import { cn } from '@/lib/utils'

type TikTokCancellationDocumentCellProps = {
  orderID: string
  saleDocNo?: string
  cancelDocNo?: string
  document: TikTokCancellationState
}

// This is deliberately shaped like ShopeeCancellationDocumentCell: one compact
// two-line summary in the table and the durable evidence in a popover.
export function TikTokCancellationDocumentCell({
  orderID,
  saleDocNo,
  cancelDocNo,
  document,
}: TikTokCancellationDocumentCellProps) {
  const isComplete = document.status === 'completed'
  const isWarning = ['review_required', 'previewed', 'creating'].includes(document.status)
  const className = isComplete
    ? 'border-accentStrong/40 bg-primary/10 text-accentStrong'
    : isWarning
      ? 'border-info/40 bg-info/10 text-info'
      : document.tone === 'danger'
        ? 'border-destructive/40 bg-destructive/10 text-destructive'
        : 'border-border bg-muted/40 text-muted-foreground'

  return (
    <div className="flex max-w-[300px] flex-col items-start gap-1 text-xs">
      <div className="flex min-w-0 items-center gap-1">
        <Badge
          variant="outline"
          className={cn('h-5 max-w-[260px] truncate whitespace-nowrap px-1.5 text-[10px]', className)}
          title={document.label}
        >
          {document.label}
        </Badge>
        <Popover>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-6 w-6 shrink-0 text-muted-foreground hover:text-foreground"
              aria-label={`ดูรายละเอียดเอกสารหลังยกเลิกของคำสั่งซื้อ ${orderID}`}
              title="ดูรายละเอียดเอกสารหลังยกเลิก"
            >
              <Info className="h-3.5 w-3.5" />
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 p-3">
            <div className="text-sm font-semibold text-foreground">รายละเอียดเอกสารหลังยกเลิก</div>
            <div className="mt-0.5 font-mono text-[11px] text-muted-foreground">Order {orderID}</div>
            <div className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2">
              <DetailField label="สถานะ" value={document.label} />
              <DetailField label="การดำเนินการ" value={document.detail} />
              <DetailField label="ใบขายเดิม" value={saleDocNo || 'ยังไม่มีเลขเอกสาร'} mono />
              <DetailField label="เอกสารหลังยกเลิก" value={cancelDocNo || 'รอเลขเอกสาร'} mono />
            </div>
          </PopoverContent>
        </Popover>
      </div>
      <code className="max-w-full truncate whitespace-nowrap text-[11px] text-muted-foreground" title={document.detail}>
        {document.detail}
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
