import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { shouldOpenTimelineFromQuery } = await vite.ssrLoadModule(
  '/src/lib/shopee-timeline-dialog.ts',
)

test.after(async () => {
  await vite.close()
})

test('opens a timeline when a query selects an order', () => {
  assert.equal(shouldOpenTimelineFromQuery('ORDER-1', false, ''), true)
})

test('does not reopen the same order while its query is being removed', () => {
  assert.equal(shouldOpenTimelineFromQuery('ORDER-1', false, 'ORDER-1'), false)
})

test('opens a different deep-linked order after dismissing the previous order', () => {
  assert.equal(shouldOpenTimelineFromQuery('ORDER-2', false, 'ORDER-1'), true)
})

test('does not open without an order or while the timeline is already open', () => {
  assert.equal(shouldOpenTimelineFromQuery('', false, ''), false)
  assert.equal(shouldOpenTimelineFromQuery('ORDER-1', true, ''), false)
})
