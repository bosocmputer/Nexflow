import type { ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

interface MarketplaceOperationsHeaderProps {
  titleID: string
  title: string
  modeLabel: string
  modeClassName?: string
  routeLabel: string
  routeTitle?: string
  description: ReactNode
  health: ReactNode
  actions: ReactNode
  children?: ReactNode
}

// Keeps Marketplace operation pages visually aligned while leaving each
// channel responsible for its own data, controls, and safety behavior.
export function MarketplaceOperationsHeader({
  titleID,
  title,
  modeLabel,
  modeClassName,
  routeLabel,
  routeTitle,
  description,
  health,
  actions,
  children,
}: MarketplaceOperationsHeaderProps) {
  return (
    <section className="rounded-lg border border-border bg-card px-3 py-2" aria-labelledby={titleID}>
      <div className="flex flex-col gap-2 xl:flex-row xl:items-center xl:justify-between">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h1 id={titleID} className="text-lg font-semibold tracking-normal">{title}</h1>
            <Badge className={cn('h-6 px-2 text-[11px] text-white', modeClassName)}>{modeLabel}</Badge>
            <span
              className="inline-flex h-6 items-center rounded-full border border-border bg-background px-2 text-xs text-muted-foreground"
              title={routeTitle}
            >
              {routeLabel}
            </span>
          </div>
          <p className="max-w-3xl text-xs leading-5 text-muted-foreground">{description}</p>
          {health}
        </div>
        <div className="flex flex-col gap-2 sm:flex-row xl:shrink-0">{actions}</div>
      </div>
      {children && <div className="mt-2">{children}</div>}
    </section>
  )
}
