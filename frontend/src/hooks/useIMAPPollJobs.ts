import client from '@/api/client'

export type IMAPPollJobStatus =
  | 'queued'
  | 'running'
  | 'completed'
  | 'completed_with_errors'
  | 'failed'

export interface IMAPPollJob {
  id: string
  account_id: string
  account_name?: string
  account_email?: string
  status: IMAPPollJobStatus
  total_count: number
  scanned_count: number
  created_count: number
  skipped_count: number
  failed_count: number
  backlog_count: number
  reason_counts?: Record<string, number>
  last_error?: string
  created_by_email?: string
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export function isActiveIMAPPollJob(job?: IMAPPollJob | null) {
  return job?.status === 'queued' || job?.status === 'running'
}

export async function createIMAPPollJob(accountID: string): Promise<IMAPPollJob> {
  const res = await client.post<{ job_id: string; job: IMAPPollJob }>(`/api/settings/imap-accounts/${accountID}/poll-jobs`)
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('nexflow:imap-poll-job-started', { detail: { job_id: res.data.job_id } }))
  }
  return res.data.job
}

export async function getIMAPPollJob(id: string): Promise<IMAPPollJob> {
  const res = await client.get<IMAPPollJob>(`/api/settings/imap-poll-jobs/${id}`)
  return res.data
}

export async function listActiveIMAPPollJobs(): Promise<IMAPPollJob[]> {
  const res = await client.get<{ data: IMAPPollJob[] }>('/api/settings/imap-poll-jobs/active')
  return res.data.data ?? []
}
