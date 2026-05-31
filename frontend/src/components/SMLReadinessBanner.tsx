import { AlertTriangle, RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useSMLReadiness } from '@/hooks/useSMLReadiness'
import { smlBlockedMessage } from '@/lib/sml-readiness'
import { cn } from '@/lib/utils'

function fmtCheckedAt(value?: string) {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat('th-TH', {
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(d)
}

export function SMLReadinessBanner() {
  const { readiness, loading, refresh } = useSMLReadiness()
  if (!readiness || readiness.ready) return null

  return (
    <div className="border-b border-warning/30 bg-warning/[0.08] px-3 py-2">
      <div className="mx-auto flex max-w-[1480px] flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-start gap-2.5">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
          <div className="min-w-0">
            <div className="text-sm font-semibold text-foreground">
              SML ของร้านนี้ยังไม่พร้อม
            </div>
            <div className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
              {smlBlockedMessage(readiness)}
              {readiness.tenant && (
                <>
                  {' '}· ฐานข้อมูล <span className="font-mono text-foreground">{readiness.tenant}</span>
                </>
              )}
              {readiness.checked_at && (
                <> · ตรวจล่าสุด {fmtCheckedAt(readiness.checked_at)}</>
              )}
            </div>
          </div>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 shrink-0 gap-1.5 bg-background"
          onClick={() => refresh(true)}
          disabled={loading}
          title="ตรวจ /health/ready ของ sml-api-bybos อีกครั้ง"
        >
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
          ตรวจอีกครั้ง
        </Button>
      </div>
    </div>
  )
}
