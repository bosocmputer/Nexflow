import { cn } from '@/lib/utils'

export interface PageHeaderProps {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  breadcrumb?: React.ReactNode
  className?: string
}

export function PageHeader({
  title,
  description,
  actions,
  breadcrumb,
  className,
}: PageHeaderProps) {
  return (
    <div className={cn('mb-5 space-y-2 rounded-lg border border-border/70 bg-card/85 px-4 py-4 shadow-sm backdrop-blur sm:px-5', className)}>
      {breadcrumb && <div className="text-xs">{breadcrumb}</div>}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground lg:text-[26px]">
            {title}
          </h1>
          {description && (
            <p className="mt-1 max-w-4xl text-sm leading-6 text-muted-foreground">{description}</p>
          )}
        </div>
        {actions && (
          <div className="flex shrink-0 flex-wrap items-center gap-2 sm:justify-end">{actions}</div>
        )}
      </div>
    </div>
  )
}
