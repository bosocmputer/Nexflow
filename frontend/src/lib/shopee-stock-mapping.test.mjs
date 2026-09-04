import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { shopeeStockMutationRawName, shopeeStockMutationSourceSKU } = await vite.ssrLoadModule(
  '/src/lib/shopee-stock-mapping.ts',
)

test.after(async () => {
  await vite.close()
})

test('uses the same canonical raw name as the stock mapping commit', () => {
  assert.equal(
    shopeeStockMutationRawName(
      'อัยน่าเอส กล่องขาว วีเนสต้า กล่องเหลือ',
      '1ก+ดีท็อก 1 แผง',
    ),
    '1ก+ดีท็อก 1 แผง',
  )
})

test('falls back to the item name and trims Shopee catalog whitespace', () => {
  assert.equal(shopeeStockMutationRawName('  สินค้าหลัก  ', '  '), 'สินค้าหลัก')
  assert.equal(shopeeStockMutationRawName('  สินค้าหลัก  ', '  ตัวเลือก  '), 'ตัวเลือก')
})

test('uses the model SKU first and ignores whitespace-only catalog values', () => {
  assert.equal(shopeeStockMutationSourceSKU('  ITEM-SKU  ', '  MODEL-SKU  '), 'MODEL-SKU')
  assert.equal(shopeeStockMutationSourceSKU('  ITEM-SKU  ', '   '), 'ITEM-SKU')
})
