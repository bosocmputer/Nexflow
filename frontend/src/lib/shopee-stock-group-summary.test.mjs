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
  sml_total_status: 'ready',
  shopee_stock_total: 42,
  target_stock_total: 48,
  target_count: 3,
  changed_count: 2,
}

test('shows collapsed parent totals after a fresh stock check', () => {
  assert.deepEqual(buildShopeeStockGroupSummary(readyGroup, false), {
    smlValue: 48,
    smlUnit: 'ชิ้น',
    smlStatusText: '',
    shopeeValue: 42,
    targetValue: 48,
    targetStatusText: '',
    changeText: 'เปลี่ยน 2 ตัวเลือก',
  })
})

test('does not present an old target as current after settings become stale', () => {
  assert.deepEqual(buildShopeeStockGroupSummary(readyGroup, true), {
    smlValue: null,
    smlUnit: '',
    smlStatusText: 'ตรวจใหม่',
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
    sml_total_status: 'mixed_unit',
    target_stock_total: null,
    target_count: 1,
  }, false), {
    smlValue: null,
    smlUnit: '',
    smlStatusText: 'หลายหน่วย',
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
    sml_total_status: 'shared_source',
  }, false), {
    smlValue: null,
    smlUnit: '',
    smlStatusText: 'สต๊อกร่วม',
    shopeeValue: 42,
    targetValue: 48,
    targetStatusText: '',
    changeText: 'เปลี่ยน 2 ตัวเลือก',
  })
})
