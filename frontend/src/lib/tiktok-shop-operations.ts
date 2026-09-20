export type TikTokSyncState = 'active' | 'error' | 'server_disabled' | 'shop_disabled'
export type TikTokStatusGroup = 'all' | 'unpaid' | 'to_ship' | 'shipping' | 'completed' | 'cancelled'

export interface TikTokStatusCounts {
  total: number
  unpaid: number
  to_ship: number
  shipping: number
  completed: number
  cancelled: number
}

export interface TikTokDocumentStateInput {
  billID?: string
  billStatus?: string
  smlDocNo?: string
  documentPath?: string
  cancellation?: TikTokCancellationRecord
}

export interface TikTokCancellationRecord {
  status: string
  cancelSMLDocNo?: string
  errorCode?: string
  errorMessage?: string
  stockRecalcStatus?: string
  stockRecalcError?: string
}

export interface TikTokDocumentState {
  label: string
  detail: string
  tone: 'muted' | 'warning' | 'danger' | 'success'
  path?: string
}

export interface TikTokCancellationState extends TikTokDocumentState {
  status: 'not_required' | 'evidence_missing' | 'review_required' | 'previewed' | 'creating' | 'completed' | 'failed' | 'reconciliation_required'
  canReviewCancellation: boolean
}

export interface TikTokRowActionsInput {
  billID?: string
  documentPath?: string
}

export interface TikTokRowActions {
  primary: 'create_document' | 'open_document'
  primaryLabel: 'สร้างเอกสาร' | 'เอกสาร'
  detailsLabel: 'รายละเอียด'
}

const TIKTOK_STATUS_GROUPS: TikTokStatusGroup[] = ['all', 'unpaid', 'to_ship', 'shipping', 'completed', 'cancelled']

const STATUS_LABELS: Record<string, string> = {
  UNPAID: 'ยังไม่ชำระ',
  ON_HOLD: 'พักรายการ',
  AWAITING_SHIPMENT: 'รอจัดส่ง',
  PARTIALLY_SHIPPING: 'จัดส่งบางส่วน',
  AWAITING_COLLECTION: 'รอรับพัสดุ',
  IN_TRANSIT: 'กำลังขนส่ง',
  DELIVERED: 'จัดส่งแล้ว',
  COMPLETED: 'สำเร็จ',
  CANCELLED: 'ยกเลิก',
}

export function canCreateTikTokReviewedBill(role?: string): boolean {
  return role === 'admin' || role === 'staff'
}

export function tiktokOrderStatusLabel(value: string): string {
  return STATUS_LABELS[value] ?? (value || 'ไม่ระบุสถานะ')
}

