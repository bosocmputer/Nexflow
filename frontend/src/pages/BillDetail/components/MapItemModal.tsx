import { useState, useEffect } from 'react'
import { ArrowLeft, CheckCircle2, ImageIcon, Plus, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { AuthImage } from '@/components/common/AuthImage'
import { ProductImagePreviewDialog } from '@/components/common/ProductImagePreviewDialog'
import { UnitSelect } from '@/components/common/UnitSelect'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import api from '@/api/client'
import type { CatalogMatch } from '@/types'
import { scoreBorderClass, scoreStyle } from '../utils/formatters'

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

function ScorePill({ score, recommended = false }: { score: number; recommended?: boolean }) {
  const pct = Math.round(score * 100)
  const s = scoreStyle(score)
  return (
    <div className="flex min-w-[72px] flex-col items-end gap-0.5">
      <span
        className={cn(
          'inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[11px] font-bold tabular-nums',
          s.bg,
          s.color,
        )}
      >
        {recommended && <CheckCircle2 className="h-3 w-3" />}
        {pct}%
      </span>
      <div className="h-1 w-14 overflow-hidden rounded-full bg-muted">
        <div
          className={cn(
            'h-full rounded-full',
            score >= 0.85 ? 'bg-success' : score >= 0.6 ? 'bg-warning' : 'bg-destructive',
          )}
          style={{ width: `${Math.max(4, Math.min(100, pct))}%` }}
        />
      </div>
    </div>
  )
}

function CatalogMatchThumbnail({
  match,
  onPreview,
}: {
  match: CatalogMatch
  onPreview: (match: CatalogMatch) => void
}) {
  const count = match.image_count ?? 0
  const hasImage = Boolean(match.image_url && count > 0)
  const image = (
    <AuthImage
      src={hasImage ? match.image_url : undefined}
      className="h-full w-full rounded-md border border-border bg-muted/35"
      imgClassName="object-cover"
      fallback={
        <div className="flex h-full w-full items-center justify-center text-muted-foreground">
          <ImageIcon className="h-4 w-4" />
        </div>
      }
    >
      {count > 1 && (
        <span className="absolute bottom-0.5 right-0.5 rounded bg-background/90 px-1 text-[10px] font-medium tabular-nums text-foreground shadow-sm">
          {count}
        </span>
      )}
    </AuthImage>
  )

  if (!hasImage) {
    return <div className="h-12 w-12 shrink-0">{image}</div>
  }

  return (
    <button
      type="button"
      className="h-12 w-12 shrink-0 rounded-md outline-none ring-offset-background transition-transform hover:scale-[1.03] focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
      onClick={() => onPreview(match)}
      aria-label={`ดูรูป ${match.item_code}`}
    >
      {image}
    </button>
  )
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
  const [tab, setTab] = useState<'search' | 'create'>('search')

  // ── Search state ─────────────────────────────────────────────────────────────
  const [query, setQuery] = useState(rawName.slice(0, 80))
  const [results, setResults] = useState<CatalogMatch[]>([])
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [previewMatch, setPreviewMatch] = useState<CatalogMatch | null>(null)

  // ── Create state ─────────────────────────────────────────────────────────────
  const [form, setForm] = useState({
    code: '',
    name: rawName.slice(0, 80),
    unit_code: currentUnit || 'ชิ้น',
  })
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')

  // Debounced search → /api/catalog/search
  useEffect(() => {
    if (tab !== 'search') return
    const q = query.trim()
    if (q.length < 2) {
      setResults([])
      return
    }
    const handle = setTimeout(async () => {
      setSearching(true)
      setSearchError('')
      try {
        const res = await api.get<{ results: CatalogMatch[] }>(
          '/api/catalog/search',
          { params: { q, top: 10 } },
        )
        setResults(res.data.results ?? [])
      } catch (err: unknown) {
        setSearchError(err instanceof Error ? err.message : 'search failed')
      } finally {
        setSearching(false)
      }
    }, 300)
    return () => clearTimeout(handle)
  }, [query, tab])

  const handleCreate = async () => {
    setCreating(true)
    setCreateError('')
    try {
      const payload = {
        code: form.code.trim(),
        name: form.name.trim(),
        unit_code: form.unit_code.trim(),
        price: 0,
      }
      const res = await api.post<{ code: string; unit_code: string }>(
        '/api/catalog/products',
        payload,
      )
      onPick(res.data.code, res.data.unit_code, {
        item_code: res.data.code,
        item_name: payload.name,
        unit_code: res.data.unit_code || payload.unit_code,
        score: 1,
      })
      onClose()
    } catch (err: unknown) {
      const e = err as { response?: { data?: { error?: string } }; message?: string }
      setCreateError(e?.response?.data?.error || e?.message || 'create failed')
    } finally {
      setCreating(false)
    }
  }

  return (
    <>
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="grid max-h-[90vh] max-w-3xl grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Search className="h-4 w-4 text-muted-foreground" />
            เลือกสินค้าจาก SML
          </DialogTitle>
        </DialogHeader>

        {/* Raw name context */}
        <div className="rounded-md border border-border bg-muted/30 p-2.5 text-sm">
          <div className="flex gap-3">
            {sourceImageUrl && (
              <div className="h-20 w-20 shrink-0 overflow-hidden rounded-md border border-border bg-background">
                <img
                  src={sourceImageUrl}
                  alt=""
                  className="h-full w-full object-cover"
                  loading="lazy"
                  referrerPolicy="no-referrer"
                />
              </div>
            )}
            <div className="min-w-0 flex-1">
              <div className="mb-1 text-xs font-medium text-muted-foreground">{rawNameLabel}</div>
              <div className="line-clamp-2 break-words font-medium leading-5">{rawName}</div>
              {currentCode && (
                <div className="mt-1.5 text-xs text-muted-foreground">
                  เลือกไว้ตอนนี้:{' '}
                  <code className="text-foreground font-mono">{currentCode}</code>
                  {' '}({currentUnit || '—'})
                </div>
              )}
            </div>
          </div>
        </div>

        <Tabs
          value={tab}
          onValueChange={(v) => setTab(v as 'search' | 'create')}
          className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]"
        >
          <TabsList className="w-full">
            <TabsTrigger value="search" className="flex-1 gap-1.5">
              <Search className="h-3.5 w-3.5" /> ค้นหาจาก SML
            </TabsTrigger>
            <TabsTrigger value="create" className="flex-1 gap-1.5">
              <Plus className="h-3.5 w-3.5" /> เพิ่มสินค้าใหม่
            </TabsTrigger>
          </TabsList>

          {/* ── Search tab ─────────────────────────────────────────────────── */}
          <TabsContent value="search" className="mt-3 grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] gap-2.5">
            <Input
              autoFocus
              placeholder="ค้นหาด้วยชื่อหรือรหัสสินค้า"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />

            {searching && (
              <p className="text-sm text-muted-foreground">กำลังค้นหา...</p>
            )}
            {searchError && (
              <p className="text-sm text-destructive">{searchError}</p>
            )}

            {!searching && results.length > 0 && (
              <div className="rounded-md bg-muted/30 px-3 py-1.5 text-xs text-muted-foreground">
                รายการแรกคือคำแนะนำหลัก ส่วนรายการคะแนนต่ำเป็นตัวเลือกสำรอง
              </div>
            )}

            {!searching && results.length === 0 && query.trim().length >= 2 && (
              <div className="rounded-md bg-muted/40 py-6 text-center text-sm text-muted-foreground">
                ไม่พบสินค้าที่ตรง
              </div>
            )}

            <div className="min-h-0 space-y-1 overflow-y-auto pr-1">
              {results.map((r, index) => {
                const recommended = index === 0 && r.score >= 0.75
                const lowScore = r.score < 0.6
                return (
                  <div
                    key={r.item_code}
                    className={cn(
                      'w-full rounded-md border bg-background px-2.5 py-1.5 text-left',
                      'transition-colors hover:bg-muted/40',
                      recommended && 'border-success/60 bg-success/[0.04]',
                      !recommended && lowScore && 'border-border/80',
                      !recommended && !lowScore && scoreBorderClass(r.score),
                    )}
                  >
                    <div className="flex min-h-[58px] items-center gap-2.5">
                      <CatalogMatchThumbnail match={r} onPreview={setPreviewMatch} />
                      <button
                        type="button"
                        onClick={() => {
                          onPick(r.item_code, r.unit_code, r)
                          onClose()
                        }}
                        className="flex min-w-0 flex-1 items-center gap-2.5 text-left outline-none"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <span className="font-mono text-[13px] font-semibold text-foreground">
                              {r.item_code}
                            </span>
                            {recommended && (
                              <Badge className="h-5 bg-success text-[10px] text-success-foreground">
                                แนะนำ
                              </Badge>
                            )}
                            <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                              หน่วย {r.unit_code || '—'}
                            </Badge>
                          </div>
                          <div className="mt-0.5 line-clamp-2 break-words text-[13px] leading-4 text-foreground">
                            {r.item_name}
                          </div>
                        </div>
                        <div className="flex shrink-0 flex-col items-end gap-1.5">
                          <ScorePill score={r.score} recommended={recommended} />
                          <span className="rounded-md bg-primary/10 px-2 py-0.5 text-[11px] font-medium text-primary">
                            เลือก
                          </span>
                        </div>
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>

            <div className="flex items-center justify-between border-t pt-2">
              <span className="text-sm text-muted-foreground">ไม่เจอที่ตรง?</span>
              <Button
                type="button"
                size="sm"
                onClick={() => {
                  setForm((f) => ({ ...f, name: query.trim() || rawName.slice(0, 80) }))
                  setTab('create')
                }}
              >
                <Plus className="h-3.5 w-3.5" />
                เพิ่มสินค้าใหม่
              </Button>
            </div>
          </TabsContent>

          {/* ── Create tab ─────────────────────────────────────────────────── */}
          <TabsContent value="create" className="mt-3 min-h-0 space-y-3 overflow-y-auto pr-1">
            <div className="space-y-3">
              <div className="space-y-1.5">
                <label className="text-sm text-muted-foreground">
                  รหัสสินค้า <span className="text-destructive">*</span>
                </label>
                <Input
                  autoFocus
                  value={form.code}
                  placeholder="เช่น BF-99001 หรือ INGU-VIT-30ML"
                  onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-sm text-muted-foreground">
                  ชื่อสินค้า <span className="text-destructive">*</span>
                </label>
                <Input
                  value={form.name}
                  onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-sm text-muted-foreground">
                  หน่วย <span className="text-destructive">*</span>
                </label>
                <UnitSelect
                  value={form.unit_code}
                  onValueChange={(unit_code) => setForm((f) => ({ ...f, unit_code }))}
                  disabled={creating}
                  placeholder="เลือกหน่วยจาก SML"
                />
              </div>

              {createError && (
                <p className="text-sm text-destructive">{createError}</p>
              )}
            </div>

            <div className="flex items-center justify-between pt-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={creating}
                onClick={() => setTab('search')}
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                กลับไปค้นหา
              </Button>
              <Button
                type="button"
                disabled={
                  creating ||
                  !form.code.trim() ||
                  !form.name.trim() ||
                  !form.unit_code.trim()
                }
                onClick={handleCreate}
              >
                {creating ? 'กำลังเพิ่ม...' : 'เพิ่มและเลือกสินค้านี้'}
              </Button>
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
    <ProductImagePreviewDialog
      open={!!previewMatch}
      onOpenChange={(v) => !v && setPreviewMatch(null)}
      imageUrl={previewMatch?.image_url}
      itemCode={previewMatch?.item_code}
      itemName={previewMatch?.item_name}
      imageCount={previewMatch?.image_count ?? 0}
    />
    </>
  )
}
