import { useCallback, useEffect, useMemo, useState } from 'react'
import { CheckCircle2, ChevronLeft, ChevronRight, RefreshCw, Search, Sparkles, Tags, X } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/common/EmptyState'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { MapItemModal } from '@/pages/BillDetail/components/MapItemModal'
import type { CatalogMatch, MarketplaceAliasReviewGroup } from '@/types'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'

const SOURCE_LABEL: Record<string, string> = {
  shopee: 'Shopee',
  lazada: 'Lazada',
  tiktok: 'TikTok',
}

type SourceFilter = 'all' | 'shopee' | 'lazada' | 'tiktok'
type SortKey = 'impact' | 'score' | 'source' | 'name'

interface ReviewResponse {
  data: MarketplaceAliasReviewGroup[]
  total: number
  page: number
  per_page: number
}

const PER_PAGE = 30

type PendingAliasConfirm = {
  group: MarketplaceAliasReviewGroup
  match: CatalogMatch
  mode: 'suggested' | 'picked'
}

export default function MarketplaceAliases() {
  const [groups, setGroups] = useState<MarketplaceAliasReviewGroup[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [source, setSource] = useState<SourceFilter>('all')
  const [sort, setSort] = useState<SortKey>('impact')
  const [draft, setDraft] = useState('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [confirmingKey, setConfirmingKey] = useState<string | null>(null)
  const [selected, setSelected] = useState<MarketplaceAliasReviewGroup | null>(null)
  const [pendingConfirm, setPendingConfirm] = useState<PendingAliasConfirm | null>(null)

  const totalItemsOnPage = useMemo(() => groups.reduce((sum, g) => sum + g.item_count, 0), [groups])
  const totalBillsOnPage = useMemo(() => groups.reduce((sum, g) => sum + g.bill_count, 0), [groups])
  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE))

  const loadGroups = useCallback(async () => {
    setLoading(true)
    try {
      const res = await client.get<ReviewResponse>('/api/marketplace-aliases/review-groups', {
        params: {
          bill_type: 'sale',
          page,
          per_page: PER_PAGE,
          source: source === 'all' ? undefined : source,
          q: query || undefined,
          sort,
        },
      })
      setGroups(res.data.data ?? [])
      setTotal(res.data.total ?? 0)
      if ((res.data.page ?? page) !== page) setPage(res.data.page ?? 1)
    } catch {
      toast.error('โหลดสินค้ารอยืนยันไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }, [page, query, sort, source])

  useEffect(() => {
    void loadGroups()
  }, [loadGroups])

  const commitSearch = () => {
    setPage(1)
    setQuery(draft.trim())
  }

  const clearSearch = () => {
    setDraft('')
    setQuery('')
    setPage(1)
  }

  const confirmGroup = async (group: MarketplaceAliasReviewGroup, match: CatalogMatch) => {
    setConfirmingKey(group.group_key)
    try {
      const res = await client.post<{ applied_items: number; ready_bills: number }>('/api/marketplace-aliases/confirm', {
        source: group.source,
        bill_type: group.bill_type,
        source_sku: group.source_sku,
        raw_name: group.raw_name,
        normalized_key: group.normalized_key,
        item_code: match.item_code,
        unit_code: match.unit_code,
      })
      toast.success(`ยืนยันแล้ว ${res.data.applied_items ?? 0} รายการ`)
      notifyWorkQueueChanged()
      setSelected(null)
      await loadGroups()
    } catch {
      toast.error('ยืนยันสินค้าไม่สำเร็จ')
    } finally {
      setConfirmingKey(null)
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-normal text-foreground">สินค้ารอยืนยัน</h1>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
            เลือกสินค้า SML ให้ชื่อสินค้าจาก marketplace หนึ่งครั้ง แล้ว Nexflow จะจำไว้ใช้กับบิลถัดไป
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void loadGroups()} disabled={loading}>
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
          รีเฟรช
        </Button>
      </div>

      <div className="rounded-lg border bg-card px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <SummaryItem label="กลุ่มรอยืนยัน" value={total} />
          <SummaryItem label="รายการในหน้านี้" value={totalItemsOnPage} />
          <SummaryItem label="บิลในหน้านี้" value={totalBillsOnPage} />
          </div>
          <span className="text-xs text-muted-foreground">
            ยืนยันแล้วจะใช้กับบิลถัดไป ถ้าไม่มั่นใจให้เลือกจาก SML ก่อน
          </span>
        </div>
      </div>

      <div className="rounded-lg border bg-card">
        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-[240px] flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') commitSearch()
                if (e.key === 'Escape') clearSearch()
              }}
              placeholder="ค้นหาชื่อสินค้า / SKU"
              className="h-9 pl-8 pr-8"
            />
            {(draft || query) && (
              <button
                type="button"
                onClick={clearSearch}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                aria-label="ล้างคำค้นหา"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
          <Button variant="outline" size="sm" onClick={commitSearch}>ค้นหา</Button>

          <Select
            value={source}
            onValueChange={(value) => {
              setSource(value as SourceFilter)
              setPage(1)
            }}
          >
            <SelectTrigger className="h-9 w-[150px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">ทุกช่องทาง</SelectItem>
              <SelectItem value="shopee">Shopee</SelectItem>
              <SelectItem value="tiktok">TikTok</SelectItem>
              <SelectItem value="lazada">Lazada</SelectItem>
            </SelectContent>
          </Select>

          <Select
            value={sort}
            onValueChange={(value) => {
              setSort(value as SortKey)
              setPage(1)
            }}
          >
            <SelectTrigger className="h-9 w-[170px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="impact">บิลกระทบมากก่อน</SelectItem>
              <SelectItem value="score">คะแนนต่ำก่อน</SelectItem>
              <SelectItem value="source">เรียงตามช่องทาง</SelectItem>
              <SelectItem value="name">เรียงตามชื่อ</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40">
                <TableHead className="w-[120px]">ช่องทาง</TableHead>
                <TableHead className="w-[35%] min-w-[240px]">สินค้า marketplace</TableHead>
                <TableHead className="w-[110px] text-right">กระทบ</TableHead>
                <TableHead className="w-[240px]">ระบบแนะนำ</TableHead>
                <TableHead className="w-[220px] text-right">จัดการ</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 8 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell colSpan={5} className="py-2">
                      <Skeleton className="h-10 w-full" />
                    </TableCell>
                  </TableRow>
                ))
              ) : groups.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="py-10">
                    <EmptyState
                      icon={Tags}
                      title="ไม่มีสินค้ารอยืนยัน"
                      description="เมื่อมีสินค้า marketplace ที่ยังไม่มั่นใจ ระบบจะรวมกลุ่มมาให้ยืนยันที่หน้านี้"
                    />
                  </TableCell>
                </TableRow>
              ) : (
                groups.map((group) => (
                  <TableRow key={group.group_key} className="align-top">
                    <TableCell className="py-3">
                      <div className="space-y-1">
                        <Badge variant="secondary">{SOURCE_LABEL[group.source] ?? group.source}</Badge>
                        {group.source_sku && (
                          <div className="max-w-[160px] truncate rounded-full border px-2 py-0.5 font-mono text-[11px] text-muted-foreground" title={`SKU ${group.source_sku}`}>
                            SKU {group.source_sku}
                          </div>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="py-3">
                      <div className="max-w-[380px] space-y-1">
                        <div className="line-clamp-2 text-sm font-medium leading-5" title={group.raw_name}>{group.raw_name}</div>
                        <div className="truncate text-xs text-muted-foreground" title={group.normalized_key}>
                          key: <code>{group.normalized_key}</code>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="py-3 text-right text-sm tabular-nums">
                      <div className="font-medium">{group.item_count.toLocaleString()} รายการ</div>
                      <div className="text-xs text-muted-foreground">{group.bill_count.toLocaleString()} บิล</div>
                    </TableCell>
                    <TableCell className="py-3">
                      {group.suggested_match ? (
                        <div className="space-y-1">
                          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                            <Sparkles className="h-3.5 w-3.5" />
                            <span>{Math.round(group.suggested_match.score * 100)}%</span>
                            {group.suggested_match.score < 0.8 && (
                              <Badge variant="outline" className="h-5 border-warning/30 bg-warning/10 px-1.5 text-[10px] text-warning">
                                ควรตรวจ
                              </Badge>
                            )}
                          </div>
                          <div className="font-mono text-sm font-semibold">{group.suggested_match.item_code}</div>
                          <div className="line-clamp-1 text-xs text-muted-foreground" title={group.suggested_match.item_name}>{group.suggested_match.item_name}</div>
                        </div>
                      ) : (
                        <span className="text-sm text-muted-foreground">ยังไม่มีสินค้าแนะนำ</span>
                      )}
                    </TableCell>
                    <TableCell className="py-3 text-right">
                      <div className="flex flex-wrap justify-end gap-2">
                        {group.suggested_match && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={confirmingKey === group.group_key}
                            onClick={() => setPendingConfirm({
                              group,
                              match: group.suggested_match as CatalogMatch,
                              mode: 'suggested',
                            })}
                          >
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            ใช้ตัวนี้
                          </Button>
                        )}
                        <Button size="sm" onClick={() => setSelected(group)}>
                          เลือกจาก SML
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs text-muted-foreground">
          <span>
            ทั้งหมด {total.toLocaleString()} กลุ่ม · หน้า {page} / {totalPages}
          </span>
          <div className="flex items-center gap-1">
            <Button
              size="icon"
              variant="outline"
              className="h-7 w-7"
              disabled={page <= 1 || loading}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              aria-label="หน้าก่อน"
            >
              <ChevronLeft className="h-3.5 w-3.5" />
            </Button>
            <Button
              size="icon"
              variant="outline"
              className="h-7 w-7"
              disabled={page >= totalPages || loading}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              aria-label="หน้าถัดไป"
            >
              <ChevronRight className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </div>

      {selected && (
        <MapItemModal
          open={!!selected}
          rawName={selected.raw_name}
          currentCode={selected.suggested_match?.item_code ?? ''}
          currentUnit={selected.suggested_match?.unit_code ?? ''}
          currentPrice={0}
          rawNameLabel="ชื่อสินค้า marketplace"
          onPick={(code, unitCode, picked) => {
            setPendingConfirm({
              group: selected,
              match: picked ?? {
                item_code: code,
                item_name: code,
                unit_code: unitCode,
                score: 1,
              },
              mode: 'picked',
            })
            setSelected(null)
          }}
          onClose={() => setSelected(null)}
        />
      )}

      <ConfirmDialog
        open={pendingConfirm !== null}
        onOpenChange={(open) => !open && setPendingConfirm(null)}
        title="ยืนยันการจับคู่สินค้านี้?"
        description={pendingConfirm ? [
          `Marketplace: ${SOURCE_LABEL[pendingConfirm.group.source] ?? pendingConfirm.group.source}`,
          `ชื่อสินค้า: ${pendingConfirm.group.raw_name}`,
          `รหัส SML ที่จะใช้: ${pendingConfirm.match.item_code}${pendingConfirm.match.item_name ? ` · ${pendingConfirm.match.item_name}` : ''}`,
          `ผลกระทบ: Nexflow จะจำการจับคู่นี้ และใช้กับบิลถัดไปที่ชื่อสินค้าเหมือนกัน`,
          pendingConfirm.mode === 'suggested'
            ? 'ตรวจอีกครั้งถ้าคะแนนแนะนำต่ำ สี/รุ่น/ขนาดอาจไม่ตรงกับสินค้าใน SML'
            : 'การเลือกจาก SML จะใช้ค่าที่คุณเลือกเป็นแหล่งอ้างอิงหลัก',
        ].join('\n') : ''}
        confirmLabel="ยืนยันการจับคู่"
        onConfirm={async () => {
          if (!pendingConfirm) return
          await confirmGroup(pendingConfirm.group, pendingConfirm.match)
          setPendingConfirm(null)
        }}
      />
    </div>
  )
}

function SummaryItem({ label, value }: { label: string; value: number }) {
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span className="text-lg font-semibold tabular-nums text-foreground">{value.toLocaleString()}</span>
      <span className="text-xs text-muted-foreground">{label}</span>
    </span>
  )
}
