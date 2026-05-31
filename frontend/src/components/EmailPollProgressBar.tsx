import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { MailCheck, ShoppingBag } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  isActiveIMAPPollJob,
  getIMAPPollJob,
  listActiveIMAPPollJobs,
  type IMAPPollJob,
} from '@/hooks/useIMAPPollJobs'
import { cn } from '@/lib/utils'

const PURCHASE_BILLS_URL = '/bills?status=needs_review&source=shopee_shipped&bill_type=purchase'
const ACTIVE_POLL_MS = 4_000
const IDLE_POLL_MS = 30_000

function doneCount(job: IMAPPollJob) {
  return Math.min(job.scanned_count || 0, Math.max(job.total_count || job.scanned_count || 0, 0))
}

function pct(job: IMAPPollJob) {
  const total = Math.max(job.total_count || 0, 1)
  return Math.min(100, Math.round((doneCount(job) / total) * 100))
}

export function EmailPollProgressBar() {
  const [jobs, setJobs] = useState<IMAPPollJob[]>([])
  const activeIdsRef = useRef<Set<string>>(new Set())

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    let inFlight = false
    let rerunRequested = false
    const load = async () => {
      if (inFlight) {
        rerunRequested = true
        return
      }
      inFlight = true
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
      let nextDelay = IDLE_POLL_MS
      try {
        const active = await listActiveIMAPPollJobs()
        if (cancelled) return
        nextDelay = active.some(isActiveIMAPPollJob) ? ACTIVE_POLL_MS : IDLE_POLL_MS
        const nextIds = new Set(active.map((job) => job.id))
        for (const id of activeIdsRef.current) {
          if (nextIds.has(id)) continue
          try {
            const finished = await getIMAPPollJob(id)
            if (finished.status === 'completed' || finished.status === 'completed_with_errors') {
              toast.success(
                `ดึงอีเมลเสร็จ: สร้างบิล ${finished.created_count.toLocaleString('th-TH')} ใบ / ต้องตรวจ ${finished.failed_count.toLocaleString('th-TH')}`,
                {
                  action: {
                    label: 'ดูใบสั่งซื้อ',
                    onClick: () => { window.location.href = PURCHASE_BILLS_URL },
                  },
                },
              )
            } else if (finished.status === 'failed') {
              toast.error(`ดึงอีเมลไม่สำเร็จ${finished.last_error ? `: ${finished.last_error}` : ''}`)
            }
          } catch {
            // Ignore stale/deleted jobs; the next active poll will resync the bar.
          }
        }
        activeIdsRef.current = nextIds
        setJobs(active)
      } catch {
        if (!cancelled) setJobs([])
      } finally {
        inFlight = false
        if (rerunRequested && !cancelled) {
          rerunRequested = false
          void load()
          return
        }
        if (!cancelled) {
          timer = setTimeout(load, nextDelay)
        }
      }
    }
    load()
    const handleJobStarted = () => {
      void load()
    }
    window.addEventListener('nexflow:imap-poll-job-started', handleJobStarted)
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
      window.removeEventListener('nexflow:imap-poll-job-started', handleJobStarted)
    }
  }, [])

  const activeJobs = useMemo(() => jobs.filter(isActiveIMAPPollJob), [jobs])
  if (activeJobs.length === 0) return null

  const total = activeJobs.reduce((sum, job) => sum + Math.max(job.total_count || 0, 0), 0)
  const done = activeJobs.reduce((sum, job) => sum + doneCount(job), 0)
  const created = activeJobs.reduce((sum, job) => sum + job.created_count, 0)
  const failed = activeJobs.reduce((sum, job) => sum + job.failed_count, 0)
  const percent = Math.min(100, Math.round((done / Math.max(total, 1)) * 100))

  return (
    <div className="border-b border-info/20 bg-info/5 px-3 py-2">
      <div className="mx-auto flex max-w-[1480px] flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-info/10 text-info">
            <MailCheck className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium text-foreground">
              <span>กำลังดึงอีเมล {activeJobs.length.toLocaleString('th-TH')} งาน</span>
              <span className="text-xs font-normal text-muted-foreground">
                ทำแล้ว {done.toLocaleString('th-TH')} / {total.toLocaleString('th-TH')}
              </span>
              <span className="text-xs font-normal text-success">
                สร้างบิล {created.toLocaleString('th-TH')}
              </span>
              {failed > 0 && (
                <span className="text-xs font-normal text-warning">
                  ต้องตรวจ {failed.toLocaleString('th-TH')}
                </span>
              )}
            </div>
            <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-background">
              <div
                className={cn('h-full rounded-full transition-all', failed > 0 ? 'bg-warning' : 'bg-info')}
                style={{ width: `${percent}%` }}
              />
            </div>
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <Button asChild variant="outline" size="sm" className="h-8 gap-1.5 bg-background">
            <Link to="/settings/email">ดูงานอีเมล</Link>
          </Button>
          <Button asChild variant="outline" size="sm" className="h-8 gap-1.5 bg-background">
            <Link to={PURCHASE_BILLS_URL}>
              <ShoppingBag className="h-3.5 w-3.5" />
              ดูใบสั่งซื้อ
            </Link>
          </Button>
        </div>
      </div>
    </div>
  )
}
