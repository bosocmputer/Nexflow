import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ChevronLeft, ChevronRight, Pencil, RefreshCw, Search, Tags, Unlink, X } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { MapItemModal } from '@/pages/BillDetail/components/MapItemModal'
import type { CatalogMatch, MarketplaceAliasImpact, MarketplaceAliasReviewGroup, MarketplaceItemAlias } from '@/types'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'
import { useAuthStore } from '@/store/auth'

const SOURCE_LABEL: Record<string, string> = { shopee: 'Shopee', lazada: 'Lazada', tiktok: 'TikTok' }
const PER_PAGE = 30

type TabKey = 'pending' | 'saved'
type SourceFilter = 'all' | 'shopee' | 'lazada' | 'tiktok'
type PendingAction =
  | { kind: 'confirm'; group: MarketplaceAliasReviewGroup; product: CatalogMatch; impact: MarketplaceAliasImpact }
  | { kind: 'update'; alias: MarketplaceItemAlias; product: CatalogMatch; impact: MarketplaceAliasImpact }
  | { kind: 'delete'; alias: MarketplaceItemAlias; impact: MarketplaceAliasImpact }

function errorMessage(error: unknown, fallback: string) {
  const candidate = error as { response?: { data?: { error?: string; message?: string } } }
  return candidate.response?.data?.message ?? candidate.response?.data?.error ?? fallback
}

