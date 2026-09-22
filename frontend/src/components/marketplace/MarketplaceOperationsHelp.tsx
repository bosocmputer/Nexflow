import { Info } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

type MarketplaceChannel = 'Shopee' | 'TikTok Shop' | 'Lazada' | 'Marketplace'

interface MarketplaceOperationsHelpProps {
  channel: MarketplaceChannel
  signalLabel: 'Webhook' | 'Push'
  syncLabel?: string
}

// Shared operator wording: a future Lazada Operations screen may use this
// component, but its presence here does not imply that Lazada Open API is live.
export function MarketplaceOperationsHelp({
  channel,
  signalLabel,
  syncLabel = 'ซิงก์สำรอง',
}: MarketplaceOperationsHelpProps) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5 bg-background" aria-label="คำอธิบายการทำงาน">
          <Info className="h-3.5 w-3.5" />
          วิธีทำงาน
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(25rem,calc(100vw-2rem))] p-3">
        <h2 className="text-sm font-semibold">การทำงานของ {channel}</h2>
        <dl className="mt-3 space-y-3 text-xs leading-5">
          <div>
            <dt className="font-medium text-foreground">{signalLabel}</dt>
            <dd className="text-muted-foreground">{channel} แจ้งการเปลี่ยนแปลงให้ Nexflow แล้วระบบตรวจข้อมูลจริงอีกครั้งก่อนทำงานต่อ</dd>
          </div>
          <div>
            <dt className="font-medium text-foreground">{syncLabel}</dt>
            <dd className="text-muted-foreground">ระบบตรวจซ้ำเป็นระยะเพื่อเก็บออเดอร์ที่อาจพลาดจาก {signalLabel} โดยไม่ส่งเอกสารซ้ำ</dd>
          </div>
          <div>
            <dt className="font-medium text-foreground">ส่ง SML อัตโนมัติ</dt>
            <dd className="text-muted-foreground">สร้างและส่งเฉพาะออเดอร์ใหม่ที่ผ่านเงื่อนไขหลังเวลาเปิดใช้ ไม่ย้อนส่งรายการเก่า</dd>
          </div>
          <div>
            <dt className="font-medium text-foreground">ตรวจระบบ</dt>
            <dd className="text-muted-foreground">ตรวจความพร้อมของการเชื่อมต่อ สินค้า และเส้นทางเอกสารเท่านั้น ไม่สร้าง Bill และไม่ส่ง SML</dd>
          </div>
        </dl>
      </PopoverContent>
    </Popover>
  )
}
