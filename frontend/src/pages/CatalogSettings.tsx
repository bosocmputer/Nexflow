import { useEffect, useState, useRef, useCallback, useMemo, useReducer } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertCircle,
  AlertTriangle,
  BookOpen,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clipboard,
  Database,
  ExternalLink,
  ImageIcon,
  Info,
  Loader2,
  RefreshCcw,
  RefreshCw,
  RotateCw,
  Search,
  Sparkles,
  Trash2,
  X,
} from 'lucide-react'

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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { AuthImage } from '@/components/common/AuthImage'
import { PageHeader } from '@/components/common/PageHeader'
import { ProductImagePreviewDialog } from '@/components/common/ProductImagePreviewDialog'
import api from '@/api/client'
import { cn } from '@/lib/utils'
import { PAGE_TITLE } from '@/lib/labels'
import { humanizeSMLConnectionError } from '@/lib/sml-readiness'
import type { CatalogItem } from '@/types'

interface CatalogStats {
  total: number
  embedded: number
  pending: number
  error: number
  hidden_code_count?: number
  index_size: number
  embed_running: boolean
  embed_status?: {
    running: boolean
    session_id?: string
    started_at?: string
    finished_at?: string
    total: number
    done: number
    errors: number
    error?: string
  }
  sync_running?: boolean
  sync_status?: {
    running: boolean
    started_at?: string
    finished_at?: string
    count: number
    error?: string
  }
}

interface ListResponse {
  data: CatalogItem[]
  total: number
  page: number
  per_page: number
}

type CatalogPullStatus = 'success' | 'not_found' | 'failed' | 'duplicate'
type HiddenCharKind = 'bom' | 'zero_width' | 'format' | 'combining_mark'

interface CatalogPullResult {
  code: string
  status: CatalogPullStatus
  item?: CatalogItem
  not_found?: boolean
  error?: string
  has_hidden_chars?: boolean
  clean_item_code?: string
  hidden_char_kinds?: HiddenCharKind[]
}

interface CatalogPullResponse {
  summary: {
    total: number
    success: number
    not_found: number
    failed: number
    duplicate: number
  }
  results: CatalogPullResult[]
}

interface HiddenCatalogCodesResponse {
  data: CatalogItem[]
  total: number
  limit: number
  has_more: boolean
}

interface InstanceSettingsStatus {
  pending_restart?: boolean
  pending_restart_settings?: string[]
}

type StatusFilter = '' | 'pending' | 'done' | 'error'
interface FetchParams { page: number; filter: StatusFilter; query: string }

const CATALOG_PULL_LIMIT = 50
const CATALOG_CODE_MAX_LEN = 64
const HIDDEN_ITEM_CODE_RE = /[\uFEFF\u200B\u200C\u200D\u2060\u180E\u0E31\u0E34-\u0E3A\u0E47-\u0E4E]/
const HIDDEN_CODE_DESCRIPTION =
  'รหัสสินค้าที่มีอักขระมองไม่เห็น เช่น zero-width, BOM หรือเครื่องหมายไทยลอย ทำให้ตาเห็นเหมือนรหัสปกติแต่ระบบมองเป็นคนละรหัส ต้องตรวจใน SML master, แก้รหัสให้ตรงกับคำแนะนำ แล้วกดรีเฟรชรายตัวหรือซิงก์ใหม่'
const ZERO_WIDTH_CODEPOINTS = new Set([0x200B, 0x200C, 0x200D, 0x2060, 0x180E])
const THAI_COMBINING_MARK_RE = /[\u0E31\u0E34-\u0E3A\u0E47-\u0E4E]/
const HIDDEN_CHAR_KIND_LABELS: Record<HiddenCharKind, string> = {
  bom: 'มี BOM',
  zero_width: 'มี zero-width character',
  format: 'มี format character ที่มองไม่เห็น',
  combining_mark: 'มีเครื่องหมายไทยลอย/combining mark',
}

function inspectCatalogCode(code: string) {
  const kinds: HiddenCharKind[] = []
  const seen = new Set<HiddenCharKind>()
  let clean = ''
  for (const ch of Array.from(code.trim())) {
    const kind = hiddenKindForChar(ch)
    if (kind) {
      if (!seen.has(kind)) {
        seen.add(kind)
        kinds.push(kind)
      }
      continue
    }
    clean += ch
  }
  return {
    hasHiddenChars: kinds.length > 0,
    cleanItemCode: clean.trim(),
    hiddenCharKinds: kinds,
  }
}

function hiddenKindForChar(ch: string): HiddenCharKind | null {
  const cp = ch.codePointAt(0)
  if (cp === 0xFEFF) return 'bom'
  if (cp != null && ZERO_WIDTH_CODEPOINTS.has(cp)) return 'zero_width'
  if (THAI_COMBINING_MARK_RE.test(ch)) return 'combining_mark'
  if (HIDDEN_ITEM_CODE_RE.test(ch)) return 'format'
  return null
}

function hiddenKindText(kinds?: string[]) {
  const labels = (kinds ?? [])
    .map((kind) => HIDDEN_CHAR_KIND_LABELS[kind as HiddenCharKind] ?? kind)
    .filter(Boolean)
  return labels.length > 0 ? labels.join(', ') : 'มีอักขระมองไม่เห็น'
}

function parseCatalogCodes(input: string) {
  const rawCodes = input.split(/[\s,]+/).map((part) => part.trim()).filter(Boolean)
  const seen = new Set<string>()
  const codes: string[] = []
  const duplicates: string[] = []
  for (const code of rawCodes) {
    if (seen.has(code)) {
      duplicates.push(code)
      continue
    }
    seen.add(code)
    codes.push(code)
  }
  return { codes, duplicates }
}

