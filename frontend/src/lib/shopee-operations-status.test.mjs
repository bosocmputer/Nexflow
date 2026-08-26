import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  cancellationDocumentTypeLabel,
  cancellationStockRecalcLabel,
  cancellationTriggerLabel,
  shouldMergeAutoSMLSuccessStatus,
} = await vite.ssrLoadModule(
  '/src/lib/shopee-operations-status.ts',
)

test.after(async () => {
  await vite.close()
})

test('merges a completed Auto SML result into the sent ERP status', () => {
  assert.equal(shouldMergeAutoSMLSuccessStatus('sent', 'BF-INV26080055', 'succeeded'), true)
})

test('keeps non-success Auto SML states as a separate status row', () => {
  assert.equal(shouldMergeAutoSMLSuccessStatus('sent', 'BF-INV26080055', 'needs_review'), false)
  assert.equal(shouldMergeAutoSMLSuccessStatus('failed', 'BF-INV26080055', 'succeeded'), false)
})

test('does not claim Auto SML completion without an SML document number', () => {
  assert.equal(shouldMergeAutoSMLSuccessStatus('sent', '', 'succeeded'), false)
})

test('labels the two verified SML cancellation document types', () => {
  assert.equal(cancellationDocumentTypeLabel('sale_cancel'), 'ยกเลิกขายสินค้าและบริการ')
  assert.equal(cancellationDocumentTypeLabel('credit_note'), 'รับคืนสินค้า/ลดหนี้')
  assert.equal(cancellationDocumentTypeLabel(''), 'รอสร้างตามเส้นทางที่ตั้งไว้')
})

test('labels automatic and manual cancellation creation without exposing implementation terms', () => {
  assert.equal(cancellationTriggerLabel('auto'), 'AUTO')
  assert.equal(cancellationTriggerLabel('manual'), 'ผู้ใช้สร้าง')
  assert.equal(cancellationTriggerLabel(''), '')
})

test('shows the durable SML stock recalculation result', () => {
  assert.deepEqual(cancellationStockRecalcLabel('succeeded'), {
    label: 'คำนวณสต๊อก SML แล้ว',
    tone: 'success',
  })
  assert.deepEqual(cancellationStockRecalcLabel('running'), {
    label: 'กำลังคำนวณสต๊อก SML',
    tone: 'info',
  })
  assert.deepEqual(cancellationStockRecalcLabel('manual_reconciliation'), {
    label: 'ต้องตรวจการคำนวณสต๊อก',
    tone: 'danger',
  })
})
