import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ChevronDown, ChevronLeft, ChevronRight, Loader2, Pencil, RefreshCw, Search, Settings2, Tags, Unlink, X } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { MapItemModal } from '@/pages/BillDetail/components/MapItemModal'
import type { CatalogMatch, MarketplaceAliasImpact, MarketplaceAliasReviewGroup, MarketplaceConversionReadiness, MarketplaceCursorPage, MarketplaceItemAlias, MarketplaceMappingJob, MarketplaceProductGroup, MarketplaceStockPolicyJob, UnitOption } from '@/types'
import { marketplaceImpactFormulaLines } from '@/lib/marketplace-impact'
import { editableMarketplaceStockPolicy } from '@/lib/marketplace-stock-policy'
import { cn } from '@/lib/utils'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'
import { useAuthStore } from '@/store/auth'

const SOURCE_LABEL: Record<string, string> = { shopee: 'Shopee', lazada: 'Lazada', tiktok: 'TikTok' }
const PER_PAGE = 30

type TabKey = 'pending' | 'saved'
type SourceFilter = 'all' | 'shopee' | 'lazada' | 'tiktok'
type StatusFilter = 'all' | 'ready' | 'fix' | 'disabled'
type ConversionConfig = {
  unitCode: string
  quantityMultiplier: number
  salesEnabled: boolean
  stockPolicy: MarketplaceItemAlias['stock_policy']
  acknowledgeManualUnmanaged: boolean
}
type ConversionEditor =
  | { kind: 'confirm'; group: MarketplaceAliasReviewGroup; product: CatalogMatch }
  | { kind: 'update'; alias: MarketplaceItemAlias; product: CatalogMatch }
