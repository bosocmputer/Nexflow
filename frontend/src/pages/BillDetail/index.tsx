import { useEffect, useMemo, useState } from 'react'
import { useLocation, useParams } from 'react-router-dom'
import { useNavigate } from 'react-router-dom'
import axios from 'axios'
import { ArrowLeft, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import client from '@/api/client'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DetailPageSkeleton } from '@/components/common/LoadingSkeleton'
import type { BillItem } from '@/types'

import { useBillData } from './hooks/useBillData'
import { BillHeader } from './components/BillHeader'
import { BillFailureCard } from './components/BillFailureCard'
import { BillTotal } from './components/BillTotal'
import { BillItemsTable } from './components/BillItemsTable'
import { BillTimeline } from './components/BillTimeline'
import { ArtifactList } from './components/ArtifactList'
import { SmlPayloadSection } from './components/SmlPayloadSection'
import { SendPurchaseDialog } from './components/SendPurchaseDialog'
import { SMLSendProgressDialog, type SMLSendProgressStatus } from './components/SMLSendProgressDialog'
import { validateForSML } from './utils/validation'
import type { RetryBillPayload } from '@/hooks/useBills'
import { useSMLReadiness } from '@/hooks/useSMLReadiness'
import { humanizeSMLConnectionError, isSMLReady, smlBlockedMessage } from '@/lib/sml-readiness'
import { notifyWorkQueueChanged } from '@/lib/work-queue-events'

type SingleSMLSendResult = {
  docNo?: string | null
  bill?: {
    sml_doc_no?: string | null
  } | null
}

type SendProgressState = {
  open: boolean
  status: SMLSendProgressStatus
  docNo: string | null
  error: string | null
}

type RecreateRouteResponse = {
  redirect_url?: string
  message?: string
}

