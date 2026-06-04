import { useState, useRef, useEffect, Fragment } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertCircle,
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock3,
  Database,
  FileSpreadsheet,
  Info,
  Loader2,
  ShieldCheck,
} from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { cn } from '@/lib/utils'

interface LazadaConfig {
  server_url: string
  guid: string
  provider: string
  config_file_name: string
  database_name: string
  doc_format_code: string
  endpoint?: string
  cust_code: string
  sale_code: string
  branch_code: string
  wh_code: string
  shelf_code: string
  unit_code: string
  vat_type: number
  vat_rate: number
  doc_time: string
}

interface LazadaOrderItem {
  sku: string
  product_name: string
  option_name?: string
  raw_name: string
  price: number
  qty: number
  no_sku?: boolean
}
interface LazadaOrder {
  order_id: string
  doc_date: string
  order_datetime?: string
  payment_time?: string
  payment_channel?: string
  buyer_username?: string
  tracking_no?: string
  status: string
  items: LazadaOrderItem[]
  item_count: number
  total_qty: number
  paid_amount?: number
  order_total_amount?: number
  item_gross_amount?: number
  line_paid_amount?: number
  shipping_amount?: number
  discount_amount?: number
  no_sku_item_count?: number
  has_no_sku?: boolean
  multi_line?: boolean
  amount_mismatch?: boolean
  existing_bill_id?: string
  duplicate: boolean
}
interface ImportPreflight {
  new_orders: number
  duplicate_orders: number
  skipped_rows: number
  no_sku_orders: number
  no_sku_items: number
  multi_item_orders: number
  amount_mismatch_orders: number
}
interface PreviewResponse {
  orders: LazadaOrder[]
  warnings: string[]
  total_orders: number
  new_count: number
  duplicate_count: number
  skipped_count: number
  import_run_id?: string
  preflight: ImportPreflight
  file_token?: string
}
interface ConfirmResult {
  order_id: string
  success: boolean
  bill_id?: string
  doc_no?: string
  message?: string
}
interface ImportRunSummary {
  id: string
  filename: string
  period_start?: string
  period_end?: string
  total_orders: number
  new_orders: number
  duplicate_orders: number
  skipped_orders: number
  warning_count: number
  created_count: number
  failed_count: number
  status: 'preview' | 'confirmed' | 'failed'
  created_at: string
  confirmed_at?: string
}

function fmt(n: number) {
  return n.toLocaleString('th-TH', { minimumFractionDigits: 2 })
}

