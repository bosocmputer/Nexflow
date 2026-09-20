import { AlertTriangle, CheckCircle2, Loader2 } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

export interface TikTokCancellationPreview {
  status: string
  message: string
  create_enabled: boolean
  review_digest: string
  order_id: string
  sale_sml_doc_no: string
  preview_cancel_sml_doc_no?: string
  destination: string
  doc_format_code: string
  existing?: {
    cancel_sml_doc_no?: string
  }
}

export function TikTokCancellationDialog({
  open,
  loading,
  error,
  preview,
  confirmed,
  creating,
  onOpenChange,
  onConfirmedChange,
  onCreate,
}: {
  open: boolean
  loading: boolean
  error: string
  preview: TikTokCancellationPreview | null
  confirmed: boolean
  creating: boolean
  onOpenChange: (open: boolean) => void
  onConfirmedChange: (confirmed: boolean) => void
  onCreate: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>สร้างเอกสารยกเลิก SML</DialogTitle>
          <DialogDescription>
            สำหรับ TikTok Shop order ที่ยกเลิกหลังส่งใบขายเข้า SML แล้วเท่านั้น ไม่รวมการคืนสินค้า/คืนเงิน
          </DialogDescription>
        </DialogHeader>
        {loading ? (
          <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
            <Loader2 className="mr-2 inline h-4 w-4 animate-spin" />
            กำลังตรวจ Preview จาก SML...
          </div>
        ) : error ? (
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>ยังตรวจเอกสารยกเลิกไม่ได้</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : preview ? (
          <div className="space-y-3">
            {!preview.create_enabled && preview.status !== 'already_exists' && (
              <Alert className="border-warning/40 bg-warning/10">
                <AlertTriangle className="h-4 w-4 text-warning" />
                <AlertTitle>เปิดให้ตรวจ Preview แล้ว แต่ยังปิดการสร้างจริง</AlertTitle>
                <AlertDescription>
                  รอ App Review ผ่านและยืนยัน Webhook cancellation ก่อนเปิด canary สำหรับ AOY ปุ่มนี้จึงไม่เขียนเอกสารเข้า SML ในตอนนี้
                </AlertDescription>
              </Alert>
            )}
            {preview.status === 'already_exists' && (
              <Alert className="border-accentStrong/30 bg-primary/10">
                <CheckCircle2 className="h-4 w-4 text-accentStrong" />
                <AlertTitle>มีเอกสารยกเลิก SML แล้ว</AlertTitle>
                <AlertDescription>{preview.existing?.cancel_sml_doc_no || preview.message}</AlertDescription>
              </Alert>
            )}
            <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
              <div className="grid gap-3 sm:grid-cols-2">
                <CancellationKV label="Order ID" value={preview.order_id} mono />
                <CancellationKV label="ใบขายเดิม" value={preview.sale_sml_doc_no} mono />
                <CancellationKV label="เลขเอกสาร Preview" value={preview.preview_cancel_sml_doc_no || preview.existing?.cancel_sml_doc_no || '—'} mono />
                <CancellationKV label="รูปแบบเอกสาร" value={preview.doc_format_code || '—'} />
              </div>
              <div className="mt-3 border-t border-border pt-3">
                <CancellationKV label="ปลายทาง SML" value={preview.destination || 'ยังไม่ได้ตั้งค่า'} />
              </div>
            </div>
            {preview.status !== 'already_exists' && (
              <label className="flex items-start gap-3 rounded-md border border-border bg-background p-3 text-sm">
                <Checkbox
                  className="mt-0.5"
                  checked={confirmed}
                  disabled={!preview.create_enabled}
                  onCheckedChange={(value) => onConfirmedChange(value === true)}
                />
                <span className="leading-5">
                  ยืนยันว่า TikTok Shop order นี้เป็นสถานะ CANCELLED และต้องสร้างเอกสาร “{preview.destination}” อ้างอิงใบขายเดิม โดยไม่สร้างเอกสารคืนสินค้า/ลดหนี้
                </span>
              </label>
            )}
          </div>
        ) : (
          <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">ยังไม่มี Preview</div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>ปิด</Button>
          {preview && preview.status !== 'already_exists' && (
            <Button
              variant="destructive"
              disabled={!preview.create_enabled || !confirmed || creating}
              title={!preview.create_enabled ? 'รอ App Review และ Webhook cancellation ก่อนเปิด canary' : !confirmed ? 'กรุณาติ๊กยืนยันก่อนสร้าง' : undefined}
              onClick={onCreate}
            >
              {creating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              สร้างเอกสารยกเลิก SML
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CancellationKV({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn('mt-0.5 break-words font-medium text-foreground', mono && 'font-mono text-xs')}>{value || '—'}</div>
    </div>
  )
}