type PendingAction =
  | { kind: 'confirm'; group: MarketplaceAliasReviewGroup; product: CatalogMatch; conversion: ConversionConfig; impact: MarketplaceAliasImpact }
  | { kind: 'update'; alias: MarketplaceItemAlias; product: CatalogMatch; conversion: ConversionConfig; impact: MarketplaceAliasImpact }
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
	const [savedGroups, setSavedGroups] = useState<MarketplaceProductGroup[]>([])
	const [groupedAvailable, setGroupedAvailable] = useState<boolean | null>(null)
	const [groupCursor, setGroupCursor] = useState('')
	const [groupCursorHistory, setGroupCursorHistory] = useState<string[]>([])
	const [nextGroupCursor, setNextGroupCursor] = useState('')
	const [status, setStatus] = useState<StatusFilter>('all')
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [source, setSource] = useState<SourceFilter>('all')
  const [draft, setDraft] = useState(initialQuery)
  const [query, setQuery] = useState(initialQuery)
  const [loading, setLoading] = useState(true)
  const [pickerGroup, setPickerGroup] = useState<MarketplaceAliasReviewGroup | null>(null)
  const [pickerAlias, setPickerAlias] = useState<MarketplaceItemAlias | null>(null)
  const [conversionEditor, setConversionEditor] = useState<ConversionEditor | null>(null)
  const [action, setAction] = useState<PendingAction | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [activeJob, setActiveJob] = useState<MarketplaceMappingJob | null>(null)
  const [activePolicyJob, setActivePolicyJob] = useState<MarketplaceStockPolicyJob | null>(null)
  const [readiness, setReadiness] = useState<MarketplaceConversionReadiness | null>(null)
  const jobPollToken = useRef(0)
  const policyJobPollToken = useRef(0)

	const groupedSaved = tab === 'saved' && groupedAvailable === true
  const pages = groupedSaved ? groupCursorHistory.length + 1 + (nextGroupCursor ? 1 : 0) : Math.max(1, Math.ceil(total / PER_PAGE))
	const currentPage = groupedSaved ? groupCursorHistory.length + 1 : page
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
		if (groupedAvailable !== false) {
		  try {
			const groupResponse = await client.get<MarketplaceCursorPage<MarketplaceProductGroup>>('/api/marketplace-aliases/product-groups', {
			  params: { source: params.source, q: params.q, status: status === 'all' ? undefined : status, cursor: groupCursor || undefined, limit: PER_PAGE },
			})
			setSavedGroups(groupResponse.data.data ?? [])
			setNextGroupCursor(groupResponse.data.next_cursor ?? '')
			setGroupedAvailable(true)
			setTotal(0)
			return
		  } catch (error) {
			if ((error as { response?: { status?: number } }).response?.status !== 404) throw error
			setGroupedAvailable(false)
		  }
		}
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
	}, [groupCursor, groupedAvailable, page, query, source, status, tab])

  useEffect(() => { void load() }, [load])
  useEffect(() => () => { jobPollToken.current += 1; policyJobPollToken.current += 1 }, [])
  useEffect(() => {
    let active = true
    let timer = 0
    const refresh = async () => {
      try {
        const response = await client.get<MarketplaceConversionReadiness>('/api/marketplace-aliases/readiness')
        if (!active) return
        setReadiness(response.data)
        if (!response.data.catalog_generation_ready || !response.data.mapping_backfill_ready || !response.data.reservation_ledger_ready) {
          timer = window.setTimeout(() => void refresh(), 5000)
        }
      } catch {
        // The page itself remains usable in off/shadow mode; mutation APIs
        // still fail closed when readiness is required.
      }
    }
    void refresh()
    return () => { active = false; window.clearTimeout(timer) }
  }, [])

  const previewImpact = async (
    identity: Pick<MarketplaceAliasReviewGroup, 'source' | 'account_key' | 'external_item_id' | 'external_variant_id' | 'source_sku' | 'raw_name' | 'normalized_key'>,
    itemCode: string,
    aliasID = '',
    deactivate = false,
    unitCode = '',
    quantityMultiplier = 1,
    salesEnabled = true,
    stockPolicy = 'blocked',
    acknowledgeManualUnmanaged = false,
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
        unit_code: unitCode,
        quantity_multiplier: quantityMultiplier,
        sales_enabled: salesEnabled,
        stock_policy: stockPolicy,
        acknowledge_manual_unmanaged: acknowledgeManualUnmanaged,
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

  const prepareConfirm = async (group: MarketplaceAliasReviewGroup, product: CatalogMatch, conversion: ConversionConfig) => {
    const impact = await previewImpact(group, product.item_code, '', false, conversion.unitCode, conversion.quantityMultiplier, conversion.salesEnabled, conversion.stockPolicy, conversion.acknowledgeManualUnmanaged)
    if (impact) setAction({ kind: 'confirm', group, product, conversion, impact })
  }

  const prepareUpdate = async (alias: MarketplaceItemAlias, product: CatalogMatch, conversion: ConversionConfig) => {
    const impact = await previewImpact(alias, product.item_code, alias.id, false, conversion.unitCode, conversion.quantityMultiplier, conversion.salesEnabled, conversion.stockPolicy, conversion.acknowledgeManualUnmanaged)
    if (impact) setAction({ kind: 'update', alias, product, conversion, impact })
  }

  const prepareDelete = async (alias: MarketplaceItemAlias) => {
    if (alias.source === 'shopee' && alias.stock_policy === 'zeroing') {
      toast.warning('กำลังตั้ง stock เป็น 0 กรุณารอให้ Shopee ยืนยันก่อนหยุดใช้การจับคู่')
      void recoverPolicyJob(alias.id)
      return
    }
    if (alias.source === 'shopee' && alias.stock_policy === 'managed') {
      toast.warning('ก่อนหยุดใช้ กรุณาเลือก “ตั้ง stock เป็น 0 แล้วปิด” หรือ “คง stock เดิมและจัดการเอง”')
      setConversionEditor({
        kind: 'update',
        alias,
        product: { item_code: alias.item_code, item_name: alias.item_name || alias.item_code, unit_code: alias.unit_code, score: 1 },
      })
      return
    }
    const impact = await previewImpact(alias, alias.item_code, alias.id, true, alias.unit_code, alias.quantity_multiplier || 1, false, 'blocked')
    if (impact) setAction({ kind: 'delete', alias, impact })
  }

  const monitorJob = async (initial: MarketplaceMappingJob) => {
    const token = jobPollToken.current + 1
    jobPollToken.current = token
    setActiveJob(initial)
    let consecutivePollFailures = 0
    for (let attempt = 0; attempt < 300; attempt += 1) {
      if (jobPollToken.current !== token) return
      let response
      try {
        response = await client.get<MarketplaceMappingJob>(`/api/marketplace-aliases/jobs/${initial.id}`)
        consecutivePollFailures = 0
      } catch (error) {
        consecutivePollFailures += 1
        if (consecutivePollFailures >= 5) {
          toast.error(errorMessage(error, 'ติดตามงาน Product Master ไม่สำเร็จ งานยังทำต่อในระบบและสามารถกดรีเฟรชเพื่อตรวจใหม่'))
          return
        }
        await new Promise((resolve) => window.setTimeout(resolve, consecutivePollFailures * 1000))
        continue
      }
      setActiveJob(response.data)
      if (response.data.status === 'completed') {
        toast.success(`ปรับ Product Master สำเร็จ ${response.data.processed_count.toLocaleString()} รายการ`)
        setActiveJob(null)
        await load()
        notifyWorkQueueChanged()
        return
      }
      if (response.data.status === 'failed' || response.data.status === 'cancelled') {
        toast.error(response.data.error_message || 'งานปรับ Product Master ไม่สำเร็จ ร้านที่เกี่ยวข้องยังคงหยุดซิงก์เพื่อความปลอดภัย')
        return
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1000))
    }
    toast.warning('งาน Product Master ยังทำอยู่ สามารถตรวจสถานะได้จากแถบด้านล่าง')
  }

  const retryActiveJob = async () => {
    if (!activeJob || activeJob.status !== 'failed') return
    try {
      const response = await client.post<MarketplaceMappingJob>(`/api/marketplace-aliases/jobs/${activeJob.id}/retry`)
      toast.success('นำงานกลับเข้าคิวแล้ว เมื่อเสร็จให้ตรวจสอบสต๊อกอีกครั้ง')
      void monitorJob(response.data)
    } catch (error) {
      toast.error(errorMessage(error, 'สั่งลองงานใหม่ไม่สำเร็จ'))
    }
  }

  const monitorPolicyJob = async (initial: MarketplaceStockPolicyJob) => {
    const token = policyJobPollToken.current + 1
    policyJobPollToken.current = token
    setActivePolicyJob(initial)
    let consecutivePollFailures = 0
    for (let attempt = 0; attempt < 600; attempt += 1) {
      if (policyJobPollToken.current !== token) return
      let response
      try {
        response = await client.get<MarketplaceStockPolicyJob>(`/api/marketplace-aliases/policy-jobs/${initial.id}`)
        consecutivePollFailures = 0
      } catch (error) {
        consecutivePollFailures += 1
        if (consecutivePollFailures >= 5) {
          toast.error(errorMessage(error, 'ติดตามงานตั้ง stock 0 ไม่สำเร็จ งานยังทำต่อในระบบและสามารถกดรีเฟรชเพื่อตรวจใหม่'))
          return
        }
        await new Promise((resolve) => window.setTimeout(resolve, consecutivePollFailures * 1000))
        continue
      }
      if (policyJobPollToken.current !== token) return
      setActivePolicyJob(response.data)
      if (response.data.status === 'completed') {
        toast.success('ตั้ง stock Shopee เป็น 0 และปิดการจัดการรายการนี้แล้ว')
        setActivePolicyJob(null)
        await load()
        return
      }
      if ((response.data.status === 'failed' || response.data.status === 'unknown') && response.data.attempt_count >= 10) {
        toast.error(response.data.error_message || 'ยังยืนยัน stock 0 ไม่ได้ รายการยังคงถูกบล็อก')
        return
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1000))
    }
    toast.warning('งานตั้ง stock 0 ยังทำอยู่ ระบบจะคงรายการนี้เป็น blocked จนกว่าจะอ่านกลับจาก Shopee สำเร็จ')
  }

  const retryPolicyJob = async () => {
    if (!activePolicyJob) return
    try {
      const response = await client.post<MarketplaceStockPolicyJob>(`/api/marketplace-aliases/policy-jobs/${activePolicyJob.id}/retry`)
      void monitorPolicyJob(response.data)
    } catch (error) {
      toast.error(errorMessage(error, 'สั่งลองตั้ง stock 0 ใหม่ไม่สำเร็จ'))
    }
  }

  const recoverPolicyJob = async (aliasID: string) => {
    try {
      const response = await client.get<MarketplaceStockPolicyJob>(`/api/marketplace-aliases/${aliasID}/policy-job`)
      if (response.data.status === 'completed') {
        toast.success('งานเดิมยืนยัน stock 0 สำเร็จแล้ว กำลังรีเฟรชรายการ')
        await load()
        return
      }
      void monitorPolicyJob(response.data)
    } catch (error) {
      toast.error(errorMessage(error, 'ไม่พบสถานะงานตั้ง stock 0 กรุณาติดต่อผู้ดูแลระบบ'))
    }
  }

  const confirmAction = async () => {
    if (!action) return
    try {
      if (action.kind === 'confirm') {
        const { group, product, conversion } = action
        const response = await client.post<{ job: MarketplaceMappingJob; policy_job?: MarketplaceStockPolicyJob }>('/api/marketplace-aliases/confirm', {
          source: group.source,
          account_key: group.account_key,
          external_item_id: group.external_item_id,
          external_variant_id: group.external_variant_id,
          bill_type: 'sale',
          source_sku: group.source_sku,
          raw_name: group.raw_name,
          normalized_key: group.normalized_key,
          item_code: product.item_code,
          unit_code: conversion.unitCode,
          quantity_multiplier: conversion.quantityMultiplier,
          sales_enabled: conversion.salesEnabled,
          stock_policy: conversion.stockPolicy,
          acknowledge_manual_unmanaged: conversion.acknowledgeManualUnmanaged,
          expected_mapping_revision: action.impact.current_mapping_revision,
          impact_digest: action.impact.impact_digest,
        })
        toast.success('บันทึก revision แล้ว กำลังปรับรายการเปิดแบบ background')
        void monitorJob(response.data.job)
        if (response.data.policy_job) void monitorPolicyJob(response.data.policy_job)
      } else if (action.kind === 'update') {
        const response = await client.put<{ job: MarketplaceMappingJob; policy_job?: MarketplaceStockPolicyJob }>(`/api/marketplace-aliases/${action.alias.id}`, {
          item_code: action.product.item_code,
          unit_code: action.conversion.unitCode,
          bill_type: 'sale',
          quantity_multiplier: action.conversion.quantityMultiplier,
          sales_enabled: action.conversion.salesEnabled,
          stock_policy: action.conversion.stockPolicy,
          acknowledge_manual_unmanaged: action.conversion.acknowledgeManualUnmanaged,
          expected_mapping_revision: action.impact.current_mapping_revision,
          impact_digest: action.impact.impact_digest,
        })
        toast.success('บันทึก revision แล้ว กำลังปรับรายการเปิดแบบ background')
        void monitorJob(response.data.job)
        if (response.data.policy_job) void monitorPolicyJob(response.data.policy_job)
      } else {
        const response = await client.delete<{ job: MarketplaceMappingJob }>(`/api/marketplace-aliases/${action.alias.id}`, {
          data: { expected_mapping_revision: action.impact.current_mapping_revision, impact_digest: action.impact.impact_digest },
        })
        toast.success('หยุดใช้ revision ใหม่แล้ว กำลังตรวจรายการเปิดแบบ background')
        void monitorJob(response.data.job)
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
	setGroupCursor('')
	setGroupCursorHistory([])
  }

	const resetProductPage = () => {
	  setPage(1)
	  setGroupCursor('')
	  setGroupCursorHistory([])
	}

	const goNext = () => {
	  if (groupedSaved && nextGroupCursor) {
		setGroupCursorHistory((value) => [...value, groupCursor])
		setGroupCursor(nextGroupCursor)
		return
	  }
	  setPage((value) => value + 1)
	}

	const goPrevious = () => {
	  if (groupedSaved) {
		const previous = groupCursorHistory[groupCursorHistory.length - 1] ?? ''
		setGroupCursorHistory((value) => value.slice(0, -1))
		setGroupCursor(previous)
		return
	  }
	  setPage((value) => value - 1)
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

      {readiness && (!readiness.catalog_generation_ready || !readiness.mapping_backfill_ready || !readiness.reservation_ledger_ready) && (
        <div className="rounded-lg border border-warning/40 bg-warning/5 p-3 text-sm">
          <p className="font-medium">กำลังเตรียมข้อมูล conversion สำหรับ tenant นี้</p>
          <p className="mt-1 text-xs text-muted-foreground">ยังไม่เปิด active mode จนกว่า Catalog, bill snapshots และ reservation ledger จะครบ ระหว่างนี้รายการกำกวมจะถูกบล็อกไว้</p>
          <div className="mt-2 flex flex-wrap gap-2">{readiness.jobs.map((job) => <Badge key={job.id} variant="outline">{job.job_type} · {job.status} · {job.processed_count.toLocaleString()}</Badge>)}</div>
          {readiness.jobs.find((job) => job.status === 'failed' && job.attempt_count >= 10)?.error_message && <p className="mt-2 text-xs text-destructive">{readiness.jobs.find((job) => job.status === 'failed' && job.attempt_count >= 10)?.error_message}</p>}
        </div>
      )}

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
				  if (event.key === 'Enter') { setQuery(draft.trim()); resetProductPage() }
                }}
                placeholder={tab === 'pending' ? 'ค้นหาสินค้าที่ต้องจับคู่' : 'ค้นหาสินค้า Marketplace หรือรหัส SML'}
                className="pl-8 pr-8"
              />
              {(draft || query) && (
				<button type="button" aria-label="ล้างคำค้นหา" className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" onClick={() => { setDraft(''); setQuery(''); resetProductPage() }}>
                  <X className="h-4 w-4" />
                </button>
              )}
            </div>
			<Button variant="outline" onClick={() => { setQuery(draft.trim()); resetProductPage() }}>ค้นหา</Button>
			<Select value={source} onValueChange={(value) => { setSource(value as SourceFilter); resetProductPage() }}>
              <SelectTrigger className="w-[145px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">ทุกช่องทาง</SelectItem>
                <SelectItem value="shopee">Shopee</SelectItem>
                <SelectItem value="lazada">Lazada</SelectItem>
                <SelectItem value="tiktok">TikTok</SelectItem>
              </SelectContent>
            </Select>
			{tab === 'saved' && groupedAvailable !== false && (
			  <Select value={status} onValueChange={(value) => { setStatus(value as StatusFilter); resetProductPage() }}>
				<SelectTrigger className="w-[160px]"><SelectValue /></SelectTrigger>
				<SelectContent>
				  <SelectItem value="all">ทุกสถานะ</SelectItem>
				  <SelectItem value="ready">พร้อมใช้งาน</SelectItem>
				  <SelectItem value="fix">ต้องตรวจสอบ</SelectItem>
				  <SelectItem value="disabled">ปิดใช้งาน</SelectItem>
				</SelectContent>
			  </Select>
			)}
          </div>

          <TabsContent value="pending" className="m-0">
            <PendingTable loading={loading} rows={pending} canManage={canManage} onPick={setPickerGroup} />
          </TabsContent>
          <TabsContent value="saved" className="m-0">
			{groupedAvailable === true ? <SavedGroupedTable
			  key={`${source}:${query}:${status}:${groupCursor}`}
			  loading={loading}
			  rows={savedGroups}
			  query={query}
			  status={status}
			  canManage={canManage}
			  onEdit={setPickerAlias}
			  onDelete={(alias) => void prepareDelete(alias)}
			/> : <SavedTable
              loading={loading}
              rows={saved}
              canManage={canManage}
              onEdit={setPickerAlias}
              onDelete={(alias) => void prepareDelete(alias)}
			/>}
          </TabsContent>

          <div className="flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs text-muted-foreground">
			<span>{tab === 'pending' ? `${total.toLocaleString()} สินค้าที่ต้องจับคู่ · ${pendingItems.toLocaleString()} รายการในหน้านี้` : groupedAvailable === true ? `${savedGroups.length.toLocaleString()} สินค้าหลักในหน้านี้` : `${total.toLocaleString()} การจับคู่ที่ใช้งานอยู่`} · หน้า {currentPage}/{pages}</span>
            <div className="flex gap-1">
			  <Button size="icon" variant="outline" className="h-8 w-8" disabled={(groupedSaved ? groupCursorHistory.length === 0 : page <= 1) || loading} onClick={goPrevious} aria-label="หน้าก่อน"><ChevronLeft className="h-4 w-4" /></Button>
			  <Button size="icon" variant="outline" className="h-8 w-8" disabled={(groupedSaved ? !nextGroupCursor : page >= pages) || loading} onClick={goNext} aria-label="หน้าถัดไป"><ChevronRight className="h-4 w-4" /></Button>
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
          onPick={(_, __, product) => { if (product) setConversionEditor({ kind: 'confirm', group: pickerGroup, product }); setPickerGroup(null) }}
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
          onPick={(_, __, product) => { if (product) setConversionEditor({ kind: 'update', alias: pickerAlias, product }); setPickerAlias(null) }}
          onClose={() => setPickerAlias(null)}
        />
      )}

      <ConversionConfigDialog
		value={conversionEditor}
		onClose={() => setConversionEditor(null)}
		onRecoverPolicyJob={(aliasID) => recoverPolicyJob(aliasID)}
		onContinue={async (conversion) => {
		  const editor = conversionEditor
		  setConversionEditor(null)
		  if (!editor) return
		  if (editor.kind === 'confirm') await prepareConfirm(editor.group, editor.product, conversion)
		  else await prepareUpdate(editor.alias, editor.product, conversion)
		}}
	  />

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
      {activeJob && <div className="fixed inset-x-0 bottom-4 z-50 mx-auto flex w-fit max-w-[calc(100vw-2rem)] items-center gap-2 rounded-md border bg-background px-3 py-2 text-sm shadow-md"><Loader2 className={cn('h-4 w-4', (activeJob.status === 'queued' || activeJob.status === 'running') && 'animate-spin')} /><span>{activeJob.status === 'failed' ? `งานล้มเหลว: ${activeJob.error_message || 'กรุณาลองใหม่'}` : `กำลังปรับ Product Master · ทำแล้ว ${activeJob.processed_count.toLocaleString()} รายการ`}</span>{activeJob.status === 'failed' && canManage && <Button size="sm" variant="outline" onClick={() => void retryActiveJob()}><RefreshCw className="h-3.5 w-3.5" />ลองใหม่</Button>}</div>}
      {activePolicyJob && <div className="fixed inset-x-0 bottom-16 z-50 mx-auto flex w-fit max-w-[calc(100vw-2rem)] items-center gap-2 rounded-md border border-warning/40 bg-background px-3 py-2 text-sm shadow-md"><Loader2 className={cn('h-4 w-4', activePolicyJob.attempt_count < 10 && 'animate-spin')} /><span>{activePolicyJob.attempt_count >= 10 && (activePolicyJob.status === 'failed' || activePolicyJob.status === 'unknown') ? `ยืนยัน stock 0 ไม่สำเร็จ: ${activePolicyJob.error_message || 'กรุณาลองใหม่'}` : 'กำลังตั้ง stock Shopee เป็น 0 และตรวจยืนยัน'}</span>{activePolicyJob.attempt_count >= 10 && canManage && <Button size="sm" variant="outline" onClick={() => void retryPolicyJob()}><RefreshCw className="h-3.5 w-3.5" />ลองใหม่</Button>}</div>}
    </div>
  )
}

