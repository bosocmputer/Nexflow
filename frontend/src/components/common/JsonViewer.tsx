import { useState } from 'react'
import { Check, ChevronDown, Copy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

export interface JsonViewerProps {
  title?: string
  data: unknown
  defaultOpen?: boolean
  className?: string
  emptyMessage?: string
}

export function JsonViewer({
  title,
  data,
  defaultOpen = false,
  className,
  emptyMessage = 'ไม่มีข้อมูล',
}: JsonViewerProps) {
  const [open, setOpen] = useState(defaultOpen)
  const [copied, setCopied] = useState(false)

  const isEmpty =
    data == null ||
    (typeof data === 'object' && Object.keys(data as object).length === 0)

  const formatted = isEmpty ? emptyMessage : JSON.stringify(data, null, 2)

  const handleCopy = async () => {
    if (isEmpty) return
    try {
      await navigator.clipboard.writeText(formatted)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard not available */
    }
  }

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className={cn('overflow-hidden rounded-lg border border-border bg-card', className)}
    >
      <div className="flex items-center justify-between gap-2 border-b border-border bg-muted/30 px-3 py-2">
        <CollapsibleTrigger className="flex flex-1 items-center gap-2 text-left text-sm font-medium text-foreground hover:text-primary">
          <ChevronDown
            className={cn(
              'h-4 w-4 text-muted-foreground transition-transform',
              open && 'rotate-180',
            )}
          />
          {title ?? 'JSON'}
          {!isEmpty && (
            <span className="text-xs font-normal text-muted-foreground">
              ({Array.isArray(data) ? `${data.length} items` : `${formatted.length} chars`})
            </span>
          )}
        </CollapsibleTrigger>
        {open && !isEmpty && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs"
            onClick={handleCopy}
          >
            {copied ? (
              <>
                <Check className="h-3 w-3" /> คัดลอกแล้ว
              </>
            ) : (
              <>
                <Copy className="h-3 w-3" /> คัดลอก
              </>
            )}
          </Button>
        )}
      </div>
      <CollapsibleContent>
        <pre
          className={cn(
            'overflow-x-auto p-3 font-mono text-xs leading-relaxed',
            isEmpty ? 'text-muted-foreground italic' : 'text-foreground',
          )}
        >
          {formatted}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  )
}
