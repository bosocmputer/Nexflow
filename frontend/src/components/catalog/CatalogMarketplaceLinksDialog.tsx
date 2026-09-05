import { useCallback, useEffect, useRef, useState } from 'react'
import { AlertCircle, Link2, Loader2, Store } from 'lucide-react'

import api from '@/api/client'
import { MarketplaceSourceChannelBadges } from '@/components/marketplace/InputChannelBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { marketplaceDisplayInputChannels } from '@/lib/billInputChannel'
import type { CatalogMarketplaceLink } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  itemCode: string
  itemName: string
}

interface MarketplaceLinkPage {
  data: CatalogMarketplaceLink[]
  has_more: boolean
  next_cursor: string
}

export function CatalogMarketplaceLinksDialog({ open, onOpenChange, itemCode, itemName }: Props) {
  const [links, setLinks] = useState<CatalogMarketplaceLink[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const requestSequence = useRef(0)

  const load = useCallback(async (cursor = '', append = false) => {
    if (!itemCode) return
    const requestID = ++requestSequence.current
    setLoading(true)
    setError('')
    try {
      const response = await api.get<MarketplaceLinkPage>(`/api/catalog/${encodeURIComponent(itemCode)}/marketplace-links`, {
        params: { limit: 25, cursor: cursor || undefined },
      })
      if (requestID !== requestSequence.current) return
      const page = response.data.data ?? []
      setLinks((current) => append ? [...current, ...page] : page)
      setNextCursor(response.data.next_cursor ?? '')
      setHasMore(Boolean(response.data.has_more))
    } catch {
      if (requestID !== requestSequence.current) return
      setError('โหลดสินค้าที่จับคู่ไม่สำเร็จ กรุณาลองใหม่')
    } finally {
      if (requestID === requestSequence.current) setLoading(false)
    }
  }, [itemCode])

  useEffect(() => {
    if (!open || !itemCode) return
    setLinks([])
    setNextCursor('')
    setHasMore(false)
    void load()
    return () => { requestSequence.current += 1 }
  }, [itemCode, load, open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-hidden p-0">
        <DialogHeader className="border-b px-5 py-4 text-left">
          <DialogTitle className="flex flex-wrap items-center gap-2">
            <Store className="h-4 w-4 text-primary" />
            สินค้าที่จับคู่ใน Marketplace
            {!loading && links.length > 0 && <Badge variant="outline">{links.length.toLocaleString()} ตัวเลือก</Badge>}
          </DialogTitle>
          <DialogDescription><span className="font-mono text-foreground">{itemCode}</span> · {itemName}</DialogDescription>
        </DialogHeader>

        <div className="min-h-0 overflow-y-auto px-5 py-4">
          {loading && links.length === 0 ? (
            <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground" role="status">
              <Loader2 className="h-4 w-4 animate-spin" />กำลังโหลดข้อมูลการจับคู่
            </div>
          ) : error && links.length === 0 ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm">
              <div className="flex items-center gap-2 text-destructive"><AlertCircle className="h-4 w-4" />{error}</div>
              <Button className="mt-3" size="sm" variant="outline" onClick={() => void load()}>ลองใหม่</Button>
            </div>
          ) : links.length === 0 ? (
            <div className="rounded-md border border-dashed px-4 py-10 text-center">
              <Link2 className="mx-auto mb-2 h-5 w-5 text-muted-foreground" />
              <p className="text-sm font-medium">สินค้านี้ยังไม่จับคู่กับ Marketplace</p>
              <p className="mt-1 text-xs text-muted-foreground">จับคู่ได้จากหน้า “จับคู่สินค้า Marketplace”</p>
            </div>
          ) : (
            <div className="overflow-hidden rounded-md border">
              <div className="divide-y">
                {links.map((link) => {
                  const showVariant = link.variant_name && link.variant_name !== link.product_name
                  const needsReview = link.conversion_status !== 'ready' || !link.scope_confirmed
                  return (
                    <div key={link.id} className="space-y-3 px-4 py-4">
                      <div className="flex flex-wrap items-center gap-2">
                        <MarketplaceSourceChannelBadges
                          source={link.source}
                          accountName={link.account_name}
                          channels={marketplaceDisplayInputChannels(link.source, { inputChannels: link.input_channels })}
                        />
                        {needsReview && <Badge variant="outline" className="border-warning/40 bg-warning/10 text-warning">รอตรวจสอบ</Badge>}
                      </div>
                      <div className="min-w-0 space-y-1">
                        <p className="break-words font-medium leading-snug">{link.product_name || 'ไม่มีชื่อสินค้า'}</p>
                        {showVariant && <p className="break-words text-sm text-muted-foreground">ตัวเลือก: <span className="text-foreground">{link.variant_name}</span></p>}
                        <div className="flex flex-wrap gap-x-3 gap-y-1 pt-1 text-xs text-muted-foreground">
                          {link.source_sku && <span>SKU: <code className="text-foreground">{link.source_sku}</code></span>}
                          {link.quantity_multiplier > 1 && <span>Marketplace 1 รายการ = {link.quantity_multiplier.toLocaleString()} {link.unit_code || 'หน่วย'} SML</span>}
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {error && links.length > 0 && <p className="mt-3 text-sm text-destructive">{error}</p>}
          {hasMore && (
            <div className="mt-3 text-center">
              <Button variant="outline" size="sm" disabled={loading || !nextCursor} onClick={() => void load(nextCursor, true)}>
                {loading && <Loader2 className="h-4 w-4 animate-spin" />}โหลดเพิ่มเติม
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