function formatEmbedProgress(status: NonNullable<CatalogStats['embed_status']>): string | null {
  if (!status.session_id && !status.started_at) return null
  const processed = status.done + status.errors
  const total = status.total || processed
  const parts = [
    total > 0 ? `${processed.toLocaleString()} / ${total.toLocaleString()} รายการ` : `${processed.toLocaleString()} รายการ`,
  ]
  if (status.errors > 0) parts.push(`ผิดพลาด ${status.errors.toLocaleString()}`)
  if (status.running && status.started_at) {
    const elapsedSec = Math.max(1, (Date.now() - new Date(status.started_at).getTime()) / 1000)
    const speed = processed / elapsedSec
    if (speed > 0 && total > processed) {
      const etaSec = Math.ceil((total - processed) / speed)
      parts.push(`ประมาณ ${formatDuration(etaSec)} ที่เหลือ`)
    }
  } else if (status.finished_at) {
    parts.push(`เสร็จล่าสุด ${new Date(status.finished_at).toLocaleTimeString('th-TH', { hour: '2-digit', minute: '2-digit' })}`)
  }
  if (status.error) parts.push(status.error)
  return parts.join(' · ')
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds} วินาที`
  const mins = Math.ceil(seconds / 60)
  if (mins < 60) return `${mins} นาที`
  const hours = Math.floor(mins / 60)
  const rest = mins % 60
  return rest ? `${hours} ชม. ${rest} นาที` : `${hours} ชม.`
}

function Pagination({
  page,
  total,
  perPage,
  onChange,
}: {
  page: number
  total: number
  perPage: number
  onChange: (p: number) => void
}) {
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  if (totalPages <= 1) return null

  const pages: (number | '…')[] = []
  if (totalPages <= 7) {
    for (let i = 1; i <= totalPages; i++) pages.push(i)
  } else {
    pages.push(1)
    if (page > 3) pages.push('…')
    for (let i = Math.max(2, page - 1); i <= Math.min(totalPages - 1, page + 1); i++) pages.push(i)
    if (page < totalPages - 2) pages.push('…')
    pages.push(totalPages)
  }

  return (
    <div className="flex items-center gap-1">
      <Button
        size="icon"
        variant="outline"
        className="h-7 w-7"
        disabled={page <= 1}
        onClick={() => onChange(page - 1)}
        aria-label="หน้าก่อน"
      >
        <ChevronLeft className="h-3.5 w-3.5" />
      </Button>
      {pages.map((p, i) =>
        p === '…' ? (
          <span key={`e${i}`} className="px-1 text-xs text-muted-foreground">
            …
          </span>
        ) : (
          <Button
            key={p}
            size="sm"
            variant={page === p ? 'default' : 'ghost'}
            className="h-7 min-w-[28px] px-2 text-xs"
            onClick={() => onChange(p as number)}
          >
            {p}
          </Button>
        ),
      )}
      <Button
        size="icon"
        variant="outline"
        className="h-7 w-7"
        disabled={page >= totalPages}
        onClick={() => onChange(page + 1)}
        aria-label="หน้าถัดไป"
      >
        <ChevronRight className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

function StatChip({
  label,
  value,
  variant = 'muted',
  tooltip,
}: {
  label: string
  value: number | string
  variant?: 'success' | 'warning' | 'danger' | 'primary' | 'muted'
  tooltip?: string
}) {
  const styles: Record<typeof variant, string> = {
    success: 'border-success/25 bg-success/5 text-success',
    warning: 'border-warning/25 bg-warning/5 text-warning',
    danger: 'border-destructive/25 bg-destructive/5 text-destructive',
    primary: 'border-primary/25 bg-primary/5 text-accent-strong',
    muted: 'border-border bg-card text-foreground',
  }
  const chip = (
    <Card className={cn('min-w-[145px] flex-1 shadow-none', styles[variant])}>
      <CardContent className="flex items-center justify-between gap-3 px-3 py-2">
        <p className="text-base font-semibold tabular-nums leading-none">{value}</p>
        <p className="inline-flex items-center gap-1 text-[11px] font-medium text-muted-foreground">
          {label}
          {tooltip && <Info className="h-3 w-3" />}
        </p>
      </CardContent>
    </Card>
  )
  if (!tooltip) return chip
  return (
    <TooltipProvider delayDuration={120}>
      <Tooltip>
        <TooltipTrigger asChild>{chip}</TooltipTrigger>
        <TooltipContent className="max-w-xs text-xs leading-relaxed">
          {tooltip}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function HiddenCodeStatCard({
  count,
  onInspect,
}: {
  count: number
  onInspect: () => void
}) {
  return (
    <Card className="min-w-[190px] flex-1 border-destructive/25 bg-destructive/5 text-destructive shadow-none">
      <CardContent className="flex items-center justify-between gap-3 px-3 py-2.5">
        <div className="min-w-0">
          <p className="text-lg font-semibold tabular-nums leading-none">{count.toLocaleString()}</p>
          <p className="mt-1 inline-flex items-center gap-1 text-[11px] font-medium text-muted-foreground">
            รหัสซ่อน
            <TooltipProvider delayDuration={120}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3 w-3 cursor-help" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs text-xs leading-relaxed">
                  {HIDDEN_CODE_DESCRIPTION}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </p>
        </div>
        <Button type="button" size="sm" variant="outline" className="h-8 shrink-0 px-2 text-xs" onClick={onInspect}>
          ดูรายการ
        </Button>
      </CardContent>
    </Card>
  )
}

function CatalogThumbnail({
  item,
  onPreview,
}: {
  item: CatalogItem
  onPreview: (item: CatalogItem) => void
}) {
  const count = item.image_count ?? 0
  const hasImage = Boolean(item.image_url && count > 0)
  const image = (
    <AuthImage
      src={hasImage ? item.image_url : undefined}
      className="h-full w-full rounded-md border border-border bg-muted/35"
      imgClassName="object-cover"
      fallback={
        <div className="flex h-full w-full items-center justify-center text-muted-foreground">
          <ImageIcon className="h-4 w-4" />
        </div>
      }
    >
      {count > 1 && (
        <span className="absolute bottom-0.5 right-0.5 rounded bg-background/90 px-1 text-[10px] font-medium tabular-nums text-foreground shadow-sm">
          {count}
        </span>
      )}
    </AuthImage>
  )

  if (!hasImage) {
    return <div className="h-11 w-11 shrink-0">{image}</div>
  }

  return (
    <button
      type="button"
      className="h-11 w-11 shrink-0 rounded-md outline-none ring-offset-background transition-transform hover:scale-[1.03] focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
      onClick={() => onPreview(item)}
      aria-label={`ดูรูป ${item.item_code}`}
    >
      {image}
    </button>
  )
}

function CatalogPullDialog({
  open,
  onOpenChange,
  onPulled,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onPulled: (firstCode: string) => Promise<void>
}) {
  const [input, setInput] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [results, setResults] = useState<CatalogPullResult[]>([])
  const [error, setError] = useState('')
  const { codes, duplicates } = useMemo(() => parseCatalogCodes(input), [input])
  const hiddenCodes = useMemo(
    () => codes
      .map((code) => ({ code, ...inspectCatalogCode(code) }))
      .filter((row) => row.hasHiddenChars),
    [codes],
  )
  const invalidLengthCodes = useMemo(
    () => codes.filter((code) => Array.from(code).length > CATALOG_CODE_MAX_LEN),
    [codes],
  )
  const resultCounts = useMemo(() => {
    return results.reduce(
      (acc, row) => {
        acc[row.status] += 1
        return acc
      },
      { success: 0, not_found: 0, failed: 0, duplicate: 0 } satisfies Record<CatalogPullStatus, number>,
    )
  }, [results])
  const problemCodes = results
    .filter((row) => row.status === 'not_found' || row.status === 'failed')
    .map((row) => row.code)
  const canSubmit =
    codes.length > 0 &&
    codes.length <= CATALOG_PULL_LIMIT &&
    invalidLengthCodes.length === 0 &&
    !submitting

  async function copyProblemCodes() {
    if (problemCodes.length === 0) return
    try {
      await navigator.clipboard.writeText(problemCodes.join('\n'))
    } catch {
      setError('คัดลอกรหัสไม่สำเร็จ')
    }
  }

  async function copyCleanCode(code: string) {
    try {
      await navigator.clipboard.writeText(code)
    } catch {
      setError('คัดลอกรหัสไม่สำเร็จ')
    }
  }

  async function handleSubmit() {
    if (!canSubmit) return
    setSubmitting(true)
    setError('')
    const duplicateResults: CatalogPullResult[] = duplicates.map((code) => {
      const meta = inspectCatalogCode(code)
      return {
        code,
        status: 'duplicate',
        has_hidden_chars: meta.hasHiddenChars,
        clean_item_code: meta.cleanItemCode,
        hidden_char_kinds: meta.hiddenCharKinds,
      }
    })
    try {
      const res = await api.post<CatalogPullResponse>('/api/catalog/refresh-batch', { codes })
      const nextResults = [...duplicateResults, ...(res.data.results ?? [])]
      setResults(nextResults)
      const firstSuccess = nextResults.find((row) => row.status === 'success')
      const firstCode = firstSuccess?.item?.item_code || firstSuccess?.code
      if (firstCode) {
        await onPulled(firstCode)
      }
    } catch (err: unknown) {
      const e = err as { response?: { data?: { error?: string } } }
      setError(humanizeSMLConnectionError(e?.response?.data?.error ?? 'ดึงสินค้าจาก SML ไม่สำเร็จ'))
    } finally {
      setSubmitting(false)
    }
  }

  function resetAndClose(nextOpen: boolean) {
    if (!nextOpen && submitting) return
    onOpenChange(nextOpen)
    if (!nextOpen) {
      setError('')
      setResults([])
    }
  }

  return (
    <Dialog open={open} onOpenChange={resetAndClose}>
      <DialogContent className="grid max-h-[90vh] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>ดึงรายตัวจาก SML</DialogTitle>
        </DialogHeader>

        <div className="-mx-6 space-y-4 overflow-y-auto px-6 py-2">
          <div className="rounded-md border border-info/25 bg-info/[0.04] px-3 py-2 text-xs text-muted-foreground">
            กรอกรหัสสินค้า SML ที่มีอยู่แล้วได้หลายรหัส ระบบจะดึงเฉพาะรหัสที่ระบุ ไม่ซิงก์สินค้าทั้งหมด และไม่สร้างข้อมูลจับคู่อัตโนมัติ
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium text-foreground">รหัสสินค้า SML</label>
            <Textarea
              value={input}
              onChange={(e) => {
                setInput(e.target.value)
                setError('')
                setResults([])
              }}
              placeholder={'ITEM001\nITEM002\nITEM003'}
              className="min-h-40 font-mono text-sm"
              disabled={submitting}
            />
            <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
              <span>{codes.length.toLocaleString()} / {CATALOG_PULL_LIMIT} รหัสที่จะดึง</span>
              {duplicates.length > 0 && <span>ซ้ำใน input {duplicates.length.toLocaleString()} รหัส</span>}
              <span>คั่นด้วยบรรทัด, comma, space หรือ tab ได้</span>
            </div>
          </div>

          {codes.length > CATALOG_PULL_LIMIT && (
            <div className="rounded-md border border-destructive/30 bg-destructive/[0.06] px-3 py-2 text-xs text-destructive">
              ดึงสินค้าได้สูงสุด {CATALOG_PULL_LIMIT} รหัสต่อครั้ง กรุณาแบ่งเป็นหลายรอบ
            </div>
          )}
          {invalidLengthCodes.length > 0 && (
            <div className="rounded-md border border-destructive/30 bg-destructive/[0.06] px-3 py-2 text-xs text-destructive">
              มีรหัสยาวเกิน {CATALOG_CODE_MAX_LEN} ตัวอักษร: <span className="font-mono">{invalidLengthCodes.slice(0, 3).join(', ')}</span>
            </div>
          )}
          {hiddenCodes.length > 0 && (
            <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs text-warning">
              <div className="font-medium">พบอักขระซ่อนในรหัสที่กรอก</div>
              <div className="mt-1 space-y-1">
                {hiddenCodes.slice(0, 5).map((row) => (
                  <div key={row.code} className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <code className="font-mono">{row.code}</code>
                    <span>{hiddenKindText(row.hiddenCharKinds)}</span>
                    {row.cleanItemCode && (
                      <>
                        <span className="text-muted-foreground">รหัสที่ควรเป็น <code className="font-mono">{row.cleanItemCode}</code></span>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          className="h-6 px-2 text-[11px]"
                          onClick={() => copyCleanCode(row.cleanItemCode)}
                        >
                          <Clipboard className="mr-1 h-3 w-3" />
                          คัดลอก
                        </Button>
                      </>
                    )}
                  </div>
                ))}
                {hiddenCodes.length > 5 && <div>และอีก {hiddenCodes.length - 5} รหัส</div>}
              </div>
              <div className="mt-2 text-muted-foreground">
                สิ่งที่ต้องทำ: ตรวจรหัสนี้ใน SML master และแก้ให้ตรงกับรหัสที่แนะนำก่อนรีเฟรชหรือซิงก์ใหม่
              </div>
            </div>
          )}

          {error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/[0.06] px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          )}

          {results.length > 0 && (
            <div className="overflow-hidden rounded-md border border-border">
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-muted/20 px-3 py-2 text-xs">
                <div>
                  <div className="font-medium text-foreground">ผลการดึงจาก SML</div>
                  <div className="mt-0.5 text-muted-foreground">
                    สำเร็จ {resultCounts.success} · ไม่พบ {resultCounts.not_found} · ล้มเหลว {resultCounts.failed} · ซ้ำ {resultCounts.duplicate}
                  </div>
                </div>
                {problemCodes.length > 0 && (
                  <Button type="button" size="sm" variant="outline" className="h-8 gap-1.5" onClick={copyProblemCodes}>
                    <Clipboard className="h-3.5 w-3.5" />
                    คัดลอกไม่พบ/ล้มเหลว
                  </Button>
                )}
              </div>
              <div className="max-h-72 overflow-y-auto divide-y divide-border">
                {results.map((row, index) => (
                  <div key={`${row.code}-${row.status}-${index}`} className="grid gap-2 px-3 py-2 text-xs sm:grid-cols-[140px_110px_minmax(0,1fr)] sm:items-center">
                    <div className="font-mono font-medium text-foreground">{row.code}</div>
                    <CatalogPullStatusBadge status={row.status} />
                    <div className="min-w-0 text-muted-foreground">
                      {row.status === 'success' && (
                        <span className="line-clamp-1">{row.item?.item_name || 'ดึงสำเร็จ'}</span>
                      )}
                      {row.status === 'not_found' && 'ไม่พบรหัสนี้ใน SML master กรุณาตรวจรหัสหรือเพิ่มสินค้าใน SML ก่อน'}
                      {row.status === 'failed' && (row.error || 'ดึงไม่สำเร็จ')}
                      {row.status === 'duplicate' && 'ซ้ำใน input ระบบข้ามให้แล้ว'}
                      {row.has_hidden_chars && row.clean_item_code && (
                        <span className="ml-1">
                          · {hiddenKindText(row.hidden_char_kinds)} · รหัสที่ควรเป็น <code className="font-mono">{row.clean_item_code}</code>
                        </span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {resultCounts.success > 0 && (
            <div className="rounded-md border border-success/30 bg-success/[0.06] px-3 py-2 text-xs text-success">
              ดึงสินค้าเรียบร้อยแล้ว ถ้าต้องการให้ระบบจับคู่จากชื่อสินค้าแบบ semantic matching ให้กด “สร้างข้อมูลจับคู่”
            </div>
          )}
        </div>

        <DialogFooter className="gap-2 sm:justify-between">
          <Button type="button" variant="ghost" onClick={() => resetAndClose(false)} disabled={submitting}>
            ปิด
          </Button>
          <Button type="button" onClick={handleSubmit} disabled={!canSubmit} className="gap-1.5">
            {submitting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            ดึงรายตัวจาก SML {codes.length > 0 ? `${codes.length} รหัส` : ''}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CatalogPullStatusBadge({ status }: { status: CatalogPullStatus }) {
  if (status === 'success') {
    return <span className="inline-flex items-center gap-1 text-success"><CheckCircle2 className="h-3.5 w-3.5" />สำเร็จ</span>
  }
  if (status === 'not_found') {
    return <span className="inline-flex items-center gap-1 text-warning"><AlertTriangle className="h-3.5 w-3.5" />ไม่พบ</span>
  }
  if (status === 'duplicate') {
    return <span className="text-muted-foreground">ซ้ำ</span>
  }
  return <span className="inline-flex items-center gap-1 text-destructive"><AlertCircle className="h-3.5 w-3.5" />ล้มเหลว</span>
}

function HiddenCodesDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [data, setData] = useState<HiddenCatalogCodesResponse | null>(null)

  const fetchHiddenCodes = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const res = await api.get<HiddenCatalogCodesResponse>('/api/catalog/hidden-codes', {
        params: { limit: 200 },
      })
      setData(res.data)
    } catch (err: unknown) {
      const e = err as { response?: { data?: { error?: string } } }
      setError(e?.response?.data?.error ?? 'โหลดรายการรหัสซ่อนไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) fetchHiddenCodes()
  }, [open, fetchHiddenCodes])

  async function copyCleanCode(code: string) {
    try {
      await navigator.clipboard.writeText(code)
    } catch {
      setError('คัดลอกรหัสไม่สำเร็จ')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[90vh] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>รายการรหัสซ่อนใน Catalog</DialogTitle>
        </DialogHeader>

        <div className="-mx-6 space-y-3 overflow-y-auto px-6 py-2">
          <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs leading-relaxed text-muted-foreground">
            รหัสเหล่านี้มาจาก SML master และมีอักขระที่มองไม่เห็น Nexflow ไม่แก้ master data ให้อัตโนมัติ ให้ตรวจรหัสใน SML, แก้ให้ตรงกับรหัสที่แนะนำ แล้วกดรีเฟรชรายตัวหรือซิงก์ใหม่
          </div>

          {error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/[0.06] px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          )}

          {loading ? (
            <div className="py-10 text-center text-sm text-muted-foreground">
              <Loader2 className="mx-auto mb-2 h-5 w-5 animate-spin" />
              กำลังโหลดรายการ…
            </div>
          ) : data && data.data.length === 0 ? (
            <div className="py-10 text-center text-sm text-muted-foreground">ไม่พบรหัสซ่อนใน Catalog</div>
          ) : data ? (
            <div className="overflow-hidden rounded-md border border-border">
              <div className="border-b border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                พบทั้งหมด {data.total.toLocaleString()} รายการ · แสดง {data.data.length.toLocaleString()} รายการ
                {data.has_more && ' · มีมากกว่านี้ กรุณาแก้เป็นรอบ'}
              </div>
              <div className="max-h-[52vh] divide-y divide-border overflow-y-auto">
                {data.data.map((item) => {
                  const fallbackMeta = item.hidden_char_kinds?.length ? null : inspectCatalogCode(item.item_code)
                  const hiddenKinds = item.hidden_char_kinds?.length ? item.hidden_char_kinds : fallbackMeta?.hiddenCharKinds
                  const cleanCode = item.clean_item_code || fallbackMeta?.cleanItemCode || ''
                  return (
                    <div key={item.item_code} className="grid gap-2 px-3 py-3 text-xs sm:grid-cols-[150px_minmax(0,1fr)_190px]">
                      <div className="min-w-0">
                        <div className="break-all font-mono font-medium text-foreground">{item.item_code}</div>
                        <div className="mt-1 text-warning">{hiddenKindText(hiddenKinds)}</div>
                      </div>
                      <div className="min-w-0 text-muted-foreground">
                        <div className="line-clamp-1 text-foreground">{item.item_name || 'ไม่มีชื่อสินค้า'}</div>
                        {cleanCode && (
                          <div className="mt-1">
                            รหัสที่ควรเป็น <code className="font-mono text-foreground">{cleanCode}</code>
                          </div>
                        )}
                        <div className="mt-1">ตรวจใน SML ก่อนแก้ เพราะรหัสที่แนะนำอาจชนกับ master อื่น</div>
                      </div>
                      <div className="flex items-start justify-end gap-1">
                        {cleanCode && (
                          <Button type="button" size="sm" variant="outline" className="h-8 gap-1.5 px-2 text-xs" onClick={() => copyCleanCode(cleanCode)}>
                            <Clipboard className="h-3.5 w-3.5" />
                            คัดลอกรหัสที่ควรเป็น
                          </Button>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          ) : null}
        </div>

        <DialogFooter className="gap-2 sm:justify-between">
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            ปิด
          </Button>
          <Button type="button" variant="outline" className="gap-1.5" onClick={fetchHiddenCodes} disabled={loading}>
            {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            โหลดรายการใหม่
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default function CatalogSettings() {
  const [stats, setStats] = useState<CatalogStats | null>(null)
  const [items, setItems] = useState<CatalogItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [embedding, setEmbedding] = useState(false)
  const [message, setMessage] = useState<{ text: string; ok: boolean } | null>(null)
  const [pendingRestart, setPendingRestart] = useState(false)
  const [pendingRestartKeys, setPendingRestartKeys] = useState<string[]>([])
  const [draft, setDraft] = useState('')
  const [pullDialogOpen, setPullDialogOpen] = useState(false)
  const [hiddenCodesOpen, setHiddenCodesOpen] = useState(false)
  const [syncConfirmOpen, setSyncConfirmOpen] = useState(false)
  const [embedConfirmOpen, setEmbedConfirmOpen] = useState(false)
  const [params, setParams] = useReducer(
    (_prev: FetchParams, next: Partial<FetchParams> & { reset?: boolean }) => {
      const base = next.reset ? { page: 1, filter: '' as StatusFilter, query: '' } : _prev
      return {
        ...base,
        page: next.page ?? 1,
        filter: next.filter ?? base.filter,
        query: next.query ?? base.query,
      }
    },
    { page: 1, filter: '', query: '' },
  )
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const PER_PAGE = 50

  const fetchStats = useCallback(async () => {
    const res = await api.get<CatalogStats>('/api/catalog/stats')
    setStats(res.data)
    return res.data
  }, [])

  const fetchInstanceStatus = useCallback(async () => {
    try {
      const res = await api.get<InstanceSettingsStatus>('/api/settings/instance')
      const pending = !!res.data.pending_restart
      setPendingRestart(pending)
      setPendingRestartKeys(res.data.pending_restart_settings ?? [])
      return pending
    } catch {
      setPendingRestart(false)
      setPendingRestartKeys([])
      return false
    }
  }, [])

  const fetchItems = useCallback(async (p: FetchParams) => {
    setLoading(true)
    try {
      const reqParams: Record<string, unknown> = { page: p.page, per_page: PER_PAGE }
      if (p.filter) reqParams.status = p.filter
      if (p.query.trim()) reqParams.q = p.query.trim()
      const res = await api.get<ListResponse>('/api/catalog', { params: reqParams })
      setItems(res.data.data ?? [])
      setTotal(res.data.total ?? 0)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchItems(params)
  }, [params, fetchItems])

  useEffect(() => {
    fetchStats()
    fetchInstanceStatus()
  }, [fetchStats, fetchInstanceStatus])

  useEffect(() => {
    if (stats?.embed_running || stats?.sync_running) {
      pollRef.current = setInterval(async () => {
        const s = await fetchStats()
        if (!s.embed_running && !s.sync_running) {
          if (pollRef.current) clearInterval(pollRef.current)
          fetchItems(params)
          if (s.sync_status?.error) {
            notify(`Sync ล้มเหลว: ${humanizeSMLConnectionError(s.sync_status.error)}`, false)
          } else if (stats?.sync_running && s.sync_status?.count) {
            notify(`Sync สำเร็จ ${s.sync_status.count} รายการ`)
          }
        }
      }, 3000)
    } else {
      if (pollRef.current) clearInterval(pollRef.current)
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stats?.embed_running, stats?.sync_running])

  function notify(text: string, ok = true) {
    setMessage({ text, ok })
    setTimeout(() => setMessage(null), 4000)
  }

  function handleFilterChange(f: StatusFilter) {
    setDraft('')
    setParams({ filter: f, page: 1, query: '' })
  }

  function commitSearch(q: string) {
    setParams({ query: q.trim(), page: 1 })
  }

  function handleSearchKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') commitSearch(draft)
    if (e.key === 'Escape') {
      setDraft('')
      commitSearch('')
    }
  }

  async function handleSync() {
    const hasPendingRestart = await fetchInstanceStatus()
    if (hasPendingRestart) {
      notify('มีการเปลี่ยนค่า SML ที่ยังไม่ได้รีสตาร์ท กรุณาไปที่การเชื่อมต่อระบบก่อน Sync', false)
      return
    }
    setSyncing(true)
    try {
      const res = await api.post<{ message?: string; sync_running?: boolean }>('/api/catalog/sync')
      notify(res.data.message === 'catalog sync already running' ? 'กำลัง Sync สินค้าอยู่แล้ว' : 'เริ่ม Sync สินค้าแล้ว')
      fetchStats()
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { error?: string; pending_restart?: boolean; pending_restart_settings?: string[] } } })?.response?.data
      if (msg?.pending_restart) {
        setPendingRestart(true)
        setPendingRestartKeys(msg.pending_restart_settings ?? [])
      }
      notify(humanizeSMLConnectionError(msg?.error ?? 'Sync ล้มเหลว'), false)
    } finally {
      setSyncing(false)
    }
  }

  async function handleEmbedAll() {
    setEmbedding(true)
    try {
      const res = await api.post<{ message: string }>('/api/catalog/embed-all')
      notify(res.data.message ?? 'เริ่ม embed แล้ว')
      fetchStats()
    } catch {
      notify('Embed ล้มเหลว', false)
    } finally {
      setEmbedding(false)
    }
  }

  async function handleReload() {
    try {
      await api.post('/api/catalog/reload-index')
      notify('Reload index สำเร็จ')
      fetchStats()
    } catch {
      notify('Reload ล้มเหลว', false)
    }
  }

  async function handleEmbedOne(code: string) {
    try {
      await api.post(`/api/catalog/${code}/embed`)
      notify(`Embed ${code} สำเร็จ`)
      fetchStats()
      fetchItems(params)
    } catch {
      notify(`Embed ${code} ล้มเหลว`, false)
    }
  }

  // Tracks which row is currently running an action so we can disable
  // its buttons and show a spinner without blocking the rest of the table.
  const [busyRow, setBusyRow] = useState<{ code: string; action: 'refresh' | 'delete' } | null>(null)
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [previewItem, setPreviewItem] = useState<CatalogItem | null>(null)

  async function handleRefreshOne(code: string) {
    setBusyRow({ code, action: 'refresh' })
    try {
      await api.post(`/api/catalog/${code}/refresh`)
      notify(`รีเฟรช ${code} จาก SML สำเร็จ`)
      fetchItems(params)
    } catch (err: unknown) {
      const e = err as { response?: { status?: number; data?: { error?: string; not_found?: boolean } } }
      if (e?.response?.data?.not_found) {
        notify(`ไม่พบ ${code} ใน SML — ลบจาก Nexflow ได้`, false)
      } else {
        notify(humanizeSMLConnectionError(e?.response?.data?.error ?? `รีเฟรช ${code} ล้มเหลว`), false)
      }
    } finally {
      setBusyRow(null)
    }
  }

  async function handleCatalogPulled(firstCode: string) {
    const nextParams = { page: 1, filter: params.filter, query: firstCode }
    notify(`ดึง ${firstCode} จาก SML สำเร็จ`)
    setDraft(firstCode)
    setParams(nextParams)
    await fetchStats()
    await fetchItems(nextParams)
  }

  async function handleDeleteOne(code: string) {
    setBusyRow({ code, action: 'delete' })
    try {
      await api.delete(`/api/catalog/${code}`)
      notify(`ลบ ${code} จาก Nexflow แล้ว (SML ไม่ถูกแตะ)`)
      fetchStats()
      fetchItems(params)
    } catch (err: unknown) {
      const e = err as { response?: { data?: { error?: string } } }
      notify(e?.response?.data?.error ?? `ลบ ${code} ล้มเหลว`, false)
    } finally {
      setBusyRow(null)
      setPendingDelete(null)
    }
  }

  const pct = useMemo(
    () => (stats && stats.total > 0 ? Math.round((stats.embedded / stats.total) * 100) : 0),
    [stats],
  )

  const isEmbedBusy = embedding || (stats?.embed_running ?? false)
  const isSyncBusy = syncing || (stats?.sync_running ?? false)
  const embedProgress = stats?.embed_status ? formatEmbedProgress(stats.embed_status) : null

  const tabs: Array<{ key: StatusFilter; label: string; count?: number }> = [
    { key: '', label: 'ทั้งหมด', count: stats?.total },
    { key: 'done', label: 'พร้อมจับคู่', count: stats?.embedded },
    { key: 'pending', label: 'รอเตรียมข้อมูล', count: stats?.pending },
    { key: 'error', label: 'มีปัญหา', count: stats?.error },
  ]

  return (
    <div className="space-y-4">
      <PageHeader
        title={PAGE_TITLE.catalog}
        description="รายการสินค้าจาก SML ที่ Nexflow ใช้จับคู่กับชื่อสินค้าจาก Email และ Marketplace"
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setSyncConfirmOpen(true)}
              disabled={isSyncBusy || pendingRestart}
              title={pendingRestart ? 'มีการเปลี่ยนค่า SML ที่ยังไม่ได้รีสตาร์ท' : 'ดึงสินค้า master ทั้งหมดจาก SML'}
            >
              <RotateCw className={cn('h-3.5 w-3.5', isSyncBusy && 'animate-spin')} />
              {pendingRestart ? 'รอรีสตาร์ท SML' : isSyncBusy ? 'กำลังซิงก์ทั้งหมด…' : 'ซิงก์ทั้งหมดจาก SML'}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="gap-1.5"
              onClick={() => setPullDialogOpen(true)}
              disabled={pendingRestart || isSyncBusy}
              title={
                pendingRestart
                  ? 'มีการเปลี่ยนค่า SML ที่ยังไม่ได้รีสตาร์ท'
                  : isSyncBusy
                    ? 'กำลังซิงก์ทั้งหมดอยู่ กรุณารอให้เสร็จก่อน'
                    : 'ดึงสินค้าใหม่จาก SML แบบระบุรหัส'
              }
            >
              <Database className="h-3.5 w-3.5" />
              ดึงรายตัวจาก SML
            </Button>
            <Button
              size="sm"
              onClick={() => setEmbedConfirmOpen(true)}
              disabled={isEmbedBusy || (stats?.total ?? 0) === 0}
              title={
                isEmbedBusy
                  ? 'กำลังเตรียมข้อมูลจับคู่อยู่'
                  : (stats?.total ?? 0) === 0
                    ? 'ยังไม่มีสินค้าใน Catalog ให้เตรียมข้อมูล'
                    : 'สร้างข้อมูลจับคู่จาก catalog ปัจจุบัน'
              }
            >
              {isEmbedBusy ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Sparkles className="h-3.5 w-3.5" />
              )}
              {isEmbedBusy ? 'กำลังเตรียมข้อมูล…' : 'สร้างข้อมูลจับคู่'}
            </Button>
            <Button variant="outline" size="sm" onClick={handleReload}>
              <RefreshCcw className="h-3.5 w-3.5" />
              โหลดรายการใหม่
            </Button>
          </>
        }
      />

      {pendingRestart && (
        <div className="rounded-lg border border-warning/35 bg-warning/[0.07] p-3 text-sm">
          <div className="flex items-start gap-2.5">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
            <div className="min-w-0 flex-1">
              <p className="font-medium text-foreground">ยัง Sync สินค้าไม่ได้ เพราะมีค่า SML ที่รอรีสตาร์ท</p>
              <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                ไปที่ <Link to="/settings/instance" className="font-medium text-link hover:underline">การเชื่อมต่อระบบ</Link> แล้วกด “รีสตาร์ทและใช้ค่าทันที” ก่อน เพื่อให้ Nexflow ใช้ headers ชุดล่าสุด
              </p>
              {pendingRestartKeys.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {pendingRestartKeys.map((key) => (
                    <Badge key={key} variant="outline" className="h-5 px-1.5 text-[10px]">
                      {key}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {stats?.sync_running && (
        <div className="rounded-lg border border-primary/25 bg-primary/[0.06] p-3 text-sm">
          <div className="flex items-start gap-2.5">
            <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-accent-strong" />
            <div>
              <p className="font-medium text-foreground">กำลัง Sync สินค้าจาก SML</p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                งานนี้อาจใช้เวลาหลายนาที ปิดหน้านี้ได้ ระบบจะทำต่อบน server
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Catalog vs Mappings explainer — without this admins assume the two
          features are the same. Catalog is the master + smart-match
          backend; Mappings is the human-curated alias table. */}
      <details className="group rounded-lg border border-info/25 bg-info/[0.035] text-sm">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-3.5 py-2.5">
          <span className="inline-flex min-w-0 items-center gap-2.5">
            <BookOpen className="h-4 w-4 shrink-0 text-info" strokeWidth={2.25} />
            <span className="font-medium text-foreground">Catalog คือฐานสินค้า SML สำหรับจับคู่ชื่อสินค้าจาก Email และ Marketplace</span>
          </span>
          <span className="text-[11px] text-link group-open:hidden">รายละเอียด</span>
          <span className="hidden text-[11px] text-muted-foreground group-open:inline">ย่อ</span>
        </summary>
        <div className="border-t border-info/15 px-3.5 py-3">
          <div className="flex items-start gap-2.5">
          <BookOpen className="mt-0.5 h-4 w-4 shrink-0 text-info" strokeWidth={2.25} />
          <div className="min-w-0 flex-1 space-y-1.5">
            <p className="text-[13px] leading-relaxed text-muted-foreground">
              <span className="font-medium text-foreground">รายการสินค้าจาก SML</span>
              ที่ Nexflow ใช้เทียบกับชื่อสินค้าจาก Email, Shopee, Lazada และ TikTok แล้วแนะนำรหัสสินค้าในหน้าบิล
            </p>
            <div className="text-[12px] leading-relaxed text-muted-foreground">
              <span className="font-medium text-foreground">เริ่มใช้งานครั้งแรก:</span>
              <span className="ml-1">
                ① กด <span className="font-medium text-foreground">ซิงก์สินค้า</span> → ② กด{' '}
                <span className="font-medium text-foreground">สร้างข้อมูลจับคู่</span> (อาจใช้เวลาหลายนาที)
              </span>
            </div>
            <p className="text-[12px] text-muted-foreground">
              ต่างจาก{' '}
              <Link to="/mappings" className="font-medium text-link hover:underline">
                ตารางจับคู่สินค้า
              </Link>{' '}
              ที่เก็บชื่อสินค้าที่เคยแก้แล้ว — ใช้คู่กันแต่คนละขั้นตอน
            </p>
          </div>
        </div>
        </div>
      </details>

      <div className="rounded-lg border border-info/20 bg-info/[0.035] px-3.5 py-2.5 text-xs leading-relaxed text-muted-foreground">
        ถ้าระบบถูก deploy หรือ restart ระหว่างสร้างข้อมูลจับคู่ งานที่ทำแล้วจะไม่หาย และ backend จะเริ่มทำต่อจากรายการที่ยังรออยู่ให้อัตโนมัติหลังกลับมาออนไลน์
      </div>

      {message && (
        <div
          className={cn(
            'fixed right-4 top-4 z-50 flex items-center gap-2 rounded-md border px-4 py-2.5 text-sm font-medium shadow-md',
            message.ok
              ? 'border-success/30 bg-success/10 text-success'
              : 'border-destructive/30 bg-destructive/10 text-destructive',
          )}
        >
          {message.ok ? '✓' : <AlertCircle className="h-4 w-4" />}
          {message.text}
        </div>
      )}

      {stats && (
        <div className="flex flex-wrap gap-2">
          <StatChip label="สินค้าทั้งหมด" value={stats.total.toLocaleString()} variant="primary" />
          <StatChip label="พร้อมจับคู่" value={stats.embedded.toLocaleString()} variant="success" />
          <StatChip label="รอเตรียมข้อมูล" value={stats.pending.toLocaleString()} variant="warning" />
          <StatChip label="โหลดไว้ใช้งาน" value={stats.index_size.toLocaleString()} variant="primary" />
          {(stats.hidden_code_count ?? 0) > 0 && (
            <HiddenCodeStatCard
              count={stats.hidden_code_count ?? 0}
              onInspect={() => setHiddenCodesOpen(true)}
            />
          )}
          {stats.embed_running || embedProgress ? (
            <Card className="flex-1 border-primary/30 bg-primary/5">
              <CardContent className="flex items-center gap-3 px-4 py-3">
                {stats.embed_running ? (
                  <Loader2 className="h-4 w-4 animate-spin text-accent-strong" />
                ) : (
                  <Sparkles className="h-4 w-4 text-accent-strong" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <p className="text-sm font-medium text-accent-strong">
                      {stats.embed_running ? 'กำลังเตรียมข้อมูลจับคู่…' : 'รอบเตรียมข้อมูลล่าสุด'}
                    </p>
                    {stats.embed_status?.session_id && (
                      <a
                        href={`https://openrouter.ai/logs?tab=sessions&session_id=${encodeURIComponent(stats.embed_status.session_id)}`}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 text-[11px] font-medium text-link hover:underline"
                      >
                        OpenRouter session
                        <ExternalLink className="h-3 w-3" />
                      </a>
                    )}
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    {embedProgress ?? 'รายการสินค้าเยอะ อาจใช้เวลาหลายนาที · ปิดหน้านี้ได้ · ระบบอัปเดตสถานะให้เอง'}
                  </p>
                </div>
              </CardContent>
            </Card>
          ) : stats.error > 0 ? (
            <StatChip label="มีปัญหา" value={stats.error.toLocaleString()} variant="danger" />
          ) : null}
        </div>
      )}

      {stats && stats.total > 0 && (
        <Card className="shadow-none">
          <CardContent className="space-y-2 px-3 py-2.5">
            <div className="flex items-baseline justify-between text-xs">
              <span className="font-medium text-foreground">ความพร้อมในการจับคู่</span>
              <span className="tabular-nums text-muted-foreground">
                {stats.embedded.toLocaleString()} / {stats.total.toLocaleString()} ({pct}%)
              </span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-success transition-all"
                style={{ width: `${pct}%` }}
              />
            </div>
          </CardContent>
        </Card>
      )}

      {/* Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-1 rounded-md border border-border bg-card p-0.5">
          {tabs.map(({ key, label, count }) => {
            const active = params.filter === key
            return (
              <button
                key={key}
                type="button"
                onClick={() => handleFilterChange(key)}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors',
                  active
                    ? 'bg-accent text-accent-foreground'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {label}
                {count != null && count > 0 && (
                  <Badge
                    variant="secondary"
                    className={cn(
                      'h-4 px-1 text-[10px] tabular-nums',
                      key === 'pending' && 'bg-warning/15 text-warning',
                      key === 'error' && 'bg-destructive/15 text-destructive',
                    )}
                  >
                    {count > 9999 ? '9999+' : count}
                  </Badge>
                )}
              </button>
            )
          })}
        </div>

        <div className="flex w-full flex-col gap-2 lg:w-auto lg:flex-row lg:items-center">
          <div className="relative w-full max-w-sm">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="ค้นหา… (Enter เพื่อค้นหา)"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={handleSearchKeyDown}
              className="h-9 pl-8 pr-16"
            />
            {draft && (
              <button
                type="button"
                className="absolute right-12 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                onClick={() => {
                  setDraft('')
                  commitSearch('')
                }}
                aria-label="ล้างการค้นหา"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="absolute right-1 top-1/2 h-7 -translate-y-1/2 px-2 text-xs"
              onClick={() => commitSearch(draft)}
            >
              ค้นหา
            </Button>
          </div>
        </div>
      </div>

      <div className="overflow-hidden rounded-lg border border-border bg-card">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/40">
              <TableHead className="w-[140px]">รหัสสินค้า</TableHead>
              <TableHead>ชื่อสินค้า</TableHead>
              <TableHead className="w-[80px]">หน่วย</TableHead>
              <TableHead className="w-[120px]">สถานะ</TableHead>
              <TableHead className="w-[120px]">เตรียมข้อมูลเมื่อ</TableHead>
              <TableHead className="w-[200px] text-right">จัดการ</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={6} className="py-12 text-center text-sm text-muted-foreground">
                  <Loader2 className="mx-auto mb-2 h-5 w-5 animate-spin" />
                  กำลังโหลด…
                </TableCell>
              </TableRow>
            ) : items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-12 text-center text-sm">
                  <Database className="mx-auto mb-2 h-8 w-8 text-muted-foreground/50" />
                  <p className="text-muted-foreground">
                    {params.query
                      ? `ไม่พบสินค้าที่ตรงกับ "${params.query}"`
                      : 'ไม่มีข้อมูล'}
                  </p>
                </TableCell>
              </TableRow>
            ) : (
              items.map((item) => (
                <TableRow key={item.item_code} className="h-16">
                  <TableCell className="py-2 font-mono text-xs font-medium">
                    <div className="space-y-1">
                      <div>{item.item_code}</div>
                      {item.has_hidden_chars && (
                        <div
                          className="inline-flex max-w-full items-center gap-1 rounded-md border border-warning/30 bg-warning/10 px-1.5 py-0.5 font-sans text-[10px] font-medium text-warning"
                          title={`รหัสนี้มีอักขระมองไม่เห็นจาก SML: ${hiddenKindText(item.hidden_char_kinds)}${item.clean_item_code ? ` รหัสที่ควรเป็น ${item.clean_item_code}` : ''}`}
                        >
                          <AlertTriangle className="h-3 w-3 shrink-0" />
                          <span className="truncate">{hiddenKindText(item.hidden_char_kinds)}</span>
                        </div>
                      )}
                      {item.has_hidden_chars && item.clean_item_code && (
                        <div className="font-sans text-[10px] text-muted-foreground">
                          รหัสที่ควรเป็น <code className="font-mono">{item.clean_item_code}</code>
                        </div>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="py-2">
                    <div className="flex min-w-0 items-center gap-3">
                      <CatalogThumbnail item={item} onPreview={setPreviewItem} />
                      <div className="min-w-0 flex-1">
                        <div className="line-clamp-2 text-sm leading-5">{item.item_name}</div>
                        {item.item_name2 && (
                          <div className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">
                            {item.item_name2}
                          </div>
                        )}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="py-2 text-xs text-muted-foreground">
                    {item.unit_code || '—'}
                  </TableCell>
                  <TableCell className="py-2">
                    <Badge
                      variant="secondary"
                      className={cn(
                        item.embedding_status === 'done' &&
                          'bg-success/15 text-success hover:bg-success/20',
                        item.embedding_status === 'pending' &&
                          'bg-warning/15 text-warning hover:bg-warning/20',
                        item.embedding_status === 'error' &&
                          'bg-destructive/15 text-destructive hover:bg-destructive/20',
                      )}
                    >
                      {item.embedding_status === 'done'
                        ? 'พร้อมจับคู่'
                        : item.embedding_status === 'pending'
                          ? 'รอเตรียมข้อมูล'
                          : 'มีปัญหา'}
                    </Badge>
                  </TableCell>
                  <TableCell className="py-2 text-xs tabular-nums text-muted-foreground">
                    {item.embedded_at
                      ? new Date(item.embedded_at).toLocaleDateString('th-TH')
                      : '—'}
                  </TableCell>
                  <TableCell className="py-2 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {item.embedding_status !== 'done' && (
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 px-2 text-xs"
                          onClick={() => handleEmbedOne(item.item_code)}
                        >
                          เตรียมข้อมูล
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 px-2"
                        title="รีเฟรชจาก SML — ดึงชื่อ/หน่วย/balance จาก SML 248 ใหม่"
                        disabled={busyRow?.code === item.item_code}
                        onClick={() => handleRefreshOne(item.item_code)}
                      >
                        {busyRow?.code === item.item_code && busyRow.action === 'refresh' ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <RefreshCw className="h-3.5 w-3.5" />
                        )}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 px-2 text-destructive hover:text-destructive"
                        title="ลบจาก Nexflow (SML ไม่ถูกแตะ)"
                        disabled={busyRow?.code === item.item_code}
                        onClick={() => setPendingDelete(item.item_code)}
                      >
                        {busyRow?.code === item.item_code && busyRow.action === 'delete' ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>
          {loading
            ? 'กำลังโหลด…'
            : `${total.toLocaleString()} รายการ · หน้า ${params.page} / ${Math.max(1, Math.ceil(total / PER_PAGE))}`}
        </span>
        <Pagination
          page={params.page}
          total={total}
          perPage={PER_PAGE}
          onChange={(p) => setParams({ page: p })}
        />
      </div>

      <ConfirmDialog
        open={syncConfirmOpen}
        onOpenChange={setSyncConfirmOpen}
        title="ซิงก์สินค้าทั้งหมดจาก SML"
        description={[
          'งานนี้จะดึง master สินค้าทั้งหมดจาก SML และอาจใช้เวลาหลายนาที',
          'ผลกระทบ: รายการสินค้าใน Nexflow จะอัปเดตตาม SML master ล่าสุด แต่ไม่แก้ master data ใน SML',
          'ถ้าต้องการเพิ่มสินค้าไม่กี่รหัส ให้ใช้ปุ่ม “ดึงรายตัวจาก SML” แทน',
        ].join('\n')}
        confirmLabel="เริ่มซิงก์ทั้งหมด"
        onConfirm={handleSync}
      />

      <ConfirmDialog
        open={embedConfirmOpen}
        onOpenChange={setEmbedConfirmOpen}
        title="สร้างข้อมูลจับคู่สินค้า?"
        description={[
          `ระบบจะเตรียมข้อมูลจับคู่จากสินค้าใน Catalog ประมาณ ${(stats?.total ?? 0).toLocaleString('th-TH')} รายการ`,
          'ผลกระทบ: งานนี้ช่วยให้ Nexflow แนะนำรหัสสินค้า SML จากชื่อสินค้า marketplace ได้แม่นขึ้น และอาจใช้เวลาหลายนาที',
          'งานที่ทำแล้วจะไม่หายหาก backend restart ระหว่างประมวลผล',
        ].join('\n')}
        confirmLabel="เริ่มสร้างข้อมูลจับคู่"
        onConfirm={handleEmbedAll}
      />

      <ConfirmDialog
        open={!!pendingDelete}
        onOpenChange={(v) => !v && setPendingDelete(null)}
        title="ลบสินค้าออกจาก Catalog"
        description={
          pendingDelete
            ? [
                `รหัสสินค้า: ${pendingDelete}`,
                'ผลกระทบ: ลบเฉพาะข้อมูลใน Nexflow catalog และข้อมูลจับคู่ที่เกี่ยวกับรายการนี้',
                'SML master จะไม่ถูกแตะ หากลบผิดให้กดดึงรายตัวจาก SML หรือซิงก์ใหม่',
              ].join('\n')
            : ''
        }
        confirmLabel="ลบจาก Nexflow"
        variant="destructive"
        onConfirm={() => {
          if (pendingDelete) handleDeleteOne(pendingDelete)
        }}
      />

      <ProductImagePreviewDialog
        open={!!previewItem}
        onOpenChange={(v) => !v && setPreviewItem(null)}
        imageUrl={previewItem?.image_url}
        itemCode={previewItem?.item_code}
        itemName={previewItem?.item_name}
        imageCount={previewItem?.image_count ?? 0}
      />

      <CatalogPullDialog
        open={pullDialogOpen}
        onOpenChange={setPullDialogOpen}
        onPulled={handleCatalogPulled}
      />

      <HiddenCodesDialog
        open={hiddenCodesOpen}
        onOpenChange={setHiddenCodesOpen}
      />
    </div>
  )
}
