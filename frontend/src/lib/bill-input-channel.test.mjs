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

test('uses observed TikTok Shop API evidence instead of the legacy Excel fallback', () => {
  assert.deepEqual(
    marketplaceDisplayInputChannels('tiktok', { inputChannels: ['tiktok_shop'] }),
    ['tiktok_shop'],
  )
  assert.deepEqual(
    marketplaceDisplayInputChannels('tiktok', { inputChannels: ['tiktok_excel'] }),
    ['tiktok_excel'],
  )
})

test('falls back to the exact TikTok account scope when observed evidence is unavailable', () => {
  assert.deepEqual(
    marketplaceDisplayInputChannels('tiktok', { accountKey: 'shop:7494619203789490654' }),
    ['tiktok_shop'],
  )
  assert.deepEqual(
    marketplaceDisplayInputChannels('tiktok', { accountKey: 'default' }),
    ['tiktok_excel'],
  )
})
