import { useEffect, useState } from 'react'
import { ImageIcon, Search } from 'lucide-react'

import api from '@/api/client'
import { AuthImage } from '@/components/common/AuthImage'
import { ProductImagePreviewDialog } from '@/components/common/ProductImagePreviewDialog'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import type { CatalogMatch } from '@/types'

interface Props {
  open: boolean
  rawName: string
  currentCode: string
  currentUnit: string
  currentPrice: number
  sourceImageUrl?: string
  rawNameLabel?: string
  onPick: (code: string, unitCode: string, picked?: CatalogMatch) => void
  onClose: () => void
}

export function MapItemModal({
  open,
  rawName,
  currentCode,
  currentUnit,
  sourceImageUrl,
  rawNameLabel = 'ชื่อสินค้าจากต้นทาง',
  onPick,
  onClose,
}: Props) {
  const [query, setQuery] = useState(rawName.slice(0, 100))
  const [results, setResults] = useState<CatalogMatch[]>([])
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [previewMatch, setPreviewMatch] = useState<CatalogMatch | null>(null)

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
        const response = await api.get<{ results: CatalogMatch[] }>('/api/catalog/search', {
          params: { q, top: 20 },
        })
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
      <Dialog open={open} onOpenChange={(value) => !value && onClose()}>
        <DialogContent className="grid max-h-[88vh] max-w-3xl grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Search className="h-4 w-4 text-muted-foreground" />
              เลือกสินค้าจาก SML
            </DialogTitle>
          </DialogHeader>

          <div className="rounded-md border bg-muted/30 p-3 text-sm">
            <div className="flex gap-3">
              {sourceImageUrl && (
                <img src={sourceImageUrl} alt="" className="h-16 w-16 shrink-0 rounded-md border object-cover" />
              )}
              <div className="min-w-0">
                <div className="text-xs font-medium text-muted-foreground">{rawNameLabel}</div>
                <div className="mt-1 line-clamp-2 break-words font-medium">{rawName}</div>
                {currentCode && (
                  <div className="mt-1 text-xs text-muted-foreground">
                    ปัจจุบัน: <code className="text-foreground">{currentCode}</code> ({currentUnit || '-'})
                  </div>
                )}
              </div>
            </div>
          </div>

          <Input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="ค้นหาด้วยรหัสหรือชื่อสินค้า อย่างน้อย 2 ตัวอักษร"
          />

          <div className="min-h-0 overflow-y-auto pr-1">
            {searching && <p className="py-8 text-center text-sm text-muted-foreground">กำลังค้นหา...</p>}
            {!searching && searchError && <p className="py-8 text-center text-sm text-destructive">{searchError}</p>}
            {!searching && !searchError && query.trim().length >= 2 && results.length === 0 && (
              <div className="rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground">
                ไม่พบสินค้าใน SML ตรวจรหัสหรือไปเพิ่มสินค้าที่เมนูสินค้าใน SML ก่อน
              </div>
            )}
            <div className="space-y-1.5">
              {results.map((item) => {
                const hasImage = Boolean(item.image_url && (item.image_count ?? 0) > 0)
                return (
                  <div key={item.item_code} className="flex min-h-16 items-center gap-3 rounded-md border bg-background p-2 hover:bg-muted/35">
                    <button
                      type="button"
                      className="h-12 w-12 shrink-0 rounded-md"
                      disabled={!hasImage}
                      onClick={() => hasImage && setPreviewMatch(item)}
                      aria-label={`ดูรูป ${item.item_code}`}
                    >
                      <AuthImage
                        src={hasImage ? item.image_url : undefined}
                        className="h-full w-full rounded-md border bg-muted/30"
                        imgClassName="object-cover"
                        fallback={<div className="flex h-full items-center justify-center text-muted-foreground"><ImageIcon className="h-4 w-4" /></div>}
                      />
                    </button>
                    <button
                      type="button"
                      className="min-w-0 flex-1 text-left"
                      onClick={() => {
                        onPick(item.item_code, item.unit_code, item)
                        onClose()
                      }}
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-sm font-semibold">{item.item_code}</span>
                        <Badge variant="outline">หน่วย {item.unit_code || '-'}</Badge>
                      </div>
                      <div className="mt-1 line-clamp-2 text-sm text-muted-foreground">{item.item_name}</div>
                    </button>
                  </div>
                )
              })}
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <ProductImagePreviewDialog
        open={previewMatch !== null}
        onOpenChange={(value) => !value && setPreviewMatch(null)}
        itemCode={previewMatch?.item_code ?? ''}
        itemName={previewMatch?.item_name ?? ''}
        imageUrl={previewMatch?.image_url}
        imageCount={previewMatch?.image_count ?? 0}
      />
    </>
  )
}
