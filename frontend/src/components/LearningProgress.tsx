import { Brain } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { MappingStats } from '@/types'

function ProgressRow({
  label,
  value,
  total,
  variant,
}: {
  label: string
  value: number
  total: number
  variant: 'success' | 'warning'
}) {
  const pct = total > 0 ? Math.round((value / total) * 100) : 0
  const fill =
    variant === 'success' ? 'bg-success' : 'bg-warning'

  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-medium tabular-nums text-foreground">
          {value}
          <span className="ml-1 text-muted-foreground">
            / {total} ({pct}%)
          </span>
        </span>
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
        <div
          className={cn('h-full rounded-full transition-all', fill)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

export default function LearningProgress({ stats }: { stats: MappingStats }) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <Brain className="h-4 w-4 text-primary" />
          ความจำการจับคู่สินค้า
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <ProgressRow
          label="ระบบจับคู่ได้เอง"
          value={stats.auto_confirmed ?? 0}
          total={stats.total ?? 0}
          variant="success"
        />
        <ProgressRow
          label="ต้องให้คนตรวจ"
          value={stats.needs_review ?? 0}
          total={stats.total ?? 0}
          variant="warning"
        />
      </CardContent>
    </Card>
  )
}
