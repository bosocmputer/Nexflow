import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ChevronLeft, ChevronRight, Pencil, RefreshCw, Search, Tags, Trash2, X } from 'lucide-react'
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
import type { CatalogMatch, Mapping, MarketplaceAliasImpact, MarketplaceAliasReviewGroup, MarketplaceItemAlias } from '@/types'
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
  const [tab, setTab] = useState<TabKey>('pending')
  const [pending, setPending] = useState<MarketplaceAliasReviewGroup[]>([])
  const [saved, setSaved] = useState<MarketplaceItemAlias[]>([])
  const [legacyMappings, setLegacyMappings] = useState<Mapping[]>([])
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
        const [aliasResponse, mappingResponse] = await Promise.all([
          client.get<{ data: MarketplaceItemAlias[]; total: number }>('/api/marketplace-aliases', { params }),
          source === 'all'
            ? client.get<{ data: Mapping[] }>('/api/mappings')
            : Promise.resolve({ data: { data: [] as Mapping[] } }),
        ])
        setSaved(aliasResponse.data.data ?? [])
        const normalizedQuery = query.toLocaleLowerCase('th-TH')
        setLegacyMappings((mappingResponse.data.data ?? []).filter((item) => (
          !normalizedQuery ||
          item.raw_name.toLocaleLowerCase('th-TH').includes(normalizedQuery) ||
          item.item_code.toLocaleLowerCase('th-TH').includes(normalizedQuery) ||
          (item.item_name ?? '').toLocaleLowerCase('th-TH').includes(normalizedQuery)
        )))
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
        description="Master กลางสำหรับจับคู่สินค้า Shopee, Lazada และ TikTok ไปยังสินค้า SML"
        actions={(
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} /> รีเฟรช
          </Button>
        )}
      />

      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList>
          <TabsTrigger value="pending">รอยืนยัน</TabsTrigger>
          <TabsTrigger value="saved">บันทึกแล้ว</TabsTrigger>
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
                placeholder="ค้นหาชื่อสินค้า, SKU หรือรหัส SML"
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
              showEmpty={source !== 'all' || legacyMappings.length === 0}
              canManage={canManage}
              onEdit={setPickerAlias}
              onDelete={(alias) => void prepareDelete(alias)}
            />
            {source === 'all' && page === 1 && (
              <LegacyMappingTable
                loading={loading}
                rows={legacyMappings}
              />
            )}
          </TabsContent>

          <div className="flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs text-muted-foreground">
            <span>{tab === 'pending' ? `${total.toLocaleString()} กลุ่ม · ${pendingItems.toLocaleString()} รายการในหน้านี้` : `${total.toLocaleString()} SKU/ช่องทาง · ${legacyMappings.length.toLocaleString()} ชื่อเดิม`} · หน้า {page}/{pages}</span>
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
          rawNameLabel="สินค้า marketplace ที่รอยืนยัน"
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
        title={action?.kind === 'delete' ? 'หยุดใช้การจับคู่นี้?' : action?.kind === 'update' ? 'ยืนยันการเปลี่ยนสินค้า?' : 'ยืนยันการจับคู่สินค้า?'}
        description={actionDescription(action)}
        confirmLabel={action?.kind === 'delete' ? 'หยุดใช้' : 'ยืนยัน'}
        variant={action?.kind === 'delete' ? 'destructive' : 'default'}
        onConfirm={confirmAction}
      />
      {previewing && <div className="fixed inset-x-0 bottom-4 z-50 mx-auto w-fit rounded-md border bg-background px-3 py-2 text-sm shadow-md">กำลังตรวจผลกระทบ...</div>}
    </div>
  )
}

