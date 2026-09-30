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
  actions,
  children,
}: MarketplaceOperationsHeaderProps) {
  return (
    <section className="rounded-lg border border-border bg-card px-3 py-3" aria-labelledby={titleID}>
      <div className="min-w-0">
        <div className="min-w-0 space-y-1">
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
          <p className="max-w-3xl text-xs leading-5 text-muted-foreground">{description}</p>
          {health}
        </div>
      </div>
      {/*
        Keep operational controls in their own full-width row.  The former
        right-aligned layout made the Auto SML status, settings button and
        switch compete for one narrow flex item as soon as the header copy or
        shop name became long.  A wrapping action bar preserves their order
        without allowing controls to overlap.
      */}
      <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-border pt-3">{actions}</div>
      {children && <div className="mt-2">{children}</div>}
    </section>
  )
}