export default function MarketplaceAliases() {
  const canManage = useAuthStore((state) => state.user?.role === 'admin')
  const [searchParams] = useSearchParams()
  const initialQuery = searchParams.get('q')?.trim() ?? ''
  const [tab, setTab] = useState<TabKey>(initialQuery ? 'saved' : 'pending')
  const [pending, setPending] = useState<MarketplaceAliasReviewGroup[]>([])
  const [saved, setSaved] = useState<MarketplaceItemAlias[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [source, setSource] = useState<SourceFilter>('all')
  const [draft, setDraft] = useState(initialQuery)
  const [query, setQuery] = useState(initialQuery)
  const [loading, setLoading] = useState(true)
  const [pickerGroup, setPickerGroup] = useState<MarketplaceAliasReviewGroup | null>(null)
  const [pickerAlias, setPickerAlias] = useState<MarketplaceItemAlias | null>(null)
  const [action, setAction] = useState<PendingAction | null>(null)
  const [previewing, setPreviewing] = useState(false)

  const pages = Math.max(1, Math.ceil(total / PER_PAGE))
  const pendingItems = useMemo(() => pending.reduce((sum, item) => sum + item.item_count, 0), [pending])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const params = {
        page,
        per_page: PER_PAGE,
        source: source === 'all' ? undefined : source,
        q: query || undefined,
      }
      if (tab === 'pending') {
        const response = await client.get<{ data: MarketplaceAliasReviewGroup[]; total: number }>('/api/marketplace-aliases/review-groups', {
          params: { ...params, bill_type: 'sale', sort: 'impact' },
        })
        setPending(response.data.data ?? [])
        setTotal(response.data.total ?? 0)
      } else {
        const aliasResponse = await client.get<{ data: MarketplaceItemAlias[]; total: number }>('/api/marketplace-aliases', {
          params: { ...params, usable_only: true },
        })
        setSaved(aliasResponse.data.data ?? [])
        setTotal(aliasResponse.data.total ?? 0)
      }
    } catch (error) {
      toast.error(errorMessage(error, 'โหลดข้อมูลการจับคู่สินค้าไม่สำเร็จ'))
    } finally {
      setLoading(false)
    }
  }, [page, query, source, tab])

  useEffect(() => { void load() }, [load])

  const previewImpact = async (
    identity: Pick<MarketplaceAliasReviewGroup, 'source' | 'account_key' | 'external_item_id' | 'external_variant_id' | 'source_sku' | 'raw_name' | 'normalized_key'>,
    itemCode: string,
    aliasID = '',
    deactivate = false,
  ) => {
    setPreviewing(true)
    try {
      const response = await client.post<MarketplaceAliasImpact>('/api/marketplace-aliases/impact-preview', {
        alias_id: aliasID,
        source: identity.source,
        account_key: identity.account_key,
        external_item_id: identity.external_item_id,
        external_variant_id: identity.external_variant_id,
        source_sku: identity.source_sku,
        raw_name: identity.raw_name,
        normalized_key: identity.normalized_key,
        item_code: itemCode,
        deactivate,
      })
      return response.data
    } catch (error) {
      toast.error(errorMessage(error, 'ตรวจสอบผลกระทบไม่สำเร็จ กรุณาลองใหม่'))
      return null
    } finally {
      setPreviewing(false)
    }
  }

  const prepareConfirm = async (group: MarketplaceAliasReviewGroup, product: CatalogMatch) => {
    const impact = await previewImpact(group, product.item_code)
    if (impact) setAction({ kind: 'confirm', group, product, impact })
  }

  const prepareUpdate = async (alias: MarketplaceItemAlias, product: CatalogMatch) => {
    const impact = await previewImpact(alias, product.item_code, alias.id)
    if (impact) setAction({ kind: 'update', alias, product, impact })
  }

  const prepareDelete = async (alias: MarketplaceItemAlias) => {
    const impact = await previewImpact(alias, alias.item_code, alias.id, true)
    if (impact) setAction({ kind: 'delete', alias, impact })
  }

  const confirmAction = async () => {
    if (!action) return
    try {
      if (action.kind === 'confirm') {
        const { group, product } = action
        const response = await client.post<{ applied_items: number }>('/api/marketplace-aliases/confirm', {
          source: group.source,
          account_key: group.account_key,
          external_item_id: group.external_item_id,
          external_variant_id: group.external_variant_id,
          bill_type: 'sale',
          source_sku: group.source_sku,
          raw_name: group.raw_name,
          normalized_key: group.normalized_key,
          item_code: product.item_code,
          unit_code: product.unit_code,
        })
        toast.success(`บันทึกแล้ว และปรับรายการเปิด ${response.data.applied_items ?? 0} รายการ`)
        notifyWorkQueueChanged()
      } else if (action.kind === 'update') {
        const response = await client.put<{ applied_items: number }>(`/api/marketplace-aliases/${action.alias.id}`, {
          item_code: action.product.item_code,
          unit_code: action.product.unit_code,
          updated_at: action.alias.updated_at,
          bill_type: 'sale',
        })
        toast.success(`แก้ไขแล้ว และปรับรายการเปิด ${response.data.applied_items ?? 0} รายการ`)
        notifyWorkQueueChanged()
      } else {
        await client.delete(`/api/marketplace-aliases/${action.alias.id}`, {
          data: { updated_at: action.alias.updated_at },
        })
        toast.success('หยุดใช้การจับคู่นี้แล้ว เอกสารเดิมไม่ถูกแก้ไข')
        notifyWorkQueueChanged()
      }
      setAction(null)
      await load()
    } catch (error) {
      toast.error(errorMessage(error, 'บันทึกการจับคู่ไม่สำเร็จ'))
      if ((error as { response?: { status?: number } }).response?.status === 409) await load()
    }
  }

  const changeTab = (value: string) => {
    setTab(value as TabKey)
    setPage(1)
  }

  return (
    <div className="space-y-4 p-4 sm:p-6">
      <PageHeader
        title="จับคู่สินค้า Marketplace"
        description="ตรวจสินค้าที่ยังจับคู่ไม่ได้ และแก้ไขสินค้าที่ระบบจดจำไว้สำหรับออเดอร์ครั้งถัดไป"
        actions={(
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} /> รีเฟรช
          </Button>
        )}
      />

      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList>
          <TabsTrigger value="pending">รอจับคู่</TabsTrigger>
          <TabsTrigger value="saved">จับคู่แล้ว</TabsTrigger>
        </TabsList>

        <div className="mt-3 rounded-lg border bg-card">
          <div className="flex flex-wrap items-center gap-2 border-b p-3">
            <div className="relative min-w-[220px] flex-1">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') { setQuery(draft.trim()); setPage(1) }
                }}
                placeholder={tab === 'pending' ? 'ค้นหาสินค้าที่ต้องจับคู่' : 'ค้นหาสินค้า Marketplace หรือรหัส SML'}
                className="pl-8 pr-8"
              />
              {(draft || query) && (
                <button type="button" aria-label="ล้างคำค้นหา" className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" onClick={() => { setDraft(''); setQuery(''); setPage(1) }}>
                  <X className="h-4 w-4" />
                </button>
              )}
            </div>
            <Button variant="outline" onClick={() => { setQuery(draft.trim()); setPage(1) }}>ค้นหา</Button>
            <Select value={source} onValueChange={(value) => { setSource(value as SourceFilter); setPage(1) }}>
              <SelectTrigger className="w-[145px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">ทุกช่องทาง</SelectItem>
                <SelectItem value="shopee">Shopee</SelectItem>
                <SelectItem value="lazada">Lazada</SelectItem>
                <SelectItem value="tiktok">TikTok</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <TabsContent value="pending" className="m-0">
            <PendingTable loading={loading} rows={pending} canManage={canManage} onPick={setPickerGroup} />
          </TabsContent>
          <TabsContent value="saved" className="m-0">
            <SavedTable
              loading={loading}
              rows={saved}
              canManage={canManage}
              onEdit={setPickerAlias}
              onDelete={(alias) => void prepareDelete(alias)}
            />
          </TabsContent>

          <div className="flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs text-muted-foreground">
            <span>{tab === 'pending' ? `${total.toLocaleString()} สินค้าที่ต้องจับคู่ · ${pendingItems.toLocaleString()} รายการในหน้านี้` : `${total.toLocaleString()} การจับคู่ที่ใช้งานอยู่`} · หน้า {page}/{pages}</span>
            <div className="flex gap-1">
              <Button size="icon" variant="outline" className="h-8 w-8" disabled={page <= 1 || loading} onClick={() => setPage((value) => value - 1)} aria-label="หน้าก่อน"><ChevronLeft className="h-4 w-4" /></Button>
              <Button size="icon" variant="outline" className="h-8 w-8" disabled={page >= pages || loading} onClick={() => setPage((value) => value + 1)} aria-label="หน้าถัดไป"><ChevronRight className="h-4 w-4" /></Button>
            </div>
          </div>
        </div>
      </Tabs>

      {pickerGroup && (
        <MapItemModal
          open
          rawName={pickerGroup.raw_name}
          currentCode=""
          currentUnit=""
          currentPrice={0}
          rawNameLabel="สินค้า Marketplace ที่ต้องจับคู่"
          onPick={(_, __, product) => { if (product) void prepareConfirm(pickerGroup, product); setPickerGroup(null) }}
          onClose={() => setPickerGroup(null)}
        />
      )}
      {pickerAlias && (
        <MapItemModal
          open
          rawName={pickerAlias.raw_name || pickerAlias.source_sku}
          currentCode={pickerAlias.item_code}
          currentUnit={pickerAlias.unit_code}
          currentPrice={0}
          onPick={(_, __, product) => { if (product) void prepareUpdate(pickerAlias, product); setPickerAlias(null) }}
          onClose={() => setPickerAlias(null)}
        />
      )}

      <ConfirmDialog
        open={action !== null}
        onOpenChange={(open) => !open && setAction(null)}
        title={action?.kind === 'delete' ? 'หยุดใช้การจับคู่นี้?' : action?.kind === 'update' ? 'เปลี่ยนสินค้า SML ที่จับคู่?' : 'บันทึกการจับคู่นี้?'}
        description={actionDescription(action)}
        confirmLabel={action?.kind === 'delete' ? 'หยุดใช้' : 'ยืนยัน'}
        variant={action?.kind === 'delete' ? 'destructive' : 'default'}
        onConfirm={confirmAction}
      />
      {previewing && <div className="fixed inset-x-0 bottom-4 z-50 mx-auto w-fit rounded-md border bg-background px-3 py-2 text-sm shadow-md">กำลังตรวจรายการที่ได้รับผลกระทบ...</div>}
    </div>
  )
}

