import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  FileSpreadsheet,
  ReceiptText,
  RefreshCw,
  Send,
  Settings2,
  Store,
  WalletCards,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DateRangePicker, type DateRangePreset } from '@/components/common/DateRangePicker'
import { resolveTikTokSettlementShopID } from '@/lib/tiktok-settlement'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/store/auth'

type Shop = {
  shop_id: string
  shop_name: string
  shop_code?: string
  disabled?: boolean
}

type Item = {
  id: string
  order_id: string
  sml_invoice_doc_no?: string
  customer_code?: string
  invoice_amount: number
  settlement_amount: number
  fee_amount: number
  shipping_amount: number
  adjustment_amount: number
  refund_amount: number
  currency: string
  status: string
  block_reason?: string
  receipt_doc_no?: string
}

type Run = {
  id: string
  shop_id: string
  shop_label: string
  statement_id: string
  payment_id: string
  payment_status: string
  currency: string
  payment_time?: string
  total_settlement_amount: number
  invoice_amount_total: number
  fee_amount_total: number
  status: string
  config_version: number
  item_count?: number
  rc_doc_no?: string
  error_msg?: string
  anomaly_reason?: string
  items?: Item[]
}

type Counts = {
  processing?: number
  ready?: number
  needs_review?: number
  sent?: number
  failed?: number
  total: number
}

type RouteSummary = {
  configured: boolean
  doc_format_code?: string
  passbook_code?: string
  passbook_name?: string
  bank_code?: string
  bank_branch?: string
  expense_code?: string
  expense_name?: string
}

type ImportNotice = {
  message: string
  partial: boolean
}

type WithdrawalRound = {
  withdrawal_id: string
  type: string
  amount: string
  currency: string
  status: string
  create_time: string
}

type IncomeWithdrawal = {
  withdrawal_id: string
  type: string
  requested_at: string
  amount_cents: number
  status: string
  completed_at: string
  currency: string
}

type IncomePreview = {
  preview_token: string
  currency: string
  order_count: number
  order_payment_total: number
  report_payment_total: number
  withdrawals: IncomeWithdrawal[]
  message: string
}

const money = (value?: number, currency = 'THB') => new Intl.NumberFormat('th-TH', {
  style: 'currency',
  currency: /^[A-Z]{3}$/.test(currency) ? currency : 'THB',
}).format(Number(value ?? 0))

const isIncomeExportRun = (run?: Pick<Run, 'statement_id'> | null) => Boolean(run?.statement_id?.startsWith('income-export:'))
const evidenceTitle = (run: Pick<Run, 'statement_id' | 'payment_id'>) => (
  isIncomeExportRun(run)
    ? `ไฟล์รายได้ · รอบถอน ${run.payment_id || run.statement_id.replace('income-export:', '')}`
    : run.statement_id
)

const statusMeta: Record<string, { text: string; className: string }> = {
  importing: { text: 'กำลังดึงข้อมูล', className: 'bg-muted text-muted-foreground' },
  reconciling: { text: 'กำลังตรวจยอด', className: 'bg-info/15 text-info' },
  ready: { text: 'พร้อมส่ง', className: 'bg-success/15 text-success' },
  needs_review: { text: 'ต้องตรวจ', className: 'bg-warning/15 text-warning' },
  sending: { text: 'กำลังส่ง SML', className: 'bg-info/15 text-info' },
  sent: { text: 'ส่งแล้ว', className: 'bg-success/15 text-success' },
  failed: { text: 'ผิดพลาด', className: 'bg-destructive/15 text-destructive' },
  unknown_result: { text: 'ต้องตรวจผล SML', className: 'bg-warning/15 text-warning' },
  superseded: { text: 'ข้อมูลใหม่กว่า', className: 'bg-muted text-muted-foreground' },
}

