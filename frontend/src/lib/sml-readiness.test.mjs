import assert from 'node:assert/strict'
import test from 'node:test'
import { createServer } from 'vite'

const vite = await createServer({ appType: 'custom', logLevel: 'error', server: { middlewareMode: true } })
const { canQueueBillForSML } = await vite.ssrLoadModule('/src/lib/sml-readiness.ts')
test.after(async () => { await vite.close() })

test('bulk send excludes a cancelled TikTok Bill even when its items validate', () => {
  assert.equal(canQueueBillForSML(true, false), false)
  assert.equal(canQueueBillForSML(false, true), false)
  assert.equal(canQueueBillForSML(true, true), true)
})
