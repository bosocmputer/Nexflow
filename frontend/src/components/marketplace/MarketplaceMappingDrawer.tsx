import { useEffect, useState, type ReactNode } from 'react'
import { ImageIcon, Search } from 'lucide-react'

import api from '@/api/client'
import { AuthImage } from '@/components/common/AuthImage'
import { ProductImagePreviewDialog } from '@/components/common/ProductImagePreviewDialog'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import type { CatalogMatch } from '@/types'

interface Props {
  open: boolean
  rawName: string
  currentCode: string
  currentUnit: string
  sourceImageUrl?: string
  rawNameLabel?: string
  onPick: (code: string, unitCode: string, picked?: CatalogMatch) => void
  onOpenChange: (open: boolean) => void
  closeOnPick?: boolean
  children?: ReactNode
  footer?: ReactNode
}

export function MarketplaceMappingDrawer({
  open,
  rawName,
  currentCode,
  currentUnit,
  sourceImageUrl,
  rawNameLabel = 'ชื่อสินค้าจาก Marketplace',
  onPick,
  onOpenChange,
  closeOnPick = true,
  children,
  footer,
}: Props) {
  const [query, setQuery] = useState(rawName.slice(0, 100))
  const [results, setResults] = useState<CatalogMatch[]>([])
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [previewMatch, setPreviewMatch] = useState<CatalogMatch | null>(null)

  useEffect(() => {
    if (!open) return
    setQuery(currentCode || rawName.slice(0, 100))
  }, [open, rawName, currentCode])

  useEffect(() => {
    if (!open) return
    const q = query.trim()
    if (q.length < 2) {
      setResults([])
      return
    }
    let active = true
    const handle = window.setTimeout(async () => {
      setSearching(true)
      setSearchError('')
      try {
        const response = await api.get<{ results: CatalogMatch[] }>('/api/catalog/search', { params: { q, top: 20 } })
        if (active) setResults(response.data.results ?? [])
      } catch {
        if (active) setSearchError('ค้นหาสินค้า SML ไม่สำเร็จ กรุณาลองใหม่')
      } finally {
        if (active) setSearching(false)
      }
    }, 250)
    return () => {
      active = false
      window.clearTimeout(handle)
    }
  }, [open, query])

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
          <SheetHeader className="border-b px-4 py-3 text-left">
            <SheetTitle className="flex items-center gap-2"><Search className="h-4 w-4 text-muted-foreground" />จับคู่สินค้า Marketplace</SheetTitle>
            <SheetDescription>เลือกสินค้าปลายทางจากรายการสินค้า SML</SheetDescription>
          </SheetHeader>

          <div className="border-b p-4">
            <div className="flex gap-3 rounded-md border bg-muted/30 p-3 text-sm">
              {sourceImageUrl && <img src={sourceImageUrl} alt="" className="h-14 w-14 shrink-0 rounded-md border object-cover" />}
              <div className="min-w-0">
                <div className="text-xs font-medium text-muted-foreground">{rawNameLabel}</div>
                <div className="mt-1 line-clamp-2 break-words font-medium">{rawName}</div>
                {currentCode && <div className="mt-1 text-xs text-muted-foreground">ปัจจุบัน: <code className="text-foreground">{currentCode}</code> ({currentUnit || '-'})</div>}
              </div>
            </div>
            <Input className="mt-3" autoFocus value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ค้นหาด้วยรหัสหรือชื่อสินค้า อย่างน้อย 2 ตัวอักษร" />
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            {children}
            {searching && <p className="py-8 text-center text-sm text-muted-foreground">กำลังค้นหา...</p>}
            {!searching && searchError && <p className="py-8 text-center text-sm text-destructive">{searchError}</p>}
            {!searching && !searchError && query.trim().length >= 2 && results.length === 0 && (
              <div className="rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground">ไม่พบสินค้าใน SML ตรวจรหัสหรือรีเฟรชที่เมนูรายการสินค้า SML</div>
            )}
            <div className="space-y-1.5">
              {results.map((item) => {
                const hasImage = Boolean(item.image_url && (item.image_count ?? 0) > 0)
                return (
                  <div key={item.item_code} className="flex min-h-16 items-center gap-3 rounded-md border bg-background p-2 hover:bg-muted/35">
                    <button type="button" className="h-12 w-12 shrink-0 rounded-md" disabled={!hasImage} onClick={() => hasImage && setPreviewMatch(item)} aria-label={`ดูรูป ${item.item_code}`}>
                      <AuthImage src={hasImage ? item.image_url : undefined} className="h-full w-full rounded-md border bg-muted/30" imgClassName="object-cover" fallback={<div className="flex h-full items-center justify-center text-muted-foreground"><ImageIcon className="h-4 w-4" /></div>} />
                    </button>
                    <button type="button" className="min-w-0 flex-1 text-left" onClick={() => { onPick(item.item_code, item.unit_code, item); if (closeOnPick) onOpenChange(false) }}>
                      <div className="flex flex-wrap items-center gap-2"><span className="font-mono text-sm font-semibold">{item.item_code}</span><Badge variant="outline">หน่วย {item.unit_code || '-'}</Badge></div>
                      <div className="mt-1 line-clamp-2 text-sm text-muted-foreground">{item.item_name}</div>
                    </button>
                  </div>
                )
              })}
            </div>
          </div>
          {footer && <div className="border-t p-4">{footer}</div>}
        </SheetContent>
      </Sheet>

      <ProductImagePreviewDialog open={previewMatch !== null} onOpenChange={(value) => !value && setPreviewMatch(null)} itemCode={previewMatch?.item_code ?? ''} itemName={previewMatch?.item_name ?? ''} imageUrl={previewMatch?.image_url} imageCount={previewMatch?.image_count ?? 0} />
    </>
  )
}
