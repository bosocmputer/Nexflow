import { Moon, Sun, Monitor, Check } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useTheme, type Theme } from '@/lib/theme'
import { cn } from '@/lib/utils'

const options: Array<{ key: Theme; label: string; icon: typeof Sun }> = [
  { key: 'light', label: 'สว่าง', icon: Sun },
  { key: 'dark', label: 'มืด', icon: Moon },
  { key: 'system', label: 'ตามระบบ', icon: Monitor },
]

export function ThemeToggle({
  variant = 'icon',
  className,
}: {
  variant?: 'icon' | 'menu-item'
  className?: string
}) {
  const { theme, setTheme } = useTheme()

  if (variant === 'menu-item') {
    return (
      <div className={cn('flex flex-col gap-0.5 px-1 py-1', className)}>
        <div className="px-2 pb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          ธีม
        </div>
        {options.map((opt) => {
          const Icon = opt.icon
          const active = theme === opt.key
          return (
            <button
              key={opt.key}
              type="button"
              onClick={() => setTheme(opt.key)}
              className={cn(
                'flex items-center justify-between rounded-sm px-2 py-1.5 text-sm hover:bg-accent',
                active && 'text-foreground',
              )}
            >
              <span className="flex items-center gap-2">
                <Icon className="h-3.5 w-3.5" />
                {opt.label}
              </span>
              {active && <Check className="h-3.5 w-3.5 text-primary" />}
            </button>
          )
        })}
      </div>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className={cn('h-8 w-8', className)} aria-label="เปลี่ยนธีม">
          <Sun className="h-4 w-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
          <Moon className="absolute h-4 w-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {options.map((opt) => {
          const Icon = opt.icon
          return (
            <DropdownMenuItem
              key={opt.key}
              onClick={() => setTheme(opt.key)}
              className="gap-2"
            >
              <Icon className="h-4 w-4" />
              {opt.label}
              {theme === opt.key && <Check className="ml-auto h-3.5 w-3.5 text-primary" />}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
