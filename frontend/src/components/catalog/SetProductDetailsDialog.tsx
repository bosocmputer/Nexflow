import { AlertTriangle, Boxes } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { CatalogSetComponent } from '@/types'

const WARNING_LABELS: Record<string, string> = {
  set_definition_missing: 'ยังไม่มีรายการส่วนประกอบ',
  nested_set_not_supported: 'มีสินค้าชุดซ้อนกัน',
  set_component_inactive: 'มีส่วนประกอบที่หยุดใช้งาน',
  set_component_not_stock_item: 'มีส่วนประกอบที่ไม่ใช่สินค้าสต๊อก',
  set_component_qty_invalid: 'จำนวนส่วนประกอบไม่ถูกต้อง',
  set_component_unit_invalid: 'หน่วยส่วนประกอบไม่ถูกต้อง',
  set_allocation_invalid: 'สัดส่วนราคาส่วนประกอบไม่ถูกต้อง',
  set_product_schema_unsupported: 'ฐานข้อมูล SML นี้ยังไม่รองรับข้อมูลสินค้าชุด',
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  itemCode: string
  itemName: string
  components?: CatalogSetComponent[]
  documentValid?: boolean
  stockValid?: boolean
  warningCodes?: string[]
  showStockStatus?: boolean
}

function formatQuantity(value: number) {
  return new Intl.NumberFormat('th-TH', { maximumFractionDigits: 6 }).format(value)
}

export function SetProductDetailsDialog({
  open,
  onOpenChange,
  itemCode,
  itemName,
  components = [],
  documentValid = false,
  stockValid = false,
  warningCodes = [],
  showStockStatus = false,
}: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-hidden p-0">
        <DialogHeader className="border-b px-5 py-4 text-left">
          <DialogTitle className="flex flex-wrap items-center gap-2">
            <Boxes className="h-4 w-4 text-primary" />
            ส่วนประกอบสินค้าชุด
            <Badge variant="outline">{components.length.toLocaleString()} รายการ</Badge>
          </DialogTitle>
          <DialogDescription><span className="font-mono text-foreground">{itemCode}</span> · {itemName}</DialogDescription>
        </DialogHeader>

        <div className="min-h-0 overflow-y-auto px-5 py-4">
          <div className="mb-3 flex flex-wrap gap-2">
            <Badge variant={documentValid ? 'default' : 'destructive'}>{documentValid ? 'พร้อมสร้างเอกสาร' : 'ยังสร้างเอกสารไม่ได้'}</Badge>
            {showStockStatus && <Badge variant={stockValid ? 'default' : 'destructive'}>{stockValid ? 'พร้อมคำนวณสต๊อก' : 'ยังคำนวณสต๊อกไม่ได้'}</Badge>}
          </div>

          {warningCodes.length > 0 && (
            <div className="mb-3 flex gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm text-amber-900 dark:text-amber-100">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>{warningCodes.map((code) => WARNING_LABELS[code] || code).join(' · ')}</span>
            </div>
          )}

          {components.length > 0 ? (
            <div className="overflow-hidden rounded-md border">
              <div className="hidden grid-cols-[90px_minmax(180px,1fr)_90px_90px] gap-3 border-b bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground sm:grid">
                <span>รหัส</span><span>สินค้า</span><span className="text-right">จำนวนต่อชุด</span><span>หน่วย</span>
              </div>
              <div className="divide-y">
                {components.map((component) => (
                  <div key={`${component.line_number}:${component.item_code}`} className="grid gap-1 px-3 py-2 text-sm sm:grid-cols-[90px_minmax(180px,1fr)_90px_90px] sm:items-center sm:gap-3">
                    <span className="font-mono font-medium">{component.item_code}</span>
                    <span className="min-w-0 truncate" title={component.item_name}>{component.item_name || '-'}</span>
                    <span className="tabular-nums sm:text-right">{formatQuantity(component.qty)}</span>
                    <span className="text-muted-foreground">{component.unit_code || '-'}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground">ยังไม่มีข้อมูลส่วนประกอบ กรุณาซิงก์สินค้า SML ใหม่</div>
          )}

          <p className="mt-3 text-xs text-muted-foreground">ราคาบิลมาจาก Marketplace ส่วนสัดส่วนใน SML ใช้แบ่งยอดให้รายการส่วนประกอบ ระบบจะจับคู่กับรหัสสินค้าชุดนี้เพียงรายการเดียว</p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
