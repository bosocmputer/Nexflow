import { useEffect, useState } from 'react'
import { Check, ChevronDown, ChevronRight, Save, Settings as SettingsIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import type { PlatformColumnMapping } from '@/types'

// Lazada column mapping lives here (next to the file uploader) so the admin
// configuring import doesn't have to bounce through /settings to set up
// column names, then come back to /import to upload. Collapsed by default
// — the assumption is "set this once at install time, never touch again".
const FIELDS: Array<{ key: string; label: string }> = [
  { key: 'order_id', label: 'หมายเลขออเดอร์' },
  { key: 'buyer_name', label: 'ชื่อลูกค้า' },
  { key: 'buyer_phone', label: 'เบอร์โทร' },
  { key: 'item_name', label: 'ชื่อสินค้า' },
  { key: 'sku', label: 'SKU' },
  { key: 'qty', label: 'จำนวน' },
  { key: 'price', label: 'ราคาต่อหน่วย' },
]

interface Props {
  // platform is 'lazada' for now — passed in so this can later support shopee
  // too without renaming the component. Shopee currently hardcodes columns
  // in the backend, so the editor stays Lazada-only in practice.
  platform: 'lazada' | 'shopee'
  // adminOnly — when false, the card is hidden entirely (staff users).
  adminOnly: boolean
}

export function LazadaColumnMapping({ platform, adminOnly }: Props) {
  const [open, setOpen] = useState(false)
  const [mappings, setMappings] = useState<PlatformColumnMapping[]>([])
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  // Lazy-load only when the card is opened — saves a request on every page
  // visit since admin rarely opens this section.
  useEffect(() => {
    if (!open || loaded) return
    client
      .get<{ mappings: PlatformColumnMapping[] }>(`/api/settings/column-mappings/${platform}`)
      .then((r) => {
        const map = new Map(r.data.mappings.map((m) => [m.field_name, m]))
        setMappings(
          FIELDS.map(
            (f) =>
              map.get(f.key) ?? { platform, field_name: f.key, column_name: '' },
          ),
        )
        setLoaded(true)
      })
      .catch(() => {
        setMappings(
          FIELDS.map((f) => ({ platform, field_name: f.key, column_name: '' })),
        )
        setLoaded(true)
      })
  }, [open, loaded, platform])

  if (!adminOnly) return null

  // Count how many fields actually have values configured — surfaces in the
  // collapsed-state header so admin knows whether mapping is set up.
  const configuredCount = mappings.filter((m) => m.column_name.trim() !== '').length

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    setSaved(false)
    try {
      await client.put(`/api/settings/column-mappings/${platform}`, { mappings })
      setSaved(true)
      setTimeout(() => setSaved(false), 2500)
    } catch (e: unknown) {
      const err = e as { response?: { data?: { error?: string } } }
      setError(err?.response?.data?.error ?? 'บันทึกไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2.5 px-5 py-3 text-left transition-colors hover:bg-accent/30"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
        )}
        <SettingsIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold">Column Mapping ({platform})</div>
          <div className="text-[11px] text-muted-foreground">
            จับคู่ชื่อ column ในไฟล์ Excel ของ {platform} กับ field ที่ระบบใช้
            {loaded && (
              <span className={cn('ml-2', configuredCount === FIELDS.length ? 'text-success' : 'text-warning')}>
                · {configuredCount} / {FIELDS.length} ตั้งค่าแล้ว
              </span>
            )}
          </div>
        </div>
      </button>

      {open && (
        <CardContent className="border-t border-border px-5 pt-4">
          <div className="overflow-hidden rounded-lg border border-border">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/40">
                  <TableHead className="w-1/3">Field</TableHead>
                  <TableHead>ชื่อ Column ในไฟล์ Excel</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {FIELDS.map((f) => {
                  const m = mappings.find((x) => x.field_name === f.key)
                  return (
                    <TableRow key={f.key}>
                      <TableCell>
                        <div className="font-mono text-xs font-medium text-foreground">
                          {f.key}
                        </div>
                        <div className="mt-0.5 text-xs text-muted-foreground">
                          {f.label}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Input
                          value={m?.column_name ?? ''}
                          onChange={(e) =>
                            setMappings((prev) =>
                              prev.map((x) =>
                                x.field_name === f.key
                                  ? { ...x, column_name: e.target.value }
                                  : x,
                              ),
                            )
                          }
                          placeholder="เช่น Order ID, ชื่อสินค้า…"
                          className="h-8"
                        />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
          <div className="mt-3 flex items-center gap-3">
            <Button size="sm" onClick={handleSave} disabled={saving}>
              <Save className="h-3.5 w-3.5" />
              {saving ? 'กำลังบันทึก…' : 'บันทึก mapping'}
            </Button>
            {saved && (
              <span className="inline-flex items-center gap-1 text-xs text-success">
                <Check className="h-3 w-3" />
                บันทึกแล้ว
              </span>
            )}
            {error && <span className="text-xs text-destructive">{error}</span>}
          </div>
        </CardContent>
      )}
    </Card>
  )
}
