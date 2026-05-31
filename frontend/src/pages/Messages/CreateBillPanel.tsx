import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, Trash2, X } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import client from '@/api/client'
import type { CatalogMatch } from '@/types'
import type { ChatConversation, ExtractedBill } from './types'

interface BillItemDraft {
  item_code: string
  raw_name: string
  unit_code: string
  qty: number
  price: number
}

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  lineUserID: string
  conversation: ChatConversation | null
  // When prefill is set (from ExtractPreviewDialog), the items + customer
  // fields seed from that data. Otherwise empty starting state.
  prefill: ExtractedBill | null
}

const emptyDraft = (): BillItemDraft => ({
  item_code: '',
  raw_name: '',
  unit_code: '',
  qty: 1,
  price: 0,
})

// CreateBillPanel is the dialog opened by the "เปิดบิลขาย" button. Lets the
// admin search the SML catalog, build a list of items, set qty/price, and
// submit. On success the bill is created (status=pending, source=line) and
// we navigate to /bills/:id where the existing Retry button does the SML push.
export function CreateBillPanel({
  open,
  onOpenChange,
  lineUserID,
  conversation,
  prefill,
}: Props) {
  const navigate = useNavigate()
  const [customerName, setCustomerName] = useState('')
  const [customerPhone, setCustomerPhone] = useState('')
  const [items, setItems] = useState<BillItemDraft[]>([emptyDraft()])
  const [submitting, setSubmitting] = useState(false)

  // Catalog search (debounced) — shared search box on top of the items list.
  const [searchQ, setSearchQ] = useState('')
  const [searchResults, setSearchResults] = useState<CatalogMatch[]>([])
  const [searching, setSearching] = useState(false)

  // Reset / prefill on open.
  useEffect(() => {
    if (!open) return
    if (prefill) {
      setCustomerName(prefill.customer_name ?? conversation?.display_name ?? '')
      // Prefer the AI-extracted phone, but fall back to whatever the admin
      // saved on the conversation (Phase 4.7 "บันทึกเบอร์" button).
      setCustomerPhone(prefill.customer_phone ?? conversation?.phone ?? '')
      setItems(
        prefill.items.length > 0
          ? prefill.items.map((it) => ({
              item_code: '',
              raw_name: it.raw_name,
              unit_code: it.unit ?? '',
              qty: it.qty || 1,
              price: it.price ?? 0,
            }))
          : [emptyDraft()],
      )
    } else {
      setCustomerName(conversation?.display_name ?? '')
      setCustomerPhone(conversation?.phone ?? '')
      setItems([emptyDraft()])
    }
    setSearchQ('')
    setSearchResults([])
  }, [open, prefill, conversation])

  // Debounced catalog search.
  useEffect(() => {
    const q = searchQ.trim()
    if (q.length < 2) {
      setSearchResults([])
      return
    }
    const handle = setTimeout(async () => {
      setSearching(true)
      try {
        const res = await client.get<{ results: CatalogMatch[] }>(
          '/api/catalog/search',
          { params: { q, top: 8 } },
        )
        setSearchResults(res.data.results ?? [])
      } catch {
        setSearchResults([])
      } finally {
        setSearching(false)
      }
    }, 250)
    return () => clearTimeout(handle)
  }, [searchQ])

  const addItemFromCatalog = (m: CatalogMatch) => {
    setItems((prev) => {
      // If the last row is empty, replace it instead of appending.
      const last = prev[prev.length - 1]
      const replaceLast = last && !last.item_code && !last.raw_name
      const newItem: BillItemDraft = {
        item_code: m.item_code,
        raw_name: m.item_name,
        unit_code: m.unit_code || '',
        qty: 1,
        price: 0,
      }
      if (replaceLast) {
        return [...prev.slice(0, -1), newItem]
      }
      return [...prev, newItem]
    })
    setSearchQ('')
    setSearchResults([])
  }

  const updateItem = (idx: number, patch: Partial<BillItemDraft>) => {
    setItems((prev) => prev.map((it, i) => (i === idx ? { ...it, ...patch } : it)))
  }

  const removeItem = (idx: number) => {
    setItems((prev) => (prev.length === 1 ? [emptyDraft()] : prev.filter((_, i) => i !== idx)))
  }

  const submit = async () => {
    const valid = items.filter((it) => it.raw_name.trim() && it.qty > 0)
    if (valid.length === 0) {
      toast.error('กรุณาเพิ่มอย่างน้อย 1 รายการ')
      return
    }
    setSubmitting(true)
    try {
      const res = await client.post<{ bill_id: string; message: string }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/bills`,
        {
          customer_name: customerName.trim(),
          customer_phone: customerPhone.trim(),
          items: valid.map((it) => ({
            item_code: it.item_code.trim(),
            raw_name: it.raw_name.trim(),
            unit_code: it.unit_code.trim(),
            qty: Number(it.qty) || 1,
            price: Number(it.price) || 0,
          })),
        },
      )
      toast.success('สร้างบิลแล้ว — ไปยังบิล')
      onOpenChange(false)
      navigate(`/bills/${res.data.bill_id}`)
    } catch (e: any) {
      toast.error('สร้างบิลล้มเหลว: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[90vh] max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto]">
        <DialogHeader>
          <DialogTitle>เปิดบิลขาย — {conversation?.display_name || lineUserID.slice(0, 12)}</DialogTitle>
        </DialogHeader>

        <div className="-mx-6 space-y-4 overflow-y-auto px-6 py-2">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label className="text-xs">ชื่อลูกค้า</Label>
              <Input
                value={customerName}
                onChange={(e) => setCustomerName(e.target.value)}
                placeholder="ชื่อสำหรับหัวบิล (default = ชื่อ LINE)"
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">เบอร์โทร</Label>
              <Input
                value={customerPhone}
                onChange={(e) => setCustomerPhone(e.target.value)}
                placeholder="(ไม่บังคับ)"
              />
            </div>
          </div>

          <div className="rounded-md border border-border bg-muted/30 p-3">
            <div className="mb-2 flex items-center gap-2">
              <Search className="h-3.5 w-3.5 text-muted-foreground" />
              <Input
                value={searchQ}
                onChange={(e) => setSearchQ(e.target.value)}
                placeholder="ค้นหาสินค้าจาก catalog SML…"
                className="h-8 text-sm"
              />
            </div>
            {searching && (
              <div className="text-xs text-muted-foreground">กำลังค้น…</div>
            )}
            {!searching && searchResults.length > 0 && (
              <ul className="divide-y divide-border rounded-md border border-border bg-card">
                {searchResults.map((m) => (
                  <li key={m.item_code}>
                    <button
                      type="button"
                      onClick={() => addItemFromCatalog(m)}
                      className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left text-xs hover:bg-accent/40"
                    >
                      <span className="shrink-0 font-mono">{m.item_code}</span>
                      <span className="min-w-0 flex-1 truncate">{m.item_name}</span>
                      <span className="shrink-0 text-muted-foreground">{m.unit_code || '—'}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-xs">รายการสินค้า</Label>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs"
                onClick={() => setItems((p) => [...p, emptyDraft()])}
              >
                + เพิ่มแถว
              </Button>
            </div>
            <div className="overflow-hidden rounded-md border border-border">
              <table className="w-full text-xs">
                <thead className="bg-muted/40 text-[10px] uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th className="px-2 py-1.5 text-left">รหัสสินค้า</th>
                    <th className="px-2 py-1.5 text-left">ชื่อสินค้า</th>
                    <th className="px-2 py-1.5 text-right">จำนวน</th>
                    <th className="px-2 py-1.5 text-right">หน่วย</th>
                    <th className="px-2 py-1.5 text-right">ราคา/หน่วย</th>
                    <th className="w-8"></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((it, idx) => (
                    <tr key={idx} className="border-t border-border">
                      <td className="px-1 py-1">
                        <Input
                          value={it.item_code}
                          onChange={(e) => updateItem(idx, { item_code: e.target.value })}
                          placeholder="(ค้นหาด้านบน)"
                          className="h-7 font-mono text-xs"
                        />
                      </td>
                      <td className="px-1 py-1">
                        <Input
                          value={it.raw_name}
                          onChange={(e) => updateItem(idx, { raw_name: e.target.value })}
                          placeholder="ชื่อสินค้า"
                          className="h-7 text-xs"
                        />
                      </td>
                      <td className="px-1 py-1">
                        <Input
                          type="number"
                          step="0.01"
                          value={it.qty}
                          onChange={(e) =>
                            updateItem(idx, { qty: Number(e.target.value) || 0 })
                          }
                          className="h-7 w-20 text-right text-xs tabular-nums"
                        />
                      </td>
                      <td className="px-1 py-1">
                        <Input
                          value={it.unit_code}
                          onChange={(e) => updateItem(idx, { unit_code: e.target.value })}
                          placeholder="ชิ้น"
                          className="h-7 w-20 text-xs"
                        />
                      </td>
                      <td className="px-1 py-1">
                        <Input
                          type="number"
                          step="0.01"
                          value={it.price}
                          onChange={(e) =>
                            updateItem(idx, { price: Number(e.target.value) || 0 })
                          }
                          className="h-7 w-24 text-right text-xs tabular-nums"
                        />
                      </td>
                      <td className="px-1 py-1 text-right">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-7 px-1.5 text-destructive"
                          onClick={() => removeItem(idx)}
                        >
                          <X className="h-3.5 w-3.5" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="text-[11px] text-muted-foreground">
              💡 ค้นหา catalog ด้านบนเพื่อเพิ่มแถวพร้อม code + ชื่อ + หน่วย จาก SML.
              ราคาใส่เอง.
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            ยกเลิก
          </Button>
          <Button onClick={submit} disabled={submitting}>
            {submitting ? 'กำลังสร้าง…' : 'สร้างบิล'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Suppress unused-import warning for Trash2 (tree-shaking — kept here so it's
// easy to swap the X icon if we want a "real" delete affordance later).
void Trash2
