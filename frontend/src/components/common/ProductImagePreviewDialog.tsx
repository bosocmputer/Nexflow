import { useEffect, useMemo, useState } from 'react'
import { ChevronLeft, ChevronRight, ImageIcon } from 'lucide-react'

import { AuthImage } from '@/components/common/AuthImage'
import api from '@/api/client'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { CatalogImage } from '@/types'

interface ProductImagePreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  imageUrl?: string
  itemCode?: string
  itemName?: string
  imageCount?: number
}

export function ProductImagePreviewDialog({
  open,
  onOpenChange,
  imageUrl,
  itemCode,
  itemName,
  imageCount = 0,
}: ProductImagePreviewDialogProps) {
  const [images, setImages] = useState<CatalogImage[]>([])
  const [activeIndex, setActiveIndex] = useState(0)
  const [failedList, setFailedList] = useState(false)

  useEffect(() => {
    setImages([])
    setActiveIndex(0)
    setFailedList(false)
  }, [imageUrl, itemCode])

  useEffect(() => {
    if (!open || !itemCode || imageCount <= 1) return
    let cancelled = false
    api
      .get<{ images?: CatalogImage[] }>(`/api/catalog/${encodeURIComponent(itemCode)}/images`)
      .then((res) => {
        if (cancelled) return
        const next = (res.data.images ?? []).filter((img) => img.image_url)
        setImages(next)
        const primaryIndex = next.findIndex((img) => img.image_url === imageUrl)
        setActiveIndex(primaryIndex >= 0 ? primaryIndex : 0)
      })
      .catch(() => {
        if (!cancelled) setFailedList(true)
      })
    return () => {
      cancelled = true
    }
  }, [open, itemCode, imageCount, imageUrl])

  const gallery = useMemo<CatalogImage[]>(() => {
    if (images.length > 0) return images
    return imageUrl ? [{ roworder: 0, image_url: imageUrl }] : []
  }, [images, imageUrl])

  const active = gallery[Math.min(activeIndex, Math.max(0, gallery.length - 1))]
  const canNavigate = gallery.length > 1
  const go = (delta: number) => {
    if (!canNavigate) return
    setActiveIndex((current) => (current + delta + gallery.length) % gallery.length)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] max-w-4xl overflow-hidden p-4 sm:p-5">
        <DialogHeader className="pr-8">
          <DialogTitle className="flex min-w-0 flex-wrap items-center gap-2 text-base">
            <span className="truncate">{itemCode || 'รูปสินค้า'}</span>
            {imageCount > 1 && (
              <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                {gallery.length > 1 ? `${activeIndex + 1}/${gallery.length}` : `${imageCount} รูป`}
              </Badge>
            )}
          </DialogTitle>
          {itemName && (
            <div className="line-clamp-2 break-words text-sm leading-5 text-muted-foreground">
              {itemName}
            </div>
          )}
        </DialogHeader>

        <div className="space-y-3">
          <div className="relative">
            <AuthImage
              src={active?.image_url}
              alt={itemName || itemCode || 'product image'}
              className="flex min-h-[280px] max-h-[70vh] w-full items-center justify-center rounded-lg border border-border bg-muted/25"
              imgClassName="h-full max-h-[70vh] w-full object-contain"
              fallback={
                <div className="flex min-h-[280px] w-full items-center justify-center text-muted-foreground">
                  <ImageIcon className="h-10 w-10" />
                </div>
              }
            />
            {canNavigate && (
              <>
                <Button
                  type="button"
                  size="icon"
                  variant="secondary"
                  className="absolute left-3 top-1/2 h-9 w-9 -translate-y-1/2 rounded-full bg-background/85 shadow"
                  onClick={() => go(-1)}
                  aria-label="รูปก่อนหน้า"
                >
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <Button
                  type="button"
                  size="icon"
                  variant="secondary"
                  className="absolute right-3 top-1/2 h-9 w-9 -translate-y-1/2 rounded-full bg-background/85 shadow"
                  onClick={() => go(1)}
                  aria-label="รูปถัดไป"
                >
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </>
            )}
          </div>

          {canNavigate && (
            <div className="flex gap-2 overflow-x-auto pb-1">
              {gallery.map((img, index) => (
                <button
                  key={`${img.roworder}-${img.image_url}`}
                  type="button"
                  className={cn(
                    'h-14 w-14 shrink-0 rounded-md border bg-muted/25 p-0.5 outline-none ring-offset-background transition-colors',
                    index === activeIndex
                      ? 'border-primary ring-2 ring-primary/25'
                      : 'border-border hover:border-primary/60',
                  )}
                  onClick={() => setActiveIndex(index)}
                  aria-label={`ดูรูปที่ ${index + 1}`}
                >
                  <AuthImage
                    src={img.image_url}
                    alt=""
                    className="h-full w-full rounded-[4px]"
                    imgClassName="object-cover"
                    fallback={
                      <div className="flex h-full w-full items-center justify-center text-muted-foreground">
                        <ImageIcon className="h-4 w-4" />
                      </div>
                    }
                  />
                </button>
              ))}
            </div>
          )}

          {failedList && imageCount > 1 && (
            <p className="text-xs text-muted-foreground">
              โหลดรายการรูปเพิ่มเติมไม่ได้ในตอนนี้ แสดงรูปหลักแทน
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
