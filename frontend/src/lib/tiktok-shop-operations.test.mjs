import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  formatTikTokMoney,
  normalizeTikTokStatusGroup,
  tiktokStatusGroupCount,
  tiktokOrderStatusLabel,
  tiktokSyncState,
} = await vite.ssrLoadModule('/src/lib/tiktok-shop-operations.ts')

test.after(async () => {
  await vite.close()
})

test('formats safe order amounts and unknown values without inventing revenue', () => {
  assert.equal(formatTikTokMoney('307.490000', 'THB'), '฿307.49')
  assert.equal(formatTikTokMoney('bad', 'THB'), '—')
  assert.equal(formatTikTokMoney('12.5', 'USD'), 'US$12.50')
})

test('translates known TikTok lifecycle states and preserves unknown states safely', () => {
  assert.equal(tiktokOrderStatusLabel('AWAITING_SHIPMENT'), 'รอจัดส่ง')
  assert.equal(tiktokOrderStatusLabel('COMPLETED'), 'สำเร็จ')
  assert.equal(tiktokOrderStatusLabel('FUTURE_STATE'), 'FUTURE_STATE')
})

test('sync state remains fail-closed when either worker or shop is disabled', () => {
  assert.equal(tiktokSyncState(true, true, ''), 'active')
  assert.equal(tiktokSyncState(false, true, ''), 'server_disabled')
  assert.equal(tiktokSyncState(true, false, ''), 'shop_disabled')
  assert.equal(tiktokSyncState(true, true, 'gateway_timeout'), 'error')
  assert.equal(tiktokSyncState(true, true, undefined), 'active')
})

test('normalizes operations status tabs and reads their server counts', () => {
  const counts = { total: 9, unpaid: 1, to_ship: 2, shipping: 3, completed: 2, cancelled: 1 }
  assert.equal(normalizeTikTokStatusGroup('shipping'), 'shipping')
  assert.equal(normalizeTikTokStatusGroup('future'), 'all')
  assert.equal(tiktokStatusGroupCount(counts, 'all'), 9)
  assert.equal(tiktokStatusGroupCount(counts, 'to_ship'), 2)
})
