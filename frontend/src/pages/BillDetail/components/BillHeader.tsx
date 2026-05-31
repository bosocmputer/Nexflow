import { ArrowLeft } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import dayjs from 'dayjs'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import BillStatusBadge from '@/components/BillStatusBadge'
import type { Bill } from '@/types'
import { isShopeeSalesBill, rawNumber, rawString, shopeeOrderID } from '@/lib/shopeeBill'
import { SOURCE_LABELS } from '../utils/formatters'
import { hasInvalidPrice } from '../utils/validation'

interface Props {
  bill: Bill
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
        {label}
      </span>
      <span className="text-[13px] leading-5 text-foreground">{value || '—'}</span>
    </div>
  )
}

export function BillHeader({ bill }: Props) {
  const navigate = useNavigate()
  const rawData = bill.raw_data as Record<string, unknown> | null
  const isPurchase = bill.bill_type === 'purchase'
  const isShopeeSale = isShopeeSalesBill(bill)
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

      <Card className="overflow-hidden rounded-xl border-border/70 shadow-sm">
        <CardHeader className="flex flex-row items-center justify-between gap-3 border-b bg-card px-5 py-3">
          <div className="flex items-center gap-2">
            <h2 className="font-mono text-xl font-bold tracking-tight">
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
                className="bg-primary/10 text-primary hover:bg-primary/15"
                title="Sales Order"
              >
                {'ขาย -> ใบสั่งขาย'}
              </Badge>
            )}
          </div>
          <BillStatusBadge status={bill.status} />
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
