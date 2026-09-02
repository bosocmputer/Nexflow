import assert from 'node:assert/strict'
import test from 'node:test'

import { documentItems, documentLocation, documentSendPresentation } from './smlPayloadSummary.js'

test('summarizes Profile V1 sale invoice details and their SML location', () => {
  const payload = {
    details: [
      { item_code: 'AH-0007', wh_code: 'AB-1', shelf_code: '001' },
      { item_code: 'AH-0061', wh_code: 'AB-1', shelf_code: '001' },
    ],
  }

  assert.equal(documentItems(payload).length, 2)
  assert.deepEqual(documentLocation(payload), { whCode: 'AB-1', shelfCode: '001' })
})

test('keeps legacy items and header location compatible', () => {
  const payload = {
    items: [{ item_code: 'LEGACY-1', wh_code: 'ITEM-WH', shelf_code: 'ITEM-SHELF' }],
    wh_code: 'HEADER-WH',
    shelf_code: 'HEADER-SHELF',
  }

  assert.equal(documentItems(payload).length, 1)
  assert.deepEqual(documentLocation(payload), { whCode: 'HEADER-WH', shelfCode: 'HEADER-SHELF' })
})

test('collapses a fully successful automatic send into one plain-language result', () => {
  assert.deepEqual(documentSendPresentation({
    status: 'sent',
    sml_sent_automatically: true,
    sml_profile_version: 'sml-document-v1',
    sml_profile_status: 'complete',
    sml_stock_job_status: 'completed',
  }), {
    complete: true,
    headline: 'ส่งเข้า SML แล้ว (AUTO)',
    detail: 'สร้างเอกสารและคำนวณต้นทุนสต๊อกเรียบร้อย',
  })
})

test('keeps recovery details visible when work after document creation is incomplete', () => {
  const result = documentSendPresentation({
    status: 'sent',
    sml_sent_automatically: false,
    sml_profile_version: 'sml-document-v1',
    sml_profile_status: 'needs_reconciliation',
    sml_stock_job_status: 'completed',
  })
  assert.equal(result.complete, false)
  assert.equal(result.headline, 'ส่งเข้า SML แล้ว')
})