function PendingTable({ loading, rows, canManage, onPick }: { loading: boolean; rows: MarketplaceAliasReviewGroup[]; canManage: boolean; onPick: (row: MarketplaceAliasReviewGroup) => void }) {
  if (!loading && rows.length === 0) return <EmptyState icon={Tags} title="ไม่มีสินค้าที่ต้องจับคู่" description="ออเดอร์ใหม่ที่ระบบหารหัสสินค้า SML ไม่พบจะมาแสดงที่นี่" />
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader><TableRow><TableHead>ช่องทางและร้าน</TableHead><TableHead className="min-w-[280px]">สินค้า Marketplace</TableHead><TableHead className="text-right">รายการที่รอ</TableHead><TableHead className="text-right">จัดการ</TableHead></TableRow></TableHeader>
        <TableBody>
          {loading ? Array.from({ length: 6 }).map((_, index) => <TableRow key={index}><TableCell colSpan={4}><Skeleton className="h-10 w-full" /></TableCell></TableRow>) : rows.map((row) => (
            <TableRow key={row.group_key}>
              <TableCell><ChannelAccount source={row.source} accountName={row.account_name} accountKey={row.account_key} /></TableCell>
              <TableCell><div className="font-medium">{row.raw_name}</div>{row.source_sku && <div className="mt-1 text-xs text-muted-foreground">SKU: <span className="font-mono">{row.source_sku}</span></div>}</TableCell>
              <TableCell className="text-right tabular-nums"><div>{row.item_count.toLocaleString()} รายการ</div><div className="text-xs text-muted-foreground">{row.bill_count.toLocaleString()} บิล</div></TableCell>
              <TableCell className="text-right">{canManage && (row.source !== 'shopee' || row.account_key.startsWith('shop:')) ? <Button size="sm" onClick={() => onPick(row)}>เลือกสินค้า SML</Button> : <span className="text-xs text-muted-foreground">{canManage ? 'ต้องระบุร้านในไฟล์' : 'ให้ผู้ดูแลยืนยัน'}</span>}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function SavedTable({ loading, rows, canManage, onEdit, onDelete }: { loading: boolean; rows: MarketplaceItemAlias[]; canManage: boolean; onEdit: (row: MarketplaceItemAlias) => void; onDelete: (row: MarketplaceItemAlias) => void }) {
  if (!loading && rows.length === 0) {
    return <EmptyState icon={Tags} title="ยังไม่มีสินค้าที่จับคู่แล้ว" description="หลังเลือกสินค้า SML จากแท็บรอจับคู่ รายการที่ระบบจดจำไว้จะมาแสดงที่นี่" />
  }
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader><TableRow><TableHead className="min-w-[340px]">สินค้า Marketplace</TableHead><TableHead className="min-w-[260px]">จับคู่กับสินค้า SML</TableHead><TableHead className="w-[150px] min-w-[150px] whitespace-nowrap text-right">การใช้งาน</TableHead><TableHead className="w-[88px] min-w-[88px] text-right">จัดการ</TableHead></TableRow></TableHeader>
        <TableBody>
          {loading ? Array.from({ length: 6 }).map((_, index) => <TableRow key={index}><TableCell colSpan={4}><Skeleton className="h-10 w-full" /></TableCell></TableRow>) : rows.map((row) => (
            <TableRow key={row.id}>
              <TableCell>
                <div className="flex items-start gap-3">
                  <ChannelAccount source={row.source} accountName={row.account_name} accountKey={row.account_key} />
                  <div className="min-w-0">
                    <div className="line-clamp-2 font-medium">{row.raw_name || row.source_sku}</div>
                    <div className="mt-1 text-xs text-muted-foreground">{row.source_sku ? <>SKU: <span className="font-mono">{row.source_sku}</span></> : 'ไม่มี SKU'}</div>
                  </div>
                </div>
              </TableCell>
              <TableCell><div className="flex flex-wrap items-center gap-2"><span className="font-mono text-sm font-semibold">{row.item_code}</span>{!row.product_active && <Badge variant="destructive">สินค้าไม่พร้อม</Badge>}</div><div className="text-xs text-muted-foreground">{row.item_name || 'ไม่พบชื่อสินค้า'} · {row.unit_code || '-'}</div></TableCell>
              <TableCell className="whitespace-nowrap text-right tabular-nums"><div>ใช้แล้ว {row.usage_count.toLocaleString()} ครั้ง</div>{row.open_item_count > 0 && <div className="text-xs text-warning">รออัปเดต {row.open_item_count.toLocaleString()} รายการ</div>}{row.stock_mapping_count > 0 && <div className="text-xs text-muted-foreground">ใช้กับซิงก์สต๊อก</div>}</TableCell>
              <TableCell className="text-right">{canManage ? <div className="flex justify-end gap-1"><Button size="icon" variant="outline" className="h-8 w-8" onClick={() => onEdit(row)} aria-label="แก้ไขการจับคู่" title="แก้ไขการจับคู่"><Pencil className="h-4 w-4" /></Button><Button size="icon" variant="outline" className="h-8 w-8 text-destructive" onClick={() => onDelete(row)} aria-label="หยุดใช้การจับคู่" title="หยุดใช้การจับคู่"><Unlink className="h-4 w-4" /></Button></div> : <span className="text-xs text-muted-foreground">ดูอย่างเดียว</span>}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function actionDescription(action: PendingAction | null) {
  if (!action) return ''
  const impact = action.impact
  const impactText = `ออเดอร์ที่ยังไม่ส่ง: ${impact.open_items.toLocaleString()} รายการ ใน ${impact.open_bills.toLocaleString()} บิล\nการซิงก์สต๊อกที่เกี่ยวข้อง: ${impact.stock_mappings.toLocaleString()} รายการ${impact.stock_conflicts ? ` · พบการจับคู่สต๊อกซ้ำ ${impact.stock_conflicts.toLocaleString()}` : ''}${impact.dry_run_required ? '\nหลังบันทึกต้องตรวจสต๊อกใหม่ก่อนเปิดซิงก์' : ''}`
  if (action.kind === 'delete') return `หยุดใช้ ${action.alias.source_sku || action.alias.raw_name} ในอนาคต\n${impactText}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน`
  if (action.kind === 'update') return `ค่าเดิม: ${action.alias.item_code}\nค่าใหม่: ${action.product.item_code} · ${action.product.item_name}\n${impactText}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน`
  return `ต้นทาง: ${action.group.source_sku || action.group.raw_name}\nสินค้า SML: ${action.product.item_code} · ${action.product.item_name}\n${impactText}`
}

function ChannelAccount({ source, accountName, accountKey }: { source: string; accountName?: string; accountKey: string }) {
  const name = accountName || (accountKey.startsWith('shop:') ? `ร้าน ${accountKey.slice(5)}` : '')
  return (
    <div className="flex shrink-0 flex-col items-start gap-1">
      <Badge variant="secondary">{SOURCE_LABEL[source] ?? source}</Badge>
      {name && <span className="max-w-[140px] truncate text-xs text-muted-foreground">{name}</span>}
    </div>
  )
}
