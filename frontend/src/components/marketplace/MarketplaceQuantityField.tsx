import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

export type MarketplaceQuantityMode = 'marketplace_qty' | 'fixed_per_item'

export function quantityModeFromMultiplier(multiplier?: number | string): MarketplaceQuantityMode {
  return Number(multiplier) > 1 ? 'fixed_per_item' : 'marketplace_qty'
}

export function MarketplaceQuantityField({
  idPrefix,
  mode,
  multiplier,
  onModeChange,
  onMultiplierChange,
}: {
  idPrefix: string
  mode: MarketplaceQuantityMode
  multiplier: string
  onModeChange: (mode: MarketplaceQuantityMode) => void
  onMultiplierChange: (value: string) => void
}) {
  const selectID = `${idPrefix}-quantity-mode`
  const multiplierID = `${idPrefix}-quantity-multiplier`

  return (
    <div className="space-y-3">
      <div className="space-y-1.5">
        <Label htmlFor={selectID}>วิธีตัดจำนวน</Label>
        <Select
          value={mode}
          onValueChange={(value) => {
            const nextMode = value as MarketplaceQuantityMode
            onModeChange(nextMode)
            if (nextMode === 'marketplace_qty') onMultiplierChange('1')
          }}
        >
          <SelectTrigger id={selectID} className="h-10"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="marketplace_qty">ตัดตามจำนวนจาก Marketplace</SelectItem>
            <SelectItem value="fixed_per_item">กำหนดจำนวนต่อ 1 รายการ</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {mode === 'marketplace_qty'
            ? 'ตัวอย่าง: Marketplace ส่ง qty 3 ระบบจะตัด SML 3 หน่วย'
            : 'ใช้เมื่อสินค้า 1 รายการใน Marketplace ต้องตัด SML มากกว่า 1 หน่วย'}
        </p>
      </div>
      {mode === 'fixed_per_item' && (
        <div className="space-y-1.5">
          <Label htmlFor={multiplierID}>จำนวนหน่วย SML ต่อ 1 รายการ</Label>
          <Input
            id={multiplierID}
            inputMode="numeric"
            type="number"
            min={1}
            max={1_000_000}
            step={1}
            value={multiplier}
            onChange={(event) => onMultiplierChange(event.target.value)}
          />
          <p className="text-xs text-muted-foreground">ตัวอย่าง: กำหนด 2 เมื่อขาย 1 รายการแล้วต้องตัด SML 2 หน่วย</p>
        </div>
      )}
    </div>
  )
}
