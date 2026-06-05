import { Menu, Search } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { KeyboardShortcut } from '@/components/common/KeyboardShortcut'
import { NotificationBell } from '@/components/layout/NotificationBell'
import { useCrumbs } from '@/lib/breadcrumbs'
import { useUIStore } from '@/lib/ui-store'
import { cn } from '@/lib/utils'

interface TopbarProps {
  onOpenPalette?: () => void
}

export default function Topbar({ onOpenPalette }: TopbarProps) {
  const crumbs = useCrumbs()
  const location = useLocation()
  const toggleMobileNav = useUIStore((s) => s.toggleMobileNav)

  const routeChip =
    location.pathname.startsWith('/sale-invoices')
      ? 'ขายสินค้าและบริการ'
      : location.pathname.startsWith('/import/shopee')
        ? 'นำเข้า Shopee ย้อนหลัง'
        : location.pathname.startsWith('/shopee-operations')
          ? 'คำสั่งซื้อ Shopee'
          : location.pathname.startsWith('/dashboard')
            ? 'Operations Console'
            : location.pathname.startsWith('/setup')
              ? 'Setup readiness'
              : 'Nexflow'

  return (
    <header className="sticky top-0 z-20 flex h-14 shrink-0 items-center gap-3 border-b border-border/70 bg-background/90 px-3 backdrop-blur-md sm:px-4">
      <button
        type="button"
        className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-border bg-card text-foreground shadow-sm transition-colors hover:bg-accent/70 md:hidden"
        onClick={toggleMobileNav}
        aria-label="เปิดเมนู"
      >
        <Menu className="h-4 w-4" />
      </button>

      {crumbs.length > 0 && (
        <Breadcrumb className="min-w-0">
          <BreadcrumbList className="flex-nowrap overflow-hidden">
            {crumbs.map((c, i) => {
              const isLast = i === crumbs.length - 1
              return (
                <span key={i} className="inline-flex items-center gap-1.5">
                  {i > 0 && <BreadcrumbSeparator />}
                  <BreadcrumbItem>
                    {isLast || !c.href ? (
                      <BreadcrumbPage className="max-w-[46vw] truncate text-foreground sm:max-w-none">{c.label}</BreadcrumbPage>
                    ) : (
                      <BreadcrumbLink asChild>
                        <Link to={c.href}>{c.label}</Link>
                      </BreadcrumbLink>
                    )}
                  </BreadcrumbItem>
                </span>
              )
            })}
          </BreadcrumbList>
        </Breadcrumb>
      )}
      <div className="ml-auto flex items-center gap-2">
        <span
          className={cn(
            'hidden h-8 items-center rounded-full border border-primary/30 bg-primary/10 px-3 text-xs font-semibold text-foreground md:inline-flex',
            location.pathname.startsWith('/sale-invoices') && 'bg-primary/20',
          )}
        >
          {routeChip}
        </span>
        <NotificationBell />
        <button
          type="button"
          className="hidden h-8 items-center gap-2 rounded-md border border-border bg-card px-3 text-xs text-muted-foreground shadow-sm transition-colors hover:bg-accent/70 hover:text-foreground sm:flex"
          onClick={onOpenPalette}
          aria-label="เปิดค้นหา"
        >
          <Search className="h-3.5 w-3.5" />
          <span>ค้นหา…</span>
          <span className="ml-4">
            <KeyboardShortcut keys="mod+k" />
          </span>
        </button>
      </div>
    </header>
  )
}
