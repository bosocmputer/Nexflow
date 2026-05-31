import { useCallback, useEffect, useMemo, useState, type ComponentType } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  CheckCircle2,
  Clock,
  Eye,
  RefreshCw,
  ReceiptText,
  Send,
  XCircle,
} from 'lucide-react'

import { PageHeader } from '@/components/common/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  getBulkSendJob,
  listBulkSendJobs,
  type BulkSendJob,
  type BulkSendJobItem,
} from '@/hooks/useBills'
import { cn } from '@/lib/utils'

const STATUS_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'all', label: 'ทุกสถานะ' },
  { value: 'queued', label: 'รอส่ง' },
  { value: 'running', label: 'กำลังส่ง' },
  { value: 'completed', label: 'สำเร็จทั้งหมด' },
  { value: 'completed_with_errors', label: 'สำเร็จบางส่วน' },
  { value: 'failed', label: 'ล้มเหลว' },
]

const ROUTE_OPTIONS: Array<{ value: string; label: string; source?: string; bill_type?: string; document_route?: string }> = [
  { value: 'all', label: 'ทุกปลายทาง' },
  { value: 'purchaseorder', label: 'ใบสั่งซื้อ', source: 'shopee_shipped', bill_type: 'purchase' },
  { value: 'saleorder', label: 'ใบสั่งขาย', bill_type: 'sale', document_route: 'saleorder' },
  { value: 'saleinvoice', label: 'ขายสินค้าและบริการ', bill_type: 'sale', document_route: 'saleinvoice' },
]

const STATUS_LABEL: Record<string, string> = {
  queued: 'รอส่ง',
  running: 'กำลังส่ง',
  completed: 'สำเร็จทั้งหมด',
  completed_with_errors: 'สำเร็จบางส่วน',
  failed: 'ล้มเหลว',
}

const ITEM_STATUS_LABEL: Record<string, string> = {
  queued: 'รอส่ง',
  running: 'กำลังส่ง',
  sent: 'ส่งแล้ว',
  failed: 'ไม่สำเร็จ',
  skipped: 'ข้าม',
}

