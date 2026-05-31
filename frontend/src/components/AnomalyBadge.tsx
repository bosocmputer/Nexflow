import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { Anomaly } from '@/types'

export default function AnomalyBadge({ anomaly }: { anomaly: Anomaly }) {
  const isError = anomaly.severity === 'error'
  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant={isError ? 'destructive' : 'secondary'}
            className="cursor-help font-normal"
          >
            {isError ? '🚫' : '⚠️'} {anomaly.type.replace('_', ' ')}
          </Badge>
        </TooltipTrigger>
        <TooltipContent className="max-w-xs text-xs">
          {anomaly.message}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
