import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, FilePlus2, Loader2, ShieldCheck } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

export interface TikTokBulkBillPreviewRow {
  shop_id: string
  shop_name?: string
  order_id: string
  order_status?: string
  review_digest?: string
  message: string
}

export interface TikTokBulkBillPreview {
  ready: TikTokBulkBillPreviewRow[]
  skipped: TikTokBulkBillPreviewRow[]
  ready_count: number
  skipped_count: number
  message: string
}

interface Props {
  open: boolean
  loading: boolean
  creating: boolean
  error: string
  preview: TikTokBulkBillPreview | null
  onOpenChange: (open: boolean) => void
  onCreate: () => void
}

// Mirrors the Shopee bulk-create review step. It intentionally creates only
// local Nexflow Bills, never sends SML, and leaves every unready order visible
// instead of hiding it behind a generic failure count.
export function TikTokBulkBillDialog({
  open,
  loading,
  creating,
  error,
  preview,
  onOpenChange,
  onCreate,
}: Props) {
  const [confirmed, setConfirmed] = useState(false)
  const readyRows = preview?.ready ?? []
  const canCreate = readyRows.length > 0 && confirmed && !loading && !creating
  const summary = useMemo(() => {
    if (!preview) return ''
    return preview.skipped_count > 0
      ? `พร้อมสร้าง ${preview.ready_count.toLocaleString('th-TH')} รายการ · ยังไม่พร้อม ${preview.skipped_count.toLocaleString('th-TH')} รายการ`
      : `พร้อมสร้าง ${preview.ready_count.toLocaleString('th-TH')} รายการ`
  }, [preview])

  useEffect(() => {
    setConfirmed(false)
  }, [open, preview?.ready.map((item) => `${item.shop_id}:${item.order_id}:${item.review_digest}`).join('|')])

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!creating) onOpenChange(nextOpen)
    }}>
      <DialogContent className="grid max-h-[92dvh] max-w-3xl grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0">
        <DialogHeader className="border-b border-border px-4 py-4 pr-12 sm:px-6">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle>สร้างเอกสาร TikTok Shop ใน Nexflow</DialogTitle>
            <Badge variant="outline">{summary || 'กำลังตรวจข้อมูล'}</Badge>
          </div>
          <DialogDescription>
            ระบบตรวจ snapshot, Product Master และเส้นทางเอกสารล่าสุดอีกครั้งก่อนสร้าง Bill แต่ยังไม่ส่ง SML, แจ้ง LINE หรือปรับสต๊อก
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 overflow-y-auto px-4 py-4 sm:px-6">
          {loading && (
            <div className="space-y-3" role="status" aria-label="กำลังตรวจออเดอร์ที่เลือก">
              <div className="h-16 animate-pulse rounded-lg bg-muted" />
              <div className="h-24 animate-pulse rounded-lg bg-muted" />
            </div>
          )}

          {!loading && error && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ตรวจรายการไม่สำเร็จ</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {!loading && !error && preview && (
            <div className="space-y-4">
              <Alert className="border-info/30 bg-info/10">
                <ShieldCheck className="h-4 w-4 text-info" />
                <AlertTitle>สร้างเฉพาะ Bill ใน Nexflow</AlertTitle>
                <AlertDescription>
                  หลังสร้างแล้ว ให้เปิด Bill เพื่อตรวจและกดส่ง SML ด้วยมือตาม flow ปกติของ Shopee การส่ง SML อัตโนมัติเป็นการตั้งค่าระดับร้านและไม่เปลี่ยนจากหน้านี้
                </AlertDescription>
              </Alert>

              <section aria-labelledby="tiktok-bulk-ready">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                  <h2 id="tiktok-bulk-ready" className="text-sm font-semibold">พร้อมสร้างเอกสาร</h2>
                  <Badge variant="outline" className="border-accentStrong/30 bg-primary/10 text-accentStrong">
                    {readyRows.length.toLocaleString('th-TH')} รายการ
                  </Badge>
                </div>
                {readyRows.length === 0 ? (
                  <p className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">ไม่มีรายการที่พร้อมสร้างในรอบนี้</p>
                ) : (
                  <div className="divide-y overflow-hidden rounded-lg border border-border">
                    {readyRows.map((row) => (
                      <div key={`${row.shop_id}:${row.order_id}`} className="flex items-start justify-between gap-3 p-3">
                        <div className="min-w-0">
                          <div className="font-mono text-xs font-medium text-foreground">{row.order_id}</div>
                          <div className="mt-0.5 truncate text-xs text-muted-foreground">{row.shop_name || row.shop_id}</div>
                        </div>
                        <Badge variant="outline" className="shrink-0 border-accentStrong/30 bg-primary/10 text-accentStrong">พร้อม</Badge>
                      </div>
                    ))}
                  </div>
                )}
              </section>

              {preview.skipped.length > 0 && (
                <section aria-labelledby="tiktok-bulk-skipped">
                  <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                    <h2 id="tiktok-bulk-skipped" className="text-sm font-semibold">ยังไม่สร้างในรอบนี้</h2>
                    <Badge variant="outline" className="border-warning/40 bg-warning/10 text-warning">
                      {preview.skipped.length.toLocaleString('th-TH')} รายการ
                    </Badge>
                  </div>
                  <div className="divide-y overflow-hidden rounded-lg border border-border">
                    {preview.skipped.map((row) => (
                      <div key={`${row.shop_id}:${row.order_id}`} className="flex items-start gap-3 p-3">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                        <div className="min-w-0 text-sm">
                          <div className="font-mono text-xs font-medium text-foreground">{row.order_id}</div>
                          <div className="mt-0.5 text-xs text-muted-foreground">{row.message}</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </section>
              )}

              {readyRows.length > 0 && (
                <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-border p-3 text-sm">
                  <Checkbox checked={confirmed} onCheckedChange={(value) => setConfirmed(value === true)} aria-label="ยืนยันการสร้างเอกสาร TikTok Shop" />
                  <span>
                    ฉันยืนยันให้สร้าง Bill {readyRows.length.toLocaleString('th-TH')} รายการใน Nexflow โดยยังไม่ส่งเข้า SML
                  </span>
                </label>
              )}
            </div>
          )}
        </div>

        <DialogFooter className="border-t border-border px-4 py-3 sm:px-6">
          <Button type="button" variant="outline" disabled={creating} onClick={() => onOpenChange(false)}>ยกเลิก</Button>
          <Button type="button" disabled={!canCreate} onClick={onCreate}>
            {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <FilePlus2 className="mr-2 h-4 w-4" />}
            ยืนยันสร้างเอกสาร {readyRows.length > 0 ? readyRows.length.toLocaleString('th-TH') : ''} รายการ
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
