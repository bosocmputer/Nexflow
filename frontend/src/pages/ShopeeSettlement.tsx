import { Component, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import axios from 'axios'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  Clock,
  Eye,
  EyeOff,
  Loader2,
  ReceiptText,
  RefreshCw,
  RotateCcw,
  Search,
  Send,
  Settings,
  Store,
  type LucideIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { DateRangePicker, type DateRangePreset } from '@/components/common/DateRangePicker'
import { PageHeader } from '@/components/common/PageHeader'
import { cn } from '@/lib/utils'

type ShopeeConnection = {
  id: string
  shop_id: number
  label: string
  shop_name?: string
  can_fetch: boolean
  token_state: string
}

type ShopeeAPIStatus = {
  enabled: boolean
  configured: boolean
  blocking_reason?: string
}

type SettlementDefaults = {
  doc_format_code: string
  passbook_code: string
  passbook_name: string
  bank_code: string
  bank_branch: string
  expense_code: string
  expense_name: string
}

type SettlementItem = {
  id: string
  order_sn: string
  payout_amount: number
  escrow_amount: number
  buyer_total_amount: number
  invoice_doc_no?: string
  invoice_doc_date?: string
  cust_code?: string
  invoice_amount: number
  difference_amount: number
  status: string
  block_reason?: string
  receipt_doc_no?: string
  existing_receipt_doc_no?: string
}

type SettlementRun = {
  id: string
  connection_id?: string
  shop_id: number
  shop_label: string
  release_time_from: string
  release_time_to: string
  release_date_from?: string
  release_date_to?: string
  status: string
  total_count: number
  ready_count: number
  blocked_count: number
  sent_count: number
  invoice_amount_total?: number
  payout_amount_total?: number
  difference_amount_total?: number
  ready_invoice_amount?: number
  ready_payout_amount?: number
  ready_difference_amount?: number
  blocked_invoice_amount?: number
  blocked_payout_amount?: number
  blocked_difference_amount?: number
  rc_doc_no?: string
  error_msg?: string
  selected_doc_format_code?: string
  selected_passbook_code?: string
  selected_passbook_name?: string
  selected_expense_code?: string
  selected_expense_name?: string
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
  hidden_at?: string
  hidden_by?: string
  hidden_reason?: string
  items?: SettlementItem[] | null
}

type SettlementCounts = {
  total: number
  running: number
  ready: number
  sending: number
  sent: number
  failed: number
  partial: number
}

type SettlementRunListResponse = {
  data: SettlementRun[]
  total?: number
  page?: number
  per_page?: number
  has_more?: boolean
}

const ALL = 'all'
const VISIBLE = 'visible'
const DEFAULT_COUNTS: SettlementCounts = {
  total: 0,
  running: 0,
  ready: 0,
  sending: 0,
  sent: 0,
  failed: 0,
  partial: 0,
}

const STATUS_OPTIONS = [
  { value: ALL, label: 'ทุกสถานะ' },
  { value: 'running', label: 'กำลังดึง' },
  { value: 'ready', label: 'พร้อมส่ง' },
  { value: 'sending', label: 'กำลังส่ง' },
  { value: 'sent', label: 'ส่งแล้ว' },
  { value: 'failed', label: 'ไม่สำเร็จ' },
  { value: 'partial', label: 'ต้องตรวจ' },
]

const money = (v: number | undefined) =>
  new Intl.NumberFormat('th-TH', { style: 'currency', currency: 'THB' }).format(Number(v ?? 0))
const today = () => dayjs().format('YYYY-MM-DD')
const dateOnly = (v?: string) => {
  if (!v) return '-'
  const datePart = v.slice(0, 10)
  if (/^\d{4}-\d{2}-\d{2}$/.test(datePart)) return dayjs(datePart).format('DD/MM/YYYY')
  return dayjs(v).format('DD/MM/YYYY')
}
const DEFAULT_SETTLEMENT_REMARK = 'รับชำระ Shopee จาก Nexflow'
const settlementReleasePresets: DateRangePreset[] = [
  {
    label: 'วันนี้',
    getRange: () => {
      const d = today()
      return { from: d, to: d }
    },
  },
  {
    label: '7 วัน',
    getRange: () => ({
      from: dayjs().subtract(6, 'day').format('YYYY-MM-DD'),
      to: today(),
    }),
  },
  {
    label: '15 วัน',
    getRange: () => ({
      from: dayjs().subtract(14, 'day').format('YYYY-MM-DD'),
      to: today(),
    }),
  },
]

function settlementItems(run: SettlementRun | null | undefined): SettlementItem[] {
  return Array.isArray(run?.items) ? run.items : []
}

function normalizeSettlementRun(run: SettlementRun | null | undefined): SettlementRun {
  if (!run) throw new Error('missing settlement run')
  return { ...run, items: settlementItems(run) }
}

function normalizeSettlementRuns(runs: SettlementRun[] | null | undefined): SettlementRun[] {
  return Array.isArray(runs) ? runs.map(normalizeSettlementRun) : []
}

function apiError(e: unknown) {
  if (axios.isAxiosError(e)) return e.response?.data?.error || e.response?.data?.message || e.message
  return e instanceof Error ? e.message : 'unknown error'
}

function isActive(status?: string) {
  return status === 'running' || status === 'sending' || status === 'pending'
}

function canHideSettlementRun(run: SettlementRun | null | undefined) {
  if (!run || run.hidden_at || isActive(run.status) || run.status === 'sent') return false
  return Number(run.ready_count || 0) <= 0
}

function primarySettlementItems(run: SettlementRun, items: SettlementItem[]) {
  if (run.status === 'sent') {
    return items.filter((item) => item.status === 'sent' || Boolean(item.receipt_doc_no))
  }
  return items.filter((item) => item.status === 'ready')
}

function skippedSettlementItems(items: SettlementItem[]) {
  return items.filter((item) =>
    item.status === 'blocked'
    || item.status === 'skipped'
    || item.status === 'failed'
    || Boolean(item.block_reason)
    || Boolean(item.existing_receipt_doc_no),
  )
}

function settlementBlockReason(item: SettlementItem) {
  if (item.existing_receipt_doc_no) return `เคยรับชำระแล้วในเอกสาร ${item.existing_receipt_doc_no}`
  const reason = item.block_reason || ''
  if (reason.includes('ไม่พบใบขาย SML') || reason.includes('doc_ref')) {
    return 'ยังไม่พบใบขาย SML ของคำสั่งซื้อนี้ ให้ส่ง/สร้างใบขายใน SML ก่อน แล้วกดรีเฟรชผล'
  }
  return reason
}

class ShopeeSettlementErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="mx-auto max-w-3xl space-y-4 p-6">
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>หน้า รับชำระ Shopee แสดงผลไม่สำเร็จ</AlertTitle>
            <AlertDescription>
              ระบบเจอข้อผิดพลาดระหว่างแสดงข้อมูล กรุณากดโหลดหน้าใหม่ หากยังพบปัญหาให้แจ้งผู้ดูแลระบบ
            </AlertDescription>
          </Alert>
          <Button onClick={() => window.location.reload()} className="gap-2">
            <RefreshCw className="h-4 w-4" />
            โหลดหน้าใหม่
          </Button>
        </div>
      )
    }
    return this.props.children
  }
}

