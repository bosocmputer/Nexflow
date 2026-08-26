import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { buildShopeeStockGroupSummary } = await vite.ssrLoadModule(
  '/src/lib/shopee-stock-group-summary.ts',
)

test.after(async () => {
  await vite.close()
})

const readyGroup = {
  summary_count: 3,
  sml_usable_total: 48,
  sml_base_unit_code: 'PCS',
  sml_base_unit_name: 'ชิ้น',
  sml_unit_totals: [
    { unit_code: 'PCS', unit_name: 'ชิ้น', quantity: 48, source_count: 3 },
  ],
  sml_total_status: 'ready',
  shopee_stock_total: 42,
  target_stock_total: 48,
  target_count: 3,
  changed_count: 2,
}

test('shows collapsed parent totals after a fresh stock check', () => {
  assert.deepEqual(buildShopeeStockGroupSummary(readyGroup, false), {
    smlText: '48 ชิ้น',
    smlWarningText: '',
    smlTitle: 'ยอด SML พร้อมใช้รวม 48 ชิ้น',
    shopeeValue: 42,
    targetValue: 48,
    targetStatusText: '',
    changeText: 'เปลี่ยน 2 ตัวเลือก',
  })
})

test('does not present an old target as current after settings become stale', () => {
  assert.deepEqual(buildShopeeStockGroupSummary(readyGroup, true), {
    smlText: '48 ชิ้น',
    smlWarningText: 'ข้อมูลเดิม ควรตรวจใหม่',
    smlTitle: 'ยอดจากการตรวจครั้งก่อน: 48 ชิ้น · ข้อมูลเปลี่ยนแล้ว กรุณากดบันทึกและตรวจสต๊อกอีกครั้ง',
    shopeeValue: 42,
    targetValue: null,
    targetStatusText: 'ตรวจใหม่',
    changeText: '',
  })
})

test('fails closed when SML quantities cannot be safely combined', () => {
  assert.deepEqual(buildShopeeStockGroupSummary({
    ...readyGroup,
    sml_usable_total: null,
    sml_unit_totals: [
      { unit_code: 'STICK', unit_name: 'แท่ง', quantity: 12, source_count: 2 },
      { unit_code: 'PACK', unit_name: 'แพ็ค', quantity: 3, source_count: 1 },
    ],
    sml_total_status: 'mixed_unit',
    target_stock_total: null,
    target_count: 1,
  }, false), {
    smlText: '12 แท่ง · 3 แพ็ค',
    smlWarningText: 'มีหลายหน่วย · ควรตรวจตัวเลือก',
    smlTitle: 'ยอด SML พร้อมใช้แยกตามหน่วย: 12 แท่ง · 3 แพ็ค · หน่วยต่างกันจึงไม่นำมาบวกกัน',
    shopeeValue: 42,
    targetValue: null,
    targetStatusText: 'รอตรวจ 2 ตัวเลือก',
    changeText: '',
  })
})

test('does not double-count a shared SML stock source', () => {
  assert.deepEqual(buildShopeeStockGroupSummary({
    ...readyGroup,
    sml_usable_total: null,
    sml_unit_totals: [
      { unit_code: 'BOX', unit_name: 'กล่อง', quantity: 91, source_count: 3 },
    ],
    sml_total_status: 'shared_source',
  }, false), {
    smlText: '91 กล่อง',
    smlWarningText: 'ใช้ร่วมกับสินค้าอื่น · ควรตรวจตัวเลือก',
    smlTitle: 'ยอด SML พร้อมใช้ 91 กล่อง · ยอดนี้ใช้ร่วมกับสินค้า Shopee รายการอื่น ระบบไม่ได้นำยอดซ้ำมาบวกในแถวนี้',
    shopeeValue: 42,
    targetValue: 48,
    targetStatusText: '',
    changeText: 'เปลี่ยน 2 ตัวเลือก',
  })
})
