import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { marketplacePendingSummary } = await vite.ssrLoadModule('/src/lib/marketplace-review.ts')

test.after(async () => {
  await vite.close()
})

test('describes a catalog-only Shopee product without claiming it is an order', () => {
  assert.deepEqual(
    marketplacePendingSummary({ catalog_product: true, item_count: 0, bill_count: 0 }),
    { primary: 'รอจับคู่', secondary: 'จากรายการสินค้า Shopee' },
  )
})

test('keeps pending order counts for products discovered from sales documents', () => {
  assert.deepEqual(
    marketplacePendingSummary({ catalog_product: false, item_count: 3, bill_count: 2 }),
    { primary: '3 รายการ', secondary: '2 บิล' },
  )
})
