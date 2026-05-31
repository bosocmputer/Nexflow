import { useState } from 'react'
import { AlertCircle, Check, Copy, RefreshCw } from 'lucide-react'
import dayjs from 'dayjs'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { isSMLReady, smlBlockedMessage } from '@/lib/sml-readiness'
import { cn } from '@/lib/utils'
import type { SMLReadiness } from '@/types'

// Failure detail schema persisted to bills.error_msg by the backend's
// recordFailure helper. Old rows have a plain string instead of JSON;
// the parse-fallback in BillFailureCard handles both shapes.
interface FailureDetail {
  route: string // SaleReserve / SaleOrder / SaleInvoice / PurchaseOrder
  doc_no_attempted: string
  error: string
  occurred_at: string
}

interface Props {
  // Backend wraps the error in JSON; legacy rows are plain strings.
  errorMsg?: string | null
  // Live error from the most recent retry click — shown inline above the
  // persisted failure (most often identical, but they can diverge if the
  // backend stamped a different message than the HTTP body the client saw).
  retryError?: string | null
  regeneratingDocNo?: boolean
  onRegenerateDocNo?: () => Promise<string | null> | string | null | void
  smlReadiness?: SMLReadiness | null
}

function parseFailure(msg: string): FailureDetail | null {
  try {
    const parsed = JSON.parse(msg)
    if (parsed && typeof parsed === 'object' && 'error' in parsed && 'route' in parsed) {
      return parsed as FailureDetail
    }
  } catch {
    /* not JSON — caller falls back to raw string */
  }
  return null
}

const ROUTE_LABEL: Record<string, string> = {
  SaleReserve: 'ใบสั่งจอง (SML 213)',
  SaleOrder: 'ใบสั่งขาย (SML 248)',
  SaleInvoice: 'ขาย -> ขายสินค้าและบริการ (SML 248)',
  PurchaseOrder: 'ซื้อ -> ใบสั่งซื้อ (SML 248)',
}

// BillFailureCard surfaces the *why* of a failed bill in a way an admin can
// (a) understand at a glance and (b) copy verbatim to send to a developer.
// Replaces the previous inline red text under BillHeader which buried
// route + attempted doc_no inside a single string.
function isDuplicateDocNoFailure(text: string) {
  const lower = text.toLowerCase()
  return (
    lower.includes('duplicate_doc_no') ||
    lower.includes('duplicate key') ||
    lower.includes('already exists') ||
    (lower.includes('doc_no') && lower.includes('exists')) ||
    text.includes('มีอยู่')
  )
}

export function BillFailureCard({
  errorMsg,
  retryError,
  regeneratingDocNo = false,
  onRegenerateDocNo,
  smlReadiness,
}: Props) {
  const [copied, setCopied] = useState(false)

  if (!errorMsg && !retryError) return null

  const detail = errorMsg ? parseFailure(errorMsg) : null
  // Raw error string the admin will copy. Prefer parsed.error for clean
  // payload; fall back to whatever string we have otherwise.
  const rawError = detail?.error ?? errorMsg ?? retryError ?? ''
  const route = detail?.route ?? ''
  const docNoAttempted = detail?.doc_no_attempted ?? ''
  const occurredAt = detail?.occurred_at
  const canRegenerateDocNo = Boolean(onRegenerateDocNo && isDuplicateDocNoFailure(rawError))
  const smlReady = isSMLReady(smlReadiness)

  const handleCopy = async () => {
    // Build a multi-line block that's useful for dev triage out-of-context:
    // includes route + doc_no + timestamp so the dev doesn't have to ask.
    const lines = [
      'Nexflow SML failure',
      route ? `Route:    ${route}` : '',
      docNoAttempted ? `Doc no:   ${docNoAttempted}` : '',
      occurredAt ? `When:     ${occurredAt}` : '',
      '',
      rawError,
    ]
      .filter(Boolean)
      .join('\n')
    try {
      await navigator.clipboard.writeText(lines)
      setCopied(true)
      toast.success('คัดลอกข้อความ error แล้ว')
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast.error('คัดลอกไม่สำเร็จ — เลือกข้อความแล้วกด Ctrl+C')
    }
  }

  return (
    <Card className="border-destructive/40 bg-destructive/[0.03]">
      <CardContent className="space-y-3 p-4">
        {/* Header */}
        <div className="flex items-start gap-2.5">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" strokeWidth={2.25} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <h3 className="text-sm font-semibold text-foreground">ส่ง SML ไม่สำเร็จ</h3>
              {route && (
                <Badge
                  variant="outline"
                  className="h-5 border-destructive/30 bg-destructive/5 px-1.5 text-[10px] font-medium text-destructive"
                >
                  {ROUTE_LABEL[route] ?? route}
                </Badge>
              )}
            </div>
            {/* Meta row — sub-context, only render fields that exist */}
            {(docNoAttempted || occurredAt) && (
              <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                {docNoAttempted && (
                  <span>
                    doc_no ที่ลอง:{' '}
                    <span className="font-mono font-medium text-foreground">{docNoAttempted}</span>
                  </span>
                )}
                {occurredAt && (
                  <span>
                    เกิดเมื่อ {dayjs(occurredAt).format('DD MMM YYYY HH:mm:ss')}
                  </span>
                )}
              </div>
            )}
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={handleCopy}
            className="h-7 shrink-0 gap-1 px-2 text-[11px]"
            title="คัดลอกข้อความ error สำหรับส่งให้ทีมดูแลระบบ"
          >
            {copied ? (
              <Check className="h-3 w-3 text-success" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
            {copied ? 'คัดลอกแล้ว' : 'คัดลอก'}
          </Button>
          {canRegenerateDocNo && (
            <Button
              variant="default"
              size="sm"
              onClick={() => onRegenerateDocNo?.()}
              disabled={regeneratingDocNo || !smlReady}
              className="h-7 shrink-0 gap-1 px-2 text-[11px]"
              title={smlReady ? 'ดึงเลข running ล่าสุดจาก SML แล้วออก doc_no ใหม่ให้บิลนี้' : smlBlockedMessage(smlReadiness)}
            >
              <RefreshCw className={cn('h-3 w-3', regeneratingDocNo && 'animate-spin')} />
              ออกเลขใหม่
            </Button>
          )}
        </div>

        {/* Error body — terminal-style, monospace, word-wrap on long lines */}
        <pre
          className={cn(
            'whitespace-pre-wrap break-words rounded-md',
            'border border-destructive/20 bg-muted/40 px-3 py-2.5',
            'font-mono text-[12px] leading-relaxed text-foreground',
          )}
        >
          {rawError || '(no error detail)'}
        </pre>

        {/* Live retry-click error, only when different from the persisted one */}
        {retryError && retryError !== rawError && (
          <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-warning">
            <span className="font-medium">retry ล่าสุด:</span> {retryError}
          </div>
        )}

        {/* Footer hint — sets expectations: copy → fix → retry */}
        <p className="text-[11px] text-muted-foreground">
          {canRegenerateDocNo
            ? 'เลขเอกสารนี้ซ้ำใน SML กดออกเลขใหม่ แล้วส่ง SML อีกครั้ง'
            : (
              <>
                ส่ง error นี้ให้ทีมดูแลระบบเพื่อแก้ไข แล้วกด{' '}
                <span className="font-medium text-foreground">Retry</span> อีกครั้งเมื่อแก้แล้ว
              </>
            )}
        </p>
      </CardContent>
    </Card>
  )
}
