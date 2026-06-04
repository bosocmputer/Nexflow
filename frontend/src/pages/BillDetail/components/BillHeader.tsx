import { ArrowLeft, RefreshCw, Send } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import dayjs from 'dayjs'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import BillStatusBadge from '@/components/BillStatusBadge'
import type { Bill, SMLReadiness } from '@/types'
import { isSMLReady, smlBlockedMessage } from '@/lib/sml-readiness'
import { cn } from '@/lib/utils'
import {
  isShopeePurchaseBill,
  isShopeeSalesBill,
  money,
  rawNumber,
  rawString,
  shopeeCoinAmount,
  shopeeGoodsTotal,
  shopeeOrderID,
  shopeePayableTotal,
} from '@/lib/shopeeBill'
import { SOURCE_LABELS } from '../utils/formatters'
import { hasInvalidPrice, type ValidationResult } from '../utils/validation'

interface Props {
  bill: Bill
  total: number
  retrying: boolean
  onRetry: () => void
  validation: ValidationResult
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

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[11px] font-medium text-muted-foreground">
        {label}
      </span>
      <span className="text-[13px] leading-5 text-foreground">{value || '—'}</span>
    </div>
  )
}

export function BillHeader({
  bill,
  total,
  retrying,
  onRetry,
  validation,
  expectedRoute,
  expectedEndpoint,
  expectedDocFormat,
  smlReadiness,
  smlReadinessLoading = false,
}: Props) {
  const navigate = useNavigate()
  const rawData = bill.raw_data as Record<string, unknown> | null
  const isPurchase = bill.bill_type === 'purchase'
  const isShopeePurchase = isShopeePurchaseBill(bill)
  const isShopeeSale = isShopeeSalesBill(bill)
  const isFailed = bill.status === 'failed'
  const canShowSendButton =
    bill.status === 'failed' ||
    bill.status === 'pending' ||
    bill.status === 'needs_review'
  const saleRoute = bill.document_route || bill.preview?.route || ''
  const saleDestinationLabel =
    saleRoute === 'saleinvoice' ? 'ขาย -> ขายสินค้าและบริการ' : 'ขาย -> ใบสั่งขาย'
  const saleDestinationTitle =
    saleRoute === 'saleinvoice' ? 'Sale Invoice' : 'Sales Order'
  const orderID = shopeeOrderID(rawData)
  const orderDateTime = rawString(rawData, 'order_datetime') || rawString(rawData, 'doc_date')
  const sellerName = rawString(rawData, 'seller_name')
  const buyerName = rawString(rawData, 'customer_name') || rawString(rawData, 'buyer_username')
  const paymentChannel = rawString(rawData, 'payment_channel')
  const trackingNo = rawString(rawData, 'tracking_no')
  const shopeeShopID = rawString(rawData, 'shopee_shop_id')
  const shopeeShopLabel = rawString(rawData, 'shopee_shop_label')
  const docDate = (rawData?.doc_date as string) || ''
  const rawItemCount = rawNumber(rawData, 'item_count')
  const itemCount = bill.items?.length ?? 0
  const issueCount = (bill.items ?? []).filter((item) => {
    return !item.item_code || !item.unit_code || !item.qty || item.qty <= 0 || hasInvalidPrice(item)
  }).length
  const goodsTotal = shopeeGoodsTotal(bill) ?? total
  const payableTotal = shopeePayableTotal(bill)
  const coinAmount = shopeeCoinAmount(bill)
  const displayTotal = isShopeePurchase || isShopeeSale ? payableTotal ?? total : total
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
    <div className="space-y-2">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="gap-1.5 text-muted-foreground hover:text-foreground -ml-2 h-8"
        onClick={() => navigate(-1)}
      >
        <ArrowLeft className="h-4 w-4" />
        กลับ
      </Button>

      <Card className="overflow-hidden rounded-lg border-border/70 shadow-sm">
        <CardHeader className="border-b bg-card px-5 py-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="min-w-0 space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="font-mono text-xl font-bold tracking-tight sm:text-2xl">
                  {bill.sml_doc_no ?? bill.id.slice(0, 8)}
                </h2>
                {isPurchase && (
                  <Badge
                    variant="secondary"
                    className="bg-warning/15 text-warning hover:bg-warning/20"
                    title="Purchase Order"
                  >
                    {'ซื้อ -> ใบสั่งซื้อ'}
                  </Badge>
                )}
                {isShopeeSale && (
                  <Badge
                    variant="secondary"
                    className="bg-primary/10 text-accent-strong hover:bg-primary/15"
                    title={saleDestinationTitle}
                  >
                    {saleDestinationLabel}
                  </Badge>
                )}
                <BillStatusBadge status={bill.status} />
              </div>
              {canShowSendButton && (
                <p className={cn(
                  'max-w-2xl text-xs leading-5',
                  validation.canSend && smlReady ? 'text-success' : 'text-warning',
                )}>
                  {readyText}
                </p>
              )}
            </div>

            <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between lg:justify-end">
              <div className="min-w-[170px] shrink-0 sm:text-right">
                <div className="text-[11px] font-medium text-muted-foreground">
                  {isShopeePurchase || isShopeeSale ? 'ยอดที่ต้องชำระทั้งหมด' : 'ยอดรวมทั้งหมด'}
                </div>
                <div className="mt-0.5 text-2xl font-bold tabular-nums tracking-tight text-foreground">
                  {money(displayTotal)}
                </div>
                {isShopeePurchase && (
                  <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground sm:justify-end">
                    <span>ยอดสินค้า {money(goodsTotal)}</span>
                    {coinAmount != null && coinAmount > 0 && (
                      <span className="font-medium text-info">Shopee Coin {money(coinAmount)}</span>
                    )}
                    {payableTotal != null && payableTotal !== goodsTotal && (
                      <span>รวมส่วนลด/ค่าส่งแล้ว</span>
                    )}
                  </div>
                )}
              </div>

              {canShowSendButton && (
                <div className="flex min-w-0 flex-col gap-1.5 sm:items-end">
                  <TooltipProvider delayDuration={150}>
                    <Tooltip>
                      <TooltipTrigger asChild>
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
                      {(!validation.canSend || !smlReady) && (
                        <TooltipContent side="left" className="max-w-xs">
                          {!smlReady
                            ? smlBlockedMessage(smlReadiness)
                            : `ยังส่งไม่ได้: พบ ${validation.issues.length} ปัญหา · ตรวจรหัสสินค้า การยืนยัน หน่วย จำนวน และราคา`}
                        </TooltipContent>
                      )}
                    </Tooltip>
                  </TooltipProvider>

                  {expectedRoute && (
                    <div className={cn('max-w-[340px] text-left text-[10px] leading-4 tabular-nums text-muted-foreground sm:text-right', !enabled && 'opacity-50')}>
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
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        </CardHeader>

        <CardContent className="px-5 py-3">
          <div className="grid grid-cols-2 gap-x-6 gap-y-3 md:grid-cols-4 xl:grid-cols-6">
            {!isPurchase && (
              <>
                <InfoRow
                  label="ลูกค้า"
                  value={buyerName || '—'}
                />
                <InfoRow
                  label="เบอร์โทร"
                  value={(rawData?.customer_phone as string) || '—'}
                />
              </>
            )}
            <InfoRow
              label="ช่องทาง"
              value={SOURCE_LABELS[bill.source] ?? bill.source}
            />
            {isShopeeSale && shopeeShopID && (
              <InfoRow
                label="ร้าน Shopee"
                value={`${shopeeShopLabel || 'Shopee shop'} · ${shopeeShopID}`}
              />
            )}
            {isPurchase && orderID && (
              <InfoRow
                label="เลขคำสั่งซื้อ"
                value={<span className="font-mono text-xs">{orderID}</span>}
              />
            )}
            {isShopeeSale && orderID && (
              <InfoRow
                label="เลขคำสั่งซื้อ"
                value={<span className="font-mono text-xs">{orderID}</span>}
              />
            )}
            {(isPurchase || isShopeeSale) && orderDateTime && (
              <InfoRow
                label="วันที่สั่งซื้อ"
                value={orderDateTime}
              />
            )}
            {isPurchase && sellerName && (
              <InfoRow
                label="ผู้ขาย Shopee"
                value={sellerName}
              />
            )}
            {isPurchase && docDate && (
              <InfoRow
                label="วันที่เอกสาร"
                value={docDate}
              />
            )}
            {isShopeeSale && paymentChannel && (
              <InfoRow
                label="ช่องทางชำระเงิน"
                value={paymentChannel}
              />
            )}
            {isShopeeSale && trackingNo && (
              <InfoRow
                label="เลขพัสดุ"
                value={<span className="font-mono text-xs">{trackingNo}</span>}
              />
            )}
            <InfoRow
              label="วันที่สร้าง"
              value={dayjs(bill.created_at).format('DD/MM/YYYY HH:mm')}
            />
            <InfoRow
              label="รายการสินค้า"
              value={`${rawItemCount ?? itemCount} รายการ`}
            />
            {isPurchase && (
              <InfoRow
                label="สถานะตรวจสินค้า"
                value={issueCount > 0 ? `ต้องแก้ ${issueCount} รายการ` : 'พร้อมส่ง SML'}
              />
            )}
            {bill.sent_at && (
              <InfoRow
                label="ส่ง SML เมื่อ"
                value={dayjs(bill.sent_at).format('DD/MM/YYYY HH:mm')}
              />
            )}
            {!isPurchase && bill.ai_confidence != null && (
              <InfoRow
                label="ความมั่นใจ"
                value={`${Math.round(bill.ai_confidence * 100)}%`}
              />
            )}
            {bill.remark && (
              <InfoRow
                label="หมายเหตุ"
                value={bill.remark}
              />
            )}
          </div>

          {/* Failure details moved to BillFailureCard (rendered alongside this
              header by the parent) — gives the error room to breathe + a
              copy button without cluttering the meta grid. */}
        </CardContent>
      </Card>
    </div>
  )
}
