import assert from 'node:assert/strict'
import test from 'node:test'
import { createServer } from 'vite'

const vite = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
const { marketplaceSMLRouteBundleConfig } = await vite.ssrLoadModule('/src/lib/marketplace-sml-route-bundle.ts')

test.after(async () => {
  await vite.close()
})

test('keeps TikTok cancellation-only while matching the Shopee bundle workflow', () => {
  const config = marketplaceSMLRouteBundleConfig('tiktok_shop')

  assert.equal(config.apiBase, '/api/settings/tiktok-shop-sml-route-bundle')
  assert.equal(config.mainChannel, 'tiktok_shop')
  assert.equal(config.cancelChannel, 'tiktok_shop_cancel')
  assert.equal(config.title, 'ตั้งค่าเอกสาร SML สำหรับคำสั่งซื้อ TikTok Shop')
  assert.equal(config.cancelHeading, '2. เมื่อ TikTok Shop ยกเลิกคำสั่งซื้อ')
  assert.deepEqual(config.saleInvoiceCancellationRoutes, ['saleinvoicecancel'])
})

test('preserves Shopee credit-note and void choices', () => {
  const config = marketplaceSMLRouteBundleConfig('shopee')

  assert.equal(config.apiBase, '/api/settings/shopee-sml-route-bundle')
  assert.equal(config.mainChannel, 'shopee_realtime')
  assert.equal(config.cancelChannel, 'shopee_realtime_cancel')
  assert.deepEqual(config.saleInvoiceCancellationRoutes, ['creditnote', 'saleinvoicecancel'])
})
