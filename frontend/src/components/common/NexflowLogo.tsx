import { cn } from '@/lib/utils'

interface NexflowLogoProps {
  className?: string
}

export function NexflowLogo({ className }: NexflowLogoProps) {
  return (
    <img
      src="/nexflow-mark.svg"
      alt="Nexflow"
      className={cn('h-8 w-8 shrink-0 rounded-lg shadow-sm', className)}
      draggable={false}
    />
  )
}
