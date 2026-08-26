import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { shouldMergeAutoSMLSuccessStatus } = await vite.ssrLoadModule(
  '/src/lib/shopee-operations-status.ts',
)

test.after(async () => {
  await vite.close()
})

test('merges a completed Auto SML result into the sent ERP status', () => {
  assert.equal(shouldMergeAutoSMLSuccessStatus('sent', 'BF-INV26080055', 'succeeded'), true)
})

test('keeps non-success Auto SML states as a separate status row', () => {
  assert.equal(shouldMergeAutoSMLSuccessStatus('sent', 'BF-INV26080055', 'needs_review'), false)
  assert.equal(shouldMergeAutoSMLSuccessStatus('failed', 'BF-INV26080055', 'succeeded'), false)
})

test('does not claim Auto SML completion without an SML document number', () => {
  assert.equal(shouldMergeAutoSMLSuccessStatus('sent', '', 'succeeded'), false)
})
