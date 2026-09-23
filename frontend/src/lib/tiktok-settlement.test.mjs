import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { resolveTikTokSettlementShopID } = await vite.ssrLoadModule('/src/lib/tiktok-settlement.ts')

test.after(async () => {
  await vite.close()
})

test('uses the sole connected shop for an action when the selector has not committed state yet', () => {
  assert.equal(resolveTikTokSettlementShopID('', [{ shop_id: '7494619203789490654', disabled: false }]), '7494619203789490654')
})

test('never guesses a shop when more than one shop is connected', () => {
  assert.equal(resolveTikTokSettlementShopID('', [
    { shop_id: 'one', disabled: false },
    { shop_id: 'two', disabled: false },
  ]), '')
})

test('uses the explicit selection even when other shops are available', () => {
  assert.equal(resolveTikTokSettlementShopID('two', [{ shop_id: 'one' }, { shop_id: 'two' }]), 'two')
})