export default function BillDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const {
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
  } =
    useBillData(id)
  const { readiness: smlReadiness, loading: smlReadinessLoading } = useSMLReadiness()

  // ⚠ All hooks must be declared BEFORE any early return. React tracks hooks
  // by call order; conditional early returns make the count vary between
  // renders and trigger error #310 ("Rendered more hooks than previous").
  // useState + useMemo BOTH live up here. Don't move them below the
  // `if (loading)` guard.

  // highlightItemId — the BillTotal warning card's "ดู →" link sets this so
  // the matching BillItemRow scrolls into view + flashes (1.5s). To re-fire
  // on second click of the same row we briefly null the state in handleJump.
  const [highlightItemId, setHighlightItemId] = useState<string | null>(null)

  // sendDialogOpen — SML 248 documents show a dialog (party picker + WH/VAT)
  // before the retry call, so admin can override per-bill send values.
  const [sendDialogOpen, setSendDialogOpen] = useState(false)
  const [directSendConfirmOpen, setDirectSendConfirmOpen] = useState(false)
  const [recreateRouteConfirmOpen, setRecreateRouteConfirmOpen] = useState(false)
  const [recreatingRoute, setRecreatingRoute] = useState(false)
  const [sendProgress, setSendProgress] = useState<SendProgressState>({
    open: false,
    status: 'sending',
    docNo: null,
    error: null,
  })

  // Frontend-side validation against backend retry rules. Memo on `bill`
  // so BillTotal/BillItemRow don't recompute on unrelated parent renders.
  // Tolerates bill=null during loading (validateForSML returns no_items).
  const validation = useMemo(
    () => (bill ? validateForSML(bill) : { canSend: false, issues: [], firstBlockingItemId: null }),
    [bill],
  )

  useEffect(() => {
    if (!bill || !id) return
    const route = bill.document_route || bill.preview?.route
    const expectedPath =
      bill.bill_type !== 'sale'
        ? `/bills/${id}`
        : route === 'saleinvoice'
          ? `/sale-invoices/${id}`
          : `/sales-orders/${id}`
    if (location.pathname !== expectedPath) {
      navigate(expectedPath, { replace: true })
    }
  }, [bill, id, location.pathname, navigate])

  const handleJumpToItem = (id: string | null) => {
    if (!id) return
    setHighlightItemId(null)
    // Defer to next tick so the row's useEffect sees null → id transition
    // even if the previous highlight was the same id.
    setTimeout(() => setHighlightItemId(id), 0)
  }

  // Marketplace purchase/sale documents need explicit per-bill SML values.
  const runSingleSMLSend = async (runner: () => Promise<SingleSMLSendResult | void>) => {
    if (retrying || (sendProgress.status === 'sending' && sendProgress.open)) return
    setSendProgress({ open: true, status: 'sending', docNo: null, error: null })
    try {
      const result = await runner()
      setSendProgress({
        open: true,
        status: 'success',
        docNo: result?.docNo || result?.bill?.sml_doc_no || null,
        error: null,
      })
    } catch (err) {
      const message =
        err instanceof Error && err.message
          ? err.message
          : 'ส่ง SML ไม่สำเร็จ'
      setSendProgress({
        open: true,
        status: 'error',
        docNo: null,
        error: humanizeSMLConnectionError(message),
      })
    }
  }

  const handleSendClick = () => {
    if (retrying || (sendProgress.status === 'sending' && sendProgress.open)) return
    if (!isSMLReady(smlReadiness)) {
      toast.error('ยังส่ง SML ไม่ได้', {
        description: smlBlockedMessage(smlReadiness),
      })
      return
    }
    if (bill?.bill_type === 'purchase' || (bill?.bill_type === 'sale' && (bill?.source === 'shopee' || bill?.source === 'lazada' || bill?.source === 'tiktok'))) {
      setSendDialogOpen(true)
    } else {
      setDirectSendConfirmOpen(true)
    }
  }

  const handlePurchaseConfirm = async (body: RetryBillPayload) => {
    setSendDialogOpen(false)
    await runSingleSMLSend(() => handleRetryWithOverride(body))
  }

  if (loading) {
    return <DetailPageSkeleton />
  }

  if (!bill) {
    return (
      <div className="space-y-4">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="gap-1.5 -ml-2 text-muted-foreground"
          onClick={() => navigate(-1)}
        >
          <ArrowLeft className="h-4 w-4" />
          กลับ
        </Button>
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          ไม่พบบิลที่ต้องการ
        </div>
      </div>
    )
  }

  const total = (bill.items ?? []).reduce(
    (s, i) => s + Math.max((i.qty ?? 0) * (i.price ?? 0) - (i.discount_amount ?? 0), 0),
    0,
  )
  const canSend =
    bill.status === 'failed' ||
    bill.status === 'pending' ||
    bill.status === 'needs_review'
  const canEdit = canSend
  const isShopeeRealtimeBill =
    bill.source === 'shopee' && bill.raw_data?.flow === 'shopee_realtime'
  const canRecreateDocumentRoute =
    isShopeeRealtimeBill &&
    bill.status !== 'sent' &&
    !bill.sml_doc_no &&
    !bill.archived_at
  const directRouteLabel =
    bill.preview?.route === 'saleinvoice'
      ? 'ขาย -> ขายสินค้าและบริการ'
      : bill.preview?.route === 'saleorder'
        ? 'ขาย -> ใบสั่งขาย'
        : bill.preview?.route === 'purchaseorder'
          ? 'ซื้อ -> ใบสั่งซื้อ'
          : bill.preview?.route === 'sale_reserve'
            ? 'ใบสั่งจอง'
            : bill.preview?.route || 'ใช้ route จาก backend'
  const directSendDescription = [
    `เอกสาร: ${bill.sml_doc_no || bill.preview?.doc_no || bill.id.slice(0, 8)}`,
    `ปลายทาง: ${directRouteLabel}${bill.preview?.doc_format_code ? ` · ${bill.preview.doc_format_code}` : ''}`,
    `จำนวนรายการ: ${(bill.items ?? []).length.toLocaleString('th-TH')} รายการ · ยอดรวมประมาณ ฿${total.toLocaleString('th-TH')}`,
    '',
    'ผลกระทบ: ระบบจะส่งเอกสารใบนี้เข้า SML ทันที และบันทึกผลกลับมาที่ Nexflow',
    'Rollback: ถ้าส่งสำเร็จแล้ว Nexflow ไม่สามารถลบหรือย้อนเอกสารใน SML ให้เอง ต้องแก้ไข/ยกเลิกใน SML ตามขั้นตอนของร้าน',
    bill.sml_doc_no ? 'Retry: จะใช้ doc_no เดิมที่บันทึกไว้ ไม่ออกเลขใหม่โดยอัตโนมัติ' : 'เลขเอกสาร: ถ้ายังไม่มี doc_no ระบบจะใช้เลขจาก backend/SML ตอนส่ง',
  ].join('\n')

  const handleDirectSendConfirm = async () => {
    await runSingleSMLSend(() => handleRetry())
  }

  const handleRecreateRouteConfirm = async () => {
    setRecreatingRoute(true)
    try {
      const res = await client.post<RecreateRouteResponse>(
        `/api/bills/${bill.id}/shopee-realtime/recreate-route`,
        { confirm: 'RECREATE_DOCUMENT_WITH_CURRENT_ROUTE' },
      )
      toast.success('พร้อมสร้างเอกสารใหม่แล้ว', {
        description: res.data.message || 'กลับไป Shopee Realtime แล้วกดสร้างเอกสารอีกครั้ง',
      })
      notifyWorkQueueChanged()
      navigate(res.data.redirect_url || '/shopee-operations')
    } catch (err) {
      const message = axios.isAxiosError(err)
        ? err.response?.data?.error || err.response?.data?.message || err.message
        : err instanceof Error
          ? err.message
          : 'เตรียมสร้างเอกสารใหม่ไม่สำเร็จ'
      toast.error('ยังสร้างใหม่ไม่ได้', { description: message })
      throw err
    } finally {
      setRecreatingRoute(false)
    }
  }

  const handleItemUpdated = (updated: BillItem) => {
    setBill((prev) => {
      if (!prev) return prev
      return {
        ...prev,
        items: (prev.items ?? []).map((it) =>
          it.id === updated.id ? { ...it, ...updated } : it,
        ),
      }
    })
  }

  const handleItemDeleted = (itemId: string) => {
    setBill((prev) => {
      if (!prev) return prev
      return { ...prev, items: (prev.items ?? []).filter((it) => it.id !== itemId) }
    })
  }

  const handleItemAdded = (newItem: BillItem) => {
    setBill((prev) => {
      if (!prev) return prev
      return { ...prev, items: [...(prev.items ?? []), newItem] }
    })
  }

  return (
    <div className="space-y-4">
      <BillHeader
        bill={bill}
        total={total}
        retrying={retrying}
        onRetry={handleSendClick}
        validation={validation}
        expectedRoute={bill.preview?.route}
        expectedEndpoint={bill.preview?.endpoint}
        expectedDocFormat={bill.preview?.doc_format}
        smlReadiness={smlReadiness}
        smlReadinessLoading={smlReadinessLoading}
      />

      {canRecreateDocumentRoute && (
        <div className="flex flex-col gap-3 rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0 space-y-1">
            <div className="font-medium text-foreground">ต้องการเปลี่ยนเส้นทางเอกสาร?</div>
            <p className="max-w-3xl text-xs leading-5 text-muted-foreground">
              เอกสารนี้ยังไม่ส่งเข้า SML สามารถปลดออกจาก Shopee Realtime แล้วกลับไปสร้างใหม่ตามเส้นทางล่าสุดในหน้าเส้นทางเอกสาร SML ได้
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 shrink-0 gap-2 bg-card"
            onClick={() => setRecreateRouteConfirmOpen(true)}
            disabled={recreatingRoute}
          >
            <RefreshCw className={recreatingRoute ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
            สร้างใหม่ตามเส้นทางปัจจุบัน
          </Button>
        </div>
      )}

      {(bill.error_msg || retryError) && (
        <BillFailureCard
          errorMsg={bill.error_msg}
          retryError={retryError}
          regeneratingDocNo={regeneratingDocNo}
          onRegenerateDocNo={handleRegenerateDocNo}
          smlReadiness={smlReadiness}
        />
      )}

      <BillTotal
        bill={bill}
        total={total}
        retrying={retrying}
        onRetry={handleSendClick}
        validation={validation}
        onJumpToItem={handleJumpToItem}
        expectedRoute={bill.preview?.route}
        expectedEndpoint={bill.preview?.endpoint}
        expectedDocFormat={bill.preview?.doc_format}
        smlReadiness={smlReadiness}
        smlReadinessLoading={smlReadinessLoading}
      />

      <BillItemsTable
        bill={bill}
        canEdit={canEdit}
        onItemUpdated={handleItemUpdated}
        onItemDeleted={handleItemDeleted}
        onItemAdded={handleItemAdded}
        onRefresh={reloadBill}
        highlightItemId={highlightItemId}
      />

      <section className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-3 border-b border-border/70 pb-2">
          <div>
            <h3 className="text-sm font-semibold text-foreground">ข้อมูลประกอบการตรวจสอบ</h3>
            <p className="mt-0.5 text-xs text-muted-foreground">
              ใช้เมื่อต้องย้อนดูหลักฐานต้นฉบับ ประวัติ และข้อมูลที่ส่งเข้า SML
            </p>
          </div>
          <span className="rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground">
            ข้อมูลส่วนนี้ไม่ต้องแก้ก่อนส่ง SML
          </span>
        </div>

        <div className="min-w-0 space-y-4">
          <ArtifactList billId={bill.id} emailGroup={bill.email_group} />
          <BillTimeline billId={bill.id} shopeeEvents={bill.shopee_events ?? []} />
          <SmlPayloadSection
            smlPayload={bill.sml_payload}
            smlResponse={bill.sml_response}
          />
        </div>
      </section>

      {(bill.bill_type === 'purchase' || (bill.bill_type === 'sale' && (bill.source === 'shopee' || bill.source === 'lazada' || bill.source === 'tiktok'))) && (
        <SendPurchaseDialog
          open={sendDialogOpen}
          bill={bill}
          onConfirm={handlePurchaseConfirm}
          onCancel={() => setSendDialogOpen(false)}
          onRegenerateDocNo={handleFetchLatestDocNo}
          regeneratingDocNo={refreshingDocNo}
          smlReadiness={smlReadiness}
          smlReadinessLoading={smlReadinessLoading}
        />
      )}
      <SMLSendProgressDialog
        open={sendProgress.open}
        status={sendProgress.status}
        docNo={sendProgress.docNo}
        error={sendProgress.error}
        onClose={() => setSendProgress((prev) => ({ ...prev, open: false }))}
      />
      <ConfirmDialog
        open={directSendConfirmOpen}
        onOpenChange={setDirectSendConfirmOpen}
        title="ยืนยันส่งเอกสารเข้า SML?"
        description={directSendDescription}
        confirmLabel="ส่งเข้า SML 1 ใบ"
        variant={bill.status === 'failed' ? 'destructive' : 'default'}
        onConfirm={handleDirectSendConfirm}
      />
      <ConfirmDialog
        open={recreateRouteConfirmOpen}
        onOpenChange={setRecreateRouteConfirmOpen}
        title="สร้างเอกสารใหม่ตามเส้นทางปัจจุบัน?"
        description={[
          'ระบบจะเก็บเอกสารเดิมไว้ใน Nexflow และปลด order นี้กลับไปที่ Shopee Realtime',
          'ยังไม่ส่งเข้า SML และไม่ลบเอกสารใน SML เพราะเอกสารนี้ยังไม่มีเลข SML',
          'หลังยืนยัน ให้กด “สร้างเอกสาร” ใน Shopee Realtime อีกครั้ง ระบบจะใช้เส้นทางล่าสุดจากหน้าเส้นทางเอกสาร SML',
        ].join('\n')}
        confirmLabel="ปลดเอกสารเดิม"
        variant="destructive"
        onConfirm={handleRecreateRouteConfirm}
      />
    </div>
  )
}
