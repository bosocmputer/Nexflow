import { AlertTriangle, RefreshCw, Send } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { isSMLReady, smlBlockedMessage } from '@/lib/sml-readiness'
import { isShopeePurchaseBill, isShopeeSalesBill, money, shopeeGoodsTotal, shopeePayableTotal } from '@/lib/shopeeBill'
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

const ROUTE_LABEL: Record<string, string> = {
  sale_reserve: 'ใบสั่งจอง',
  saleorder: 'ใบสั่งขาย',
  saleinvoice: 'ขาย -> ขายสินค้าและบริการ',
  purchaseorder: 'ซื้อ -> ใบสั่งซื้อ',
}

export function BillTotal({
  bill,
  total,
  retrying,
  onRetry,
  validation,
  onJumpToItem,
  expectedRoute,
  expectedEndpoint,
  expectedDocFormat,
  smlReadiness,
  smlReadinessLoading = false,
}: Props) {
  const canShowSendButton =
    bill.status === 'failed' ||
    bill.status === 'pending' ||
    bill.status === 'needs_review'
  const isPurchase = bill.bill_type === 'purchase'
  const isShopeePurchase = isShopeePurchaseBill(bill)
  const isShopeeSale = isShopeeSalesBill(bill)
  const isFailed = bill.status === 'failed'
  const goodsTotal = shopeeGoodsTotal(bill) ?? total
  const payableTotal = shopeePayableTotal(bill)

  // Send is enabled only when validation passes AND we're not mid-retry.
  // The disabled state is communicated by both the button's :disabled state
  // and the warning card above (which is the "why" — the button alone
  // wouldn't tell the admin what to fix).
  const smlReady = isSMLReady(smlReadiness)
  const enabled = validation.canSend && smlReady && !retrying
  const readyText = !smlReady
    ? (smlReadinessLoading ? 'กำลังตรวจสถานะ SML ของร้านนี้' : 'SML ของร้านนี้ยังไม่พร้อม กรุณาตรวจการเชื่อมต่อก่อนส่ง')
    : validation.canSend
    ? 'รายการครบแล้ว พร้อมเลือกผู้ขาย/คลัง/ภาษีและส่งเข้า SML'
    : `ยังต้องแก้ ${validation.issues.length} จุดก่อนส่งเข้า SML`

  const buttonLabel = retrying
    ? 'กำลังส่ง...'
    : isFailed
      ? `ลองส่งใหม่${isPurchase ? 'ไป SML' : ''}`
      : `ส่งเข้า SML${isPurchase ? ' (บิลซื้อ)' : ''}`

  return (
    <Card className="rounded-xl border-border/70 shadow-sm">
      <CardContent className="space-y-2.5 px-5 py-3">
        {/* Top row — total + send button */}
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div>
            <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {isShopeePurchase || isShopeeSale ? 'ยอดที่ต้องชำระทั้งหมด' : 'ยอดรวมทั้งหมด'}
            </div>
            <div className="mt-0.5 text-xl font-bold tabular-nums tracking-tight">
              {money(isShopeePurchase || isShopeeSale ? payableTotal ?? total : total)}
            </div>
            {isShopeePurchase && (
              <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                <span>ยอดสินค้า {money(goodsTotal)}</span>
                {payableTotal != null && payableTotal !== goodsTotal && (
                  <span>รวมส่วนลด/ค่าส่งแล้ว</span>
                )}
              </div>
            )}
            {canShowSendButton && (
              <p className={cn(
                'mt-0.5 text-xs',
                validation.canSend && smlReady ? 'text-success' : 'text-warning',
              )}>
                {readyText}
              </p>
            )}
          </div>

          {canShowSendButton && (
            <div className="flex flex-col items-end gap-1.5">
              <TooltipProvider delayDuration={150}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    {/* Wrap button in a span so a disabled button still
                        receives hover events (raw <button disabled> swallows
                        them, which means the tooltip wouldn't fire on the
                        very state we most need to explain). */}
                    <span className={!enabled ? 'cursor-not-allowed' : ''}>
                      <Button
                        type="button"
                        onClick={onRetry}
                        disabled={!enabled}
                        variant={isFailed ? 'destructive' : 'default'}
                        className="h-10 shrink-0 gap-2 rounded-lg px-4"
                      >
                        {retrying ? (
                          <RefreshCw className="h-4 w-4 animate-spin" />
                        ) : isFailed ? (
                          <RefreshCw className="h-4 w-4" />
                        ) : (
                          <Send className="h-4 w-4" />
                        )}
                        {buttonLabel}
                      </Button>
                    </span>
                  </TooltipTrigger>
                  {/* Tooltip only renders content when the button is disabled
                      because of validation — when retrying, the button text
                      already explains itself ("กำลังส่ง..."). */}
                  {(!validation.canSend || !smlReady) && (
                    <TooltipContent side="left" className="max-w-xs">
                      {!smlReady
                        ? smlBlockedMessage(smlReadiness)
                        : `ยังส่งไม่ได้ — พบ ${validation.issues.length} ปัญหา · ตรวจรหัสสินค้า การยืนยัน หน่วย จำนวน และราคา`}
                    </TooltipContent>
                  )}
                </Tooltip>
              </TooltipProvider>

              {/* Route preview — always visible when send area is shown so
                  admin can see the routing even before validation passes.
                  Dimmed when button is disabled to signal "preview only". */}
              {canShowSendButton && expectedRoute && (
                <div className={cn("max-w-[340px] text-right text-[10px] leading-4 tabular-nums text-muted-foreground", !enabled && "opacity-50")}>
                  ปลายทาง SML:{' '}
                  <span className="font-medium text-foreground">
                    {ROUTE_LABEL[expectedRoute] ?? expectedRoute}
                  </span>
                  {expectedDocFormat && (
                    <>
                      {' '}· รหัสเอกสาร{' '}
                      <code className="rounded bg-muted px-1 py-0.5 font-mono">
                        {expectedDocFormat}
                      </code>
                    </>
                  )}
                  {expectedEndpoint && expectedEndpoint.startsWith('http') && (
                    <div
                      className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground/70"
                      title={expectedEndpoint}
                    >
                      {expectedEndpoint}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Validation warning card — only renders when there are issues to
            fix. Each issue links to the first offending row. Sits between
            the total + button summary and the items table so admin sees
            "what to do" before they look down at items. */}
        {canShowSendButton && !smlReady && (
          <div className="rounded-md border border-warning/40 bg-warning/[0.07] px-3 py-2">
            <div className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" strokeWidth={2.25} />
              <div className="min-w-0 flex-1">
                <div className="text-sm font-semibold text-foreground">ยังส่ง SML ไม่ได้ — ฐานข้อมูลร้านยังไม่พร้อม</div>
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
                  ยังส่ง SML ไม่ได้ — พบ {validation.issues.length}{' '}
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
                          className="shrink-0 rounded-md bg-primary/10 px-2 py-1 text-[11px] font-medium text-primary hover:bg-primary/15"
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
      </CardContent>
    </Card>
  )
}
