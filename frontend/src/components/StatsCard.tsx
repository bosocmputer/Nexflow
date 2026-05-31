import { type LucideIcon } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export interface StatsCardProps {
  label: string
  value: string | number
  icon: LucideIcon
  variant?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
  hint?: string
  className?: string
}

const tone: Record<NonNullable<StatsCardProps['variant']>, string> = {
  primary: 'bg-primary/10 text-primary',
  success: 'bg-success/10 text-success',
  warning: 'bg-warning/10 text-warning',
  danger: 'bg-destructive/10 text-destructive',
  info: 'bg-info/10 text-info',
}

export default function StatsCard({
  label,
  value,
  icon: Icon,
  variant = 'primary',
  hint,
  className,
}: StatsCardProps) {
  return (
    <Card className={cn('rounded-xl border-border/80 shadow-sm transition-shadow hover:shadow-md', className)}>
      <CardContent className="flex items-start justify-between gap-3 p-5">
        <div className="min-w-0 flex-1">
          <p className="text-xs font-medium text-muted-foreground">{label}</p>
          <p className="mt-2 text-2xl font-semibold tabular-nums tracking-tight text-foreground">
            {value}
          </p>
          {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
        </div>
        <div className={cn('flex h-9 w-9 items-center justify-center rounded-lg', tone[variant])}>
          <Icon className="h-[18px] w-[18px]" strokeWidth={2} />
        </div>
      </CardContent>
    </Card>
  )
}
