import { AlertTriangle, CheckCircle2 } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

type Props = {
  sourceLabel: string
  rawData?: Record<string, unknown> | null
  smlDocumentAmount: number
  reviewed: boolean
  canConfirm: boolean
  confirming: boolean
  onConfirm: () => void
}

const amount = (rawData: Record<string, unknown> | null | undefined, key: string) => {
  const value = Number(rawData?.[key] ?? 0)
  return Number.isFinite(value) ? value : 0
}

const roundMoney = (value: number) => Math.round((value + Number.EPSILON) * 100) / 100

const formatBaht = (value: number, signed = false) => {
  const prefix = signed && value > 0 ? '+' : ''
  return `${prefix}${value.toLocaleString('th-TH', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} บาท`
}

export function MarketplaceAmountReviewAlert({
  sourceLabel,
  rawData,
  smlDocumentAmount,
  reviewed,
  canConfirm,
  confirming,
  onConfirm,
}: Props) {
  const orderAmount = amount(rawData, 'order_total_amount') || amount(rawData, 'paid_total_amount')
  const itemGross = amount(rawData, 'item_gross_amount')
  const platformDiscount = amount(rawData, 'platform_discount_amount')
  const sellerDiscount = amount(rawData, 'seller_discount_amount')
  const totalDiscount = amount(rawData, 'discount_amount') || platformDiscount + sellerDiscount
  const netProduct = rawData?.net_product_amount == null
    ? itemGross - totalDiscount
    : amount(rawData, 'net_product_amount')
  const shipping = amount(rawData, 'shipping_amount')
  const taxes = amount(rawData, 'taxes_amount')
  const paymentDiscount = amount(rawData, 'payment_discount_amount')
  const itemizedAmount = roundMoney(netProduct + shipping + taxes - paymentDiscount)
  const unallocatedAmount = roundMoney(orderAmount - itemizedAmount)
  const isTikTok = sourceLabel.toLocaleLowerCase().includes('tiktok')
  const unallocatedLabel = isTikTok
    ? 'ยอดที่ TikTok เรียกเก็บเพิ่มจากผู้ซื้อ'
    : 'ยอดที่ Marketplace เรียกเก็บเพิ่มจากผู้ซื้อ'

  return (
    <Alert className="border-warning/35 bg-warning/10">
      <AlertTriangle className="h-4 w-4 text-warning" />
      <AlertTitle>ยอดจาก {sourceLabel} มีส่วนที่ไฟล์ไม่ได้แจกแจง</AlertTitle>
      <AlertDescription className="space-y-3">
        <p className="max-w-4xl text-sm leading-6 text-foreground">
          {sourceLabel} แจ้งยอดผู้ซื้อชำระ {formatBaht(orderAmount)} แต่แจกแจงจากสินค้า ค่าส่ง ภาษี และส่วนลดได้ {formatBaht(itemizedAmount)}
          {' '}จึงมีส่วนต่าง {formatBaht(unallocatedAmount, true)} ที่ไฟล์ Excel ไม่ได้แจกแจงเป็นรายการ
        </p>

        <dl className="grid gap-x-6 gap-y-1.5 border-y border-warning/25 py-3 text-sm sm:grid-cols-2 lg:grid-cols-3">
          <AmountRow label="สินค้าก่อนส่วนลด" value={itemGross} />
          <AmountRow label={`ส่วนลดสินค้า (${sourceLabel} / ร้าน)`} value={-totalDiscount} />
          <AmountRow label="สินค้าหลังส่วนลด" value={netProduct} emphasized />
          <AmountRow label="ค่าส่งที่ผู้ซื้อชำระ" value={shipping} />
          <AmountRow label="ภาษีจากไฟล์" value={taxes} />
          <AmountRow label="ส่วนลดช่องทางชำระเงิน" value={-paymentDiscount} />
          <AmountRow label="รวมยอดที่แจกแจงได้" value={itemizedAmount} emphasized />
          <AmountRow label={`Order Amount จาก ${sourceLabel}`} value={orderAmount} emphasized />
          <AmountRow label={unallocatedLabel} value={unallocatedAmount} emphasized warning />
        </dl>

        <div className="flex flex-col gap-3 rounded-md bg-card/80 px-3 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0 space-y-1">
            <p className="font-semibold text-foreground">
              ยอดที่จะส่ง SML ตามรายการในบิล: {formatBaht(smlDocumentAmount)}
            </p>
            <p className="max-w-4xl text-xs leading-5 text-muted-foreground">
              {isTikTok
                ? 'ส่วนต่างอาจเป็นค่าประกัน/ความคุ้มครองที่ TikTok เรียกเก็บจากผู้ซื้อ แต่ไม่แสดงเป็นคอลัมน์ใน Excel จึงไม่ใช่รายการขายที่ระบบจะส่ง SML อัตโนมัติ '
                : 'ระบบไม่รวมยอดที่ไฟล์ไม่ได้แจกแจงเข้า SML อัตโนมัติ หากยอดนี้เป็นรายได้ของร้าน ให้เพิ่มรายการในบิลก่อนยืนยัน '}
              ส่วนการแยกยอดก่อน VAT และ VAT จะคำนวณตาม vat_type ของเส้นทาง SML ที่เลือก
            </p>
          </div>
          {reviewed ? (
            <span className="inline-flex shrink-0 items-center gap-1.5 text-sm font-medium text-accent-strong">
              <CheckCircle2 className="h-4 w-4" />
              รับทราบแล้ว ใช้ยอดตามรายการในบิล
            </span>
          ) : canConfirm ? (
            <Button type="button" size="sm" variant="outline" className="h-auto min-h-9 shrink-0 whitespace-normal bg-card text-center" onClick={onConfirm} disabled={confirming}>
              {confirming ? 'กำลังบันทึก...' : `รับทราบและใช้ยอดบิล ${formatBaht(smlDocumentAmount)}`}
            </Button>
          ) : null}
        </div>
      </AlertDescription>
    </Alert>
  )
}

function AmountRow({
  label,
  value,
  emphasized = false,
  warning = false,
}: {
  label: string
  value: number
  emphasized?: boolean
  warning?: boolean
}) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={warning ? 'font-semibold tabular-nums text-warning' : emphasized ? 'font-semibold tabular-nums text-foreground' : 'tabular-nums text-foreground'}>
        {formatBaht(value, warning)}
      </dd>
    </div>
  )
}
