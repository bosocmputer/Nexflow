import type { ReactNode } from 'react'
import { AlertTriangle, Info } from 'lucide-react'

import { cn } from '@/lib/utils'

type SafetyTone = 'info' | 'warning' | 'danger'

interface ActionSafetyPanelProps {
  title: ReactNode
  description?: ReactNode
  items?: Array<{
    label: ReactNode
    value: ReactNode
    detail?: ReactNode
  }>
  tone?: SafetyTone
  className?: string
}

export function ActionSafetyPanel({
  title,
  description,
  items = [],
  tone = 'warning',
  className,
}: ActionSafetyPanelProps) {
  const Icon = tone === 'info' ? Info : AlertTriangle
  const toneClass = {
    info: 'border-info/30 bg-info/[0.05]',
    warning: 'border-warning/35 bg-warning/[0.08]',
    danger: 'border-destructive/35 bg-destructive/[0.06]',
  }[tone]
  const iconClass = {
    info: 'text-info',
    warning: 'text-warning',
    danger: 'text-destructive',
  }[tone]

  return (
    <div className={cn('rounded-md border px-3 py-2.5 text-xs', toneClass, className)}>
      <div className="flex items-start gap-2">
        <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', iconClass)} />
        <div className="min-w-0 flex-1">
          <div className="font-medium text-foreground">{title}</div>
          {description && (
            <div className="mt-0.5 leading-relaxed text-muted-foreground">
              {description}
            </div>
          )}
          {items.length > 0 && (
            <div className="mt-2 grid gap-1.5 sm:grid-cols-2">
              {items.map((item, index) => (
                <div key={index} className="min-w-0 rounded-md border border-border/70 bg-background/75 px-2 py-1.5">
                  <div className="text-[10px] font-medium text-muted-foreground">
                    {item.label}
                  </div>
                  <div className="mt-0.5 truncate font-medium text-foreground">
                    {item.value}
                  </div>
                  {item.detail && (
                    <div className="mt-0.5 line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">
                      {item.detail}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
