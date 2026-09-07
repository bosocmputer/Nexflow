import { useEffect, useState } from 'react'
import { Info } from 'lucide-react'
import client from '@/api/client'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { stockJobSummary } from '@/pages/BillDetail/utils/presentation'
import type { Bill } from '@/types'

// Load only the selected bill when opened, never one request per table row.
export function SMLBillInfo({ billId }: { billId: string }) {
  const [open, setOpen] = useState(false)
  const [bill, setBill] = useState<Bill | null>(null)
  const [error, setError] = useState(false)
  useEffect(() => {
    if (!open) return
    let alive = true
    setBill(null)
    setError(false)
    client.get<Bill>(`/api/bills/${billId}`).then(({ data }) => {
      if (alive) setBill(data)
    }).catch(() => { if (alive) setError(true) })
    return () => { alive = false }
  }, [open, billId])
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" aria-label="ดูรายละเอียดเอกสาร SML และสต๊อก" title="ดูรายละเอียดเอกสาร SML และสต๊อก" className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={(event) => event.stopPropagation()}>
          <Info className="h-3.5 w-3.5" aria-hidden />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(20rem,calc(100vw-2rem))] space-y-2 p-3 text-xs" onClick={(event) => event.stopPropagation()}>
        <h4 className="text-sm font-semibold">รายละเอียดเอกสาร SML และสต๊อก</h4>
        {error ? <p role="alert">โหลดรายละเอียดไม่สำเร็จ กรุณาปิดแล้วเปิดใหม่</p> : !bill ? <p role="status">กำลังโหลดรายละเอียด…</p> : <dl className="grid grid-cols-2 gap-x-3 gap-y-2 break-words">
          <div><dt className="text-muted-foreground">เลขเอกสาร SML</dt><dd>{bill.sml_doc_no || 'ยังไม่มีเลขเอกสาร'}</dd></div>
          <div><dt className="text-muted-foreground">วิธีส่ง</dt><dd>{bill.sml_sent_automatically ? 'อัตโนมัติ (AUTO)' : 'ไม่ได้ระบุว่าเป็นการส่งอัตโนมัติ'}</dd></div>
          <div><dt className="text-muted-foreground">ต้นทุนและสต๊อก</dt><dd>{stockJobSummary(bill.sml_stock_job_status) || 'ไม่พบสถานะงานสต๊อกที่ยืนยันได้'}</dd></div>
          <div><dt className="text-muted-foreground">ที่มาของสถานะ</dt><dd>งานสต๊อกของบิลนี้ ไม่ใช่สถานะการซิงก์สต๊อกไป Shopee</dd></div>
        </dl>}
      </PopoverContent>
    </Popover>
  )
}