function fmtDateTime(s: string) {
  if (!s) return '—'
  return new Date(s).toLocaleString('th-TH', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function lazadaDestination(config?: LazadaConfig | null) {
  const docFormat = (config?.doc_format_code ?? '').trim().toUpperCase()
  const endpoint = (config?.endpoint ?? '').toLowerCase()
  const isSaleInvoice = endpoint.includes('saleinvoice') || docFormat === 'SI'
  return isSaleInvoice
    ? {
        documentName: 'เอกสารขายสินค้าและบริการ',
        shortName: 'ขายสินค้าและบริการ',
        smlPath: 'ขาย -> ขายสินค้าและบริการ',
        action: 'สร้างเอกสารขายสินค้าและบริการ',
        done: 'สร้างเอกสารขายสินค้าและบริการแล้ว',
        listPath: '/sale-invoices',
        listName: 'ขายสินค้าและบริการ',
      }
    : {
        documentName: 'ใบสั่งขาย',
        shortName: 'ใบสั่งขาย',
        smlPath: 'ขาย -> ใบสั่งขาย',
        action: 'สร้างใบสั่งขาย',
        done: 'สร้างใบสั่งขายแล้ว',
        listPath: '/sales-orders',
        listName: 'ใบสั่งขาย',
      }
}

function SummaryCard({
  label,
  value,
  variant = 'muted',
}: {
  label: string
  value: number
  variant?: 'success' | 'danger' | 'primary' | 'muted'
}) {
  const tone: Record<typeof variant, string> = {
    success: 'border-success/30 bg-success/5 text-success',
    danger: 'border-destructive/30 bg-destructive/5 text-destructive',
    primary: 'border-primary/30 bg-primary/5 text-accent-strong',
    muted: 'border-border bg-muted/30 text-foreground',
  }
  return (
    <Card className={cn('text-center', tone[variant])}>
      <CardContent className="p-4">
        <p className="text-3xl font-semibold tabular-nums">{value}</p>
        <p className="mt-1 text-xs font-medium text-muted-foreground">{label}</p>
      </CardContent>
    </Card>
  )
}

type Step = 'idle' | 'uploading' | 'preview' | 'confirming' | 'done'

export default function LazadaImport() {
  const fileRef = useRef<HTMLInputElement>(null)
  const [step, setStep] = useState<Step>('idle')
  const [config, setConfig] = useState<LazadaConfig | null>(null)
  const [preview, setPreview] = useState<PreviewResponse | null>(null)
  const [selectedIDs, setSelectedIDs] = useState<Set<string>>(new Set())
  const [results, setResults] = useState<{
    success_count: number
    fail_count: number
    results: ConfirmResult[]
  } | null>(null)
  const [error, setError] = useState('')
  const [expandedOrders, setExpandedOrders] = useState<Set<string>>(new Set())
  const [recentRuns, setRecentRuns] = useState<ImportRunSummary[]>([])
  const [confirmElapsed, setConfirmElapsed] = useState(0)

  // Track config load + ready states separately so preflight UI can render
  // a missing-config banner BEFORE admin uploads a file. Without this, file
  // upload silently succeeds → preview works → confirm fails late with a
  // confusing "config missing" error.
  const [configLoading, setConfigLoading] = useState(true)
  const configReady = !configLoading
  const destination = lazadaDestination(config)

  const fallbackConfig: LazadaConfig = {
    server_url: '',
    guid: '',
    provider: '',
    config_file_name: '',
    database_name: '',
    doc_format_code: 'SR',
    endpoint: '',
    cust_code: '',
    sale_code: '',
    branch_code: '',
    wh_code: '',
    shelf_code: '',
    unit_code: '',
    vat_type: -1,
    vat_rate: -1,
    doc_time: '',
  }

  useEffect(() => {
    let alive = true
    client
      .get<LazadaConfig>('/api/settings/lazada-config')
      .then((res) => {
        if (alive) setConfig(res.data)
      })
      .catch(() => {
        if (alive) setError('โหลด config ไม่ได้')
      })
      .finally(() => {
        if (alive) setConfigLoading(false)
      })
    client
      .get<{ runs: ImportRunSummary[] }>('/api/import/lazada/runs?limit=5')
      .then((res) => {
        if (alive) setRecentRuns(res.data.runs ?? [])
      })
      .catch(() => undefined)
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    if (step !== 'confirming') {
      setConfirmElapsed(0)
      return
    }
    const startedAt = Date.now()
    const timer = window.setInterval(() => {
      setConfirmElapsed(Math.floor((Date.now() - startedAt) / 1000))
    }, 1000)
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('beforeunload', onBeforeUnload)
    }
  }, [step])

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    e.target.value = ''
    setStep('uploading')
    setError('')
    setPreview(null)
    setResults(null)
    const form = new FormData()
    form.append('file', file)
    try {
      const res = await client.post<PreviewResponse>(
        '/api/import/lazada/preview',
        form,
        { headers: { 'Content-Type': 'multipart/form-data' } },
      )
      setPreview(res.data)
      setSelectedIDs(
        new Set(res.data.orders.filter((o) => !o.duplicate).map((o) => o.order_id)),
      )
      setStep('preview')
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          'อัปโหลดไฟล์ไม่ได้',
      )
      setStep('idle')
    }
  }

  const handleConfirm = async () => {
    if (!preview || selectedIDs.size === 0) return
    setStep('confirming')
    setError('')
    try {
      const res = await client.post('/api/import/lazada/confirm', {
        config: config ?? fallbackConfig,
        order_ids: Array.from(selectedIDs),
        orders: preview.orders,
        file_token: preview.file_token,
        import_run_id: preview.import_run_id,
      }, { timeout: 120000 })
      setResults(res.data)
      setStep('done')
    } catch (err: unknown) {
      setError(
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          'ส่งข้อมูลไม่ได้',
      )
      setStep('preview')
    }
  }

  const toggleOrder = (id: string) =>
    setSelectedIDs((p) => {
      const s = new Set(p)
      if (s.has(id)) s.delete(id)
      else s.add(id)
      return s
    })
  const toggleAll = () => {
    if (!preview) return
    const nonDup = preview.orders.filter((o) => !o.duplicate).map((o) => o.order_id)
    setSelectedIDs(selectedIDs.size === nonDup.length ? new Set() : new Set(nonDup))
  }
  const toggleExpand = (id: string) =>
    setExpandedOrders((p) => {
      const s = new Set(p)
      if (s.has(id)) s.delete(id)
      else s.add(id)
      return s
    })

  return (
    <div className="space-y-5">
      <PageHeader
        title="Lazada Excel"
        description={`อัปโหลดไฟล์จาก Lazada Seller Center แล้วสร้างเป็น${destination.documentName}สำหรับตรวจและส่งเข้า SML`}
      />

      <Alert>
        <Info className="h-4 w-4" />
        <AlertTitle>ช่องทางรับข้อมูลสำหรับ{destination.shortName}</AlertTitle>
        <AlertDescription>
          หน้านี้ทำหน้าที่นำเข้าไฟล์เท่านั้น ระบบจะสร้างรายการไปที่เมนู{' '}
          <Link to={destination.listPath} className="font-medium text-link hover:underline">
            {destination.listName}
          </Link>{' '}
          เพื่อให้ตรวจสินค้า หน่วย จำนวน ราคา และส่งเข้า SML ปลายทาง{' '}
          <span className="font-medium text-foreground">{destination.smlPath}</span>
        </AlertDescription>
      </Alert>

      {recentRuns.length > 0 && step === 'idle' && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Clock3 className="h-4 w-4 text-muted-foreground" />
              ประวัติการนำเข้าล่าสุด
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 pt-0">
            {recentRuns.map((run) => (
              <div
                key={run.id}
                className="grid gap-2 rounded-md border border-border px-3 py-2 text-xs sm:grid-cols-[minmax(0,1fr)_auto]"
              >
                <div className="min-w-0">
                  <div className="truncate font-medium text-foreground">
                    {run.filename || 'Lazada Excel'}
                  </div>
                  <div className="mt-0.5 text-muted-foreground">
                    {fmtDateTime(run.created_at)}
                    {run.period_start && run.period_end
                      ? ` · ${run.period_start} ถึง ${run.period_end}`
                      : ''}
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-1 sm:justify-end">
                  <Badge variant={run.status === 'confirmed' ? 'default' : 'secondary'}>
                    {run.status === 'confirmed' ? 'สร้างแล้ว' : 'ตรวจรายการ'}
                  </Badge>
                  <Badge variant="outline">ใหม่ {run.new_orders}</Badge>
                  <Badge variant="outline">ซ้ำ {run.duplicate_orders}</Badge>
                  <Badge variant="outline">ข้าม {run.skipped_orders}</Badge>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <input
        ref={fileRef}
        type="file"
        accept=".xlsx"
        className="sr-only"
        onChange={handleFileChange}
      />

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {(step === 'idle' || step === 'uploading') && (
        <>
          <div
            className={cn(
              'flex flex-col items-center justify-center rounded-lg border-2 border-dashed border-border bg-muted/20 p-10 text-center',
              step === 'uploading' && 'opacity-60',
            )}
          >
            {step === 'uploading' ? (
              <p className="text-sm text-muted-foreground">กำลังวิเคราะห์ไฟล์…</p>
            ) : (
              <>
                <FileSpreadsheet className="mb-3 h-10 w-10 text-muted-foreground" />
                <p className="text-sm font-medium text-foreground">
                  คลิกเพื่อเลือกไฟล์ Excel (.xlsx) จาก Lazada
                </p>
                <p className="mt-1 text-[11px] text-muted-foreground">
                  รองรับไฟล์ที่ export จาก Lazada Seller Center ที่มี column: orderNumber, itemName, variation, paidPrice, sellerSku
                </p>
                <Button
                  className="mt-4"
                  onClick={() => fileRef.current?.click()}
                  disabled={!configReady}
                  title={!configReady ? 'กำลังเตรียมหน้า import' : undefined}
                >
                  {configLoading
                    ? 'กำลังโหลด config…'
                    : 'เลือกไฟล์ Lazada'}
                </Button>
              </>
            )}
          </div>
        </>
      )}

      {step === 'preview' && preview && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <SummaryCard
              label="Orders ทั้งหมด"
              value={preview.total_orders}
              variant="primary"
            />
            <SummaryCard
              label="Order ใหม่"
              value={preview.preflight?.new_orders ?? preview.new_count}
              variant="success"
            />
            <SummaryCard
              label="เลือกแล้ว"
              value={selectedIDs.size}
              variant="success"
            />
            <SummaryCard
              label="ซ้ำ (ข้ามไป)"
              value={preview.duplicate_count}
              variant="muted"
            />
          </div>

          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>นโยบายการนำเข้าซ้ำ</AlertTitle>
            <AlertDescription>
              ถ้าไฟล์ Lazada ครอบคลุมช่วงวันที่เดิม ระบบจะสร้างเฉพาะ Order ID ที่ยังไม่มีใน Nexflow และจะข้ามรายการซ้ำโดยไม่เขียนทับบิลเดิม
            </AlertDescription>
          </Alert>

          {(preview.preflight?.no_sku_items ?? 0) > 0 && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>ไฟล์นี้ไม่มี SKU บางรายการ</AlertTitle>
              <AlertDescription>
                พบ {preview.preflight.no_sku_items} รายการสินค้าใน {preview.preflight.no_sku_orders} order ที่ไม่มี SKU ระบบจะใช้ชื่อสินค้า + ตัวเลือกสินค้าเป็นข้อมูลจับคู่แทน
              </AlertDescription>
            </Alert>
          )}

          {(preview.warnings ?? []).length > 0 && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>คำเตือน ({preview.warnings.length} รายการ)</AlertTitle>
              <AlertDescription>
                <ul className="mt-1 list-disc pl-5 text-xs">
                  {preview.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" size="sm" onClick={toggleAll}>
              {selectedIDs.size === preview.orders.filter((o) => !o.duplicate).length
                ? 'ยกเลิกทั้งหมด'
                : 'เลือกทั้งหมด'}
            </Button>
            <Button
              size="sm"
              disabled={selectedIDs.size === 0}
              onClick={handleConfirm}
            >
              {destination.action} {selectedIDs.size} รายการ
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setStep('idle')
                setPreview(null)
              }}
            >
              เลือกไฟล์ใหม่
            </Button>
          </div>

          <div className="overflow-hidden rounded-lg border border-border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/40">
                  <TableHead className="w-10">
                    <Checkbox
                      checked={
                        selectedIDs.size ===
                        preview.orders.filter((o) => !o.duplicate).length
                      }
                      onCheckedChange={toggleAll}
                      aria-label="เลือกทั้งหมด"
                    />
                  </TableHead>
                  <TableHead>Order ID</TableHead>
                  <TableHead>วันที่</TableHead>
                  <TableHead>ผู้ซื้อ</TableHead>
                  <TableHead>สถานะ</TableHead>
                  <TableHead>สินค้า</TableHead>
                  <TableHead className="text-right">Qty รวม</TableHead>
                  <TableHead className="text-right">ยอดชำระ</TableHead>
                  <TableHead>Preflight</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {preview.orders.map((order) => {
                  const expanded = expandedOrders.has(order.order_id)
                  return (
                    <Fragment key={order.order_id}>
                      <TableRow
                        className={cn(
                          order.duplicate && 'bg-muted/30 text-muted-foreground',
                        )}
                      >
                        <TableCell>
                          <Checkbox
                            checked={selectedIDs.has(order.order_id)}
                            disabled={order.duplicate}
                            onCheckedChange={() => toggleOrder(order.order_id)}
                            aria-label={`เลือก order ${order.order_id}`}
                          />
                        </TableCell>
                        <TableCell>
                          <button
                            type="button"
                            className="inline-flex items-center gap-1 font-mono text-xs font-medium text-foreground hover:text-link"
                            onClick={() => toggleExpand(order.order_id)}
                          >
                            {expanded ? (
                              <ChevronDown className="h-3 w-3" />
                            ) : (
                              <ChevronRight className="h-3 w-3" />
                            )}
                            {order.order_id}
                          </button>
                        </TableCell>
                        <TableCell className="text-xs tabular-nums text-muted-foreground">
                          {order.order_datetime || order.doc_date}
                        </TableCell>
                        <TableCell className="max-w-[160px] truncate text-xs text-muted-foreground">
                          {order.buyer_username || '—'}
                        </TableCell>
                        <TableCell>
                          <Badge variant="secondary" className="text-xs font-normal">
                            {order.status}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-xs">
                          {order.item_count} รายการ
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {order.total_qty}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {order.paid_amount != null ? `฿${fmt(order.paid_amount)}` : '—'}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                          {order.duplicate && (
                            <Badge variant="secondary" className="bg-warning/15 text-warning hover:bg-warning/20">
                              มีในระบบแล้ว
                            </Badge>
                          )}
                          {order.has_no_sku && (
                            <Badge variant="outline" className="border-warning/40 text-warning">
                              ไม่มี SKU {order.no_sku_item_count}
                            </Badge>
                          )}
                          {order.multi_line && (
                            <Badge variant="outline">หลายรายการ</Badge>
                          )}
                          {order.amount_mismatch && (
                            <Badge variant="outline">
                              มีส่วนต่างยอด
                            </Badge>
                          )}
                          </div>
                        </TableCell>
                      </TableRow>
                      {expanded && (
                        <TableRow>
                          <TableCell colSpan={9} className="bg-muted/20 p-0">
                            <div className="overflow-hidden border border-border bg-background">
                              <Table>
                                <TableHeader>
                                  <TableRow className="bg-muted/30">
                                    <TableHead className="text-[11px]">SKU</TableHead>
                                    <TableHead className="text-[11px]">ชื่อสินค้า</TableHead>
                                    <TableHead className="text-[11px]">ตัวเลือก</TableHead>
                                    <TableHead className="text-right text-[11px]">ราคา</TableHead>
                                    <TableHead className="text-right text-[11px]">จำนวน</TableHead>
                                  </TableRow>
                                </TableHeader>
                                <TableBody>
                                  {order.items.map((item, i) => (
                                    <TableRow key={i}>
                                      <TableCell className="font-mono text-xs">
                                        {item.sku || (
                                          <Badge variant="outline" className="border-warning/40 text-warning">
                                            ไม่มี SKU
                                          </Badge>
                                        )}
                                      </TableCell>
                                      <TableCell className="text-sm">
                                        <div>{item.raw_name || item.product_name}</div>
                                        {item.raw_name && item.raw_name !== item.product_name && (
                                          <div className="mt-1 text-[11px] text-muted-foreground">
                                            ต้นทาง: {item.product_name}
                                          </div>
                                        )}
                                      </TableCell>
                                      <TableCell className="text-xs text-muted-foreground">
                                        {item.option_name || '—'}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {fmt(item.price)}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {item.qty}
                                      </TableCell>
                                    </TableRow>
                                  ))}
                                </TableBody>
                              </Table>
                            </div>
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        </>
      )}

      {step === 'confirming' && (
        <Card className="overflow-hidden">
          <CardContent className="p-0">
            <div className="border-b border-border bg-muted/30 px-5 py-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-semibold text-foreground">
                    กำลัง{destination.action}จาก Lazada Excel
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    ระบบกำลังจับคู่สินค้า สร้างเอกสาร และแนบไฟล์ต้นฉบับไว้เป็นหลักฐาน
                  </p>
                </div>
                <Badge variant="secondary" className="gap-1.5">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  {confirmElapsed}s
                </Badge>
              </div>
              <div className="mt-4 h-2 overflow-hidden rounded-full bg-background">
                <div
                  className="h-full rounded-full bg-primary transition-all duration-500"
                  style={{ width: `${Math.min(92, 18 + confirmElapsed * 2)}%` }}
                />
              </div>
            </div>
            <div className="grid gap-3 p-5 sm:grid-cols-3">
              <div className="rounded-md border border-border p-3">
                <Database className="h-4 w-4 text-accent-strong" />
                <p className="mt-2 text-xs font-medium">Order ที่เลือก</p>
                <p className="mt-1 text-2xl font-semibold tabular-nums">
                  {selectedIDs.size}
                </p>
              </div>
              <div className="rounded-md border border-border p-3">
                <ShieldCheck className="h-4 w-4 text-success" />
                <p className="mt-2 text-xs font-medium">กันนำเข้าซ้ำ</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Order ID ที่มีแล้วจะถูกข้าม ไม่เขียนทับบิลเดิม
                </p>
              </div>
              <div className="rounded-md border border-border p-3">
                <Clock3 className="h-4 w-4 text-warning" />
                <p className="mt-2 text-xs font-medium">กรุณารอหน้านี้</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  ถ้าเปลี่ยนเมนู งานบน server อาจยังทำต่อ ให้ดูผลย้อนหลังในประวัติการนำเข้า
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {step === 'done' && results && (
        <>
          <Alert>
            <CheckCircle2 className="h-4 w-4 text-success" />
            <AlertTitle>{destination.done} {results.success_count} รายการ</AlertTitle>
            <AlertDescription>
              ระบบ map สินค้าให้เบื้องต้น แต่ <b>ยังไม่ส่ง SML</b> — กรุณาไปที่เมนู{destination.listName}เพื่อตรวจสินค้า
              หน่วย จำนวน ราคา และส่งเข้า SML ปลายทาง {destination.smlPath}
            </AlertDescription>
          </Alert>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <SummaryCard
              label="สร้างเอกสารสำเร็จ"
              value={results.success_count}
              variant="success"
            />
            <SummaryCard
              label="ข้าม / ล้มเหลว"
              value={results.fail_count}
              variant="danger"
            />
            <SummaryCard
              label="ทั้งหมด"
              value={results.results.length}
              variant="primary"
            />
          </div>

          <div className="flex gap-2">
            <Button asChild>
              <Link to={destination.listPath}>
                ไปตรวจ{destination.listName}
                <ArrowRight className="h-4 w-4" />
              </Link>
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                setStep('idle')
                setPreview(null)
                setResults(null)
              }}
            >
              นำเข้าไฟล์ใหม่
            </Button>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-sm">รายละเอียดผลลัพธ์</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow className="bg-muted/40">
                    <TableHead>Order ID</TableHead>
                    <TableHead>ผล</TableHead>
                    <TableHead>หมายเหตุ</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {results.results.map((r) => (
                    <TableRow key={r.order_id}>
                      <TableCell className="font-mono text-xs">{r.order_id}</TableCell>
                      <TableCell>
                        {r.success ? (
                          r.bill_id ? (
                            <Link
                              to={`${destination.listPath}/${r.bill_id}`}
                              className="inline-flex items-center gap-1 font-medium text-success hover:underline"
                            >
                              เปิดรายละเอียด
                              <ArrowRight className="h-3 w-3" />
                            </Link>
                          ) : (
                            <span className="font-medium text-success">สำเร็จ</span>
                          )
                        ) : (
                          r.bill_id ? (
                            <Link
                              to={`${destination.listPath}/${r.bill_id}`}
                              className="inline-flex items-center gap-1 font-medium text-warning hover:underline"
                            >
                              เปิดรายการเดิม
                              <ArrowRight className="h-3 w-3" />
                            </Link>
                          ) : (
                            <span className="font-medium text-destructive">
                              ข้าม / ล้มเหลว
                            </span>
                          )
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {r.message}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
