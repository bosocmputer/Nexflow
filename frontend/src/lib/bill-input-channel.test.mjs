import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { classifyBillInputChannel, marketplaceDisplayInputChannels } = await vite.ssrLoadModule('/src/lib/billInputChannel.ts')

test.after(async () => {
  await vite.close()
})

test('shows a catalog-synced Shopee product as API only before any Excel use', () => {
  assert.deepEqual(
    marketplaceDisplayInputChannels('shopee', { catalogProduct: true }),
    ['shopee'],
  )
})

test('shows only channels with observed Product Master use', () => {
  assert.deepEqual(
    marketplaceDisplayInputChannels('shopee', { inputChannels: ['shopee'] }),
    ['shopee'],
  )
  assert.deepEqual(
    marketplaceDisplayInputChannels('shopee', { inputChannels: ['shopee', 'shopee_excel'] }),
    ['shopee', 'shopee_excel'],
  )
})

test('rejects unknown channel evidence and keeps the legacy shared fallback', () => {
  assert.deepEqual(
    marketplaceDisplayInputChannels('shopee', { inputChannels: ['bad_channel'] }),
    ['shopee', 'shopee_excel'],
  )
})

test('keeps TikTok Shop API separate from TikTok Excel', () => {
  assert.equal(
    classifyBillInputChannel({ source: 'tiktok', raw_data: { flow: 'tiktok_shop_api_reviewed' } }),
    'tiktok_shop',
  )
  assert.equal(
    classifyBillInputChannel({ source: 'tiktok', raw_data: { flow: 'tiktok_excel' } }),
    'tiktok_excel',
  )
})
