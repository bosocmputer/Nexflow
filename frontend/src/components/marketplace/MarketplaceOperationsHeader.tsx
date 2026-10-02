import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

interface MarketplaceOperationsHeaderProps {
  titleID: string
  title: string
  modeLabel: string
  modeClassName?: string
  routeLabel: string
  routeTitle?: string
  routeTo?: string
  description: ReactNode
  health: ReactNode
  scopeControls: ReactNode
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
  routeTo,
  description,
  health,
  scopeControls,
  actions,
  children,
}: MarketplaceOperationsHeaderProps) {
  return (
    <section className="rounded-lg border border-border bg-card px-4 py-3" aria-labelledby={titleID}>
      <div className="grid gap-2 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-start lg:gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h1 id={titleID} className="text-lg font-semibold tracking-normal">{title}</h1>
            <Badge className={cn('h-6 px-2 text-[11px] text-white', modeClassName)}>{modeLabel}</Badge>
            {routeTo ? (
              <Link
                to={routeTo}
                className="inline-flex h-6 items-center rounded-full border border-border bg-background px-2 text-xs text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                title={routeTitle}
              >
                {routeLabel}
              </Link>
            ) : (
              <span
                className="inline-flex h-6 items-center rounded-full border border-border bg-background px-2 text-xs text-muted-foreground"
                title={routeTitle}
              >
                {routeLabel}
              </span>
            )}
          </div>
          <p className="mt-1 max-w-3xl text-xs leading-5 text-muted-foreground">{description}</p>
        </div>
        <div className="min-w-0 lg:max-w-xl lg:justify-self-end lg:pt-0.5">
          {health}
        </div>
      </div>
      <div
        data-slot="marketplace-operations-toolbar"
        className="mt-3 flex flex-col gap-2 border-t border-border pt-3 lg:flex-row lg:items-center lg:justify-between"
      >
        <div
          data-slot="marketplace-operations-scope"
          className="flex min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center"
        >
          {scopeControls}
        </div>
        <div
          data-slot="marketplace-operations-actions"
          className="flex flex-wrap items-center gap-2 lg:shrink-0 lg:justify-end"
        >
          {actions}
        </div>
      </div>
      {children && <div className="mt-2">{children}</div>}
    </section>
  )
}
