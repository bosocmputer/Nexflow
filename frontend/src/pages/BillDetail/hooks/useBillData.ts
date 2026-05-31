import { createElement, useState, useEffect, useCallback } from 'react'
import { toast } from 'sonner'
import { ensureShopeeShippingLine, getBill, getLatestBillDocNo, regenerateBillDocNo, retryBill } from '@/hooks/useBills'
import type { RetryBillPayload } from '@/hooks/useBills'
import type { Bill } from '@/types'

const SHOPEE_SHIPPING_SOURCE_SKU = '__shopee_shipping__'

function rawNumber(payload: Record<string, unknown> | null | undefined, key: string) {
  const value = payload?.[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const parsed = Number(value.replace(/,/g, '').trim())
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

function shouldEnsureShopeeShippingLine(bill: Bill) {
  if (bill.source !== 'shopee_shipped' || bill.bill_type !== 'purchase') return false
  if (!['failed', 'pending', 'needs_review'].includes(bill.status)) return false
  if ((bill.items ?? []).some((item) => item.source_sku === SHOPEE_SHIPPING_SOURCE_SKU)) return false
  const shippingAmount = rawNumber(bill.raw_data, 'shipping_amount')
  return shippingAmount != null && shippingAmount >= 0
}

function docToastDescription(docNo: string | null | undefined) {
  const clean = docNo?.trim()
  if (!clean) return undefined
  return createElement(
    'span',
    { className: 'font-mono font-semibold text-foreground' },
    `Doc: ${clean}`,
  )
}

export interface UseBillDataReturn {
  bill: Bill | null
  loading: boolean
  retrying: boolean
  regeneratingDocNo: boolean
  refreshingDocNo: boolean
  retryError: string | null
  reloadBill: () => Promise<Bill | null>
  handleRetry: () => Promise<SMLRetryResult>
  handleRetryWithOverride: (body: RetryBillPayload) => Promise<SMLRetryResult>
  handleRegenerateDocNo: () => Promise<string | null>
  handleFetchLatestDocNo: () => Promise<string | null>
  setBill: React.Dispatch<React.SetStateAction<Bill | null>>
}

export type SMLRetryResult = {
  bill: Bill | null
  docNo: string | null
}

export function useBillData(id: string | undefined): UseBillDataReturn {
  const [bill, setBill] = useState<Bill | null>(null)
  const [loading, setLoading] = useState(true)
  const [retrying, setRetrying] = useState(false)
  const [regeneratingDocNo, setRegeneratingDocNo] = useState(false)
  const [refreshingDocNo, setRefreshingDocNo] = useState(false)
  const [retryError, setRetryError] = useState<string | null>(null)

  const reloadBill = useCallback(async () => {
    if (!id) return null
    let updated = await getBill(id)
    if (shouldEnsureShopeeShippingLine(updated)) {
      const result = await ensureShopeeShippingLine(id)
      if (result.inserted) {
        toast.success('เติมรายการค่าขนส่ง Shopee แล้ว')
        updated = await getBill(id)
      }
    }
    setBill(updated)
    return updated
  }, [id])

  useEffect(() => {
    if (!id) return
    setLoading(true)
    reloadBill()
      .catch(() => setBill(null))
      .finally(() => setLoading(false))
  }, [id, reloadBill])

  const doRetry = useCallback(
    async (body?: RetryBillPayload): Promise<SMLRetryResult> => {
      if (!id) return { bill: null, docNo: null }
      setRetrying(true)
      setRetryError(null)
      try {
        const retryResult = await retryBill(id, body)
        const updated = await reloadBill()
        const docNo = retryResult.doc_no?.trim() || updated?.sml_doc_no?.trim() || null
        toast.success('ส่ง SML สำเร็จ', {
          description: docToastDescription(docNo),
        })
        return { bill: updated, docNo }
      } catch (err) {
        try {
          await reloadBill()
        } catch {
          // Keep the existing bill in view if the follow-up refresh also fails.
        }
        const message =
          err instanceof Error && err.message
            ? err.message
            : 'Retry ล้มเหลว — กรุณาลองใหม่อีกครั้ง'
        setRetryError(message)
        toast.error('ส่ง SML ไม่สำเร็จ', {
          description: 'ดูรายละเอียดในการ์ด Error ด้านบน',
        })
        throw new Error(message)
      } finally {
        setRetrying(false)
      }
    },
    [id, reloadBill],
  )

  const handleRetry = useCallback(() => doRetry(), [doRetry])

  const handleRetryWithOverride = useCallback(
    (body: RetryBillPayload) => doRetry(body),
    [doRetry],
  )

  const handleRegenerateDocNo = useCallback(async () => {
    if (!id) return null
    setRegeneratingDocNo(true)
    try {
      const result = await regenerateBillDocNo(id)
      await reloadBill()
      toast.success('ออกเลขเอกสารใหม่แล้ว', {
        description: docToastDescription(result.doc_no),
      })
      return result.doc_no || null
    } catch (err) {
      const message =
        err instanceof Error && err.message
          ? err.message
          : 'ออกเลขเอกสารใหม่ไม่สำเร็จ'
      toast.error('ออกเลขเอกสารใหม่ไม่สำเร็จ', { description: message })
      return null
    } finally {
      setRegeneratingDocNo(false)
    }
  }, [id, reloadBill])

  const handleFetchLatestDocNo = useCallback(async () => {
    if (!id) return null
    setRefreshingDocNo(true)
    try {
      const result = await getLatestBillDocNo(id)
      toast.success('ดึงเลขล่าสุดแล้ว', {
        description: docToastDescription(result.doc_no),
      })
      return result.doc_no || null
    } catch (err) {
      const message =
        err instanceof Error && err.message
          ? err.message
          : 'ดึงเลขล่าสุดไม่สำเร็จ'
      toast.error('ดึงเลขล่าสุดไม่สำเร็จ', { description: message })
      return null
    } finally {
      setRefreshingDocNo(false)
    }
  }, [id])

  return {
    bill,
    loading,
    retrying,
    regeneratingDocNo,
    refreshingDocNo,
    retryError,
    reloadBill,
    handleRetry,
    handleRetryWithOverride,
    handleRegenerateDocNo,
    handleFetchLatestDocNo,
    setBill,
  }
}
