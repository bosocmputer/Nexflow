import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  buildTikTokReviewedBillRequest,
  buildTikTokShadowMappingPayload,
  formatTikTokMoney,
  normalizeTikTokStatusGroup,
  tiktokStatusGroupCount,
  tiktokOrderStatusLabel,
  tiktokBillShadowMappingLabel,
  tiktokBillShadowRouteLabel,
  tiktokBillShadowReadinessLabel,
  tiktokDocumentState,
  tiktokShadowMappingValidation,
  tiktokSyncState,
} = await vite.ssrLoadModule('/src/lib/tiktok-shop-operations.ts')

test.after(async () => {
  await vite.close()
})

test('formats safe order amounts and unknown values without inventing revenue', () => {
  assert.equal(formatTikTokMoney('307.490000', 'THB'), '฿307.49')
  assert.equal(formatTikTokMoney('bad', 'THB'), '—')
  assert.equal(formatTikTokMoney('12.5', 'USD'), 'US$12.50')
})

test('translates known TikTok lifecycle states and preserves unknown states safely', () => {
  assert.equal(tiktokOrderStatusLabel('AWAITING_SHIPMENT'), 'รอจัดส่ง')
  assert.equal(tiktokOrderStatusLabel('COMPLETED'), 'สำเร็จ')
  assert.equal(tiktokOrderStatusLabel('FUTURE_STATE'), 'FUTURE_STATE')
})

test('sync state remains fail-closed when either worker or shop is disabled', () => {
  assert.equal(tiktokSyncState(true, true, ''), 'active')
  assert.equal(tiktokSyncState(false, true, ''), 'server_disabled')
  assert.equal(tiktokSyncState(true, false, ''), 'shop_disabled')
  assert.equal(tiktokSyncState(true, true, 'gateway_timeout'), 'error')
  assert.equal(tiktokSyncState(true, true, undefined), 'active')
})

test('normalizes operations status tabs and reads their server counts', () => {
  const counts = { total: 9, unpaid: 1, to_ship: 2, shipping: 3, completed: 2, cancelled: 1 }
  assert.equal(normalizeTikTokStatusGroup('shipping'), 'shipping')
  assert.equal(normalizeTikTokStatusGroup('future'), 'all')
  assert.equal(tiktokStatusGroupCount(counts, 'all'), 9)
  assert.equal(tiktokStatusGroupCount(counts, 'to_ship'), 2)
})

test('presents bill shadow state in operator language without exposing route internals', () => {
  assert.equal(tiktokBillShadowReadinessLabel(false, 1), 'ต้องแก้ไข 1 จุดก่อนสร้าง Bill')
  assert.equal(tiktokBillShadowReadinessLabel(true, 0), 'ข้อมูลพร้อมสำหรับขั้นตรวจทาน')
  assert.equal(tiktokBillShadowMappingLabel('ready'), 'พร้อมใช้')
  assert.equal(tiktokBillShadowMappingLabel('legacy_unscoped'), 'ต้องยืนยันร้าน')
  assert.equal(tiktokBillShadowMappingLabel('future_state'), 'ยังไม่พร้อม')
  assert.equal(tiktokBillShadowRouteLabel('sale_invoice'), 'ขายสินค้าและบริการ / SI')
  assert.equal(tiktokBillShadowRouteLabel('sale_order'), 'ใบสั่งขาย')
  assert.equal(tiktokBillShadowRouteLabel(''), 'ยังไม่ได้ตั้งค่า')
})

test('builds only the exact TikTok product and SKU mapping payload', () => {
  assert.deepEqual(buildTikTokShadowMappingPayload({
    productID: '1729429119195974110',
    skuID: '1729429118580984286',
    itemCode: ' AH-0006 ',
    unitCode: ' แท่ง ',
    quantityMultiplier: 2,
  }), {
    product_id: '1729429119195974110',
    sku_id: '1729429118580984286',
    item_code: 'AH-0006',
    unit_code: 'แท่ง',
    quantity_multiplier: 2,
  })
  assert.equal('account_key' in buildTikTokShadowMappingPayload({
    productID: '1', skuID: '2', itemCode: 'A', unitCode: 'ชิ้น', quantityMultiplier: 1,
  }), false)
})

test('blocks incomplete or unsafe TikTok shadow mapping selections', () => {
  assert.equal(tiktokShadowMappingValidation('', 'แท่ง', 1), 'กรุณาเลือกสินค้า SML')
  assert.equal(tiktokShadowMappingValidation('AH-0006', '', 1), 'กรุณาเลือกหน่วย SML')
  assert.equal(tiktokShadowMappingValidation('AH-0006', 'แท่ง', 0), 'จำนวนต้องเป็นเลขจำนวนเต็ม 1 ถึง 1,000,000')
  assert.equal(tiktokShadowMappingValidation('AH-0006', 'แท่ง', 1.5), 'จำนวนต้องเป็นเลขจำนวนเต็ม 1 ถึง 1,000,000')
  assert.equal(tiktokShadowMappingValidation('AH-0006', 'แท่ง', 1_000_001), 'จำนวนต้องเป็นเลขจำนวนเต็ม 1 ถึง 1,000,000')
  assert.equal(tiktokShadowMappingValidation('AH-0006', 'แท่ง', 2), '')
})

test('builds an explicit reviewed Bill confirmation and rejects stale-shaped evidence', () => {
  const digest = 'a'.repeat(64)
  assert.deepEqual(buildTikTokReviewedBillRequest(digest), {
    confirm: 'CREATE_REVIEWED_BILL',
    review_digest: digest,
  })
  assert.throws(() => buildTikTokReviewedBillRequest('A'.repeat(64)), /review digest/i)
  assert.throws(() => buildTikTokReviewedBillRequest('not-a-digest'), /review digest/i)
})

test('presents TikTok Nexflow and SML document states with the same operational vocabulary as Shopee', () => {
  assert.deepEqual(tiktokDocumentState({}), {
    label: 'รอสร้างเอกสาร',
    detail: 'ตรวจตัวอย่าง Bill ก่อนสร้าง',
    tone: 'muted',
  })
  assert.deepEqual(tiktokDocumentState({
    billID: '03ee1216-acb4-4a88-842c-7edc6eb44292',
    billStatus: 'pending',
    documentPath: '/sale-invoices/03ee1216-acb4-4a88-842c-7edc6eb44292',
  }), {
    label: 'สร้างเอกสารแล้ว',
    detail: 'ยังไม่ส่ง SML',
    tone: 'warning',
    path: '/sale-invoices/03ee1216-acb4-4a88-842c-7edc6eb44292',
  })
  assert.deepEqual(tiktokDocumentState({
    billID: '03ee1216-acb4-4a88-842c-7edc6eb44292',
    billStatus: 'sent',
    smlDocNo: 'BF-INV26090001',
    documentPath: '/sale-invoices/03ee1216-acb4-4a88-842c-7edc6eb44292',
  }), {
    label: 'ส่ง SML แล้ว',
    detail: 'BF-INV26090001',
    tone: 'success',
    path: '/sale-invoices/03ee1216-acb4-4a88-842c-7edc6eb44292',
  })
})