export default function ShopeeSettlement() {
  return (
    <ShopeeSettlementErrorBoundary>
      <ShopeeSettlementContent />
    </ShopeeSettlementErrorBoundary>
  )
}

function ShopeeSettlementContent() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [connections, setConnections] = useState<ShopeeConnection[]>([])
  const [apiStatus, setApiStatus] = useState<ShopeeAPIStatus | null>(null)
  const [defaults, setDefaults] = useState<SettlementDefaults | null>(null)
  const [runs, setRuns] = useState<SettlementRun[]>([])
  const [totalRuns, setTotalRuns] = useState(0)
  const [counts, setCounts] = useState<SettlementCounts>(DEFAULT_COUNTS)
  const [loading, setLoading] = useState(false)
  const [status, setStatus] = useState(ALL)
  const [hiddenMode, setHiddenMode] = useState(VISIBLE)
  const [shopID, setShopID] = useState(ALL)
  const [search, setSearch] = useState('')
  const [dateFrom, setDateFrom] = useState(dayjs().subtract(30, 'day').format('YYYY-MM-DD'))
  const [dateTo, setDateTo] = useState(today())
  const [pullOpen, setPullOpen] = useState(false)
  const [pullConnectionID, setPullConnectionID] = useState('')
  const [pullFrom, setPullFrom] = useState(dayjs().subtract(7, 'day').format('YYYY-MM-DD'))
  const [pullTo, setPullTo] = useState(today())
  const [creatingPreview, setCreatingPreview] = useState(false)
  const [selectedRun, setSelectedRun] = useState<SettlementRun | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [sending, setSending] = useState(false)
  const [hidingRun, setHidingRun] = useState(false)
  const [sendRemark, setSendRemark] = useState(DEFAULT_SETTLEMENT_REMARK)
  const [sendDocDate, setSendDocDate] = useState(today())
  const [sendDocTime, setSendDocTime] = useState(dayjs().format('HH:mm'))
  const page = Math.max(1, Number(searchParams.get('page') || '1') || 1)
  const perPage = 20
  const totalPages = Math.max(1, Math.ceil(totalRuns / perPage))

  const settingsReady = Boolean(defaults?.doc_format_code && defaults?.passbook_code)
  const shopeeAPIEnabled = apiStatus?.enabled !== false
  const visibleConnections = shopeeAPIEnabled ? connections : []
  const selectedItems = settlementItems(selectedRun)
  const readyItems = selectedItems.filter((i) => i.status === 'ready')
  const readyTotals = {
    invoice: Number(selectedRun?.ready_invoice_amount ?? readyItems.reduce((sum, i) => sum + Number(i.invoice_amount || 0), 0)),
    payout: Number(selectedRun?.ready_payout_amount ?? readyItems.reduce((sum, i) => sum + Number(i.payout_amount || 0), 0)),
    diff: Number(selectedRun?.ready_difference_amount ?? readyItems.reduce((sum, i) => sum + Math.max(0, Number(i.difference_amount || 0)), 0)),
  }
  const allTotals = {
    invoice: Number(selectedRun?.invoice_amount_total ?? selectedItems.reduce((sum, i) => sum + Number(i.invoice_amount || 0), 0) ?? 0),
    payout: Number(selectedRun?.payout_amount_total ?? selectedItems.reduce((sum, i) => sum + Number(i.payout_amount || 0), 0) ?? 0),
    diff: Number(selectedRun?.difference_amount_total ?? selectedItems.reduce((sum, i) => sum + Math.max(0, Number(i.difference_amount || 0)), 0) ?? 0),
  }
  const activeDetail = isActive(selectedRun?.status)

  const setPage = (nextPage: number) => {
    const next = new URLSearchParams(searchParams)
    if (nextPage <= 1) next.delete('page')
    else next.set('page', String(nextPage))
    setSearchParams(next)
  }

  const resetPage = () => {
    if (page <= 1) return
    const next = new URLSearchParams(searchParams)
    next.delete('page')
    setSearchParams(next, { replace: true })
  }

  const loadBasics = async () => {
    const [connRes, defaultRes, statusRes] = await Promise.all([
      client.get<{ data: ShopeeConnection[] }>('/api/shopee-api/connections'),
      client.get<{ data: SettlementDefaults }>('/api/settings/shopee-settlement-defaults'),
      client.get<ShopeeAPIStatus>('/api/settings/shopee-api/status'),
    ])
    const conns = connRes.data.data ?? []
    setConnections(conns)
    setApiStatus(statusRes.data)
    if (!statusRes.data.enabled) {
      setPullConnectionID('')
    } else if (!pullConnectionID && conns.length > 0) {
      setPullConnectionID(conns[0].id)
    }
    setDefaults(defaultRes.data.data ?? null)
  }

  const listParams = useMemo(() => {
    const params = new URLSearchParams()
    params.set('page', String(page))
    params.set('per_page', String(perPage))
    if (status !== ALL) params.set('status', status)
    if (hiddenMode === 'only' || hiddenMode === 'all') params.set('hidden', hiddenMode)
    if (shopID !== ALL) params.set('shop_id', shopID)
    if (search.trim()) params.set('search', search.trim())
    if (dateFrom) params.set('date_from', dateFrom)
    if (dateTo) params.set('date_to', dateTo)
    return params
  }, [dateFrom, dateTo, hiddenMode, page, search, shopID, status])

  const countParams = useMemo(() => {
    const params = new URLSearchParams()
    if (status !== ALL) params.set('status', status)
    if (hiddenMode === 'only' || hiddenMode === 'all') params.set('hidden', hiddenMode)
    if (shopID !== ALL) params.set('shop_id', shopID)
    if (search.trim()) params.set('search', search.trim())
    if (dateFrom) params.set('date_from', dateFrom)
    if (dateTo) params.set('date_to', dateTo)
    return params
  }, [dateFrom, dateTo, hiddenMode, search, shopID, status])

  const loadRuns = async () => {
    setLoading(true)
    try {
      const [runRes, countRes] = await Promise.all([
        client.get<SettlementRunListResponse>(`/api/shopee-settlements?${listParams}`),
        client.get<SettlementCounts>(`/api/shopee-settlements/counts?${countParams}`),
      ])
      const nextTotal = typeof runRes.data.total === 'number' ? runRes.data.total : (runRes.data.data ?? []).length
      const nextTotalPages = Math.max(1, Math.ceil(nextTotal / perPage))
      if (page > nextTotalPages) {
        setRuns([])
        setTotalRuns(nextTotal)
        setPage(nextTotalPages)
        return
      }
      setRuns(normalizeSettlementRuns(runRes.data.data))
      setTotalRuns(nextTotal)
      setCounts(countRes.data ?? DEFAULT_COUNTS)
    } catch (e) {
      toast.error('โหลดงานรับชำระ Shopee ไม่สำเร็จ: ' + apiError(e))
      setRuns([])
      setTotalRuns(0)
      setCounts(DEFAULT_COUNTS)
    } finally {
      setLoading(false)
    }
  }

  const refreshSelectedRun = async (id: string) => {
    const res = await client.get<{ data: SettlementRun }>(`/api/shopee-settlements/${id}`)
    const run = normalizeSettlementRun(res.data.data)
    setSelectedRun(run)
    return run
  }

  const reconcileSelectedRun = async (id: string) => {
    const res = await client.post<{ data: SettlementRun }>(`/api/shopee-settlements/${id}/reconcile`)
    const run = normalizeSettlementRun(res.data.data)
    setSelectedRun(run)
    void loadRuns()
    return run
  }

  useEffect(() => {
    loadBasics().catch((e) => toast.error('โหลดข้อมูลตั้งต้นไม่สำเร็จ: ' + apiError(e)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    loadRuns()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listParams, countParams])

  useEffect(() => {
    if (!selectedRun?.id || !activeDetail) return
    const timer = window.setInterval(() => {
      refreshSelectedRun(selectedRun.id)
        .then((run) => {
          if (!isActive(run.status)) {
            if (selectedRun.status === 'sending' && run.status === 'sent' && run.rc_doc_no) {
              toast.success(`ส่งรับชำระเข้า SML สำเร็จ: ${run.rc_doc_no}`)
            }
            void loadRuns()
          }
        })
        .catch(() => undefined)
    }, 2500)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRun?.id, activeDetail])

  const openRun = async (run: SettlementRun) => {
    setDetailOpen(true)
    setSelectedRun(normalizeSettlementRun(run))
    try {
      await refreshSelectedRun(run.id)
    } catch (e) {
      toast.error('โหลดรายละเอียดงานไม่สำเร็จ: ' + apiError(e))
    }
  }

  const startPreview = async () => {
    if (!pullConnectionID) {
      toast.error('กรุณาเลือกร้าน Shopee')
      return
    }
    const fromDate = dayjs(pullFrom)
    const toDate = dayjs(pullTo)
    if (!fromDate.isValid() || !toDate.isValid()) {
      toast.error('กรุณาเลือกวันที่ release ให้ถูกต้อง')
      return
    }
    if (toDate.isBefore(fromDate, 'day')) {
      toast.error('วันที่สิ้นสุดต้องมากกว่าหรือเท่ากับวันที่เริ่มต้น')
      return
    }
    if (toDate.diff(fromDate, 'day') > 14) {
      toast.error('ระบบดึงช่วงวันที่ Shopee release เงินได้ครั้งละไม่เกิน 15 วัน')
      return
    }
    setCreatingPreview(true)
    try {
      const res = await client.post<{ run_id: string; run: SettlementRun }>('/api/shopee-settlements/preview', {
        connection_id: pullConnectionID,
        release_time_from: pullFrom,
        release_time_to: pullTo,
      })
      setPullOpen(false)
      if (res.data.run?.id) {
        setSelectedRun(normalizeSettlementRun(res.data.run))
        setDetailOpen(true)
      } else if (res.data.run_id) {
        await refreshSelectedRun(res.data.run_id)
        setDetailOpen(true)
      }
      toast.success('เริ่มดึงรายการ Shopee ที่ release แล้ว')
      void loadRuns()
    } catch (e) {
      toast.error('ดึงรายการไม่สำเร็จ: ' + apiError(e))
    } finally {
      setCreatingPreview(false)
    }
  }

  const openSendConfirm = () => {
    if (!selectedRun) return
    if (!settingsReady) {
      toast.error('กรุณาตั้งค่ารูปแบบเอกสารรับชำระและบัญชีรับเงินที่หน้าเส้นทางเอกสาร SML')
      return
    }
    if (readyTotals.diff > 0 && !defaults?.expense_code) {
      toast.error('รอบนี้มีส่วนต่าง Shopee กรุณาตั้งค่าใช้จ่ายส่วนต่างก่อนส่ง')
      return
    }
    setSendDocDate(today())
    setSendDocTime(dayjs().format('HH:mm'))
    setSendRemark(DEFAULT_SETTLEMENT_REMARK)
    setConfirmOpen(true)
  }

  const sendReceipt = async () => {
    if (!selectedRun?.id || !defaults) return
    setSending(true)
    try {
      const res = await client.post<{ run: SettlementRun }>(`/api/shopee-settlements/${selectedRun.id}/send`, {
        doc_format_code: defaults.doc_format_code,
        passbook_code: defaults.passbook_code,
        passbook_name: defaults.passbook_name,
        bank_code: defaults.bank_code,
        bank_branch: defaults.bank_branch,
        expense_code: defaults.expense_code,
        expense_name: defaults.expense_name,
        doc_date: sendDocDate,
        doc_time: sendDocTime,
        remark: sendRemark.trim() || DEFAULT_SETTLEMENT_REMARK,
      })
      setSelectedRun(normalizeSettlementRun(res.data.run))
      setConfirmOpen(false)
      toast.success(res.data.run?.rc_doc_no ? `ส่งรับชำระเข้า SML สำเร็จ: ${res.data.run.rc_doc_no}` : 'เริ่มส่งรับชำระเข้า SML')
      void loadRuns()
    } catch (e) {
      toast.error('ส่งรับชำระไม่สำเร็จ: ' + apiError(e))
    } finally {
      setSending(false)
    }
  }

  const hideSelectedRun = async () => {
    if (!selectedRun?.id || hidingRun) return
    if (!window.confirm('ซ่อนงานรับชำระนี้จากรายการปกติใช่ไหม? ข้อมูลประวัติและการกันส่งซ้ำจะยังอยู่ครบ')) return
    setHidingRun(true)
    try {
      const res = await client.post<{ data: SettlementRun }>(`/api/shopee-settlements/${selectedRun.id}/hide`, {
        reason: 'ซ่อนจากรายการโดยผู้ใช้',
      })
      const run = normalizeSettlementRun(res.data.data)
      setSelectedRun(run)
      toast.success('ซ่อนงานรับชำระ Shopee แล้ว')
      if (hiddenMode === VISIBLE) {
        setDetailOpen(false)
        setSelectedRun(null)
      }
      void loadRuns()
    } catch (e) {
      toast.error('ซ่อนงานรับชำระไม่สำเร็จ: ' + apiError(e))
    } finally {
      setHidingRun(false)
    }
  }

  const restoreSelectedRun = async () => {
    if (!selectedRun?.id || hidingRun) return
    setHidingRun(true)
    try {
      const res = await client.post<{ data: SettlementRun }>(`/api/shopee-settlements/${selectedRun.id}/restore`)
      const run = normalizeSettlementRun(res.data.data)
      setSelectedRun(run)
      toast.success('กู้คืนงานรับชำระ Shopee แล้ว')
      if (hiddenMode === 'only') {
        setDetailOpen(false)
        setSelectedRun(null)
      }
      void loadRuns()
    } catch (e) {
      toast.error('กู้คืนงานรับชำระไม่สำเร็จ: ' + apiError(e))
    } finally {
      setHidingRun(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="รับชำระ Shopee"
        description="ดึงรายการ Shopee ที่ release เงินแล้ว จับคู่กับใบขาย SML และส่งเข้าเมนูรับชำระหนี้"
        actions={
          <Button
            className="gap-2"
            onClick={() => setPullOpen(true)}
            disabled={!shopeeAPIEnabled}
            title={!shopeeAPIEnabled ? 'Shopee API ปิดใช้งานใน instance นี้' : undefined}
          >
            <RefreshCw className="h-4 w-4" />
            ดึงรอบถอนเงิน
          </Button>
        }
      />

      <div className="grid grid-cols-2 gap-2 lg:grid-cols-5">
        <QueueMetric label="กำลังทำงาน" value={counts.running + counts.sending} icon={Clock} tone="primary" />
        <QueueMetric label="พร้อมส่ง" value={counts.ready} icon={ReceiptText} tone="success" />
        <QueueMetric label="ส่งแล้ว" value={counts.sent} icon={CheckCircle2} tone="success" />
        <QueueMetric label="ต้องตรวจ" value={counts.partial} icon={AlertTriangle} tone="warning" />
        <QueueMetric label="ผิดพลาด" value={counts.failed} icon={AlertTriangle} tone="danger" />
      </div>

      <div className="rounded-xl border border-border/70 bg-card p-3 shadow-sm">
        <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <ReceiptText className="h-3.5 w-3.5 shrink-0 text-primary" />
          <Link to="/sale-invoices" className="font-medium text-primary hover:underline">
            ขายสินค้าและบริการ
          </Link>
          <span>→</span>
          <span className="font-medium text-foreground">ลูกหนี้ -&gt; รับชำระหนี้</span>
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground">
            RC
          </code>
          <Link to="/settings/channels" className="font-medium text-primary hover:underline sm:ml-auto">
            ตั้งค่าเส้นทาง
          </Link>
        </div>

        {!settingsReady && (
          <Alert className="mb-3 border-warning/40 bg-warning/10 text-warning">
            <Settings className="h-4 w-4" />
            <AlertTitle>ยังตั้งค่ารับชำระ Shopee ไม่ครบ</AlertTitle>
            <AlertDescription>
              กรุณาเลือก doc format รับชำระและบัญชีรับเงินในหน้า{' '}
              <Link to="/settings/channels" className="font-medium underline">
                เส้นทางเอกสาร SML
              </Link>{' '}
              ก่อนส่งเข้า SML
            </AlertDescription>
          </Alert>
        )}
        {apiStatus && !apiStatus.enabled && (
          <Alert className="mb-3 border-warning/40 bg-warning/10 text-warning">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>Shopee API ปิดใช้งานใน instance นี้</AlertTitle>
            <AlertDescription>
              ระบบซ่อนร้าน Shopee ที่เคยเชื่อมต่อไว้ก่อน เพื่อไม่ให้เข้าใจผิดว่าสามารถดึงรอบถอนเงินได้.
              {apiStatus.blocking_reason ? ` ${apiStatus.blocking_reason}` : ' ให้เปิดใช้งาน Shopee API ใน server ก่อนใช้เมนูนี้'}
            </AlertDescription>
          </Alert>
        )}

        <div className="mb-2 flex flex-wrap gap-1.5">
          {STATUS_OPTIONS.map((o) => (
            <button
              key={o.value}
              type="button"
              onClick={() => {
                setStatus(o.value)
                resetPage()
              }}
              className={cn(
                'rounded-full border px-2.5 py-1 text-xs font-medium transition-colors',
                status === o.value
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border bg-background text-muted-foreground hover:bg-accent/70 hover:text-foreground',
              )}
            >
              {o.label}
            </button>
          ))}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative w-full max-w-sm">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="ค้นหา order SN / ใบขาย SML / RC doc_no..."
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                resetPage()
              }}
              className="h-9 pl-8"
            />
          </div>
          <label className="flex w-full items-center gap-1.5 rounded-md border border-border bg-background px-2 text-xs text-muted-foreground sm:w-auto">
            <Store className="h-3.5 w-3.5 text-primary" />
            <select
              value={shopID}
              onChange={(e) => {
                setShopID(e.target.value)
                resetPage()
              }}
              className="h-9 min-w-0 bg-transparent text-xs text-foreground outline-none"
              aria-label="กรองตามร้าน Shopee"
            >
              <option value={ALL}>ทุกร้าน Shopee</option>
              {visibleConnections.map((shop) => (
                <option key={shop.id} value={String(shop.shop_id)}>
                  {shop.label || shop.shop_name || 'Shopee shop'} · {shop.shop_id}
                </option>
              ))}
            </select>
          </label>
          <Input
            type="date"
            value={dateFrom}
            onChange={(e) => {
              setDateFrom(e.target.value)
              resetPage()
            }}
            className="h-9 w-full sm:w-auto"
          />
          <Input
            type="date"
            value={dateTo}
            onChange={(e) => {
              setDateTo(e.target.value)
              resetPage()
            }}
            className="h-9 w-full sm:w-auto"
          />
          <label className="flex w-full items-center gap-1.5 rounded-md border border-border bg-background px-2 text-xs text-muted-foreground sm:w-auto">
            แสดง
            <select
              value={hiddenMode}
              onChange={(e) => {
                setHiddenMode(e.target.value)
                resetPage()
              }}
              className="h-9 min-w-0 bg-transparent text-xs text-foreground outline-none"
              aria-label="กรองงานที่ซ่อน"
            >
              <option value={VISIBLE}>ปกติ</option>
              <option value="only">ที่ซ่อน</option>
              <option value="all">ทั้งหมด</option>
            </select>
          </label>
          <Button variant="outline" size="sm" className="gap-1.5 sm:ml-auto" onClick={() => { void loadBasics(); void loadRuns() }}>
            <RefreshCw className="h-3.5 w-3.5" />
            รีเฟรช
          </Button>
        </div>
      </div>

      <SettlementTable
        runs={runs}
        loading={loading}
        page={page}
        total={totalRuns}
        totalPages={totalPages}
        hiddenMode={hiddenMode}
        onPageChange={setPage}
        onOpen={openRun}
      />

      <PullDialog
        open={pullOpen}
        onOpenChange={setPullOpen}
        connections={visibleConnections}
        apiEnabled={shopeeAPIEnabled}
        connectionID={pullConnectionID}
        setConnectionID={setPullConnectionID}
        from={pullFrom}
        setFrom={setPullFrom}
        to={pullTo}
        setTo={setPullTo}
        loading={creatingPreview}
        onSubmit={startPreview}
      />

      <RunDetailDialog
        open={detailOpen}
        onOpenChange={setDetailOpen}
        run={selectedRun}
        defaults={defaults}
        settingsReady={settingsReady}
        readyTotals={readyTotals}
        allTotals={allTotals}
        readyDiff={readyTotals.diff}
        active={activeDetail}
        sending={sending}
        hiding={hidingRun}
        onRefresh={() => {
          if (selectedRun?.id) {
            reconcileSelectedRun(selectedRun.id)
              .then((run) => {
                if (run.ready_count === 0 && run.blocked_count > 0) {
                  toast.info('รีเฟรชแล้ว: รายการในรอบนี้ถูก block เพราะเคยรับชำระแล้วหรือส่งไม่ได้')
                } else {
                  toast.success('รีเฟรชผลรับชำระ Shopee แล้ว')
                }
              })
              .catch((e) => toast.error('รีเฟรชผลไม่สำเร็จ: ' + apiError(e)))
          }
        }}
        onSend={openSendConfirm}
        onHide={hideSelectedRun}
        onRestore={restoreSelectedRun}
      />

      <ConfirmSendDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        run={selectedRun}
        defaults={defaults}
        totalInvoice={readyTotals.invoice}
        totalPayout={readyTotals.payout}
        totalDiff={readyTotals.diff}
        docDate={sendDocDate}
        docTime={sendDocTime}
        remark={sendRemark}
        setRemark={setSendRemark}
        sending={sending}
        onConfirm={sendReceipt}
      />
    </div>
  )
}

function PullDialog(props: {
  open: boolean
  onOpenChange: (v: boolean) => void
  connections: ShopeeConnection[]
  apiEnabled: boolean
  connectionID: string
  setConnectionID: (v: string) => void
  from: string
  setFrom: (v: string) => void
  to: string
  setTo: (v: string) => void
  loading: boolean
  onSubmit: () => void
}) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>ดึงรอบถอนเงินจาก Shopee</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <Alert className="border-info/30 bg-info/10">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>เลือกตามวันที่ Shopee ปล่อยเงินเข้าร้าน</AlertTitle>
            <AlertDescription>
              ไม่ใช่วันที่สั่งซื้อหรือวันที่ออกบิล ระบบดึงครั้งละไม่เกิน 15 วันเพื่อให้เสถียรและลดโอกาส Shopee API timeout
            </AlertDescription>
          </Alert>
          <Field label="ร้าน Shopee">
            <select
              className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
              value={props.connectionID}
              onChange={(e) => props.setConnectionID(e.target.value)}
              disabled={!props.apiEnabled || props.connections.length === 0}
            >
              {!props.apiEnabled ? (
                <option value="">Shopee API ปิดใช้งาน</option>
              ) : props.connections.length === 0 ? (
                <option value="">ยังไม่มีร้านที่เชื่อมต่อ</option>
              ) : (
                props.connections.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.label || c.shop_name || c.shop_id}
                  </option>
                ))
              )}
            </select>
          </Field>
          <Field label="ช่วงวันที่ Shopee release เงิน">
            <DateRangePicker
              from={props.from}
              to={props.to}
              onFromChange={props.setFrom}
              onToChange={props.setTo}
              presets={settlementReleasePresets}
              title="วันที่ Shopee ปล่อยเงิน"
              description="เลือกช่วง release เงินที่ต้องการดึง ระบบจำกัดครั้งละไม่เกิน 15 วัน"
              className="w-full"
            />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)} disabled={props.loading}>
            ยกเลิก
          </Button>
          <Button className="gap-2" onClick={props.onSubmit} disabled={props.loading || !props.connectionID || !props.apiEnabled}>
            {props.loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            ดึงรายการ
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SettlementTable({ runs, loading, page, total, totalPages, hiddenMode, onPageChange, onOpen }: {
  runs: SettlementRun[]
  loading: boolean
  page: number
  total: number
  totalPages: number
  hiddenMode: string
  onPageChange: (page: number) => void
  onOpen: (run: SettlementRun) => void
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[980px] text-sm">
          <thead className="bg-muted/50 text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2 text-left">รอบ release</th>
              <th className="px-3 py-2 text-left">ร้าน</th>
              <th className="px-3 py-2 text-right">ทั้งหมด</th>
              <th className="px-3 py-2 text-right">พร้อมส่ง</th>
              <th className="px-3 py-2 text-right">ยอดทั้งหมด</th>
              <th className="px-3 py-2 text-right">Payout</th>
              <th className="px-3 py-2 text-left">RC doc_no</th>
              <th className="px-3 py-2 text-left">สถานะ</th>
              <th className="px-3 py-2 text-right"></th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr>
                <td colSpan={9} className="px-3 py-8 text-center text-muted-foreground">
                  กำลังโหลด...
                </td>
              </tr>
            )}
            {!loading && runs.length === 0 && (
              <tr>
                <td colSpan={9} className="px-3 py-8 text-center text-muted-foreground">
                  {hiddenMode === 'only' ? 'ยังไม่มีงานรับชำระที่ซ่อนในเงื่อนไขนี้' : 'ยังไม่มีงานรับชำระในเงื่อนไขนี้'}
                </td>
              </tr>
            )}
            {!loading && runs.map((run) => (
              <tr key={run.id} className="border-t border-border hover:bg-muted/30">
                <td className="px-3 py-2">
                  <div className="font-medium">{dateOnly(run.release_date_from || run.release_time_from)} - {dateOnly(run.release_date_to || run.release_time_to)}</div>
                  <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                    <span>สร้าง {dayjs(run.created_at).format('DD/MM/YY HH:mm')}</span>
                    {run.hidden_at && (
                      <Badge variant="secondary" className="h-5 bg-muted text-[10px] text-muted-foreground">
                        ซ่อนแล้ว
                      </Badge>
                    )}
                  </div>
                </td>
                <td className="px-3 py-2">{run.shop_label}</td>
                <td className="px-3 py-2 text-right tabular-nums">{run.total_count.toLocaleString()}</td>
                <td className="px-3 py-2 text-right tabular-nums">{run.ready_count.toLocaleString()}</td>
                <td className="px-3 py-2 text-right tabular-nums">{money(run.invoice_amount_total)}</td>
                <td className="px-3 py-2 text-right tabular-nums">{money(run.payout_amount_total)}</td>
                <td className="px-3 py-2">
                  <code className="font-mono text-xs">{run.rc_doc_no || '-'}</code>
                </td>
                <td className="px-3 py-2"><RunStatusBadge run={run} /></td>
                <td className="px-3 py-2 text-right">
                  <Button variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => onOpen(run)}>
                    <Eye className="h-3.5 w-3.5" />
                    ดูรายละเอียด
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex flex-col gap-2 border-t border-border bg-muted/20 px-3 py-2 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
        <span>
          หน้า {page.toLocaleString('th-TH')} / {totalPages.toLocaleString('th-TH')} · ทั้งหมด {total.toLocaleString('th-TH')} รอบ
        </span>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={page <= 1 || loading}
            onClick={() => onPageChange(page - 1)}
          >
            ก่อนหน้า
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= totalPages || loading}
            onClick={() => onPageChange(page + 1)}
          >
            ถัดไป
          </Button>
        </div>
      </div>
    </div>
  )
}