function PendingTable({ loading, rows, canManage, onPick }: { loading: boolean; rows: MarketplaceAliasReviewGroup[]; canManage: boolean; onPick: (row: MarketplaceAliasReviewGroup) => void }) {
  if (!loading && rows.length === 0) return <EmptyState icon={Tags} title="ไม่มีสินค้ารอยืนยัน" description="สินค้าที่ SKU ไม่ตรงและยังไม่มีการจับคู่จะมาแสดงที่นี่" />
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader><TableRow><TableHead>ช่องทาง</TableHead><TableHead className="min-w-[280px]">สินค้า marketplace</TableHead><TableHead className="text-right">ผลกระทบ</TableHead><TableHead className="text-right">จัดการ</TableHead></TableRow></TableHeader>
        <TableBody>
          {loading ? Array.from({ length: 6 }).map((_, index) => <TableRow key={index}><TableCell colSpan={4}><Skeleton className="h-10 w-full" /></TableCell></TableRow>) : rows.map((row) => (
            <TableRow key={row.group_key}>
              <TableCell><div className="flex flex-col items-start gap-1"><Badge variant="secondary">{SOURCE_LABEL[row.source] ?? row.source}</Badge><span className="text-xs text-muted-foreground">{row.account_name || accountLabel(row.account_key)}</span></div></TableCell>
              <TableCell><div className="font-medium">{row.raw_name}</div>{row.source_sku && <code className="mt-1 block text-xs text-muted-foreground">SKU {row.source_sku}</code>}{row.external_item_id && <div className="mt-1 text-xs text-muted-foreground">Item {row.external_item_id}{row.external_variant_id && ` / Model ${row.external_variant_id}`}</div>}</TableCell>
              <TableCell className="text-right tabular-nums"><div>{row.item_count.toLocaleString()} รายการ</div><div className="text-xs text-muted-foreground">{row.bill_count.toLocaleString()} บิล</div></TableCell>
              <TableCell className="text-right">{canManage && (row.source !== 'shopee' || row.account_key.startsWith('shop:')) ? <Button size="sm" onClick={() => onPick(row)}>เลือกสินค้า SML</Button> : <span className="text-xs text-muted-foreground">{canManage ? 'ต้องระบุร้านในไฟล์' : 'ให้ผู้ดูแลยืนยัน'}</span>}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function SavedTable({ loading, rows, showEmpty, canManage, onEdit, onDelete }: { loading: boolean; rows: MarketplaceItemAlias[]; showEmpty: boolean; canManage: boolean; onEdit: (row: MarketplaceItemAlias) => void; onDelete: (row: MarketplaceItemAlias) => void }) {
  if (!loading && rows.length === 0) {
    return showEmpty ? <EmptyState icon={Tags} title="ยังไม่มีการจับคู่ที่บันทึก" description="เมื่อยืนยันสินค้า รายการจะมาแสดงและแก้ไขได้ที่แท็บนี้" /> : null
  }
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader><TableRow><TableHead>ช่องทาง</TableHead><TableHead className="min-w-[240px]">SKU / ชื่อต้นทาง</TableHead><TableHead className="min-w-[220px]">สินค้า SML</TableHead><TableHead>ผู้ยืนยัน</TableHead><TableHead className="text-right">ใช้แล้ว</TableHead><TableHead className="text-right">จัดการ</TableHead></TableRow></TableHeader>
        <TableBody>
          {loading ? Array.from({ length: 6 }).map((_, index) => <TableRow key={index}><TableCell colSpan={6}><Skeleton className="h-10 w-full" /></TableCell></TableRow>) : rows.map((row) => (
            <TableRow key={row.id}>
              <TableCell><div className="flex flex-col items-start gap-1"><Badge variant="secondary">{SOURCE_LABEL[row.source] ?? row.source}</Badge><span className="text-xs text-muted-foreground">{row.account_name || accountLabel(row.account_key)}</span>{!row.scope_confirmed && <Badge variant="outline" className="border-warning/40 bg-warning/10 text-warning">ต้องยืนยันขอบเขต</Badge>}</div></TableCell>
              <TableCell><div className="flex flex-wrap items-center gap-1.5"><span className="font-mono text-xs">{row.source_sku || '-'}</span><Badge variant="outline">{matchMethodLabel(row.match_method)}</Badge></div><div className="mt-1 line-clamp-2 text-sm text-muted-foreground">{row.raw_name}</div>{row.external_item_id && <div className="mt-1 text-xs text-muted-foreground">Item {row.external_item_id}{row.external_variant_id && ` / Model ${row.external_variant_id}`}</div>}</TableCell>
              <TableCell><div className="flex flex-wrap items-center gap-2"><span className="font-mono text-sm font-semibold">{row.item_code}</span>{!row.product_active && <Badge variant="destructive">สินค้าไม่พร้อม</Badge>}</div><div className="text-xs text-muted-foreground">{row.item_name || 'ไม่พบชื่อสินค้า'} · {row.unit_code || '-'}</div></TableCell>
              <TableCell><div className="text-sm">{row.confirmed_name || '-'}</div><div className="text-xs text-muted-foreground">{new Date(row.updated_at).toLocaleString('th-TH')}</div></TableCell>
              <TableCell className="text-right tabular-nums"><div>{row.usage_count.toLocaleString()} ครั้ง</div>{row.open_item_count > 0 && <div className="text-xs text-warning">กระทบ {row.open_item_count.toLocaleString()} รายการเปิด</div>}{row.stock_mapping_count > 0 && <div className="text-xs text-muted-foreground">Stock {row.stock_mapping_count.toLocaleString()} รายการ</div>}</TableCell>
              <TableCell className="text-right">{canManage ? <div className="flex justify-end gap-1"><Button size="icon" variant="outline" className="h-8 w-8" onClick={() => onEdit(row)} aria-label="แก้ไข"><Pencil className="h-4 w-4" /></Button><Button size="icon" variant="outline" className="h-8 w-8 text-destructive" onClick={() => onDelete(row)} aria-label="หยุดใช้"><Trash2 className="h-4 w-4" /></Button></div> : <span className="text-xs text-muted-foreground">ดูอย่างเดียว</span>}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function LegacyMappingTable({ loading, rows }: { loading: boolean; rows: Mapping[] }) {
  if (!loading && rows.length === 0) return null
  return (
    <div className="border-t">
      <div className="px-3 pb-1 pt-3">
        <h2 className="text-sm font-semibold">จับคู่จากชื่อเดิม</h2>
        <p className="text-xs text-muted-foreground">เก็บไว้อ้างอิงเพื่อช่วยตรวจรายการเท่านั้น ต้องยืนยันร้านและช่องทางก่อนจึงจะใช้เป็น Master ได้</p>
      </div>
      <div className="overflow-x-auto">
        <Table>
          <TableHeader><TableRow><TableHead className="min-w-[280px]">ชื่อต้นทาง</TableHead><TableHead className="min-w-[220px]">สินค้า SML</TableHead><TableHead>ผู้ยืนยัน</TableHead><TableHead className="text-right">ใช้แล้ว</TableHead><TableHead className="text-right">สถานะ</TableHead></TableRow></TableHeader>
          <TableBody>
            {loading ? Array.from({ length: 4 }).map((_, index) => <TableRow key={index}><TableCell colSpan={5}><Skeleton className="h-10 w-full" /></TableCell></TableRow>) : rows.map((row) => (
              <TableRow key={row.id}>
                <TableCell><div className="font-medium">{row.raw_name}</div><Badge variant="outline" className="mt-1">ไม่มี SKU</Badge></TableCell>
                <TableCell><div className="flex flex-wrap items-center gap-2"><span className="font-mono text-sm font-semibold">{row.item_code}</span>{!row.product_active && <Badge variant="destructive">สินค้าไม่พร้อม</Badge>}</div><div className="text-xs text-muted-foreground">{row.item_name || 'ไม่พบชื่อสินค้า'} · {row.unit_code || '-'}</div></TableCell>
                <TableCell><div className="text-sm">{row.confirmed_name || '-'}</div><div className="text-xs text-muted-foreground">{new Date(row.updated_at).toLocaleString('th-TH')}</div></TableCell>
                <TableCell className="text-right tabular-nums"><div>{row.usage_count.toLocaleString()} ครั้ง</div>{row.open_item_count > 0 && <div className="text-xs text-warning">กระทบ {row.open_item_count.toLocaleString()} รายการเปิด</div>}</TableCell>
                <TableCell className="text-right"><Badge variant="outline" className="border-warning/40 bg-warning/10 text-warning">ต้องยืนยันขอบเขต</Badge></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function actionDescription(action: PendingAction | null) {
  if (!action) return ''
  const impact = action.impact
  const impactText = `เอกสารเปิด: ${impact.open_items.toLocaleString()} รายการ ใน ${impact.open_bills.toLocaleString()} บิล\nStock mapping: ${impact.stock_mappings.toLocaleString()} รายการ${impact.stock_conflicts ? ` · พบ conflict ${impact.stock_conflicts.toLocaleString()}` : ''}${impact.dry_run_required ? '\nหลังบันทึกต้องทำ Dry-run ใหม่ก่อนซิงก์สต๊อก' : ''}`
  if (action.kind === 'delete') return `หยุดใช้ ${action.alias.source_sku || action.alias.raw_name} ในอนาคต\n${impactText}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน`
  if (action.kind === 'update') return `ค่าเดิม: ${action.alias.item_code}\nค่าใหม่: ${action.product.item_code} · ${action.product.item_name}\n${impactText}\nเอกสารที่ส่ง SML แล้วจะไม่ถูกเปลี่ยน`
  return `ต้นทาง: ${action.group.source_sku || action.group.raw_name}\nสินค้า SML: ${action.product.item_code} · ${action.product.item_name}\n${impactText}`
}

function accountLabel(accountKey: string) {
  if (!accountKey || accountKey === 'default') return 'ยังไม่ระบุร้าน'
  if (accountKey.startsWith('shop:')) return `ร้าน Shopee ${accountKey.slice(5)}`
  return accountKey
}

function matchMethodLabel(method: MarketplaceItemAlias['match_method']) {
  if (method === 'exact_sku') return 'SKU ตรง'
  if (method === 'manual_identity') return 'Item/Model'
  if (method === 'manual_sku') return 'SKU ยืนยัน'
  if (method === 'legacy') return 'ข้อมูลเดิม'
  return 'ชื่อยืนยัน'
}
