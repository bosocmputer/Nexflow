import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
} from '@/components/ui/table'
import type { Bill, BillItem } from '@/types'
import { isShopeePurchaseBill, isShopeeSalesBill, money } from '@/lib/shopeeBill'
import { hasInvalidPrice } from '../utils/validation'
import { BillItemRow } from './BillItemRow'

interface Props {
  bill: Bill
  canEdit: boolean
  onItemUpdated: (updated: BillItem) => void
  onItemDeleted: (itemId: string) => void
  onItemAdded: (item: BillItem) => void
  onRefresh: () => Promise<unknown>
  // BillTotal's "ดู →" link sets this to the offending item id; the matching
  // row briefly flashes (1.5s) so admin's eye is drawn to the right place
  // even when the items list is long.
  highlightItemId?: string | null
}

interface DiscountSummary {
  shopee_discount_amount?: number
  shop_discount_amount?: number
  total_discount_amount?: number
  shopee_discount_codes?: string[]
  shop_discount_codes?: string[]
}

function discountSummaryFromBill(bill: Bill): DiscountSummary | null {
  const value = bill.raw_data?.discount_summary
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  return value as DiscountSummary
}

export function BillItemsTable({
  bill,
  canEdit,
  onItemUpdated,
  onItemDeleted,
  onItemAdded,
  onRefresh,
  highlightItemId,
}: Props) {
  const items = bill.items ?? []
  const rawNameLabel = isShopeeSalesBill(bill) ? 'ชื่อสินค้าจาก Excel' : 'ชื่อสินค้าจากอีเมล'
  const showDiscountColumn = isShopeePurchaseBill(bill)
  const discountSummary = showDiscountColumn ? discountSummaryFromBill(bill) : null
  const totalDiscount = discountSummary?.total_discount_amount ?? 0
  const itemDiscountTotal = items.reduce((sum, item) => sum + (item.discount_amount ?? 0), 0)
  const parsedDiscountNotApplied = bill.status === 'sent' && totalDiscount > 0 && itemDiscountTotal <= 0
  const discountCodes = [
    ...(discountSummary?.shopee_discount_codes ?? []),
    ...(discountSummary?.shop_discount_codes ?? []),
  ]
  const visibleColumnCount = canEdit
    ? showDiscountColumn ? 10 : 9
    : showDiscountColumn ? 9 : 8
  const issueCount = items.filter((item) => {
    return (
      !item.item_code ||
      item.mapped !== true ||
      !item.unit_code ||
      !item.qty ||
      item.qty <= 0 ||
      hasInvalidPrice(item)
    )
  }).length

  return (
    <Card className="rounded-2xl border-border/70 shadow-sm">
      <CardHeader className="flex flex-row items-start justify-between gap-3 pb-3">
        <div>
          <CardTitle className="text-sm font-semibold">
            รายการสินค้า ({items.length} รายการ)
          </CardTitle>
          <p className="mt-1 text-xs text-muted-foreground">
            ตรวจรหัสสินค้า หน่วย จำนวน และราคาให้ครบก่อนส่งเข้า SML
          </p>
          {showDiscountColumn && (
            <div className="mt-2 max-w-3xl rounded-md border border-info/20 bg-info/5 px-3 py-2 text-xs leading-5 text-muted-foreground">
              <span className="font-medium text-foreground">ส่วนลด:</span>{' '}
              {parsedDiscountNotApplied
                ? `${money(totalDiscount)} พบในอีเมล แต่บิลนี้ส่ง SML แล้ว ระบบไม่แก้ย้อนหลัง`
                : totalDiscount > 0
                ? `${money(totalDiscount)} จากโค้ด Shopee ${money(discountSummary?.shopee_discount_amount ?? 0)} + ร้านค้า ${money(discountSummary?.shop_discount_amount ?? 0)}`
                : 'ไม่พบส่วนลดในอีเมลนี้'}
              {!parsedDiscountNotApplied && ' · หารเท่ากันตามจำนวนรายการสินค้า ไม่รวมค่าขนส่ง'}
              {discountCodes.length > 0 && (
                <span className="ml-1">· โค้ด: {discountCodes.join(', ')}</span>
              )}
            </div>
          )}
        </div>
        {issueCount > 0 ? (
          <span className="rounded-md bg-warning/10 px-2 py-1 text-xs font-medium text-warning">
            ต้องแก้ {issueCount} รายการ
          </span>
        ) : items.length > 0 ? (
          <span className="rounded-md bg-success/10 px-2 py-1 text-xs font-medium text-success">
            พร้อมส่ง
          </span>
        ) : null}
      </CardHeader>
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <Table className={showDiscountColumn ? 'min-w-[1210px]' : 'min-w-[1080px]'}>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[360px]">{rawNameLabel}</TableHead>
                <TableHead className="w-[220px]">รหัสสินค้า SML</TableHead>
                <TableHead className="w-[300px]">ชื่อสินค้าใน SML</TableHead>
                <TableHead className="w-[130px] text-center">ความมั่นใจ</TableHead>
                <TableHead className="w-[110px] text-right">จำนวน</TableHead>
                <TableHead className="w-[120px]">หน่วย</TableHead>
                <TableHead className="w-[140px] text-right">ราคา</TableHead>
                {showDiscountColumn && (
                  <TableHead className="w-[130px] text-right">ส่วนลด</TableHead>
                )}
                <TableHead className="w-[140px] text-right">รวม</TableHead>
                {canEdit && <TableHead className="w-[170px] text-center">จัดการ</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <BillItemRow
                  key={item.id}
                  item={item}
                  billId={bill.id}
                  editable={canEdit}
                  onUpdated={onItemUpdated}
                  onDeleted={onItemDeleted}
                  onRefresh={onRefresh}
                  highlighted={item.id === highlightItemId}
                  rawNameLabel={rawNameLabel}
                  showDiscountColumn={showDiscountColumn}
                  tableColumnCount={visibleColumnCount}
                />
              ))}
              {items.length === 0 && (
                <TableRow>
                  <td
                    colSpan={visibleColumnCount}
                    className="py-8 text-center text-sm text-muted-foreground"
                  >
                    ยังไม่มีรายการสินค้า
                  </td>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>

      </CardContent>
    </Card>
  )
}
