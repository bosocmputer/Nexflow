import { useState, useEffect, useCallback } from 'react'
import client from '../api/client'
import { notifyWorkQueueChanged } from '../lib/work-queue-events'
import { humanizeSMLConnectionError } from '../lib/sml-readiness'
import type { Bill, BillItem, BillListResponse } from '../types'

interface BillsFilter {
  page?: number
  per_page?: number
  status?: string
  shopee_status?: string
  source?: string
  bill_type?: string
  document_route?: string
  email_account_id?: string
  search?: string
  shopee_shop_id?: string
  archived?: 'include' | 'only' | ''
  date_from?: string
  date_to?: string
  cursor?: string
  limit?: number
  include_total?: boolean
}

export interface RetryBillPayload {
  party_code?: string
  party_name?: string
  doc_no?: string
  remark?: string
  remark_2?: string
  branch_code?: string
  sale_code?: string
  unit_code?: string
  doc_time?: string
  wh_code?: string
  shelf_code?: string
  vat_type?: number
  vat_rate?: number
  inquiry_type?: number
}

export interface RetryBillResponse {
  message?: string
  doc_no?: string
  error?: string
}

export type BulkSendJobStatus =
  | 'queued'
  | 'running'
  | 'completed'
  | 'completed_with_errors'
  | 'failed'

export type BulkSendJobItemStatus = 'queued' | 'running' | 'sent' | 'failed' | 'skipped'

export interface BulkSendJobItem {
  id: string
  job_id: string
  bill_id: string
  sequence: number
  status: BulkSendJobItemStatus
  order_no: string
  doc_no_attempted: string
  doc_no: string
  error: string
  attempts: number
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export interface BulkSendJob {
  id: string
  client_request_id: string
  status: BulkSendJobStatus
  source: string
  bill_type: string
  document_route: string
  title: string
  request_payload: RetryBillPayload
  filter_snapshot: Record<string, unknown>
  total_count: number
  sent_count: number
  failed_count: number
  skipped_count: number
  created_by_email?: string
  last_error: string
  created_at: string
  started_at?: string
  finished_at?: string
  updated_at: string
  items?: BulkSendJobItem[]
}

export interface BulkSendJobListResponse {
  data: BulkSendJob[]
  total: number
  page: number
  per_page: number
  has_more: boolean
}

export function useBills(filter: BillsFilter = {}) {
  const [data, setData] = useState<BillListResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      if (filter.page) params.set('page', String(filter.page))
      if (filter.per_page) params.set('per_page', String(filter.per_page))
      if (filter.status) params.set('status', filter.status)
      if (filter.shopee_status) params.set('shopee_status', filter.shopee_status)
      if (filter.source) params.set('source', filter.source)
      if (filter.bill_type) params.set('bill_type', filter.bill_type)
      if (filter.document_route) params.set('document_route', filter.document_route)
      if (filter.email_account_id) params.set('email_account_id', filter.email_account_id)
      if (filter.search) params.set('search', filter.search)
      if (filter.shopee_shop_id) params.set('shopee_shop_id', filter.shopee_shop_id)
      if (filter.archived) params.set('archived', filter.archived)
      if (filter.date_from) params.set('date_from', filter.date_from)
      if (filter.date_to) params.set('date_to', filter.date_to)
      if (filter.cursor) params.set('cursor', filter.cursor)
      if (filter.limit) params.set('limit', String(filter.limit))
      if (filter.include_total) params.set('include_total', 'true')
      const res = await client.get<BillListResponse>(`/api/bills?${params}`)
      setData(res.data)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to fetch bills')
    } finally {
      setLoading(false)
    }
  }, [JSON.stringify(filter)])

  useEffect(() => { fetch() }, [fetch])

  return { data, loading, error, refetch: fetch }
}

export async function getBill(id: string): Promise<Bill> {
  const res = await client.get<Bill>(`/api/bills/${id}`)
  return res.data
}

export async function retryBill(
  id: string,
  body?: RetryBillPayload,
): Promise<RetryBillResponse> {
  const res = await client.post<RetryBillResponse>(`/api/bills/${id}/retry`, body ?? {}, {
    validateStatus: () => true,
  })
  if (res.status !== 200) {
    throw new Error(humanizeSMLConnectionError(res.data?.error || res.data?.message || `ส่ง SML ไม่สำเร็จ (HTTP ${res.status})`))
  }
  notifyWorkQueueChanged()
  return res.data
}

