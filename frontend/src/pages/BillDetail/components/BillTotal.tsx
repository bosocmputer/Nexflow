import { AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { isSMLReady, smlBlockedMessage } from '@/lib/sml-readiness'
import type { Bill, SMLReadiness } from '@/types'
import { issueLabel, type ValidationResult } from '../utils/validation'

interface Props {
  bill: Bill
  total: number
  retrying: boolean
  onRetry: () => void
  // Frontend-side validation against backend retry rules. When canSend=false
  // the Send button is disabled + a warning card lists the offending issues.
  // Each issue can be clicked to scroll/highlight the first row that hit it.
  validation: ValidationResult
  onJumpToItem: (itemId: string | null) => void
  // expectedRoute / expectedDocFormat — preview of what'll happen when admin
  // clicks Send. Surfaces the SML route + doc_no pattern BEFORE the round-trip
  // so admins can spot misconfigured channels (e.g. shopee bill routed to
  // sale_reserve because endpoint string doesn't match the keywords).
  expectedRoute?: string
  expectedEndpoint?: string
  expectedDocFormat?: string
  smlReadiness?: SMLReadiness | null
  smlReadinessLoading?: boolean
}

export function BillTotal({
  bill,
  validation,
  onJumpToItem,
  smlReadiness,
}: Props) {
  const canShowSendButton =
    bill.status === 'failed' ||
    bill.status === 'pending' ||
    bill.status === 'needs_review'
  const smlReady = isSMLReady(smlReadiness)

  if (!canShowSendButton || (smlReady && validation.canSend)) return null

  return (
    <div className="space-y-2.5">
      {/* Validation warning card — only renders when there are issues to
          fix. Each issue links to the first offending row. Sits between
          the header summary and the items table so admin sees what to do
          before they look down at items. */}
      {canShowSendButton && !smlReady && (
        <div className="rounded-md border border-warning/40 bg-warning/[0.07] px-3 py-2">
          <div className="flex items-start gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" strokeWidth={2.25} />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-semibold text-foreground">ยังส่ง SML ไม่ได้: ฐานข้อมูลร้านยังไม่พร้อม</div>
              <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                {smlBlockedMessage(smlReadiness)} เปิดเครื่อง SML/Postgres ของร้านนี้ แล้วกดตรวจอีกครั้งบนแถบแจ้งเตือนด้านบน
              </p>
            </div>
          </div>
        </div>
      )}

      {canShowSendButton && !validation.canSend && (
        <div
          className={cn(
            'rounded-md border border-warning/40 bg-warning/[0.06] px-3 py-2',
          )}
        >
          <div className="flex items-start gap-2">
            <AlertTriangle
              className="mt-0.5 h-4 w-4 shrink-0 text-warning"
              strokeWidth={2.25}
            />
            <div className="min-w-0 flex-1 space-y-1.5">
              <div className="text-sm font-semibold text-foreground">
                ยังส่ง SML ไม่ได้: พบ {validation.issues.length}{' '}
                ปัญหาที่ต้องแก้
              </div>
              <ul className="space-y-0.5 text-[13px]">
                {validation.issues.map((issue) => (
                  <li
                    key={issue.kind}
                    className="flex items-baseline gap-1.5"
                  >
                    <span className="text-muted-foreground/60">•</span>
                    <span className="flex-1 text-foreground">
                      <span className="font-medium tabular-nums">
                        {issue.count}
                      </span>{' '}
                      {issue.kind === 'no_items'
                        ? issueLabel(issue.kind)
                        : `รายการ${issueLabel(issue.kind)}`}
                    </span>
                    {issue.firstItemId && (
                      <button
                        type="button"
                        onClick={() => onJumpToItem(issue.firstItemId)}
                        className="shrink-0 rounded-md bg-primary/10 px-2 py-1 text-[11px] font-medium text-link hover:bg-primary/15"
                      >
                        ไปแก้รายการ
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
