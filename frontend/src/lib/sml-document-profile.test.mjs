import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveProfileTemplate, validateProfileText } from './smlDocumentProfile.js'

test('resolves only bounded document tokens', () => {
  assert.equal(
    resolveProfileTemplate('{{channel}} | {{order_ref}} | {{bill_no}}', {
      channel: 'Shopee API', order_ref: 'ORDER-1', bill_no: 'BF-1',
    }),
    'Shopee API | ORDER-1 | BF-1',
  )
})

test('rejects unknown tokens, control characters, and more than 255 unicode characters', () => {
  assert.match(validateProfileText('{{buyer_name}}', true), /token/)
  assert.match(validateProfileText('safe\nunsafe'), /อักขระควบคุม/)
  assert.match(validateProfileText('ก'.repeat(256)), /255/)
  assert.equal(validateProfileText('ก'.repeat(255)), '')
})