export async function ensureShopeeShippingLine(
  id: string,
): Promise<{ inserted: boolean; item?: BillItem | null }> {
  const res = await client.post<{ inserted: boolean; item?: BillItem | null }>(
    `/api/bills/${id}/ensure-shopee-shipping-line`,
  )
  if (res.data.inserted) notifyWorkQueueChanged()
  return res.data
}

export async function regenerateBillDocNo(
  id: string,
): Promise<{ doc_no: string; route: string }> {
  const res = await client.post<{ doc_no?: string; route?: string; error?: string; message?: string }>(
    `/api/bills/${id}/regenerate-doc-no`,
    {},
    { validateStatus: () => true },
  )
  if (res.status < 200 || res.status >= 300) {
    throw new Error(
      humanizeSMLConnectionError(
        res.data?.error ||
          res.data?.message ||
          `ออกเลขเอกสารใหม่ไม่สำเร็จ (HTTP ${res.status})`,
      ),
    )
  }
  notifyWorkQueueChanged()
  return { doc_no: res.data.doc_no ?? '', route: res.data.route ?? '' }
}

export async function getLatestBillDocNo(
  id: string,
): Promise<{ doc_no: string; route: string }> {
  const res = await client.get<{ doc_no?: string; route?: string; error?: string; message?: string }>(
    `/api/bills/${id}/latest-doc-no`,
    { validateStatus: () => true },
  )
  if (res.status < 200 || res.status >= 300) {
    throw new Error(
      humanizeSMLConnectionError(
        res.data?.error ||
          res.data?.message ||
          `ดึงเลขล่าสุดไม่สำเร็จ (HTTP ${res.status})`,
      ),
    )
  }
  return { doc_no: res.data.doc_no ?? '', route: res.data.route ?? '' }
}

export async function createBulkSendJob(body: {
  client_request_id: string
  bill_ids: string[]
  payload: RetryBillPayload
  filter_snapshot: Record<string, unknown>
  source: string
  bill_type: string
  document_route?: string
  title: string
}): Promise<BulkSendJob> {
  const res = await client.post<{ job_id: string; job: BulkSendJob }>('/api/bills/bulk-send-jobs', body)
  notifyWorkQueueChanged()
  return res.data.job
}

export async function getBulkSendJob(id: string): Promise<BulkSendJob> {
  const res = await client.get<BulkSendJob>(`/api/bills/bulk-send-jobs/${id}`)
  return res.data
}

export async function listBulkSendJobs(params: {
  page?: number
  per_page?: number
  status?: string
  source?: string
  bill_type?: string
  document_route?: string
} = {}): Promise<BulkSendJobListResponse> {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.per_page) search.set('per_page', String(params.per_page))
  if (params.status) search.set('status', params.status)
  if (params.source) search.set('source', params.source)
  if (params.bill_type) search.set('bill_type', params.bill_type)
  if (params.document_route) search.set('document_route', params.document_route)
  const res = await client.get<BulkSendJobListResponse>(`/api/bills/bulk-send-jobs?${search}`)
  return res.data
}

export async function getActiveBulkSendJob(params: {
  source: string
  bill_type: string
  document_route?: string
  shopee_shop_id?: string
}): Promise<BulkSendJob | null> {
  const search = new URLSearchParams()
  if (params.source) search.set('source', params.source)
  if (params.bill_type) search.set('bill_type', params.bill_type)
  if (params.document_route) search.set('document_route', params.document_route)
  if (params.shopee_shop_id) search.set('shopee_shop_id', params.shopee_shop_id)
  try {
    const res = await client.get<BulkSendJob>(`/api/bills/bulk-send-jobs/active?${search}`)
    return res.data
  } catch (err) {
    const maybe = err as { response?: { status?: number } }
    if (maybe.response?.status === 404) return null
    throw err
  }
}

export async function retryFailedBulkSendJob(id: string, clientRequestID: string): Promise<BulkSendJob> {
  const res = await client.post<{ job_id: string; job: BulkSendJob }>(`/api/bills/bulk-send-jobs/${id}/retry-failed`, {
    client_request_id: clientRequestID,
  })
  notifyWorkQueueChanged()
  return res.data.job
}

export async function archiveBill(id: string, reason?: string): Promise<void> {
  await client.post(`/api/bills/${id}/archive`, { reason })
  notifyWorkQueueChanged()
}

export async function restoreBill(id: string): Promise<void> {
  await client.post(`/api/bills/${id}/restore`)
  notifyWorkQueueChanged()
}

export async function deleteBill(id: string): Promise<void> {
  await client.delete(`/api/bills/${id}`, { data: { confirm: 'DELETE' } })
  notifyWorkQueueChanged()
}