export function formatTikTokMoney(value: string, currency: string): string {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '—'
  try {
    return new Intl.NumberFormat('th-TH', {
      style: 'currency',
      currency: currency || 'THB',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    return '—'
  }
}

export function tiktokSyncState(workerEnabled: boolean, shopEnabled: boolean, errorCode?: string): TikTokSyncState {
  if (!workerEnabled) return 'server_disabled'
  if (!shopEnabled) return 'shop_disabled'
  if (errorCode?.trim()) return 'error'
  return 'active'
}

export function normalizeTikTokStatusGroup(value: string | null | undefined): TikTokStatusGroup {
  return TIKTOK_STATUS_GROUPS.includes(value as TikTokStatusGroup) ? value as TikTokStatusGroup : 'all'
}

export function tiktokStatusGroupCount(counts: TikTokStatusCounts, group: TikTokStatusGroup): number {
  return group === 'all' ? counts.total : counts[group]
}

export function tiktokDocumentState(input: TikTokDocumentStateInput): TikTokDocumentState {
  const billID = input.billID?.trim() ?? ''
  const billStatus = input.billStatus?.trim().toLowerCase() ?? ''
  const smlDocNo = input.smlDocNo?.trim() ?? ''
  const documentPath = input.documentPath?.trim() ?? ''
  const path = billID && documentPath ? documentPath : undefined

  if (!billID) {
    return { label: 'รอสร้างเอกสาร', detail: 'ยังไม่มีเอกสารขายใน Nexflow', tone: 'muted' }
  }
  if (smlDocNo || billStatus === 'sent') {
    return { label: 'ส่ง SML แล้ว', detail: smlDocNo || 'บันทึกเข้า SML แล้ว', tone: 'success', ...(path ? { path } : {}) }
  }
  if (billStatus === 'failed') {
    return { label: 'ส่ง SML ไม่สำเร็จ', detail: 'เปิดเอกสารเพื่อตรวจสอบ', tone: 'danger', ...(path ? { path } : {}) }
  }
  return {
    label: 'สร้างเอกสารแล้ว',
    detail: billStatus === 'needs_review' ? 'ต้องตรวจข้อมูลก่อนส่ง SML' : 'ยังไม่ส่ง SML',
    tone: 'warning',
    ...(path ? { path } : {}),
  }
}

export function tiktokCancellationState(input: TikTokDocumentStateInput): TikTokCancellationState {
  const billID = input.billID?.trim() ?? ''
  const billStatus = input.billStatus?.trim().toLowerCase() ?? ''
  const smlDocNo = input.smlDocNo?.trim() ?? ''
  const documentPath = input.documentPath?.trim() ?? ''
  const path = billID && documentPath ? documentPath : undefined
  const cancellation = input.cancellation

  if (!billID) {
    return {
      status: 'not_required',
      label: 'ไม่ต้องสร้างเอกสารยกเลิก',
      detail: 'ออเดอร์นี้ไม่มีใบขายใน Nexflow หรือ SML',
      tone: 'muted',
      canReviewCancellation: false,
    }
  }
  if (!smlDocNo && billStatus !== 'sent') {
    return {
      status: 'not_required',
      label: 'ไม่ต้องสร้างเอกสารยกเลิก SML',
      detail: 'ใบขายเดิมยังไม่เคยส่งเข้า SML',
      tone: 'muted',
      ...(path ? { path } : {}),
      canReviewCancellation: false,
    }
  }
  if (!smlDocNo) {
    return {
      status: 'evidence_missing',
      label: 'ต้องตรวจเอกสารเดิม',
      detail: 'สถานะบอกว่าส่ง SML แล้ว แต่ไม่พบเลขเอกสาร SML',
      tone: 'danger',
      ...(path ? { path } : {}),
      canReviewCancellation: false,
    }
  }
  if (cancellation) {
    const status = cancellation.status?.trim().toLowerCase()
    const cancelDocNo = cancellation.cancelSMLDocNo?.trim() ?? ''
    const transition = cancelDocNo ? `${smlDocNo} → ${cancelDocNo}` : `ใบขาย ${smlDocNo}`
    if (status === 'created' || status === 'already_exists') {
      const stock = cancellation.stockRecalcStatus === 'succeeded'
        ? 'คำนวณสต๊อกแล้ว'
        : cancellation.stockRecalcStatus === 'manual_reconciliation'
          ? 'ต้องตรวจสต๊อก'
          : 'รอคำนวณสต๊อก'
      return { status: 'completed', label: 'ยกเลิกใน SML แล้ว', detail: `${transition} · ${stock}`, tone: 'success', ...(path ? { path } : {}), canReviewCancellation: true }
    }
    if (status === 'unknown') {
      return { status: 'reconciliation_required', label: 'ต้องตรวจผลใน SML', detail: `${transition} · ห้ามออกเลขใหม่`, tone: 'danger', ...(path ? { path } : {}), canReviewCancellation: true }
    }
    if (status === 'creating') {
      return { status: 'creating', label: 'กำลังสร้างเอกสารยกเลิก', detail: transition, tone: 'warning', ...(path ? { path } : {}), canReviewCancellation: true }
    }
    if (status === 'previewed') {
      return { status: 'previewed', label: 'ตรวจ Preview แล้ว', detail: `ใบขาย ${smlDocNo} · รอยืนยันสร้าง`, tone: 'warning', ...(path ? { path } : {}), canReviewCancellation: true }
    }
    if (status === 'failed' || status === 'blocked') {
      return { status: 'failed', label: 'สร้างเอกสารยกเลิกไม่สำเร็จ', detail: cancellation.errorMessage?.trim() || transition, tone: 'danger', ...(path ? { path } : {}), canReviewCancellation: true }
    }
  }
  return {
    status: 'review_required',
    label: 'รอตรวจเอกสารยกเลิก',
    detail: `ใบขาย ${smlDocNo} ถูกส่งเข้า SML แล้ว`,
    tone: 'warning',
    ...(path ? { path } : {}),
    canReviewCancellation: true,
  }
}

export function tiktokRowActions(input: TikTokRowActionsInput): TikTokRowActions {
  const hasDocument = Boolean(input.billID?.trim() && input.documentPath?.trim())
  return {
    primary: hasDocument ? 'open_document' : 'create_document',
    primaryLabel: hasDocument ? 'เอกสาร' : 'สร้างเอกสาร',
    detailsLabel: 'รายละเอียด',
  }
}

export function tiktokBillShadowReadinessLabel(ready: boolean, blockerCount: number): string {
  if (ready) return 'ข้อมูลพร้อมสำหรับตรวจและสร้างเอกสาร'
  return `ต้องแก้ไข ${Math.max(0, blockerCount).toLocaleString('th-TH')} จุดก่อนสร้างเอกสาร`
}

export function tiktokBillShadowMappingLabel(status: string): string {
  if (status === 'ready') return 'พร้อมใช้'
  if (status === 'missing') return 'ยังไม่ได้จับคู่'
  if (status === 'legacy_unscoped') return 'ต้องยืนยันร้าน'
  return 'ยังไม่พร้อม'
}

export function tiktokBillShadowRouteLabel(route: string): string {
  if (route === 'sale_invoice') return 'ขายสินค้าและบริการ / SI'
  if (route === 'sale_order') return 'ใบสั่งขาย'
  return 'ยังไม่ได้ตั้งค่า'
}

export interface TikTokShadowMappingPayloadInput {
  productID: string
  skuID: string
  itemCode: string
  unitCode: string
  quantityMultiplier: number
}

export interface TikTokShadowMappingPayload {
  product_id: string
  sku_id: string
  item_code: string
  unit_code: string
  quantity_multiplier: number
}

export function buildTikTokShadowMappingPayload(input: TikTokShadowMappingPayloadInput): TikTokShadowMappingPayload {
  return {
    product_id: input.productID.trim(),
    sku_id: input.skuID.trim(),
    item_code: input.itemCode.trim(),
    unit_code: input.unitCode.trim(),
    quantity_multiplier: input.quantityMultiplier,
  }
}

export function tiktokShadowMappingValidation(itemCode: string, unitCode: string, quantityMultiplier: number): string {
  if (!itemCode.trim()) return 'กรุณาเลือกสินค้า SML'
  if (!unitCode.trim()) return 'กรุณาเลือกหน่วย SML'
  if (!Number.isInteger(quantityMultiplier) || quantityMultiplier < 1 || quantityMultiplier > 1_000_000) {
    return 'จำนวนต้องเป็นเลขจำนวนเต็ม 1 ถึง 1,000,000'
  }
  return ''
}

export interface TikTokReviewedBillRequest {
  confirm: 'CREATE_REVIEWED_BILL'
  review_digest: string
}

export function buildTikTokReviewedBillRequest(reviewDigest: string): TikTokReviewedBillRequest {
  const digest = reviewDigest.trim()
  if (!/^[a-f0-9]{64}$/.test(digest)) {
    throw new Error('invalid review digest')
  }
  return {
    confirm: 'CREATE_REVIEWED_BILL',
    review_digest: digest,
  }
}

export interface TikTokCancellationRequest {
  confirm: 'CREATE_TIKTOK_SML_CANCEL_DOCUMENT'
  review_digest: string
}

export function buildTikTokCancellationRequest(reviewDigest: string): TikTokCancellationRequest {
  const digest = reviewDigest.trim()
  if (!/^[a-f0-9]{64}$/.test(digest)) {
    throw new Error('invalid review digest')
  }
  return { confirm: 'CREATE_TIKTOK_SML_CANCEL_DOCUMENT', review_digest: digest }
}