function fmtDate(value?: string) {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString('th-TH', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function durationLabel(start?: string, finish?: string) {
  if (!start) return '—'
  const a = new Date(start).getTime()
  const b = finish ? new Date(finish).getTime() : Date.now()
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return '—'
  const seconds = Math.round((b - a) / 1000)
  if (seconds < 60) return `${seconds} วิ`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${minutes} นาที ${rest} วิ`
}

function statusTone(status: string) {
  if (status === 'completed') return 'border-success/30 bg-success/10 text-success'
  if (status === 'completed_with_errors') return 'border-warning/30 bg-warning/10 text-warning'
  if (status === 'failed') return 'border-destructive/30 bg-destructive/10 text-destructive'
  if (status === 'running') return 'border-primary/30 bg-primary/10 text-primary'
  return 'border-muted-foreground/20 bg-muted text-muted-foreground'
}

function routeLabel(job: BulkSendJob) {
  if (job.document_route === 'saleinvoice') return 'ขายสินค้าและบริการ'
  if (job.document_route === 'saleorder') return 'ใบสั่งขาย'
  if (job.bill_type === 'purchase') return 'ใบสั่งซื้อ'
  return job.title || 'ส่ง SML'
}

function itemPath(job: BulkSendJob, item: BulkSendJobItem) {
  if (job.document_route === 'saleinvoice') return `/sale-invoices/${item.bill_id}`
  if (job.document_route === 'saleorder') return `/sales-orders/${item.bill_id}`
  return `/bills/${item.bill_id}`
}

function ProgressBar({ job }: { job: BulkSendJob }) {
  const done = job.sent_count + job.failed_count + job.skipped_count
  const total = Math.max(job.total_count || 0, 1)
  const pct = Math.min(100, Math.round((done / total) * 100))
  return (
    <div className="min-w-[150px]">
      <div className="h-2 overflow-hidden rounded-full bg-muted">
        <div
          className={cn(
            'h-full rounded-full transition-all',
            job.failed_count > 0 ? 'bg-warning' : 'bg-primary',
          )}
          style={{ width: `${pct}%` }}
        />
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {done.toLocaleString('th-TH')} / {job.total_count.toLocaleString('th-TH')}
      </div>
    </div>
  )
}

function StatCard({ label, value, tone }: { label: string; value: number; tone?: 'success' | 'warning' | 'danger' }) {
  return (
    <Card className={cn(
      'shadow-none',
      tone === 'success' && 'border-success/25 bg-success/5',
      tone === 'warning' && 'border-warning/25 bg-warning/5',
      tone === 'danger' && 'border-destructive/25 bg-destructive/5',
    )}>
      <CardContent className="p-3">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="mt-1 text-2xl font-semibold tabular-nums">{value.toLocaleString('th-TH')}</div>
      </CardContent>
    </Card>
  )
}

export default function BulkSendJobs() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [jobs, setJobs] = useState<BulkSendJob[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<BulkSendJob | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  const page = Math.max(1, Number(searchParams.get('page') || '1') || 1)
  const rawStatus = searchParams.get('status') || 'all'
  const rawRoute = searchParams.get('route') || 'all'
  const status = STATUS_OPTIONS.some((option) => option.value === rawStatus) ? rawStatus : 'all'
  const route = ROUTE_OPTIONS.some((option) => option.value === rawRoute) ? rawRoute : 'all'
  const perPage = 20
  const totalPages = Math.max(1, Math.ceil(total / perPage))

  const routeFilter = useMemo(
    () => ROUTE_OPTIONS.find((option) => option.value === route) ?? ROUTE_OPTIONS[0],
    [route],
  )

  const fetchJobs = useCallback(async () => {
    setLoading(true)
    try {
      const res = await listBulkSendJobs({
        page,
        per_page: perPage,
        status: status === 'all' ? '' : status,
        source: routeFilter.source,
        bill_type: routeFilter.bill_type,
        document_route: routeFilter.document_route,
      })
      setJobs(res.data ?? [])
      setTotal(res.total ?? 0)
      setError('')
    } catch {
      setError('โหลดประวัติส่ง SML แบบกลุ่มไม่ได้')
    } finally {
      setLoading(false)
    }
  }, [page, status, routeFilter])

  useEffect(() => {
    fetchJobs()
  }, [fetchJobs])

  const pageStats = useMemo(() => ({
    sent: jobs.reduce((sum, job) => sum + job.sent_count, 0),
    failed: jobs.reduce((sum, job) => sum + job.failed_count, 0),
    skipped: jobs.reduce((sum, job) => sum + job.skipped_count, 0),
  }), [jobs])

  const setParam = (key: 'status' | 'route', value: string) => {
    const next = new URLSearchParams(searchParams)
    if (value === 'all') next.delete(key)
    else next.set(key, value)
    next.delete('page')
    setSearchParams(next)
  }

  const setPage = (nextPage: number) => {
    const next = new URLSearchParams(searchParams)
    if (nextPage <= 1) next.delete('page')
    else next.set('page', String(nextPage))
    setSearchParams(next)
  }

  const openDetail = async (job: BulkSendJob) => {
    setSelected(job)
    setDetailLoading(true)
    try {
      const detail = await getBulkSendJob(job.id)
      setSelected(detail)
    } finally {
      setDetailLoading(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="ประวัติส่ง SML แบบกลุ่ม"
        description="ตรวจย้อนหลังว่าแต่ละ batch ส่งกี่บิล สำเร็จ/ไม่สำเร็จ/ข้ามอะไร และเปิดดูบิลต้นทางได้ทันที"
      />

      <div className="grid gap-3 md:grid-cols-4">
        <StatCard label="จำนวนงานทั้งหมด" value={total} />
        <StatCard label="ส่งสำเร็จในหน้านี้" value={pageStats.sent} tone="success" />
        <StatCard label="ไม่สำเร็จในหน้านี้" value={pageStats.failed} tone={pageStats.failed > 0 ? 'danger' : undefined} />
        <StatCard label="ข้ามในหน้านี้" value={pageStats.skipped} tone={pageStats.skipped > 0 ? 'warning' : undefined} />
      </div>

      <Card className="border-primary/20 bg-primary/5 shadow-none">
        <CardContent className="flex flex-col gap-2 p-3 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-start gap-2">
            <ReceiptText className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
            <span>
              หน้านี้เป็นประวัติส่ง “บิล” เข้า SML แบบกลุ่มเท่านั้น ส่วนงานรับชำระ Shopee และเลข RC ดูแยกที่หน้า{' '}
              <Link to="/shopee-settlements" className="font-medium text-primary hover:underline">
                รับชำระ Shopee
              </Link>
            </span>
          </div>
          <Button asChild variant="outline" size="sm" className="shrink-0">
            <Link to="/shopee-settlements">เปิดหน้ารับชำระ</Link>
          </Button>
        </CardContent>
      </Card>

      <Card className="shadow-none">
        <CardContent className="space-y-3 p-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="grid gap-2 sm:grid-cols-2">
              <Select value={status} onValueChange={(value) => setParam('status', value)}>
                <SelectTrigger className="h-9 min-w-[180px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={route} onValueChange={(value) => setParam('route', value)}>
                <SelectTrigger className="h-9 min-w-[210px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ROUTE_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <Button variant="outline" size="sm" onClick={fetchJobs} disabled={loading}>
              <RefreshCw className={cn('mr-2 h-4 w-4', loading && 'animate-spin')} />
              รีเฟรช
            </Button>
          </div>

          {error && (
            <div className="rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

          <div className="overflow-hidden rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>เวลา</TableHead>
                  <TableHead>งาน</TableHead>
                  <TableHead>สถานะ</TableHead>
                  <TableHead>Progress</TableHead>
                  <TableHead>ผลลัพธ์</TableHead>
                  <TableHead>ผู้ทำรายการ</TableHead>
                  <TableHead className="text-right">จัดการ</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading && jobs.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} className="py-8 text-center text-sm text-muted-foreground">
                      กำลังโหลดประวัติส่ง SML...
                    </TableCell>
                  </TableRow>
                ) : jobs.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} className="py-8 text-center text-sm text-muted-foreground">
                      ยังไม่มีประวัติส่ง SML แบบกลุ่มตามตัวกรองนี้
                    </TableCell>
                  </TableRow>
                ) : jobs.map((job) => (
                  <TableRow key={job.id}>
                    <TableCell className="whitespace-nowrap align-top">
                      <div className="font-medium">{fmtDate(job.created_at)}</div>
                      <div className="text-xs text-muted-foreground">{durationLabel(job.started_at, job.finished_at)}</div>
                    </TableCell>
                    <TableCell className="min-w-[220px] align-top">
                      <div className="font-medium">{routeLabel(job)}</div>
                      <div className="mt-0.5 max-w-[340px] truncate text-xs text-muted-foreground">{job.title || job.id}</div>
                    </TableCell>
                    <TableCell className="align-top">
                      <Badge variant="outline" className={cn('whitespace-nowrap', statusTone(job.status))}>
                        {STATUS_LABEL[job.status] ?? job.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="align-top"><ProgressBar job={job} /></TableCell>
                    <TableCell className="whitespace-nowrap align-top text-sm">
                      <span className="text-success">{job.sent_count.toLocaleString('th-TH')} สำเร็จ</span>
                      <span className="mx-1 text-muted-foreground">/</span>
                      <span className={job.failed_count > 0 ? 'text-destructive' : 'text-muted-foreground'}>
                        {job.failed_count.toLocaleString('th-TH')} fail
                      </span>
                      <span className="mx-1 text-muted-foreground">/</span>
                      <span className="text-muted-foreground">{job.skipped_count.toLocaleString('th-TH')} ข้าม</span>
                    </TableCell>
                    <TableCell className="max-w-[180px] truncate align-top text-sm text-muted-foreground">
                      {job.created_by_email || 'system'}
                    </TableCell>
                    <TableCell className="text-right align-top">
                      <Button variant="ghost" size="sm" onClick={() => openDetail(job)}>
                        <Eye className="mr-2 h-4 w-4" />
                        ดูรายการ
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <span>หน้า {page.toLocaleString('th-TH')} / {totalPages.toLocaleString('th-TH')}</span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}>
                ก่อนหน้า
              </Button>
              <Button variant="outline" size="sm" disabled={page >= totalPages || loading} onClick={() => setPage(page + 1)}>
                ถัดไป
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>รายละเอียด batch ส่ง SML</DialogTitle>
            <DialogDescription>
              {selected ? `${routeLabel(selected)} · ${STATUS_LABEL[selected.status] ?? selected.status}` : ''}
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="space-y-4">
              <div className="grid gap-2 sm:grid-cols-4">
                <MiniMetric icon={Send} label="ทั้งหมด" value={selected.total_count} />
                <MiniMetric icon={CheckCircle2} label="สำเร็จ" value={selected.sent_count} tone="success" />
                <MiniMetric icon={XCircle} label="ไม่สำเร็จ" value={selected.failed_count} tone="danger" />
                <MiniMetric icon={Clock} label="ข้าม" value={selected.skipped_count} tone="warning" />
              </div>
              {selected.last_error && (
                <div className="rounded-md border border-destructive/25 bg-destructive/5 p-3 text-sm text-destructive">
                  {selected.last_error}
                </div>
              )}
              <div className="max-h-[55vh] overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-14">ลำดับ</TableHead>
                      <TableHead>Order</TableHead>
                      <TableHead>สถานะ</TableHead>
                      <TableHead>Doc no</TableHead>
                      <TableHead>Error</TableHead>
                      <TableHead className="text-right">บิล</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {detailLoading ? (
                      <TableRow>
                        <TableCell colSpan={6} className="py-8 text-center text-sm text-muted-foreground">
                          กำลังโหลดรายการใน batch...
                        </TableCell>
                      </TableRow>
                    ) : (selected.items ?? []).length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={6} className="py-8 text-center text-sm text-muted-foreground">
                          ไม่พบรายการย่อยของ batch นี้
                        </TableCell>
                      </TableRow>
                    ) : selected.items!.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="tabular-nums">{item.sequence}</TableCell>
                        <TableCell className="font-medium">{item.order_no || item.bill_id.slice(0, 8)}</TableCell>
                        <TableCell>
                          <Badge variant="outline" className={cn('whitespace-nowrap', item.status === 'sent' && 'border-success/30 bg-success/10 text-success', item.status === 'failed' && 'border-destructive/30 bg-destructive/10 text-destructive')}>
                            {ITEM_STATUS_LABEL[item.status] ?? item.status}
                          </Badge>
                        </TableCell>
                        <TableCell className="font-mono text-xs">{item.doc_no || item.doc_no_attempted || '—'}</TableCell>
                        <TableCell className="max-w-[260px] truncate text-sm text-muted-foreground">
                          {item.error || '—'}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button asChild variant="ghost" size="sm">
                            <Link to={itemPath(selected, item)}>เปิดบิล</Link>
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function MiniMetric({
  icon: Icon,
  label,
  value,
  tone,
}: {
  icon: ComponentType<{ className?: string }>
  label: string
  value: number
  tone?: 'success' | 'warning' | 'danger'
}) {
  return (
    <div className={cn(
      'flex items-center gap-2 rounded-md border p-3',
      tone === 'success' && 'border-success/25 bg-success/5',
      tone === 'warning' && 'border-warning/25 bg-warning/5',
      tone === 'danger' && 'border-destructive/25 bg-destructive/5',
    )}>
      <Icon className={cn(
        'h-4 w-4 text-muted-foreground',
        tone === 'success' && 'text-success',
        tone === 'warning' && 'text-warning',
        tone === 'danger' && 'text-destructive',
      )} />
      <div>
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-lg font-semibold tabular-nums">{value.toLocaleString('th-TH')}</div>
      </div>
    </div>
  )
}
