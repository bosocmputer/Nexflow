import { useEffect, useMemo, useState, type ComponentType } from 'react'
import {
  AlertTriangle,
  Archive,
  CalendarClock,
  Database,
  HardDrive,
  RefreshCw,
  ScrollText,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'

import api from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PageHeader } from '@/components/common/PageHeader'
import { cn } from '@/lib/utils'

interface Summary {
  archive_days: number
  purge_days: number
  bills: { to_archive: number; to_purge: number; archived: number }
  audit_logs: TableMetrics
  ai_usage_logs: TableMetrics
  chat_messages: TableMetrics
  db_size_mb: number
  policy?: {
    hot_log_days: number
    auto_archive_days: number
    summary_days: number
    purge_mode: string
  }
}

interface TableMetrics {
  to_purge: number
  rows: number
  size_mb: number
  oldest_at?: string | null
}

const emptyTableMetrics: TableMetrics = {
  to_purge: 0,
  rows: 0,
  size_mb: 0,
  oldest_at: null,
}

function metricOrEmpty(metric?: TableMetrics): TableMetrics {
  return metric ?? emptyTableMetrics
}

function numberFormat(value: number): string {
  return value.toLocaleString()
}

function sizeFormat(value: number): string {
  return `${value.toFixed(1)} MB`
}

