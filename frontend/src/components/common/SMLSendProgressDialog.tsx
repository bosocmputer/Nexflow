import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, Loader2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

export type SMLSendProgressStatus = 'sending' | 'success' | 'warning' | 'error'

interface Props {
  open: boolean
  status: SMLSendProgressStatus
  docNo?: string | null
  error?: string | null
  onClose: () => void
}

// One centered result surface for every irreversible SML write.  The caller
// owns polling its durable job/run; this component only presents that state.
export function SMLSendProgressDialog({
  open,
  status,
  docNo,
  error,
  onClose,
}: Props) {
  const [showSlowHint, setShowSlowHint] = useState(false)
  const sending = status === 'sending'
  const needsReview = status === 'warning'

  useEffect(() => {
    if (!open || !sending) {
      setShowSlowHint(false)
      return
    }
    const timer = window.setTimeout(() => setShowSlowHint(true), 8000)
    return () => window.clearTimeout(timer)
  }, [open, sending])

  const title =
    status === 'success'
      ? 'ส่งเข้า SML สำเร็จ'
      : needsReview
        ? 'ต้องตรวจผลใน SML'
        : status === 'error'
          ? 'ส่งเข้า SML ไม่สำเร็จ'
          : 'กำลังส่งเอกสารเข้า SML'

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!nextOpen && !sending) onClose()
    }}>
      <DialogContent
        className="sm:max-w-md [&>button]:hidden"
        onEscapeKeyDown={(event) => {
          if (sending) event.preventDefault()
        }}
        onPointerDownOutside={(event) => {
          if (sending) event.preventDefault()
        }}
      >
        <DialogHeader className="items-center text-center">
          <div className={[
            'mb-1 flex h-12 w-12 items-center justify-center rounded-full',
            status === 'success'
              ? 'bg-success/10 text-success'
              : needsReview
                ? 'bg-warning/10 text-warning'
                : status === 'error'
                  ? 'bg-destructive/10 text-destructive'
                  : 'bg-info/10 text-info',
          ].join(' ')}>
            {status === 'success' ? (
              <CheckCircle2 className="h-6 w-6" />
            ) : status === 'error' || needsReview ? (
              <AlertTriangle className="h-6 w-6" />
            ) : (
              <Loader2 className="h-6 w-6 animate-spin" />
            )}
          </div>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {status === 'success'
              ? 'ระบบบันทึกผลการส่งและอัปเดตสถานะเอกสารแล้ว'
              : needsReview
                ? 'ระบบไม่สร้างเอกสารซ้ำ กรุณาตรวจผลใน SML ก่อนดำเนินการต่อ'
                : status === 'error'
                  ? 'ระบบยังเก็บรายการไว้ให้ตรวจหรือทดลองส่งใหม่ได้'
                  : 'กรุณารอสักครู่ ระบบกำลังส่งข้อมูลไปยัง SML และตรวจผลล่าสุดกลับมา'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {(status === 'success' || needsReview) && docNo?.trim() && (
            <div className={[
              'rounded-md border px-3 py-2 text-sm',
              status === 'success'
                ? 'border-success/25 bg-success/[0.06]'
                : 'border-warning/25 bg-warning/[0.06]',
            ].join(' ')}>
              <div className="text-xs font-medium text-muted-foreground">เลขเอกสาร SML</div>
              <div className="mt-1 font-mono text-lg font-semibold text-foreground">{docNo.trim()}</div>
            </div>
          )}

          {(status === 'error' || needsReview) && (
            <div className={[
              'rounded-md border px-3 py-2 text-sm',
              needsReview
                ? 'border-warning/25 bg-warning/[0.06] text-warning'
                : 'border-destructive/25 bg-destructive/[0.06] text-destructive',
            ].join(' ')}>
              {error?.trim() || (needsReview
                ? 'ไม่พบผลที่ยืนยันได้จาก SML กรุณาตรวจเอกสารก่อนลองใหม่'
                : 'ส่ง SML ไม่สำเร็จ กรุณาตรวจข้อมูลแล้วลองใหม่อีกครั้ง')}
            </div>
          )}

          {sending && (
            <div className="rounded-md border border-info/25 bg-info/[0.04] px-3 py-2 text-sm text-muted-foreground">
              <div className="font-medium text-foreground">โปรดรอจนกว่าระบบจะแสดงผลลัพธ์</div>
              <div className="mt-0.5 text-xs">ระหว่างนี้ระบบล็อกการส่งรายการเดิมเพื่อป้องกันเอกสารซ้ำ</div>
              {showSlowHint && (
                <div className="mt-2 rounded-md border border-warning/30 bg-warning/[0.08] px-2.5 py-1.5 text-xs text-warning">
                  SML อาจใช้เวลานานกว่าปกติ กรุณารอสักครู่และอย่าเพิ่งกดส่งซ้ำ
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter className="sm:justify-center">
          <Button type="button" onClick={onClose} disabled={sending}>
            {status === 'success' ? 'ปิด' : needsReview ? 'รับทราบ' : status === 'error' ? 'กลับไปตรวจ' : 'กำลังส่ง...'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
