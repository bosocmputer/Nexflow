import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { JsonViewer } from '@/components/common/JsonViewer'
import { documentItems, documentLocation } from '@/lib/smlPayloadSummary.js'
import { useAuthStore } from '@/store/auth'
import type { Bill } from '@/types'
import { SML_INQUIRY_TYPE_FIELD_LABEL, smlInquiryTypeLabel } from '../utils/presentation'
import { classifyBillInputChannel } from '@/lib/billInputChannel'
import { smlRouteLabel } from '@/lib/audit-log-meta'

interface Props {
  smlPayload?: Record<string, unknown> | null
  smlResponse?: Record<string, unknown> | null
  bill: Bill
}

function text(value: unknown): string {
  if (value == null || value === '') return '—'
  if (typeof value === 'number') return value.toLocaleString()
  return String(value)
}

function money(value: unknown): string {
  const raw = typeof value === 'number' ? String(value) : typeof value === 'string' ? value.trim() : ''
  if (!/^-?\d+(?:\.\d+)?$/.test(raw)) return '—'
  const negative = raw.startsWith('-')
  const [whole, fraction = ''] = (negative ? raw.slice(1) : raw).split('.')
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  return `฿${negative ? '-' : ''}${grouped}.${fraction.padEnd(2, '0')}`
}

function vatLabel(value: unknown): string {
  switch (Number(value)) {
    case 0:
      return 'แยกนอก'
    case 1:
      return 'รวมใน'
    case 2:
      return 'อัตรา 0%'
    default:
      return '—'
  }
}

function SummaryItem({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="min-w-0 border-b border-border/60 py-2.5 last:border-b-0 sm:[&:nth-last-child(-n+2)]:border-b-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'mt-1 break-words font-mono text-xs font-medium text-foreground' : 'mt-1 break-words text-sm font-medium text-foreground'}>
        {value}
      </dd>
    </div>
  )
}