function dateLabel(value?: string | null): string {
  if (!value) return 'ยังไม่มีข้อมูล'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString('th-TH', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export default function OldDataSettings() {
  const [archiveDays, setArchiveDays] = useState(180)
  const [purgeDays, setPurgeDays] = useState(730)
  const [summary, setSummary] = useState<Summary | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [archiving, setArchiving] = useState(false)
  const [purging, setPurging] = useState(false)

  const [confirmPurge, setConfirmPurge] = useState(false)
  const [purgeBills, setPurgeBills] = useState(false)
  const [purgeAudit, setPurgeAudit] = useState(false)
  const [purgeAI, setPurgeAI] = useState(false)
  const [purgeChat, setPurgeChat] = useState(false)

  const fetchSummary = async () => {
    setLoading(true)
    try {
      const res = await api.get('/api/bills/old-data/summary', {
        params: { archive_days: archiveDays, purge_days: purgeDays },
      })
      setSummary(res.data)
      setLoadError('')
    } catch {
      setLoadError('โหลดข้อมูลสรุปไม่ได้ กรุณาตรวจสิทธิ์ผู้ใช้หรือการเชื่อมต่อ backend')
      toast.error('โหลดข้อมูลไม่ได้')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchSummary() }, [])

  const handleArchive = async () => {
    setArchiving(true)
    try {
      const res = await api.post('/api/bills/old-data/archive', { archive_days: archiveDays })
      toast.success(`เก็บบิลเก่าสำเร็จ ${numberFormat(res.data.archived ?? 0)} รายการ`)
      fetchSummary()
    } catch (e: any) {
      toast.error(e?.response?.data?.error || 'เก็บข้อมูลไม่สำเร็จ')
    } finally {
      setArchiving(false)
    }
  }

  const handlePurge = async () => {
    setPurging(true)
    setConfirmPurge(false)
    try {
      const res = await api.post('/api/bills/old-data/purge', {
        purge_days: purgeDays,
        purge_bills: purgeBills,
        purge_audit: purgeAudit,
        purge_ai: purgeAI,
        purge_chat: purgeChat,
      })
      const parts: string[] = []
      if (res.data.purged_bills != null) parts.push(`บิล ${numberFormat(res.data.purged_bills)} รายการ`)
      if (res.data.purged_audit_logs != null) parts.push(`ประวัติการทำงาน ${numberFormat(res.data.purged_audit_logs)} รายการ`)
      if (res.data.purged_ai_usage_logs != null) parts.push(`ประวัติ AI ${numberFormat(res.data.purged_ai_usage_logs)} รายการ`)
      if (res.data.purged_chat_messages != null) parts.push(`ข้อความแชท ${numberFormat(res.data.purged_chat_messages)} ข้อความ`)
      toast.success(parts.length ? `ลบข้อมูลถาวรแล้ว: ${parts.join(', ')}` : 'ตรวจแล้ว ไม่มีรายการที่ถูกลบ')
      fetchSummary()
    } catch (e: any) {
      toast.error(e?.response?.data?.error || 'ลบข้อมูลไม่สำเร็จ')
    } finally {
      setPurging(false)
    }
  }

  const anyPurgeSelected = purgeBills || purgeAudit || purgeAI || purgeChat
  const summaryReady = !!summary

  const toArchive = summary?.bills?.to_archive ?? 0
  const toPurgeBills = summary?.bills?.to_purge ?? 0
  const archivedCount = summary?.bills?.archived ?? 0
  const auditMetrics = metricOrEmpty(summary?.audit_logs)
  const aiUsageMetrics = metricOrEmpty(summary?.ai_usage_logs)
  const chatMetrics = metricOrEmpty(summary?.chat_messages)
  const logsEligible = auditMetrics.to_purge + aiUsageMetrics.to_purge + chatMetrics.to_purge
  const dbSizeMB = summary?.db_size_mb ?? 0

  const tableRows = useMemo(
    () => [
      {
        key: 'audit',
        label: 'ประวัติการทำงาน',
        description: 'เหตุการณ์ระบบ, การส่ง SML, การตั้งค่า และ error ที่ทีม support ใช้ตรวจย้อนหลัง',
        metric: auditMetrics,
        selected: purgeAudit,
        setSelected: setPurgeAudit,
      },
      {
        key: 'ai',
        label: 'ประวัติการใช้ AI',
        description: 'จำนวน token, รุ่น AI, session และสถานะงาน extract/search',
        metric: aiUsageMetrics,
        selected: purgeAI,
        setSelected: setPurgeAI,
      },
      {
        key: 'chat',
        label: 'ข้อความแชท',
        description: 'ข้อความ LINE OA และ media metadata ที่ใช้ใน inbox',
        metric: chatMetrics,
        selected: purgeChat,
        setSelected: setPurgeChat,
      },
    ],
    [auditMetrics, aiUsageMetrics, chatMetrics, purgeAudit, purgeAI, purgeChat],
  )

  return (
    <div className="mx-auto max-w-7xl space-y-6 p-6">
      <PageHeader
        title="จัดการข้อมูลเก่า"
        description="ดูสุขภาพข้อมูล, เก็บบิลที่ปิดงานแล้ว และลบข้อมูลถาวรอย่างระมัดระวัง"
        actions={
          <Button variant="outline" onClick={fetchSummary} disabled={loading}>
            <RefreshCw className={cn('mr-2 h-4 w-4', loading && 'animate-spin')} />
            รีเฟรช
          </Button>
        }
      />

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          icon={HardDrive}
          label="ขนาดฐานข้อมูล"
          value={summaryReady ? sizeFormat(dbSizeMB) : '—'}
          hint="รวมฐานข้อมูล Nexflow ทั้งหมด"
        />
        <MetricCard
          icon={Archive}
          label="บิลที่เก็บแล้ว"
          value={summaryReady ? numberFormat(archivedCount) : '—'}
          hint="ซ่อนจากคิวงานประจำ แต่ยังค้นย้อนหลังได้"
        />
        <MetricCard
          icon={CalendarClock}
          label="บิลเข้าเงื่อนไขเก็บ"
          value={summaryReady ? numberFormat(toArchive) : '—'}
          hint={`สถานะปิดงาน, ข้าม หรือล้มเหลว เก่ากว่า ${summary?.archive_days ?? archiveDays} วัน`}
          tone={toArchive > 0 ? 'warning' : 'default'}
        />
        <MetricCard
          icon={ScrollText}
          label="Logs เข้าเงื่อนไขลบ"
          value={summaryReady ? numberFormat(logsEligible) : '—'}
          hint={`ข้อมูลละเอียดเก่ากว่า ${summary?.purge_days ?? purgeDays} วัน`}
          tone={logsEligible > 0 ? 'warning' : 'default'}
        />
      </div>

      {loadError && (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          <div className="flex gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <span>{loadError}</span>
          </div>
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm">
              <ShieldCheck className="h-4 w-4 text-primary" />
              นโยบายข้อมูล
            </CardTitle>
            <CardDescription>
              ค่านี้ช่วยให้ระบบเร็วขึ้นโดยเก็บข้อมูลละเอียดไว้เฉพาะช่วงที่ใช้ตรวจงานประจำ
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <PolicyChip
              label="ประวัติละเอียด"
              value={`${summary?.policy?.hot_log_days ?? 90} วัน`}
              hint="หลังจากนี้สรุปเป็นรายวัน"
            />
            <PolicyChip
              label="เก็บบิลอัตโนมัติ"
              value={`${summary?.policy?.auto_archive_days ?? 180} วัน`}
              hint="เฉพาะบิล sent/skipped"
            />
            <PolicyChip
              label="สรุปรายวัน"
              value={`${summary?.policy?.summary_days ?? 730} วัน`}
              hint={`โหมดลบ: ${summary?.policy?.purge_mode ?? 'batch'}`}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm">ช่วงเวลาที่ใช้คำนวณ</CardTitle>
            <CardDescription>ปรับเพื่อดูจำนวนที่เข้าเงื่อนไขก่อนทำงานจริง</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="archive-days">เก็บบิลเก่าเกิน</Label>
                <Input
                  id="archive-days"
                  type="number"
                  min={30}
                  value={archiveDays}
                  onChange={e => setArchiveDays(Number(e.target.value))}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="purge-days">ลบข้อมูลเกิน</Label>
                <Input
                  id="purge-days"
                  type="number"
                  min={90}
                  value={purgeDays}
                  onChange={e => setPurgeDays(Number(e.target.value))}
                />
              </div>
            </div>
            <Button variant="outline" className="w-full" onClick={fetchSummary} disabled={loading}>
              <RefreshCw className={cn('mr-2 h-4 w-4', loading && 'animate-spin')} />
              คำนวณใหม่
            </Button>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="text-sm">ตารางข้อมูลที่ระบบดูแล</CardTitle>
            <CardDescription>
              ใช้ดูจำนวนแถว, ขนาดตาราง และรายการที่เข้าเงื่อนไขลบถาวรก่อนเลือกดำเนินการ
            </CardDescription>
          </div>
          <Badge variant="outline" className="w-fit">
            ไม่เลือกข้อมูลลบโดยอัตโนมัติ
          </Badge>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto rounded-md border">
            <Table className="min-w-[760px]">
              <TableHeader>
                <TableRow className="bg-muted/40 hover:bg-muted/40">
                  <TableHead>ข้อมูล</TableHead>
                  <TableHead className="text-right">จำนวนแถว</TableHead>
                  <TableHead className="text-right">ขนาด</TableHead>
                  <TableHead>เก่าสุด</TableHead>
                  <TableHead className="text-right">เข้าเงื่อนไขลบ</TableHead>
                  <TableHead className="w-[150px]">เลือกเพื่อลบ</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tableRows.map((row) => (
                  <TableRow key={row.key}>
                    <TableCell>
                      <div className="font-medium">{row.label}</div>
                      <div className="mt-1 max-w-[360px] text-xs text-muted-foreground">{row.description}</div>
                    </TableCell>
                    <TableCell className="text-right font-mono">{numberFormat(row.metric.rows)}</TableCell>
                    <TableCell className="text-right font-mono">{sizeFormat(row.metric.size_mb)}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{dateLabel(row.metric.oldest_at)}</TableCell>
                    <TableCell className="text-right">
                      <span className={cn('font-mono font-semibold', row.metric.to_purge > 0 && 'text-warning')}>
                        {numberFormat(row.metric.to_purge)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
                        <Checkbox checked={row.selected} onCheckedChange={v => row.setSelected(!!v)} />
                        ลบถาวร
                      </label>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm">
              <Archive className="h-4 w-4 text-primary" />
              เก็บบิลเก่า
            </CardTitle>
            <CardDescription>
              ซ่อนบิลที่ปิดงานแล้ว, ข้ามแล้ว หรือต้องเก็บออกจากคิวประจำวัน ข้อมูลยังอยู่ครบและกู้คืนได้
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-md border bg-muted/30 p-4 text-sm">
              มีบิลที่เข้าเงื่อนไขเก็บ{' '}
              <span className="font-semibold tabular-nums text-foreground">{numberFormat(toArchive)}</span>{' '}
              รายการ จากช่วงเก่ากว่า{' '}
              <span className="font-semibold">{summary?.archive_days ?? archiveDays}</span> วัน
            </div>
            <Button
              onClick={handleArchive}
              disabled={archiving || !summary || toArchive === 0}
              variant="outline"
            >
              <Archive className={cn('mr-2 h-4 w-4', archiving && 'animate-pulse')} />
              {archiving ? 'กำลังเก็บข้อมูล...' : `เก็บบิลเก่า ${numberFormat(toArchive)} รายการ`}
            </Button>
          </CardContent>
        </Card>

        <Card className="border-destructive/30 bg-destructive/[0.015]">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm text-destructive">
              <Trash2 className="h-4 w-4" />
              ลบข้อมูลถาวร
            </CardTitle>
            <CardDescription>
              ใช้เฉพาะหลังตรวจ backup และยืนยัน scope แล้ว ข้อมูลที่ลบจะย้อนกลับไม่ได้
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-md border border-destructive/25 bg-destructive/5 p-4 text-sm text-destructive">
              <div className="flex gap-2">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                <div>
                  ปุ่มลบจะเปิดได้เมื่อเลือกประเภทข้อมูลเท่านั้น ค่าเริ่มต้นไม่เลือกอะไรเพื่อป้องกันการลบผิด
                </div>
              </div>
            </div>
            <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox checked={purgeBills} onCheckedChange={v => setPurgeBills(!!v)} />
              ลบข้อมูลบิลที่เก่ากว่า {summary?.purge_days ?? purgeDays} วัน
              <span className="text-muted-foreground">({numberFormat(toPurgeBills)} รายการ)</span>
            </label>
            <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
              {purgeBills && <Badge variant="outline">บิล {numberFormat(toPurgeBills)}</Badge>}
              {purgeAudit && <Badge variant="outline">ประวัติการทำงาน {numberFormat(auditMetrics.to_purge)}</Badge>}
              {purgeAI && <Badge variant="outline">ประวัติ AI {numberFormat(aiUsageMetrics.to_purge)}</Badge>}
              {purgeChat && <Badge variant="outline">ข้อความแชท {numberFormat(chatMetrics.to_purge)}</Badge>}
              {!anyPurgeSelected && <span>ยังไม่ได้เลือกข้อมูลที่จะลบ</span>}
            </div>
            <Button
              variant="destructive"
              onClick={() => setConfirmPurge(true)}
              disabled={purging || !anyPurgeSelected}
            >
              <Trash2 className="mr-2 h-4 w-4" />
              {purging ? 'กำลังลบข้อมูล...' : 'ลบข้อมูลถาวร'}
            </Button>
          </CardContent>
        </Card>
      </div>

      <Dialog open={confirmPurge} onOpenChange={setConfirmPurge}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <AlertTriangle className="h-5 w-5" />
              ยืนยันการลบข้อมูลถาวร
            </DialogTitle>
            <DialogDescription className="space-y-2">
              <span className="block">
                ข้อมูลที่เลือกและเก่ากว่า <strong>{purgeDays} วัน</strong> จะถูกลบออกจากฐานข้อมูลถาวร
              </span>
              <span className="block font-medium text-destructive">
                ตรวจ backup และขอบเขตข้อมูลให้เรียบร้อยก่อนดำเนินการ
              </span>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmPurge(false)}>ยกเลิก</Button>
            <Button variant="destructive" onClick={handlePurge}>
              ยืนยัน ลบข้อมูลถาวร
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function MetricCard({
  icon: Icon,
  label,
  value,
  hint,
  tone = 'default',
}: {
  icon: ComponentType<{ className?: string }>
  label: string
  value: string
  hint: string
  tone?: 'default' | 'warning'
}) {
  return (
    <Card className={cn(tone === 'warning' && 'border-warning/40 bg-warning/5')}>
      <CardContent className="flex items-start gap-3 p-4">
        <div className={cn('rounded-md bg-muted p-2', tone === 'warning' && 'bg-warning/15 text-warning')}>
          <Icon className="h-4 w-4" />
        </div>
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="mt-1 text-2xl font-semibold tabular-nums tracking-tight">{value}</div>
          <div className="mt-1 text-xs text-muted-foreground">{hint}</div>
        </div>
      </CardContent>
    </Card>
  )
}

function PolicyChip({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-md border bg-muted/20 p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold tabular-nums">{value}</div>
      <div className="mt-1 text-xs text-muted-foreground">{hint}</div>
    </div>
  )
}
