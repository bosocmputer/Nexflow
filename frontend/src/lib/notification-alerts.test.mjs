import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { isOrderAlertNotification } = await vite.ssrLoadModule('/src/lib/notification-alerts.ts')

test.after(async () => {
  await vite.close()
})

test('treats TikTok Shop, Shopee, and NextStep new-order notifications as audible order alerts', () => {
  for (const dedupeKey of [
    'tiktok_shop:new_order:shop-1:order-1',
    'shopee:new_order:264993963:order-2',
    'nextstep:new_order:doc-3',
  ]) {
    assert.equal(isOrderAlertNotification({ dedupe_key: dedupeKey }), true, dedupeKey)
  }
})

test('does not alarm for non-order TikTok notifications', () => {
  assert.equal(isOrderAlertNotification({ dedupe_key: 'tiktok_shop:cancelled:shop-1:order-1' }), false)
  assert.equal(isOrderAlertNotification({}), false)
})
