import type { AuditLog } from '@/lib/audit-log-meta'

export type SMLResolutionStatus = 'resolved' | 'succeeded' | 'unresolved' | 'unknown'

export interface SMLAttemptTimelineItem {
  kind: 'sml_attempt'
  attempt_id: string
  events: AuditLog[]
  failure_count: number
  resolution_status: SMLResolutionStatus
  can_retry: boolean
  summary: string
}

export interface PlainTimelineItem {
  kind: 'event'
  event: AuditLog
}

export type BillTimelineItem = SMLAttemptTimelineItem | PlainTimelineItem

const attemptActions = new Set([
  'profile_requested',
  'core_committed',
  'reconcile_queued',
  'profile_complete',
  'profile_terminal_failure',
  'profile_retry_requested',
  'sml_sent',
  'sml_failed',
  'sml_erp_log_warning',
  'sml_stock_recalc_ok',
  'sml_stock_recalc_failed',
])

function text(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function attemptID(event: AuditLog): string {
  return text(event.attempt_id) || text(event.detail?.attempt_id)
}

function evidenceKey(event: AuditLog): string {
  const docNo = text(event.detail?.doc_no) || text(event.detail?.doc_no_attempted)
  const route = text(event.detail?.route).toLowerCase()
  return docNo && route ? `${docNo}\u0000${route}` : ''
}

function addEvidence(index: Map<string, Set<string>>, key: string, id: string) {
  if (!key || !id) return
  const ids = index.get(key) ?? new Set<string>()
  ids.add(id)
  index.set(key, ids)
}

function singleEvidence(index: Map<string, Set<string>>, key: string): string {
  if (!key) return ''
  const ids = index.get(key)
  return ids?.size === 1 ? [...ids][0] : ''
}

function isSuccessful(events: AuditLog[]): boolean {
  return events.some((event) =>
    event.action === 'sml_sent' ||
    event.action === 'core_committed' ||
    event.core_status === 'created' ||
    event.detail?.core_status === 'created',
  )
}

function isUnknown(events: AuditLog[]): boolean {
  return events.some((event) =>
    event.resolution_status === 'unknown' ||
    event.detail?.resolution_status === 'unknown',
  )
}

function attemptSummary(events: AuditLog[], failureCount: number): string {
  const automatic = events.some((event) => event.detail?.via === 'shopee_auto_sml')
  const success = isSuccessful(events)
  if (success) {
    const prefix = automatic ? 'ส่ง SML อัตโนมัติสำเร็จ' : 'ส่ง SML สำเร็จ'
    return failureCount > 0 ? `${prefix}หลังลองใหม่ ${failureCount} ครั้ง` : prefix
  }
  if (isUnknown(events)) return 'ผลการเรียก SML ยังไม่แน่นอน'
  if (failureCount > 0) return `ส่ง SML ยังไม่สำเร็จ · ลองแล้ว ${failureCount} ครั้ง`
  return 'กำลังส่งข้อมูลไป SML'
}

function buildAttemptItem(id: string, events: AuditLog[]): SMLAttemptTimelineItem {
  const failureCount = events.filter((event) => event.action === 'sml_failed').length
  const success = isSuccessful(events)
  const resolutionStatus: SMLResolutionStatus = success
    ? (failureCount > 0 ? 'resolved' : 'succeeded')
    : isUnknown(events)
      ? 'unknown'
      : 'unresolved'
  const serverAllowsRetry = events.some((event) => event.can_retry === true)

  return {
    kind: 'sml_attempt',
    attempt_id: id,
    events,
    failure_count: failureCount,
    resolution_status: resolutionStatus,
    can_retry: !success && resolutionStatus === 'unresolved' && serverAllowsRetry,
    summary: attemptSummary(events, failureCount),
  }
}

// Group only when immutable attempt identity is explicit or legacy evidence is
// unique. A shared document number alone is never enough when multiple attempt
// IDs exist, which prevents separate attempts from being presented as one.
export function groupSMLTimelineEvents(events: AuditLog[]): BillTimelineItem[] {
  const traceAttempts = new Map<string, Set<string>>()
  const documentAttempts = new Map<string, Set<string>>()
  for (const event of events) {
    const id = attemptID(event)
    if (!id || !attemptActions.has(event.action)) continue
    addEvidence(traceAttempts, text(event.trace_id), id)
    addEvidence(documentAttempts, evidenceKey(event), id)
  }

  const assigned = new Map<string, string>()
  const groups = new Map<string, AuditLog[]>()
  for (const event of events) {
    if (!attemptActions.has(event.action)) continue
    const id = attemptID(event) ||
      singleEvidence(traceAttempts, text(event.trace_id)) ||
      singleEvidence(documentAttempts, evidenceKey(event))
    if (!id) continue
    assigned.set(event.id, id)
    const group = groups.get(id) ?? []
    group.push(event)
    groups.set(id, group)
  }

  const emitted = new Set<string>()
  const result: BillTimelineItem[] = []
  for (const event of events) {
    const id = assigned.get(event.id)
    if (!id) {
      result.push({ kind: 'event', event })
      continue
    }
    if (emitted.has(id)) continue
    emitted.add(id)
    result.push(buildAttemptItem(id, groups.get(id) ?? []))
  }
  return result
}
