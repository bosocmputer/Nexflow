import { FileSpreadsheet } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import {
  billInputChannelLabel,
  marketplaceSourceInputChannels,
  type BillInputChannel,
} from '@/lib/billInputChannel'
import { cn } from '@/lib/utils'

const CHANNEL_CLASS: Record<BillInputChannel, string> = {
  shopee: 'border-[#EE4D2D] bg-[#EE4D2D] text-white',
  shopee_excel: 'border-[#EE4D2D] bg-[#EE4D2D] text-white',
  lazada_excel: 'border-[#1d3491] bg-[#1d3491] text-white',
  tiktok_excel: 'border-[#111817] bg-[#111817] text-white',
}

export function InputChannelBadge({
  channel,
  count,
  label,
  className,
}: {
  channel: BillInputChannel
  count?: number
  label?: string
  className?: string
}) {
  const excel = channel.endsWith('_excel')
  return (
    <span
      className={cn(
        'inline-flex h-6 w-fit items-center gap-1.5 whitespace-nowrap rounded-md border px-2 text-[11px] font-semibold leading-none',
        CHANNEL_CLASS[channel],
        className,
      )}
    >
      {excel && <FileSpreadsheet className="h-3.5 w-3.5" aria-hidden="true" />}
      {label || billInputChannelLabel(channel)}
      {count != null && <span className="tabular-nums opacity-85">{count.toLocaleString()}</span>}
    </span>
  )
}

export function MarketplaceSourceChannelBadges({
  source,
  count,
  accountName,
  className,
}: {
  source: string
  count?: number
  accountName?: string
  className?: string
}) {
  const channels = marketplaceSourceInputChannels(source)
  if (channels.length === 0) {
    return <Badge variant="secondary" className={className}>{source || 'Marketplace'}</Badge>
  }
  const title = source === 'shopee'
    ? 'การจับคู่นี้ใช้ร่วมกันได้ทั้ง Shopee API และ Shopee Excel'
    : undefined
  return (
    <span className={cn('flex flex-wrap gap-1.5', className)} title={title}>
      {channels.map((channel) => (
        <InputChannelBadge
          key={channel}
          channel={channel}
          count={count}
          label={channel === 'shopee' && accountName ? `${billInputChannelLabel(channel)} (${accountName})` : undefined}
        />
      ))}
    </span>
  )
}
