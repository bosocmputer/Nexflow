import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { JsonViewer } from '@/components/common/JsonViewer'
import { Badge } from '@/components/ui/badge'
import { remark2Label } from '@/lib/smlRemark2'

interface Props {
  smlPayload?: Record<string, unknown> | null
  smlResponse?: Record<string, unknown> | null
}

function text(value: unknown): string {
  if (value == null || value === '') return '—'
  if (typeof value === 'number') return value.toLocaleString()
  return String(value)
}

function money(value: unknown): string {
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n)) return '—'
  return `฿${n.toLocaleString(undefined, { maximumFractionDigits: 2 })}`
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

function SummaryCell({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="rounded-md border bg-background px-3 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className={mono ? 'mt-1 font-mono text-xs text-foreground' : 'mt-1 text-sm text-foreground'}>
        {value}
      </div>
    </div>
  )
}

export function SmlPayloadSection({ smlPayload, smlResponse }: Props) {
  if (!smlPayload && !smlResponse) return null
  const items = Array.isArray(smlPayload?.items) ? smlPayload?.items : []
  const firstItem = items[0] as Record<string, unknown> | undefined
  const responseStatus = text(smlResponse?.status)
  const responseLabel = responseStatus === '—' ? 'มี response' : responseStatus
  const whCode = smlPayload?.wh_code ?? firstItem?.wh_code
  const shelfCode = smlPayload?.shelf_code ?? firstItem?.shelf_code
  const remark2 = typeof smlPayload?.remark_2 === 'string' ? smlPayload.remark_2 : ''

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between gap-3">
          <CardTitle className="text-sm font-semibold">ข้อมูลการส่งเข้า SML</CardTitle>
          {smlResponse && (
            <Badge
              variant="secondary"
              className={
                responseStatus === 'success'
                  ? 'bg-success/15 text-success'
                  : 'bg-muted text-muted-foreground'
              }
            >
              {responseStatus === 'success' ? 'ส่งสำเร็จ' : responseLabel}
            </Badge>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-0">
        {smlPayload && (
          <div className="space-y-2">
            <div className="grid gap-2 sm:grid-cols-2">
              <SummaryCell label="เลขเอกสาร SML" value={text(smlPayload.doc_no)} mono />
              <SummaryCell label="อ้างอิง Shopee" value={text(smlPayload.doc_ref)} mono />
              <SummaryCell label="ผู้ขาย" value={text(smlPayload.supplier_name ?? smlPayload.cust_code)} />
              <SummaryCell label="คู่ค้า" value={text(smlPayload.cust_code)} mono />
              <SummaryCell label="คลัง" value={`${text(whCode)} / ${text(shelfCode)}`} mono />
              <SummaryCell
                label="ภาษี"
                value={`${vatLabel(smlPayload.vat_type)} · ${text(smlPayload.vat_rate)}%`}
              />
              <SummaryCell label="สถานะเอกสาร" value={remark2Label(remark2)} />
              <SummaryCell label="จำนวนรายการ" value={`${items.length.toLocaleString()} รายการ`} />
              <SummaryCell label="ยอดสุทธิ" value={money(smlPayload.total_amount)} />
            </div>
            <p className="text-xs text-muted-foreground">
              สรุปนี้คือข้อมูลหลักที่ส่งเข้า SML แล้ว ส่วน JSON ด้านล่างเก็บไว้สำหรับตรวจ field แบบละเอียด
            </p>
          </div>
        )}
        {smlPayload && (
          <JsonViewer
            title="ข้อมูลที่ส่งไป SML"
            data={smlPayload}
            defaultOpen={false}
          />
        )}
        {smlResponse && (
          <JsonViewer
            title="ผลตอบกลับจาก SML"
            data={smlResponse}
            defaultOpen={false}
          />
        )}
      </CardContent>
    </Card>
  )
}
