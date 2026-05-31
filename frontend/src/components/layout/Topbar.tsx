import { Search } from 'lucide-react'
import { Link } from 'react-router-dom'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { KeyboardShortcut } from '@/components/common/KeyboardShortcut'
import { useCrumbs } from '@/lib/breadcrumbs'

interface TopbarProps {
  onOpenPalette?: () => void
}

export default function Topbar({ onOpenPalette }: TopbarProps) {
  const crumbs = useCrumbs()

  return (
    <header className="sticky top-0 z-20 flex h-12 shrink-0 items-center gap-3 border-b border-border/70 bg-background/85 px-4 backdrop-blur-md">
      {crumbs.length > 0 && (
        <Breadcrumb>
          <BreadcrumbList>
            {crumbs.map((c, i) => {
              const isLast = i === crumbs.length - 1
              return (
                <span key={i} className="inline-flex items-center gap-1.5">
                  {i > 0 && <BreadcrumbSeparator />}
                  <BreadcrumbItem>
                    {isLast || !c.href ? (
                      <BreadcrumbPage className="text-foreground">{c.label}</BreadcrumbPage>
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
        <button
          type="button"
          className="hidden h-8 items-center gap-2 rounded-full border border-border bg-card px-3 text-xs text-muted-foreground shadow-sm transition-colors hover:bg-accent/70 hover:text-foreground md:flex"
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