function ConversionConfigDialog({ value, onClose, onContinue, onRecoverPolicyJob }: {
  value: ConversionEditor | null
  onClose: () => void
  onContinue: (config: ConversionConfig) => Promise<void>
  onRecoverPolicyJob: (aliasID: string) => Promise<void>
}) {
  const [units, setUnits] = useState<UnitOption[]>([])
  const [unitCode, setUnitCode] = useState('')
  const [multiplier, setMultiplier] = useState('1')
  const [salesEnabled, setSalesEnabled] = useState(true)
  const [stockPolicy, setStockPolicy] = useState<MarketplaceItemAlias['stock_policy']>('blocked')
  const [manualUnmanagedAcknowledged, setManualUnmanagedAcknowledged] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [loadingUnits, setLoadingUnits] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [unitError, setUnitError] = useState('')

  const source = value?.kind === 'confirm' ? value.group.source : value?.alias.source
  const product = value?.product
  const currentStockPolicy: MarketplaceItemAlias['stock_policy'] = value?.kind === 'update' ? value.alias.stock_policy : 'blocked'
  const stockPolicyOptions: Array<{ value: MarketplaceItemAlias['stock_policy']; label: string; disabled?: boolean }> = currentStockPolicy === 'managed'
    ? [
        { value: 'managed', label: 'ให้ระบบส่งสต๊อก' },
        { value: 'zeroing', label: 'ตั้งสต๊อกเป็น 0 แล้วหยุดส่ง' },
        { value: 'manual_unmanaged', label: 'ไม่ส่งสต๊อก ฉันจัดการเอง' },
      ]
    : currentStockPolicy === 'zeroing'
      ? [{ value: 'zeroing', label: 'กำลังตั้งสต๊อกเป็น 0 เพื่อหยุด' }]
      : currentStockPolicy === 'disabled_zero'
        ? [
            { value: 'disabled_zero', label: 'หยุดแล้ว (สต๊อกเป็น 0)', disabled: true },
            { value: 'managed', label: 'ให้ระบบส่งสต๊อกอีกครั้ง' },
            { value: 'manual_unmanaged', label: 'ไม่ส่งสต๊อก ฉันจัดการเอง' },
          ]
        : currentStockPolicy === 'manual_unmanaged'
          ? [
              { value: 'manual_unmanaged', label: 'ไม่ส่งสต๊อก ฉันจัดการเอง' },
              { value: 'managed', label: 'ให้ระบบส่งสต๊อก' },
              { value: 'zeroing', label: 'ตั้งสต๊อกเป็น 0 แล้วหยุดส่ง' },
            ]
          : [
              { value: 'managed', label: 'ให้ระบบส่งสต๊อก' },
              { value: 'zeroing', label: 'ตั้งสต๊อกเป็น 0 แล้วหยุดส่ง' },
              { value: 'manual_unmanaged', label: 'ไม่ส่งสต๊อก ฉันจัดการเอง' },
            ]
  useEffect(() => {
    if (!value || !product) return
    const existing = value.kind === 'update' ? value.alias : null
    setMultiplier(String(existing?.quantity_multiplier || 1))
    setSalesEnabled(existing?.sales_enabled ?? true)
    setStockPolicy(source === 'shopee' ? editableMarketplaceStockPolicy(existing?.stock_policy) : 'blocked')
    setManualUnmanagedAcknowledged(existing?.stock_policy === 'manual_unmanaged')
    setAdvancedOpen(existing?.stock_policy === 'manual_unmanaged' || existing?.stock_policy === 'zeroing' || existing?.stock_policy === 'disabled_zero' || existing?.sales_enabled === false)
    setUnitCode(existing?.item_code === product.item_code ? existing.unit_code : product.unit_code)
    setUnits([])
    setUnitError('')
    let active = true
    setLoadingUnits(true)
    client.get<{ units: UnitOption[] }>(`/api/catalog/${encodeURIComponent(product.item_code)}/units`)
      .then((response) => {
        if (!active) return
        const rows = response.data.units ?? []
        setUnits(rows)
        const preferred = existing?.item_code === product.item_code ? existing.unit_code : product.unit_code
        setUnitCode(rows.some((unit) => unit.code === preferred) ? preferred : (rows.find((unit) => unit.is_default)?.code || rows[0]?.code || preferred))
        if (rows.length === 0) setUnitError('ยังไม่มีหน่วยนับที่พิสูจน์ conversion ได้ รายการจะถูกบันทึกเป็น “ต้องตรวจสอบ”')
      })
      .catch(() => {
        if (active) setUnitError('โหลดหน่วยนับไม่สำเร็จ กรุณาซิงก์ Catalog แล้วลองใหม่')
      })
      .finally(() => { if (active) setLoadingUnits(false) })
    return () => { active = false }
  }, [product, source, value])

  const multiplierValue = Number(multiplier)
  const multiplierValid = Number.isInteger(multiplierValue) && multiplierValue >= 1 && multiplierValue <= 1_000_000
  const selectedUnit = units.find((unit) => unit.code === unitCode)
  const stand = selectedUnit?.stand_value_exact || selectedUnit?.stand_value
  const divide = selectedUnit?.divide_value_exact || selectedUnit?.divide_value
  const baseFactor = stand && divide && Number(divide) > 0 ? multiplierValue * Number(stand) / Number(divide) : null
  const requiresManualAcknowledgement = source === 'shopee' && stockPolicy === 'manual_unmanaged' && currentStockPolicy !== 'manual_unmanaged'
  const zeroingInProgress = source === 'shopee' && currentStockPolicy === 'zeroing'
  const canSubmit = Boolean(product && selectedUnit && multiplierValid && !unitError && !loadingUnits && !submitting && !zeroingInProgress && (!requiresManualAcknowledgement || manualUnmanagedAcknowledged))
  const submitDisabledReason = loadingUnits
    ? 'กำลังโหลดหน่วยนับ'
    : unitError || (zeroingInProgress ? 'กำลังตั้ง stock เป็น 0 กรุณารอให้งานเดิมเสร็จ' : !selectedUnit ? 'ต้องเลือกหน่วยที่มี conversion จาก Catalog' : !multiplierValid ? 'จำนวนต้องเป็นเลขจำนวนเต็ม 1 ถึง 1,000,000' : requiresManualAcknowledgement && !manualUnmanagedAcknowledged ? 'ต้องยืนยันว่าจะจัดการ stock Shopee เอง' : '')

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    try {
      await onContinue({
        unitCode,
        quantityMultiplier: multiplierValue,
        salesEnabled,
        stockPolicy: source === 'shopee' ? stockPolicy : 'blocked',
        acknowledgeManualUnmanaged: requiresManualAcknowledgement && manualUnmanagedAcknowledged,
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={value !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>กำหนดหน่วยและการตัดสต๊อก</DialogTitle>
          <DialogDescription>{product ? `${product.item_code} · ${product.item_name}` : 'ตรวจค่าที่ใช้กับตัวเลือก Marketplace นี้'}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="grid gap-2">
            <Label htmlFor="marketplace-unit">หน่วย SML</Label>
            <Select value={unitCode} onValueChange={setUnitCode} disabled={loadingUnits || units.length === 0}>
              <SelectTrigger id="marketplace-unit"><SelectValue placeholder={loadingUnits ? 'กำลังโหลดหน่วย...' : 'เลือกหน่วย SML'} /></SelectTrigger>
              <SelectContent>{units.map((unit) => <SelectItem key={unit.code} value={unit.code}>{unit.code}{unit.name_1 && unit.name_1 !== unit.code ? ` · ${unit.name_1}` : ''}{unit.stand_value && unit.divide_value ? ` (${unit.stand_value}/${unit.divide_value})` : ''}</SelectItem>)}</SelectContent>
            </Select>
            {unitError && <p className="text-xs text-warning">{unitError}</p>}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="marketplace-multiplier">จำนวนหน่วย SML ต่อ 1 รายการ Marketplace</Label>
            <Input id="marketplace-multiplier" type="number" min={1} max={1_000_000} step={1} value={multiplier} onChange={(event) => setMultiplier(event.target.value)} aria-invalid={!multiplierValid} />
            {!multiplierValid && <p className="text-xs text-destructive">กรอกจำนวนเต็มตั้งแต่ 1 ถึง 1,000,000</p>}
          </div>
          <div className="rounded-md border bg-muted/30 p-3 text-sm">
            <p className="font-medium">สูตรที่จะใช้</p>
            <p className="mt-1 text-muted-foreground">1 Marketplace = {multiplierValid ? multiplierValue.toLocaleString() : '-'} {unitCode || 'หน่วย SML'}{baseFactor != null ? ` = ${baseFactor.toLocaleString('th-TH', { maximumFractionDigits: 6 })} หน่วยฐาน` : ''}</p>
            <p className="mt-1 text-xs text-muted-foreground">จำนวนจาก Marketplace จะคูณค่านี้ตอนส่ง SML และหารกลับด้วยสูตรเดียวกันตอนคำนวณสต๊อก</p>
          </div>
          <p className="text-xs text-muted-foreground">หลังบันทึก ระบบจะใช้หน่วยนี้กับออเดอร์ใหม่ และเตรียมการซิงก์สต๊อกให้โดยอัตโนมัติ</p>
          <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen} className="rounded-md border">
            <CollapsibleTrigger asChild>
              <Button type="button" variant="ghost" className="h-auto w-full justify-between rounded-md px-3 py-2.5 font-normal">
                <span className="flex items-center gap-2 text-sm"><Settings2 className="h-4 w-4" />ตัวเลือกเพิ่มเติม</span>
                <ChevronDown className={cn('h-4 w-4 transition-transform', advancedOpen && 'rotate-180')} />
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent className="space-y-4 border-t p-3">
              <div className="flex items-center justify-between gap-4">
                <div><Label htmlFor="marketplace-sales-enabled">ส่งเอกสารขายเข้า SML</Label><p className="text-xs text-muted-foreground">ใช้เฉพาะกรณีที่ต้องการเก็บออเดอร์ไว้ แต่ยังไม่ส่งเอกสาร</p></div>
                <Switch id="marketplace-sales-enabled" checked={salesEnabled} onCheckedChange={setSalesEnabled} />
              </div>
              {source === 'shopee' && (
                <div className="grid gap-2 border-t pt-3">
                  <Label htmlFor="marketplace-stock-policy">การส่งสต๊อก Shopee</Label>
                  <Select
                    value={stockPolicy}
                    disabled={currentStockPolicy === 'zeroing'}
                    onValueChange={(nextValue) => {
                      const nextPolicy = nextValue as MarketplaceItemAlias['stock_policy']
                      setStockPolicy(nextPolicy)
                      setManualUnmanagedAcknowledged(nextPolicy === 'manual_unmanaged' && currentStockPolicy === 'manual_unmanaged')
                    }}
                  >
                    <SelectTrigger id="marketplace-stock-policy"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {stockPolicyOptions.map((option) => <SelectItem key={option.value} value={option.value} disabled={option.disabled}>{option.label}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  {stockPolicy === 'manual_unmanaged' && (
                    <div className="space-y-2 rounded-md border border-warning/40 bg-warning/5 p-3">
                      <p className="text-xs text-warning">ระบบจะไม่ส่งสต๊อกให้รายการนี้ คุณต้องดูแลจำนวนใน Shopee เอง และจะใช้สต๊อกร่วมกับรายการที่ระบบดูแลไม่ได้</p>
                      {requiresManualAcknowledgement && (
                        <label className="flex cursor-pointer items-start gap-2 text-xs">
                          <Checkbox checked={manualUnmanagedAcknowledged} onCheckedChange={(checked) => setManualUnmanagedAcknowledged(checked === true)} />
                          <span>ฉันรับทราบว่าจะตรวจและจัดการจำนวนสต๊อกของตัวเลือกนี้ใน Shopee เอง</span>
                        </label>
                      )}
                    </div>
                  )}
                  {stockPolicy === 'zeroing' && (
                    <div className="flex items-center justify-between gap-2 rounded-md border border-warning/40 bg-warning/5 p-3">
                      <p className="text-xs text-warning">{zeroingInProgress ? 'กำลังรอ Shopee ยืนยันสต๊อก 0 จึงยังแก้รายการนี้ไม่ได้' : 'ระบบจะตั้งสต๊อกเป็น 0 และตรวจยืนยันกับ Shopee ก่อนหยุดส่ง'}</p>
                      {zeroingInProgress && value?.kind === 'update' && <Button type="button" size="sm" variant="outline" onClick={() => void onRecoverPolicyJob(value.alias.id)}>ดูสถานะงาน</Button>}
                    </div>
                  )}
                </div>
              )}
            </CollapsibleContent>
          </Collapsible>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={submitting}>ยกเลิก</Button>
          <Button type="button" onClick={() => void submit()} disabled={!canSubmit}>{submitting && <Loader2 className="h-4 w-4 animate-spin" />}ตรวจสอบและบันทึก</Button>
        </DialogFooter>
        {!canSubmit && !submitting && submitDisabledReason && <p className="text-right text-xs text-muted-foreground">ยังดำเนินการไม่ได้: {submitDisabledReason}</p>}
      </DialogContent>
    </Dialog>
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

type GroupVariantState = { rows: MarketplaceItemAlias[]; nextCursor: string; loading: boolean; error: string }

function SavedGroupedTable({
	loading,
	rows,
	query,
	status,
	canManage,
	onEdit,
	onDelete,
}: {
	loading: boolean
	rows: MarketplaceProductGroup[]
	query: string
	status: StatusFilter
	canManage: boolean
	onEdit: (row: MarketplaceItemAlias) => void
	onDelete: (row: MarketplaceItemAlias) => void
}) {
	const [expanded, setExpanded] = useState<Set<string>>(new Set())
	const [variants, setVariants] = useState<Record<string, GroupVariantState>>({})

	useEffect(() => {
		setExpanded(new Set())
		setVariants({})
	}, [rows])

	const groupKey = (group: MarketplaceProductGroup) => `${group.source}\u0000${group.account_key}\u0000${group.parent_key}`
	const loadVariants = async (group: MarketplaceProductGroup, append = false) => {
		const key = groupKey(group)
		const current = variants[key]
		setVariants((value) => ({ ...value, [key]: { rows: append ? current?.rows ?? [] : [], nextCursor: current?.nextCursor ?? '', loading: true, error: '' } }))
		try {
			const response = await client.get<MarketplaceCursorPage<MarketplaceItemAlias>>(
				`/api/marketplace-aliases/product-groups/${encodeURIComponent(group.parent_key)}/variants`,
				{ params: {
					source: group.source,
					account_key: group.account_key,
					q: query || undefined,
					status: status === 'all' ? undefined : status,
					cursor: append ? current?.nextCursor || undefined : undefined,
					limit: 100,
				} },
			)
			setVariants((value) => ({
				...value,
				[key]: {
					rows: append ? [...(current?.rows ?? []), ...(response.data.data ?? [])] : response.data.data ?? [],
					nextCursor: response.data.next_cursor ?? '',
					loading: false,
					error: '',
				},
			}))
		} catch (error) {
			setVariants((value) => ({ ...value, [key]: { rows: current?.rows ?? [], nextCursor: current?.nextCursor ?? '', loading: false, error: errorMessage(error, 'โหลดตัวเลือกไม่สำเร็จ') } }))
		}
	}

	const toggleGroup = (group: MarketplaceProductGroup) => {
		const key = groupKey(group)
		setExpanded((value) => {
			const next = new Set(value)
			if (next.has(key)) next.delete(key)
			else next.add(key)
			return next
		})
		if (!variants[key]) void loadVariants(group)
	}

	if (!loading && rows.length === 0) {
		return <EmptyState icon={Tags} title="ไม่พบสินค้าหลัก" description="ลองเปลี่ยนคำค้นหา ช่องทาง หรือสถานะ" />
	}
	return (
		<div className="overflow-x-auto">
			<Table>
				<TableHeader><TableRow><TableHead className="min-w-[360px]">สินค้าหลัก Marketplace</TableHead><TableHead className="min-w-[220px]">ตัวเลือก</TableHead><TableHead className="min-w-[150px]">ช่องทางและร้าน</TableHead><TableHead className="w-[92px] text-right">เปิดดู</TableHead></TableRow></TableHeader>
				<TableBody>
					{loading ? Array.from({ length: 6 }).map((_, index) => <TableRow key={index}><TableCell colSpan={4}><Skeleton className="h-10 w-full" /></TableCell></TableRow>) : rows.map((group) => {
						const key = groupKey(group)
						const isOpen = expanded.has(key)
						const child = variants[key]
						return (
							<Fragment key={key}>
								<TableRow key={`${key}:parent`} className={cn(isOpen && 'bg-muted/25')}>
									<TableCell>
										<button type="button" className="w-full text-left" aria-expanded={isOpen} onClick={() => toggleGroup(group)}>
											<div className="line-clamp-2 font-medium">{group.product_name || 'ไม่พบชื่อสินค้าหลัก'}</div>
											<div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
												<span className="font-mono">{group.parent_key}</span>
												{group.parent_key_kind === 'derived' && <Badge variant="outline">จัดกลุ่มจากชื่อ</Badge>}
											</div>
										</button>
									</TableCell>
									<TableCell>
										<div className="font-medium tabular-nums">{group.variant_count.toLocaleString()} ตัวเลือก</div>
										<div className="mt-1 flex flex-wrap gap-1">
											{group.ready_count > 0 && <Badge variant="outline" className="border-success/30 bg-success/10 text-success">พร้อม {group.ready_count}</Badge>}
											{group.fix_count > 0 && <Badge variant="outline" className="border-warning/30 bg-warning/10 text-warning">ตรวจสอบ {group.fix_count}</Badge>}
											{group.disabled_count > 0 && <Badge variant="secondary">ปิด {group.disabled_count}</Badge>}
										</div>
									</TableCell>
									<TableCell><ChannelAccount source={group.source} accountName={group.account_name} accountKey={group.account_key} /></TableCell>
									<TableCell className="text-right"><Button type="button" size="icon" variant="ghost" className="h-8 w-8" onClick={() => toggleGroup(group)} aria-label={isOpen ? 'ซ่อนตัวเลือก' : 'แสดงตัวเลือก'} aria-expanded={isOpen}><ChevronDown className={cn('h-4 w-4 transition-transform', isOpen && 'rotate-180')} /></Button></TableCell>
								</TableRow>
								{isOpen && (
									<TableRow key={`${key}:children`} className="hover:bg-transparent">
										<TableCell colSpan={4} className="bg-muted/10 p-0">
											{child?.loading && child.rows.length === 0 && <div className="flex items-center justify-center gap-2 px-4 py-8 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />กำลังโหลดตัวเลือก</div>}
											{child?.error && <div className="flex items-center justify-between gap-3 px-4 py-4 text-sm text-destructive"><span>{child.error}</span><Button size="sm" variant="outline" onClick={() => void loadVariants(group)}>ลองใหม่</Button></div>}
											{child && !child.loading && !child.error && child.rows.length === 0 && <div className="px-4 py-6 text-center text-sm text-muted-foreground">ไม่พบตัวเลือกที่ตรงกับตัวกรอง</div>}
											{child?.rows.map((alias) => <GroupedVariantRow key={alias.id} alias={alias} canManage={canManage} onEdit={onEdit} onDelete={onDelete} />)}
											{child?.nextCursor && <div className="flex justify-center border-t px-4 py-2"><Button size="sm" variant="outline" disabled={child.loading} onClick={() => void loadVariants(group, true)}>{child.loading && <Loader2 className="h-4 w-4 animate-spin" />}โหลดตัวเลือกเพิ่ม</Button></div>}
										</TableCell>
									</TableRow>
								)}
							</Fragment>
						)
					})}
				</TableBody>
			</Table>
		</div>
	)
}

function GroupedVariantRow({ alias, canManage, onEdit, onDelete }: { alias: MarketplaceItemAlias; canManage: boolean; onEdit: (row: MarketplaceItemAlias) => void; onDelete: (row: MarketplaceItemAlias) => void }) {
	const status = alias.conversion_status === 'ready' && alias.sales_enabled ? 'ready' : alias.sales_enabled ? 'review' : 'disabled'
	return (
		<div className="grid min-h-16 grid-cols-[minmax(240px,1.25fr)_minmax(220px,1fr)_minmax(190px,0.8fr)_88px] items-center gap-4 border-t px-4 py-2 text-sm first:border-t-0">
			<div className="min-w-0 pl-8">
				<div className="line-clamp-2 font-medium">{alias.source_variant_name || alias.raw_name || 'ไม่มีชื่อตัวเลือก'}</div>
				<div className="mt-1 text-xs text-muted-foreground">SKU: <span className="font-mono text-foreground">{alias.source_sku || '-'}</span></div>
			</div>
			<div className="min-w-0"><div className="font-mono font-semibold">{alias.item_code}</div><div className="truncate text-xs text-muted-foreground">{alias.item_name || 'ไม่พบชื่อสินค้า SML'} · {alias.unit_code || '-'}</div></div>
			<div>
				<Badge variant="outline" className={cn(status === 'ready' && 'border-success/30 bg-success/10 text-success', status === 'review' && 'border-warning/30 bg-warning/10 text-warning')}>{status === 'ready' ? 'พร้อมใช้งาน' : status === 'review' ? 'ต้องตรวจสอบ' : 'ปิดใช้งาน'}</Badge>
				<div className="mt-1 text-xs text-muted-foreground">{conversionSummary(alias)}</div>
			</div>
			<div className="flex justify-end gap-1">{canManage ? <><Button size="icon" variant="outline" className="h-8 w-8" onClick={() => onEdit(alias)} aria-label="แก้ไขตัวเลือก"><Pencil className="h-4 w-4" /></Button><Button size="icon" variant="outline" className="h-8 w-8 text-destructive" onClick={() => onDelete(alias)} aria-label="หยุดใช้ตัวเลือก"><Unlink className="h-4 w-4" /></Button></> : <span className="text-xs text-muted-foreground">ดูอย่างเดียว</span>}</div>
		</div>
	)
}

function conversionSummary(alias: MarketplaceItemAlias) {
	const multiplier = alias.quantity_multiplier || 1
	if (!alias.unit_stand_value || !alias.unit_divide_value) return `1 Marketplace = ${multiplier} ${alias.unit_code || 'หน่วย SML'}`
	const stand = Number(alias.unit_stand_value)
	const divide = Number(alias.unit_divide_value)
	const factor = Number.isFinite(stand) && Number.isFinite(divide) && divide > 0 ? (multiplier * stand) / divide : null
	const base = factor === null ? `${multiplier} × ${alias.unit_stand_value}/${alias.unit_divide_value}` : factor.toLocaleString('th-TH', { maximumFractionDigits: 6 })
	return `1 Marketplace = ${multiplier} ${alias.unit_code || 'หน่วย SML'} = ${base} หน่วยฐาน`
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
	const impactText = `${marketplaceImpactFormulaLines(impact).join('\n')}\nออเดอร์ที่ยังไม่ส่ง: ${impact.open_items.toLocaleString()} รายการ ใน ${impact.open_bills.toLocaleString()} บิล${impact.manual_override_items ? ` · ข้าม manual override ${impact.manual_override_items.toLocaleString()}` : ''}\nไม่แก้ย้อนหลัง: attempted/sent ${impact.attempted_items.toLocaleString()} · archived ${impact.archived_items.toLocaleString()}\nReservation ที่ต้อง reconcile: ${impact.reservation_moves.toLocaleString()}\nการซิงก์สต๊อกที่เกี่ยวข้อง: ${impact.stock_mappings.toLocaleString()} รายการ ใน ${impact.affected_shop_ids.length.toLocaleString()} ร้าน${impact.stock_conflicts ? ` · พบการจับคู่สต๊อกซ้ำ ${impact.stock_conflicts.toLocaleString()}` : ''}${impact.dry_run_required ? '\nหลังบันทึกต้องตรวจสต๊อกใหม่ก่อนเปิดซิงก์' : ''}`
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