function RunDetailDialog(props: {
  open: boolean
  onOpenChange: (v: boolean) => void
  run: SettlementRun | null
  defaults: SettlementDefaults | null
  settingsReady: boolean
  readyTotals: { invoice: number; payout: number; diff: number }
  allTotals: { invoice: number; payout: number; diff: number }
  readyDiff: number
  active: boolean
  sending: boolean
  hiding: boolean
  onRefresh: () => void
  onSend: () => void
  onHide: () => void
  onRestore: () => void
}) {
  const { run } = props
  const [showSkippedItems, setShowSkippedItems] = useState(false)
  useEffect(() => {
    setShowSkippedItems(false)
  }, [run?.id])
  if (!run) return null
  const items = settlementItems(run)
  const mainItems = primarySettlementItems(run, items)
  const skippedItems = skippedSettlementItems(items)
  const hasExpenseProblem = props.readyDiff > 0 && !props.defaults?.expense_code
  const disabledReason = props.sending
    ? 'กำลังส่งคำขอ กรุณารอสักครู่'
    : !props.settingsReady
    ? 'ยังตั้งค่า doc format และบัญชีรับเงินไม่ครบ'
    : props.active
      ? 'งานกำลังทำงานอยู่ กรุณารอให้เสร็จก่อน'
      : run.status === 'sent'
        ? 'รอบนี้ส่งรับชำระเข้า SML แล้ว'
        : run.ready_count <= 0
          ? 'ไม่มีรายการพร้อมส่ง รายการอาจถูกส่งรับชำระแล้วหรือถูก block'
          : hasExpenseProblem
            ? 'ต้องเลือกค่าใช้จ่ายส่วนต่าง Shopee ก่อนส่ง'
            : ''
  const canSend = !disabledReason
  const canHide = canHideSettlementRun(run)
  const canRestore = Boolean(run.hidden_at)
  const footerMessage = disabledReason || 'พร้อมสร้าง RC (ใบรับชำระใน SML) จากรายการพร้อมส่ง'
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="grid max-h-[90vh] max-w-5xl grid-rows-[auto_minmax(0,1fr)_auto]">
        <DialogHeader>
          <DialogTitle>รายละเอียดรับชำระ Shopee</DialogTitle>
        </DialogHeader>
        <div className="-mx-6 space-y-4 overflow-y-auto px-6 py-2">
          {props.active && (
            <div className="rounded-lg border border-info/30 bg-info/10 p-4">
              <div className="flex items-center gap-3">
                <Loader2 className="h-6 w-6 animate-spin text-info" />
                <div>
                  <div className="font-medium text-foreground">
                    {run.status === 'sending' ? 'กำลังส่งรับชำระเข้า SML' : 'กำลังดึงรายการจาก Shopee'}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    งานยังทำต่อ สามารถปิด dialog แล้วกลับมาเปิดดูผลได้
                  </div>
                </div>
              </div>
            </div>
          )}
          {run.error_msg && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>งานไม่สำเร็จ</AlertTitle>
              <AlertDescription>{run.error_msg}</AlertDescription>
            </Alert>
          )}
          {!props.settingsReady && (
            <Alert className="border-warning/40 bg-warning/10 text-warning">
              <Settings className="h-4 w-4" />
              <AlertTitle>ยังตั้งค่ารับชำระ Shopee ไม่ครบ</AlertTitle>
              <AlertDescription>ต้องตั้งค่า doc format และบัญชีรับเงินใน /settings/channels ก่อนส่งเข้า SML</AlertDescription>
            </Alert>
          )}
          {hasExpenseProblem && (
            <Alert className="border-warning/40 bg-warning/10 text-warning">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ต้องเลือกค่าใช้จ่ายส่วนต่าง Shopee</AlertTitle>
              <AlertDescription>รายการพร้อมส่งมีส่วนต่าง {money(props.readyDiff)} กรุณาตั้งค่า expense ที่หน้าเส้นทางเอกสาร SML ก่อนส่ง</AlertDescription>
            </Alert>
          )}
          {run.ready_count > 0 && run.blocked_count > 0 && (
            <Alert className="border-info/30 bg-info/10">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ระบบจะส่งเฉพาะรายการพร้อมส่ง</AlertTitle>
              <AlertDescription>มี {run.blocked_count.toLocaleString()} รายการที่ถูกข้าม ระบบจะไม่รวมรายการเหล่านี้ใน RC และไม่ส่งซ้ำ</AlertDescription>
            </Alert>
          )}
          {run.ready_count === 0 && run.blocked_count > 0 && !props.active && run.status !== 'sent' && (
            <Alert className="border-warning/40 bg-warning/10 text-warning">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ไม่มีรายการพร้อมส่ง</AlertTitle>
              <AlertDescription>รายการในรอบนี้ถูก block ทั้งหมด เช่น เคยรับชำระแล้วหรือยังไม่พบใบขาย SML ถ้าตรวจแล้วไม่ต้องใช้ สามารถซ่อนงานนี้จากรายการปกติได้</AlertDescription>
            </Alert>
          )}
          {run.hidden_at && (
            <Alert className="border-muted bg-muted/40">
              <EyeOff className="h-4 w-4" />
              <AlertTitle>งานนี้ถูกซ่อนจากรายการปกติ</AlertTitle>
              <AlertDescription>
                ซ่อนเมื่อ {dayjs(run.hidden_at).format('DD/MM/YYYY HH:mm')}
                {run.hidden_reason ? ` · เหตุผล: ${run.hidden_reason}` : ''}
              </AlertDescription>
            </Alert>
          )}
          <div className="grid gap-3 sm:grid-cols-5">
            <SummaryBox label="ทั้งหมด" value={`${run.total_count}`} />
            <SummaryBox label="พร้อมส่ง" value={`${run.ready_count}`} tone="ok" />
            <SummaryBox label="ส่งแล้ว" value={`${run.sent_count}`} tone="ok" />
            <SummaryBox label="ส่งไม่ได้" value={`${run.blocked_count}`} tone="warn" />
            <SummaryBox label="RC doc_no" value={run.rc_doc_no || '-'} mono />
          </div>
          <div className="space-y-3 rounded-md border border-border bg-muted/25 p-3">
            <div className="rounded-md border border-border bg-background p-3">
              <div className="flex flex-wrap items-center gap-2">
                <div className="text-sm font-semibold text-foreground">รายการที่จะส่งเข้า SML รอบนี้</div>
                <Badge variant="secondary" className="bg-primary/10 text-primary">RC = ใบรับชำระใน SML</Badge>
              </div>
              {run.ready_count > 0 ? (
                <div className="mt-3 grid gap-2 text-sm md:grid-cols-3">
                  <div>ยอดตามใบขายใน SML: <b>{money(props.readyTotals.invoice)}</b></div>
                  <div>ยอดเงินจริงที่ Shopee ปล่อย: <b>{money(props.readyTotals.payout)}</b></div>
                  <div>ค่าธรรมเนียม/ส่วนต่าง Shopee: <b>{money(props.readyTotals.diff)}</b></div>
                </div>
              ) : (
                <div className="mt-3 rounded-md border border-dashed border-warning/50 bg-warning/10 p-3 text-sm text-warning">
                  ยังไม่มีรายการที่จะสร้างใบรับชำระ เพราะรายการในรอบนี้ถูกข้าม/เคยรับชำระแล้ว
                </div>
              )}
            </div>
            <div className="rounded-md border border-border/70 bg-background/60 p-3">
              <div className="mb-1 text-xs font-medium text-muted-foreground">ภาพรวมทั้งหมดที่ดึงมา</div>
              <div className="grid gap-2 text-sm md:grid-cols-3">
                <div>ยอดตามใบขายใน SML: <b>{money(props.allTotals.invoice)}</b></div>
                <div>ยอดเงินจริงที่ Shopee ปล่อย: <b>{money(props.allTotals.payout)}</b></div>
                <div>ค่าธรรมเนียม/ส่วนต่าง Shopee: <b>{money(props.allTotals.diff)}</b></div>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              ถ้า Shopee โอนน้อยกว่ายอดใบขาย ระบบจะลงส่วนต่างเป็นค่าใช้จ่ายที่เลือกไว้
            </p>
          </div>
          <div className="overflow-x-auto rounded-md border border-border">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-muted/25 px-3 py-2">
              <div>
                <div className="text-sm font-medium text-foreground">
                  {run.status === 'sent' ? 'รายการที่ส่งเข้า SML แล้ว' : 'รายการพร้อมส่งเข้า SML'}
                </div>
                <div className="text-xs text-muted-foreground">
                  {run.status === 'sent'
                    ? 'รายการเหล่านี้ถูกนำไปสร้าง RC แล้ว'
                    : 'ตารางนี้แสดงเฉพาะรายการที่จะถูกนำไปสร้าง RC'}
                </div>
              </div>
              <Badge variant="secondary" className="bg-success/15 text-success">
                {mainItems.length.toLocaleString()} รายการ
              </Badge>
            </div>
            <table className="w-full min-w-[900px] text-sm">
              <thead className="bg-muted/50 text-xs text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 text-left">คำสั่งซื้อ</th>
                  <th className="px-3 py-2 text-left">ใบขาย SML</th>
                  <th className="px-3 py-2 text-right">ยอดตามใบขายใน SML</th>
                  <th className="px-3 py-2 text-right">ยอดเงินจริงที่ Shopee ปล่อย</th>
                  <th className="px-3 py-2 text-right">ค่าธรรมเนียม/ส่วนต่าง</th>
                  <th className="px-3 py-2 text-left">สถานะ</th>
                </tr>
              </thead>
              <tbody>
                {mainItems.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-3 py-8 text-center text-muted-foreground">
                      {items.length === 0
                        ? 'กำลังรอรายการจาก Shopee หรือยังไม่มีรายการในรอบนี้'
                        : skippedItems.length > 0
                          ? 'ไม่มีรายการพร้อมส่งในรอบนี้ รายการที่ส่งไม่ได้อยู่ในส่วนรายการที่ถูกข้ามด้านล่าง'
                          : 'กำลังตรวจรายการจาก Shopee/SML กรุณารอสักครู่'}
                    </td>
                  </tr>
                )}
                {mainItems.map((item) => (
                  <tr key={item.id} className="border-t border-border">
                    <td className="px-3 py-2 font-mono text-xs">{item.order_sn}</td>
                    <td className="px-3 py-2">
                      <div className="font-medium">{item.invoice_doc_no || '-'}</div>
                      <div className="text-xs text-muted-foreground">{item.cust_code || ''}</div>
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{money(item.invoice_amount)}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{money(item.payout_amount)}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{money(item.difference_amount)}</td>
                    <td className="px-3 py-2">
                      <div className="flex flex-col gap-1">
                        <StatusBadge status={item.status} alreadyReceived={Boolean(item.existing_receipt_doc_no)} />
                        {(item.block_reason || item.existing_receipt_doc_no) && (
                          <span className="max-w-[320px] text-xs text-muted-foreground">
                            {settlementBlockReason(item)}
                          </span>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {skippedItems.length > 0 && (
            <div className="rounded-md border border-warning/30 bg-warning/5">
              <button
                type="button"
                className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left"
                onClick={() => setShowSkippedItems((v) => !v)}
              >
                <div>
                  <div className="text-sm font-medium text-warning">
                    มี {skippedItems.length.toLocaleString()} รายการถูกข้าม
                  </div>
                  <div className="text-xs text-muted-foreground">
                    รายการเหล่านี้ไม่ถูกนำไปสร้าง RC รอบนี้ แต่ยังเก็บไว้เพื่อ audit และกันส่งซ้ำ
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2 text-sm text-warning">
                  {showSkippedItems ? 'ซ่อนรายการที่ถูกข้าม' : 'ดูรายการที่ถูกข้าม'}
                  {showSkippedItems ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                </div>
              </button>
              {showSkippedItems && (
                <div className="overflow-x-auto border-t border-warning/20 bg-background">
                  <table className="w-full min-w-[900px] text-sm">
                    <thead className="bg-muted/40 text-xs text-muted-foreground">
                      <tr>
                        <th className="px-3 py-2 text-left">คำสั่งซื้อ</th>
                        <th className="px-3 py-2 text-left">ใบขาย SML</th>
                        <th className="px-3 py-2 text-right">ยอดตามใบขายใน SML</th>
                        <th className="px-3 py-2 text-right">ยอดเงินจริงที่ Shopee ปล่อย</th>
                        <th className="px-3 py-2 text-right">ค่าธรรมเนียม/ส่วนต่าง</th>
                        <th className="px-3 py-2 text-left">เหตุผลที่ถูกข้าม</th>
                      </tr>
                    </thead>
                    <tbody>
                      {skippedItems.map((item) => (
                        <tr key={item.id} className="border-t border-border">
                          <td className="px-3 py-2 font-mono text-xs">{item.order_sn}</td>
                          <td className="px-3 py-2">
                            <div className="font-medium">{item.invoice_doc_no || '-'}</div>
                            <div className="text-xs text-muted-foreground">{item.cust_code || ''}</div>
                          </td>
                          <td className="px-3 py-2 text-right tabular-nums">{money(item.invoice_amount)}</td>
                          <td className="px-3 py-2 text-right tabular-nums">{money(item.payout_amount)}</td>
                          <td className="px-3 py-2 text-right tabular-nums">{money(item.difference_amount)}</td>
                          <td className="px-3 py-2">
                            <div className="flex flex-col gap-1">
                              <StatusBadge status={item.status} alreadyReceived={Boolean(item.existing_receipt_doc_no)} />
                              <span className="max-w-[360px] text-xs text-muted-foreground">
                                {settlementBlockReason(item) || 'รายการนี้ถูกข้ามจากการสร้าง RC รอบนี้'}
                              </span>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>
        <div className="-mx-6 -mb-6 flex flex-col gap-3 border-t border-border bg-background px-6 py-4 sm:flex-row sm:items-center sm:justify-between">
          <div className={cn('text-sm', canSend ? 'text-muted-foreground' : 'text-warning')}>
            {footerMessage}
          </div>
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:items-center">
            <Button variant="outline" onClick={() => props.onOpenChange(false)} disabled={props.sending}>
              ปิด
            </Button>
            <Button variant="outline" onClick={props.onRefresh} disabled={props.sending || props.active}>
              รีเฟรชผล
            </Button>
            {canRestore && (
              <Button variant="outline" className="gap-2" onClick={props.onRestore} disabled={props.hiding || props.sending || props.active}>
                {props.hiding ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
                กู้คืน
              </Button>
            )}
            {canHide && (
              <Button variant="outline" className="gap-2" onClick={props.onHide} disabled={props.hiding || props.sending || props.active}>
                {props.hiding ? <Loader2 className="h-4 w-4 animate-spin" /> : <EyeOff className="h-4 w-4" />}
                ซ่อนจากรายการ
              </Button>
            )}
            <Button className="gap-2" onClick={props.onSend} disabled={!canSend || props.sending}>
              {props.sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
              {canSend ? `ส่งเฉพาะรายการพร้อมส่ง ${run.ready_count.toLocaleString()} รายการ` : 'ส่งรับชำระเข้า SML'}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ConfirmSendDialog(props: {
  open: boolean
  onOpenChange: (v: boolean) => void
  run: SettlementRun | null
  defaults: SettlementDefaults | null
  totalInvoice: number
  totalPayout: number
  totalDiff: number
  docDate: string
  docTime: string
  remark: string
  setRemark: (v: string) => void
  sending: boolean
  onConfirm: () => void
}) {
  if (!props.run || !props.defaults) return null
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>ยืนยันส่งรับชำระเข้า SML</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 text-sm">
          <div className="grid gap-2 rounded-md border border-border bg-muted/25 p-3">
            <SummaryLine label="จำนวน order" value={`${props.run.ready_count.toLocaleString()} รายการ`} />
            <SummaryLine label="ยอดตามใบขายใน SML" value={money(props.totalInvoice)} />
            <SummaryLine label="ยอดเงินจริงที่ Shopee ปล่อย" value={money(props.totalPayout)} />
            <SummaryLine label="ค่าธรรมเนียม/ส่วนต่าง Shopee" value={money(props.totalDiff)} />
            <SummaryLine label="วันที่เอกสาร (doc_date)" value={dateOnly(props.docDate)} />
            <SummaryLine label="เวลาเอกสาร (doc_time)" value={props.docTime || '-'} mono />
            <SummaryLine label="รูปแบบเอกสาร RC (ใบรับชำระ)" value={props.defaults.doc_format_code || '-'} mono />
            <SummaryLine label="บัญชีรับเงิน" value={`${props.defaults.passbook_code} · ${props.defaults.passbook_name || '-'}`} />
            <SummaryLine label="ค่าใช้จ่ายส่วนต่าง" value={props.defaults.expense_code ? `${props.defaults.expense_code} · ${props.defaults.expense_name || '-'}` : 'ไม่ใช้ในรอบนี้'} />
          </div>
          <p className="text-xs text-muted-foreground">
            ระบบจะสร้างเอกสาร RC 1 ใบสำหรับรายการพร้อมส่งในรอบนี้ และจะไม่รวมรายการที่เคยรับชำระแล้วหรือถูก block
          </p>
          <Field label="หมายเหตุ (remark)">
            <Textarea
              value={props.remark}
              onChange={(e) => props.setRemark(e.target.value)}
              rows={3}
              placeholder={DEFAULT_SETTLEMENT_REMARK}
              disabled={props.sending}
            />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)} disabled={props.sending}>
            ยกเลิก
          </Button>
          <Button className="gap-2" onClick={props.onConfirm} disabled={props.sending}>
            {props.sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
            ยืนยันส่ง SML
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="space-y-1.5"><Label>{label}</Label>{children}</div>
}

function QueueMetric({ label, value, icon: Icon, tone }: {
  label: string
  value: number
  icon: LucideIcon
  tone: 'primary' | 'success' | 'warning' | 'danger'
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-3">
        <div className={cn(
          'rounded-md p-2',
          tone === 'success'
            ? 'bg-success/10 text-success'
            : tone === 'warning'
              ? 'bg-warning/15 text-warning'
              : tone === 'danger'
                ? 'bg-destructive/10 text-destructive'
                : 'bg-primary/10 text-primary',
        )}>
          <Icon className="h-4 w-4" />
        </div>
        <div>
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="text-lg font-semibold tabular-nums">{value.toLocaleString()}</div>
        </div>
      </CardContent>
    </Card>
  )
}

function SummaryBox({ label, value, tone, mono }: { label: string; value: string; tone?: 'ok' | 'warn'; mono?: boolean }) {
  return (
    <div className="rounded-md border border-border bg-background p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn('mt-1 text-lg font-semibold', mono && 'font-mono text-sm', tone === 'ok' && 'text-success', tone === 'warn' && 'text-warning')}>
        {value}
      </div>
    </div>
  )
}

function SummaryLine({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn('text-right font-medium', mono && 'font-mono')}>{value}</span>
    </div>
  )
}

function RunStatusBadge({ run }: { run: SettlementRun }) {
  if (run.status === 'failed') return <StatusBadge status="failed" />
  if (run.status === 'sent') return <StatusBadge status="sent" />
  if (run.status === 'sending') return <StatusBadge status="sending" />
  if (run.status === 'running' || run.status === 'pending') return <StatusBadge status={run.status} />
  if (run.ready_count > 0) return <StatusBadge status="ready" />
  if (run.blocked_count > 0) {
    return <Badge variant="secondary" className="w-fit bg-warning/15 text-warning">ไม่มีรายการพร้อมส่ง</Badge>
  }
  if (run.status === 'partial') {
    return <Badge variant="secondary" className="w-fit bg-warning/15 text-warning">ต้องตรวจ</Badge>
  }
  return <StatusBadge status={run.status} />
}

function StatusBadge({ status, alreadyReceived }: { status: string; alreadyReceived?: boolean }) {
  if (alreadyReceived) {
    return <Badge variant="secondary" className="w-fit bg-warning/15 text-warning">เคยรับชำระแล้ว</Badge>
  }
  const map: Record<string, { label: string; cls: string }> = {
    running: { label: 'กำลังดึง', cls: 'bg-info/15 text-info' },
    ready: { label: 'พร้อมส่ง', cls: 'bg-success/15 text-success' },
    sending: { label: 'กำลังส่ง', cls: 'bg-info/15 text-info' },
    sent: { label: 'ส่งแล้ว', cls: 'bg-success/15 text-success' },
    blocked: { label: 'ส่งไม่ได้', cls: 'bg-warning/15 text-warning' },
    failed: { label: 'ไม่สำเร็จ', cls: 'bg-destructive/15 text-destructive' },
    partial: { label: 'ต้องตรวจ', cls: 'bg-warning/15 text-warning' },
    pending: { label: 'รอทำงาน', cls: 'bg-muted text-muted-foreground' },
  }
  const item = map[status] ?? { label: status, cls: 'bg-muted text-muted-foreground' }
  return (
    <Badge variant="secondary" className={cn('w-fit', item.cls)}>
      {status === 'sent' && <CheckCircle2 className="mr-1 h-3 w-3" />}
      {item.label}
    </Badge>
  )
}
