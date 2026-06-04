import { useEffect, useMemo, useState } from 'react'
import type React from 'react'
import { Link } from 'react-router-dom'
import dayjs from 'dayjs'
import {
  Activity,
  AlertTriangle,
  Bot,
  Coins,
  ExternalLink,
  RefreshCw,
  Search,
  Zap,
} from 'lucide-react'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { DateRangePicker } from '@/components/common/DateRangePicker'
import { PageHeader } from '@/components/common/PageHeader'
import { cn } from '@/lib/utils'

interface Bucket {
  key: string
  label: string
  requests: number
  success: number
  errors: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  estimated_cost_usd: number
}

interface UsageLog {
  id: string
  provider: string
  model: string
  feature: string
  operation: string
  bill_id?: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
  estimated_cost_usd: number
  duration_ms?: number
  status: 'success' | 'error'
  error?: string
  metadata?: Record<string, unknown>
  created_at: string
}

interface Summary {
  total: Bucket
  today: Bucket
  seven_days: Bucket
  month: Bucket
  by_model: Bucket[]
  by_feature: Bucket[]
  top_expensive: UsageLog[]
  daily: Bucket[]
  estimated_thb_rate: number
}

const FEATURE_LABEL: Record<string, string> = {
  email_extract: 'อ่านอีเมล/ไฟล์แนบ',
  shopee_email_parse: 'อ่านอีเมล Shopee',
  media_extract: 'อ่านรูป/PDF',
  daily_insight: 'Daily Insight',
  catalog_embed: 'เตรียมข้อมูลสินค้า',
  audio_transcription: 'ถอดเสียง',
}

function moneyUSD(v: number) {
  if (!v) return '$0.0000'
  return `$${v.toFixed(v < 0.01 ? 5 : 4)}`
}

function moneyTHB(v: number, rate: number) {
  return `฿${(v * rate).toLocaleString(undefined, { maximumFractionDigits: v * rate < 10 ? 2 : 0 })}`
}