export function SmlPayloadSection({ smlPayload, smlResponse, bill }: Props) {
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const isTikTokShopPrepared = classifyBillInputChannel(bill) === 'tiktok_shop' && !smlPayload && !smlResponse
  if (!smlPayload && !smlResponse && !isTikTokShopPrepared) return null
  const items = documentItems(smlPayload)
  const { whCode, shelfCode } = documentLocation(smlPayload)
  const partyCode = text(smlPayload?.cust_code)
  const partyName = text(smlPayload?.cust_name ?? smlPayload?.supplier_name ?? smlPayload?.party_name)
  const party = partyName === '—' ? partyCode : partyCode === '—' ? partyName : `${partyCode} · ${partyName}`
  const remark = typeof smlPayload?.remark === 'string' ? smlPayload.remark.trim() : ''
  const remark2 = typeof smlPayload?.remark_2 === 'string' ? smlPayload.remark_2.trim() : ''
  const branchCode = typeof smlPayload?.branch_code === 'string' ? smlPayload.branch_code.trim() : ''
  const defaults = bill.preview?.sml_defaults
  const preparedPartyCode = text(defaults?.party_code)
  const preparedPartyName = text(defaults?.party_name)
  const preparedParty = preparedPartyName === '—'
    ? preparedPartyCode
    : preparedPartyCode === '—'
      ? preparedPartyName
      : `${preparedPartyCode} · ${preparedPartyName}`
  const preparedOrderID = text(bill.raw_data?.tiktok_order_id ?? bill.raw_data?.order_id)
  const preparedTotal = (bill.items ?? []).reduce((sum, item) => {
    const gross = item.gross_amount ?? item.qty * (item.price ?? 0)
    return sum + Math.max(gross - (item.discount_amount ?? 0), 0)
  }, 0)
  const excludedBuyerCharges = bill.raw_data?.excluded_buyer_platform_charges

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-semibold">
          {isTikTokShopPrepared ? 'สรุปข้อมูลเตรียมส่งเข้า SML' : 'สรุปข้อมูลที่ส่งเข้า SML'}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 pt-0">
        {isTikTokShopPrepared && (
          <div className="space-y-2">
            <p className="text-xs leading-5 text-muted-foreground">
              รูปแบบเดียวกับข้อมูลสรุปหลังส่งของ Shopee แต่ชุดนี้เป็นข้อมูลตรวจ UAT จาก Bill และเส้นทางปัจจุบัน ยังไม่มีการส่งหรือสร้างเอกสารใน SML
            </p>
            <dl className="grid gap-x-6 rounded-md border border-warning/25 bg-warning/[0.04] px-3 sm:grid-cols-2">
              <SummaryItem label="สถานะ" value="ยังไม่ส่ง · รอตรวจ UAT" />
              <SummaryItem label="ปลายทาง SML" value={smlRouteLabel(bill.preview?.route)} />
              <SummaryItem label="ตัวอย่างเลขเอกสาร" value={text(bill.preview?.doc_no)} mono />
              <SummaryItem label="อ้างอิงคำสั่งซื้อ" value={preparedOrderID} mono />
              <SummaryItem label="รูปแบบเอกสาร" value={text(bill.preview?.doc_format_code)} mono />
              <SummaryItem label="วิธีส่ง" value="ยังไม่ส่งเข้า SML" />
              <SummaryItem label="ลูกค้า SML" value={preparedParty} />
              <SummaryItem label="คลัง / พื้นที่เก็บ" value={`${text(defaults?.wh_code)} / ${text(defaults?.shelf_code)}`} mono />
              <SummaryItem label="ภาษี" value={`${vatLabel(defaults?.vat_type)} · ${text(defaults?.vat_rate)}%`} />
              <SummaryItem label="จำนวนรายการ" value={`${(bill.items ?? []).length.toLocaleString('th-TH')} รายการ`} />
              <SummaryItem label="ยอดสุทธิที่จะส่ง SML" value={money(preparedTotal)} />
              {excludedBuyerCharges != null && Number(excludedBuyerCharges) > 0 && (
                <SummaryItem
                  label="ยอดที่ TikTok เรียกเก็บเพิ่มจากผู้ซื้อ"
                  value={`${money(excludedBuyerCharges)} · ไม่สร้างเป็นรายการ SML`}
                />
              )}
            </dl>
            <p className="text-[11px] leading-4 text-muted-foreground">
              เลขเอกสารด้านบนเป็นเพียงตัวอย่างและยังไม่ถูกจอง เลขจริงจะยืนยันเมื่อได้รับอนุมัติและเริ่มส่ง SML เท่านั้น
            </p>
          </div>
        )}
        {smlPayload && (
          <div className="space-y-2">
            <dl className="grid gap-x-6 rounded-md bg-muted/20 px-3 sm:grid-cols-2">
              <SummaryItem label="เลขเอกสาร SML" value={text(smlPayload.doc_no ?? bill.sml_doc_no)} mono />
              <SummaryItem label="อ้างอิงคำสั่งซื้อ" value={text(smlPayload.doc_ref)} mono />
              <SummaryItem label="วิธีส่ง" value={bill.sml_sent_automatically ? 'อัตโนมัติจาก Shopee (AUTO)' : 'ส่งโดยผู้ใช้'} />
              <SummaryItem label="รูปแบบเอกสาร" value={text(smlPayload.doc_format_code)} mono />
              <SummaryItem
                label={SML_INQUIRY_TYPE_FIELD_LABEL}
                value={smlInquiryTypeLabel(
                  smlPayload.inquiry_type,
                  bill.bill_type,
                  bill.bill_type === 'sale' ? 0 : undefined,
                )}
              />
              <SummaryItem label="ลูกค้า SML" value={party} />
              <SummaryItem label="คลัง / พื้นที่เก็บ" value={`${text(whCode)} / ${text(shelfCode)}`} mono />
              <SummaryItem
                label="ภาษี"
                value={`${vatLabel(smlPayload.vat_type)} · ${text(smlPayload.vat_rate)}%`}
              />
              <SummaryItem label="จำนวนรายการ" value={`${items.length.toLocaleString('th-TH')} รายการ`} />
              <SummaryItem label="ยอดสุทธิ" value={money(smlPayload.total_amount_decimal ?? smlPayload.total_amount)} />
              {branchCode && <SummaryItem label="สาขา" value={branchCode} mono />}
              <SummaryItem label="หมายเหตุ 1" value={text(remark)} />
              <SummaryItem label="หมายเหตุ 2" value={text(remark2)} />
            </dl>
          </div>
        )}
        {isAdmin && (smlPayload || smlResponse) && (
          <div className="space-y-2 border-t border-border/70 pt-3">
            <p className="text-xs text-muted-foreground">ข้อมูลเทคนิคสำหรับผู้ดูแลระบบ ใช้เมื่อต้องตรวจสอบแต่ละ field</p>
            {smlPayload && <JsonViewer title="ค่าที่ส่งไป SML" data={smlPayload} defaultOpen={false} />}
            {smlResponse && <JsonViewer title="ผลตอบกลับจาก SML" data={smlResponse} defaultOpen={false} />}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
