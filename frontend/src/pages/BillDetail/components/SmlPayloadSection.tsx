import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { JsonViewer } from '@/components/common/JsonViewer'
import { documentItems, documentLocation } from '@/lib/smlPayloadSummary.js'
import { useAuthStore } from '@/store/auth'
import type { Bill } from '@/types'

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
  if (!smlPayload && !smlResponse) return null
  const items = documentItems(smlPayload)
  const { whCode, shelfCode } = documentLocation(smlPayload)
  const partyCode = text(smlPayload?.cust_code)
  const partyName = text(smlPayload?.cust_name ?? smlPayload?.supplier_name ?? smlPayload?.party_name)
  const party = partyName === '—' ? partyCode : partyCode === '—' ? partyName : `${partyCode} · ${partyName}`
  const remark = typeof smlPayload?.remark === 'string' ? smlPayload.remark.trim() : ''
  const remark2 = typeof smlPayload?.remark_2 === 'string' ? smlPayload.remark_2.trim() : ''
  const branchCode = typeof smlPayload?.branch_code === 'string' ? smlPayload.branch_code.trim() : ''

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-semibold">สรุปข้อมูลที่ส่งเข้า SML</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 pt-0">
        {smlPayload && (
          <div className="space-y-2">
            <dl className="grid gap-x-6 rounded-md bg-muted/20 px-3 sm:grid-cols-2">
              <SummaryItem label="เลขเอกสาร SML" value={text(smlPayload.doc_no ?? bill.sml_doc_no)} mono />
              <SummaryItem label="อ้างอิงคำสั่งซื้อ" value={text(smlPayload.doc_ref)} mono />
              <SummaryItem label="วิธีส่ง" value={bill.sml_sent_automatically ? 'อัตโนมัติจาก Shopee (AUTO)' : 'ส่งโดยผู้ใช้'} />
              <SummaryItem label="รูปแบบเอกสาร" value={text(smlPayload.doc_format_code)} mono />
              <SummaryItem label="ลูกค้า SML" value={party} />
              <SummaryItem label="คลัง / พื้นที่เก็บ" value={`${text(whCode)} / ${text(shelfCode)}`} mono />
              <SummaryItem
                label="ภาษี"
                value={`${vatLabel(smlPayload.vat_type)} · ${text(smlPayload.vat_rate)}%`}
              />
              <SummaryItem label="จำนวนรายการ" value={`${items.length.toLocaleString('th-TH')} รายการ`} />
              <SummaryItem label="ยอดสุทธิ" value={money(smlPayload.total_amount_decimal ?? smlPayload.total_amount)} />
              {branchCode && <SummaryItem label="สาขา" value={branchCode} mono />}
              {remark && <SummaryItem label="หมายเหตุ 1" value={remark} />}
              {remark2 && <SummaryItem label="หมายเหตุ 2" value={remark2} />}
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
