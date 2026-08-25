import { InputChannelBadge } from '@/components/marketplace/InputChannelBadge'
import { billSourceLabel } from '@/lib/labels'
import {
  classifyBillInputChannel,
} from '@/lib/billInputChannel'
import { cn } from '@/lib/utils'
import type { Bill } from '@/types'

export function BillInputChannelBadge({ bill, className }: { bill: Bill; className?: string }) {
  const channel = classifyBillInputChannel(bill)
  if (!channel) {
    return (
      <span className={cn(
        'inline-flex h-6 w-fit items-center rounded-md border border-border bg-muted px-2 text-[11px] font-medium text-foreground',
        className,
      )}>
        {billSourceLabel(bill.source)}
      </span>
    )
  }

  return <InputChannelBadge channel={channel} className={className} />
}
