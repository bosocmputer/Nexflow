import { useState } from 'react'
import { Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent } from '@/components/ui/card'
import api from '@/api/client'
import type { BillItem } from '@/types'

interface Props {
  billId: string
  onAdded: (item: BillItem) => void
}

export function AddItemForm({ billId, onAdded }: Props) {
  const [open, setOpen] = useState(false)
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState({
    raw_name: '',
    item_code: '',
    unit_code: '',
    qty: '1',
    price: '0',
  })

  const reset = () =>
    setDraft({ raw_name: '', item_code: '', unit_code: '', qty: '1', price: '0' })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!draft.raw_name.trim() || Number(draft.qty) <= 0) return
    setAdding(true)
    try {
      const payload: Record<string, unknown> = {
        raw_name: draft.raw_name.trim(),
        qty: Number(draft.qty),
      }
      if (draft.item_code.trim()) payload.item_code = draft.item_code.trim()
      if (draft.unit_code.trim()) payload.unit_code = draft.unit_code.trim()
      if (Number(draft.price) > 0) payload.price = Number(draft.price)

      const res = await api.post<BillItem>(`/api/bills/${billId}/items`, payload)
      onAdded(res.data)
      reset()
      setOpen(false)
    } catch (err) {
      console.error('add item failed', err)
    } finally {
      setAdding(false)
    }
  }

  if (!open) {
    return (
      <div className="mt-3">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setOpen(true)}
          className="gap-1.5"
        >
          <Plus className="h-3.5 w-3.5" />
          เพิ่มรายการสินค้า
        </Button>
      </div>
    )
  }

  return (
    <Card className="mt-3 border-dashed">
      <CardContent className="pt-4 pb-3">
        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="grid grid-cols-[2fr_1fr_1fr_1fr] gap-2">
            <Input
              placeholder="ชื่อสินค้าจากต้นทาง"
              value={draft.raw_name}
              onChange={(e) => setDraft((d) => ({ ...d, raw_name: e.target.value }))}
              autoFocus
              required
              className="h-8 text-sm"
            />
            <Input
              placeholder="รหัสสินค้า (ถ้ามี)"
              value={draft.item_code}
              onChange={(e) => setDraft((d) => ({ ...d, item_code: e.target.value }))}
              className="h-8 text-sm font-mono"
            />
            <Input
              placeholder="หน่วย"
              value={draft.unit_code}
              onChange={(e) => setDraft((d) => ({ ...d, unit_code: e.target.value }))}
              className="h-8 text-sm"
            />
            <Input
              type="number"
              step="any"
              min="0"
              placeholder="ราคา/หน่วย"
              value={draft.price}
              onChange={(e) => setDraft((d) => ({ ...d, price: e.target.value }))}
              className="h-8 text-sm"
            />
          </div>
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">จำนวน:</span>
              <Input
                type="number"
                step="any"
                min="0"
                value={draft.qty}
                onChange={(e) => setDraft((d) => ({ ...d, qty: e.target.value }))}
                className="h-8 w-20 text-sm"
              />
            </div>
            <Button type="submit" size="sm" className="h-8" disabled={adding}>
              <Plus className="h-3.5 w-3.5" />
              {adding ? 'กำลังเพิ่ม...' : 'เพิ่มรายการ'}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8"
              onClick={() => {
                reset()
                setOpen(false)
              }}
              disabled={adding}
            >
              <X className="h-3.5 w-3.5" />
              ยกเลิก
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
