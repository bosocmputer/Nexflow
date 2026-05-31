import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ChevronRight, FileText, Loader2 } from 'lucide-react'
import dayjs from 'dayjs'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import client from '@/api/client'
import { cn } from '@/lib/utils'

interface BillSummary {
  id: string
  bill_type: 'sale' | 'purchase'
  source: string
  status: string
  sml_doc_no?: string | null
  created_at: string
  sent_at?: string | null
  error_msg?: string | null
}

interface Props {
  lineUserID: string
}

const STATUS_COLORS: Record<string, string> = {
  sent: 'bg-success/15 text-success',
  pending: 'bg-warning/15 text-warning',
  needs_review: 'bg-warning/15 text-warning',
  failed: 'bg-destructive/15 text-destructive',
  confirmed: 'bg-info/15 text-info',
  skipped: 'bg-muted text-muted-foreground',
}

// CustomerHistoryPanel sits inside MessageThread (collapsible). Shows the
// last 10 bills this LINE customer has placed — answers the daily question
// "เคยสั่งสินค้าอะไรบ้าง" without leaving the chat.
export function CustomerHistoryPanel({ lineUserID }: Props) {
  const [bills, setBills] = useState<BillSummary[]>([])
  const [loading, setLoading] = useState(false)
  // Default closed — tighter layout. Admin clicks the bar to expand.
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let alive = true
    setLoading(true)
    client
      .get<{ data: BillSummary[] | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/history`,
      )
      .then((res) => {
        if (alive) setBills(res.data.data ?? [])
      })
      .catch(() => {
        if (alive) setBills([])
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [lineUserID])

  const sentCount = bills.filter((b) => b.status === 'sent').length

  // Skip rendering entirely when there are no bills and we've finished loading
  // — avoids an empty bar wasting a row of vertical space.
  if (!loading && bills.length === 0) {
    return null
  }

  return (
    <div className="border-b border-border bg-muted/20">
      <Button
        variant="ghost"
        className="flex h-7 w-full items-center justify-between gap-1.5 rounded-none px-3 text-[11px]"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="flex items-center gap-1.5 font-medium">
          {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          ประวัติคำสั่งซื้อ
        </span>
        <span className="text-muted-foreground">
          {loading ? '…' : `${bills.length} บิล · ${sentCount} ส่งแล้ว`}
        </span>
      </Button>
      {open && (
        <div className="px-4 pb-3">
          {loading ? (
            <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
              <Loader2 className="h-3 w-3 animate-spin" />
              กำลังโหลด…
            </div>
          ) : bills.length === 0 ? (
            <div className="py-2 text-xs text-muted-foreground">
              ยังไม่เคยสั่งซื้อ
            </div>
          ) : (
            <ul className="space-y-1">
              {bills.map((b) => (
                <li key={b.id}>
                  <Link
                    to={`/bills/${b.id}`}
                    className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-accent/40"
                  >
                    <FileText className="h-3 w-3 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate font-mono text-[11px]">
                      {b.sml_doc_no || b.id.slice(0, 8)}
                    </span>
                    <Badge
                      variant="secondary"
                      className={cn('h-4 px-1.5 text-[9px]', STATUS_COLORS[b.status] ?? '')}
                    >
                      {b.status}
                    </Badge>
                    <span className="shrink-0 tabular-nums text-muted-foreground">
                      {dayjs(b.created_at).format('DD/MM HH:mm')}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