function tokens(v: number) {
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`
  if (v >= 1_000) return `${(v / 1_000).toFixed(1)}K`
  return v.toLocaleString()
}

function StatCard({
  icon: Icon,
  label,
  bucket,
  rate,
  tone = 'primary',
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  bucket: Bucket
  rate: number
  tone?: 'primary' | 'success' | 'warning'
}) {
  return (
    <Card className={cn(
      'shadow-none',
      tone === 'success' && 'border-success/25 bg-success/5',
      tone === 'warning' && 'border-warning/25 bg-warning/5',
      tone === 'primary' && 'border-primary/20 bg-primary/[0.03]',
    )}>
      <CardContent className="flex items-center gap-3 p-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-md bg-background">
          <Icon className="h-4 w-4 text-accent-strong" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="mt-0.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="text-lg font-semibold tabular-nums">{moneyTHB(bucket.estimated_cost_usd, rate)}</span>
            <span className="text-xs text-muted-foreground">{moneyUSD(bucket.estimated_cost_usd)}</span>
          </div>
          <div className="mt-0.5 text-[11px] text-muted-foreground">
            {bucket.requests.toLocaleString()} requests · {tokens(bucket.total_tokens)} tokens
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function BucketTable({ rows, rate, kind }: { rows: Bucket[]; rate: number; kind: 'model' | 'feature' }) {
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/40">
            <TableHead>{kind === 'model' ? 'Model' : 'Feature'}</TableHead>
            <TableHead className="text-right">Requests</TableHead>
            <TableHead className="text-right">Tokens</TableHead>
            <TableHead className="text-right">Input / Output</TableHead>
            <TableHead className="text-right">Cost</TableHead>
            <TableHead className="text-right">Errors</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                ยังไม่มี usage
              </TableCell>
            </TableRow>
          ) : rows.map((r) => (
            <TableRow key={r.key}>
              <TableCell className="font-medium">
                {kind === 'feature' ? FEATURE_LABEL[r.key] ?? r.key : <code>{r.key}</code>}
                {kind === 'feature' && <div className="text-xs text-muted-foreground">{r.key}</div>}
              </TableCell>
              <TableCell className="text-right tabular-nums">{r.requests.toLocaleString()}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens(r.total_tokens)}</TableCell>
              <TableCell className="text-right text-xs tabular-nums text-muted-foreground">
                {tokens(r.input_tokens)} / {tokens(r.output_tokens)}
              </TableCell>
              <TableCell className="text-right">
                <div className="font-medium tabular-nums">{moneyTHB(r.estimated_cost_usd, rate)}</div>
                <div className="text-xs text-muted-foreground">{moneyUSD(r.estimated_cost_usd)}</div>
              </TableCell>
              <TableCell className="text-right">
                {r.errors > 0 ? (
                  <Badge variant="destructive">{r.errors}</Badge>
                ) : (
                  <span className="text-xs text-muted-foreground">0</span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

export default function AIUsage() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [logs, setLogs] = useState<UsageLog[]>([])
  const [loading, setLoading] = useState(false)
  const [dateFrom, setDateFrom] = useState(dayjs().startOf('month').format('YYYY-MM-DD'))
  const [dateTo, setDateTo] = useState('')
  const [query, setQuery] = useState('')

  const load = async () => {
    setLoading(true)
    try {
      const params: Record<string, string> = {}
      if (dateFrom) params.date_from = dateFrom
      if (dateTo) params.date_to = dateTo
      const [s, l] = await Promise.all([
        client.get<Summary>('/api/ai-usage/summary', { params }),
        client.get<{ data: UsageLog[] }>('/api/ai-usage/logs', { params: { ...params, page_size: 80 } }),
      ])
      setSummary(s.data)
      setLogs(l.data.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dateFrom, dateTo])

  const rate = summary?.estimated_thb_rate ?? 36
  const filteredLogs = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return logs
    return logs.filter((l) =>
      `${l.model} ${l.feature} ${l.operation} ${sessionID(l) ?? ''} ${l.bill_id ?? ''} ${l.error ?? ''}`.toLowerCase().includes(q),
    )
  }, [logs, query])

  return (
    <div className="space-y-4">
      <PageHeader
        title="การใช้งาน AI"
        description="ดูจำนวนการใช้งาน รุ่น AI งานที่เรียกใช้ และค่าใช้จ่ายประมาณการของ Nexflow"
        actions={
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
            รีเฟรช
          </Button>
        }
      />

      <Card className="shadow-none">
        <CardContent className="flex flex-wrap items-end gap-2 p-3">
          <div className="space-y-1">
            <div className="text-xs text-muted-foreground">ช่วงวันที่</div>
            <DateRangePicker
              from={dateFrom}
              to={dateTo}
              onFromChange={setDateFrom}
              onToChange={setDateTo}
              className="h-8 min-w-[210px] text-xs"
            />
          </div>
          <div className="ml-auto rounded-md bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
            ค่าเงินบาทใช้ประมาณการ {rate} บาท/USD · ต้นทุนเป็นค่าประมาณจาก usage ที่ provider ส่งกลับหรือ token estimate
          </div>
        </CardContent>
      </Card>

      {summary && (
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-4">
          <StatCard icon={Coins} label="วันนี้" bucket={summary.today} rate={rate} tone="success" />
          <StatCard icon={Activity} label="7 วันล่าสุด" bucket={summary.seven_days} rate={rate} />
          <StatCard icon={Zap} label="เดือนนี้" bucket={summary.month} rate={rate} />
          <StatCard icon={AlertTriangle} label="ตามตัวกรอง" bucket={summary.total} rate={rate} tone={summary.total.errors > 0 ? 'warning' : 'primary'} />
        </div>
      )}

      <Tabs defaultValue="model" className="space-y-3">
        <TabsList className="h-9">
          <TabsTrigger value="model" className="text-xs">แยกตาม model</TabsTrigger>
          <TabsTrigger value="feature" className="text-xs">แยกตามงาน</TabsTrigger>
          <TabsTrigger value="logs" className="text-xs">Request logs</TabsTrigger>
        </TabsList>

        <TabsContent value="model">
          <BucketTable rows={summary?.by_model ?? []} rate={rate} kind="model" />
        </TabsContent>

        <TabsContent value="feature">
          <BucketTable rows={summary?.by_feature ?? []} rate={rate} kind="feature" />
        </TabsContent>

        <TabsContent value="logs" className="space-y-2">
          <div className="relative max-w-sm">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="ค้นหา model / feature / session / bill"
              className="h-8 pl-8 text-xs"
            />
          </div>
          <div className="overflow-hidden rounded-lg border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/40">
                  <TableHead>เวลา</TableHead>
                  <TableHead>Feature</TableHead>
                  <TableHead>Model</TableHead>
                  <TableHead className="text-right">Tokens</TableHead>
                  <TableHead className="text-right">Cost</TableHead>
                  <TableHead>Session</TableHead>
                  <TableHead>Bill</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredLogs.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="py-10 text-center text-sm text-muted-foreground">
                      ยังไม่มี request log
                    </TableCell>
                  </TableRow>
                ) : filteredLogs.map((l) => (
                  <TableRow key={l.id}>
                    <TableCell className="text-xs text-muted-foreground">
                      {dayjs(l.created_at).format('DD/MM/YY HH:mm')}
                    </TableCell>
                    <TableCell>
                      <div className="text-sm font-medium">{FEATURE_LABEL[l.feature] ?? l.feature}</div>
                      <div className="text-xs text-muted-foreground">{l.operation}</div>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{l.model}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">
                      {tokens(l.total_tokens)}
                      <div className="text-muted-foreground">{tokens(l.input_tokens)} / {tokens(l.output_tokens)}</div>
                    </TableCell>
                    <TableCell className="text-right text-xs tabular-nums">
                      {moneyTHB(l.estimated_cost_usd, rate)}
                      <div className="text-muted-foreground">{moneyUSD(l.estimated_cost_usd)}</div>
                    </TableCell>
                    <TableCell>
                      <SessionLink log={l} />
                    </TableCell>
                    <TableCell>
                      {l.bill_id ? (
                        <Link className="font-mono text-xs text-link hover:underline" to={`/bills/${l.bill_id}`}>
                          {l.bill_id.slice(0, 8)}…
                        </Link>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={l.status === 'error' ? 'destructive' : 'secondary'} className={l.status === 'success' ? 'bg-success/15 text-success' : undefined}>
                        {l.status === 'success' ? 'สำเร็จ' : 'ผิดพลาด'}
                      </Badge>
                      {l.duration_ms != null && <div className="mt-1 text-[10px] text-muted-foreground">{l.duration_ms}ms</div>}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </TabsContent>
      </Tabs>

      <div className="rounded-md border border-info/25 bg-info/[0.04] px-3 py-2 text-xs text-muted-foreground">
        <Bot className="mr-1 inline h-3.5 w-3.5 text-info" />
        หน้านี้ใช้เพื่อคุมต้นทุนและตรวจว่า AI ถูกใช้กับงานไหนมากที่สุด ค่าใช้จ่ายเป็นประมาณการเพื่อบริหารงานภายใน
      </div>
    </div>
  )
}

function sessionID(log: UsageLog): string {
  const v = log.metadata?.session_id
  return typeof v === 'string' && v.trim() ? v : ''
}

function SessionLink({ log }: { log: UsageLog }) {
  const id = sessionID(log)
  if (!id) return <span className="text-xs text-muted-foreground">—</span>
  const href = `https://openrouter.ai/logs?tab=sessions&session_id=${encodeURIComponent(id)}`
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex max-w-[180px] items-center gap-1 font-mono text-xs text-link hover:underline"
      title={id}
    >
      <span className="truncate">{id}</span>
      <ExternalLink className="h-3 w-3 shrink-0" />
    </a>
  )
}
