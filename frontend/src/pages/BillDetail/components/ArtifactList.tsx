import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Download, ExternalLink, Eye, History, Paperclip, Printer, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'
import dayjs from 'dayjs'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useArtifacts, openArtifact, printArtifact, recordArtifactPrint } from '../hooks/useArtifacts'
import type { BillArtifact } from '../hooks/useArtifacts'
import { KIND_META, fmtSize, isUserVisibleArtifact } from '../utils/formatters'
import api from '@/api/client'
import type { BillEmailGroup, BillEmailRelatedBill, EmailPrintEvent } from '@/types'

interface Props {
  billId: string
  emailGroup?: BillEmailGroup | null
}

// EmailPreviewModal renders HTML email content in a sandboxed iframe so the
// browser treats it as a rendered email (layout, images, Thai text) instead of
// a raw text dump in a new tab.
function EmailPreviewModal({
  billId,
  artId,
  filename,
  displayName,
  emailGroup,
  onPrinted,
  onClose,
}: {
  billId: string
  artId: string
  filename: string
  displayName: string
  emailGroup?: BillEmailGroup | null
  onPrinted: (artId: string, filename: string) => Promise<void>
  onClose: () => void
}) {
  const [src, setSrc] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  // Fetch once on mount — inject CSS to strip body margin and force all links
  // to open in a new tab (Shopee emails embed <a> without target="_blank").
  useEffect(() => {
    let alive = true
    let objectURL = ''
    api
      .get(`/api/bills/${billId}/artifacts/${artId}/preview`, { responseType: 'blob' })
      .then((res) => {
        if (!alive) return
        const ct = (res.headers['content-type'] ?? '').toString() || 'text/html; charset=utf-8'
        return res.data.text().then((html: string) => {
          // Reset body margin so the email starts at the top of the iframe,
          // and patch every <a> to open in a new tab.
          const resetCss = `<style>*{box-sizing:border-box}html,body{margin:0!important;padding:0!important}img{display:block;max-width:100%}table{margin:0!important}</style>`
          const patched = html
            .replace(/<head([^>]*)>/i, `<head$1>${resetCss}`)
            .replace(/<a\s/gi, '<a target="_blank" rel="noopener noreferrer" ')
          const blob = new Blob([patched], { type: ct })
          objectURL = URL.createObjectURL(blob)
          if (alive) setSrc(objectURL)
        })
      })
      .catch(() => {
        if (alive) toast.error('เปิดตัวอย่างอีเมลไม่สำเร็จ')
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [artId, billId])

  const handlePrint = async () => {
    await onPrinted(artId, filename)
  }

  const handleClose = () => {
    onClose()
  }

  const duplicateNote = emailDuplicateNote(emailGroup)

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={handleClose}
    >
      <div
        className="relative flex h-[90vh] w-full max-w-4xl flex-col rounded-lg bg-white shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b px-4 py-2">
          <div className="min-w-0">
            <span className="block truncate text-sm font-medium text-foreground">{displayName}</span>
            <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">{filename}</span>
            {duplicateNote && (
              <span className="mt-0.5 block truncate text-xs text-warning">{duplicateNote}</span>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5"
              onClick={handlePrint}
            >
              <Printer className="h-3.5 w-3.5" />
              พิมพ์
            </Button>
            <button
              type="button"
              onClick={handleClose}
              title="ปิด"
              className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>
        <div className="flex-1 overflow-hidden">
          {loading && (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              กำลังโหลด...
            </div>
          )}
          {src && (
            <iframe
              src={src}
              title={filename}
              className="h-full w-full border-0"
              sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
              referrerPolicy="no-referrer"
            />
          )}
        </div>
      </div>
    </div>,
    document.body
  )
}

export function ArtifactList({ billId, emailGroup }: Props) {
  const { items, loading } = useArtifacts(billId)
  const [previewArt, setPreviewArt] = useState<{ id: string; filename: string; contentType: string; displayName: string } | null>(null)
  const [printEvents, setPrintEvents] = useState<EmailPrintEvent[]>(emailGroup?.print_events ?? [])

  useEffect(() => {
    setPrintEvents(emailGroup?.print_events ?? [])
  }, [emailGroup?.message_id, emailGroup?.print_events])

  const handlePrintArtifact = async (artId: string, filename: string) => {
    try {
      await printArtifact(billId, artId, filename)
    } catch (err) {
      console.error('artifact print failed', err)
      toast.error('พิมพ์อีเมลไม่สำเร็จ')
      return
    }

    try {
      const event = await recordArtifactPrint(billId, artId)
      setPrintEvents((prev) => [event, ...prev.filter((p) => p.id !== event.id)])
      toast.success('บันทึกประวัติการพิมพ์แล้ว')
    } catch (err) {
      console.error('record artifact print failed', err)
      toast.warning('เปิดหน้าพิมพ์แล้ว แต่บันทึกประวัติการพิมพ์ไม่สำเร็จ')
    }
  }

  if (loading) return null

  const visibleItems = items.filter((a) => isUserVisibleArtifact(a.kind))

  if (visibleItems.length === 0) {
    return (
      <Card className="rounded-2xl border-border/70 shadow-sm">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Paperclip className="h-4 w-4 text-muted-foreground" />
            หลักฐานต้นฉบับ (0)
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0">
          <p className="text-xs text-muted-foreground">
            ไม่มีไฟล์หลักฐานสำหรับแสดง
          </p>
        </CardContent>
      </Card>
    )
  }

  const duplicateNote = emailDuplicateNote(emailGroup)

  return (
    <>
      <Card className="rounded-2xl border-border/70 shadow-sm">
        <CardHeader className="pb-3">
          <div>
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Paperclip className="h-4 w-4 text-muted-foreground" />
              หลักฐานต้นฉบับ ({visibleItems.length})
            </CardTitle>
            <p className="mt-1 text-xs text-muted-foreground">
              เปิดดูเฉพาะเมื่อต้องย้อนตรวจหลักฐานต้นฉบับ
            </p>
          </div>
        </CardHeader>
        <CardContent className="space-y-3 pt-0">
          <EmailGroupContext
            billId={billId}
            emailGroup={emailGroup}
            printEvents={printEvents}
          />

          <div className="space-y-1">
            {visibleItems.map((a) => {
              const meta = KIND_META[a.kind] ?? { icon: '', label: a.kind, desc: '' }
              const display = artifactDisplay(a, meta)
              const ct = a.content_type ?? ''
              const isHtml = ct.startsWith('text/html') || a.kind === 'email_html' || a.kind === 'email_text'
              const isPrintableEmail = a.kind === 'email_html' || a.kind === 'email_text'
              const previewable =
                ct === 'application/pdf' ||
                ct.startsWith('image/') ||
                ct.startsWith('text/') ||
                ct === 'application/json'

              const handlePreview = () => {
                if (isHtml) {
                  setPreviewArt({ id: a.id, filename: a.filename, contentType: ct, displayName: display.label })
                } else {
                  openArtifact(billId, a.id, a.filename, 'preview')
                }
              }

              return (
                <div
                  key={a.id}
                  className="flex items-start gap-3 border-b border-border/50 py-3 last:border-0"
                >
                  <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                    <Paperclip className="h-4 w-4" />
                  </span>
                  <div className="flex-1 min-w-0">
                    <div className="font-medium text-sm break-words">{display.label}</div>
                    {display.desc && <div className="mt-0.5 line-clamp-2 text-xs leading-snug text-muted-foreground">{display.desc}</div>}
                    {isPrintableEmail && duplicateNote && (
                      <div className="mt-1 rounded-md bg-warning/10 px-2 py-1 text-xs leading-snug text-warning">
                        {duplicateNote}
                      </div>
                    )}
                    <div className="mt-1 font-mono text-[11px] text-muted-foreground/70">
                      {a.filename} · {fmtSize(a.size_bytes)} ·{' '}
                      {dayjs(a.created_at).format('DD/MM/YY HH:mm')}
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    {previewable && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-8 gap-1.5"
                        title={a.sha256 ? `SHA256: ${a.sha256.slice(0, 16)}…` : ''}
                        onClick={handlePreview}
                      >
                        <Eye className="h-3.5 w-3.5" />
                        ดู
                      </Button>
                    )}
                    {isPrintableEmail && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-8 gap-1.5"
                        title={a.sha256 ? `SHA256: ${a.sha256.slice(0, 16)}…` : ''}
                        onClick={() => handlePrintArtifact(a.id, a.filename)}
                      >
                        <Printer className="h-3.5 w-3.5" />
                        พิมพ์
                      </Button>
                    )}
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-8 gap-1.5"
                      title={a.sha256 ? `SHA256: ${a.sha256.slice(0, 16)}…` : ''}
                      onClick={() => openArtifact(billId, a.id, a.filename, 'download')}
                    >
                      <Download className="h-3.5 w-3.5" />
                      ดาวน์โหลด
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        </CardContent>
      </Card>

      {previewArt && (
        <EmailPreviewModal
          billId={billId}
          artId={previewArt.id}
          filename={previewArt.filename}
          displayName={previewArt.displayName}
          emailGroup={emailGroup}
          onPrinted={handlePrintArtifact}
          onClose={() => setPreviewArt(null)}
        />
      )}
    </>
  )
}

function artifactDisplay(
  artifact: BillArtifact,
  meta: { icon: string; label: string; desc: string },
): { label: string; desc: string } {
  if (artifact.kind !== 'email_html' && artifact.kind !== 'email_text') {
    return { label: meta.label, desc: meta.desc }
  }

  const subject = metaString(artifact.source_meta?.subject)
  const eventType = metaString(artifact.source_meta?.event_type)

  return {
    label: emailEvidenceLabel(subject, eventType),
    desc: subject || meta.desc,
  }
}

function emailEvidenceLabel(subject: string, eventType: string): string {
  if (eventType === 'payment_confirmed' || subject.includes('ยืนยันการชำระเงิน')) {
    return 'อีเมลยืนยันการชำระเงิน'
  }
  if (eventType === 'shipped' || subject.includes('ถูกจัดส่งแล้ว')) {
    return 'อีเมลแจ้งจัดส่ง'
  }
  return 'อีเมลต้นฉบับ'
}

function metaString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function EmailGroupContext({
  billId,
  emailGroup,
  printEvents,
}: {
  billId: string
  emailGroup?: BillEmailGroup | null
  printEvents: EmailPrintEvent[]
}) {
  if (!emailGroup?.message_id) return null

  const related = emailGroup.related_bills ?? []
  const showRelated = related.length > 1
  const showHistory = emailGroup.has_printable_email
  if (!showRelated && !showHistory) return null

  return (
    <div className="space-y-3 border-b border-border/50 pb-3">
      {showRelated && (
        <div>
          <div className="mb-1.5 flex items-center justify-between gap-2">
            <div className="text-xs font-medium text-foreground">
              บิลอื่นจาก Email #{emailGroup.group_key}
            </div>
            <span className="text-[11px] text-muted-foreground">
              {related.length.toLocaleString('th-TH')} คำสั่งซื้อ
            </span>
          </div>
          <div className="max-h-32 space-y-1 overflow-y-auto pr-1">
            {related.map((b) => (
              <Link
                key={b.id}
                to={billPath(b)}
                className={`flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-muted ${
                  b.id === billId ? 'bg-info/10 text-info' : 'text-muted-foreground'
                }`}
              >
                <span className="min-w-0 truncate">
                  <span className="font-mono text-foreground">{b.order_id || b.id.slice(0, 8)}</span>
                  {b.party_name && <span> · {b.party_name}</span>}
                  {b.id === billId && <span className="ml-1 font-medium">(บิลนี้)</span>}
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  <span className="tabular-nums">{formatMoney(b.total_amount ?? 0)}</span>
                  <ExternalLink className="h-3 w-3" />
                </span>
              </Link>
            ))}
          </div>
        </div>
      )}

      {showHistory && (
        <div>
          <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-foreground">
            <History className="h-3.5 w-3.5 text-muted-foreground" />
            ประวัติการพิมพ์เมลต้นฉบับ
          </div>
          {printEvents.length > 0 ? (
            <div className="space-y-1 text-xs text-muted-foreground">
              {printEvents.slice(0, 5).map((event) => (
                <div key={event.id} className="flex items-center justify-between gap-2">
                  <span className="min-w-0 truncate">
                    {event.requested_by_name || event.requested_by_email || 'ผู้ใช้ระบบ'}
                  </span>
                  <span className="shrink-0 tabular-nums">
                    {dayjs(event.created_at).format('DD/MM/YY HH:mm')}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <div className="text-xs text-muted-foreground">
              ยังไม่มีประวัติการพิมพ์สำหรับเมลฉบับนี้
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function emailDuplicateNote(emailGroup?: BillEmailGroup | null): string {
  const count = emailGroup?.order_count ?? 0
  if (count <= 1) return ''
  return `เมลฉบับนี้สร้าง ${count.toLocaleString('th-TH')} คำสั่งซื้อ การพิมพ์จากหลายบิลจะได้เอกสารซ้ำกัน`
}

function billPath(b: BillEmailRelatedBill): string {
  if (b.bill_type !== 'sale') return `/bills/${b.id}`
  if (b.document_route === 'saleinvoice') return `/sale-invoices/${b.id}`
  return `/sales-orders/${b.id}`
}

function formatMoney(value: number): string {
  return new Intl.NumberFormat('th-TH', {
    style: 'currency',
    currency: 'THB',
    maximumFractionDigits: 0,
  }).format(value)
}
