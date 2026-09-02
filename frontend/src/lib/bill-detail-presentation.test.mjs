import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  SML_INQUIRY_TYPE_FIELD_LABEL,
  artifactEvidenceState,
  billSMLStatusLabel,
  formatBangkokDateTime,
  smlInquiryTypeLabel,
} = await vite.ssrLoadModule('/src/pages/BillDetail/utils/presentation.ts')

test.after(async () => {
  await vite.close()
})

test('formats an ISO order timestamp in Asia/Bangkok using a Gregorian year', () => {
  assert.equal(
    formatBangkokDateTime('2026-09-02T09:43:16Z'),
    '02/09/2026 16:43 น.',
  )
})

test('preserves an invalid order timestamp so the original evidence stays visible', () => {
  assert.equal(formatBangkokDateTime('not-a-date'), 'not-a-date')
})

test('labels automatic and manual successful SML sends in the document header', () => {
  assert.equal(billSMLStatusLabel('sent', true), 'ส่งแล้ว (AUTO)')
  assert.equal(billSMLStatusLabel('sent', false), 'ส่งแล้ว')
  assert.equal(billSMLStatusLabel('pending', true), '')
})

test('shows the user-facing inquiry type without its technical numeric code', () => {
  assert.equal(SML_INQUIRY_TYPE_FIELD_LABEL, 'ประเภทรายการ')
  assert.equal(smlInquiryTypeLabel(0, 'sale'), 'ขายเงินเชื่อ')
  assert.equal(smlInquiryTypeLabel(1, 'sale'), 'ขายเงินสด')
  assert.equal(smlInquiryTypeLabel(2, 'sale'), 'ขายเงินเชื่อ (สินค้าบริการ)')
  assert.equal(smlInquiryTypeLabel(3, 'sale'), 'ขายเงินสด (สินค้าบริการ)')
})

test('keeps purchase inquiry_type semantics and exposes missing values plainly', () => {
  assert.equal(smlInquiryTypeLabel('1', 'purchase'), 'ซื้อสินค้าเงินสด')
  assert.equal(smlInquiryTypeLabel(undefined, 'sale'), '—')
  assert.equal(smlInquiryTypeLabel(undefined, 'sale', 0), 'ขายเงินเชื่อ')
  assert.equal(smlInquiryTypeLabel(9, 'sale'), 'ไม่ทราบประเภทรายการ')
})

test('hides an empty evidence card for Shopee API while preserving real files and other sources', () => {
  assert.equal(artifactEvidenceState(0, true), 'hidden')
  assert.equal(artifactEvidenceState(1, true), 'list')
  assert.equal(artifactEvidenceState(0, false), 'empty')
  assert.equal(artifactEvidenceState(2, false), 'list')
})
