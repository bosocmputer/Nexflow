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

export type SMLSendProgressStatus = 'sending' | 'success' | 'error'

interface Props {
  open: boolean
  status: SMLSendProgressStatus
  docNo?: string | null
  error?: string | null
  onClose: () => void
}

export function SMLSendProgressDialog({
  open,
  status,
  docNo,
  error,
  onClose,
}: Props) {
  const [showSlowHint, setShowSlowHint] = useState(false)
  const sending = status === 'sending'

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
              : status === 'error'
                ? 'bg-destructive/10 text-destructive'
                : 'bg-info/10 text-info',
          ].join(' ')}>
            {status === 'success' ? (
              <CheckCircle2 className="h-6 w-6" />
            ) : status === 'error' ? (
              <AlertTriangle className="h-6 w-6" />
            ) : (
              <Loader2 className="h-6 w-6 animate-spin" />
            )}
          </div>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {status === 'success'
              ? 'ระบบบันทึกผลการส่งและอัปเดตสถานะบิลแล้ว'
              : status === 'error'
                ? 'ระบบยังเก็บบิลไว้ให้แก้ไขหรือลองส่งใหม่ได้'
                : 'กรุณารอสักครู่ ระบบกำลังส่งข้อมูลไปยัง SML และดึงผลล่าสุดกลับมา'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {status === 'success' && (
            <div className="rounded-md border border-success/25 bg-success/[0.06] px-3 py-2 text-sm">
              <div className="text-xs font-medium text-muted-foreground">เลขเอกสาร SML</div>
              <div className="mt-1 font-mono text-lg font-semibold text-foreground">
                {docNo?.trim() || 'ส่งสำเร็จ แต่ยังไม่พบเลขเอกสารล่าสุด'}
              </div>
            </div>
          )}

          {status === 'error' && (
            <div className="rounded-md border border-destructive/25 bg-destructive/[0.06] px-3 py-2 text-sm text-destructive">
              {error?.trim() || 'ส่ง SML ไม่สำเร็จ กรุณาตรวจข้อมูลแล้วลองใหม่อีกครั้ง'}
            </div>
          )}

          {sending && (
            <div className="rounded-md border border-info/25 bg-info/[0.04] px-3 py-2 text-sm text-muted-foreground">
              <div className="font-medium text-foreground">โปรดรอจนกว่าระบบจะแสดงผลลัพธ์</div>
              <div className="mt-0.5 text-xs">
                ระหว่างนี้ระบบจะล็อกปุ่มส่งเพื่อป้องกันการส่งซ้ำ
              </div>
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
            {status === 'success' ? 'ปิด' : status === 'error' ? 'กลับไปแก้ไข' : 'กำลังส่ง...'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
