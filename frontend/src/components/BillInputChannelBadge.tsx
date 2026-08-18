import { FileSpreadsheet } from 'lucide-react'

import { billSourceLabel } from '@/lib/labels'
import {
  billInputChannelLabel,
  classifyBillInputChannel,
  type BillInputChannel,
} from '@/lib/billInputChannel'
import { cn } from '@/lib/utils'
import type { Bill } from '@/types'

const CHANNEL_CLASS: Record<BillInputChannel, string> = {
  shopee: 'border-[#c93d20] bg-[#c93d20] text-white',
  shopee_excel: 'border-[#c93d20] bg-[#c93d20] text-white',
  lazada_excel: 'border-[#1d3491] bg-[#1d3491] text-white',
  tiktok_excel: 'border-[#111817] bg-[#111817] text-white',
}

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

  const excel = channel.endsWith('_excel')
  return (
    <span
      className={cn(
        'inline-flex h-6 w-fit items-center gap-1.5 rounded-md border px-2 text-[11px] font-semibold leading-none',
        CHANNEL_CLASS[channel],
        className,
      )}
    >
      {excel && <FileSpreadsheet className="h-3.5 w-3.5" aria-hidden="true" />}
      {billInputChannelLabel(channel)}
    </span>
  )
}