const tiktokStatementPresets: DateRangePreset[] = [
  {
    label: 'วันนี้',
    getRange: () => {
      const today = dayjs().format('YYYY-MM-DD')
      return { from: today, to: today }
    },
  },
  {
    label: '7 วัน',
    getRange: () => ({
      from: dayjs().subtract(6, 'day').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
  },
  {
    label: '15 วัน',
    getRange: () => ({
      from: dayjs().subtract(14, 'day').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
  },
]

export default function TikTokSettlement() {
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const [shops, setShops] = useState<Shop[]>([])
  const [shopID, setShopID] = useState('')
  const [runs, setRuns] = useState<Run[]>([])
  const [counts, setCounts] = useState<Counts>({ total: 0 })
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<Run | null>(null)
  const [route, setRoute] = useState<RouteSummary | null>(null)
  const [sendConfirmOpen, setSendConfirmOpen] = useState(false)
  const [sending, setSending] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsConfirmOpen, setSettingsConfirmOpen] = useState(false)
  const [readEnabled, setReadEnabled] = useState(false)
  const [smlEnabled, setSmlEnabled] = useState(false)
  const [settingsVersion, setSettingsVersion] = useState(0)
  const [importNotice, setImportNotice] = useState<ImportNotice | null>(null)
  const [withdrawalOpen, setWithdrawalOpen] = useState(false)
  const [withdrawals, setWithdrawals] = useState<WithdrawalRound[]>([])
  const [withdrawalsTruncated, setWithdrawalsTruncated] = useState(false)
  const [withdrawalLoading, setWithdrawalLoading] = useState(false)
  const incomeFileRef = useRef<HTMLInputElement>(null)
  const [incomeOpen, setIncomeOpen] = useState(false)
  const [incomePreview, setIncomePreview] = useState<IncomePreview | null>(null)
  const [incomeLoading, setIncomeLoading] = useState(false)
  const [selectedIncomeWithdrawalID, setSelectedIncomeWithdrawalID] = useState('')
  const [bankReceivedAt, setBankReceivedAt] = useState('')
  const [bankReceiptConfirmed, setBankReceiptConfirmed] = useState(false)
  const [creatingIncomeCandidate, setCreatingIncomeCandidate] = useState(false)
  const [from, setFrom] = useState(dayjs().subtract(14, 'day').format('YYYY-MM-DD'))
  const [to, setTo] = useState(dayjs().format('YYYY-MM-DD'))
  const [runStatus, setRunStatus] = useState('all')

  const resolvedShopID = resolveTikTokSettlementShopID(shopID, shops)

  const load = async () => {
    setLoading(true)
    try {
      const params = {
        ...(shopID ? { shop_id: shopID } : {}),
        date_from: from,
        date_to: to,
        ...(runStatus !== 'all' ? { status: runStatus } : {}),
      }
      const [connections, list, summary] = await Promise.all([
        client.get<{ data: Shop[] }>('/api/tiktok-shop-api/local-connections'),
        client.get<{ data: Run[] }>('/api/tiktok-settlements', { params }),
        client.get<Counts>('/api/tiktok-settlements/counts'),
      ])
      const active = (connections.data.data ?? []).filter((shop) => !shop.disabled)
      setShops(active)
      if (!shopID && active.length === 1) setShopID(active[0].shop_id)
      setRuns(list.data.data ?? [])
      setCounts(summary.data)
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'โหลดรายการรับชำระ TikTok Shop ไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [shopID, from, to, runStatus])

  const preflight = async () => {
    if (!resolvedShopID) {
      toast.error('กรุณาเลือกร้านก่อนตรวจระบบ')
      return
    }
    try {
      const response = await client.post<{ data: { message: string } }>(
        '/api/tiktok-settlements/preflight',
        null,
        { params: { shop_id: resolvedShopID } },
      )
      toast.success(response.data.data.message)
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'ตรวจระบบไม่สำเร็จ')
    }
  }

  const inspectWithdrawals = async () => {
    if (!resolvedShopID) {
      toast.error('กรุณาเลือกร้านก่อนดูรอบถอนเงิน')
      return
    }
    setWithdrawalLoading(true)
    setWithdrawalsTruncated(false)
    setWithdrawalOpen(true)
    try {
      const response = await client.post<{ data: WithdrawalRound[]; has_more?: boolean; message?: string }>(
        '/api/tiktok-settlements/withdrawals/search',
        { shop_id: resolvedShopID, date_from: from, date_to: to },
      )
      setWithdrawals(response.data.data ?? [])
      setWithdrawalsTruncated(Boolean(response.data.has_more))
    } catch (error: any) {
      setWithdrawalOpen(false)
      toast.error(error?.response?.data?.error?.message ?? 'ดึงรอบถอนเงิน TikTok Shop ไม่สำเร็จ')
    } finally {
      setWithdrawalLoading(false)
    }
  }

  const previewIncomeExport = async (file?: File) => {
    if (!file) return
    if (!resolvedShopID) {
      toast.error('กรุณาเลือกร้านก่อนนำเข้าไฟล์รายได้')
      return
    }
    setIncomeLoading(true)
    try {
      const form = new FormData()
      form.append('shop_id', resolvedShopID)
      form.append('file', file)
      const response = await client.post<{ data: IncomePreview }>('/api/tiktok-settlements/income-exports/preview', form)
      setIncomePreview(response.data.data)
      setSelectedIncomeWithdrawalID('')
      setBankReceivedAt('')
      setBankReceiptConfirmed(false)
      setIncomeOpen(true)
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'อ่านไฟล์รายได้ TikTok Shop ไม่สำเร็จ')
    } finally {
      setIncomeLoading(false)
      if (incomeFileRef.current) incomeFileRef.current.value = ''
    }
  }

  const createIncomeCandidate = async () => {
    if (!incomePreview || !selectedIncomeWithdrawalID || !bankReceivedAt || !bankReceiptConfirmed) {
      toast.error('กรุณาเลือกรอบถอน ระบุเวลาเงินเข้า และยืนยันข้อมูลก่อนสร้างร่าง RC')
      return
    }
    setCreatingIncomeCandidate(true)
    try {
      const response = await client.post<{ data: Run }>('/api/tiktok-settlements/income-exports/receipt-candidates', {
        preview_token: incomePreview.preview_token,
        withdrawal_id: selectedIncomeWithdrawalID,
        bank_received_at: dayjs(bankReceivedAt).toISOString(),
        confirmation: 'CONFIRM_TIKTOK_BANK_RECEIPT',
      })
      setIncomeOpen(false)
      setIncomePreview(null)
      await load()
      await open(response.data.data)
      toast.success('สร้างร่าง RC แล้ว กรุณาตรวจรายการก่อนส่งเข้า SML')
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'สร้างร่าง RC จากไฟล์รายได้ไม่สำเร็จ')
    } finally {
      setCreatingIncomeCandidate(false)
    }
  }

  const selectedIncomeWithdrawal = incomePreview?.withdrawals.find((withdrawal) => withdrawal.withdrawal_id === selectedIncomeWithdrawalID)
  const incomeTotalsMatch = Boolean(
    incomePreview
      && selectedIncomeWithdrawal
      && incomePreview.order_payment_total === incomePreview.report_payment_total
      && selectedIncomeWithdrawal.amount_cents === Math.round(incomePreview.order_payment_total * 100),
  )

  const openSettings = async () => {
    if (!resolvedShopID) {
      toast.error('กรุณาเลือกร้านก่อนตั้งค่า')
      return
    }
    try {
      const response = await client.get<{
        data: { read_enabled: boolean; sml_send_enabled: boolean; config_version: number }
      }>('/api/tiktok-settlements/settings', { params: { shop_id: resolvedShopID } })
      setReadEnabled(response.data.data.read_enabled)
      setSmlEnabled(response.data.data.sml_send_enabled)
      setSettingsVersion(response.data.data.config_version)
      setSettingsOpen(true)
    } catch {
      toast.error('โหลดการตั้งค่าร้านไม่สำเร็จ')
    }
  }

  const confirmSaveSettings = async () => {
    if (!resolvedShopID) {
      toast.error('กรุณาเลือกร้านก่อนบันทึกการตั้งค่า')
      return
    }
    try {
      await client.put(`/api/tiktok-settlements/settings/${resolvedShopID}`, {
        read_enabled: readEnabled,
        sml_send_enabled: smlEnabled,
        expected_config_version: settingsVersion,
      })
      toast.success('บันทึกการตั้งค่าแล้ว')
      setSettingsOpen(false)
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'บันทึกการตั้งค่าไม่สำเร็จ')
    }
  }

  const open = async (run: Run) => {
    try {
      const [detail, routeResult] = await Promise.all([
        client.get<{ data: Run }>(`/api/tiktok-settlements/${run.id}`),
        client.get<{ data: RouteSummary }>('/api/tiktok-settlements/route'),
      ])
      setSelected(detail.data.data)
      setRoute(routeResult.data.data)
    } catch {
      toast.error('โหลดรายละเอียด Statement ไม่สำเร็จ')
    }
  }

  const reconcile = async () => {
    if (!selected) return
    try {
      const response = await client.post<{ data: Run }>(`/api/tiktok-settlements/${selected.id}/reconcile`)
      setSelected(response.data.data)
      await load()
      toast.success('ตรวจเทียบ Statement ล่าสุดแล้ว')
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'ตรวจเทียบไม่สำเร็จ')
    }
  }

  const confirmSend = async () => {
    if (!selected) return
    setSending(true)
    try {
      await client.post(`/api/tiktok-settlements/${selected.id}/send`, {
        confirm: 'CONFIRM_TIKTOK_RC',
        expected_config_version: String(selected.config_version),
      })
      toast.success('เริ่มส่ง RC เข้า SML แล้ว')
      setSelected(null)
      await load()
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? 'ส่ง RC ไม่สำเร็จ')
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="space-y-5">
      <section className="rounded-lg border border-border/70 bg-card p-2.5 shadow-sm">
        <div className="flex flex-col gap-2 xl:flex-row xl:items-start xl:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-lg font-semibold tracking-tight text-foreground">รับชำระ TikTok Shop</h1>
              <code className="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-accent-strong">RC</code>
              <p className="sr-only">นำเข้าไฟล์รายได้ TikTok Shop แล้วเลือกรอบถอนเงินที่เงินเข้าบัญชีจริง ระบบจะไม่สร้างเอกสารรับชำระหนี้จากยอดที่คาดเดา</p>
              <span className="hidden text-xs text-muted-foreground sm:inline">·</span>
              <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
                <ReceiptText className="h-3.5 w-3.5 shrink-0 text-accent-strong" />
                <Link to="/sale-invoices" className="font-medium text-link hover:underline">ขายสินค้าและบริการ</Link>
                <span>→</span>
                <span className="font-medium text-foreground">ลูกหนี้ -&gt; รับชำระหนี้</span>
              </span>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-1.5 xl:justify-end">
            <SettlementMetricChip label="กำลังตรวจ" value={counts.processing ?? 0} tone="primary" />
            <SettlementMetricChip label="พร้อมส่ง" value={counts.ready ?? 0} tone="success" />
            <SettlementMetricChip label="ส่งแล้ว" value={counts.sent ?? 0} tone="success" />
            <SettlementMetricChip label="ต้องตรวจ" value={counts.needs_review ?? 0} tone="warning" />
            <SettlementMetricChip label="ผิดพลาด" value={counts.failed ?? 0} tone="danger" />
            <input
              ref={incomeFileRef}
              className="sr-only"
              type="file"
              accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              onChange={(event) => void previewIncomeExport(event.target.files?.[0])}
            />
            <Button className="h-8 w-full justify-center gap-1.5 sm:w-auto" size="sm" onClick={() => incomeFileRef.current?.click()} disabled={incomeLoading}>
              <FileSpreadsheet className="h-4 w-4" />
              {incomeLoading ? 'กำลังอ่านไฟล์…' : 'นำเข้าไฟล์รายได้'}
            </Button>
            <Button className="h-8 w-full justify-center gap-1.5 sm:w-auto" size="sm" variant="outline" onClick={inspectWithdrawals}>
              <WalletCards className="h-4 w-4" />
              ตรวจรอบถอนเงิน
            </Button>
            <Button className="h-8 w-full justify-center gap-1.5 sm:w-auto" size="sm" variant="outline" onClick={preflight}>
              <CheckCircle2 className="h-4 w-4" />
              ตรวจระบบ
            </Button>
            <Button asChild className="h-8 w-full justify-center sm:w-auto" size="sm" variant="outline">
              <Link to="/settings/channels">ตั้งค่าเส้นทาง</Link>
            </Button>
            {isAdmin && (
              <Button className="h-8 w-full justify-center gap-1.5 sm:w-auto" size="sm" variant="outline" onClick={openSettings}>
                <Settings2 className="h-4 w-4" />
                ตั้งค่าร้าน
              </Button>
            )}
          </div>
        </div>

        <div className="mt-2 space-y-2 border-t border-border/60 pt-2">
          <div className="grid gap-1.5 sm:grid-cols-2 lg:flex lg:flex-wrap lg:items-center">
            <Select value={resolvedShopID} onValueChange={setShopID}>
              <SelectTrigger className="h-8 w-full text-xs sm:w-[220px]" aria-label="กรองตามร้าน TikTok Shop">
                <Store className="mr-2 h-3.5 w-3.5 shrink-0 text-accent-strong" />
                <SelectValue placeholder="ร้าน TikTok Shop" />
              </SelectTrigger>
              <SelectContent>
                {shops.map((shop) => (
                  <SelectItem key={shop.shop_id} value={shop.shop_id}>
                    {shop.shop_name || shop.shop_id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <DateRangePicker
              from={from}
              to={to}
              onFromChange={setFrom}
              onToChange={setTo}
              onRangeChange={(range) => {
                setFrom(range.from)
                setTo(range.to)
              }}
              presets={tiktokStatementPresets}
              title="ช่วงวันที่รอบถอนเงิน"
              description="ใช้ตรวจรอบถอนและกรองร่าง RC ที่สร้างแล้ว"
              className="!h-8 w-full !min-w-0 text-xs sm:w-[260px]"
            />
            <Select value={runStatus} onValueChange={setRunStatus}>
              <SelectTrigger className="h-8 w-full text-xs sm:w-[150px]" aria-label="กรองตามสถานะงาน">
                <SelectValue placeholder="สถานะงาน" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">ทุกสถานะงาน</SelectItem>
                <SelectItem value="ready">พร้อมส่ง</SelectItem>
                <SelectItem value="needs_review">ต้องตรวจ</SelectItem>
                <SelectItem value="sent">ส่งแล้ว</SelectItem>
                <SelectItem value="failed">ผิดพลาด</SelectItem>
              </SelectContent>
            </Select>
            <Button className="h-8 w-full justify-center gap-1.5 lg:ml-auto lg:w-auto" size="sm" variant="outline" onClick={() => void load()}>
              <RefreshCw className="h-3.5 w-3.5" />
              รีเฟรช
            </Button>
          </div>
        </div>
      </section>

      {importNotice && (
        <div
          role="status"
          className={cn(
            'flex items-start justify-between gap-3 rounded-md border p-3 text-sm',
            importNotice.partial ? 'border-warning/30 bg-warning/5 text-warning' : 'border-success/30 bg-success/5 text-success',
          )}
        >
          <div className="flex gap-2">
            {importNotice.partial ? <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" /> : <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />}
            <p>{importNotice.message}</p>
          </div>
          <Button variant="ghost" size="sm" className="h-7 shrink-0 px-2" onClick={() => setImportNotice(null)}>ปิด</Button>
        </div>
      )}

      <Card className="border-border/70 shadow-none">
        <CardContent className="p-0">
          {loading && <div className="p-6 text-sm text-muted-foreground">กำลังโหลดร่างรับชำระ…</div>}
          <div className="hidden overflow-x-auto md:block">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/30 text-left text-muted-foreground">
                <tr>
                  <th className="p-3">หลักฐานรับเงิน / วันที่</th>
                  <th className="p-3">ร้าน</th>
                  <th className="p-3 text-right">ยอด TikTok / Order</th>
                  <th className="p-3">สถานะ</th>
                  <th className="p-3">RC</th>
                  <th className="p-3"><span className="sr-only">การดำเนินการ</span></th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <tr key={run.id} className="border-b last:border-0">
                    <td className="p-3">
                      <p className="font-medium">{evidenceTitle(run)}</p>
                      <p className="text-xs text-muted-foreground">{run.payment_time ? dayjs(run.payment_time).format('DD/MM/YY HH:mm') : '-'}</p>
                    </td>
                    <td className="p-3">
                      <Badge className="border-[#111817] bg-[#111817] text-white hover:bg-[#111817]">TikTok</Badge>
                      <p className="mt-1 text-xs text-muted-foreground">{run.shop_label}</p>
                    </td>
                    <td className="p-3 text-right">
                      <p className="font-medium">{money(run.total_settlement_amount, run.currency)}</p>
                      <p className="text-xs text-muted-foreground">{run.item_count ?? '-'} คำสั่งซื้อ</p>
                    </td>
                    <td className="p-3">
                      <Badge variant="secondary" className={statusMeta[run.status]?.className}>{statusMeta[run.status]?.text ?? run.status}</Badge>
                      <p className="mt-1 max-w-[200px] truncate text-xs text-muted-foreground">{run.anomaly_reason || run.error_msg}</p>
                    </td>
                    <td className="p-3 font-mono text-xs">{run.rc_doc_no || '-'}</td>
                    <td className="p-3 text-right">
                      <Button size="sm" variant="outline" onClick={() => void open(run)}>
                        รายละเอียด
                        <ChevronRight className="ml-1 h-4 w-4" />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="divide-y md:hidden">
            {runs.map((run) => (
              <button key={run.id} onClick={() => void open(run)} className="w-full p-4 text-left">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="font-medium">{evidenceTitle(run)}</p>
                    <p className="mt-1 truncate text-xs text-muted-foreground">{run.shop_label} · {money(run.total_settlement_amount, run.currency)}</p>
                  </div>
                  <Badge variant="secondary" className={statusMeta[run.status]?.className}>{statusMeta[run.status]?.text ?? run.status}</Badge>
                </div>
              </button>
            ))}
          </div>

          {!loading && runs.length === 0 && (
            <div className="p-10 text-center text-sm text-muted-foreground">
              ยังไม่มีร่างรับชำระในช่วงนี้ กรุณานำเข้าไฟล์รายได้จาก TikTok Shop เมื่อเงินเข้าบัญชีจริงแล้ว
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={Boolean(selected)} onOpenChange={(openDialog) => !openDialog && setSelected(null)}>
        <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><WalletCards className="h-5 w-5" />{selected ? evidenceTitle(selected) : 'หลักฐานรับเงิน TikTok Shop'}</DialogTitle>
            <DialogDescription>
              {isIncomeExportRun(selected)
                ? 'สร้างจากไฟล์รายได้ที่พนักงานยืนยันว่าเงินเข้าบัญชีแล้ว โปรดตรวจรายการก่อนกดส่ง RC เข้า SML'
                : 'TikTok แจ้งว่าโอนแล้ว เป็นหลักฐานจากแพลตฟอร์ม ไม่ใช่การยืนยันจาก statement ธนาคาร'}
            </DialogDescription>
          </DialogHeader>

          {selected && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
                <Metric label="ยอดใบขาย SML" value={money(selected.invoice_amount_total)} />
                <Metric label="TikTok โอน" value={money(selected.total_settlement_amount)} />
                <Metric label="Fee / commission" value={money(selected.fee_amount_total)} />
                <Metric label="Payment ID" value={selected.payment_id || '-'} />
              </div>
              {(selected.anomaly_reason || selected.error_msg) && (
                <div className="flex gap-2 rounded-md border border-warning/30 bg-warning/5 p-3 text-sm text-warning">
                  <AlertTriangle className="h-4 w-4 shrink-0" />
                  {selected.anomaly_reason || selected.error_msg}
                </div>
              )}
              <div className="rounded-md border">
                <div className="grid grid-cols-[1fr_auto_auto] gap-2 border-b p-2 text-xs text-muted-foreground">
                  <span>คำสั่งซื้อ / ใบขาย SML</span>
                  <span>ยอด TikTok</span>
                  <span>ผลตรวจ</span>
                </div>
                {(selected.items ?? []).map((item) => (
                  <div key={item.id} className="grid grid-cols-[1fr_auto_auto] gap-2 border-b p-2 text-sm last:border-0">
                    <div className="min-w-0">
                      <p className="truncate">{item.order_id}</p>
                      <p className="truncate text-xs text-muted-foreground">{item.sml_invoice_doc_no || item.block_reason || 'รอตรวจ SML'}</p>
                    </div>
                    <span>{money(item.settlement_amount)}</span>
                    <Badge variant="secondary" className={item.status === 'ready' || item.status === 'sent' ? 'bg-success/15 text-success' : 'bg-warning/15 text-warning'}>
                      {item.status === 'ready' ? 'ผ่าน' : item.status === 'sent' ? 'ส่งแล้ว' : 'ต้องตรวจ'}
                    </Badge>
                  </div>
                ))}
              </div>
            </div>
          )}

          <DialogFooter className="gap-2">
            <Button variant="outline" onClick={() => void reconcile()} disabled={!selected || selected.status === 'sent'}>ตรวจเทียบใหม่</Button>
            <Button onClick={() => setSendConfirmOpen(true)} disabled={sending || selected?.status !== 'ready' || !route?.configured}>
              <Send className="mr-2 h-4 w-4" />
              ยืนยันส่ง RC เข้า SML
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={incomeOpen} onOpenChange={(openDialog) => {
        setIncomeOpen(openDialog)
        if (!openDialog) setIncomePreview(null)
      }}>
        <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><FileSpreadsheet className="h-5 w-5" />สร้างร่าง RC จากไฟล์รายได้</DialogTitle>
            <DialogDescription>
              เลือกรอบถอนที่อยู่ในไฟล์ และใช้เฉพาะเมื่อพนักงานเห็นเงินเข้าบัญชีจริงแล้ว ระบบตรวจยอดก่อนสร้างร่าง RC และจะไม่ส่ง SML ในขั้นตอนนี้
            </DialogDescription>
          </DialogHeader>
          {incomePreview && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3 rounded-md border bg-muted/20 p-3 text-sm sm:grid-cols-3">
                <Metric label="คำสั่งซื้อในไฟล์" value={`${incomePreview.order_count} รายการ`} />
                <Metric label="ยอดรายละเอียด" value={money(incomePreview.order_payment_total, incomePreview.currency)} />
                <Metric label="ยอดสรุปรายงาน" value={money(incomePreview.report_payment_total, incomePreview.currency)} />
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium" htmlFor="income-withdrawal">รอบถอนเงินในไฟล์</label>
                <Select value={selectedIncomeWithdrawalID} onValueChange={setSelectedIncomeWithdrawalID}>
                  <SelectTrigger id="income-withdrawal">
                    <SelectValue placeholder="เลือกรอบถอนเงินที่เงินเข้าบัญชีแล้ว" />
                  </SelectTrigger>
                  <SelectContent>
                    {incomePreview.withdrawals.map((withdrawal) => (
                      <SelectItem key={withdrawal.withdrawal_id} value={withdrawal.withdrawal_id}>
                        {withdrawal.withdrawal_id} · {money(withdrawal.amount_cents / 100, withdrawal.currency)} · {withdrawal.status}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {selectedIncomeWithdrawal && (
                  <p className="text-xs text-muted-foreground">
                    คำขอถอน {selectedIncomeWithdrawal.requested_at || '-'} · สำเร็จ {selectedIncomeWithdrawal.completed_at || '-'}
                  </p>
                )}
              </div>
              {selectedIncomeWithdrawal && !incomeTotalsMatch && (
                <div className="flex gap-2 rounded-md border border-warning/30 bg-warning/5 p-3 text-sm text-warning">
                  <AlertTriangle className="h-4 w-4 shrink-0" />
                  ยอดรอบถอนต้องตรงกับยอดรายละเอียดและยอดสรุปรายงานทั้งชุด ระบบจึงจะสร้างร่าง RC ได้ กรุณาส่งออกไฟล์ในช่วงที่ตรงกับรอบถอนนี้
                </div>
              )}
              <div className="space-y-2">
                <label className="text-sm font-medium" htmlFor="bank-received-at">วันและเวลาที่เงินเข้าบัญชีจริง</label>
                <Input
                  id="bank-received-at"
                  type="datetime-local"
                  value={bankReceivedAt}
                  onChange={(event) => setBankReceivedAt(event.target.value)}
                />
              </div>
              <label className="flex cursor-pointer items-start gap-3 rounded-md border p-3 text-sm">
                <Checkbox checked={bankReceiptConfirmed} onCheckedChange={(checked) => setBankReceiptConfirmed(checked === true)} />
                <span>
                  ฉันยืนยันว่าเงินของรอบถอนนี้เข้าบัญชีจริงแล้ว และไฟล์รายได้นี้เป็นรายละเอียดของรอบถอนที่เลือก
                  <span className="mt-1 block text-xs text-muted-foreground">การยืนยันนี้สร้างเพียงร่าง RC; ยังต้องตรวจและกดส่ง SML แยกต่างหาก</span>
                </span>
              </label>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setIncomeOpen(false)}>ยกเลิก</Button>
            <Button onClick={() => void createIncomeCandidate()} disabled={!incomeTotalsMatch || !bankReceivedAt || !bankReceiptConfirmed || creatingIncomeCandidate}>
              {creatingIncomeCandidate ? 'กำลังสร้างร่าง…' : 'สร้างร่าง RC เพื่อ ตรวจสอบ'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={withdrawalOpen} onOpenChange={setWithdrawalOpen}>
        <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><WalletCards className="h-5 w-5" />รอบถอนเงิน TikTok Shop</DialogTitle>
            <DialogDescription>
              แสดงเฉพาะรายการที่กดถอนเงินในช่วงวันที่เลือก ใช้ตรวจเทียบกับไฟล์รายได้เท่านั้น TikTok Shop ยังไม่เปิด API ที่บอกว่าออเดอร์ใดอยู่ในรอบถอนของตลาดไทย และระบบจะไม่เดาจากยอดเงินหรือวันเวลา
            </DialogDescription>
          </DialogHeader>
          {withdrawalLoading ? (
            <p className="py-6 text-center text-sm text-muted-foreground">กำลังดึงรอบถอนเงิน…</p>
          ) : withdrawals.length === 0 ? (
              <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">ไม่พบรายการกดถอนเงินในช่วงวันที่เลือก</p>
          ) : (
            <div className="space-y-2">
              {withdrawalsTruncated && (
                <p className="rounded-md border border-warning/30 bg-warning/5 p-2 text-xs text-warning">แสดง 100 รอบแรกของช่วงวันที่เลือก กรุณาเลือกช่วงวันที่แคบลงเพื่อให้ตรวจครบ</p>
              )}
              <div className="overflow-hidden rounded-md border">
                <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-b bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                  <span>รอบถอนเงิน / วันที่</span><span className="text-right">ยอด / สถานะ</span>
                </div>
                {withdrawals.map((withdrawal) => (
                  <div key={withdrawal.withdrawal_id} className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-b px-3 py-2.5 text-sm last:border-0">
                    <div className="min-w-0">
                      <p className="truncate font-medium">{withdrawal.withdrawal_id}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">{withdrawal.type} · {dayjs(withdrawal.create_time).format('DD/MM/YY HH:mm')}</p>
                    </div>
                    <div className="text-right">
                      <p className="font-medium tabular-nums">{money(Number(withdrawal.amount), withdrawal.currency)}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">{withdrawal.status}</p>
                      <p className="mt-2 text-xs text-muted-foreground">{withdrawal.status === 'SUCCESS' ? 'TikTok ยังไม่ส่งรายการออเดอร์ของรอบนี้ผ่าน API' : 'ยังไม่สำเร็จ'}</p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setWithdrawalOpen(false)}>ปิด</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>ตั้งค่ารับชำระ TikTok Shop</DialogTitle>
            <DialogDescription>เปิดอ่านเพื่อตรวจรอบถอนเงินและนำเข้าไฟล์รายได้ ส่วนส่ง SML ยังคงต้องให้พนักงานกดยืนยันทีละร่าง RC</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium">อนุญาตอ่านข้อมูลการเงิน</p>
                <p className="text-xs text-muted-foreground">ใช้กับตรวจระบบและตรวจรอบถอนเงินด้วยมือ</p>
              </div>
              <Switch checked={readEnabled} onCheckedChange={setReadEnabled} />
            </div>
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium">อนุญาตส่ง RC เข้า SML</p>
                <p className="text-xs text-muted-foreground">ยังไม่มี Auto RC และต้องยืนยันก่อนส่งทุกครั้ง</p>
              </div>
              <Switch checked={smlEnabled} onCheckedChange={setSmlEnabled} disabled={!readEnabled} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSettingsOpen(false)}>ยกเลิก</Button>
            <Button onClick={() => setSettingsConfirmOpen(true)}>บันทึกการตั้งค่า</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={settingsConfirmOpen}
        onOpenChange={setSettingsConfirmOpen}
        title="ยืนยันเปลี่ยนการตั้งค่าร้าน"
        description={`ร้านนี้จะ${readEnabled ? '' : 'ไม่'}อนุญาตให้อ่านข้อมูลรอบถอนเงิน และ${smlEnabled ? '' : 'ไม่'}อนุญาตให้ส่ง RC เข้า SML\n\nการส่ง RC ยังต้องยืนยันทีละร่างเสมอ และไม่มี Auto RC`}
        confirmLabel="ยืนยันบันทึก"
        onConfirm={confirmSaveSettings}
      />

      <ConfirmDialog
        open={sendConfirmOpen}
        onOpenChange={setSendConfirmOpen}
        title="ยืนยันสร้าง RC เข้า SML"
        description={selected ? `${isIncomeExportRun(selected) ? `พนักงานยืนยันเงินเข้าบัญชีแล้วสำหรับ ${evidenceTitle(selected)}` : `TikTok แจ้งว่าโอนแล้วสำหรับ ${evidenceTitle(selected)}`}\n${selected.items?.length ?? selected.item_count ?? 0} คำสั่งซื้อ · ยอด TikTok ${money(selected.total_settlement_amount, selected.currency)}\nรูปแบบ RC: ${route?.doc_format_code || '-'} · บัญชีรับเงิน: ${route?.passbook_name || route?.passbook_code || '-'}\nค่าใช้จ่าย TikTok: ${route?.expense_name || route?.expense_code || '-'}\n\nการยืนยันนี้จะสร้างเอกสารรับชำระหนี้ใน SML และไม่มี Auto RC` : ''}
        confirmLabel="ยืนยันส่ง RC"
        onConfirm={confirmSend}
      />
    </div>
  )
}

function SettlementMetricChip({ label, value, tone }: {
  label: string
  value: number
  tone: 'primary' | 'success' | 'warning' | 'danger'
}) {
  const toneClass = tone === 'success'
    ? 'border-success/25 bg-success/10 text-success'
    : tone === 'warning'
      ? 'border-warning/30 bg-warning/10 text-warning'
      : tone === 'danger'
        ? 'border-destructive/25 bg-destructive/10 text-destructive'
        : 'border-primary/25 bg-primary/10 text-accent-strong'

  return (
    <span className={cn('inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[11px]', toneClass)}>
      <span className="font-semibold tabular-nums">{value.toLocaleString()}</span>
      <span className="text-foreground/75">{label}</span>
    </span>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-muted/40 p-2">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 truncate text-sm font-semibold">{value}</p>
    </div>
  )
}
