import { useEffect, useState } from 'react'
import { Loader2, Sparkles } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import client from '@/api/client'
import type { ExtractedBill } from './types'

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  lineUserID: string
  messageId: string
  // Called when admin clicks "ใช้ข้อมูลนี้สร้างบิล" — parent forwards extracted
  // data to CreateBillPanel as prefill.
  onUseAsBill: (extracted: ExtractedBill) => void
}

// ExtractPreviewDialog runs AI extract on a media chat_message and shows the
// result for admin review. No DB writes happen here — admin either dismisses
// or feeds the data into CreateBillPanel.
export function ExtractPreviewDialog({
  open,
  onOpenChange,
  lineUserID,
  messageId,
  onUseAsBill,
}: Props) {
  const [loading, setLoading] = useState(false)
  const [extracted, setExtracted] = useState<ExtractedBill | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) {
      setExtracted(null)
      setError('')
      return
    }
    setLoading(true)
    setError('')
    client
      .post<{ extracted: ExtractedBill | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/messages/${messageId}/extract`,
      )
      .then((res) => {
        setExtracted(res.data.extracted ?? null)
      })
      .catch((e: any) => {
        setError(e?.response?.data?.error ?? e?.message ?? 'extract failed')
      })
      .finally(() => setLoading(false))
  }, [open, lineUserID, messageId])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sparkles className="h-4 w-4 text-info" />
            ผลจาก AI extract
          </DialogTitle>
        </DialogHeader>

        <div className="min-h-[120px] py-2 text-sm">
          {loading && (
            <div className="flex items-center justify-center gap-2 py-8 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              กำลังวิเคราะห์ด้วย AI…
            </div>
          )}
          {!loading && error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-xs text-destructive">
              {error}
            </div>
          )}
          {!loading && !error && extracted && (
            <div className="space-y-3">
              {(extracted.customer_name || extracted.customer_phone) && (
                <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-xs">
                  {extracted.customer_name && <div>ลูกค้า: {extracted.customer_name}</div>}
                  {extracted.customer_phone && <div>เบอร์: {extracted.customer_phone}</div>}
                </div>
              )}
              <div>
                <div className="mb-1 text-xs font-medium text-muted-foreground">
                  รายการสินค้า ({extracted.items.length})
                </div>
                {extracted.items.length === 0 ? (
                  <div className="rounded-md border border-warning/40 bg-warning/5 p-3 text-xs text-warning">
                    AI ไม่พบรายการสินค้าในสื่อนี้ — กดปิดแล้วลองอีกครั้ง หรือใส่รายการเอง
                  </div>
                ) : (
                  <ul className="divide-y divide-border rounded-md border border-border">
                    {extracted.items.map((it, i) => (
                      <li key={i} className="flex items-center justify-between gap-2 px-3 py-1.5 text-xs">
                        <span className="min-w-0 flex-1 truncate">{it.raw_name}</span>
                        <span className="shrink-0 tabular-nums">
                          {it.qty} {it.unit ?? ''}
                        </span>
                        {it.price != null && (
                          <span className="shrink-0 tabular-nums text-muted-foreground">
                            ฿{Number(it.price).toLocaleString()}
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              {extracted.confidence != null && (
                <div className="text-[11px] text-muted-foreground">
                  Confidence: {(extracted.confidence * 100).toFixed(0)}%
                </div>
              )}
            </div>
          )}
          {!loading && !error && !extracted && (
            <div className="py-8 text-center text-xs text-muted-foreground">
              ไม่พบข้อมูล
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            ปิด
          </Button>
          {extracted && extracted.items.length > 0 && (
            <Button
              onClick={() => {
                onUseAsBill(extracted)
                toast.success('ใส่ข้อมูลใน Create Bill panel แล้ว')
              }}
            >
              ใช้ข้อมูลนี้สร้างบิล
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
