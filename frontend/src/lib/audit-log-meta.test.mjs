import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  ACTION_META,
  SOURCE_LABELS,
  auditViaLabel,
  isSMLAuditLog,
  summarize,
} = await vite.ssrLoadModule('/src/lib/audit-log-meta.ts')

test.after(async () => {
  await vite.close()
})

function audit(action, detail = {}, source = 'shopee_realtime') {
  return {
    id: `test-${action}`,
    action,
    source,
    detail,
    created_at: '2026-08-27T01:56:40Z',
  }
}

test('uses Thai labels for every Shopee automatic SML cancellation milestone', () => {
  assert.equal(ACTION_META.shopee_sml_cancel_created_auto.label, 'สร้างเอกสารยกเลิก/รับคืนอัตโนมัติแล้ว')
  assert.equal(ACTION_META.shopee_sml_cancel_auto_failed.label, 'สร้างเอกสารยกเลิก/รับคืนอัตโนมัติไม่สำเร็จ')
  assert.equal(ACTION_META.shopee_sml_cancel_stock_recalc_ok.label, 'คำนวณสต๊อกหลังยกเลิกสำเร็จ')
  assert.equal(ACTION_META.shopee_sml_cancel_stock_recalc_failed.label, 'คำนวณสต๊อกหลังยกเลิกไม่สำเร็จ')
})

test('summarizes the verified AOY automatic cancellation without exposing raw keys', () => {
  const text = summarize(audit('shopee_sml_cancel_created_auto', {
    order_sn: '260827ECCFMCSC',
    sale_sml_doc_no: 'BF-INV26080060',
    cancel_sml_doc_no: 'CN26080002',
    trigger_source: 'auto',
  }))

  assert.equal(text, 'ออเดอร์ 260827ECCFMCSC · BF-INV26080060 → CN26080002 · อัตโนมัติ')
})

test('summarizes cancellation stock recalculation and failures in Thai', () => {
  assert.equal(summarize(audit('shopee_sml_cancel_stock_recalc_ok', {
    order_sn: '260827ECCFMCSC',
    cancel_doc_no: 'CN26080002',
    item_count: 2,
  }, 'sml')), 'CN26080002 · 2 รหัสสินค้า · ออเดอร์ 260827ECCFMCSC')

  assert.equal(summarize(audit('shopee_sml_cancel_auto_failed', {
    order_sn: '260827ECCFMCSC',
    sale_sml_doc_no: 'BF-INV26080060',
    error: 'route changed',
  })), 'ออเดอร์ 260827ECCFMCSC · BF-INV26080060 · route changed')
})

test('labels realtime Shopee and Auto SML send details for operators', () => {
  assert.equal(SOURCE_LABELS.shopee_realtime, 'Shopee API')
  assert.equal(auditViaLabel('shopee_auto_sml'), 'ส่งอัตโนมัติจาก Shopee')
  assert.equal(summarize(audit('sml_sent', {
    doc_no: 'BF-INV26080060',
    route: 'SaleInvoice',
    via: 'shopee_auto_sml',
  }, 'shopee')), 'BF-INV26080060 · ขาย -> ขายสินค้าและบริการ · ส่งอัตโนมัติจาก Shopee')
})

test('includes Shopee cancellation lifecycle events in the SML quick view', () => {
  assert.equal(isSMLAuditLog(audit('shopee_sml_cancel_created_auto')), true)
  assert.equal(isSMLAuditLog(audit('shopee_sml_cancel_stock_recalc_ok', {}, 'sml')), true)
  assert.equal(isSMLAuditLog(audit('bill_created')), false)
})

test('recognizes Shopee realtime bill creation details', () => {
  assert.equal(summarize(audit('bill_created', {
    via: 'shopee_realtime',
    order_id: '260827ECCFMCSC',
    items_count: 2,
  })), 'ออเดอร์ 260827ECCFMCSC · 2 รายการ')
})

test('summarizes Auto SML setting changes without raw Shopee status keys', () => {
  assert.equal(summarize(audit('shopee_auto_sml_setting_updated', {
    before: { enabled: true, trigger_status: 'READY_TO_SHIP', config_version: 2 },
    after: { enabled: true, trigger_status: 'PROCESSED', config_version: 3 },
  })), 'เปิดใช้งาน · รอจัดส่ง → เตรียมจัดส่งแล้ว · รุ่นตั้งค่า 3')
})

test('labels SML recovery in plain Thai without implying that the document is resent', () => {
  assert.equal(ACTION_META.core_committed.label, 'สร้างเอกสาร SML แล้ว')
  assert.equal(ACTION_META.profile_terminal_failure.label, 'ข้อมูลประกอบ SML ต้องให้ผู้ดูแลแก้ไข')
  assert.equal(summarize(audit('profile_requested', {
    profile_version: 'sml-document-v1',
    via: 'shopee_auto_sml',
    config_version: 2,
  }, 'sml')), 'ส่งอัตโนมัติจาก Shopee')
  assert.equal(summarize(audit('profile_complete', {
    profile_version: 'sml-document-v1',
    completed_checks: ['header', 'detail', 'vat', 'shipment', 'log'],
  }, 'sml')), 'ผ่านการตรวจ 5 ขั้นตอน')
  assert.equal(summarize(audit('profile_retry_requested', {
    profile_version: 'sml-document-v1',
    manual_retry_count: 2,
  }, 'sml')), 'ไม่สร้างบิลซ้ำ · ผู้ดูแลลองครั้งที่ 2')
  assert.equal(isSMLAuditLog(audit('profile_complete', {}, 'sml')), true)
})
